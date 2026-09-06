// Package annotate runs annotators over a mailbox.
//
// # Two engines, one pipeline
//
// A rule annotator is a query expression, evaluated by the database in one
// pass. An LLM annotator is a prompt, evaluated one message at a time against
// a provider. They produce the same rows, are versioned the same way, re-run
// the same way, and are queried the same way.
//
// The rule engine exists first and on purpose. It has no external dependency,
// runs over a whole mailbox in milliseconds, and is deterministic — so the
// schema, the runner, the re-run logic and the query surface were all proven
// against it before a model was involved. When an LLM annotation looks wrong,
// that history is what makes it possible to tell a bad prompt from a broken
// pipeline.
//
// # Consent
//
// A run sends message bodies to whatever provider is configured. For a local
// Ollama that is nothing leaving the machine; for a hosted API it is every
// message in scope. [Plan] reports which, and [Run] refuses a remote provider
// unless the annotator carries explicit consent.
package annotate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// Result is one record an annotator produced for one message.
type Result struct {
	// Matched is false when the annotator looked and found nothing. That is
	// recorded, not discarded: "evaluated, no" is a different answer from
	// "not evaluated" and the query language distinguishes them.
	Matched    bool
	Data       map[string]any
	Confidence *float64
}

// Plan is what a run would do, reported before it does it.
type Plan struct {
	Annotator string `json:"annotator"`
	Version   int    `json:"version"`
	Engine    string `json:"engine"`
	Scope     string `json:"scope"`
	// Pending is how many messages still need evaluating at this version.
	Pending int64 `json:"pending"`
	Total   int64 `json:"total"`
	// Provider is the LLM that would be used, empty for rules.
	Provider string `json:"provider,omitempty"`
	// Remote is true when message content would leave this machine.
	Remote bool `json:"remote"`
	// Endpoint is where it would go, for a remote provider.
	Endpoint string `json:"endpoint,omitempty"`
	// EstimatedChars is a rough size of what would be sent.
	EstimatedChars int64 `json:"estimatedChars,omitempty"`
}

// Outcome is what a run did.
type Outcome struct {
	Annotator string `json:"annotator"`
	Version   int    `json:"version"`
	Evaluated int64  `json:"evaluated"`
	Matched   int64  `json:"matched"`
	Empty     int64  `json:"empty"`
	Failed    int64  `json:"failed"`
	Records   int64  `json:"records"`
	Duration  string `json:"duration"`
	DryRun    bool   `json:"dryRun"`
}

// localEndpoints are providers that do not leave the machine.
func isRemote(cfg store.LLMConfig) bool {
	if cfg.Provider == "" {
		return false
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = llm.DefaultEndpoints[cfg.Provider]
	}
	host := strings.ToLower(endpoint)
	for _, local := range []string{"://localhost", "://127.0.0.1", "://[::1]", "://0.0.0.0"} {
		if strings.Contains(host, local) {
			return false
		}
	}
	return true
}

// Describe reports what running an annotator would involve, without running it.
//
// Always available, and `iql annotate run` prints it before a first remote run
// whether or not the user asked. The mistake this exists to prevent is a bulk
// job quietly shipping a whole mailbox to a hosted API, which is a different
// act from asking one question about one thread.
func Describe(name, scope string) (*Plan, error) {
	a, err := store.GetAnnotator(name)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("no annotator named %q", name)
	}

	progress, err := store.Progress(a, scope)
	if err != nil {
		return nil, err
	}

	p := &Plan{
		Annotator: a.Name,
		Version:   a.Version,
		Engine:    a.Engine,
		Scope:     scope,
		Total:     progress.Total,
		Pending:   progress.Total - progress.Evaluated,
	}
	if p.Pending < 0 {
		p.Pending = 0
	}

	if a.Engine == store.EngineLLM {
		cfg, err := store.GetLLMConfig()
		if err != nil {
			return nil, err
		}
		p.Provider = cfg.Provider
		p.Remote = isRemote(cfg)
		if p.Remote {
			p.Endpoint = cfg.Endpoint
			if p.Endpoint == "" {
				p.Endpoint = llm.DefaultEndpoints[cfg.Provider]
			}
		}
		// Rough, and deliberately so: an exact token count needs the
		// tokeniser, and the number exists to convey scale before a decision.
		p.EstimatedChars = p.Pending * 2000
	}

	return p, nil
}

