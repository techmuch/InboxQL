package cli

import (
	"context"
	"flag"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/mailer"
	"github.com/user/inboxql/internal/store"
)

func init() {
	register(&Command{
		Name:    "draft",
		Summary: "compose a reply or a new message",
		Usage: `iql draft <create|list|show|delete> [flags]

create flags:
  --account <id>        sending account (required unless only one exists)
  --to <a,b>            recipients, comma separated
  --cc <a,b>            carbon copy
  --bcc <a,b>           blind carbon copy
  --subject <text>      subject line
  --body <text|->       body text, or - to read stdin
  --body-file <path>    body from a file
  --reply-to <msg-id>   reply to a stored message: fills in recipients,
                        subject and threading headers
  --bullets <text>      with an LLM configured, expand these notes into prose
                        (requirements.md 4.2, "Bullet-to-Draft")
  --origin <human|agent> who composed this; recorded for the approver

Creating a draft sends nothing. It is stored with status "draft" until
` + "`iql send`" + ` moves it to the outbox and a human approves it.`,
		Run: runDraft,
	})

	register(&Command{
		Name:    "send",
		Summary: "queue a draft for delivery (does not send)",
		Usage: `iql send <draft-id> [--json]

Moves the draft into the outbox with status "queued". Nothing is transmitted.

Delivery requires ` + "`iql outbox approve <id>`" + ` run by a person at a terminal.
That is a deliberate boundary: an agent can research, compose and queue a
reply, but cannot put mail on the wire unattended. There is no flag to bypass
it.`,
		Run: runSend,
	})

	register(&Command{
		Name:    "outbox",
		Summary: "review and approve queued messages",
		Usage: `iql outbox <list|show|approve|reject> [id]

  list      queued messages awaiting approval
  show      print a queued message in full, exactly as it will be sent
  approve   deliver it — requires a terminal and a typed confirmation
  reject    return it to draft status, with an optional --reason

approve refuses to run when stdin is not a terminal, which is what stops an
agent from approving its own drafts. Review with ` + "`show`" + ` before approving:
the sender is a real mailbox and delivery cannot be undone.`,
		Run: runOutbox,
	})
}

// --- draft ------------------------------------------------------------------

func runDraft(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	switch sub {
	case "create", "new":
		return draftCreate(ctx, rest)
	case "list", "":
		return draftList(ctx, rest)
	case "show":
		return draftShow(ctx, rest)
	case "delete", "rm":
		return draftDelete(ctx, rest)
	default:
		return Fail(ExitUsage, "unknown subcommand %q (want create, list, show or delete)", sub)
	}
}

