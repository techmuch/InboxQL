// Package gliner extracts entities by scoring spans of the text itself.
//
// # Why this exists beside the LLM extractor
//
// An LLM extractor is asked for JSON and returns whatever it likes. Run over
// this mailbox it answered an order confirmation with
// {"due date":"N/A","invoice number":"N/A", ...} at confidence 1.0 — three
// values that are not in the message, presented as certainties, which
// `extract:x.due_date` would then match. A span model cannot do that. The only
// thing it can return is a pair of offsets into the text it was given, so
// every value it produces is, by construction, a substring of the message.
//
// It is not better at everything. It reads 160 words at a time, it has no idea
// what a field means beyond the words of its name, and it will happily offer
// "Table 31" as an invoice number. But it offers it at 0.52 against 0.88 for a
// real amount, and a person checking the claim sees the same characters the
// model scored.
//
// # What the model is
//
// GLiNER v1: a DeBERTa-v3 encoder, a span-scoring head, and a label set
// supplied at inference time rather than trained in. The prompt is the labels
// themselves —
//
//	[CLS] <<ENT>> amount <<ENT>> due date <<SEP>> the message words … [SEP]
//
// — and the output is a score for every (start word, width, label) triple. No
// generation, no sampling, no instruction following.
//
// # What it costs
//
// The weights are a few hundred megabytes on disk and are not shipped with the
// binary; see [Open]. Inference is pure Go — no cgo, no ONNX Runtime, no
// shared library to install — at roughly three seconds per message on a laptop
// CPU, against about forty for a local generative model doing the same job.
package gliner

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/gomlx/compute"
	_ "github.com/gomlx/compute/gobackend" // the pure Go backend
	"github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/gomlx/onnx-gomlx/onnx/parser"
	sp "github.com/tggo/goSentencePiece"
)

// Token ids that are not in spm.model; they come from added_tokens.json.
const (
	idCLS  = 1
	idSEP  = 2
	idUNK  = 3
	idENT  = 128002 // <<ENT>>
	idSEPP = 128003 // <<SEP>>
)

// The fixed shapes the prepared graph is compiled for.
//
// They are constants rather than limits discovered at runtime because the
// graph is compiled once, ahead of time, against exactly these numbers —
// see the package's model preparation notes. MaxLabels is the reason an
// annotator's schema cannot carry more than twelve fields; Window and MaxLen
// are why a long message is read in several passes.
const (
	// Window is how many words one pass reads.
	Window = 160
	// MaxLen is the subtoken budget for one pass, prompt included. Whichever
	// of the two runs out first ends the pass.
	MaxLen = 384
	// MaxWidth is the longest span, in words, the model can propose.
	MaxWidth = 12
	// MaxLabels is how many labels one pass can score.
	MaxLabels = 12
	// overlap is how many words two consecutive passes share, so an entity
	// that straddles a window boundary is still seen whole by one of them.
	overlap = 16
	// MaxWindows bounds how much of one message is read.
	//
	// A pass costs a couple of seconds, so a fifty-kilobyte marketing email
	// would otherwise take minutes on its own and a mailbox of them an
	// afternoon. This is the same admission the embedding pass makes with
	// maxEmbeddingChars, and for the same reason: the first part of a document
	// is the part that carries what identifies it, and a bounded answer that
	// arrives is worth more than a complete one that does not.
	//
	// [Reach] reports what fraction was actually read, so nothing downstream
	// has to guess whether "no amount found" means the message has none or
	// that nobody looked at the end of it.
	MaxWindows = 8
)

// ModelFiles are what [Open] needs to find in its directory.
var ModelFiles = []string{"model.onnx", "spm.model"}

// padLabels fill the unused class slots when a caller asks for fewer than
// MaxLabels.
//
// Ordinary entity types rather than nonsense strings: the point is to occupy a
// slot the way a real label would. This is in distribution — GLiNER is trained
// with negative types alongside the real ones — and the scores for these
// columns are discarded.
var padLabels = []string{
	"colour", "animal", "vehicle", "sport", "plant", "mineral",
	"instrument", "planet", "language", "fabric", "weather", "tool",
}

// Span is one entity the model found, and where it is.
type Span struct {
	Label string `json:"label"`
	// Text is taken from the source by offset. It is never something the
	// model composed, which is the property the whole package exists for.
	Text  string  `json:"text"`
	Start int     `json:"start"` // byte offset into the text, inclusive
	End   int     `json:"end"`   // byte offset into the text, exclusive
	Score float64 `json:"score"` // this span's own probability, not the pass's
}

