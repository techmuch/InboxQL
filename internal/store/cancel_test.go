package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A query that nobody is waiting for must stop.
//
// # The failure this exists to catch
//
// Every read used db.Query, which ignores context. Closing a browser tab
// returned from the HTTP handler and left the statement running inside SQLite
// to completion — holding one of eight pool connections and a core the whole
// time. On a real mailbox the worst query took 11.9 minutes, so a handful of
// abandoned ones emptied the pool and every later query waited before it even
// started.
//
// mattn/go-sqlite3 turns a cancelled context into sqlite3_interrupt, but only
// when the statement was started with one.
func TestACancelledQueryStopsRunning(t *testing.T) {
	openQueryFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already given up on, before it starts

	_, err := RunQueryContext(ctx, "folder:inbox", 50, 0)
	if err == nil {
		t.Fatal("a cancelled query reported success")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// A deadline that expires mid-flight is the same promise as an outright
// cancel, and is what an HTTP server's write timeout turns into.
func TestAQueryStopsWhenItsDeadlinePasses(t *testing.T) {
	openQueryFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	if _, err := RunQueryContext(ctx, "folder:inbox", 50, 0); err == nil {
		t.Fatal("a query past its deadline reported success")
	}
}

// The uncancellable entry point still works, because a CLI command has no
// context to give and should not be made to invent one.
func TestRunQueryStillWorksWithoutAContext(t *testing.T) {
	openQueryFixture(t)

	res, err := RunQuery("folder:inbox", 50, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if res.Kind != "messages" {
		t.Errorf("kind = %q, want messages", res.Kind)
	}
}
