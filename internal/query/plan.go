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
	// PlanTickets returns ticket rows.
	PlanTickets
	// PlanDrafts returns draft rows.
	PlanDrafts
	// PlanContacts returns contact rows.
	PlanContacts
	// PlanThreads returns conversation keys, one per row.
	//
	// Unlike every other kind, the statement is not the answer — it selects
	// which conversations to assemble, and the store fills each one in. That
	// is deliberate: a timeline is heterogeneous, and expressing "messages and
	// tickets and drafts, interleaved by time" as one SQL statement means a
	// union of three tables padded to a common column list. Selecting the keys
	// and then loading each entity by its own query is both faster and the
	// only version a person can read.
	PlanThreads
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
	// Entity is the table the plan reads from: messages, or tickets.
	Entity string
	// GroupField and Bucket name what an aggregate grouped by. A chart cannot
	// build a drill-down from labels alone — it needs to know that "2026-03"
	// came from `month` and not from a sender called 2026-03.
	GroupField string
	Bucket     string
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
	entity := q.Entity()
	where, args, err := CompileFilterFor(q.Filter, opt, entity)
	if err != nil {
		return nil, err
	}

	p := &pipeline{opt: opt, where: where, args: args, selectList: selectList, entity: entity}
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
	entity     string

	extractor string
	terminal  *Stage
	sort      *Stage
	limit     int
	expand    bool
	sample    bool
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

		case StageSample:
			// A random sample, not the newest n.
			//
			// Judging an annotator by reading its most recent results is how
			// you conclude it works: recent mail is the mail you already know
			// about. Reading a random draw is how you find out.
			p.limit = s.N
			p.sample = true

		case StageCount, StageTop, StageSeries, StageAggregate, StageParticipants, StageTimeline, StageNetwork:
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
	// Checked before the entity split because a timeline is the one result
	// shape that is the same question of every entity: mail, tickets and
	// drafts all resolve to the conversations they belong to.
	if p.terminal != nil && p.terminal.Kind == StageTimeline {
		return p.buildTimeline()
	}
	if p.terminal != nil && p.terminal.Kind == StageNetwork {
		return p.buildNetwork()
	}
	if p.entity == EntityTicket {
		return p.buildTickets()
	}
	if p.entity == EntityDraft {
		return p.buildDrafts()
	}
	if p.entity == EntityContact {
		return p.buildContacts()
	}
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
	if p.sample {
		order = "RANDOM()"
		ordered = true
	}
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

// draftSelectList is the column list store.scanDraft expects.
const draftSelectList = `d.id, d.account_id, d.in_reply_to, d.thread_key, d.to_addrs, d.cc_addrs, d.bcc_addrs,
	d.subject, d.body, d.status, d.origin, d.created_at, d.updated_at, d.queued_at, d.sent_at, d.last_error`

// buildDrafts plans a query over the drafts table.
//
// Newest first, because a draft list is a working queue rather than an archive
// — the one you were writing is the one you want.
func (p *pipeline) buildDrafts() (*Plan, error) {
	args := append([]any{}, p.args...)

	if p.terminal != nil {
		switch p.terminal.Kind {
		case StageCount:
			if p.terminal.Field == "" {
				return &Plan{
					SQL:    "SELECT COUNT(*) FROM drafts d WHERE " + p.where,
					Args:   args,
					Kind:   PlanScalar,
					Entity: EntityDraft,
				}, nil
			}
			return p.buildDraftGroups(p.terminal.Field, p.clampLimit(200))
		case StageTop:
			return p.buildDraftGroups(p.terminal.Field, p.clampLimit(p.terminal.N))
		default:
			return nil, fmt.Errorf("%s does not apply to drafts", p.terminal.Kind)
		}
	}

	order := "d.updated_at DESC"
	if p.sort != nil {
		switch strings.ToLower(p.sort.Field) {
		case "date", "updated", "":
			order = "d.updated_at"
		case "created":
			order = "d.created_at"
		case "subject":
			order = "d.subject"
		case "status":
			order = "d.status"
		default:
			return nil, fmt.Errorf("cannot sort drafts by %q (try: date, created, subject, status)", p.sort.Field)
		}
		if p.sort.Desc {
			order += " DESC"
		} else {
			order += " ASC"
		}
	}

	limit := p.clampLimit(p.opt.defaultLimit())
	args = append(args, limit)
	sql := "SELECT " + draftSelectList + " FROM drafts d WHERE " + p.where +
		" ORDER BY " + order + " LIMIT ?"
	if p.opt.Offset > 0 {
		sql += " OFFSET ?"
		args = append(args, p.opt.Offset)
	}
	return &Plan{SQL: sql, Args: args, Kind: PlanDrafts, Limit: limit, Entity: EntityDraft}, nil
}

