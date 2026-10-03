package query

import (
	"strings"
	"testing"
)

// compileLog plans a log query and returns its SQL.
func compileLog(src string) (string, error) {
	q, err := Parse(src)
	if err != nil {
		return "", err
	}
	plan, err := Build(q, Options{}, "l.id")
	if err != nil {
		return "", err
	}
	return plan.SQL, nil
}

// The log is a kind, like contacts and files, so `in:logs` picks it as the row
// source and every other field resolves against it.
func TestInLogsSelectsTheLog(t *testing.T) {
	q, err := Parse("in:logs level:error")
	if err != nil {
		t.Fatal(err)
	}
	if got := q.Entity(); got != EntityLog {
		t.Errorf("entity = %q, want %q", got, EntityLog)
	}
}

// Levels are ordered, so `level>warn` is "worse than a warning" — the query
// somebody wants when something has gone wrong. Ranked in SQL rather than
// expanded into a list of the levels above, so adding a level later cannot
// leave the comparison behind.
func TestLevelComparesByseverity(t *testing.T) {
	for _, c := range []struct{ src, op string }{
		{"in:logs level>warn", ">"},
		{"in:logs level>=warn", ">="},
		{"in:logs level<error", "<"},
		{"in:logs level<=info", "<="},
		{"in:logs level:error", "="},
	} {
		sql, err := compileLog(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if !strings.Contains(sql, "CASE COALESCE(l.level") {
			t.Errorf("%s did not rank the level: %s", c.src, sql)
		}
		if !strings.Contains(sql, c.op+" ?") {
			t.Errorf("%s did not compile %q: %s", c.src, c.op, sql)
		}
	}
}

// A row written before levels existed is an error, because that is the only
// thing the table ever held. Defaulting it to anything else would rewrite
// history to make a new column look populated.
func TestOldRowsReadAsErrors(t *testing.T) {
	sql, err := compileLog("in:logs level:error")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "COALESCE(l.level, 'error')") {
		t.Errorf("a row with no level does not read as an error: %s", sql)
	}
}

func TestUnknownLevelIsRejected(t *testing.T) {
	_, err := compileLog("in:logs level:verbose")
	if err == nil {
		t.Fatal("an invented level was accepted")
	}
	if !strings.Contains(err.Error(), "debug") {
		t.Errorf("the error does not say what the levels are: %v", err)
	}
}

// A field that belongs to mail does not quietly resolve against the log. The
// log has few fields on purpose, and silently matching nothing would be worse
// than saying so.
func TestAMailFieldOnTheLogIsRefused(t *testing.T) {
	_, err := compileLog("in:logs subject:invoice")
	if err == nil {
		t.Fatal("subject: was accepted on a log query")
	}
	if !strings.Contains(err.Error(), "level") {
		t.Errorf("the error does not say what is available: %v", err)
	}
}

// Dates work as everywhere else, against the line's own time rather than any
// message's.
func TestLogDatesUseTheLinesOwnTime(t *testing.T) {
	sql, err := compileLog("in:logs after:2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "l.created_at >=") {
		t.Errorf("after: did not use the log's time: %s", sql)
	}
	if strings.Contains(sql, "m.date") {
		t.Errorf("after: reached for a message date: %s", sql)
	}
}

// The two questions a log is actually asked.
func TestLogGrouping(t *testing.T) {
	for _, c := range []struct{ src, expr string }{
		{"in:logs | count by level", "COALESCE(l.level, 'error')"},
		{"in:logs | count by category", "l.category"},
		{"in:logs | count by job", "l.job_id"},
		{"in:logs | count by hour", "l.created_at"},
	} {
		sql, err := compileLog(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if !strings.Contains(sql, c.expr) {
			t.Errorf("%s grouped on something else: %s", c.src, sql)
		}
		if !strings.Contains(sql, "FROM error_log l") {
			t.Errorf("%s did not read the log: %s", c.src, sql)
		}
	}
}

func TestLogRejectsAGroupingItCannotDo(t *testing.T) {
	_, err := compileLog("in:logs | count by domain")
	if err == nil {
		t.Fatal("grouping logs by domain was accepted")
	}
}

// An aggregate that reads extracted values has nothing to read here, and
// saying so beats returning an empty series.
func TestLogRejectsAnAggregateThatDoesNotApply(t *testing.T) {
	_, err := compileLog("in:logs | series x by week")
	if err == nil {
		t.Fatal("a series over logs was accepted")
	}
}
