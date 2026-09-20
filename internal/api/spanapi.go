package api

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// # A message's extracted spans, cut into runs
//
// The span engine's whole claim is that a value it returns is a piece of the
// message, so a surprising one can be checked against the characters the model
// scored. That check needs the offsets, and this is the only place they reach
// a reader.
//
// # Why the server does the slicing
//
// The stored offsets are BYTE offsets into UTF-8, as everything in this
// codebase is. JavaScript strings are UTF-16. Hand the browser raw offsets and
// it lands a byte or two short on exactly the messages that contain a narrow
// no-break space — the one a mail client writes in "9:50 PM" — and the result
// looks like a broken decoder rather than an encoding mismatch. That mistake
// has already been made once here, in a verification script, and it took an
// hour to see.
//
// So this returns the text already cut into runs. The client concatenates and
// marks; it never computes an offset, and the whole class of error is gone
// from the browser.

// Segment is one run of the message text, marked or not.
type Segment struct {
	Text string `json:"text"`
	// Label is empty for ordinary text. When set, this run is an extracted
	// value and the rest of the fields describe it.
	Label string `json:"label,omitempty"`
	// Score is this span's own probability, not the annotator's average.
	Score      *float64 `json:"score,omitempty"`
	Annotator  string   `json:"annotator,omitempty"`
	Annotation string   `json:"annotationId,omitempty"`
	// Source is rule, llm or human. A human ruling outranks the machine and
	// survives re-runs, so it is worth showing differently.
	Source string `json:"source,omitempty"`
}

// MarkedField is one part of a message — its subject or its body — as runs.
type MarkedField struct {
	Field    string    `json:"field"`
	Segments []Segment `json:"segments"`
	Marks    int       `json:"marks"`
}

// PlainAnnotation is a result that has no offsets: a label, or an extractor
// that wrote no span. Reported separately rather than dropped, so the panel
// can say what else matched without pretending it knows where.
type PlainAnnotation struct {
	Annotator string   `json:"annotator"`
	Kind      string   `json:"kind"`
	Engine    string   `json:"engine"`
	Status    string   `json:"status"`
	Source    string   `json:"source"`
	Score     *float64 `json:"score,omitempty"`
	Data      any      `json:"data,omitempty"`
	Model     string   `json:"model,omitempty"`
}

// SpanResponse is what a reader needs to show a message's extractions.
type SpanResponse struct {
	MessageID string            `json:"messageId"`
	Fields    []MarkedField     `json:"fields"`
	Other     []PlainAnnotation `json:"other"`
	// Labels present, so a legend can be built without walking the segments.
	Labels []string `json:"labels"`
}

// span is one extracted value, before segmentation.
type span struct {
	field      string
	start, end int
	label      string
	score      *float64
	annotator  string
	id         string
	source     string
}

func registerSpanRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/messages/{id}/annotations", handleMessageAnnotations)
	mux.HandleFunc("PUT /api/messages/{id}/annotations/{annotator}", handleCorrectSpans)
	mux.HandleFunc("DELETE /api/messages/{id}/annotations/{annotator}", handleClearSpans)
}

