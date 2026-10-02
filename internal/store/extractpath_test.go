package store

import (
	"strings"
	"testing"

	"github.com/user/inboxql/internal/query"
)

// Completion offers the whole `<annotator>.<field>` path, not the annotator
// alone.
//
// Naming an extracted value takes both halves, and offering them a step at a
// time is exactly what the old `| extract receipts | sum amount` made people
// do — with nothing anywhere to say which fields `receipts` even has.
func TestCompletionOffersWholeExtractedPaths(t *testing.T) {
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer CloseDB()

	if err := SaveAnnotator(&Annotator{
		Name: "receipts", Kind: KindExtract, Engine: EngineGLiNER,
		Instructions: "find things",
		SchemaJSON:   `{"amount":"x","merchant":"y","order number":"z"}`,
	}); err != nil {
		t.Fatal(err)
	}
	// A label has no fields, so completing to a path from it would produce
	// something that cannot be summed.
	if err := SaveAnnotator(&Annotator{
		Name: "money", Kind: KindLabel, Engine: EngineRule, Instructions: "is:unread",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := extractPathCandidates("")
	if err != nil {
		t.Fatal(err)
	}

	values := map[string]bool{}
	for _, c := range got {
		values[c.Value] = true
	}

	if !values["receipts.amount"] {
		t.Errorf("receipts.amount was not offered; got %v", values)
	}
	// A field with a space has to be quoted or the completion does not parse.
	if !values[`"receipts.order number"`] {
		t.Errorf("a spaced field was offered unquoted; got %v", values)
	}
	for v := range values {
		if strings.HasPrefix(v, "money") {
			t.Errorf("a label was offered as an extracted path: %q", v)
		}
	}
}

// A half-typed quoted path arrives with its opening quote, because a quote is
// not a token boundary. Matching the prefix against it would rule out every
// candidate — the dropdown would empty the moment somebody typed the quote
// they need for a spaced field.
func TestCompletionMatchesThroughAnOpeningQuote(t *testing.T) {
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer CloseDB()

	if err := SaveAnnotator(&Annotator{
		Name: "receipts", Kind: KindExtract, Engine: EngineGLiNER,
		Instructions: "find things", SchemaJSON: `{"order number":"z"}`,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := extractPathCandidates(`"receipts.ord`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Value != `"receipts.order number"` {
		t.Errorf("got %+v, want the quoted path", got)
	}
}

// The grammar half and the data half have to agree about where a path may be
// typed, or the dropdown is empty exactly where it is needed.
func TestCompleteOffersPathsWhereAFieldIsExpected(t *testing.T) {
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer CloseDB()

	if err := SaveAnnotator(&Annotator{
		Name: "receipts", Kind: KindExtract, Engine: EngineGLiNER,
		Instructions: "find things", SchemaJSON: `{"amount":"x","merchant":"y"}`,
	}); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		src string
		pos int
	}{
		{"| sum x", 6},         // the field position, nothing typed
		{"| sum rec", 9},       // half a path
		{"| series rec", 12},   // and for a series
		{"| count by rec", 14}, // and as a grouping key
		{"| top rec", 9},       // and for top
	} {
		got, err := Complete(c.src, c.pos)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if !offers(got, "receipts.amount") {
			vals := []string{}
			for _, x := range got.Candidates {
				vals = append(vals, x.Value)
			}
			t.Errorf("%q at %d offered %v, want receipts.amount among them", c.src, c.pos, vals)
		}
	}
}

// A grouping key is still a grouping key: the built-ins must not be displaced
// by the extracted paths now offered beside them.
func TestGroupingStillOffersTheBuiltInKeys(t *testing.T) {
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer CloseDB()

	got, err := Complete("| count by x", 11)
	if err != nil {
		t.Fatal(err)
	}
	if !offers(got, "from") {
		t.Error("`from` is no longer offered as a grouping key")
	}
}

func offers(c *query.Completion, value string) bool {
	for _, cand := range c.Candidates {
		if cand.Value == value {
			return true
		}
	}
	return false
}