func (p *pipeline) buildDraftGroups(field string, limit int) (*Plan, error) {
	var expr string
	switch strings.ToLower(field) {
	case "status", "":
		expr = "d.status"
	case "origin", "raised":
		expr = "d.origin"
	case "account":
		expr = "d.account_id"
	case "day", "week", "month", "year":
		b, err := bucketExpr("d.created_at", strings.ToLower(field))
		if err != nil {
			return nil, err
		}
		expr = b
	default:
		return nil, fmt.Errorf("cannot group drafts by %q (try: status, origin, account, day, week, month, year)", field)
	}

	args := append([]any{}, p.args...)
	args = append(args, limit)
	sql := "SELECT " + expr + " AS label, COUNT(*) AS value FROM drafts d WHERE " + p.where +
		" GROUP BY label ORDER BY value DESC, label ASC LIMIT ?"
	return &Plan{SQL: sql, Args: args, Kind: PlanGroups, Limit: limit, Ordered: true,
		Entity: EntityDraft, GroupField: strings.ToLower(field)}, nil
}

// buildTickets plans a query over the tickets table.
//
// A deliberately smaller surface than the message pipeline: tickets are
// listed, counted and grouped by their own fields. Time-bucketed series over
// extracted values are a question about mail, not about tickets.
func (p *pipeline) buildTickets() (*Plan, error) {
	args := append([]any{}, p.args...)

	if p.sample {
		// Tickets are a working set, not a population to sample.
		return nil, fmt.Errorf("sample applies to messages, not tickets")
	}
	if p.terminal != nil {
		switch p.terminal.Kind {
		case StageCount:
			if p.terminal.Field == "" {
				return &Plan{
					SQL:    "SELECT COUNT(*) FROM tickets t WHERE " + p.where,
					Args:   args,
					Kind:   PlanScalar,
					Entity: EntityTicket,
				}, nil
			}
			return p.buildTicketGroups(p.terminal.Field, p.clampLimit(200))
		case StageTop:
			return p.buildTicketGroups(p.terminal.Field, p.clampLimit(p.terminal.N))
		default:
			return nil, fmt.Errorf("%s does not apply to tickets; it reads extracted message data", p.terminal.Kind)
		}
	}

	order := "COALESCE(t.due_at, t.updated_at) ASC"
	if p.sort != nil {
		col, err := ticketSortColumn(p.sort.Field)
		if err != nil {
			return nil, err
		}
		dir := "ASC"
		if p.sort.Desc {
			dir = "DESC"
		}
		order = col + " " + dir
	}

	limit := p.clampLimit(p.opt.defaultLimit())
	args = append(args, limit)
	sql := "SELECT " + ticketSelectList + " FROM tickets t WHERE " + p.where +
		" ORDER BY " + order + " LIMIT ?"
	if p.opt.Offset > 0 {
		sql += " OFFSET ?"
		args = append(args, p.opt.Offset)
	}
	return &Plan{SQL: sql, Args: args, Kind: PlanTickets, Limit: limit, Entity: EntityTicket}, nil
}

