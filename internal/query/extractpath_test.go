package query

import (
	"strings"
	"testing"
)

// # One spelling for an extracted value
//
// The filter side has always written `extract:receipts.amount`. A pipeline had
// to say the same thing in two places — `| extract receipts | sum amount` —
// across stages whose order did not matter, which is also why the grouper got
// a bare word it could not resolve.
//
// These pin the path form, and that the old one still parses.

func TestAggregateReadsTheExtractorFromTheField(t *testing.T) {
	for _, src := range []string{
		"| sum receipts.amount",
		"| avg receipts.amount by month",
		"| series receipts.amount by week",
		"| min receipts.amount",
		"| max receipts.amount",
	} {
		q, err := Parse(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		s := q.Stages[len(q.Stages)-1]
		if s.Annotator != "receipts" {
			t.Errorf("%s: annotator = %q, want receipts", src, s.Annotator)
		}
		if s.Field != "amount" {
			t.Errorf("%s: field = %q, want amount", src, s.Field)
		}
	}
}

// A field with no dot keeps its old meaning — the annotator is left empty for
// `| extract` to supply. This is what keeps saved queries answering as they
// did, and it is the only reason the deprecated stage still exists.
func TestABareFieldLeavesTheExtractorUnnamed(t *testing.T) {
	q, err := Parse("| extract receipts | sum amount")
	if err != nil {
		t.Fatal(err)
	}
	var agg, ext *Stage
	for i := range q.Stages {
		switch q.Stages[i].Kind {
		case StageAggregate:
			agg = &q.Stages[i]
		case StageExtract:
			ext = &q.Stages[i]
		}
	}
	if ext == nil || ext.Annotator != "receipts" {
		t.Fatalf("the extract stage did not survive: %+v", ext)
	}
	if agg == nil {
		t.Fatal("no aggregate")
	}
	if agg.Annotator != "" {
		t.Errorf("a bare field invented an annotator %q", agg.Annotator)
	}
	if agg.Field != "amount" {
		t.Errorf("field = %q, want amount", agg.Field)
	}
}

// Only the first dot splits. An annotator name is a slug and has none; a field
// name may, and the halves must not be swapped by a second one.
func TestSplitPathTakesTheFirstDotOnly(t *testing.T) {
	cases := []struct{ in, name, field string }{
		{"receipts.amount", "receipts", "amount"},
		{"receipts.line.total", "receipts", "line.total"},
		{"amount", "", "amount"},
		{".amount", "", ".amount"},     // no name before the dot
		{"receipts.", "", "receipts."}, // no field after it
		{"", "", ""},
	}
	for _, c := range cases {
		name, field := splitPath(c.in)
		if name != c.name || field != c.field {
			t.Errorf("splitPath(%q) = (%q, %q), want (%q, %q)",
				c.in, name, field, c.name, c.field)
		}
	}
}

// Grouping by an extracted value is the gap the path form closes: with the
// extractor in the field there is finally something to resolve.
func TestCountByAnExtractedField(t *testing.T) {
	q, err := Parse("| count by receipts.merchant")
	if err != nil {
		t.Fatal(err)
	}
	s := q.Stages[len(q.Stages)-1]
	if s.Kind != StageCount {
		t.Fatalf("kind = %v", s.Kind)
	}
	// count keeps the whole path in Field; the grouper splits it, because that
	// is where the annotator can be checked against the ones that exist.
	if s.Field != "receipts.merchant" {
		t.Errorf("field = %q, want the whole path", s.Field)
	}

	g, err := groupKey("receipts.merchant", Options{
		KnownAnnotator: func(n string) bool { return n == "receipts" },
	})
	if err != nil {
		t.Fatalf("groupKey: %v", err)
	}
	if g.join == "" {
		t.Error("no join to the annotations")
	}
	if len(g.preArgs) != 1 || g.preArgs[0] != "receipts" {
		t.Errorf("preArgs = %v, want just the annotator name", g.preArgs)
	}
}

// An unknown annotator is named, not reported as an unknown grouping key — the
// user wrote a path, so the path is what the error should be about.
func TestGroupingByAnUnknownExtractorSaysSo(t *testing.T) {
	_, err := groupKey("nosuch.thing", Options{
		KnownAnnotator: func(string) bool { return false },
	})
	if err == nil {
		t.Fatal("an unknown extractor was accepted")
	}
	if !strings.Contains(err.Error(), `"nosuch"`) {
		t.Errorf("error does not name the extractor: %v", err)
	}
}

// A bare word that is not a grouping key still fails, and the message now says
// the path form is available — otherwise it is undiscoverable.
func TestAnUnknownGroupKeyMentionsThePathForm(t *testing.T) {
	_, err := groupKey("merchant", Options{})
	if err == nil {
		t.Fatal("merchant was accepted as a grouping key")
	}
	if !strings.Contains(err.Error(), "<annotator>.<field>") {
		t.Errorf("error does not mention the path form: %v", err)
	}
}