// Options controls a run.
type Options struct {
	// Scope is a query expression narrowing which messages to consider.
	Scope string
	// Limit caps how many messages this run evaluates. Zero means all of them.
	Limit int
	// DryRun evaluates nothing and writes nothing.
	DryRun bool
	// BatchSize is how many messages are fetched per round.
	BatchSize int
	// Progress, when set, is called after each message.
	Progress func(done, total int64)
}

// Run applies an annotator to every message that still needs it.
func Run(ctx context.Context, name string, opt Options) (*Outcome, error) {
	a, err := store.GetAnnotator(name)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("no annotator named %q", name)
	}

	started := time.Now()
	out := &Outcome{Annotator: a.Name, Version: a.Version, DryRun: opt.DryRun}

	if opt.DryRun {
		plan, err := Describe(name, opt.Scope)
		if err != nil {
			return nil, err
		}
		out.Evaluated = plan.Pending
		out.Duration = time.Since(started).String()
		return out, nil
	}

	switch a.Engine {
	case store.EngineRule:
		matched, empty, err := store.ApplyRule(a, opt.Scope)
		if err != nil {
			return nil, err
		}
		out.Matched, out.Empty = matched, empty
		out.Evaluated, out.Records = matched+empty, matched

	case store.EngineLLM:
		if err := runLLM(ctx, a, opt, out); err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("annotator %q has unknown engine %q", a.Name, a.Engine)
	}

	out.Duration = time.Since(started).String()
	return out, nil
}