func (p *pipeline) buildTicketGroups(field string, limit int) (*Plan, error) {
	expr, err := ticketGroupKey(field)
	if err != nil {
		return nil, err
	}
	args := append([]any{}, p.args...)
	args = append(args, limit)

	sql := "SELECT " + expr + " AS label, COUNT(*) AS value FROM tickets t WHERE " + p.where +
		" GROUP BY label ORDER BY value DESC, label ASC LIMIT ?"
	return &Plan{SQL: sql, Args: args, Kind: PlanGroups, Limit: limit, Ordered: true,
		Entity: EntityTicket, GroupField: strings.ToLower(field)}, nil
}

// ticketSelectList is the column list store.scanTicket expects.
const ticketSelectList = `t.id, COALESCE(t.thread_key, ''), t.title, COALESCE(t.body, ''), t.status,
	COALESCE(t.priority, ''), t.due_at, t.origin, COALESCE(t.annotator_id, ''), t.confidence,
	t.created_at, t.updated_at, t.closed_at`

func ticketGroupKey(field string) (string, error) {
	switch strings.ToLower(field) {
	case "status", "":
		return "t.status", nil
	case "priority":
		return "COALESCE(t.priority, '(none)')", nil
	case "raised", "origin":
		return "t.origin", nil
	case "day", "week", "month", "year":
		return bucketExpr("t.created_at", strings.ToLower(field))
	default:
		return "", fmt.Errorf("cannot group tickets by %q (try: status, priority, raised, day, week, month, year)", field)
	}
}

func ticketSortColumn(field string) (string, error) {
	switch strings.ToLower(field) {
	case "due", "":
		return "COALESCE(t.due_at, t.updated_at)", nil
	case "created":
		return "t.created_at", nil
	case "updated":
		return "t.updated_at", nil
	case "status":
		return "t.status", nil
	case "priority":
		return "t.priority", nil
	case "title", "ticket":
		return "t.title", nil
	default:
		return "", fmt.Errorf("cannot sort tickets by %q (try: due, created, updated, status, priority, title)", field)
	}
}

func (p *pipeline) buildScalarCount() (*Plan, error) {
	sql := "SELECT COUNT(*) FROM messages m WHERE " + p.effectiveWhere()
	return &Plan{SQL: sql, Args: append([]any{}, p.args...), Kind: PlanScalar}, nil
}

// buildGroupCount counts messages per distinct value of a grouping key.
func (p *pipeline) buildGroupCount(field string, limit int, desc bool) (*Plan, error) {
	g, err := groupKey(field, p.opt)
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

	return &Plan{SQL: sql, Args: args, Kind: PlanGroups, Limit: limit, Ordered: true,
		GroupField: strings.ToLower(field), Bucket: bucketOf(field)}, nil
}

