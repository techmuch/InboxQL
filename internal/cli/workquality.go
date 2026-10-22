package cli

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/work"
)

// The measuring half of `iql work`: checks, metrics and targets.

type auditFlags struct {
	limit       int
	model       string
	only        []string
	template    string
	digestOut   string
	out         string
	digest      bool
	base        string
	randomShare float64
	all         bool
	usage       store.WorkUsage
}

func workAudit(ctx *Context, sub string, args []string, f auditFlags) error {
	switch sub {
	case "next":
		b, err := work.AuditNext(work.AuditOptions{Limit: f.limit, Model: f.model, RandomShare: f.randomShare, Only: f.only})
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		if len(b.Items) == 0 && !ctx.JSON {
			ctx.Printf("Nothing to check: every answer given in the last 90 days has been checked or is being read.\n")
			return nil
		}
		return emitBatch(ctx, b, f.model, f.template, f.digestOut, f.out, f.digest, false)

	case "submit":
		return workSubmit(ctx, args, f.base, false, f.usage, true)

	case "list":
		list, err := store.Disagreements(!f.all, 200)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			type row struct {
				*store.WorkAudit
				OriginalSays string `json:"originalSays"`
				CheckerSays  string `json:"checkerSays"`
			}
			out := make([]row, 0, len(list))
			for _, a := range list {
				out = append(out, row{a, work.Describe(a.Original), work.Describe(a.Checker)})
			}
			return ctx.EmitJSON(map[string]any{"disagreements": out})
		}
		if len(list) == 0 {
			ctx.Printf("No disagreements are waiting for you.\n")
			return nil
		}
		t := ctx.Printer().NewTable("ID", "ELEMENT", "SUBJECT", "FIRST READ", "CHECK", "SETTLED")
		for _, a := range list {
			t.Row(a.ID, a.Element, truncate(a.Subject, 40), truncate(work.Describe(a.Original), 30),
				truncate(work.Describe(a.Checker), 30), a.Resolution)
		}
		return t.Flush()

	case "resolve":
		if len(args) != 2 {
			return Fail(ExitUsage, "usage: iql work audit resolve <id> original|checker|neither")
		}
		a, err := work.Resolve(args[0], args[1], calsync.New())
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(a)
		}
		ctx.Printf("Settled: %s on “%s” — %s was right.\n", a.Element, a.Subject, map[string]string{
			store.ResolvedOriginal: "the first read", store.ResolvedChecker: "the check",
			store.ResolvedNeither: "neither (flagged for a deep read)"}[a.Resolution])
		return nil
	}
	return Fail(ExitUsage, "unknown audit command %q (next, submit, list, resolve)", sub)
}

func auditSubmit(ctx *Context, subs []work.Submission) error {
	var results []*work.AuditResult
	failed, checked, agreed, disagreed := 0, 0, 0, 0
	for _, s := range subs {
		r, err := work.AuditSubmit(s)
		if err != nil {
			return Fail(ExitError, "%s: %v", s.Thread, err)
		}
		if !r.OK() {
			failed++
		}
		checked += r.Checked
		agreed += r.Agreed
		disagreed += len(r.Disagree)
		results = append(results, r)
	}
	if ctx.JSON {
		if err := ctx.EmitJSON(map[string]any{"checked": checked, "agreed": agreed, "disagreed": disagreed,
			"refused": failed, "results": results}); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			if !r.OK() {
				ctx.Printf("✗ %s  nothing recorded:\n", r.Thread)
				for _, p := range r.Problems {
					ctx.Printf("    %s\n", p.String())
				}
				continue
			}
			ctx.Printf("✓ %s  %d of %d agreed", r.Thread, r.Agreed, r.Checked)
			for _, d := range r.Disagree {
				ctx.Printf("; disagreed on %s", d.Element)
			}
			ctx.Printf("\n")
		}
		if disagreed > 0 {
			ctx.Printf("\n%d disagreements wait for a person: Intelligence → Work loop, or `iql work audit list`.\n", disagreed)
		}
	}
	if failed > 0 {
		return &Error{Code: ExitUsage, Err: fmt.Errorf("%d of %d checks refused; fix and resend them", failed, len(results))}
	}
	return nil
}

