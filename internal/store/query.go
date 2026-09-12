package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/query"
	"strconv"
)

// QueryGroup is one row of an aggregate result.
//
// Value is a float because aggregates over extracted data are not counts —
// `| sum amount by month` on invoice extractions is currency. Counts arrive
// here as whole numbers and marshal as such.
type QueryGroup struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// QueryResult is what running a query produces.
//
// One type for three shapes because a caller that accepts a query string
// cannot know in advance whether it ends in an aggregate; Kind says which
// field to read.
type QueryResult struct {
	Query string `json:"query"`
	// Kind is "messages", "groups", "count", "tickets", "drafts", "contacts",
	// "attachments" or "threads".
	Kind        string             `json:"kind"`
	Count       int                `json:"count"`
	Messages    []*message.Message `json:"messages,omitempty"`
	Tickets     []*Ticket          `json:"tickets,omitempty"`
	Drafts      []*Draft           `json:"drafts,omitempty"`
	Contacts    []*Contact         `json:"contacts,omitempty"`
	Attachments []*AttachmentFile  `json:"attachments,omitempty"`
	Topics      []ContactTopic     `json:"topics,omitempty"`
	Threads     []*Thread          `json:"threads,omitempty"`
	Groups      []QueryGroup       `json:"groups,omitempty"`
	Total       int64              `json:"total,omitempty"`
	// GroupField and Bucket describe an aggregate result, so a chart can turn
	// a clicked mark back into a query term.
	GroupField string `json:"groupField,omitempty"`
	Bucket     string `json:"bucket,omitempty"`
	// SQL is the compiled statement, returned so `--explain` can show it and
	// so a user learning the language can see what it became.
	SQL  string `json:"sql,omitempty"`
	Args []any  `json:"args,omitempty"`
}

// The compiler is told the contact column list once, for the same reason it is
// handed the message one: the schema lives here, not there.
func init() { query.SetContactSelectList(ContactSelectList) }

// queryOptions supplies the compiler with the parts that need the database.
func queryOptions(limit, offset int) query.Options {
	return query.Options{
		FolderSQL: func(folder string) (string, bool) {
			if folder == "" || folder == FolderAll {
				return "", true
			}
			if !ValidFolder(folder) {
				return "", false
			}
			if IsDraftFolder(folder) {
				// Drafts are their own entity. `folder:drafts` is kept as
				// sugar for `in:drafts`, and Query.Entity resolves it before
				// the compiler gets here — so reaching this point means a
				// drafts folder was asked for in a query about mail.
				return "1=0", true
			}
			return folderClause(folder), true
		},
		IgnoredTopicWords:  ignoredTopicWords,
		SimilarIDs:         similarMessageIDs,
		SimilarAttachments: similarAttachmentKeys,
		ThreadIDs:          ThreadMessageIDs,
		SavedQuery:         savedQueryText,
		SelfAddresses:      selfAddresses,
		TimeField:          annotatorTimeField,
		KnownAnnotator:     annotatorExists,
		FullText:           ftsEnabled.Load(),
		DefaultLimit:       limit,
		Offset:             offset,
	}
}

// ThreadMessageIDs returns every message in the conversation named by an id.
//
// # Why it takes either kind of id
//
// `thread:` used to accept only a message id, and resolve the conversation
// through it. But a conversation's own name is its thread_key, and that is
// what everything holding a conversation has to hand: the timeline's rows, a
// ticket's ThreadKey, a draft's. Passing one produced an empty result rather
// than an error — so the timeline's own "Open this conversation" button
// silently matched nothing, which reads exactly like a conversation with no
// mail in it.
//
// Accepting both is what `thread:` already means to a reader: the conversation
// identified by this, whichever way it is identified.
//
// Keyed on thread_key, which is maintained on write from the References
// header. Falls back to the message itself when it has no key, which is the
// case for rows written before v17 and never re-saved.
func ThreadMessageIDs(id string) ([]string, error) {
	rows, err := db.Query(`
		SELECT id FROM messages
		WHERE thread_key IS NOT NULL
		  AND thread_key = COALESCE(
		        (SELECT thread_key FROM messages WHERE id = ?),
		        ?)
		ORDER BY date ASC`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var rowID string
		if err := rows.Scan(&rowID); err != nil {
			return nil, err
		}
		ids = append(ids, rowID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		// Either the id is unknown or the row predates thread keys. Returning
		// the message alone is right for the second case and harmless for the
		// first, where the caller gets an empty result anyway.
		var exists int
		if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE id = ?", id).Scan(&exists); err != nil {
			return nil, err
		}
		if exists > 0 {
			return []string{id}, nil
		}
	}
	return ids, nil
}