// Model is a loaded GLiNER.
//
// Safe for concurrent use: the graph is immutable once compiled and the
// tokenizer is stateless, but the single compiled executable is serialised
// because it holds working buffers.
type Model struct {
	dir  string
	card Card
	enc  *sp.Encoder
	exec *model.Exec
	mu   sync.Mutex
}

// Dir is where the weights live under a data directory.
func Dir(dataDir string) string { return filepath.Join(dataDir, "models", "gliner") }

// Installed reports whether a usable model is present.
//
// Used by the health check, so it answers the question a person would ask —
// "can I run this?" — without loading three-quarters of a gigabyte to find out.
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
// The weights are not in the binary and are not fetched on demand. A few
// hundred megabytes arriving because somebody ran a command that mentioned an
// annotator is not a surprise anyone should have; `iql gliner install` puts
// them there deliberately, and this says plainly when they are missing.
func Open(dataDir string) (*Model, error) {
	dir := Dir(dataDir)
	if !Installed(dataDir) {
		return nil, fmt.Errorf(
			"no GLiNER model in %s.\nInstall one with `iql gliner install`", dir)
	}

	spm, err := sp.LoadModel(filepath.Join(dir, "spm.model"))
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

	store := model.NewStore()
	if err := om.VariablesToScope(store.RootScope()); err != nil {
		return nil, fmt.Errorf("reading the weights: %w", err)
	}

	backend, err := compute.New()
	if err != nil {
		return nil, fmt.Errorf("starting the compute backend: %w", err)
	}

	// The slice form rather than a typed one: the graph takes seven inputs and
	// the typed variants stop at six.
	exec, err := model.NewExec(backend, store,
		func(scope *model.Scope, in []*graph.Node) *graph.Node {
			return om.CallGraph(scope, in[0].Graph(), map[string]*graph.Node{
				"input_ids":        in[0],
				"attention_mask":   in[1],
				"words_mask":       in[2],
				"word_positions":   in[3],
				"prompt_positions": in[4],
				"span_idx":         in[5],
				"span_mask":        in[6],
			}, "logits")[0]
		})
	if err != nil {
		return nil, fmt.Errorf("building the graph: %w", err)
	}

	return &Model{dir: dir, card: ReadCard(dataDir), enc: sp.NewEncoder(spm), exec: exec}, nil
}

// checkInputs rejects a model that was not prepared for this code.
//
// A stock GLiNER export cannot be compiled ahead of time — it locates words
// and label markers with NonZero, whose output shape depends on its data — so
// the shipped model is a rewritten one that takes those positions as inputs.
// Loading a stock file would otherwise fail several hundred nodes later with a
// shape error that says nothing about the real cause.
func checkInputs(om onnx.Model) error {
	names, _ := om.Inputs()
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, want := range []string{"word_positions", "prompt_positions"} {
		if !have[want] {
			return fmt.Errorf(
				"this model file has not been prepared for InboxQL (no %q input).\n"+
					"Use the one `iql gliner install` fetches, not a stock GLiNER export", want)
		}
	}
	return nil
}

// Name identifies the model for provenance.
//
// It goes into annotations.model on every record, so it has to answer "which
// weights produced this", not just "which engine". The repository and the
// first bytes of the digest do that; a model dropped into the directory by
// hand has no card and says so, which is more useful than a confident name
// that means nothing.
func (m *Model) Name() string {
	switch {
	case m.card.Repo != "" && m.card.Digest != "":
		return fmt.Sprintf("gliner %s@%s", m.card.Repo, m.card.Digest[:12])
	case m.card.Repo != "":
		return "gliner " + m.card.Repo
	default:
		return "gliner (unrecorded)"
	}
}

// Word is one whitespace-delimited word and where it sits in the source.
type Word struct {
	Text  string
	Start int
	End   int
}