func draftCreate(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("draft create", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	accountID := fs.String("account", "", "sending account id")
	to := fs.String("to", "", "recipients, comma separated")
	cc := fs.String("cc", "", "carbon copy")
	bcc := fs.String("bcc", "", "blind carbon copy")
	subject := fs.String("subject", "", "subject line")
	body := fs.String("body", "", "body text, or - for stdin")
	bodyFile := fs.String("body-file", "", "read the body from a file")
	replyTo := fs.String("reply-to", "", "message id being replied to")
	bullets := fs.String("bullets", "", "notes to expand into prose via the LLM")
	origin := fs.String("origin", store.OriginHuman, "human or agent")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	d := &store.Draft{
		ID:      uuid.New().String(),
		Subject: *subject,
		To:      splitList(*to),
		Cc:      splitList(*cc),
		Bcc:     splitList(*bcc),
		Status:  store.DraftStatusDraft,
		Origin:  *origin,
	}

	// Replying fills in whatever the user did not override, so the common case
	// is `--reply-to <id> --bullets "..."` and nothing else.
	if *replyTo != "" {
		original, err := store.GetMessageByID(*replyTo)
		if err != nil {
			return Fail(ExitError, "failed to load the message being replied to: %v", err)
		}
		if original == nil {
			return Fail(ExitNotFound, "no message with id %q", *replyTo)
		}
		d.InReplyTo = original.MessageID
		if d.AccountID == "" {
			d.AccountID = original.AccountID
		}
		if len(d.To) == 0 && original.From != "" {
			d.To = []string{original.From}
		}
		if d.Subject == "" {
			d.Subject = replySubject(original.Subject)
		}
	}

	if *accountID != "" {
		d.AccountID = *accountID
	}
	if d.AccountID == "" {
		// With a single account there is no ambiguity worth making the caller
		// resolve; with several, guessing would be a way to mail the wrong person.
		accounts, err := store.ListAccounts()
		if err != nil {
			return Fail(ExitError, "failed to list accounts: %v", err)
		}
		switch len(accounts) {
		case 0:
			return Fail(ExitNotConfigured, "no accounts configured; run `iql account add` first")
		case 1:
			d.AccountID = accounts[0].ID
		default:
			return Fail(ExitUsage, "several accounts exist; pass --account <id>")
		}
	}
	if _, err := requireAccount(d.AccountID); err != nil {
		return err
	}

	text, err := ctx.ReadBody(*body, *bodyFile)
	if err != nil {
		return err
	}
	d.Body = text

	// Bullet-to-Draft: only meaningful with a provider, and an explicit error
	// beats silently storing the raw bullets as if they were the message.
	if *bullets != "" {
		cfg, err := store.GetLLMConfig()
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		provider, err := llm.New(cfg)
		if err != nil {
			return Fail(ExitNotConfigured,
				"--bullets needs an LLM provider (`iql llm configure`).\n\n"+
					"Without one, compose the text yourself and pass it with --body.")
		}

		generated, err := generateFromBullets(provider, d, *replyTo, *bullets)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		d.Body = generated
		d.Origin = store.OriginLLM
	}

	if strings.TrimSpace(d.Body) == "" {
		return Fail(ExitUsage, "the draft has no body; pass --body, --body-file or --bullets")
	}
	if len(d.To) == 0 {
		return Fail(ExitUsage, "the draft has no recipients; pass --to")
	}
	if strings.TrimSpace(d.Subject) == "" {
		return Fail(ExitUsage, "the draft has no subject; pass --subject")
	}

	if err := store.SaveDraft(d); err != nil {
		return Fail(ExitError, "failed to save draft: %v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(d)
	}
	ctx.Printf("Created draft %s\n", d.ID)
	ctx.Printf("  to       %s\n", strings.Join(d.To, ", "))
	ctx.Printf("  subject  %s\n\n", d.Subject)
	ctx.Printf("Review it with `iql draft show %s`, then queue it with `iql send %s`.\n", d.ID, d.ID)
	return nil
}

// replySubject prefixes "Re: " unless the subject already carries one.
func replySubject(subject string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		return subject
	}
	return "Re: " + subject
}

func generateFromBullets(provider llm.Provider, d *store.Draft, replyTo, bullets string) (string, error) {
	var b strings.Builder

	if replyTo != "" {
		if thread, err := store.GetThread(replyTo); err == nil && len(thread) > 0 {
			b.WriteString("You are replying to this thread. Most recent messages last:\n\n")
			// The last few messages carry the context a reply needs; the whole
			// thread would mostly be quoted text.
			start := 0
			if len(thread) > 5 {
				start = len(thread) - 5
			}
			for _, m := range thread[start:] {
				b.WriteString("--- " + m.Date.Format(time.RFC1123) + " from " + m.From + "\n")
				b.WriteString(strings.TrimSpace(m.Body) + "\n\n")
			}
		}
	}

	b.WriteString("Write the reply body covering exactly these points:\n")
	for _, line := range strings.Split(bullets, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			b.WriteString("- " + line + "\n")
		}
	}
	b.WriteString("\nSubject line: " + d.Subject + "\n")

	timeout, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	out, err := provider.Complete(timeout, draftSystemPrompt, b.String())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

const draftSystemPrompt = "You draft email replies for the mailbox owner. Output only the " +
	"body text — no subject line, no headers, no commentary, no markdown fences. " +
	"Cover every point given and add nothing substantive that was not asked for. " +
	"Match the register of the thread; default to plain and professional."

func draftList(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("draft list", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	status := fs.String("status", "", "filter by draft, queued, sent or failed")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	drafts, err := store.ListDrafts(*status)
	if err != nil {
		return Fail(ExitError, "failed to list drafts: %v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"count": len(drafts), "drafts": drafts})
	}
	if len(drafts) == 0 {
		ctx.Printf("No drafts.\n")
		return nil
	}
	t := ctx.Printer().NewTable("ID", "STATUS", "ORIGIN", "TO", "SUBJECT")
	for _, d := range drafts {
		// An agent-written draft is the one a human should read hardest, so
		// the origin column says so in colour as well as in text.
		origin := d.Origin
		if origin == "agent" {
			origin = t.Cell(ui.Warn, origin)
		}
		t.Row(d.ID, t.Cell(draftState(d.Status), d.Status), origin,
			ui.Truncate(strings.Join(d.To, ","), 24), ui.Truncate(d.Subject, 40))
	}
	return t.Flush()
}

// draftState maps a draft's status onto the shared status vocabulary.
func draftState(status string) ui.Status {
	switch status {
	case store.DraftStatusQueued:
		return ui.Warn // waiting on a person
	case store.DraftStatusSent:
		return ui.OK
	case store.DraftStatusFailed:
		return ui.Bad
	default:
		return ui.Note
	}
}

func draftShow(ctx *Context, args []string) error {
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql draft show <draft-id>")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	d, err := requireDraft(args[0])
	if err != nil {
		return err
	}
	if ctx.JSON {
		return ctx.EmitJSON(d)
	}
	printDraft(ctx, d)
	return nil
}

func draftDelete(ctx *Context, args []string) error {
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql draft delete <draft-id>")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	if _, err := requireDraft(args[0]); err != nil {
		return err
	}
	if err := store.DeleteDraft(args[0]); err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]string{"deleted": args[0]})
	}
	ctx.Printf("Deleted draft %s\n", args[0])
	return nil
}

