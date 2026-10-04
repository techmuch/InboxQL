package store

import (
	"database/sql"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Categories name the subsystem a line came from.
//
// `app` is where everything that has not been given a category of its own
// lands — which is most of it, because the standard library's log package is
// routed here and its call sites say nothing about where they are.
const (
	ErrorCategoryImport = "import"
	ErrorCategoryApp    = "app"
)

// Levels, lowest first. The strings are what `level:` matches and what is
// stored, so they are lowercase and short.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// Levels in order of severity, for comparison and for help.
var Levels = []string{LevelDebug, LevelInfo, LevelWarn, LevelError}

// LevelRank orders a level for `level>warn`. An unknown one sorts with info,
// which is where an unrecognised line is least likely to be either hidden or
// alarming.
func LevelRank(level string) int {
	switch strings.ToLower(level) {
	case LevelDebug:
		return 0
	case LevelWarn:
		return 2
	case LevelError:
		return 3
	}
	return 1
}

// LoggedError is one recorded failure.
//
// Deliberately not tied to import: an error worth showing a person is worth
// recording the same way wherever it came from.
type LoggedError struct {
	ID string `json:"id"`
	// Level is how bad this is: debug, info, warn or error.
	Level     string `json:"level"`
	Category  string `json:"category"`
	JobID     string `json:"jobId,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	// Context is where it happened — a mailbox path, say.
	Context string `json:"context,omitempty"`
	// Reference identifies the specific item, e.g. the file that would not parse.
	Reference string `json:"reference,omitempty"`
	Message   string `json:"message"`
	// Duration is how long the thing took, in milliseconds, for the lines that
	// are about something taking time.
	//
	// A pointer so that "not about a duration" and "took no time" stay
	// different. `duration>0` should not match a migration notice, and a
	// default of zero would have every row claiming to be instantaneous.
	Duration  *int64    `json:"durationMs,omitempty"`
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
	if e.Level == "" {
		// Everything written through this function before there were levels
		// was an error, and a caller that still does not say means the same
		// thing it always did.
		e.Level = LevelError
	}
	e.Message = sanitiseForLog(e.Message)
	e.Reference = sanitiseForLog(e.Reference)
	e.Context = sanitiseForLog(e.Context)

	_, err := db.Exec(`
		INSERT INTO error_log (id, level, category, job_id, account_id, context, reference, message, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.Level, e.Category, nullIfEmpty(e.JobID), nullIfEmpty(e.AccountID),
		nullIfEmpty(e.Context), nullIfEmpty(e.Reference), e.Message, e.Duration, e.CreatedAt.UnixMilli())
	return err
}

// LogLines records a batch in one transaction.
//
// The batch is the point. Logging into the same database the application is
// working in means every line is a write, and during a sync that contends with
// the thing being logged. One transaction for a few hundred lines costs about
// what one line did.
func LogLines(lines []*LoggedError) error {
	if len(lines) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO error_log (id, level, category, job_id, account_id, context, reference, message, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range lines {
		if e.ID == "" {
			e.ID = uuid.New().String()
		}
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now()
		}
		if e.Level == "" {
			e.Level = LevelInfo
		}
		if _, err := stmt.Exec(e.ID, e.Level, e.Category,
			nullIfEmpty(e.JobID), nullIfEmpty(e.AccountID),
			nullIfEmpty(e.Context), nullIfEmpty(e.Reference),
			sanitiseForLog(e.Message), e.Duration, e.CreatedAt.UnixMilli()); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	// MinLevel keeps lines at this level and above. Empty means everything.
	MinLevel string
	// MinDuration keeps lines that took at least this many milliseconds. A
	// line with no duration never matches: a migration notice is not a fast
	// query, and sweeping the untimed ones in would make the number useless
	// for the question it answers.
	MinDuration int
	Limit       int
	Offset      int
}

// clauses builds the shared WHERE for listing, counting and clearing, so the
// three cannot come to differ about what a filter means — which is how a UI
// ends up clearing more than it was showing.
func (q ErrorQuery) clauses() ([]string, []any) {
	var where []string
	var args []any
	if q.Category != "" {
		where = append(where, "category = ?")
		args = append(args, q.Category)
	}
	if q.JobID != "" {
		where = append(where, "job_id = ?")
		args = append(args, q.JobID)
	}
	if q.MinDuration > 0 {
		where = append(where, "duration_ms IS NOT NULL AND duration_ms >= ?")
		args = append(args, q.MinDuration)
	}
	if q.MinLevel != "" {
		// Ranked in SQL rather than by listing the levels above this one, so
		// adding a level later does not silently leave this behind.
		where = append(where,
			`CASE COALESCE(level, 'error')
			   WHEN 'debug' THEN 0 WHEN 'warn' THEN 2 WHEN 'error' THEN 3 ELSE 1 END >= ?`)
		args = append(args, LevelRank(q.MinLevel))
	}
	return where, args
}

const errorColumns = `id, COALESCE(level, 'error'), category, job_id, account_id, context, reference, message, duration_ms, created_at`

// ListErrors returns logged errors, newest first.
func ListErrors(q ErrorQuery) ([]*LoggedError, error) {
	query := "SELECT " + errorColumns + " FROM error_log"
	where, args := q.clauses()
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
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
		var duration sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Level, &e.Category, &jobID, &accountID,
			&context, &reference, &e.Message, &duration, &created); err != nil {
			return nil, err
		}
		e.JobID = jobID.String
		e.AccountID = accountID.String
		e.Context = context.String
		e.Reference = reference.String
		if duration.Valid {
			d := duration.Int64
			e.Duration = &d
		}
		e.CreatedAt = time.UnixMilli(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountErrors totals the log under a filter, so a UI can page without
// fetching everything.
func CountErrors(q ErrorQuery) (int, error) {
	query := "SELECT COUNT(*) FROM error_log"
	where, args := q.clauses()
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var n int
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

// ClearErrors removes logged errors under a filter, returning how many went.
func ClearErrors(q ErrorQuery) (int64, error) {
	query := "DELETE FROM error_log"
	where, args := q.clauses()
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MaxLogLines caps the log.
//
// Debug-logging a sync writes thousands of lines a minute into the same
// database the mail is in, so an uncapped log is a disk-filling bug waiting for
// somebody to turn the level up and forget. Fifty thousand is a few days of
// ordinary use and a few hours of debug, which is the shape that matters:
// enough to answer "what happened last week" and not enough to be a problem.
const MaxLogLines = 50_000

// PruneLog trims the log to the newest n lines, returning how many went.
//
// By row count rather than by age. An idle week should not erase the record of
// the busy one before it, and a cap is also the thing that bounds the disk —
// which is what this is for.
//
// Human-facing failures are not special-cased. A log where errors are kept
// forever and their surrounding context is not is a log that shows a failure
// with nothing around it, which is the half that was never the useful half.
func PruneLog(keep int) (int64, error) {
	if keep <= 0 {
		keep = MaxLogLines
	}
	res, err := db.Exec(`
		DELETE FROM error_log
		WHERE id NOT IN (
			SELECT id FROM error_log ORDER BY created_at DESC, rowid DESC LIMIT ?
		)`, keep)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountLog is how many lines are stored.
func CountLog() (int64, error) {
	var n int64
	err := db.QueryRow("SELECT COUNT(*) FROM error_log").Scan(&n)
	return n, err
}
