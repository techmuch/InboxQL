// Package logging records what the application is doing, where it can be read.
//
// # Why this exists
//
// There was a table called error_log with one producer — the importer — and
// eighty-odd `log.Printf` calls scattered across nine packages going to
// stderr. So the UI's "Error log" tab showed one subsystem's failures while
// every sync, annotator run, model call and migration was invisible unless
// somebody happened to be watching a terminal.
//
// This routes both into the same place, so the question "what has this thing
// been doing" has an answer that outlives the terminal.
//
// # Two sinks, always
//
// Everything still goes to stderr. If the database were the only sink the logs
// that matter most would be exactly the ones that cannot be written: the lines
// from before it opened, and the lines about it failing. The database is the
// one you can query later; stderr is the one that always works.
//
// # Writes are batched
//
// Logging into the same SQLite the application is working in means every line
// is a write transaction — during a sync, contending with the thing being
// logged. Lines go onto a channel and a single goroutine drains them in
// batches. That makes logging cheap for the caller and turns a few hundred
// transactions into one.
//
// It also means a line can be dropped rather than block the work that produced
// it. A full buffer is reported once, as a line of its own, so the gap is
// visible rather than silent.
package logging

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/user/inboxql/internal/store"
)

// Buffer and batch sizes.
//
// The buffer is large enough that an ordinary burst — a sync logging every
// mailbox — never touches it, and small enough that a runaway loop is bounded
// by memory rather than by the disk. The flush interval is what decides how
// stale the UI's view is; a second is below noticing.
const (
	bufferSize    = 4096
	batchSize     = 256
	flushInterval = time.Second
)

// level is the floor, changed atomically at runtime.
//
// Runtime rather than a startup flag because logging is turned up *after*
// something goes wrong. A flag would mean reproducing the problem first.
var level = new(slog.LevelVar)

var (
	lines   chan *store.LoggedError
	dropped atomic.Int64
	start   sync.Once
	finish  sync.Once
	stop    chan struct{}
	done    chan struct{}
)

// Start installs the logger.
//
// Safe to call once; later calls do nothing, because a second writer would
// double every line.
func Start() {
	start.Do(func() {
		lines = make(chan *store.LoggedError, bufferSize)
		stop = make(chan struct{})
		done = make(chan struct{})
		finish = sync.Once{}

		handler := &handler{
			stderr: slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}),
		}
		slog.SetDefault(slog.New(handler))

		// The standard library's log package feeds the same handler, which is
		// what captures the eighty-odd existing call sites without touching
		// one of them. They arrive as info: the call sites say nothing about
		// severity, and guessing from the text would be wrong often enough to
		// matter.
		// Flags off so the stored message is the message, not a message with a
		// date glued to the front that every reader then has to strip. The
		// terminal still gets a timestamp; stdlogWriter writes it.
		log.SetFlags(0)
		log.SetOutput(stdlogWriter{})

		go drain()
	})
}

// Stop flushes what is buffered and stops writing.
//
// Called on shutdown. Without it the last few seconds of a run — which is
// where the interesting lines usually are — are the ones that never land.
//
// Idempotent, because the caller is a shutdown path and shutdown paths get
// reached twice: once on the normal exit and once from whatever else decided
// the process was ending. Closing an already-closed channel panics, and
// panicking while shutting down would lose the lines this exists to save.
func Stop() {
	if stop == nil {
		return
	}
	finish.Do(func() {
		close(stop)
		<-done
	})
}

// SetLevel changes the floor. Lines below it are neither stored nor printed.
func SetLevel(name string) {
	level.Set(parseLevel(name))
}

// Level reports the current floor.
func Level() string { return nameOf(level.Level()) }

// Dropped is how many lines the buffer could not take.
func Dropped() int64 { return dropped.Load() }

// handler is a slog.Handler that writes to stderr and to the store.
type handler struct {
	stderr slog.Handler
	attrs  []slog.Attr
	group  string
}

func (h *handler) Enabled(_ context.Context, l slog.Level) bool { return l >= level.Level() }

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{stderr: h.stderr.WithAttrs(attrs),
		attrs: append(append([]slog.Attr{}, h.attrs...), attrs...), group: h.group}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{stderr: h.stderr.WithGroup(name), attrs: h.attrs, group: name}
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	// stderr first, and its error is not allowed to stop the database write.
	// The two sinks fail independently because they fail for different
	// reasons, and losing both to one of them is the outcome worth avoiding.
	_ = h.stderr.Handle(ctx, r)
	enqueue(h.lineFrom(r))
	return nil
}