// --- send -------------------------------------------------------------------

func runSend(ctx *Context, args []string) error {
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql send <draft-id>")
	}
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	d, err := requireDraft(args[0])
	if err != nil {
		return err
	}

	switch d.Status {
	case store.DraftStatusSent:
		return Fail(ExitUsage, "draft %s was already sent on %s", d.ID, d.SentAt.Format(time.RFC1123))
	case store.DraftStatusQueued:
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"id": d.ID, "status": d.Status, "alreadyQueued": true})
		}
		ctx.Printf("Draft %s is already queued, awaiting approval.\n", d.ID)
		return nil
	}

	acc, err := requireAccount(d.AccountID)
	if err != nil {
		return err
	}
	// Validate before queueing so a malformed draft is rejected while it can
	// still be fixed, rather than at the approval prompt.
	msg, err := mailer.FromDraft(d, acc)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if err := msg.Validate(); err != nil {
		return Fail(ExitUsage, "%v", err)
	}
	if acc.SMTPHost == "" {
		return Fail(ExitNotConfigured,
			"account %s has no SMTP host; set one with `iql account add --smtp-host ...`", acc.ID)
	}

	now := time.Now()
	d.Status = store.DraftStatusQueued
	d.QueuedAt = &now
	d.LastError = ""
	if err := store.SaveDraft(d); err != nil {
		return Fail(ExitError, "failed to queue draft: %v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{
			"id":       d.ID,
			"status":   d.Status,
			"queuedAt": d.QueuedAt,
			"note":     "Nothing has been sent. A person must run `iql outbox approve " + d.ID + "` from a terminal.",
		})
	}
	ctx.Printf("Queued draft %s. Nothing has been sent yet.\n\n", d.ID)
	ctx.Printf("  iql outbox show %s      review it\n", d.ID)
	ctx.Printf("  iql outbox approve %s   deliver it\n", d.ID)
	return nil
}

// --- outbox -----------------------------------------------------------------

func runOutbox(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	switch sub {
	case "list", "":
		queued, err := store.ListDrafts(store.DraftStatusQueued)
		if err != nil {
			return Fail(ExitError, "failed to list the outbox: %v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"count": len(queued), "queued": queued})
		}
		if len(queued) == 0 {
			ctx.Printf("Outbox is empty.\n")
			return nil
		}
		t := ctx.Printer().NewTable("ID", "ORIGIN", "TO", "SUBJECT")
		for _, d := range queued {
			origin := d.Origin
			if origin == "agent" {
				origin = t.Cell(ui.Warn, origin)
			}
			t.Row(d.ID, origin, ui.Truncate(strings.Join(d.To, ","), 24), ui.Truncate(d.Subject, 40))
		}
		if err := t.Flush(); err != nil {
			return Fail(ExitError, "writing outbox: %v", err)
		}
		ctx.Printf("\n%s awaiting approval.\n", count(len(queued), "message", "messages"))
		return nil

	case "show":
		if len(rest) != 1 {
			return Fail(ExitUsage, "usage: iql outbox show <draft-id>")
		}
		d, err := requireDraft(rest[0])
		if err != nil {
			return err
		}
		acc, err := requireAccount(d.AccountID)
		if err != nil {
			return err
		}
		msg, err := mailer.FromDraft(d, acc)
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{
				"draft": d, "from": msg.From, "recipients": msg.Recipients(),
				"rendered": string(msg.Render()),
			})
		}
		// Print exactly what goes on the wire: an approver should review the
		// real thing, not a summary of it.
		ctx.Printf("%s", msg.Render())
		return nil

	case "approve":
		return outboxApprove(ctx, rest)

	case "reject":
		return outboxReject(ctx, rest)

	default:
		return Fail(ExitUsage, "unknown subcommand %q (want list, show, approve or reject)", sub)
	}
}