// RunQuery parses, compiles and executes a query string.
func RunQuery(src string, limit, offset int) (*QueryResult, error) {
	q, err := query.Parse(src)
	if err != nil {
		return nil, err
	}

	plan, err := query.Build(q, queryOptions(limit, offset), aliasedMessageColumns())
	if err != nil {
		return nil, err
	}

	res := &QueryResult{Query: src, SQL: plan.SQL, Args: plan.Args,
		GroupField: plan.GroupField, Bucket: plan.Bucket}

	switch plan.Kind {
	case query.PlanTopics:
		topics, err := scanContactTopics(plan)
		if err != nil {
			return nil, err
		}
		res.Kind, res.Topics, res.Count = "topics", topics, len(topics)
		return res, nil

	case query.PlanContacts:
		contacts, err := scanContacts(plan)
		if err != nil {
			return nil, err
		}
		res.Kind, res.Contacts, res.Count = "contacts", contacts, len(contacts)
		return res, nil

	case query.PlanAttachments:
		files, err := scanAttachmentFiles(plan)
		if err != nil {
			return nil, err
		}
		res.Kind, res.Attachments, res.Count = "attachments", files, len(files)
		return res, nil

	case query.PlanThreads:
		threads, err := scanThreads(plan)
		if err != nil {
			return nil, err
		}
		res.Kind, res.Threads, res.Count = "threads", threads, len(threads)
		return res, nil

	case query.PlanDrafts:
		drafts, err := scanDrafts(plan)
		if err != nil {
			return nil, err
		}
		res.Kind, res.Drafts, res.Count = "drafts", drafts, len(drafts)
		return res, nil

	case query.PlanTickets:
		tickets, err := scanTickets(plan)
		if err != nil {
			return nil, err
		}
		res.Kind, res.Tickets, res.Count = "tickets", tickets, len(tickets)
		return res, nil

	case query.PlanScalar:
		var n int64
		if err := db.QueryRow(plan.SQL, plan.Args...).Scan(&n); err != nil {
			return nil, err
		}
		res.Kind, res.Total, res.Count = "count", n, 1
		return res, nil

	case query.PlanGroups:
		groups, err := scanGroups(plan)
		if err != nil {
			return nil, err
		}
		res.Kind, res.Groups, res.Count = "groups", groups, len(groups)
		return res, nil

	default:
		msgs, err := scanMessages(plan)
		if err != nil {
			return nil, err
		}
		res.Kind, res.Messages, res.Count = "messages", msgs, len(msgs)
		return res, nil
	}
}

// filterClause compiles just the filter half of a query into a WHERE clause
// over alias `m`.
//
// Used wherever a query string narrows something that is not a message list —
// an annotator's scope, a rule's definition, a progress count — so those all
// accept the same language the search bar does rather than inventing a second
// way to say "mail from Stripe".
func filterClause(src string) (string, []any, error) {
	q, err := query.Parse(src)
	if err != nil {
		return "", nil, err
	}
	if len(q.Stages) > 0 {
		return "", nil, fmt.Errorf("a pipeline stage is not allowed here: %q selects messages, it cannot aggregate them", src)
	}
	return query.CompileFilter(q.Filter, queryOptions(0, 0))
}

