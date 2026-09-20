package gliner

import (
	"fmt"
	"os"

	"github.com/gomlx/compute-onnx/support/protos"
	"google.golang.org/protobuf/proto"
)

// # Preparing a stock GLiNER export
//
// A GLiNER ONNX file as published cannot be compiled ahead of time, which is
// what running it without a C++ runtime requires. Three things stand in the
// way, and all three are the export recomputing something the caller already
// knows:
//
//   - It locates each word's first subtoken with NonZero(words_mask > 0), and
//     the label markers with NonZero(input_ids == <<ENT>>). NonZero's output
//     shape depends on its data, so no shape can be known before a value
//     arrives — but the caller built words_mask and wrote the prompt, so it
//     knows both answers exactly. They become inputs.
//   - It counts the label markers to size the label-embedding buffer. The
//     caller always writes the same number of markers, padding a shorter
//     label set, so the count is a constant.
//   - It sorts the batch by length to pack the LSTM, then unsorts with
//     ScatterElements. At batch size one there is only one permutation.
//
// Plus two ops that are legal ONNX but unimplemented in the Go converter, each
// replaced by an exact equivalent.
//
// The result is a graph with no data-dependent shapes, which compiles once and
// runs for every message. Nothing here is an approximation: each edit either
// supplies a value the caller already has, or replaces an operation with one
// that computes the same thing.
//
// This runs on the machine, once, at install time. It is why `iql gliner
// install` does not simply download a file.

// Node names in the export this understands.
//
// They are specific to the transformers.js-style export published as
// onnx-community/gliner_base and its siblings. A file that does not have them
// is not one this can prepare, and [Prepare] says so rather than producing a
// graph that fails later with a shape error five hundred nodes downstream.
const (
	nodeWordNonZero   = "/NonZero_2" // NonZero(words_mask > 0)
	nodeWordGreater   = "/Greater"   // its only feeder
	nodePromptNonZero = "/NonZero_1" // NonZero(input_ids == <<ENT>>)
	nodeFlatten       = "/Flatten_1" // Flatten(words_mask, axis=2)
	nodeUnsort        = "/rnn/ScatterElements"
	nodeLabelCount    = "/ReduceSum" // counts the <<ENT>> markers

	inputWordPositions   = "word_positions"
	inputPromptPositions = "prompt_positions"
	inputTextLengths     = "text_lengths"
)

// Prepare rewrites a stock GLiNER export into one this package can run.
func Prepare(srcPath, dstPath string) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("reading the model: %w", err)
	}
	var m protos.ModelProto
	if err := proto.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("parsing the model: %w", err)
	}
	g := m.Graph
	if g == nil {
		return fmt.Errorf("the model has no graph")
	}

	if err := rewireNonZero(g, nodePromptNonZero, inputPromptPositions, MaxLabels); err != nil {
		return err
	}
	if err := rewireNonZero(g, nodeWordNonZero, inputWordPositions, Window); err != nil {
		return err
	}
	dropNode(g, nodeWordGreater) // nothing reads it now

	if err := flattenAsReshape(g); err != nil {
		return err
	}
	if err := replaceWithConstant(g, nodeUnsort, int64Tensor("gliner.unsort", []int64{1}, []int64{0})); err != nil {
		return err
	}
	if err := replaceWithConstant(g, nodeLabelCount, int64Tensor("gliner.labels", []int64{1, 1}, []int64{MaxLabels})); err != nil {
		return err
	}
	if err := foldTextLengths(g); err != nil {
		return err
	}
	if err := pinInputShapes(g); err != nil {
		return err
	}

	out, err := proto.Marshal(&m)
	if err != nil {
		return fmt.Errorf("serialising the model: %w", err)
	}
	if err := os.WriteFile(dstPath, out, 0o644); err != nil {
		return fmt.Errorf("writing the model: %w", err)
	}
	return nil
}

// rewireNonZero replaces one NonZero with a graph input of the same shape.
//
// NonZero returns indices as [rank, count] — row 0 the batch index, row 1 the
// position — so the replacement input has exactly that shape, and every node
// downstream of it is untouched.
func rewireNonZero(g *protos.GraphProto, nodeName, inputName string, count int) error {
	node := findNode(g, nodeName)
	if node == nil {
		return unknownExport(nodeName)
	}
	target := node.Output[0]
	dropNode(g, nodeName)

	rewired := 0
	for _, n := range g.Node {
		for i, in := range n.Input {
			if in == target {
				n.Input[i] = inputName
				rewired++
			}
		}
	}
	if rewired == 0 {
		return fmt.Errorf("nothing reads %s, so this export is not the expected one", target)
	}

	g.Input = append(g.Input, valueInfo(inputName, protos.TensorProto_INT64, []int64{2, int64(count)}))
	return nil
}

