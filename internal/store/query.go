package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/query"
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
	// Kind is "messages", "groups" or "count".
	Kind     string             `json:"kind"`
	Count    int                `json:"count"`
	Messages []*message.Message `json:"messages,omitempty"`
	Tickets  []*Ticket          `json:"tickets,omitempty"`
	Groups   []QueryGroup       `json:"groups,omitempty"`
	Total    int64              `json:"total,omitempty"`
	// GroupField and Bucket describe an aggregate result, so a chart can turn
	// a clicked mark back into a query term.
	GroupField string `json:"groupField,omitempty"`
	Bucket     string `json:"bucket,omitempty"`
	// SQL is the compiled statement, returned so `--explain` can show it and
	// so a user learning the language can see what it became.
	SQL  string `json:"sql,omitempty"`
	Args []any  `json:"args,omitempty"`
}

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
				// Drafts live in their own table and have never been rows in
				// messages, so no predicate over messages can select them.
				return "1=0", true
			}
			return folderClause(folder), true
		},
		ThreadIDs:      ThreadMessageIDs,
		SavedQuery:     savedQueryText,
		SelfAddresses:  selfAddresses,
		TimeField:      annotatorTimeField,
		KnownAnnotator: annotatorExists,
		FullText:       ftsEnabled.Load(),
		DefaultLimit:   limit,
		Offset:         offset,
	}
}

// ThreadMessageIDs returns every message sharing a conversation with the given
// message.
//
// Keyed on thread_key, which is maintained on write from the References
// header. Falls back to the message itself when it has no key, which is the
// case for rows written before v18 and never re-saved.
func ThreadMessageIDs(messageID string) ([]string, error) {
	rows, err := db.Query(`
		SELECT id FROM messages
		WHERE thread_key IS NOT NULL
		  AND thread_key = (SELECT thread_key FROM messages WHERE id = ?)
		ORDER BY date ASC`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		// Either the id is unknown or the row predates thread keys. Returning
		// the message alone is right for the second case and harmless for the
		// first, where the caller gets an empty result anyway.
		var exists int
		if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE id = ?", messageID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists > 0 {
			return []string{messageID}, nil
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
