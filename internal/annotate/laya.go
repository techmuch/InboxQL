package annotate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/laya"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// # The decision engine
//
// The same loop as the other two, writing the same rows, for the same reason:
// everything that makes an annotator useful is in the rows, not in what
// produced them.
//
// What it adds is the one thing the other engines could not do together. A rule
// label is free and exact and cannot answer anything a query cannot say. A span
// extractor cannot label at all. So "is this actually about a purchase, or a
// newsletter that mentions a price" had only one home — the LLM — and that is
// the engine that answered a receipt with three invented fields at confidence
// 1.0.
//
// This one emits no tokens. It scores the options it was given and returns a
// distribution over them, so an answer outside the option set is not something
// it can produce. Roughly two seconds a message against about forty.
//
// # Labels, plain or levelled
//
// A noul question with a threshold is exactly a label: yes, no, and a
// probability. A score question is a levelled label — importance from
// "important" to "ignorable" — stored as a match carrying the level, with the
// level's probability as its score. Choice questions are still unused: nothing
// yet needs an unordered pick that a set of yes/no labels would not say better.
//
// # The confidence is an ordering, not a frequency
//
// The checkpoint ships uncalibrated: every temperature 1.0, and the upstream
// project's own measurement puts its expected calibration error at 0.466. Their
// source is explicit that a threshold applied to the shipped weights "selects
// below model accuracy". So `label:x@0.9` against these scores ranks messages
// correctly and does not mean "ninety percent of these are right". Fitting a
// temperature on real corrections is a separate job; until it is done, this
// engine's scores are comparable to each other and to nothing else.

// layaThreshold is the probability above which a noul is a label.
//
// Not a knob on the annotator, for the same reason the span engine's is not:
// `label:x@0.8` already asks "how sure is sure enough" at the point of reading,
// against stored scores, without re-running anything. This only decides whether
// a row is written at all.
const layaThreshold = 0.5

