package store

import (
	"database/sql"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Error categories. One producer today; the column exists so a second one does
// not require a schema change.
const (
	ErrorCategoryImport = "import"
)

// LoggedError is one recorded failure.
//
// Deliberately not tied to import: an error worth showing a person is worth
// recording the same way wherever it came from.
type LoggedError struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	JobID     string `json:"jobId,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	// Context is where it happened — a mailbox path, say.
	Context string `json:"context,omitempty"`
	// Reference identifies the specific item, e.g. the file that would not parse.
	Reference string    `json:"reference,omitempty"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

// maxLoggedMessage bounds a stored message. A pathological error string — a
// whole message body echoed back in a parser error, for instance — should not
// be able to bloat the database one row at a time.
const maxLoggedMessage = 2000

// LogError records a failure.
//
// Best-effort by design: the caller is already handling something that went
// wrong, and failing to write the record must not escalate into failing the
// operation. The error is returned for callers that care, and ignored by most.
func LogError(e *LoggedError) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	e.Message = sanitiseForLog(e.Message)
	e.Reference = sanitiseForLog(e.Reference)
	e.Context = sanitiseForLog(e.Context)

	_, err := db.Exec(`
		INSERT INTO error_log (id, category, job_id, account_id, context, reference, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.Category, nullIfEmpty(e.JobID), nullIfEmpty(e.AccountID),
		nullIfEmpty(e.Context), nullIfEmpty(e.Reference), e.Message, e.CreatedAt.UnixMilli())
	return err
}

// sanitiseForLog makes an error string safe to store and to print.
//
// The text can contain arbitrary bytes: a MIME parser handed a corrupt message
// will happily quote the offending header back, and that header came from an
// email. Writing raw control characters into a log means terminal escape
// sequences in `iql errors` output and unreadable rows in the UI, so anything
// non-printable becomes a visible placeholder instead.
func sanitiseForLog(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	truncated := false
	for _, r := range s {
		if b.Len() >= maxLoggedMessage {
			truncated = true
			break
		}
		switch {
		case r == '\n' || r == '\t':
			// Newlines and tabs carry real structure in a parser error.
			b.WriteRune(r)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		case r == utf8.RuneError:
			b.WriteRune('\uFFFD')
		default:
			b.WriteRune('\uFFFD')
		}
	}
	if truncated {
		b.WriteString("…")
	}
	return strings.TrimSpace(b.String())
}

// ErrorQuery filters the log.
type ErrorQuery struct {
	Category string
	JobID    string
	Limit    int
	Offset   int
}

const errorColumns = `id, category, job_id, account_id, context, reference, message, created_at`

// ListErrors returns logged errors, newest first.
func ListErrors(q ErrorQuery) ([]*LoggedError, error) {
	query := "SELECT " + errorColumns + " FROM error_log"
	var clauses []string
	var args []any

	if q.Category != "" {
		clauses = append(clauses, "category = ?")
		args = append(args, q.Category)
	}
	if q.JobID != "" {
		clauses = append(clauses, "job_id = ?")
		args = append(args, q.JobID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	query += " ORDER BY created_at DESC, rowid DESC LIMIT ? OFFSET ?"
	args = append(args, limit, q.Offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*LoggedError
	for rows.Next() {
		e := &LoggedError{}
		var jobID, accountID, context, reference sql.NullString
		var created int64
		if err := rows.Scan(&e.ID, &e.Category, &jobID, &accountID,
			&context, &reference, &e.Message, &created); err != nil {
			return nil, err
		}
		e.JobID = jobID.String
		e.AccountID = accountID.String
		e.Context = context.String
		e.Reference = reference.String
		e.CreatedAt = time.UnixMilli(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountErrors totals the log under a filter, so a UI can page without
// fetching everything.
func CountErrors(q ErrorQuery) (int, error) {
	query := "SELECT COUNT(*) FROM error_log"
	var clauses []string
	var args []any
	if q.Category != "" {
		clauses = append(clauses, "category = ?")
		args = append(args, q.Category)
	}
	if q.JobID != "" {
		clauses = append(clauses, "job_id = ?")
		args = append(args, q.JobID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	var n int
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

// ClearErrors removes logged errors under a filter, returning how many went.
func ClearErrors(q ErrorQuery) (int64, error) {
	query := "DELETE FROM error_log"
	var clauses []string
	var args []any
	if q.Category != "" {
		clauses = append(clauses, "category = ?")
		args = append(args, q.Category)
	}
	if q.JobID != "" {
		clauses = append(clauses, "job_id = ?")
		args = append(args, q.JobID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
