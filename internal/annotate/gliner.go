package annotate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/gliner"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// # The span engine
//
// Structurally the same loop as runLLM — take the pending messages, evaluate
// each, write one annotation per record — and deliberately so. Everything that
// makes an annotator useful is in the rows it writes, not in what produced
// them, so the two engines write identical rows and every query, re-run,
// override and progress count works on both without knowing which ran.
//
// Three differences, all of them subtractions:
//
//   - No consent, because nothing leaves the machine. The profile, the model
//     override and AllowRemote are all about reaching a gateway, and there is
//     no gateway.
//   - No parsing. runLLM spends half its length recovering JSON from prose,
//     because a model asked for JSON returns markdown fences, apologies and
//     trailing commas. This engine returns offsets.
//   - No invented values. A record's value is a substring of the message by
//     construction, which is the reason to prefer this for extraction at all.
//
// And one addition: confidence means something per record. runLLM copies one
// self-reported number onto every record it parsed out of a single reply; here
// each record carries the score of its own span.

// glinerThreshold is the score below which a span is not a record.
//
// 0.5 is GLiNER's own default and the value the spike was judged at. It is not
// a knob on the annotator because a per-annotator threshold would be a second
// place to express "how sure is sure enough", when `extract:x@0.8` already
// asks that question at the point of reading, against stored scores, without
// re-running anything.
const glinerThreshold = 0.5

// runGLiNER applies a span extractor to every message that still needs it.
func runGLiNER(ctx context.Context, a *store.Annotator, opt Options, out *Outcome, dataDir string) error {
	labels := a.Labels()
	if len(labels) == 0 {
		return fmt.Errorf(
			"annotator %q has no fields in its schema, so there is nothing to look for.\n"+
				"A span extractor's labels are its schema's field names", a.Name)
	}
	if len(labels) > gliner.MaxLabels {
		return fmt.Errorf(
			"annotator %q asks for %d fields; this engine scores at most %d at once.\n"+
				"Split it into two annotators, which also lets them be re-run separately",
			a.Name, len(labels), gliner.MaxLabels)
	}

	m, err := gliner.Open(dataDir)
	if err != nil {
		return err
	}

	batch := opt.BatchSize
	if batch <= 0 {
		batch = 25
	}

	var total int64
	if p, err := store.Progress(a, opt.Scope); err == nil {
		total = p.Total - p.Evaluated
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		want := batch
		if opt.Limit > 0 {
			remaining := int64(opt.Limit) - out.Evaluated
			if remaining <= 0 {
				break
			}
			if int64(want) > remaining {
				want = int(remaining)
			}
		}

		msgs, err := store.PendingMessages(a, opt.Scope, want)
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			break
		}

		for _, msg := range msgs {
			if err := ctx.Err(); err != nil {
				return err
			}

			text, subjectLen := spanText(msg)
			spans, err := m.Extract(text, labels, glinerThreshold)
			anns := make([]*store.Annotation, 0, len(spans))

			switch {
			case err != nil:
				// Recorded rather than retried forever: without a row the
				// message stays pending and the next run tries it again, so
				// one unreadable message would block the queue.
				out.Failed++
				anns = append(anns, &store.Annotation{
					Status: store.StatusFailed, Source: store.SourceLLM,
					Model: m.Name(), Error: err.Error(),
				})

			case len(spans) == 0:
				// "Looked, found nothing" — a different answer from "not
				// evaluated", and the one that makes -extract:x honest.
				out.Empty++
				anns = append(anns, &store.Annotation{
					Status: store.StatusEmpty, Source: store.SourceLLM, Model: m.Name(),
				})

			default:
				out.Matched++
				for i, r := range records(spans, subjectLen) {
					payload, _ := json.Marshal(r.data)
					score := r.score
					anns = append(anns, &store.Annotation{
						Seq: i, Status: store.StatusOK, Source: store.SourceLLM,
						DataJSON: string(payload), Confidence: &score, Model: m.Name(),
					})
					out.Records++
				}
			}

			if err := store.SaveAnnotations(a.ID, a.Version, msg.ID, anns); err != nil {
				return err
			}
			out.Evaluated++
			if opt.Progress != nil {
				opt.Progress(out.Evaluated, total)
			}
		}
	}
	return nil
}

// record is one extracted row and the score of the span behind it.
type record struct {
	data  map[string]any
	score float64
}

// records turns spans into annotation rows.
//
// One row per span rather than one row per message, because that is what makes
// `extract:invoices.amount@0.8` mean "this amount scored 0.8" instead of "the
// reply that mentioned this amount did". A message with three amounts in it
// has three rows, each with its own score, exactly as a message with three
// participants has three rows in message_participants.
//
// # Offsets that resolve to something
//
// The model is shown the subject and the body joined together, so its offsets
// are into that joined string — which is not stored anywhere and which nothing
// downstream can reconstruct without knowing the join. Storing them raw would
// be storing a pointer into a buffer that no longer exists.
//
// So each row says which part it came from and carries the offset within that
// part. "field" names which of the two a reader should open, and start and end
// index into it directly. The value itself is also stored, so a consumer that
// only wants the amount never has to resolve an offset at all.
//
// The offsets are BYTE offsets into the UTF-8 text, as everywhere else in this
// codebase. Worth saying out loud: mail is full of characters like the narrow
// no-break space that mail clients put in "9:50 PM", and a reader that indexes
// by character instead lands a byte or two short in exactly the messages that
// matter, which looks like a decoding bug and is not one.
func records(spans []gliner.Span, subjectLen int) []record {
	out := make([]record, 0, len(spans))
	for _, s := range spans {
		field, start, end := "subject", s.Start, s.End
		if s.Start >= subjectLen {
			// Past the subject and the blank line that separates them.
			field = "body"
			start -= subjectLen
			end -= subjectLen
		}
		out = append(out, record{
			data: map[string]any{
				s.Label: s.Text,
				"field": field,
				"start": start,
				"end":   end,
			},
			score: s.Score,
		})
	}
	return out
}

// spanText is what the model is shown, and how much of it is the subject.
//
// The plain body, not the HTML: offsets have to point into something a reader
// can be shown, and an offset into a stripped-down copy of the markup points
// at nothing anyone can check. This is the opposite of the choice
// renderMessage makes for the LLM extractor, and for the same underlying
// reason — there, structure is the data and nobody ever looks at an offset.
//
// The subject leads, separated by a blank line so the model does not read it
// as the first sentence of the body. It is often where the amount and the
// reference number actually are: "Order & Pay Receipt for $80.44 at Blue Moon
// Pizza" carries the total, and the body under it carries the line items.
//
// The returned length is where the body begins, so a span can be reported
// against whichever field it fell in — see [records].
func spanText(m *message.Message) (text string, subjectLen int) {
	body := m.Body
	if strings.TrimSpace(body) == "" {
		body = m.NormalizedBody
	}
	if strings.TrimSpace(m.Subject) == "" {
		return body, 0
	}
	const sep = "\n\n"
	return m.Subject + sep + body, len(m.Subject) + len(sep)
}
