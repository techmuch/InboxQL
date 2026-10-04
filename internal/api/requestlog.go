package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/user/inboxql/internal/store"
)

// # Requests, which were invisible
//
// Nothing recorded that a request had happened. That is the backbone of "what
// has this thing been doing": it catches an interface polling too hard, and it
// is where sixty queries behind one request would have shown up as a slow
// listing rather than as nothing at all.
//
// Off by default, because it is the noisiest category by an order of magnitude
// and the least often what somebody turning logging up is looking for. The
// level is a floor; this is an allow-list on top of it, so `debug` can be used
// to chase one thing without ten thousand request lines burying it.

// LogRequests wraps a handler, recording each request and how long it took.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := store.LoadLogSettings()
		if !cfg.Categories["http"] {
			next.ServeHTTP(w, r)
			return
		}

		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		ms := time.Since(started).Milliseconds()

		// The path without its query string. A query string on this API carries
		// the user's search terms, and a request log is not where those should
		// arrive by accident — `logQuery` records them deliberately, behind a
		// setting that says so.
		path := r.URL.Path

		switch {
		case rec.status >= 500:
			slog.Error("request failed",
				"category", "http", "ms", ms, "ref", r.Method+" "+path,
				"status", rec.status)
		case cfg.SlowMs > 0 && ms >= int64(cfg.SlowMs):
			slog.Warn("slow request",
				"category", "http", "ms", ms, "ref", r.Method+" "+path,
				"status", rec.status)
		default:
			slog.Debug("request",
				"category", "http", "ms", ms, "ref", r.Method+" "+path,
				"status", rec.status)
		}
	})
}

// statusRecorder remembers the status a handler wrote.
//
// Needed because http.ResponseWriter does not expose it, and a request log that
// cannot say whether a request succeeded is half a log.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.written {
		r.status, r.written = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}

// Flush keeps the streaming endpoints streaming.
//
// Without it the wrapper swallows http.Flusher and every SSE endpoint buffers
// until it closes — which is the whole command channel and every job's
// progress, broken by a logging decorator.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// quiet reports paths not worth a line each.
//
// Unused today and kept as the obvious next lever: the heartbeat is every ten
// seconds per window and says nothing when it succeeds.
func quiet(path string) bool {
	return strings.HasSuffix(path, "/state")
}
