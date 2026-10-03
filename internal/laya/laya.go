// Package laya answers typed questions about a text in one forward pass.
//
// # Why this exists beside the other two
//
// A rule annotator is a query: free, exact, and unable to answer anything the
// query language cannot say. A span annotator reads values out of the text and
// cannot label at all. So every label needing judgement — "is this actually a
// purchase, or a newsletter that mentions a price" — had to be the LLM, which
// is the engine that answered a receipt with three "N/A" fabrications at
// confidence 1.0.
//
// Laya is an encoder with a scoring head. It emits no tokens: the only thing it
// can return is a probability over the options it was given, so a label it has
// never been offered cannot appear. One question is about a second on this
// machine, against roughly forty for a local generative model.
//
// # What the model is
//
// mmBERT-base with a decision head. The question is written into the sequence
// as text, with one [MASK] marker per option:
//
//	[CLS] noul question: does the sender want a refund [SEP]
//	[MASK] false: no, … [MASK] true: yes, … [SEP] the message [SEP]
//
// The head scores the hidden state at each marker, and a softmax over those
// scores is the answer. Three question shapes fall out of that one mechanism:
// pick one of n (choice), where on a scale (score), and is this true (noul).
//
// # What it costs, and what it does not promise
//
// The weights are not in the binary; see [Open]. Inference is pure Go — no cgo,
// no ONNX Runtime — at roughly a second a question at the default length.
//
// **The shipped probabilities are not calibrated.** The checkpoint ships with
// every temperature at 1.0 and the upstream project measures its expected
// calibration error at 0.466, which is close to meaningless; their own source
// says the shipped checkpoints are over-confident and that a threshold applied
// to them "selects below model accuracy". So [Decision.Probability] is an
// ordering, not a frequency, until a temperature has been fitted on real
// corrections. Nothing here claims otherwise.
package laya

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gomlx/compute"
	_ "github.com/gomlx/compute/gobackend" // the pure Go backend
	"github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/gomlx/onnx-gomlx/onnx/parser"
	sp "github.com/tggo/goSentencePiece"
)

// Special token ids, from the checkpoint's tokenizer_config.json.
//
// mmBERT uses the Gemma vocabulary, where the roles are played by tokens with
// other names: the classifier position is <bos> and the separator is <eos>.
// Named for their role here rather than their spelling there, because the role
// is what the sequence layout means.
const (
	idPad  = 0
	idSep  = 1 // <eos>
	idCLS  = 2 // <bos>
	idMask = 4 // <mask>, the option marker
)

// Question types, in the order the model's type embedding expects. These
// values go into the graph as `qtype` and are not ours to renumber.
const (
	qtypeChoice = 0
	qtypeScore  = 1
	qtypeNoul   = 2
)

// Budgets.
//
// MaxLen is well below the checkpoint's own 1024 because cost is linear in
// sequence length here — about 9.5 ms a token — so 1024 would be ten seconds a
// question. At 256 the state still gets ~200 tokens, which is a subject and the
// first paragraph or two: enough to decide what a message is about, which is
// the only thing this engine is for.
const (
	MaxLen     = 256 // the whole sequence
	HeadMaxLen = 96  // the question half, leaving the rest for the message
	MaxOptions = 32
	optMaxLen  = 48 // tokens per rendered option
)

// ModelFiles are what an installation must contain.
var ModelFiles = []string{"model.onnx", "tokenizer.json"}

// Question is one typed thing to decide about a text.
type Question struct {
	// Kind is "choice", "score" or "noul".
	Kind string
	// Instructions is the question in words. It is read as text, so it reads
	// like a question rather than like a prompt: the model is scoring options
	// against it, not following it.
	Instructions string
	// Options are the choices, in answer order. For a noul they are supplied
	// automatically and must be empty.
	Options []Option
}

// Option is one answer the model may give, and what it means.
type Option struct {
	// Label is what comes back as the answer.
	Label string
	// Describe is an optional gloss. It costs tokens from the head budget and
	// is usually worth it: "billing: invoices, payments and refunds" scores
	// better than "billing" alone.
	Describe string
}

