package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// RawResult is the outcome of an ad-hoc SQL query.
type RawResult struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	// Truncated reports that more rows matched than were returned.
	Truncated bool          `json:"truncated"`
	Elapsed   time.Duration `json:"-"`
}

// RunRawSQL executes a read-only statement against a second, read-only
// connection to the same database.
//
// # Why this exists
//
// The query language will never cover everything, and pretending otherwise
// produces a language that grows a feature per question. This is the escape
// hatch: whatever it cannot express, write SQL. It also doubles as the way to
// find out what the language should learn next — the queries people write here
// are the backlog.
//
// # Why read-only is enforced twice
//
// The connection is opened with mode=ro and query_only, so the enforcement is
// SQLite's rather than a check on the string. A statement prefix check alone
// is not a safety property: `WITH x AS (...) DELETE ...` starts with WITH, and
// a blocklist of keywords is a guess about a grammar rather than a rule. The
// prefix check below is kept only to produce a clearer error than SQLite's.
func RunRawSQL(dataDir, statement string, limit int) (*RawResult, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(statement), ";"))
	if trimmed == "" {
		return nil, fmt.Errorf("no statement given")
	}
	if !looksReadOnly(trimmed) {
		return nil, fmt.Errorf("only SELECT, WITH and EXPLAIN statements are allowed here; " +
			"this connection is read-only, so a write would fail anyway")
	}
	if limit <= 0 {
		limit = 200
	}

	dbPath := filepath.Join(dataDir, DBNAME)
	dsn := "file:" + url.PathEscape(dbPath) + "?mode=ro&_busy_timeout=5000&_query_only=1"
	ro, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	defer ro.Close()
	ro.SetMaxOpenConns(1)

	started := time.Now()
	rows, err := ro.Query(trimmed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	out := &RawResult{Columns: cols, Rows: [][]any{}}
	for rows.Next() {
		if len(out.Rows) >= limit {
			out.Truncated = true
			break
		}
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, c := range cells {
			// []byte reaches JSON as base64, which is unreadable for the text
			// columns this mostly returns.
			if b, ok := c.([]byte); ok {
				cells[i] = string(b)
			}
		}
		out.Rows = append(out.Rows, cells)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.Elapsed = time.Since(started)
	return out, nil
}

// looksReadOnly rejects the obvious writes with a useful message.
func looksReadOnly(statement string) bool {
	head := strings.ToLower(strings.Fields(statement)[0])
	switch head {
	case "select", "with", "explain", "pragma", "values":
		return true
	}
	return false
}

// SchemaOverview lists the tables and views available to RunRawSQL.
//
// The point of an escape hatch nobody can navigate is limited, and `.schema`
// is not available without the sqlite3 binary.
func SchemaOverview() (*RawResult, error) {
	rows, err := db.Query(`
		SELECT type, name, COALESCE(sql, '')
		FROM sqlite_master
		WHERE type IN ('table', 'view')
		  AND name NOT LIKE 'sqlite_%'
		  AND name NOT LIKE '%_fts_%'
		  AND name NOT LIKE '%_fts'
		ORDER BY type, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := &RawResult{Columns: []string{"type", "name", "sql"}, Rows: [][]any{}}
	for rows.Next() {
		var kind, name, ddl string
		if err := rows.Scan(&kind, &name, &ddl); err != nil {
			return nil, err
		}
		out.Rows = append(out.Rows, []any{kind, name, ddl})
	}
	return out, rows.Err()
}
