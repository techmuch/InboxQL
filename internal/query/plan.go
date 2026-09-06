package query

import (
	"fmt"
	"strings"
)

// PlanKind is the shape of a plan's result set.
type PlanKind int

const (
	// PlanMessages returns message rows.
	PlanMessages PlanKind = iota
	// PlanGroups returns (label, value) pairs: every aggregate stage.
	PlanGroups
	// PlanScalar returns a single count.
	PlanScalar
)

// Plan is a compiled query ready to execute.
type Plan struct {
	SQL  string
	Args []any
	Kind PlanKind
	// Limit is the row cap actually applied, after clamping.
	Limit int
	// Ordered records whether the caller asked for a specific order, so a
	// group result can be left in value order rather than re-sorted.
	Ordered bool
}

// threadKeyExpr names the column that identifies a conversation.
//
// Maintained on write (see store.writeRefs) rather than derived per query: the
// alternative is a recursive walk of message_refs on every thread query, and
// the value only changes when a message is written.
const threadKeyExpr = "m.thread_key"

// Build compiles a parsed query into an executable plan.
//
// selectList is the caller's message column list, already qualified with the
// `m` alias. It is passed in rather than hardcoded so the store keeps one
// canonical column list and this package stays out of the schema's business.
func Build(q *Query, opt Options, selectList string) (*Plan, error) {
	where, args, err := CompileFilter(q.Filter, opt)
	if err != nil {
		return nil, err
	}

	p := &pipeline{opt: opt, where: where, args: args, selectList: selectList}
	if err := p.read(q.Stages); err != nil {
		return nil, err
	}
	return p.build()
}

type pipeline struct {
	opt        Options
	where      string
	args       []any
	selectList string

	extractor string
	terminal  *Stage
	sort      *Stage
	limit     int
	expand    bool
}

// read validates the stage list and reduces it to the few things a statement
// can express.
func (p *pipeline) read(stages []Stage) error {
	for i := range stages {
		s := stages[i]
		switch s.Kind {
		case StageExtract:
			p.extractor = s.Annotator

		case StageThread:
			p.expand = true

		case StageSort:
			cp := s
			p.sort = &cp

		case StageLimit:
			p.limit = s.N

		case StageCount, StageTop, StageSeries, StageAggregate, StageParticipants:
			if p.terminal != nil {
				return fmt.Errorf("a query can end in only one aggregate; found %s after %s", s.Kind, p.terminal.Kind)
			}
			cp := s
			p.terminal = &cp

		default:
			return fmt.Errorf("unknown stage %q", s.Kind)
		}
	}
	return nil
}

func (p *pipeline) clampLimit(dflt int) int {
	n := p.limit
	if n <= 0 {
		n = dflt
	}
	if max := p.opt.maxLimit(); n > max {
		n = max
	}
	return n
}

// expandToThreads rewrites the filter so it selects whole conversations.
//
// `| thread` means "and everything else in those conversations", which is a
// second pass over the same table keyed on thread_key rather than a join.
func (p *pipeline) effectiveWhere() string {
	if !p.expand {
		return p.where
	}
	return "m.thread_key IN (SELECT t.thread_key FROM messages t WHERE t.id IN " +
		"(SELECT m.id FROM messages m WHERE " + p.where + "))"
}

func (p *pipeline) build() (*Plan, error) {
	if p.terminal == nil {
		return p.buildMessages()
	}

	switch p.terminal.Kind {
	case StageCount:
		if p.terminal.Field == "" {
			return p.buildScalarCount()
		}
		return p.buildGroupCount(p.terminal.Field, p.clampLimit(500), true)
	case StageTop:
		return p.buildGroupCount(p.terminal.Field, p.clampLimit(p.terminal.N), true)
	case StageParticipants:
		return p.buildGroupCount("anyone", p.clampLimit(p.terminal.N), true)
	case StageSeries:
		return p.buildSeries(*p.terminal)
	case StageAggregate:
		return p.buildAggregate(*p.terminal)
	}
	return nil, fmt.Errorf("cannot plan stage %q", p.terminal.Kind)
}