// Decision is one answer, with the whole distribution behind it.
type Decision struct {
	// Label is the chosen option. For a noul it is "true" or "false"; for a
	// score it is the level's name.
	Label string
	// Index is the chosen option's position.
	Index int
	// Probability is the mass on Label. See the package comment: this is an
	// ordering until a temperature has been fitted, not a frequency.
	Probability float64
	// Probabilities is the full distribution, in option order.
	Probabilities []float64
	// Level is the expected level for a score question — the probability
	// weighted mean rather than the argmax, because "mostly 2 with some 3" is
	// a different answer from "certainly 2" and the mean is the one that says
	// so.
	Level float64
	// Escalate is the model's own view that this one should go to something
	// larger. It comes from a second head trained for that, not from the
	// probabilities, so it is reported rather than derived.
	Escalate float64
}

// Calibrate rescales this decision's probabilities with a fitted temperature.
//
// The ordering is unchanged — the answer stays the answer — so this cannot make
// the model more or less accurate. It only makes the number mean what it looks
// like it means.
func (d *Decision) Calibrate(t *Temperature) {
	if t == nil || len(d.Probabilities) != 2 {
		return
	}
	pTrue := t.Rescale(d.Probabilities[1])
	d.Probabilities = []float64{1 - pTrue, pTrue}
	if d.Index == 1 {
		d.Probability = pTrue
	} else {
		d.Probability = 1 - pTrue
	}
	d.Level = pTrue
}

// Model is a loaded checkpoint.
type Model struct {
	dir  string
	card Card
	tok  *sp.Tokenizer
	exec *model.Exec

	// One sequence length at a time: the graph is compiled per shape, and a
	// message that happened to tokenize two tokens shorter would otherwise pay
	// for a recompile. Everything is padded to MaxLen instead.
	mu sync.Mutex
}

// Dir is where the weights live under a data directory.
func Dir(dataDir string) string { return filepath.Join(dataDir, "models", "laya") }

// Installed reports whether a usable checkpoint is on disk.
func Installed(dataDir string) bool {
	dir := Dir(dataDir)
	for _, f := range ModelFiles {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil || st.Size() == 0 {
			return false
		}
	}
	return true
}

// Open loads the model from a data directory.
//
// The weights are not in the binary and are not fetched on demand, for the same
// reason the span model's are not: a gigabyte arriving because somebody ran a
// command that mentioned an annotator is not a surprise anyone should have.
func Open(dataDir string) (*Model, error) {
	dir := Dir(dataDir)
	if !Installed(dataDir) {
		return nil, fmt.Errorf(
			"no Laya model in %s.\nInstall one with `iql laya install`", dir)
	}

	tok, err := sp.NewTokenizerFromJSON(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, fmt.Errorf("reading the tokenizer: %w", err)
	}

	om, err := parser.ParseFile(filepath.Join(dir, "model.onnx"))
	if err != nil {
		return nil, fmt.Errorf("reading the model: %w", err)
	}
	if err := checkInputs(om); err != nil {
		return nil, err
	}
	om = om.AllowDTypePromotion()

	store := model.NewStore()
	if err := om.VariablesToScope(store.RootScope()); err != nil {
		return nil, fmt.Errorf("reading the weights: %w", err)
	}

	backend, err := compute.New()
	if err != nil {
		return nil, fmt.Errorf("starting the compute backend: %w", err)
	}

	exec, err := model.NewExec(backend, store,
		func(scope *model.Scope, in []*graph.Node) []*graph.Node {
			return om.CallGraph(scope, in[0].Graph(), map[string]*graph.Node{
				"input_ids":      in[0],
				"attention_mask": in[1],
				"marker_pos":     in[2],
				"marker_mask":    in[3],
				"qtype":          in[4],
			}, "logits", "act_logits")
		})
	if err != nil {
		return nil, fmt.Errorf("building the graph: %w", err)
	}

	return &Model{dir: dir, card: ReadCard(dataDir), tok: tok, exec: exec}, nil
}

// checkInputs rejects a file that is not the export this code understands.
//
// A stock fp16 export parses and then fails at execution with "FusedLayerNorm:
// dtype Float16: op not implemented", several hundred nodes in and saying
// nothing about the cause. The installed file is upcast to fp32 at install
// time; this catches the other one early.
func checkInputs(om onnx.Model) error {
	names, _ := om.Inputs()
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, want := range []string{"input_ids", "attention_mask", "marker_pos", "marker_mask", "qtype"} {
		if !have[want] {
			return fmt.Errorf(
				"this model file is not a Laya export (no %q input).\n"+
					"Use the one `iql laya install` fetches", want)
		}
	}
	return nil
}

