package cli

import (
	"context"
	"flag"
	"strings"
	"time"

	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "search",
		Summary: "find messages",
		Usage: `iql search [--query <text>] [flags] [--json]

Substring matching over subject, body and sender — not ranked relevance.
There is no full-text index yet, so a bare --query on a large mailbox is a
table scan; narrow it with --account or a date range when you can.

Flags:
  --query <text>       text to look for
  --account <id>       restrict to one account
  --from <address>     sender contains this
  --since <YYYY-MM-DD> on or after this date
  --until <YYYY-MM-DD> on or before this date
  --unread             only messages without the \Seen flag
  --folder <name>      inbox, starred, sent, spam or trash
  --limit <n>          maximum results (default 25)
  --offset <n>         skip the first n results
  --full               include message bodies in JSON output

Returns a JSON array of message summaries under --json. Bodies are omitted by
default so a wide search stays small; use ` + "`iql read`" + ` for one message.`,
		Run: runSearch,
	})

	register(&Command{
		Name:    "read",
		Summary: "read a message or a whole thread",
		Usage: `iql read <message-id> [--thread] [--json]

Flags:
  --thread   return every message in the conversation, oldest first

Threading groups by normalised subject. Proper References/In-Reply-To
threading needs header parsing that does not exist yet, so an unrelated
message sharing a subject line can appear.`,
		Run: runRead,
	})

	register(&Command{
		Name:    "analyze",
		Summary: "summarise a thread, or emit context for an agent to analyse",
		Usage: `iql analyze <message-id> [--prompt <question>] [--json]

With an LLM provider configured, returns prose answering --prompt (or a
summary when none is given).

With no provider configured this is not an error: it emits a structured JSON
bundle — the thread, its participants and timing — for whatever agent is
driving the CLI to reason over itself. Check the "mode" field to tell which
you got: "llm" or "context".

Flags:
  --prompt <question>  what to ask about the thread
  --max-messages <n>   cap how much of the thread is included (default 20)`,
		Run: runAnalyze,
	})
}

