package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "export",
		Summary: "extract messages to .eml or .json",
		Usage: `iql export [flags]

Writes matching messages to a directory, one file each, or to a single JSON
document on stdout.

Flags:
  --account <id>       restrict to one account
  --query <text>       substring match, as in ` + "`iql search`" + `
  --from <address>     sender contains this
  --since <YYYY-MM-DD> on or after this date
  --until <YYYY-MM-DD> on or before this date
  --thread <msg-id>    export a whole conversation instead of a search
  --format <eml|json>  output format (default eml)
  --out <dir>          destination directory; omit for stdout with --format json
  --limit <n>          maximum messages (default 1000)

.eml files hold the raw RFC822 headers captured at sync time followed by the
stored body, which is what other mail clients can import.`,
		Run: runExport,
	})
}

func runExport(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	q := store.SearchQuery{}
	fs.StringVar(&q.AccountID, "account", "", "restrict to one account")
	fs.StringVar(&q.Text, "query", "", "substring match")
	fs.StringVar(&q.From, "from", "", "sender contains")
	fs.StringVar(&q.Since, "since", "", "on or after YYYY-MM-DD")
	fs.StringVar(&q.Until, "until", "", "on or before YYYY-MM-DD")
	fs.IntVar(&q.Limit, "limit", 1000, "maximum messages")
	thread := fs.String("thread", "", "export the thread containing this message id")
	format := fs.String("format", "eml", "eml or json")
	out := fs.String("out", "", "destination directory")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if *format != "eml" && *format != "json" {
		return Fail(ExitUsage, "--format must be eml or json")
	}
	if *format == "eml" && *out == "" {
		return Fail(ExitUsage, "--out <dir> is required for .eml export")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	var msgs []*message.Message
	var err error
	if *thread != "" {
		if msgs, err = store.GetThread(*thread); err != nil {
			return Fail(ExitError, "failed to load thread: %v", err)
		}
		if len(msgs) == 0 {
			return Fail(ExitNotFound, "no message with id %q", *thread)
		}
	} else {
		if msgs, err = store.SearchMessages(q); err != nil {
			return Fail(ExitError, "export query failed: %v", err)
		}
	}

	if len(msgs) == 0 {
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"exported": 0})
		}
		ctx.Printf("Nothing matched; no files written.\n")
		return nil
	}

	// JSON to stdout is the pipeline-friendly path, so it stays available
	// without a destination directory.
	if *format == "json" && *out == "" {
		results := make([]messageSummary, 0, len(msgs))
		for _, m := range msgs {
			results = append(results, summarise(m, true))
		}
		return ctx.EmitJSON(map[string]any{"count": len(results), "messages": results})
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		return Fail(ExitError, "cannot create %s: %v", *out, err)
	}

	written := 0
	for _, m := range msgs {
		var name string
		var data []byte

		if *format == "json" {
			name = m.ID + ".json"
			data, err = json.MarshalIndent(summarise(m, true), "", "  ")
			if err != nil {
				return Fail(ExitError, "cannot encode message %s: %v", m.ID, err)
			}
		} else {
			name = exportFilename(m)
			data = renderEML(m)
		}

		path := filepath.Join(*out, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return Fail(ExitError, "cannot write %s: %v", path, err)
		}
		written++
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"exported": written, "directory": *out, "format": *format})
	}
	ctx.Printf("Exported %d message(s) to %s\n", written, *out)
	return nil
}

// exportFilename builds a name that sorts chronologically and stays unique.
func exportFilename(m *message.Message) string {
	subject := slug(m.Subject)
	if subject == "" {
		subject = "no-subject"
	}
	subject = truncate(subject, 60)
	// The id suffix guarantees uniqueness when two messages share a subject
	// and a timestamp, which happens with bulk mail.
	short := m.ID
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("%s-%s-%s.eml", m.Date.Format("20060102-150405"), subject, short)
}

// renderEML reconstructs an RFC822 message from what was stored.
//
// The raw headers captured at sync time are reused verbatim when present, so
// the export keeps whatever the original server sent; only when they are
// missing are minimal headers synthesised.
func renderEML(m *message.Message) []byte {
	var b strings.Builder

	header := strings.TrimSpace(string(m.Header))
	if header != "" && header != "No Header" {
		b.WriteString(header)
		if !strings.HasSuffix(header, "\n") {
			b.WriteString("\r\n")
		}
	} else {
		fmt.Fprintf(&b, "From: %s\r\n", m.From)
		if len(m.To) > 0 {
			fmt.Fprintf(&b, "To: %s\r\n", strings.Join(m.To, ", "))
		}
		fmt.Fprintf(&b, "Subject: %s\r\n", m.Subject)
		fmt.Fprintf(&b, "Date: %s\r\n", m.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
		if m.MessageID != "" {
			fmt.Fprintf(&b, "Message-ID: %s\r\n", m.MessageID)
		}
		b.WriteString("MIME-Version: 1.0\r\n")
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	}

	b.WriteString("\r\n")
	body := m.Body
	if body == "" {
		body = m.HTMLBody
	}
	b.WriteString(body)
	return []byte(b.String())
}