func (p *pipeline) buildMessages() (*Plan, error) {
	order := "m.date DESC"
	ordered := false
	if p.sort != nil {
		col, err := sortColumn(p.sort.Field)
		if err != nil {
			return nil, err
		}
		dir := "ASC"
		if p.sort.Desc {
			dir = "DESC"
		}
		order = col + " " + dir
		ordered = true
	}

	limit := p.clampLimit(p.opt.defaultLimit())
	args := append([]any{}, p.args...)
	args = append(args, limit)

	sql := "SELECT " + p.selectList + " FROM messages m WHERE " + p.effectiveWhere() +
		" ORDER BY " + order + " LIMIT ?"

	if p.opt.Offset > 0 {
		sql += " OFFSET ?"
		args = append(args, p.opt.Offset)
	}

	return &Plan{SQL: sql, Args: args, Kind: PlanMessages, Limit: limit, Ordered: ordered}, nil
}

func (p *pipeline) buildScalarCount() (*Plan, error) {
	sql := "SELECT COUNT(*) FROM messages m WHERE " + p.effectiveWhere()
	return &Plan{SQL: sql, Args: append([]any{}, p.args...), Kind: PlanScalar}, nil
}

// buildGroupCount counts messages per distinct value of a grouping key.
func (p *pipeline) buildGroupCount(field string, limit int, desc bool) (*Plan, error) {
	g, err := groupKey(field)
	if err != nil {
		return nil, err
	}

	args := append([]any{}, g.preArgs...)
	args = append(args, p.args...)

	// COUNT(DISTINCT m.id): grouping by a multi-valued key joins one message
	// to several rows, and without DISTINCT a message with three recipients
	// would count three times towards a total that claims to count messages.
	sql := "SELECT " + g.expr + " AS label, COUNT(DISTINCT m.id) AS value FROM messages m " +
		g.join + " WHERE " + p.effectiveWhere()
	if g.having != "" {
		sql += " AND " + g.having
	}
	dir := "DESC"
	if !desc {
		dir = "ASC"
	}
	if g.chronological {
		// A time series reads left to right, not biggest first.
		dir = "ASC"
		sql += " GROUP BY label ORDER BY label " + dir
	} else {
		sql += " GROUP BY label ORDER BY value " + dir + ", label ASC"
	}
	sql += " LIMIT ?"
	args = append(args, limit)

	return &Plan{SQL: sql, Args: args, Kind: PlanGroups, Limit: limit, Ordered: true}, nil
}

// buildSeries sums an extracted numeric field into time buckets.
func (p *pipeline) buildSeries(s Stage) (*Plan, error) {
	agg := s
	agg.Func = "sum"
	if agg.Bucket == "" {
		agg.Bucket = "day"
	}
	return p.buildAggregate(agg)
}

// buildAggregate reduces extracted values, optionally bucketed by time.
//
// The row source is annotations rather than messages: one message can yield
// several records — a weekly digest with seven bars is seven rows — and
// collapsing them to the message would throw away the series.
func (p *pipeline) buildAggregate(s Stage) (*Plan, error) {
	annotator := p.extractor
	if s.Annotator != "" {
		annotator = s.Annotator
	}
	if annotator == "" {
		return nil, fmt.Errorf("%s %s: name the extractor first, e.g. `| extract saas-metrics | %s %s by month`",
			s.Func, s.Field, s.Func, s.Field)
	}

	if p.opt.KnownAnnotator != nil && !p.opt.KnownAnnotator(annotator) {
		return nil, fmt.Errorf("no annotator named %q (list them with `iql annotate list`)", annotator)
	}

	fn := strings.ToUpper(s.Func)
	switch fn {
	case "SUM", "AVG", "MIN", "MAX":
	default:
		return nil, fmt.Errorf("%q is not an aggregate (sum, avg, min, max)", s.Func)
	}

	var preArgs []any
	valuePath := "$." + s.Field
	preArgs = append(preArgs, valuePath)
	valueExpr := fn + "(CAST(json_extract(a.data_json, ?) AS REAL))"

	label := "'" + strings.ReplaceAll(s.Field, "'", "''") + "'"
	chronological := false
	if s.Bucket != "" {
		timeCol := "m.date"
		if p.opt.TimeField != nil {
			if path := p.opt.TimeField(annotator); path != "" {
				// The extraction's own timestamp wins over the message's.
				// Stored as text, so it is converted rather than compared.
				preArgs = append([]any{path}, preArgs...)
				timeCol = "COALESCE(CAST(strftime('%s', json_extract(a.data_json, ?)) AS INTEGER) * 1000, m.date)"
			}
		}
		b, err := bucketExpr(timeCol, s.Bucket)
		if err != nil {
			return nil, err
		}
		label = b
		chronological = true
	}

	args := append([]any{}, preArgs...)
	args = append(args, annotator)
	args = append(args, p.args...)

	sql := "SELECT " + label + " AS label, " + valueExpr + " AS value " +
		"FROM messages m " +
		"JOIN annotations a ON a.message_id = m.id AND a.status = 'ok' " +
		"JOIN annotators an ON an.id = a.annotator_id AND a.annotator_version = an.version " +
		"WHERE an.name = ? AND " + p.effectiveWhere() +
		" GROUP BY label"

	if chronological {
		sql += " ORDER BY label ASC"
	} else {
		sql += " ORDER BY value DESC"
	}

	limit := p.clampLimit(1000)
	sql += " LIMIT ?"
	args = append(args, limit)

	return &Plan{SQL: sql, Args: args, Kind: PlanGroups, Limit: limit, Ordered: true}, nil
}

