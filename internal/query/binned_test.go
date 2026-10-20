package query

import (
	"reflect"
	"strings"
	"testing"
)

func binOptions() Options {
	return Options{
		FolderSQL:      func(string) (string, bool) { return "1=1", true },
		KnownMailLabel: func(name string) bool { return name == "trash" || name == "spam" || name == "Receipts" },
		KnownAnnotator: func(string) bool { return false },
		SavedQuery: func(name string) (string, bool, error) {
			switch name {
			case "binned":
				return "folder:trash", true, nil
			case "plain":
				return "from:bob", true, nil
			}
			return "", false, nil
		},
		ThreadIDs: func(string) ([]string, error) { return []string{"a"}, nil },
	}
}

func leftOutOf(t *testing.T, src string) []string {
	t.Helper()
	q, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	plan, err := Build(q, binOptions(), "m.id")
	if err != nil {
		t.Fatalf("Build(%q): %v", src, err)
	}
	return plan.LeftOut
}

// What a query has to say to see Trash or Spam, and that saying one does not
// bring back the other.
func TestQueriesLeaveOutTheBinsTheyDoNotMention(t *testing.T) {
	both := []string{BinTrash, BinSpam}
	cases := []struct {
		src  string
		want []string
	}{
		{"", both},
		{"from:bob", both},
		{"from:bob | count by day", both},
		{"folder:inbox", both},
		{"label:Receipts", both},
		{"folder:trash", []string{BinSpam}},
		{"label:trash", []string{BinSpam}},
		{"is:deleted", []string{BinSpam}},
		{"folder:spam", []string{BinTrash}},
		{"is:junk", []string{BinTrash}},
		{"label:spam", []string{BinTrash}},
		{"folder:all", nil},
		{"folder:trash OR folder:spam", nil},
		// Negated is still mentioned: redundant now, not a way to say "and
		// Spam too".
		{"-folder:trash from:bob", []string{BinSpam}},
		{"from:bob (is:deleted OR subject:x)", []string{BinSpam}},
		{"label:tr*", []string{BinSpam}},
		// A saved query's own terms count.
		{"saved:binned", []string{BinSpam}},
		{"saved:plain", both},
		// id: names messages exactly.
		{"id:abc", nil},
		{"id:abc OR id:def", nil},
		// thread: is a conversation, not a message: its trashed replies stay out.
		{"thread:abc", both},
		// Files are mail too.
		{"in:attachments", both},
		// Things that are not mail are not narrowed.
		{"in:tickets", nil},
		{"in:drafts", nil},
		{"in:contacts", nil},
	}
	for _, c := range cases {
		got := leftOutOf(t, c.src)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q left out %v, want %v", c.src, got, c.want)
		}
	}
}

// `| thread` widens to whole conversations; the bins are applied again after,
// or a trashed reply comes back in with its conversation.
func TestThreadExpansionIsNarrowedAgain(t *testing.T) {
	q, err := Parse("from:bob | thread")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Build(q, binOptions(), "m.id")
	if err != nil {
		t.Fatal(err)
	}
	where := plan.SQL[strings.Index(plan.SQL, "m.thread_key IN"):]
	if !strings.Contains(where, ")) AND (NOT EXISTS") {
		t.Errorf("the widened query is not narrowed again:\n%s", plan.SQL)
	}
}

// Lists say how much they left out; aggregates do not, and a query that
// mentions both bins has nothing to count.
func TestHiddenIsCountedForLists(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"from:bob", true},
		{"from:bob | timeline", true},
		{"from:bob | count by day", false},
		{"folder:all", false},
		{"in:attachments", false},
	}
	for _, c := range cases {
		q, err := Parse(c.src)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := Build(q, binOptions(), "m.id")
		if err != nil {
			t.Fatal(err)
		}
		if got := plan.HiddenSQL != ""; got != c.want {
			t.Errorf("%q: hidden counted = %v, want %v", c.src, got, c.want)
		}
		if plan.HiddenSQL != "" && !strings.Contains(plan.HiddenSQL, "LIMIT 1000") {
			t.Errorf("%q: the hidden count is not capped: %s", c.src, plan.HiddenSQL)
		}
	}
}