// runLaya applies a decision annotator to every message that still needs it.
func runLaya(ctx context.Context, a *store.Annotator, opt Options, out *Outcome, dataDir string) error {
	if a.Kind != store.KindLabel {
		return fmt.Errorf(
			"annotator %q is an extractor, and this engine cannot extract.\n"+
				"It scores options and returns which one, so there is nothing to pull out of\n"+
				"the text. Use --engine gliner to extract values, or --kind label here",
			a.Name)
	}
	if strings.TrimSpace(a.Instructions) == "" {
		return fmt.Errorf(
			"annotator %q has no instructions, and they are the question being asked.\n"+
				"Write it as a statement about the message: "+
				"\"this message is about a purchase the reader made\"", a.Name)
	}

	m, err := laya.Open(dataDir)
	if err != nil {
		return err
	}

	q := layaQuestion(a)
	levelled := len(a.Levels()) > 0
	thread := a.Unit() == store.UnitThread

	// A fitted temperature, when somebody has made one. Applied here rather
	// than inside the model because it belongs to this annotator's question,
	// not to the weights: two annotators asking different things of the same
	// checkpoint are over-confident by different amounts.
	temp := laya.LoadTemperatures(dataDir)[a.Name]

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

			text := layaText(msg)
			// For a conversation, the facts the database knows go first, in a
			// line, so the model judges only what the newest message says.
			var digest *store.ThreadDigest
			if thread {
				if dg, err := store.DigestThread(msg.ID); err == nil {
					digest = dg
					text = digestLine(dg) + "\n\n" + text
				}
			}

			d, derr := m.Decide(text, q)
			if derr == nil && temp != nil {
				d.Calibrate(temp)
			}
			anns := make([]*store.Annotation, 0, 1)

			switch {
			case derr != nil:
				// Recorded rather than retried forever: without a row the
				// message stays pending and the next run tries it again, so
				// one bad message would block the queue.
				out.Failed++
				anns = append(anns, &store.Annotation{
					Status: store.StatusFailed, Source: store.SourceLLM,
					Model: m.Name(), Error: derr.Error(),
				})

			case levelled:
				// A level is always an answer — there is no "no" — so the row
				// is a match carrying which level, and the probability of that
				// level as its score.
				out.Matched++
				out.Records++
				score := d.Probability
				anns = append(anns, &store.Annotation{
					Status: store.StatusOK, Source: store.SourceLLM,
					DataJSON:   layaPayload(a, d, digest, d.Label),
					Confidence: &score, Model: m.Name(),
				})

			case d.Label != "true" || d.Probability < layaThreshold:
				// "Looked, and the answer is no" — a different thing from "not
				// evaluated", and what makes -label:x honest.
				//
				// The probability is kept on the negative row too. A message
				// the model called false at 0.51 is not the same evidence as
				// one it called false at 0.99, and throwing that away would
				// make the near misses — the rows most worth a human ruling —
				// indistinguishable from the certain ones.
				out.Empty++
				score := d.Probabilities[len(d.Probabilities)-1]
				anns = append(anns, &store.Annotation{
					Status: store.StatusEmpty, Source: store.SourceLLM,
					DataJSON:   layaPayload(a, d, digest, ""),
					Confidence: &score, Model: m.Name(),
				})

			default:
				out.Matched++
				out.Records++
				score := d.Probability
				anns = append(anns, &store.Annotation{
					Status: store.StatusOK, Source: store.SourceLLM,
					DataJSON:   layaPayload(a, d, digest, ""),
					Confidence: &score, Model: m.Name(),
				})
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

// layaQuestion is the question an annotator asks of the decision model.
//
// A yes/no label is a noul. A levelled one is a score question: the model
// returns a distribution over the levels, so one pass says which level and how
// sure — rather than one yes/no question per level, which would be four passes
// whose answers need not agree.
//
// Levels are stored best first, because that is how a person reads them, and
// the model numbers them from the bottom up ("level 0" is the least), so they
// are reversed on the way in. The answer comes back by label, not by position,
// so nothing else needs to know.
func layaQuestion(a *store.Annotator) laya.Question {
	levels := a.Levels()
	if len(levels) == 0 {
		return laya.Question{Kind: "noul", Instructions: a.Instructions}
	}
	opts := make([]laya.Option, 0, len(levels))
	for i := len(levels) - 1; i >= 0; i-- {
		opts = append(opts, laya.Option{Label: levels[i].Label, Describe: levels[i].Describe})
	}
	return laya.Question{Kind: "score", Instructions: a.Instructions, Options: opts}
}

// layaPayload is what is stored beside a decision.
//
// For a conversation it includes which way it points — whether the newest
// message is yours — because that is what turns "this expects a reply" into
// "waiting on you" or "waiting on them", and it is a fact about the moment of
// the decision: a later reply makes a new newest message and a new decision.
func layaPayload(a *store.Annotator, d *laya.Decision, digest *store.ThreadDigest, level string) string {
	v := map[string]any{"label": a.Name, "escalate": d.Escalate}
	if level != "" {
		v["level"] = level
	}
	if digest != nil {
		v["lastFromMe"] = digest.LastFromMe
		v["messages"] = digest.Messages
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// digestLine states a conversation's shape in one line, ahead of its newest
// message, in words rather than fields because the model reads text.
func digestLine(d *store.ThreadDigest) string {
	who := "from them"
	if d.LastFromMe {
		who = "from you"
	}
	replied := "you have not replied"
	if d.YouReplied {
		replied = "you have replied"
	}
	return fmt.Sprintf("Conversation: %d message(s), %d people, newest %s; %s.",
		d.Messages, d.Participants, who, replied)
}

// layaText is what the model is shown.
//
// Subject first, then the body, because the budget is short and a subject line
// is the densest sentence in a message — on a mailbox of receipts it alone
// often settles the question. The sender is included for the same reason: who
// sent it is evidence about what it is, and it costs a handful of tokens.
//
// Attachments are deliberately absent. The span engine reads them because the
// values it wants are inside the PDFs; a label is a question about what the
// message *is*, which its own text answers, and a hundred pages of invoice
// would push the subject out of the window to say the same thing.
func layaText(m *message.Message) string {
	var b strings.Builder
	if from := strings.TrimSpace(m.From); from != "" {
		b.WriteString("From: ")
		b.WriteString(from)
		b.WriteString("\n")
	}
	if subj := strings.TrimSpace(m.Subject); subj != "" {
		b.WriteString("Subject: ")
		b.WriteString(subj)
		b.WriteString("\n\n")
	}
	b.WriteString(stripQuotedHeaders(strings.TrimSpace(m.Body)))
	return b.String()
}

// stripQuotedHeaders removes the header block a forwarded message carries.
//
// # Why this is not cosmetic
//
// A forward's body opens with the quoted headers of the message inside it —
// "---------- Forwarded message ---------", then From, Date, Subject, To, Cc.
// On this mailbox that is 150 to 250 characters before any content, and the
// budget is about 200 tokens, so it is both noise and most of the window.
//
// It is worse than noise. Measured on one order confirmation, feeding the
// block dropped the model from 0.74 to 0.002 on "is this a purchase" — a
// confident, wrong "no" on a message whose own subject line says Order
// Confirmation. The addresses in the block are a different person's, which is
// evidence against the question actually being asked, and there is a lot of it
// relative to the content.
//
// Only the leading run is removed, and only lines that look like headers. A
// body that quotes a header later, in prose, is left alone — it is content
// there, and this cannot tell the difference well enough to be worth the risk.
func stripQuotedHeaders(body string) string {
	const marker = "Forwarded message"
	start := strings.Index(body, marker)
	if start < 0 || start > 200 {
		return body
	}
	// Back up to the start of the marker's own line.
	if nl := strings.LastIndexByte(body[:start], '\n'); nl >= 0 {
		start = nl + 1
	} else {
		start = 0
	}

	rest := body[start:]
	consumed := 0
	for _, line := range strings.SplitAfter(rest, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "---") || isQuotedHeader(t) {
			consumed += len(line)
			continue
		}
		break
	}
	return strings.TrimSpace(body[:start] + rest[consumed:])
}

// isQuotedHeader reports whether a line is one of the headers a forward quotes.
//
// A fixed list rather than "anything before a colon": a receipt's body is full
// of lines like "Order #: 109870" and "Payment Method: Visa", which are the
// content and must survive.
func isQuotedHeader(line string) bool {
	for _, h := range []string{"From:", "To:", "Cc:", "Bcc:", "Date:", "Sent:", "Subject:", "Reply-To:"} {
		if strings.HasPrefix(line, h) {
			return true
		}
	}
	return false
}