// senderJoin reaches the normalised sender address.
const senderJoin = "JOIN message_participants p ON p.message_id = m.id AND p.role = ?"

// group describes how to aggregate by one key.
type group struct {
	expr    string
	join    string
	having  string
	preArgs []any
	// chronological orders the result by label rather than by count.
	chronological bool
}

func groupKey(field string) (group, error) {
	f := strings.ToLower(field)

	switch f {
	case "hour", "day", "week", "month", "year":
		expr, err := bucketExpr("m.date", f)
		if err != nil {
			return group{}, err
		}
		return group{expr: expr, chronological: true}, nil

	case "from", "sender":
		// The participant edge rather than m.from_addr: the column holds the
		// raw header, so grouping on it splits "alice@x.com" from
		// "Alice <alice@x.com>" into two senders and produces a domain of
		// "x.com>" for the second.
		return group{expr: "p.address", join: senderJoin, preArgs: []any{"from"}}, nil

	case "domain":
		return group{
			expr:    "SUBSTR(p.address, INSTR(p.address, '@') + 1)",
			join:    senderJoin,
			preArgs: []any{"from"},
			having:  "INSTR(p.address, '@') > 0",
		}, nil

	case "to", "cc", "bcc":
		return group{
			expr:    "p.address",
			join:    "JOIN message_participants p ON p.message_id = m.id AND p.role = ?",
			preArgs: []any{f},
		}, nil

	case "anyone", "participant", "participants":
		return group{expr: "p.address", join: "JOIN message_participants p ON p.message_id = m.id"}, nil

	case "account":
		return group{expr: "m.account_id"}, nil

	case "mailbox", "folder":
		return group{expr: "COALESCE(m.mailbox, '(unknown)')"}, nil

	case "subject":
		return group{expr: "m.subject"}, nil

	case "thread":
		return group{expr: threadKeyExpr}, nil

	case "label":
		return group{
			expr: "an.name",
			join: "JOIN annotations a ON a.message_id = m.id AND a.status = 'ok' " +
				"JOIN annotators an ON an.id = a.annotator_id AND a.annotator_version = an.version",
		}, nil

	default:
		return group{}, fmt.Errorf("cannot group by %q (try: from, domain, to, day, week, month, year, account, mailbox, label, thread)", field)
	}
}

func sortColumn(field string) (string, error) {
	switch strings.ToLower(field) {
	case "date", "":
		return "m.date", nil
	case "size":
		return "m.size", nil
	case "subject":
		return "m.subject", nil
	case "from", "sender":
		return "LOWER(m.from_addr)", nil
	case "account":
		return "m.account_id", nil
	default:
		return "", fmt.Errorf("cannot sort by %q (try: date, size, subject, from, account)", field)
	}
}
