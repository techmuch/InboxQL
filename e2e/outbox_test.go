//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// The central safety property, from AGENTS.md: "You can compose email. You
// cannot send it." Delivery requires a person at a terminal typing yes.
//
// It is enforced by a TTY check with no override — no --force, no --yes, no
// environment variable. Until now nothing asserted that, which made the most
// important promise in the product also one of the least defended: a refactor
// that dropped the check would have passed every test.
func TestOutboxApprovalRequiresATerminal(t *testing.T) {
	e := newEnv(t)
	draftID := seedDraft(t, e)

	// exec.Command gives the child a pipe, not a TTY, which is exactly the
	// situation an agent or a script is in.
	r := e.run("outbox", "approve", draftID)

	if r.ExitCode != 4 {
		t.Errorf("outbox approve exited %d from a pipe, want 4 (needs human approval)\n%s",
			r.ExitCode, r.Stderr)
	}
}

// There must be no way around the gate. If any of these ever succeeds, the
// promise in AGENTS.md is false.
func TestNoFlagBypassesApproval(t *testing.T) {
	e := newEnv(t)
	draftID := seedDraft(t, e)

	for _, bypass := range [][]string{
		{"outbox", "approve", draftID, "--force"},
		{"outbox", "approve", draftID, "--yes"},
		{"outbox", "approve", draftID, "-y"},
	} {
		r := e.run(bypass...)
		if r.ExitCode == 0 {
			t.Errorf("iql %s succeeded; the approval gate has an override", strings.Join(bypass, " "))
		}
	}

	// Nor through the environment.
	r := e.runWithStdin(strings.NewReader("yes\n"), "outbox", "approve", draftID)
	if r.ExitCode == 0 {
		t.Error("piping \"yes\" satisfied the approval gate; it must require a terminal")
	}
}

// Queuing is not sending. `iql send` moves a draft to the outbox and says so.
func TestSendOnlyQueues(t *testing.T) {
	e := newEnv(t)
	draftID := seedDraft(t, e)

	var listed struct {
		Count  int `json:"count"`
		Queued []struct {
			ID string `json:"id"`
		} `json:"queued"`
	}
	e.run("--json", "outbox", "list").JSON(t, &listed)

	if listed.Count != 1 {
		t.Fatalf("outbox holds %d messages after send, want 1", listed.Count)
	}
	if listed.Queued[0].ID != draftID {
		t.Errorf("outbox holds %q, want the draft %q", listed.Queued[0].ID, draftID)
	}
}

// A rejected draft returns to draft status; this is the path an agent may use.
func TestRejectReturnsDraftToComposing(t *testing.T) {
	e := newEnv(t)
	draftID := seedDraft(t, e)

	if r := e.run("outbox", "reject", draftID, "--reason", "not ready"); r.ExitCode != 0 {
		t.Fatalf("outbox reject exited %d\n%s", r.ExitCode, r.Stderr)
	}

	var listed struct {
		Count int `json:"count"`
	}
	e.run("--json", "outbox", "list").JSON(t, &listed)
	if listed.Count != 0 {
		t.Errorf("outbox still holds %d messages after reject, want 0", listed.Count)
	}
}

// An agent-composed draft must be recorded as such, so whoever approves it
// knows to read it more carefully.
func TestAgentOriginIsRecorded(t *testing.T) {
	e := newEnv(t)
	seedDraft(t, e)

	var listed struct {
		Drafts []struct {
			Origin string `json:"origin"`
		} `json:"drafts"`
	}
	e.run("--json", "draft", "list").JSON(t, &listed)

	if len(listed.Drafts) == 0 {
		t.Fatal("no drafts listed")
	}
	if listed.Drafts[0].Origin != "agent" {
		t.Errorf("origin recorded as %q, want \"agent\"", listed.Drafts[0].Origin)
	}
}

// seedDraft creates an account, composes a draft against it and queues it,
// returning the draft id.
func seedDraft(t *testing.T, e *env) string {
	t.Helper()

	r := e.run("account", "add",
		"--id", "test", "--name", "Test", "--email", "me@example.com",
		"--host", "imap.example.com", "--smtp-host", "smtp.example.com")
	if r.ExitCode != 0 {
		t.Fatalf("account add exited %d\n%s", r.ExitCode, r.Stderr)
	}

	r = e.run("--json", "draft", "create",
		"--to", "someone@example.com",
		"--subject", "Test message",
		"--body", "Body text.",
		"--origin", "agent")
	if r.ExitCode != 0 {
		t.Fatalf("draft create exited %d\n%s", r.ExitCode, r.Stderr)
	}

	var created struct {
		ID string `json:"id"`
	}
	r.JSON(t, &created)
	if created.ID == "" {
		t.Fatalf("draft create returned no id: %s", r.Stdout)
	}

	if r := e.run("send", created.ID); r.ExitCode != 0 {
		t.Fatalf("send exited %d\n%s", r.ExitCode, r.Stderr)
	}
	return created.ID
}
