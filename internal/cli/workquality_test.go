package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/store"
)

// The measuring commands, end to end on an empty mailbox: metrics, targets,
// a flag, and checks with nothing yet to check.
func TestWorkMeasuringCommands(t *testing.T) {
	// The store is one per process: an earlier test's database left open
	// would be the one init "creates".
	store.CloseDB()
	t.Cleanup(store.CloseDB)
	dir := t.TempDir()
	run := func(args ...string) (int, string) {
		t.Helper()
		var out bytes.Buffer
		code := Execute(append([]string{"--data", dir}, args...), nil, &out, &out)
		return code, out.String()
	}
	if code, out := run("init"); code != ExitOK {
		t.Fatalf("init: %d %s", code, out)
	}
	if code, out := run("elements", "install"); code != ExitOK {
		t.Fatalf("elements install: %d %s", code, out)
	}

	code, out := run("--json", "work", "metrics", "--since", "7d")
	if code != ExitOK {
		t.Fatalf("metrics: %d %s", code, out)
	}
	var m struct {
		Elements []struct{ Element string } `json:"elements"`
	}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &m); err != nil || len(m.Elements) == 0 {
		t.Fatalf("metrics JSON: %v %s", err, out)
	}
	if code, out := run("work", "metrics"); code != ExitOK || !strings.Contains(out, "not measured yet") {
		t.Errorf("metrics text: %d %s", code, out)
	}

	if code, out := run("work", "targets", "set", "receipts", "--target", "0.99", "--cost", "high"); code != ExitOK || !strings.Contains(out, "99%") {
		t.Errorf("targets set: %d %s", code, out)
	}
	if code, _ := run("work", "targets", "set", "receipts", "--target", "3"); code != ExitUsage {
		t.Errorf("an impossible target: exit %d", code)
	}
	if code, out := run("work", "flag", "t1@x", "--reason", "looks off"); code != ExitOK || !strings.Contains(out, "--deep") {
		t.Errorf("flag: %d %s", code, out)
	}
	if code, out := run("work", "audit", "next"); code != ExitOK || !strings.Contains(out, "Nothing to check") {
		t.Errorf("audit next: %d %s", code, out)
	}
	if code, out := run("work", "audit", "list"); code != ExitOK || !strings.Contains(out, "No disagreements") {
		t.Errorf("audit list: %d %s", code, out)
	}
	if code, _ := run("work", "audit", "resolve", "wa_nope", "checker"); code != ExitUsage {
		t.Errorf("resolving an unknown check: exit %d", code)
	}
	if code, out := run("work", "next", "--deep"); code != ExitOK || !strings.Contains(out, "Nothing to read again") {
		t.Errorf("next --deep: %d %s", code, out)
	}
}

// A template element left null has nothing to merge into: the override is
// the whole answer.
func TestMergeOntoNullTemplateAnswer(t *testing.T) {
	base, _ := decodeSubmissions([]byte(`[{"thread":"t1","lease":"l","model":"m","answers":{"receipts":null}}]`))
	ov, _ := decodeSubmissions([]byte(`[{"thread":"t1","answers":{"receipts":{"m1":{"none":"no"}}}}]`))
	merged := mergeBaseSubmissions(base, ov)
	if got := string(merged[0].Answers["receipts"]); got != `{"m1":{"none":"no"}}` {
		t.Errorf("merged = %s", got)
	}
}