// messageSummary is the compact form returned by search: enough to decide
// whether to read the message, without carrying bodies for every hit.
type messageSummary struct {
	ID      string    `json:"id"`
	Account string    `json:"accountId"`
	From    string    `json:"from"`
	To      []string  `json:"to,omitempty"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
	Unread  bool      `json:"unread"`
	Snippet string    `json:"snippet,omitempty"`
	Body    string    `json:"body,omitempty"`
}

func summarise(m *message.Message, full bool) messageSummary {
	s := messageSummary{
		ID: m.ID, Account: m.AccountID, From: m.From, To: m.To,
		Subject: m.Subject, Date: m.Date, Unread: !hasFlag(m.Flags, `\Seen`),
		Snippet: snippet(m.Body, 200),
	}
	if full {
		s.Body = m.Body
	}
	return s
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}

// snippet collapses whitespace and truncates, so a JSON listing stays readable.
func snippet(body string, n int) string {
	return truncate(strings.Join(strings.Fields(body), " "), n)
}

func runSearch(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	q := store.SearchQuery{}
	fs.StringVar(&q.Text, "query", "", "text to search for")
	fs.StringVar(&q.AccountID, "account", "", "restrict to one account")
	fs.StringVar(&q.From, "from", "", "sender contains")
	fs.StringVar(&q.Since, "since", "", "on or after YYYY-MM-DD")
	fs.StringVar(&q.Until, "until", "", "on or before YYYY-MM-DD")
	fs.BoolVar(&q.Unread, "unread", false, "only unread messages")
	fs.StringVar(&q.Folder, "folder", "", "inbox, starred, sent, spam or trash")
	fs.IntVar(&q.Limit, "limit", 25, "maximum results")
	fs.IntVar(&q.Offset, "offset", 0, "skip the first n results")
	full := fs.Bool("full", false, "include bodies in JSON output")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if !store.ValidFolder(q.Folder) {
		return Fail(ExitUsage, "unknown folder %q (want inbox, starred, sent, drafts, spam or trash)", q.Folder)
	}
	if store.IsDraftFolder(q.Folder) {
		return Fail(ExitUsage, "drafts are not messages; use `iql draft list`")
	}
	// A bare positional is the natural way to type this.
	if q.Text == "" && fs.NArg() > 0 {
		q.Text = strings.Join(fs.Args(), " ")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	msgs, err := store.SearchMessages(q)
	if err != nil {
		return Fail(ExitError, "search failed: %v", err)
	}

	if ctx.JSON {
		results := make([]messageSummary, 0, len(msgs))
		for _, m := range msgs {
			results = append(results, summarise(m, *full))
		}
		return ctx.EmitJSON(map[string]any{
			"query": q.Text, "count": len(results), "results": results,
		})
	}

	if len(msgs) == 0 {
		ctx.Printf("No messages matched.\n")
		return nil
	}
	for _, m := range msgs {
		mark := " "
		if !hasFlag(m.Flags, `\Seen`) {
			mark = "*"
		}
		ctx.Printf("%s %s  %-28s  %s\n", mark, m.Date.Format("2006-01-02 15:04"),
			truncate(m.From, 28), truncate(m.Subject, 60))
		ctx.Printf("    %s\n", m.ID)
	}
	ctx.Printf("\n%d message(s).\n", len(msgs))
	return nil
}

func runRead(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	thread := fs.Bool("thread", false, "return the whole conversation")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if fs.NArg() != 1 {
		return Fail(ExitUsage, "usage: iql read <message-id> [--thread]")
	}
	id := fs.Arg(0)

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	var msgs []*message.Message
	if *thread {
		var err error
		if msgs, err = store.GetThread(id); err != nil {
			return Fail(ExitError, "failed to load thread: %v", err)
		}
	} else {
		m, err := store.GetMessageByID(id)
		if err != nil {
			return Fail(ExitError, "failed to load message: %v", err)
		}
		if m != nil {
			msgs = []*message.Message{m}
		}
	}
	if len(msgs) == 0 {
		return Fail(ExitNotFound, "no message with id %q", id)
	}

	if ctx.JSON {
		results := make([]messageSummary, 0, len(msgs))
		for _, m := range msgs {
			results = append(results, summarise(m, true))
		}
		if *thread {
			return ctx.EmitJSON(map[string]any{"threadOf": id, "count": len(results), "messages": results})
		}
		return ctx.EmitJSON(results[0])
	}

	for i, m := range msgs {
		if i > 0 {
			ctx.Printf("\n%s\n\n", strings.Repeat("-", 60))
		}
		ctx.Printf("From:    %s\n", m.From)
		if len(m.To) > 0 {
			ctx.Printf("To:      %s\n", strings.Join(m.To, ", "))
		}
		ctx.Printf("Date:    %s\n", m.Date.Format(time.RFC1123))
		ctx.Printf("Subject: %s\n", m.Subject)
		ctx.Printf("Id:      %s\n\n", m.ID)
		body := m.Body
		if strings.TrimSpace(body) == "" {
			body = "(no plain-text body; this message may be HTML only)"
		}
		ctx.Printf("%s\n", strings.TrimSpace(body))
	}
	return nil
}

// analysisContext is what analyze emits when no LLM is configured. It is also
// exactly what gets fed to the model when one is, so the two modes cannot
// drift apart in what they consider relevant.
type analysisContext struct {
	Mode         string           `json:"mode"` // "context" or "llm"
	Prompt       string           `json:"prompt,omitempty"`
	ThreadOf     string           `json:"threadOf"`
	Subject      string           `json:"subject"`
	MessageCount int              `json:"messageCount"`
	Truncated    bool             `json:"truncated"`
	Participants []participant    `json:"participants"`
	Span         *span            `json:"span,omitempty"`
	Messages     []messageSummary `json:"messages"`
	Answer       string           `json:"answer,omitempty"`
	Note         string           `json:"note,omitempty"`
}

type participant struct {
	Address string `json:"address"`
	Sent    int    `json:"sent"`
}

type span struct {
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
	Days  int       `json:"days"`
}

func runAnalyze(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	prompt := fs.String("prompt", "", "what to ask about the thread")
	maxMessages := fs.Int("max-messages", 20, "cap on messages included")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if fs.NArg() != 1 {
		return Fail(ExitUsage, "usage: iql analyze <message-id> [--prompt <question>]")
	}
	id := fs.Arg(0)

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	msgs, err := store.GetThread(id)
	if err != nil {
		return Fail(ExitError, "failed to load thread: %v", err)
	}
	if len(msgs) == 0 {
		return Fail(ExitNotFound, "no message with id %q", id)
	}

	bundle := buildContext(id, *prompt, msgs, *maxMessages)

	cfg, err := store.GetLLMConfig()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	provider, err := llm.New(cfg)
	if err != nil {
		// Not configured is a supported state, not a failure: the structured
		// bundle is the deliverable for an agent driving the CLI.
		bundle.Mode = "context"
		bundle.Note = "No LLM provider is configured, so this is the thread as structured " +
			"context rather than a generated answer. Configure one with `iql llm configure`, " +
			"or analyse this payload yourself."
		if ctx.JSON {
			return ctx.EmitJSON(bundle)
		}
		return printContext(ctx, bundle)
	}

	timeout, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	question := *prompt
	if question == "" {
		question = "Summarise this email thread: what is being asked, what was decided, " +
			"and what is still outstanding."
	}

	answer, err := provider.Complete(timeout, analyzeSystemPrompt, renderThreadForLLM(bundle, question))
	if err != nil {
		return Fail(ExitError, "%v", err)
	}

	bundle.Mode = "llm"
	bundle.Answer = strings.TrimSpace(answer)

	if ctx.JSON {
		return ctx.EmitJSON(bundle)
	}
	ctx.Printf("%s\n", bundle.Answer)
	return nil
}

const analyzeSystemPrompt = "You are analysing an email thread on behalf of the mailbox owner. " +
	"Be concise and concrete. Quote sparingly. If the thread does not answer the question, " +
	"say so rather than speculating."

func buildContext(id, prompt string, msgs []*message.Message, max int) *analysisContext {
	bundle := &analysisContext{
		ThreadOf:     id,
		Prompt:       prompt,
		Subject:      msgs[0].Subject,
		MessageCount: len(msgs),
	}

	counts := map[string]int{}
	for _, m := range msgs {
		counts[m.From]++
	}
	for addr, n := range counts {
		bundle.Participants = append(bundle.Participants, participant{Address: addr, Sent: n})
	}

	first, last := msgs[0].Date, msgs[len(msgs)-1].Date
	bundle.Span = &span{First: first, Last: last, Days: int(last.Sub(first).Hours() / 24)}

	// Keep the most recent messages when trimming: the tail of a thread is
	// what a question is almost always about.
	included := msgs
	if max > 0 && len(msgs) > max {
		included = msgs[len(msgs)-max:]
		bundle.Truncated = true
	}
	for _, m := range included {
		bundle.Messages = append(bundle.Messages, summarise(m, true))
	}
	return bundle
}

func renderThreadForLLM(bundle *analysisContext, question string) string {
	var b strings.Builder
	b.WriteString("Thread subject: " + bundle.Subject + "\n")
	if bundle.Truncated {
		b.WriteString("(only the most recent messages are shown)\n")
	}
	b.WriteString("\n")
	for _, m := range bundle.Messages {
		b.WriteString("--- " + m.Date.Format(time.RFC1123) + " from " + m.From + "\n")
		b.WriteString(strings.TrimSpace(m.Body) + "\n\n")
	}
	b.WriteString("Question: " + question + "\n")
	return b.String()
}

func printContext(ctx *Context, bundle *analysisContext) error {
	ctx.Printf("Thread: %s\n", bundle.Subject)
	ctx.Printf("%d message(s)", bundle.MessageCount)
	if bundle.Span != nil {
		ctx.Printf(" over %d day(s)", bundle.Span.Days)
	}
	ctx.Printf("\n\nParticipants:\n")
	for _, p := range bundle.Participants {
		ctx.Printf("  %-40s %d message(s)\n", p.Address, p.Sent)
	}
	ctx.Printf("\n%s\n", bundle.Note)
	ctx.Printf("\nRe-run with --json to get the full thread as a structured payload.\n")
	return nil
}