// aliasedMessageColumns qualifies the canonical column list with the `m` alias
// the compiler uses for its predicates.
//
// Derived from messageColumns rather than written out again: the hand-written
// copy in ListMessagesFiltered had already drifted once, losing `mailbox`.
func aliasedMessageColumns() string {
	cols := strings.FieldsFunc(messageColumns, func(r rune) bool { return r == ',' })
	for i, c := range cols {
		cols[i] = "m." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

func scanDrafts(plan *query.Plan) ([]*Draft, error) {
	rows, err := db.Query(plan.SQL, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Draft{}
	for rows.Next() {
		d, err := scanDraft(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// scanContactTopics reads the topic rows and finishes the arithmetic.
//
// The plan orders by lift because ordering has to happen before the LIMIT.
// Share and lift are computed here from the counts it returned, so the numbers
// shown are the ones the ordering used rather than a second calculation that
// could disagree with it.
func scanContactTopics(plan *query.Plan) ([]ContactTopic, error) {
	rows, err := db.Query(plan.SQL, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ContactTopic{}
	for rows.Next() {
		var ct ContactTopic
		var mine, corpusHits, corpus int64
		if err := rows.Scan(&ct.Topic, &ct.Messages, &mine, &corpusHits, &corpus); err != nil {
			return nil, err
		}
		if mine > 0 {
			ct.Share = float64(ct.Messages) / float64(mine)
		}
		if corpusHits > 0 && corpus > 0 && ct.Share > 0 {
			ct.Lift = ct.Share / (float64(corpusHits) / float64(corpus))
		}
		out = append(out, ct)
	}
	return out, rows.Err()
}

func scanAttachmentFiles(plan *query.Plan) ([]*AttachmentFile, error) {
	rows, err := db.Query(plan.SQL, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*AttachmentFile{}
	for rows.Next() {
		f, err := scanAttachmentFile(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func scanContacts(plan *query.Plan) ([]*Contact, error) {
	rows, err := db.Query(plan.SQL, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Contact{}
	for rows.Next() {
		c, err := scanContact(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := PopulateContactTags(out); err != nil {
		return nil, err
	}
	return out, nil
}

func scanTickets(plan *query.Plan) ([]*Ticket, error) {
	rows, err := db.Query(plan.SQL, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Evidence is loaded per ticket rather than joined in: a ticket has few
	// sources, and joining would multiply the ticket rows by them.
	for _, t := range out {
		if t.Sources, err = ticketSources(t.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func scanGroups(plan *query.Plan) ([]QueryGroup, error) {
	rows, err := db.Query(plan.SQL, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []QueryGroup{}
	for rows.Next() {
		var label sql.NullString
		var value sql.NullFloat64
		if err := rows.Scan(&label, &value); err != nil {
			return nil, err
		}
		groups = append(groups, QueryGroup{Label: label.String, Value: value.Float64})
	}
	return groups, rows.Err()
}

func scanMessages(plan *query.Plan) ([]*message.Message, error) {
	rows, err := db.Query(plan.SQL, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := []*message.Message{}
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// CountQuery returns how many messages a filter matches, ignoring any pipeline.
//
// Used for paging totals, where the caller needs "of how many" alongside a
// page of results.
func CountQuery(src string) (int64, error) {
	q, err := query.Parse(src)
	if err != nil {
		return 0, err
	}
	q.Stages = []query.Stage{{Kind: query.StageCount}}

	plan, err := query.Build(q, queryOptions(0, 0), "m.id")
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.QueryRow(plan.SQL, plan.Args...).Scan(&n)
	return n, err
}

// ExplainQuery compiles a query without running it.
func ExplainQuery(src string) (*QueryResult, error) {
	q, err := query.Parse(src)
	if err != nil {
		return nil, err
	}
	plan, err := query.Build(q, queryOptions(0, 0), "m.id")
	if err != nil {
		return nil, err
	}
	kind := "messages"
	switch plan.Kind {
	case query.PlanGroups:
		kind = "groups"
	case query.PlanScalar:
		kind = "count"
	case query.PlanTickets:
		kind = "tickets"
	case query.PlanDrafts:
		kind = "drafts"
	case query.PlanThreads:
		kind = "threads"
	case query.PlanContacts:
		kind = "contacts"
	case query.PlanTopics:
		kind = "topics"
	}
	return &QueryResult{Query: src, Kind: kind, SQL: plan.SQL, Args: plan.Args}, nil
}

// annotatorExists reports whether an annotator name is real.
func annotatorExists(name string) bool {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM annotators WHERE name = ?", name).Scan(&n); err != nil {
		// A query should not fail because this lookup did; the worst case is
		// the pre-existing behaviour of an empty result.
		return true
	}
	return n > 0
}

// annotatorTimeField reports which extracted field carries a record's own
// timestamp, as declared in the annotator's schema.
//
// Empty means "use the message date". Getting this wrong is the quiet failure
// mode of extracted time series: a Monday digest reporting last week charts a
// week late, and the shape still looks plausible.
func annotatorTimeField(name string) string {
	var schemaJSON string
	err := db.QueryRow("SELECT schema_json FROM annotators WHERE name = ?", name).Scan(&schemaJSON)
	if err != nil {
		return ""
	}
	var schema struct {
		TimeField string `json:"timeField"`
	}
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return ""
	}
	if schema.TimeField == "" {
		return ""
	}
	return "$." + schema.TimeField
}

// ValidateQuery parses a query and reports the first problem with it.
func ValidateQuery(src string) error {
	if _, err := query.Parse(src); err != nil {
		return err
	}
	if _, err := ExplainQuery(src); err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}

// ignoredTopicWords reads the words the user asked not to see as topics.
//
// Read per query rather than cached: it is one row, and a cached copy would
// keep answering with the old list after the setting changed.
func ignoredTopicWords() []string {
	raw, err := GetSetting("ignore_words")
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, w := range strings.Split(raw, ",") {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// SettingSimilarityThreshold is the default for `similar:` when a query does
// not name one.
//
// A stored preference rather than a constant because the right value depends
// on the embedding model: cosine values sit in a band whose width the model
// decides, so a number tuned against one model means something else against
// another.
const SettingSimilarityThreshold = "similarity_threshold"

// DefaultSimilarityThreshold reads the stored default.
func DefaultSimilarityThreshold() float64 {
	raw, err := GetSetting(SettingSimilarityThreshold)
	if err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && v >= 0 && v <= 1 {
			return v
		}
	}
	// Deliberately loose. A first look at "more like this" that returns
	// nothing teaches nothing; one that returns too much at least shows the
	// shape of the distribution, and the number is adjustable from there.
	return 0.5
}

func similarMessageIDs(messageID string, threshold float64) ([]string, error) {
	if threshold < 0 {
		threshold = DefaultSimilarityThreshold()
	}
	// A cap, because `similar:` is a filter and an unbounded one over a large
	// mailbox would put every id into a single IN clause.
	neighbours, err := SimilarTo(messageID, threshold, 500)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(neighbours))
	for _, n := range neighbours {
		ids = append(ids, n.MessageID)
	}
	return ids, nil
}
