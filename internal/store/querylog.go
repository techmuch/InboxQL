package store

import (
	"log/slog"
	"strings"
	"time"
)

// # Timing every query
//
// # Why not simply log them all
//
// A page load fires several, the Windows panel polls every five seconds, and an
// annotator listing runs one per annotator. At `info` that drowns the line
// worth reading; at a million messages it is thousands an hour.
//
// So every query is a `debug` line and a slow one is a `warn`. The default
// level then surfaces exactly "this took 2.2 seconds" and nothing else — which
// is a line that did not exist at any level before.
//
// # Why the SQL only on the slow ones
//
// "Took 2.2 s" says something is wrong. The statement says what, because it can
// be pasted into `iql sql --explain`. On every row it is bloat; above the
// threshold it is the reason the row is there.

// logQuery records one query and how long it took.
//
// Called from the one place every query goes through, so there is no second
// path that quietly goes untimed.
func logQuery(src, sql string, elapsed time.Duration, rows int, err error) {
	cfg := LoadLogSettings()
	if !cfg.Categories["query"] {
		return
	}

	ms := elapsed.Milliseconds()

	// The words of a query are user content: `from:solicitor@…` is as sensitive
	// as the mail it finds, and the log outlives the query. Recorded by default
	// because this is a local database, and omitted entirely when somebody has
	// said not to — not redacted, because a redaction that leaves the shape of
	// a query is still a record of what was searched for.
	text := src
	if !cfg.QueryText {
		text = "(query text not recorded)"
	}

	switch {
	case err != nil:
		// A query that did not compile. Visible in the interface and gone the
		// moment the box is retyped, which is exactly when somebody wants to
		// know what they had.
		slog.Warn("query failed: "+err.Error(),
			"category", "query", "ms", ms, "ref", text)

	case cfg.SlowMs > 0 && ms >= int64(cfg.SlowMs):
		slog.Warn("slow query",
			"category", "query", "ms", ms, "ref", text,
			"rows", rows, "where", collapse(sql))

	default:
		slog.Debug("query",
			"category", "query", "ms", ms, "ref", text, "rows", rows)
	}
}

// collapse puts a statement on one line.
//
// The compiled SQL arrives indented across twenty lines, and a log row is read
// in a table. Whitespace is the only thing lost and it is the only thing a
// reader pasting this into `iql sql` does not need.
func collapse(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