// bucketOf reports whether a grouping key is itself a time bucket.
func bucketOf(field string) string {
	switch strings.ToLower(field) {
	case "hour", "day", "week", "month", "year":
		return strings.ToLower(field)
	}
	return ""
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

	return &Plan{SQL: sql, Args: args, Kind: PlanGroups, Limit: limit, Ordered: true,
		GroupField: s.Field, Bucket: s.Bucket}, nil
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

func groupKey(field string, opt Options) (group, error) {
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

	case "topic":
		// The first word of the subject line. This is a placeholder and always
		// was — there is no topic modelling here — but it is the placeholder
		// the dashboard's Topic Trends widget has always shown, and expressing
		// it as a grouping key is what let the hand-rolled GetTopicStats go.
		// Real topics are a job for an annotator.
		// Extracted topics where they exist, the subject's first word where
		// they do not.
		//
		// Degrading per message rather than per mailbox is what makes running
		// the annotator worth doing incrementally: every message it reaches
		// gets a real topic, and the rest keep the placeholder answer instead
		// of vanishing from the chart.
		expr := "COALESCE(mt.topic, LOWER(SUBSTR(m.subject, 1, INSTR(m.subject || ' ', ' ') - 1)))"
		g := group{
			expr: expr,
			join: "LEFT JOIN message_topics mt ON mt.message_id = m.id",
			// Parenthesised: the ignore filter is appended with AND, which
			// binds tighter than OR — without these the exclusion applied to
			// only half the condition and `fwd:` came straight back.
			having: "(m.subject != '' OR mt.topic IS NOT NULL)",
		}

		// The ignore list belongs to the language, not to one consumer of it.
		if opt.IgnoredTopicWords != nil {
			if words := opt.IgnoredTopicWords(); len(words) > 0 {
				g.having += " AND " + expr + " NOT IN (" + placeholders(len(words)) + ")"
				for _, w := range words {
					g.preArgs = append(g.preArgs, strings.ToLower(strings.TrimSpace(w)))
				}
			}
		}
		return g, nil

	case "thread":
		return group{expr: threadKeyExpr}, nil

	case "label":
		return group{
			expr: "an.name",
			join: "JOIN annotations a ON a.message_id = m.id AND a.status = 'ok' " +
				"JOIN annotators an ON an.id = a.annotator_id AND a.annotator_version = an.version",
		}, nil

	default:
		return group{}, fmt.Errorf("cannot group by %q (try: %s)", field, strings.Join(GroupKeys, ", "))
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

// buildTimeline selects the conversations a query touches, newest first.
//
// # Why COALESCE on the key
//
// thread_key is maintained on write, so rows written before v17 and never
// re-saved have none, and a ticket someone raised by hand belongs to no
// conversation at all. Falling back to the row's own id makes each of those
// its own single-item thread, which is what they are. Dropping them instead
// would be the failure this project keeps re-learning: a view that silently
// omits rows looks like it is working.
//
// The store's loader knows about the fallback and matches it — it looks up the
// id branch only for rows whose key is NULL, so a key can never collide with
// an unrelated row's id.
func (p *pipeline) buildTimeline() (*Plan, error) {
	limit := p.clampLimit(50)
	args := append([]any{}, p.args...)
	args = append(args, limit)

	var sql string
	switch p.entity {
	case EntityTicket:
		sql = "SELECT COALESCE(t.thread_key, t.id) AS k FROM tickets t WHERE " + p.where +
			" GROUP BY k ORDER BY MAX(COALESCE(t.due_at, t.updated_at)) DESC LIMIT ?"
	case EntityDraft:
		sql = "SELECT COALESCE(d.thread_key, d.id) AS k FROM drafts d WHERE " + p.where +
			" GROUP BY k ORDER BY MAX(d.updated_at) DESC LIMIT ?"
	default:
		sql = "SELECT COALESCE(m.thread_key, m.id) AS k FROM messages m WHERE " + p.effectiveWhere() +
			" GROUP BY k ORDER BY MAX(m.date) DESC LIMIT ?"
	}

	if p.opt.Offset > 0 {
		sql += " OFFSET ?"
		args = append(args, p.opt.Offset)
	}

	return &Plan{SQL: sql, Args: args, Kind: PlanThreads, Limit: limit,
		Entity: p.entity, Ordered: true}, nil
}

// contactSelectList is the column list store.scanContact expects.
//
// Passed in by the store for the same reason the message list is: this package
// stays out of the schema's business.
var contactSelectList = ""

// SetContactSelectList lets the store declare its contact column list once.
func SetContactSelectList(cols string) { contactSelectList = cols }

// buildContacts plans a query over the contacts table.
//
// Ordered by how much mail there is, because "who do I deal with" is almost
// always the question — an alphabetical contact list is a phone book, and a
// phone book is not what a mailbox is for.
func (p *pipeline) buildContacts() (*Plan, error) {
	args := append([]any{}, p.args...)

	if p.terminal != nil {
		switch p.terminal.Kind {
		case StageCount:
			if p.terminal.Field == "" {
				return &Plan{
					SQL:    "SELECT COUNT(*) FROM contacts c WHERE " + p.where,
					Args:   args,
					Kind:   PlanScalar,
					Entity: EntityContact,
				}, nil
			}
			return p.buildContactGroups(p.terminal.Field, p.clampLimit(200))
		case StageTop:
			return p.buildContactGroups(p.terminal.Field, p.clampLimit(p.terminal.N))
		default:
			return nil, fmt.Errorf("%s does not apply to contacts", p.terminal.Kind)
		}
	}

	order := "messages DESC, c.address ASC"
	if p.sort != nil {
		switch strings.ToLower(p.sort.Field) {
		case "messages", "":
			order = "messages"
		case "name":
			order = "COALESCE(NULLIF(c.display_name, ''), NULLIF(c.header_name, ''), c.address)"
		case "email", "address":
			order = "c.address"
		case "seen", "last":
			order = "last_seen"
		default:
			return nil, fmt.Errorf("cannot sort contacts by %q (try messages, name, email or seen)", p.sort.Field)
		}
		if p.sort.Desc {
			order += " DESC"
		} else {
			order += " ASC"
		}
	}

	limit := p.clampLimit(p.opt.defaultLimit())
	args = append(args, limit)
	sql := "SELECT " + contactSelectList + " FROM contacts c WHERE " + p.where +
		" ORDER BY " + order + " LIMIT ?"
	if p.opt.Offset > 0 {
		sql += " OFFSET ?"
		args = append(args, p.opt.Offset)
	}
	return &Plan{SQL: sql, Args: args, Kind: PlanContacts, Limit: limit, Entity: EntityContact}, nil
}

func (p *pipeline) buildContactGroups(field string, limit int) (*Plan, error) {
	var expr string
	switch strings.ToLower(field) {
	case "kind":
		expr = "c.kind"
	case "org":
		expr = "COALESCE(NULLIF(c.org, ''), '(unknown)')"
	case "domain":
		expr = "SUBSTR(c.address, INSTR(c.address, '@') + 1)"
	default:
		return nil, fmt.Errorf("cannot group contacts by %q (try kind, org or domain)", field)
	}

	args := append([]any{}, p.args...)
	args = append(args, limit)
	sql := "SELECT " + expr + " AS label, COUNT(*) AS value FROM contacts c WHERE " + p.where +
		" GROUP BY label ORDER BY value DESC LIMIT ?"
	return &Plan{SQL: sql, Args: args, Kind: PlanGroups, Limit: limit, Ordered: true,
		Entity: EntityContact, GroupField: field}, nil
}

// placeholders builds "?, ?, ?" for an IN clause.
//
// n is always a slice length this package computed, never anything a user
// supplied, so the values still arrive as bound arguments.
func placeholders(n int) string {
	if n <= 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// buildNetwork returns who appears alongside whom.
//
// # Why it is computed and not stored
//
// An edge is "these two were on the same message", which message_participants
// already records — a self-join is the whole implementation. An edges table
// would be a cache of a join, and it would go stale the first time a message
// was deleted or re-imported, with nothing to notice.
//
// # Two kinds of edge, deliberately distinguished
//
// `corresponded` is one of them sending to the other; `co-present` is both
// being recipients of someone else's message. The second is much weaker
// evidence of a relationship — everyone cc'd on a company-wide announcement is
// co-present with everyone else — so it is counted separately rather than
// summed into one misleading number.
func (p *pipeline) buildNetwork() (*Plan, error) {
	limit := p.clampLimit(p.terminal.N)
	args := append([]any{}, p.args...)
	args = append(args, p.args...)
	args = append(args, limit)

	scope := "SELECT m.id FROM messages m WHERE " + p.effectiveWhere()

	sql := `SELECT a.address || ' — ' || b.address AS label, COUNT(DISTINCT a.message_id) AS value
		FROM message_participants a
		JOIN message_participants b
		  ON a.message_id = b.message_id AND a.address < b.address
		WHERE a.message_id IN (` + scope + `)
		  AND b.message_id IN (` + scope + `)
		GROUP BY label
		ORDER BY value DESC
		LIMIT ?`

	return &Plan{SQL: sql, Args: args, Kind: PlanGroups, Limit: limit, Ordered: true,
		GroupField: "pair"}, nil
}
