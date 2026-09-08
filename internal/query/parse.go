package query

import (
	"fmt"
	"strconv"
	"strings"
)

// Error is a parse failure with the offset it happened at, so a UI can put a
// caret under the offending character.
type Error struct {
	Pos int
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s (at position %d)", e.Msg, e.Pos) }

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokWord
	tokNot
	tokOr
	tokLParen
	tokRParen
	tokPipe
)

type token struct {
	kind tokenKind
	text string
	pos  int
	// quoted records that part of the token arrived inside quotes, which
	// suppresses operator interpretation: `subject:"-50% off"` is a phrase,
	// not a negation of a term called 50%.
	quoted bool
	// quotedFrom is where quoting began, as an offset into text. Zero means
	// the whole token was quoted and is therefore free text; a positive value
	// means a field name preceded the quote, as in `subject:"Re: hello"`,
	// and only the part before it may be read as syntax. -1 means unquoted.
	quotedFrom int
}

// lex splits the source into tokens.
//
// A word runs until whitespace or a structural character. Quotes may open
// anywhere in a word so `subject:"quarterly report"` is one token whose value
// keeps its space.
func lex(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]

		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
			continue
		case c == '(':
			toks = append(toks, token{kind: tokLParen, text: "(", pos: i})
			i++
			continue
		case c == ')':
			toks = append(toks, token{kind: tokRParen, text: ")", pos: i})
			i++
			continue
		case c == '|':
			toks = append(toks, token{kind: tokPipe, text: "|", pos: i})
			i++
			continue
		case c == '-':
			// A leading dash negates. Inside a word it is just a character,
			// which is what keeps `from:mail-noreply@x.com` working, and a
			// dash meant literally can still be quoted. A dash with nothing
			// after it is a typo, and the parser says so rather than
			// searching for "-".
			toks = append(toks, token{kind: tokNot, text: "-", pos: i})
			i++
			continue
		}

		start := i
		var sb strings.Builder
		quoted := false
		quotedFrom := -1
		for i < len(src) {
			c := src[i]
			if c == '"' {
				quoted = true
				if quotedFrom < 0 {
					quotedFrom = sb.Len()
				}
				i++
				for i < len(src) && src[i] != '"' {
					sb.WriteByte(src[i])
					i++
				}
				if i >= len(src) {
					return nil, &Error{Pos: start, Msg: "unterminated quoted value"}
				}
				i++ // closing quote
				continue
			}
			// A zero-argument function is part of the word, not a group.
			// `me()` has to survive lexing intact, while `from:(a OR b)` must
			// still open a group — the difference is whether the paren is
			// immediately closed.
			if c == '(' && i+1 < len(src) && src[i+1] == ')' && sb.Len() > 0 {
				sb.WriteString("()")
				i += 2
				continue
			}
			if isSpace(c) || c == '(' || c == ')' || c == '|' {
				break
			}
			sb.WriteByte(c)
			i++
		}

		text := sb.String()
		if text == "" && !quoted {
			return nil, &Error{Pos: start, Msg: "empty token"}
		}

		kind := tokWord
		if !quoted {
			switch strings.ToLower(text) {
			case "or":
				kind = tokOr
			case "not":
				kind = tokNot
			case "and":
				// Explicit AND is accepted and ignored; juxtaposition already
				// means AND, and rejecting the word people naturally type
				// would be gratuitous.
				continue
			}
		}
		toks = append(toks, token{kind: kind, text: text, pos: start, quoted: quoted, quotedFrom: quotedFrom})
	}

	toks = append(toks, token{kind: tokEOF, pos: len(src), quotedFrom: -1})
	return toks, nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token         { return p.toks[p.i] }
func (p *parser) next() token         { t := p.toks[p.i]; p.i++; return t }
func (p *parser) at(k tokenKind) bool { return p.toks[p.i].kind == k }