func printMetrics(ctx *Context, m *work.Metrics) {
	since := "all time"
	if !m.Since.IsZero() {
		since = "since " + m.Since.Format("2006-01-02")
	}
	if m.Model != "" {
		since += ", " + m.Model
	}
	r := m.Reading
	ctx.Printf("Work loop, %s\n\n", since)
	ctx.Printf("Reading\n")
	ctx.Printf("  %d conversations stored (%.1f a day), %d refused (%s), %d leases ran out\n",
		r.Stored, r.PerDay, r.Refused, pct(r.RefusalRate), r.Expired)
	if r.MedianSeconds > 0 {
		ctx.Printf("  %s from lease to submit, typically\n", (time.Duration(r.MedianSeconds) * time.Second).String())
	}
	ctx.Printf("  %s of answers went back as the template guessed them\n", pct(r.GuessedShare))
	if r.Usage != nil {
		ctx.Printf("  %.0f tokens and $%.4f a conversation (%d batches reported usage)\n", r.Usage.Tokens, r.Usage.Cost, r.Usage.Batches)
	}
	for _, p := range r.Problems {
		ctx.Printf("  refused %d× — %s\n", p.Count, p.Problem)
	}

	ctx.Printf("\nAccuracy\n")
	if m.Accuracy != nil {
		ctx.Printf("  %s right on %d settled random checks (probably %s–%s", pct(m.Accuracy.Accuracy), m.Accuracy.N,
			pct(m.Accuracy.Low), pct(m.Accuracy.High))
		if m.Accuracy.Open > 0 {
			ctx.Printf(", with %d disagreements still to settle", m.Accuracy.Open)
		}
		ctx.Printf(")\n")
	} else {
		ctx.Printf("  not measured yet: `iql work audit next` checks answers\n")
	}
	ctx.Printf("  %d disagreements wait for you; %d conversations flagged for a deep read; %d checks due\n",
		m.Checks.Open, m.Flagged, m.Checks.Due)
	kinds := make([]string, 0, len(m.Actions))
	for k := range m.Actions {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		o := m.Actions[k]
		ctx.Printf("  %s: %d kept, %d undone by you, %d waiting\n", k, o.Kept, o.Undone, o.Open)
	}

	ctx.Printf("\n")
	t := ctx.Printer().NewTable("ELEMENT", "ANSWERED", "GUESSED", "CHECKED", "ACCURACY", "TARGET", "STATUS", "GUESS")
	for _, e := range m.Elements {
		acc := "—"
		if e.Route.Estimate != nil {
			acc = fmt.Sprintf("%s (%s–%s)", pct(e.Route.Estimate.Accuracy), pct(e.Route.Estimate.Low), pct(e.Route.Estimate.High))
		}
		guess := "yes"
		if !e.Route.Guess {
			guess = "no"
		}
		t.Row(e.Element, strconv.Itoa(e.Answered), pctOf(e.Prefilled, e.Answered), strconv.Itoa(e.Route.Random.N),
			acc, pct(e.Route.Target), e.Route.Status, guess)
	}
	_ = t.Flush()
}

func printRoutes(ctx *Context, r *work.Routing) {
	names := make([]string, 0, len(r.Elements))
	for n := range r.Elements {
		names = append(names, n)
	}
	sort.Strings(names)
	t := ctx.Printer().NewTable("ELEMENT", "TARGET", "COST", "CHECKED", "STATUS", "GUESS", "CHECK RATE")
	for _, n := range names {
		e := r.Elements[n]
		guess := "yes"
		if !e.Guess {
			guess = "no — " + e.Why
		}
		t.Row(n, pct(e.Target), e.Cost, strconv.Itoa(e.Random.N), e.Status, guess, pct(e.CheckRate))
	}
	_ = t.Flush()
}

func pct(f float64) string { return strconv.FormatFloat(f*100, 'f', 0, 64) + "%" }

func pctOf(n, of int) string {
	if of == 0 {
		return "—"
	}
	return pct(float64(n) / float64(of))
}
