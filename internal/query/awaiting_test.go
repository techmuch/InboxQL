package query

import "testing"

// Bare `awaiting:me` is about mail — the conversations you owe — and
// `in:contacts awaiting:me` is about people.
func TestAwaitingIsAboutMailUnlessAskedForPeople(t *testing.T) {
	for src, want := range map[string]string{
		"awaiting:me":              EntityMessage,
		"awaiting:them | timeline": EntityMessage,
		"in:contacts awaiting:me":  EntityContact,
		"in:contacts has:awaiting": EntityContact,
	} {
		q, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		if got := q.Entity(); got != want {
			t.Errorf("%q is about %s, want %s", src, got, want)
		}
		if _, err := Build(q, Options{SelfAddresses: func() ([]string, error) { return []string{"me@x.com"}, nil }}, "m.id"); err != nil {
			t.Errorf("Build(%q): %v", src, err)
		}
	}
	if _, err := Parse("awaiting:everyone"); err == nil {
		q, _ := Parse("awaiting:everyone")
		if _, err := Build(q, Options{}, "m.id"); err == nil {
			t.Error("awaiting:everyone compiled")
		}
	}
}

func TestSQLListQuotes(t *testing.T) {
	if got := SQLList([]string{"Me@X.com", "o'brien@x.com"}); got != "('me@x.com', 'o''brien@x.com')" {
		t.Errorf("SQLList = %s", got)
	}
	if SQLList(nil) != "" {
		t.Error("an empty list is not empty")
	}
}