// Parse turns query text into a Query.
//
// An empty string is a valid query meaning "everything", because the UI's
// search bar starts empty and that should list the mailbox rather than error.
func Parse(src string) (*Query, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}

	q := &Query{Source: src}

	if p.at(tokEOF) {
		q.Filter = &All{}
		return q, nil
	}

	if p.at(tokPipe) {
		// A query that is only a pipeline filters nothing first.
		q.Filter = &All{}
	} else {
		node, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		q.Filter = node
	}

	for p.at(tokPipe) {
		p.next()
		stage, err := p.parseStage()
		if err != nil {
			return nil, err
		}
		q.Stages = append(q.Stages, stage)
	}

	if !p.at(tokEOF) {
		t := p.peek()
		return nil, &Error{Pos: t.pos, Msg: fmt.Sprintf("unexpected %q", t.text)}
	}
	return q, nil
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	if !p.at(tokOr) {
		return left, nil
	}
	nodes := []Node{left}
	for p.at(tokOr) {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, right)
	}
	return &Or{Nodes: nodes}, nil
}

func (p *parser) parseAnd() (Node, error) {
	var nodes []Node
	for {
		switch p.peek().kind {
		case tokEOF, tokRParen, tokPipe, tokOr:
			switch len(nodes) {
			case 0:
				return nil, &Error{Pos: p.peek().pos, Msg: "expected a term"}
			case 1:
				return nodes[0], nil
			default:
				return &And{Nodes: nodes}, nil
			}
		}
		n, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
}

func (p *parser) parseUnary() (Node, error) {
	if p.at(tokNot) {
		pos := p.next().pos
		if p.at(tokEOF) || p.at(tokRParen) || p.at(tokPipe) {
			return nil, &Error{Pos: pos, Msg: "negation with nothing to negate"}
		}
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Not{Node: inner}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Node, error) {
	if p.at(tokLParen) {
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.at(tokRParen) {
			return nil, &Error{Pos: p.peek().pos, Msg: "missing closing parenthesis"}
		}
		p.next()
		return inner, nil
	}
	if !p.at(tokWord) {
		t := p.peek()
		return nil, &Error{Pos: t.pos, Msg: fmt.Sprintf("expected a term, found %q", t.text)}
	}
	return p.parseTerm(p.next())
}

// parseTerm splits `field:value` and works out the match operator.
func (p *parser) parseTerm(t token) (Node, error) {
	text := t.text

	// A token quoted from the start is free text: quoting is how you search
	// for something that contains a colon.
	if t.quotedFrom == 0 {
		return &Term{Field: "", Op: OpMatch, Value: text, Pos: t.pos}, nil
	}

	// Only the unquoted head may be read as syntax, so `subject:"a:b"` keeps
	// its field and searches for the literal "a:b".
	head := text
	if t.quotedFrom > 0 && t.quotedFrom <= len(text) {
		head = text[:t.quotedFrom]
	}

	// `conf>0.9` and friends: an operator directly after the field name.
	if !t.quoted {
		if field, op, value, ok := splitComparison(text); ok {
			return &Term{Field: canonicalField(field), Op: op, Value: value, Pos: t.pos}, nil
		}
	}

	idx := strings.Index(head, ":")
	if idx < 0 {
		// Bare word: free text.
		return &Term{Field: "", Op: matchOpFor(text, t.quoted), Value: text, Pos: t.pos}, nil
	}

	field := canonicalField(text[:idx])
	value := text[idx+1:]

	if field == "" {
		return nil, &Error{Pos: t.pos, Msg: "missing field name before ':'"}
	}

	// `from:(alice OR bob)` — a group scoped to one field, which is Gmail's
	// spelling and reads far better than repeating the field in every branch.
	// The lexer already separates "from:" from the paren, so this is a rewrite
	// of the parsed group rather than new syntax.
	if value == "" && p.at(tokLParen) {
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.at(tokRParen) {
			return nil, &Error{Pos: p.peek().pos, Msg: "missing closing parenthesis"}
		}
		p.next()
		applyField(inner, field)
		return inner, nil
	}

	if value == "" {
		return nil, &Error{Pos: t.pos, Msg: fmt.Sprintf("%s: needs a value", field)}
	}

	// A confidence floor rides along with the label it qualifies, so that two
	// labels in one query can carry different thresholds.
	qualifier := ""
	if field == "label" {
		if at := strings.LastIndex(value, "@"); at > 0 {
			qualifier = value[at+1:]
			value = value[:at]
			if _, err := strconv.ParseFloat(qualifier, 64); err != nil {
				return nil, &Error{Pos: t.pos, Msg: fmt.Sprintf("label confidence %q is not a number", qualifier)}
			}
		}
	}

	op := OpMatch
	if t.quoted {
		// The value arrived quoted: it means itself, operators included.
		return &Term{Field: field, Op: OpMatch, Value: value, Qualifier: qualifier, Pos: t.pos}, nil
	}
	switch {
	case strings.HasPrefix(value, ">="):
		op, value = OpGreaterOrEqual, value[2:]
	case strings.HasPrefix(value, "<="):
		op, value = OpLessOrEqual, value[2:]
	case strings.HasPrefix(value, ">"):
		op, value = OpGreater, value[1:]
	case strings.HasPrefix(value, "<"):
		op, value = OpLess, value[1:]
	case strings.HasPrefix(value, "="):
		op, value = OpExact, value[1:]
	default:
		op = matchOpFor(value, false)
	}

	if value == "" {
		return nil, &Error{Pos: t.pos, Msg: fmt.Sprintf("%s: needs a value after %s", field, op)}
	}

	return &Term{Field: field, Op: op, Value: value, Qualifier: qualifier, Pos: t.pos}, nil
}

// applyField distributes a field name over the bare words in a group.
//
// Only terms that arrived without a field of their own are rewritten, so
// `from:(alice OR to:bob)` still means what it says rather than being
// flattened into nonsense.
func applyField(n Node, field string) {
	switch t := n.(type) {
	case *And:
		for _, c := range t.Nodes {
			applyField(c, field)
		}
	case *Or:
		for _, c := range t.Nodes {
			applyField(c, field)
		}
	case *Not:
		applyField(t.Node, field)
	case *Term:
		if t.Field == "" {
			t.Field = field
		}
	}
}

// matchOpFor promotes a value containing * to a glob, unless it was quoted —
// `from:"a*b"` searches for a literal asterisk.
func matchOpFor(v string, quoted bool) Op {
	if !quoted && strings.Contains(v, "*") {
		return OpGlob
	}
	return OpMatch
}

// splitComparison handles `conf>0.9`, where the operator follows the field
// name directly rather than a colon.
func splitComparison(text string) (field string, op Op, value string, ok bool) {
	for _, cand := range []struct {
		sym string
		op  Op
	}{{">=", OpGreaterOrEqual}, {"<=", OpLessOrEqual}, {">", OpGreater}, {"<", OpLess}} {
		if i := strings.Index(text, cand.sym); i > 0 {
			// Not a comparison if a colon came first — `subject:a>b` is a
			// value containing a bracket, not an operator.
			if c := strings.Index(text, ":"); c >= 0 && c < i {
				return "", 0, "", false
			}
			return text[:i], cand.op, text[i+len(cand.sym):], true
		}
	}
	return "", 0, "", false
}

// parseStage reads one pipeline verb.
func (p *parser) parseStage() (Stage, error) {
	if !p.at(tokWord) {
		return Stage{}, &Error{Pos: p.peek().pos, Msg: "expected a pipeline stage after '|'"}
	}
	verb := p.next()
	name := strings.ToLower(verb.text)

	word := func() (string, bool) {
		if p.at(tokWord) {
			return p.next().text, true
		}
		return "", false
	}
	// `by` is noise that makes the verbs read like English; accept and skip it.
	skipBy := func() {
		if p.at(tokWord) && strings.EqualFold(p.peek().text, "by") {
			p.next()
		}
	}

	switch name {
	case "count":
		s := Stage{Kind: StageCount}
		skipBy()
		if f, ok := word(); ok {
			s.Field = strings.ToLower(f)
		}
		return s, nil

	case "top":
		s := Stage{Kind: StageTop, N: 10}
		f, ok := word()
		if !ok {
			return Stage{}, &Error{Pos: verb.pos, Msg: "top: needs a field, e.g. `| top from 10`"}
		}
		s.Field = strings.ToLower(f)
		if p.at(tokWord) {
			if n, err := strconv.Atoi(p.peek().text); err == nil {
				p.next()
				s.N = n
			}
		}
		return s, nil

	case "sort":
		s := Stage{Kind: StageSort, Field: "date", Desc: true}
		if f, ok := word(); ok {
			s.Field = strings.ToLower(f)
			s.Desc = false
		}
		if p.at(tokWord) {
			switch strings.ToLower(p.peek().text) {
			case "desc":
				p.next()
				s.Desc = true
			case "asc":
				p.next()
				s.Desc = false
			}
		}
		return s, nil

	case "sample":
		f, ok := word()
		if !ok {
			return Stage{}, &Error{Pos: verb.pos, Msg: "sample: needs a number"}
		}
		n, err := strconv.Atoi(f)
		if err != nil || n <= 0 {
			return Stage{}, &Error{Pos: verb.pos, Msg: fmt.Sprintf("sample: %q is not a row count", f)}
		}
		return Stage{Kind: StageSample, N: n}, nil

	case "limit":
		f, ok := word()
		if !ok {
			return Stage{}, &Error{Pos: verb.pos, Msg: "limit: needs a number"}
		}
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return Stage{}, &Error{Pos: verb.pos, Msg: fmt.Sprintf("limit: %q is not a row count", f)}
		}
		return Stage{Kind: StageLimit, N: n}, nil

	case "series":
		s := Stage{Kind: StageSeries, Bucket: "day"}
		f, ok := word()
		if !ok {
			return Stage{}, &Error{Pos: verb.pos, Msg: "series: needs a field, e.g. `| series signups by week`"}
		}
		s.Field = f
		skipBy()
		if b, ok := word(); ok {
			s.Bucket = strings.ToLower(b)
		}
		if !validBucket(s.Bucket) {
			return Stage{}, &Error{Pos: verb.pos, Msg: fmt.Sprintf("series: %q is not a bucket (day, week, month, year)", s.Bucket)}
		}
		return s, nil

	case "sum", "avg", "min", "max":
		s := Stage{Kind: StageAggregate, Func: name, Bucket: ""}
		f, ok := word()
		if !ok {
			return Stage{}, &Error{Pos: verb.pos, Msg: name + ": needs a field"}
		}
		s.Field = f
		skipBy()
		if b, ok := word(); ok {
			s.Bucket = strings.ToLower(b)
			if !validBucket(s.Bucket) {
				return Stage{}, &Error{Pos: verb.pos, Msg: fmt.Sprintf("%s: %q is not a bucket (day, week, month, year)", name, s.Bucket)}
			}
		}
		return s, nil

	case "extract":
		f, ok := word()
		if !ok {
			return Stage{}, &Error{Pos: verb.pos, Msg: "extract: needs an annotator name"}
		}
		return Stage{Kind: StageExtract, Annotator: f}, nil

	case "thread", "threads":
		return Stage{Kind: StageThread}, nil

	case "timeline":
		return Stage{Kind: StageTimeline}, nil

	case "network", "graph":
		s := Stage{Kind: StageNetwork, N: 50}
		if f, ok := word(); ok {
			n, err := strconv.Atoi(f)
			if err != nil || n <= 0 {
				return Stage{}, &Error{Pos: verb.pos, Msg: fmt.Sprintf("network: %q is not a row count", f)}
			}
			s.N = n
		}
		return s, nil

	case "participants":
		return Stage{Kind: StageParticipants}, nil

	default:
		return Stage{}, &Error{Pos: verb.pos, Msg: fmt.Sprintf("unknown pipeline stage %q", verb.text)}
	}
}

func validBucket(b string) bool {
	switch b {
	case "day", "week", "month", "year", "hour":
		return true
	}
	return false
}