func runLLM(ctx context.Context, a *store.Annotator, opt Options, out *Outcome) error {
	cfg, err := store.GetLLMConfig()
	if err != nil {
		return err
	}
	provider, err := llm.New(cfg)
	if err != nil {
		return err
	}

	// Consent is checked here rather than at configuration time because it is
	// this run, over this many messages, that does the sending.
	if isRemote(cfg) && !a.AllowRemote {
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = llm.DefaultEndpoints[cfg.Provider]
		}
		return fmt.Errorf(
			"annotator %q would send message bodies to %s, and has no consent recorded.\n"+
				"Re-create it with --allow-remote if that is what you want, or point the\n"+
				"provider at a local model with `iql llm configure --provider ollama`",
			a.Name, endpoint)
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

		for _, m := range msgs {
			if err := ctx.Err(); err != nil {
				return err
			}
			results, err := evaluate(ctx, provider, a, m)
			anns := make([]*store.Annotation, 0, len(results))

			switch {
			case err != nil:
				// A failure is recorded rather than retried forever: without a
				// row the message stays pending and the next run tries it
				// again, so one poisonous message would block the queue.
				out.Failed++
				anns = append(anns, &store.Annotation{
					Status: store.StatusFailed, Source: store.SourceLLM,
					Model: provider.Name(), Error: err.Error(),
				})

			case len(results) == 0:
				out.Empty++
				anns = append(anns, &store.Annotation{
					Status: store.StatusEmpty, Source: store.SourceLLM, Model: provider.Name(),
				})

			default:
				matchedAny := false
				for i, r := range results {
					status := store.StatusEmpty
					if r.Matched {
						status = store.StatusOK
						matchedAny = true
						out.Records++
					}
					payload, _ := json.Marshal(r.Data)
					anns = append(anns, &store.Annotation{
						Seq: i, Status: status, Source: store.SourceLLM,
						DataJSON: string(payload), Confidence: r.Confidence, Model: provider.Name(),
					})
				}
				if matchedAny {
					out.Matched++
				} else {
					out.Empty++
				}
			}

			if err := store.SaveAnnotations(a.ID, a.Version, m.ID, anns); err != nil {
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

// evaluate asks the provider about one message and parses the reply.
func evaluate(ctx context.Context, p llm.Provider, a *store.Annotator, m *message.Message) ([]Result, error) {
	system := buildSystemPrompt(a)
	user := renderMessage(a, m)

	raw, err := p.Complete(ctx, system, user)
	if err != nil {
		return nil, err
	}
	return parseResponse(a, raw)
}

// buildSystemPrompt turns an annotator into instructions plus an output
// contract.
//
// The contract is stated in the prompt and enforced on the way back, because a
// model that returns prose around its JSON is the common case rather than the
// exception.
func buildSystemPrompt(a *store.Annotator) string {
	var b strings.Builder
	b.WriteString("You are a precise email annotator. Follow the instruction exactly.\n\n")
	b.WriteString("INSTRUCTION:\n")
	b.WriteString(a.Instructions)
	b.WriteString("\n\n")

	if a.Kind == store.KindLabel {
		b.WriteString(`Reply with JSON only, in this exact shape:
{"matched": true|false, "confidence": 0.0-1.0}

"matched" is true only if the instruction clearly applies to this message.
When you are unsure, answer false with a low confidence rather than guessing.`)
	} else {
		b.WriteString("Extract every record the instruction describes.\n")
		if a.SchemaJSON != "" && a.SchemaJSON != "{}" {
			b.WriteString("Each record must match this schema:\n")
			b.WriteString(a.SchemaJSON)
			b.WriteString("\n")
		}
		b.WriteString(`Reply with JSON only, in this exact shape:
{"records": [ {...}, {...} ], "confidence": 0.0-1.0}

Return {"records": []} if the message contains none of this data.
Never invent a value that is not present in the message.`)
	}

	b.WriteString("\n\nReturn the JSON object and nothing else. No prose, no code fence.")
	return b.String()
}

// renderMessage builds the user half of the prompt.
//
// html_body is preferred for extractors because the structure is the data: a
// table of numbers behind a chart image survives as a table, and the
// plain-text fallback that InboxQL stores has had exactly that structure
// stripped out of it.
func renderMessage(a *store.Annotator, m *message.Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\n", m.From)
	if len(m.To) > 0 {
		fmt.Fprintf(&b, "To: %s\n", strings.Join(m.To, ", "))
	}
	fmt.Fprintf(&b, "Date: %s\n", m.Date.Format(time.RFC3339))
	fmt.Fprintf(&b, "Subject: %s\n\n", m.Subject)

	body := m.Body
	if a.Kind == store.KindExtract && strings.TrimSpace(m.HTMLBody) != "" {
		body = m.HTMLBody
	}
	if strings.TrimSpace(body) == "" {
		body = m.NormalizedBody
	}

	const maxBody = 24000
	if len(body) > maxBody {
		body = body[:maxBody] + "\n\n[truncated]"
	}
	b.WriteString(body)
	return b.String()
}

// parseResponse extracts the JSON object from a completion.
func parseResponse(a *store.Annotator, raw string) ([]Result, error) {
	text := strings.TrimSpace(raw)

	// Models fence JSON in markdown often enough that stripping it is cheaper
	// than failing the row and re-running the whole job.
	if i := strings.Index(text, "```"); i >= 0 {
		rest := text[i+3:]
		if j := strings.Index(rest, "\n"); j >= 0 {
			rest = rest[j+1:]
		}
		if k := strings.Index(rest, "```"); k >= 0 {
			rest = rest[:k]
		}
		text = strings.TrimSpace(rest)
	}
	if i := strings.Index(text, "{"); i > 0 {
		text = text[i:]
	}
	if j := strings.LastIndex(text, "}"); j >= 0 && j < len(text)-1 {
		text = text[:j+1]
	}

	if a.Kind == store.KindLabel {
		var reply struct {
			Matched    bool     `json:"matched"`
			Confidence *float64 `json:"confidence"`
		}
		if err := json.Unmarshal([]byte(text), &reply); err != nil {
			return nil, fmt.Errorf("could not read the model's reply as JSON: %w", err)
		}
		if !reply.Matched {
			return nil, nil
		}
		return []Result{{Matched: true, Confidence: reply.Confidence}}, nil
	}

	var reply struct {
		Records    []map[string]any `json:"records"`
		Confidence *float64         `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		return nil, fmt.Errorf("could not read the model's reply as JSON: %w", err)
	}

	out := make([]Result, 0, len(reply.Records))
	for _, rec := range reply.Records {
		if len(rec) == 0 {
			continue
		}
		out = append(out, Result{Matched: true, Data: rec, Confidence: reply.Confidence})
	}
	return out, nil
}