// handleCorrectSpans records a person's ruling on what a message contains.
//
// PUT rather than POST because it replaces: a correction is a statement about
// all of the message's values, not an edit to one of them. Sending an empty
// list is meaningful — "the machine found things and none of them are right" —
// and is not the same as DELETE, which withdraws the ruling entirely.
func handleCorrectSpans(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Spans []store.SpanCorrection `json:"spans"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	id, annotator := r.PathValue("id"), r.PathValue("annotator")
	if err := store.SetHumanSpans(annotator, id, req.Spans); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	handleMessageAnnotations(w, r)
}

func handleClearSpans(w http.ResponseWriter, r *http.Request) {
	id, annotator := r.PathValue("id"), r.PathValue("annotator")
	if err := store.ClearHumanSpans(annotator, id); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	handleMessageAnnotations(w, r)
}

func handleMessageAnnotations(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	msg, err := store.GetMessageByID(id)
	if err != nil || msg == nil {
		writeError(w, http.StatusNotFound, "no such message")
		return
	}

	anns, err := store.ListAnnotations(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}

	// Annotator metadata, so a span can say which annotator produced it
	// without a lookup per row.
	byID := map[string]*store.Annotator{}
	if list, err := store.ListAnnotators(); err == nil {
		for _, a := range list {
			byID[a.ID] = a
		}
	}

	out := SpanResponse{MessageID: id, Fields: []MarkedField{}, Other: []PlainAnnotation{}}
	var spans []span
	seenLabel := map[string]bool{}

	for _, a := range anns {
		meta := byID[a.AnnotatorID]
		name := ""
		if meta != nil {
			name = meta.Name
		}

		s, ok := spanOf(a, name)
		if !ok {
			p := PlainAnnotation{
				Annotator: name, Status: a.Status, Source: a.Source,
				Score: a.Confidence, Model: a.Model,
			}
			if meta != nil {
				p.Kind, p.Engine = meta.Kind, meta.Engine
			}
			if a.DataJSON != "" && a.DataJSON != "{}" {
				var v any
				if json.Unmarshal([]byte(a.DataJSON), &v) == nil {
					p.Data = v
				}
			}
			out.Other = append(out.Other, p)
			continue
		}
		spans = append(spans, s)
		if !seenLabel[s.label] {
			seenLabel[s.label] = true
			out.Labels = append(out.Labels, s.label)
		}
	}
	sort.Strings(out.Labels)

	for _, f := range []struct {
		name string
		text string
	}{
		{"subject", msg.Subject},
		{"body", bodyOf(msg)},
	} {
		marked := segment(f.text, spansIn(spans, f.name))
		out.Fields = append(out.Fields, marked)
		out.Fields[len(out.Fields)-1].Field = f.name
	}

	writeJSON(w, http.StatusOK, out)
}

// spanOf reads an annotation as a span, or reports that it is not one.
//
// A span record carries "field", "start" and "end" alongside its value; a
// label or an empty result carries none of them. Deciding from the row rather
// than from the engine means a human correction, which is written the same
// way, is marked the same way.
func spanOf(a *store.Annotation, annotator string) (span, bool) {
	if a.Status != store.StatusOK || a.DataJSON == "" {
		return span{}, false
	}
	var raw map[string]any
	if json.Unmarshal([]byte(a.DataJSON), &raw) != nil {
		return span{}, false
	}

	field, _ := raw["field"].(string)
	start, okStart := numberOf(raw["start"])
	end, okEnd := numberOf(raw["end"])
	if field == "" || !okStart || !okEnd || end <= start {
		return span{}, false
	}

	// The remaining key is the label, and its value is the extracted text.
	label := ""
	for k := range raw {
		if k != "field" && k != "start" && k != "end" {
			label = k
			break
		}
	}
	if label == "" {
		return span{}, false
	}

	return span{
		field: field, start: start, end: end, label: label,
		score: a.Confidence, annotator: annotator, id: a.ID, source: a.Source,
	}, true
}

func numberOf(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

func spansIn(all []span, field string) []span {
	var out []span
	for _, s := range all {
		if s.field == field {
			out = append(out, s)
		}
	}
	return out
}

// segment cuts text into marked and unmarked runs.
//
// Overlaps cannot happen within one annotator — the decode is greedy and
// non-overlapping — but two annotators over one message can collide, so the
// higher-scoring span wins and the loser is dropped rather than nesting marks
// the renderer would have to unpick.
func segment(text string, spans []span) MarkedField {
	out := MarkedField{Segments: []Segment{}}
	if text == "" {
		return out
	}
	if len(spans) == 0 {
		out.Segments = append(out.Segments, Segment{Text: text})
		return out
	}

	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return scoreOf(spans[i]) > scoreOf(spans[j])
	})

	cursor := 0
	for _, s := range spans {
		// Clamp rather than trust: an offset past the end means the body
		// changed under a stored annotation, and a panicking viewer is a worse
		// answer than an unmarked one.
		if s.start < cursor || s.start >= len(text) {
			continue
		}
		end := s.end
		if end > len(text) {
			end = len(text)
		}
		if end <= s.start {
			continue
		}

		if s.start > cursor {
			out.Segments = append(out.Segments, Segment{Text: text[cursor:s.start]})
		}
		out.Segments = append(out.Segments, Segment{
			Text: text[s.start:end], Label: s.label, Score: s.score,
			Annotator: s.annotator, Annotation: s.id, Source: s.source,
		})
		out.Marks++
		cursor = end
	}
	if cursor < len(text) {
		out.Segments = append(out.Segments, Segment{Text: text[cursor:]})
	}
	return out
}

func scoreOf(s span) float64 {
	if s.score == nil {
		return 0
	}
	return *s.score
}

// bodyOf is the text the span engine was shown, and so the text the offsets
// index. It has to match annotate.spanText's choice exactly, or every offset
// is against a different string.
func bodyOf(m *message.Message) string {
	if m.Body != "" {
		return m.Body
	}
	return m.NormalizedBody
}