func outboxApprove(ctx *Context, args []string) error {
	if len(args) != 1 {
		return Fail(ExitUsage, "usage: iql outbox approve <draft-id>")
	}

	// The gate. Approval is the one action in InboxQL that puts mail on the wire,
	// and it is reachable only from an interactive terminal. There is
	// deliberately no --yes or --force: a flag would be available to the same
	// agent the gate exists to stop.
	if !IsTerminal() {
		return Fail(ExitNeedsApprove,
			"approval requires a terminal.\n\n"+
				"This message stays queued. A person must run:\n"+
				"    iql outbox approve %s\n\n"+
				"There is no flag to bypass this.", args[0])
	}

	d, err := requireDraft(args[0])
	if err != nil {
		return err
	}
	if d.Status != store.DraftStatusQueued {
		return Fail(ExitUsage, "draft %s is %q, not queued; run `iql send %s` first", d.ID, d.Status, d.ID)
	}

	acc, err := requireAccount(d.AccountID)
	if err != nil {
		return err
	}
	msg, err := mailer.FromDraft(d, acc)
	if err != nil {
		return Fail(ExitUsage, "%v", err)
	}

	ctx.Printf("%s\n", strings.Repeat("=", 60))
	ctx.Printf("%s", msg.Render())
	ctx.Printf("%s\n\n", strings.Repeat("=", 60))
	ctx.Printf("From account : %s\n", acc.ID)
	ctx.Printf("Via SMTP     : %s:%d\n", acc.SMTPHost, acc.SMTPPort)
	ctx.Printf("Recipients   : %s\n", strings.Join(msg.Recipients(), ", "))
	if d.Origin != store.OriginHuman {
		ctx.Printf("\nComposed by  : %s — read it carefully.\n", d.Origin)
	}
	ctx.Printf("\n")

	if !ctx.Confirm("Send this message? It cannot be recalled") {
		ctx.Printf("Not sent. The draft stays in the outbox.\n")
		return Fail(ExitError, "cancelled")
	}

	if err := mailer.Send(acc, msg); err != nil {
		now := time.Now()
		d.Status = store.DraftStatusFailed
		d.LastError = err.Error()
		d.UpdatedAt = now
		store.SaveDraft(d)
		return Fail(ExitError, "delivery failed: %v", err)
	}

	now := time.Now()
	d.Status = store.DraftStatusSent
	d.SentAt = &now
	d.LastError = ""
	if err := store.SaveDraft(d); err != nil {
		// The mail is already gone; losing the record is bad but not a reason
		// to imply it was not sent.
		ctx.Printf("WARNING: message was sent but the record could not be updated: %v\n", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"id": d.ID, "status": d.Status, "sentAt": d.SentAt})
	}
	ctx.Printf("Sent to %s\n", strings.Join(msg.Recipients(), ", "))
	return nil
}

func outboxReject(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("outbox reject", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	reason := fs.String("reason", "", "why it was rejected, kept on the draft")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if fs.NArg() != 1 {
		return Fail(ExitUsage, "usage: iql outbox reject <draft-id> [--reason <text>]")
	}

	d, err := requireDraft(fs.Arg(0))
	if err != nil {
		return err
	}
	if d.Status != store.DraftStatusQueued {
		return Fail(ExitUsage, "draft %s is %q, not queued", d.ID, d.Status)
	}

	// Back to draft rather than deleted: the work of composing it is worth
	// keeping, and the reason tells whoever revises it what was wrong.
	d.Status = store.DraftStatusDraft
	d.QueuedAt = nil
	d.LastError = *reason
	if err := store.SaveDraft(d); err != nil {
		return Fail(ExitError, "failed to update draft: %v", err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"id": d.ID, "status": d.Status, "reason": *reason})
	}
	ctx.Printf("Returned draft %s to draft status.\n", d.ID)
	return nil
}

// --- shared -----------------------------------------------------------------

func requireDraft(id string) (*store.Draft, error) {
	d, err := store.GetDraft(id)
	if err != nil {
		return nil, Fail(ExitError, "failed to load draft: %v", err)
	}
	if d == nil {
		return nil, Fail(ExitNotFound, "no draft with id %q", id)
	}
	return d, nil
}

func requireAccount(id string) (*account.Account, error) {
	acc, err := store.GetAccount(id)
	if err != nil {
		return nil, Fail(ExitError, "failed to load account: %v", err)
	}
	if acc == nil {
		return nil, Fail(ExitNotFound, "no account with id %q", id)
	}
	return acc, nil
}

func printDraft(ctx *Context, d *store.Draft) {
	ctx.Printf("Id       %s\n", d.ID)
	ctx.Printf("Status   %s\n", d.Status)
	ctx.Printf("Origin   %s\n", d.Origin)
	ctx.Printf("Account  %s\n", d.AccountID)
	ctx.Printf("To       %s\n", strings.Join(d.To, ", "))
	if len(d.Cc) > 0 {
		ctx.Printf("Cc       %s\n", strings.Join(d.Cc, ", "))
	}
	if len(d.Bcc) > 0 {
		ctx.Printf("Bcc      %s\n", strings.Join(d.Bcc, ", "))
	}
	ctx.Printf("Subject  %s\n", d.Subject)
	if d.LastError != "" {
		ctx.Printf("Note     %s\n", d.LastError)
	}
	ctx.Printf("\n%s\n", strings.TrimSpace(d.Body))
}
