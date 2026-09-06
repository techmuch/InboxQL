package query

import (
	"strings"
	"testing"
	"time"
)

// render turns an AST back into a canonical string, so tests can assert on
// structure without depending on SQL.
func render(n Node) string {
	switch t := n.(type) {
	case *All:
		return "*"
	case *Not:
		return "-" + render(t.Node)
	case *And:
		parts := make([]string, len(t.Nodes))
		for i, c := range t.Nodes {
			parts[i] = render(c)
		}
		return "(" + strings.Join(parts, " AND ") + ")"
	case *Or:
		parts := make([]string, len(t.Nodes))
		for i, c := range t.Nodes {
			parts[i] = render(c)
		}
		return "(" + strings.Join(parts, " OR ") + ")"
	case *Term:
		s := t.Field + ":" + t.Op.String() + ":" + t.Value
		if t.Qualifier != "" {
			s += "@" + t.Qualifier
		}
		return s
	}
	return "?"
}

func TestParseStructure(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "*"},
		{"invoice", ":match:invoice"},
		{"from:alice", "from:match:alice"},
		{"from:=alice@x.com", "from:=:alice@x.com"},
		{"from:*@acme.com", "from:glob:*@acme.com"},

		// Juxtaposition is AND; OR binds looser than AND.
		{"a b", "(:match:a AND :match:b)"},
		{"a OR b", "(:match:a OR :match:b)"},
		{"a b OR c", "((:match:a AND :match:b) OR :match:c)"},
		{"a AND b", "(:match:a AND :match:b)"},

		// Negation is a unary operator over any node, not a term property.
		{"-from:alice", "-from:match:alice"},
		{"NOT from:alice", "-from:match:alice"},
		{"-(a b)", "-(:match:a AND :match:b)"},
		{"--a", "--:match:a"},
		{"-(from:a OR from:b)", "-(from:match:a OR from:match:b)"},

		// A dash inside a word is a character, not an operator.
		{"from:mail-noreply@x.com", "from:match:mail-noreply@x.com"},

		// Quoting suppresses operator interpretation entirely.
		{`subject:"Re: hello"`, "subject:match:Re: hello"},
		{`"-50% off"`, ":match:-50% off"},

		// Aliases resolve to canonical fields.
		{"since:2026-01-01", "after:match:2026-01-01"},
		{"until:2026-01-01", "before:match:2026-01-01"},
		{"sender:bob", "from:match:bob"},

		// Comparisons, with and without a colon.
		{"conf>0.9", "conf:>:0.9"},
		{"larger:>5mb", "larger:>:5mb"},
		{"label:invoice@0.8", "label:match:invoice@0.8"},
	}

	for _, c := range cases {
		q, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got := render(q.Filter); got != c.want {
			t.Errorf("Parse(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestParseStages(t *testing.T) {
	cases := []struct {
		in    string
		check func(*testing.T, []Stage)
	}{
		{"| count", func(t *testing.T, s []Stage) {
			if len(s) != 1 || s[0].Kind != StageCount || s[0].Field != "" {
				t.Errorf("got %+v", s)
			}
		}},
		{"| count by week", func(t *testing.T, s []Stage) {
			if s[0].Kind != StageCount || s[0].Field != "week" {
				t.Errorf("got %+v", s)
			}
		}},
		{"| count week", func(t *testing.T, s []Stage) {
			// `by` is optional sugar, so both forms have to agree.
			if s[0].Field != "week" {
				t.Errorf("got %+v", s)
			}
		}},
		{"| top from 5", func(t *testing.T, s []Stage) {
			if s[0].Kind != StageTop || s[0].Field != "from" || s[0].N != 5 {
				t.Errorf("got %+v", s)
			}
		}},
		{"| top from", func(t *testing.T, s []Stage) {
			if s[0].N != 10 {
				t.Errorf("default top N = %d, want 10", s[0].N)
			}
		}},
		{"| sort size desc", func(t *testing.T, s []Stage) {
			if s[0].Kind != StageSort || s[0].Field != "size" || !s[0].Desc {
				t.Errorf("got %+v", s)
			}
		}},
		{"| extract metrics | sum signups by month", func(t *testing.T, s []Stage) {
			if len(s) != 2 || s[0].Annotator != "metrics" {
				t.Fatalf("got %+v", s)
			}
			if s[1].Kind != StageAggregate || s[1].Func != "sum" ||
				s[1].Field != "signups" || s[1].Bucket != "month" {
				t.Errorf("got %+v", s[1])
			}
		}},
		{"from:x | thread", func(t *testing.T, s []Stage) {
			if s[0].Kind != StageThread {
				t.Errorf("got %+v", s)
			}
		}},
	}

	for _, c := range cases {
		q, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		t.Run(c.in, func(t *testing.T) { c.check(t, q.Stages) })
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"-",
		"(",
		"(a",
		"a)",
		"from:",
		"|",
		"| frobnicate",
		"| top",
		"| limit",
		"| limit banana",
		"| series",
		"| series x by fortnight",
		`unterminated:"quote`,
		"a OR",
		"label:x@notanumber",
	} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", in)
		}
	}
}