// SplitWords splits on unicode whitespace, keeping byte offsets.
//
// gliner_config.json says words_splitter_type "whitespace", so this is the
// model's own notion of a word. Punctuation stays attached, which is why a
// span can come back as "Marietta." — correct per the model, and tidied at the
// edges rather than here.
func SplitWords(text string) []Word {
	var out []Word
	start := -1
	for i, r := range text {
		if unicode.IsSpace(r) {
			if start >= 0 {
				out = append(out, Word{Text: text[start:i], Start: start, End: i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, Word{Text: text[start:], Start: start, End: len(text)})
	}
	return out
}

// Reach reports how far into a text [Extract] will read, in bytes.
//
// Returns len(text) when the whole thing fits. A caller that records this
// alongside the result can tell "this message has no amount in it" from "the
// part that was read has no amount in it", which are different answers that
// look identical from a row with no records.
func Reach(text string) int {
	words := SplitWords(text)
	if len(words) == 0 {
		return len(text)
	}
	// The same arithmetic Extract does, without running the model: each pass
	// advances by a window less the overlap, and the last one reaches its full
	// width.
	reached := Window + (MaxWindows-1)*(Window-overlap)
	if reached >= len(words) {
		return len(text)
	}
	return words[reached-1].End
}

// Extract finds every span in text that matches one of the labels.
//
// A message longer than one window is read in several overlapping passes and
// the results merged, so an entity that would have straddled a boundary is
// seen whole by one of them — up to [MaxWindows] passes, after which the rest
// of the message is not read. [Reach] says where that line falls.
func (m *Model) Extract(text string, labels []string, threshold float64) ([]Span, error) {
	if len(labels) == 0 {
		return nil, fmt.Errorf("no labels to look for")
	}
	if len(labels) > MaxLabels {
		return nil, fmt.Errorf(
			"this model scores at most %d labels at once, and %d were given",
			MaxLabels, len(labels))
	}

	words := SplitWords(text)
	if len(words) == 0 {
		return nil, nil
	}

	// Highest score wins per (label, offsets), because overlapping windows see
	// the same entity twice with slightly different context.
	best := map[Span]float64{}
	for from, passes := 0, 0; from < len(words) && passes < MaxWindows; passes++ {
		spans, fit, err := m.pass(text, words, from, labels, threshold)
		if err != nil {
			return nil, err
		}
		for _, s := range spans {
			key := Span{Label: s.Label, Start: s.Start, End: s.End}
			if prev, seen := best[key]; !seen || s.Score > prev {
				best[key] = s.Score
			}
		}
		if fit <= 0 {
			// A single word that does not fit the token budget at all. Skip
			// it rather than spinning: one unreadable word is not a reason to
			// abandon the message.
			from++
			continue
		}
		if from+fit >= len(words) {
			break
		}
		from += max(fit-overlap, 1)
	}

	out := make([]Span, 0, len(best))
	for key, score := range best {
		key.Score = score
		key.Text = strings.TrimSpace(text[key.Start:key.End])
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

// pass runs one window and reports how many words it managed to read.
func (m *Model) pass(text string, words []Word, from int, labels []string, threshold float64) ([]Span, int, error) {
	asked := len(labels)
	padded := append([]string{}, labels...)
	for i := 0; len(padded) < MaxLabels; i++ {
		padded = append(padded, padLabels[i%len(padLabels)])
	}

	// The prompt: [<<ENT>> label]* <<SEP>>, as words. A two-word label is one
	// prompt word that happens to tokenize into several subtokens.
	ids := []int{idCLS}
	entAt := make([]int, 0, MaxLabels)
	for _, l := range padded {
		entAt = append(entAt, len(ids))
		ids = append(ids, idENT)
		ids = append(ids, m.encodeWord(l)...)
	}
	ids = append(ids, idSEPP)

	// The text, recording where each word's first subtoken landed.
	wordsMask := make([]int64, len(ids)) // zero for [CLS] and the prompt
	firstSubtoken := make([]int, 0, Window)
	fit := 0
	for k := 0; k < Window && from+k < len(words); k++ {
		sub := m.encodeWord(words[from+k].Text)
		if len(ids)+len(sub)+1 > MaxLen { // room for the trailing [SEP]
			break
		}
		firstSubtoken = append(firstSubtoken, len(ids))
		for j := range sub {
			if j == 0 {
				wordsMask = append(wordsMask, int64(k+1))
			} else {
				wordsMask = append(wordsMask, 0)
			}
		}
		ids = append(ids, sub...)
		fit = k + 1
	}
	ids = append(ids, idSEP)
	wordsMask = append(wordsMask, 0)
	if fit == 0 {
		return nil, 0, nil
	}
	window := words[from : from+fit]

	// Padding is inert rather than merely tolerated: attention_mask is 0 over
	// the padded tokens, span_mask is false for every span that runs past the
	// real text, and each padded word slot points at the last real word's
	// first subtoken, so it resolves to a word the graph already writes
	// instead of reaching for one that does not exist.
	inputIDs := make([]int64, MaxLen)
	attMask := make([]int64, MaxLen)
	for i, id := range ids {
		inputIDs[i] = int64(id)
		attMask[i] = 1
	}
	for len(wordsMask) < MaxLen {
		wordsMask = append(wordsMask, 0)
	}
	wordsMask = wordsMask[:MaxLen]

	// word_positions and prompt_positions are what the graph's two NonZero
	// lookups would have returned: row 0 batch indices, row 1 token indices.
	last := int64(firstSubtoken[len(firstSubtoken)-1])
	wordBatch := make([]int64, Window)
	wordToken := make([]int64, Window)
	for i := range wordToken {
		if i < len(firstSubtoken) {
			wordToken[i] = int64(firstSubtoken[i])
		} else {
			wordToken[i] = last
		}
	}
	entBatch := make([]int64, MaxLabels)
	entToken := make([]int64, MaxLabels)
	for i, at := range entAt {
		entToken[i] = int64(at)
	}

	numSpans := Window * MaxWidth
	spanIdx := make([][]int64, numSpans)
	spanMask := make([]bool, numSpans)
	for s := 0; s < Window; s++ {
		for w := 0; w < MaxWidth; w++ {
			i := s*MaxWidth + w
			spanIdx[i] = []int64{int64(s), int64(s + w)}
			spanMask[i] = s+w <= fit-1
		}
	}

	m.mu.Lock()
	out := m.exec.MustCall1(
		[][]int64{inputIDs},
		[][]int64{attMask},
		[][]int64{wordsMask},
		[][]int64{wordBatch, wordToken},
		[][]int64{entBatch, entToken},
		[][][]int64{spanIdx},
		[][]bool{spanMask},
	)
	m.mu.Unlock()

	flat, err := tensors.CopyFlatData[float32](out)
	if err != nil {
		return nil, fit, fmt.Errorf("reading the scores: %w", err)
	}
	shape := out.Shape()
	if shape.Rank() != 4 {
		return nil, fit, fmt.Errorf("scores have rank %d, want 4", shape.Rank())
	}
	L, K, C := shape.Dimensions[1], shape.Dimensions[2], shape.Dimensions[3]

	var cands []Span
	for l := 0; l < L && l < fit; l++ {
		for k := 0; k < K; k++ {
			end := l + k
			if end >= fit {
				break
			}
			// Only the columns that were asked for. C is the stride; the
			// padding labels occupy the columns past `asked`.
			for c := 0; c < asked; c++ {
				score := sigmoid(float64(flat[((l*K)+k)*C+c]))
				if score < threshold {
					continue
				}
				cands = append(cands, Span{
					Label: labels[c],
					Start: window[l].Start,
					End:   window[end].End,
					Score: score,
				})
			}
		}
	}

	return flatten(cands, window, text), fit, nil
}

// flatten resolves overlaps: highest score wins, and nothing may overlap it.
//
// GLiNER's own greedy decode. Without it the same three words come back as
// four different entities, each a prefix of the last.
func flatten(cands []Span, window []Word, text string) []Span {
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })

	taken := make([]bool, len(window))
	slot := func(byteOffset int) int {
		for i := range window {
			if window[i].Start == byteOffset {
				return i
			}
		}
		return -1
	}

	var kept []Span
	for _, c := range cands {
		lo, hi := slot(c.Start), -1
		for i := range window {
			if window[i].End == c.End {
				hi = i
				break
			}
		}
		if lo < 0 || hi < 0 {
			continue
		}
		clash := false
		for w := lo; w <= hi; w++ {
			if taken[w] {
				clash = true
				break
			}
		}
		if clash {
			continue
		}
		for w := lo; w <= hi; w++ {
			taken[w] = true
		}
		c.Text = strings.TrimSpace(text[c.Start:c.End])
		kept = append(kept, c)
	}
	return kept
}

// encodeWord returns the subtoken ids for one word.
//
// Each word is encoded on its own, which is what is_split_into_words means for
// a SentencePiece tokenizer: every word is treated as though a space preceded
// it, so it picks up the leading word-start marker.
func (m *Model) encodeWord(w string) []int {
	ids := m.enc.Encode(w)
	if len(ids) == 0 {
		return []int{idUNK}
	}
	return ids
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }
