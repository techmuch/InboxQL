package ask

import (
	"context"
	"strings"
	"testing"
)

const stream = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"iql --json query \"topic:kitchen extract:receipts | sum receipts.total\""}}]},"session_id":"s1"}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"{...}"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"iql mail archive m1"}}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"You have spent $5,412.20."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"You have spent $5,412.20 of the quote.","session_id":"s1","total_cost_usd":0.031,"permission_denials":[{"tool_name":"Bash","tool_input":{"command":"iql mail archive m1"}}]}
`

func TestAskReadsTheAnswerAndTheCommands(t *testing.T) {
	var gotArgs, gotEnv []string
	Runner = func(_ context.Context, bin string, args, env []string, _ string) ([]byte, error) {
		gotArgs, gotEnv = args, env
		return []byte(stream), nil
	}
	a := Ask(context.Background(), "How much on the kitchen?", Options{Bin: "/x/claude", DataDir: "/d", IQLDir: "/bin/iqldir"})
	if a.Error != "" || a.Text != "You have spent $5,412.20 of the quote." || a.Session != "s1" || a.CostUSD != 0.031 {
		t.Fatalf("answer: %+v", a)
	}
	if len(a.Commands) != 2 || a.Commands[0].Refused || !a.Commands[1].Refused {
		t.Fatalf("commands: %+v", a.Commands)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"-p", "stream-json", "Bash(iql --json query:*)", "--disallowedTools"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args lack %q", want)
		}
	}
	if strings.Contains(joined, "dangerously") {
		t.Errorf("permissions skipped: %s", joined)
	}
	// Every allowed command is a read: nothing that sends, moves, labels,
	// creates or deletes.
	for _, tool := range AllowedTools {
		words := strings.Fields(strings.NewReplacer("Bash(", " ", ":*)", " ").Replace(tool))
		for _, w := range words {
			for _, verb := range []string{"archive", "send", "label", "labels", "trash", "delete", "create", "approve", "move", "edit", "new", "save", "mail", "draft"} {
				if w == verb {
					t.Errorf("allowed tool %q can %s", tool, verb)
				}
			}
		}
	}
	env := strings.Join(gotEnv, "\n")
	if !strings.Contains(env, "INBOXQL_DATA=/d") || !strings.Contains(env, "PATH=/bin/iqldir") {
		t.Errorf("env: data dir or iql not passed")
	}
}

func TestAskContinuesASession(t *testing.T) {
	var gotArgs []string
	Runner = func(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
		gotArgs = args
		return []byte(`{"type":"result","result":"Yes.","session_id":"s1"}`), nil
	}
	Ask(context.Background(), "and the island?", Options{Bin: "c", Session: "s1"})
	if !strings.Contains(strings.Join(gotArgs, " "), "--resume s1") {
		t.Fatalf("no resume: %v", gotArgs)
	}
}

func TestAskSaysWhyItCannot(t *testing.T) {
	if a := Ask(context.Background(), "x", Options{}); !strings.Contains(a.Error, "not installed") {
		t.Errorf("no binary: %+v", a)
	}
	if a := Ask(context.Background(), "  ", Options{Bin: "c"}); a.Error == "" {
		t.Error("empty question accepted")
	}
	Runner = func(context.Context, string, []string, []string, string) ([]byte, error) {
		return []byte(`{"type":"system"}`), nil
	}
	if a := Ask(context.Background(), "x", Options{Bin: "c"}); a.Error == "" {
		t.Error("no result line should be an error")
	}
}