// A parse error carries the offset so a UI can point at it.
func TestParseErrorsCarryAPosition(t *testing.T) {
	_, err := Parse("from:alice (unclosed")
	if err == nil {
		t.Fatal("want an error")
	}
	perr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *query.Error", err)
	}
	if perr.Pos <= 0 {
		t.Errorf("Pos = %d, want the offset of the problem", perr.Pos)
	}
	if !strings.Contains(err.Error(), "position") {
		t.Errorf("message %q does not mention the position", err.Error())
	}
}

func TestParseDateValue(t *testing.T) {
	Now = func() time.Time { return time.Date(2026, 3, 15, 10, 30, 0, 0, time.Local) }
	defer func() { Now = time.Now }()

	cases := []struct{ in, wantStart string }{
		{"2026-03-15", "2026-03-15 00:00"},
		{"2026-03", "2026-03-01 00:00"},
		{"2026", "2026-01-01 00:00"},
		{"today", "2026-03-15 00:00"},
		{"yesterday", "2026-03-14 00:00"},
		{"7d", "2026-03-08 00:00"},
		{"2w", "2026-03-01 00:00"},
		{"1m", "2026-02-15 00:00"},
		{"1y", "2025-03-15 00:00"},
	}
	for _, c := range cases {
		start, _, err := ParseDateValue(c.in)
		if err != nil {
			t.Errorf("ParseDateValue(%q): %v", c.in, err)
			continue
		}
		if got := time.UnixMilli(start).Format("2006-01-02 15:04"); got != c.wantStart {
			t.Errorf("ParseDateValue(%q) = %s, want %s", c.in, got, c.wantStart)
		}
	}

	// A month range covers the month, not a day of it.
	start, end, _ := ParseDateValue("2026-02")
	days := time.UnixMilli(end).Sub(time.UnixMilli(start)).Hours() / 24
	if days != 28 {
		t.Errorf("February 2026 spans %v days, want 28", days)
	}

	for _, bad := range []string{"", "notadate", "2026-13-45", "5x"} {
		if _, _, err := ParseDateValue(bad); err == nil {
			t.Errorf("ParseDateValue(%q) succeeded, want an error", bad)
		}
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"1024", 1024},
		{"5mb", 5_000_000},
		{"500kb", 500_000},
		{"1.5gb", 1_500_000_000},
		{"2M", 2_000_000},
	}
	for _, c := range cases {
		got, err := ParseSize(c.in)
		if err != nil {
			t.Errorf("ParseSize(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "banana", "-5mb"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) succeeded, want an error", bad)
		}
	}
}

// FTS5 reads bare punctuation as query syntax, so every value is quoted into a
// phrase. Without this an ordinary subject line is a syntax error, not a search.
func TestFTSPhraseQuoting(t *testing.T) {
	cases := []struct{ in, want string }{
		{"invoice", `"invoice"`},
		{"Re: hello", `"Re: hello"`},
		{`say "hi"`, `"say ""hi"""`},
		{"invoice*", `"invoice"*`},
		{"a OR b", `"a OR b"`},
	}
	for _, c := range cases {
		if got := ftsPhrase(c.in); got != c.want {
			t.Errorf("ftsPhrase(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// The compiler must never interpolate a value, whatever it contains.
func TestCompilerParameterisesEveryValue(t *testing.T) {
	opt := Options{FullText: true}
	for _, src := range []string{
		`subject:"'; DROP TABLE messages; --"`,
		`from:"' OR 1=1"`,
		"from:100%",
		"account:x'y",
	} {
		q, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		sql, args, err := CompileFilter(q.Filter, opt)
		if err != nil {
			t.Fatalf("CompileFilter(%q): %v", src, err)
		}
		if strings.Contains(sql, "DROP") || strings.Contains(sql, "1=1") || strings.Contains(sql, "x'y") {
			t.Errorf("CompileFilter(%q) put the value in the SQL: %s", src, sql)
		}
		if len(args) == 0 {
			t.Errorf("CompileFilter(%q) bound no arguments", src)
		}
	}
}

// Without an index the same terms still have to compile to something correct.
func TestCompilesWithoutFullTextIndex(t *testing.T) {
	q, err := Parse("invoice subject:receipt")
	if err != nil {
		t.Fatal(err)
	}
	sql, _, err := CompileFilter(q.Filter, Options{FullText: false})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "messages_fts") {
		t.Errorf("compiled against the index despite FullText:false: %s", sql)
	}
	if !strings.Contains(sql, "instr(") {
		t.Errorf("no substring fallback in: %s", sql)
	}
}

func TestOnlyOneAggregatePerQuery(t *testing.T) {
	q, err := Parse("| count by week | top from 5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(q, Options{}, "m.id"); err == nil {
		t.Error("Build accepted two terminal aggregates")
	}
}