// flattenAsReshape swaps a Flatten this converter does not implement for the
// Reshape that means the same thing.
//
// Flatten(x, axis=2) on a rank-2 tensor is [d0*d1, 1], which is legal ONNX and
// exactly what Reshape to [-1, 1] produces.
func flattenAsReshape(g *protos.GraphProto) error {
	node := findNode(g, nodeFlatten)
	if node == nil {
		return unknownExport(nodeFlatten)
	}
	shapeName := "gliner.flatten_shape"
	g.Initializer = append(g.Initializer,
		int64Tensor(shapeName, []int64{2}, []int64{-1, 1}))

	node.OpType = "Reshape"
	node.Attribute = nil
	node.Input = []string{node.Input[0], shapeName}
	return nil
}

// replaceWithConstant turns a node into a Constant producing value.
//
// The node's output name is kept, so every consumer is untouched.
func replaceWithConstant(g *protos.GraphProto, nodeName string, value *protos.TensorProto) error {
	node := findNode(g, nodeName)
	if node == nil {
		return unknownExport(nodeName)
	}
	node.OpType = "Constant"
	node.Input = nil
	node.Attribute = []*protos.AttributeProto{{
		Name: "value",
		Type: protos.AttributeProto_TENSOR,
		T:    value,
	}}
	return nil
}

// foldTextLengths moves text_lengths from an input to a constant.
//
// It only ever carries the window size, and as an input it keeps a shape
// computation from resolving.
func foldTextLengths(g *protos.GraphProto) error {
	kept := g.Input[:0]
	found := false
	for _, in := range g.Input {
		if in.GetName() == inputTextLengths {
			found = true
			continue
		}
		kept = append(kept, in)
	}
	if !found {
		return unknownExport(inputTextLengths)
	}
	g.Input = kept
	g.Initializer = append(g.Initializer,
		int64Tensor(inputTextLengths, []int64{1, 1}, []int64{Window}))
	return nil
}

// pinInputShapes writes the real dimensions into the graph.
//
// Declared as [batch_size, sequence_length] a shape is genuinely not a
// constant, so nothing derived from it can be resolved before a value arrives.
// Every call passes the same shape, so saying so turns the whole shape algebra
// into arithmetic that can be done once.
func pinInputShapes(g *protos.GraphProto) error {
	want := map[string][]int64{
		"input_ids":          {1, MaxLen},
		"attention_mask":     {1, MaxLen},
		"words_mask":         {1, MaxLen},
		"span_idx":           {1, Window * MaxWidth, 2},
		"span_mask":          {1, Window * MaxWidth},
		inputWordPositions:   {2, Window},
		inputPromptPositions: {2, MaxLabels},
	}
	for _, in := range g.Input {
		dims, ok := want[in.GetName()]
		if !ok {
			continue
		}
		shape := in.GetType().GetTensorType().GetShape()
		if shape == nil || len(shape.Dim) != len(dims) {
			return fmt.Errorf("input %q has an unexpected rank", in.GetName())
		}
		for i, d := range dims {
			shape.Dim[i].Value = &protos.TensorShapeProto_Dimension_DimValue{DimValue: d}
		}
		delete(want, in.GetName())
	}
	if len(want) > 0 {
		for name := range want {
			return unknownExport(name)
		}
	}
	return nil
}

func findNode(g *protos.GraphProto, name string) *protos.NodeProto {
	for _, n := range g.Node {
		if n.GetName() == name {
			return n
		}
	}
	return nil
}

func dropNode(g *protos.GraphProto, name string) {
	kept := g.Node[:0]
	for _, n := range g.Node {
		if n.GetName() == name {
			continue
		}
		kept = append(kept, n)
	}
	g.Node = kept
}

func int64Tensor(name string, dims, values []int64) *protos.TensorProto {
	return &protos.TensorProto{
		Name:      name,
		Dims:      dims,
		DataType:  int32(protos.TensorProto_INT64),
		Int64Data: values,
	}
}

func valueInfo(name string, dt protos.TensorProto_DataType, dims []int64) *protos.ValueInfoProto {
	shape := &protos.TensorShapeProto{}
	for _, d := range dims {
		shape.Dim = append(shape.Dim, &protos.TensorShapeProto_Dimension{
			Value: &protos.TensorShapeProto_Dimension_DimValue{DimValue: d},
		})
	}
	return &protos.ValueInfoProto{
		Name: name,
		Type: &protos.TypeProto{
			Value: &protos.TypeProto_TensorType{
				TensorType: &protos.TypeProto_Tensor{
					ElemType: int32(dt),
					Shape:    shape,
				},
			},
		},
	}
}

func unknownExport(what string) error {
	return fmt.Errorf(
		"this does not look like a GLiNER export InboxQL can prepare: no %q.\n"+
			"It understands the onnx-community/gliner_* exports", what)
}