// Decide answers one question about one text.
func (m *Model) Decide(text string, q Question) (*Decision, error) {
	opts, err := renderOptions(q)
	if err != nil {
		return nil, err
	}
	qt, err := qtypeOf(q.Kind)
	if err != nil {
		return nil, err
	}

	ids, markers, err := m.buildSequence(text, q.Kind, q.Instructions, opts)
	if err != nil {
		return nil, err
	}

	logits, act, err := m.forward(ids, markers, qt)
	if err != nil {
		return nil, err
	}
	return decide(q, opts, logits[:len(markers)], act), nil
}

// buildSequence lays out the question and the text as one token sequence.
//
//	[CLS] <kind> question: <instructions> [SEP] [MASK] opt0 [MASK] opt1 … [SEP] text [SEP]
//
// The marker positions are returned because the head reads the hidden state at
// exactly those indices. This layout is the checkpoint's, not a choice — the
// weights were trained against it, and a sequence assembled any other way
// returns confident nonsense rather than an error.
func (m *Model) buildSequence(text, kind, instructions string, opts []string) (ids []int, markers []int, err error) {
	return m.buildSequenceWith(m.encode, text, kind, instructions, opts)
}

// buildSequenceWith is buildSequence with the tokenizer supplied, so the token
// budget — the part with the arithmetic in it — can be tested without loading
// a gigabyte of weights.
func (m *Model) buildSequenceWith(encode func(string) ([]int, error), text, kind, instructions string, opts []string) (ids []int, markers []int, err error) {
	head, err := encode(fmt.Sprintf("%s question: %s", kind, strings.TrimSpace(instructions)))
	if err != nil {
		return nil, nil, err
	}

	optIDs := make([][]int, len(opts))
	for i, o := range opts {
		enc, err := encode(" " + o)
		if err != nil {
			return nil, nil, err
		}
		if len(enc) > optMaxLen {
			enc = enc[:optMaxLen]
		}
		optIDs[i] = append([]int{idMask}, enc...)
	}

	// The options are the part that cannot be cut without changing the
	// question, so they get the budget first and the instructions take what is
	// left. When even the options overrun, each is trimmed equally rather than
	// dropping the last ones — a missing option cannot be chosen, which would
	// silently change the answer set.
	used := 0
	for _, o := range optIDs {
		used += len(o)
	}
	if budget := HeadMaxLen - used; budget < 16 {
		per := max(4, (HeadMaxLen-16)/max(1, len(optIDs)))
		for i := range optIDs {
			if len(optIDs[i]) > per {
				optIDs[i] = optIDs[i][:per]
			}
		}
		used = 0
		for _, o := range optIDs {
			used += len(o)
		}
	}
	if room := max(8, HeadMaxLen-used); len(head) > room {
		head = head[:room]
	}

	ids = append([]int{idCLS}, head...)
	ids = append(ids, idSep)
	for _, o := range optIDs {
		markers = append(markers, len(ids))
		ids = append(ids, o...)
	}
	ids = append(ids, idSep)

	body, err := encode(text)
	if err != nil {
		return nil, nil, err
	}
	if room := MaxLen - len(ids) - 1; room > 0 {
		if len(body) > room {
			body = body[:room]
		}
		ids = append(ids, body...)
	}
	ids = append(ids, idSep)
	if len(ids) > MaxLen {
		ids = ids[:MaxLen]
	}

	kept := markers[:0]
	for _, p := range markers {
		if p < len(ids) {
			kept = append(kept, p)
		}
	}
	markers = kept
	if len(markers) == 0 {
		return nil, nil, fmt.Errorf("no options survived the token budget")
	}
	return ids, markers, nil
}

// encode tokenizes without special tokens; this package adds its own.
func (m *Model) encode(s string) ([]int, error) {
	enc := m.tok.EncodeWithOptions(s, false)
	if enc == nil {
		return nil, fmt.Errorf("tokenizing %q failed", truncate(s, 40))
	}
	return enc.IDs, nil
}