// lineFrom turns a record into a row.
//
// The attributes this package knows about become columns; anything else is
// appended to the message, because a log line nobody can read is worse than a
// long one and the alternative is a second table.
func (h *handler) lineFrom(r slog.Record) *store.LoggedError {
	e := &store.LoggedError{
		Level:     nameOf(r.Level),
		Category:  store.ErrorCategoryApp,
		Message:   r.Message,
		CreatedAt: r.Time,
	}
	var extra []string
	take := func(a slog.Attr) {
		switch a.Key {
		case "category":
			e.Category = a.Value.String()
		case "job", "jobId":
			e.JobID = a.Value.String()
		case "account", "accountId":
			e.AccountID = a.Value.String()
		case "context", "where":
			e.Context = a.Value.String()
		case "ref", "reference":
			e.Reference = a.Value.String()
		default:
			extra = append(extra, a.Key+"="+a.Value.String())
		}
	}
	for _, a := range h.attrs {
		take(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		take(a)
		return true
	})
	if len(extra) > 0 {
		e.Message += " " + strings.Join(extra, " ")
	}
	return e
}

// stdlogWriter feeds the standard library's log output through slog.
type stdlogWriter struct{}

func (stdlogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	// Not slog.Info: that would route back through the handler and print to
	// stderr a second time, since the standard logger's own output is already
	// going there through this writer's caller. The record is built directly
	// and only the database copy is enqueued.
	if level.Level() <= slog.LevelInfo {
		fmt.Fprintf(os.Stderr, "%s %s\n", time.Now().Format("2006/01/02 15:04:05"), msg)
		enqueue(&store.LoggedError{
			Level: store.LevelInfo, Category: store.ErrorCategoryApp,
			Message: msg, CreatedAt: time.Now(),
		})
	}
	return len(p), nil
}

// enqueue hands a line to the writer, or counts it as dropped.
//
// Never blocks. A log line is not worth stalling a sync for, and a full buffer
// already means the writer cannot keep up — waiting would make that worse.
func enqueue(e *store.LoggedError) {
	if lines == nil {
		return
	}
	select {
	case lines <- e:
	default:
		dropped.Add(1)
	}
}

// drain batches lines into the store.
func drain() {
	defer close(done)

	batch := make([]*store.LoggedError, 0, batchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if n := dropped.Swap(0); n > 0 {
			// Reported as a line of its own so the gap is visible. Without it
			// a burst that overran the buffer looks like a quiet period.
			batch = append(batch, &store.LoggedError{
				Level: store.LevelWarn, Category: store.ErrorCategoryApp,
				Message:   fmt.Sprintf("log buffer full: %d lines dropped", n),
				CreatedAt: time.Now(),
			})
		}
		if err := store.LogLines(batch); err != nil {
			// Straight to stderr: trying to log this failure through the
			// logger is how a broken database becomes an infinite loop.
			fmt.Fprintf(os.Stderr, "could not write %d log lines: %v\n", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case e := <-lines:
			batch = append(batch, e)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-stop:
			// Drain what is already queued before going. The last seconds of a
			// run are where the interesting lines usually are.
			for {
				select {
				case e := <-lines:
					batch = append(batch, e)
					if len(batch) >= batchSize {
						flush()
					}
					continue
				default:
				}
				break
			}
			flush()
			return
		}
	}
}

func parseLevel(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case store.LevelDebug:
		return slog.LevelDebug
	case store.LevelWarn:
		return slog.LevelWarn
	case store.LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func nameOf(l slog.Level) string {
	switch {
	case l <= slog.LevelDebug:
		return store.LevelDebug
	case l >= slog.LevelError:
		return store.LevelError
	case l >= slog.LevelWarn:
		return store.LevelWarn
	default:
		return store.LevelInfo
	}
}

// reset returns the package to its unstarted state.
//
// Only tests call this. The state is process-wide by design — one default
// logger, one buffer — and a test that shared another's writer would pass
// against a broken package, so the Once is reset rather than worked around.
func reset() {
	Stop()
	start, finish = sync.Once{}, sync.Once{}
	lines, stop, done = nil, nil, nil
	dropped.Store(0)
}

// resetStopOnly restarts the writer after a flush, leaving what was written.
func resetStopOnly() {
	start, finish = sync.Once{}, sync.Once{}
	lines, stop, done = nil, nil, nil
	Start()
}
