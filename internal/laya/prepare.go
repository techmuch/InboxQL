package laya

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/gomlx/compute-onnx/support/protos"
	"google.golang.org/protobuf/proto"
)

// # Preparing a stock Laya export
//
// The published ONNX file is float16, which is the right choice for the runtime
// it was exported for: onnxruntime-web on a GPU. The pure Go backend here
// implements layer normalisation for float32 and float64 only, so the stock
// file parses, builds a graph, and then fails at execution with
//
//	FusedLayerNorm: dtype Float16: op not implemented
//
// several hundred nodes in, saying nothing about the cause.
//
// So the weights are widened to float32 once, at install time. This is not an
// approximation in the direction that matters — every float16 value is exactly
// representable as a float32 — it only costs disk: 616 MB becomes 1230 MB.
//
// Going the other way, and teaching the backend float16, would be the better
// fix and belongs upstream. This is the version that works today without
// vendoring a numerics kernel.

// ONNX TensorProto data types.
const (
	dtFloat   = 1
	dtFloat16 = 10
)

// Prepare widens a float16 export to float32.
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

	var widened int
	for _, t := range g.Initializer {
		if widenTensor(t) {
			widened++
		}
	}
	for _, n := range g.Node {
		for _, a := range n.Attribute {
			if widenTensor(a.T) {
				widened++
			}
			// A Cast that targeted float16 would narrow the widened values
			// straight back, and the layer norm after it would fail exactly as
			// before.
			if n.OpType == "Cast" && a.Name == "to" && a.I == dtFloat16 {
				a.I = dtFloat
			}
		}
	}
	// Declared types, including every intermediate: the converter reads these
	// to decide what a node produces, so leaving them at float16 contradicts
	// the data now in the initialisers.
	for _, vs := range [][]*protos.ValueInfoProto{g.Input, g.Output, g.ValueInfo} {
		for _, v := range vs {
			if v.Type == nil {
				continue
			}
			if tt := v.Type.GetTensorType(); tt != nil && tt.ElemType == dtFloat16 {
				tt.ElemType = dtFloat
			}
		}
	}

	if widened == 0 {
		return fmt.Errorf("no float16 tensors found; this file is not a stock Laya export")
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

// widenTensor rewrites one float16 tensor as float32 in place.
func widenTensor(t *protos.TensorProto) bool {
	if t == nil || t.DataType != dtFloat16 {
		return false
	}
	switch {
	case len(t.RawData) > 0:
		n := len(t.RawData) / 2
		out := make([]byte, n*4)
		for i := 0; i < n; i++ {
			v := halfToFloat(binary.LittleEndian.Uint16(t.RawData[i*2:]))
			binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
		}
		t.RawData = out
	case len(t.Int32Data) > 0:
		// ONNX stores float16 bit-wise in int32_data when raw_data is unused.
		out := make([]float32, len(t.Int32Data))
		for i, b := range t.Int32Data {
			out[i] = halfToFloat(uint16(b))
		}
		t.FloatData, t.Int32Data = out, nil
	}
	t.DataType = dtFloat
	return true
}

// halfToFloat converts an IEEE-754 binary16 bit pattern to float32.
//
// Written out rather than pulled in: the subnormal and NaN cases are the ones a
// shortcut gets wrong, and they are the ones that would show up as a handful of
// wrong answers rather than as a crash.
func halfToFloat(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h) & 0x3ff

	switch exp {
	case 0:
		if mant == 0 {
			return math.Float32frombits(sign) // signed zero
		}
		// Subnormal in binary16 is normal in binary32: shift the mantissa up
		// until its implicit leading bit appears, dropping the exponent to
		// match.
		e := uint32(127 - 15 + 1)
		for mant&0x400 == 0 {
			mant <<= 1
			e--
		}
		return math.Float32frombits(sign | e<<23 | (mant&0x3ff)<<13)
	case 0x1f:
		// Infinity or NaN; the mantissa carries the payload.
		return math.Float32frombits(sign | 0xff<<23 | mant<<13)
	default:
		return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
	}
}