// forward runs one padded sequence through the graph.
func (m *Model) forward(ids, markers []int, qt int) (logits []float64, escalate float64, err error) {
	padded := make([]int64, MaxLen)
	mask := make([]int64, MaxLen)
	for i, id := range ids {
		padded[i], mask[i] = int64(id), 1
	}
	for i := len(ids); i < MaxLen; i++ {
		padded[i] = idPad
	}

	pos := make([]int64, MaxOptions)
	mmask := make([]bool, MaxOptions)
	for i, p := range markers {
		pos[i], mmask[i] = int64(p), true
	}

	// Serialised: the graph is compiled per input shape and the executor is
	// shared, so two callers at once is a data race on it rather than a win.
	m.mu.Lock()
	defer m.mu.Unlock()

	out, err := m.exec.Call(
		tensors.FromValue([][]int64{padded}),
		tensors.FromValue([][]int64{mask}),
		tensors.FromValue([][]int64{pos}),
		tensors.FromValue([][]bool{mmask}),
		tensors.FromValue([]int64{int64(qt)}),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("running the model: %w", err)
	}

	raw, ok := out[0].Value().([][]float32)
	if !ok || len(raw) == 0 {
		return nil, 0, fmt.Errorf("the model returned %T, not logits", out[0].Value())
	}
	logits = make([]float64, len(raw[0]))
	for i, v := range raw[0] {
		logits[i] = float64(v)
	}

	if a, ok := out[1].Value().([][]float32); ok && len(a) > 0 && len(a[0]) == 2 {
		e := softmax([]float64{float64(a[0][0]), float64(a[0][1])})
		escalate = e[1]
	}
	return logits, escalate, nil
}

// decide turns marker logits into an answer.
func decide(q Question, opts []string, logits []float64, escalate float64) *Decision {
	p := softmax(logits)

	best := 0
	for i, v := range p {
		if v > p[best] {
			best = i
		}
	}

	d := &Decision{
		Index: best, Probability: p[best],
		Probabilities: p, Escalate: escalate,
	}
	for i, v := range p {
		d.Level += float64(i) * v
	}

	switch q.Kind {
	case "noul":
		d.Label = []string{"false", "true"}[min(best, 1)]
	default:
		if best < len(q.Options) {
			d.Label = q.Options[best].Label
		} else if best < len(opts) {
			d.Label = opts[best]
		}
	}
	return d
}

// renderOptions writes each option as the text the model scores.
//
// The wording is the checkpoint's. A noul's two options are supplied here
// rather than by the caller because the model was trained against these exact
// strings, and a caller inventing its own would be asking a different question
// than the one the weights answer.
func renderOptions(q Question) ([]string, error) {
	switch q.Kind {
	case "noul":
		if len(q.Options) != 0 {
			return nil, fmt.Errorf("a noul question has no options of its own")
		}
		return []string{
			"false: no, the statement does not hold",
			"true: yes, the statement holds",
		}, nil
	case "choice", "score":
		if len(q.Options) < 2 {
			return nil, fmt.Errorf("a %s question needs at least two options", q.Kind)
		}
		if len(q.Options) > MaxOptions {
			return nil, fmt.Errorf("a question takes at most %d options, got %d",
				MaxOptions, len(q.Options))
		}
		out := make([]string, len(q.Options))
		for i, o := range q.Options {
			switch {
			case q.Kind == "score":
				out[i] = fmt.Sprintf("level %d: %s", i, firstNonEmpty(o.Describe, o.Label))
			case o.Describe == "":
				out[i] = o.Label
			default:
				out[i] = o.Label + ": " + o.Describe
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown question kind %q (choice, score or noul)", q.Kind)
	}
}

func qtypeOf(kind string) (int, error) {
	switch kind {
	case "choice":
		return qtypeChoice, nil
	case "score":
		return qtypeScore, nil
	case "noul":
		return qtypeNoul, nil
	}
	return 0, fmt.Errorf("unknown question kind %q", kind)
}

func softmax(x []float64) []float64 {
	if len(x) == 0 {
		return nil
	}
	hi := x[0]
	for _, v := range x {
		if v > hi {
			hi = v
		}
	}
	out := make([]float64, len(x))
	var sum float64
	for i, v := range x {
		out[i] = math.Exp(v - hi)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
