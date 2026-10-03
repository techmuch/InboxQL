package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/user/inboxql/internal/uisession"
)

// # The windows, over HTTP
//
// SSE down, POST up. One-way server-to-client is exactly the shape of a command
// channel, there are already three SSE endpoints here, and it needs no new
// dependency and no second authentication path.
//
// Everything is behind the same middleware as the rest of the API, which is
// what keeps a page on another origin from watching — or driving — somebody's
// mail client. That matters more here than elsewhere: the other endpoints read
// mail, and this one moves the screen in front of a person.

var sessions = uisession.New()

// Sessions is the registry, for the CLI and for tests.
func Sessions() *uisession.Registry { return sessions }

func registerUIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/ui/register", handleUIRegister)
	mux.HandleFunc("POST /api/ui/{name}/state", handleUIState)
	mux.HandleFunc("DELETE /api/ui/{name}", handleUIForget)
	mux.HandleFunc("GET /api/ui/{name}/events", handleUIEvents)
	mux.HandleFunc("GET /api/ui", handleUIList)
	mux.HandleFunc("POST /api/ui/{name}/command", handleUICommand)
}

// handleUIRegister gives a window its name.
func handleUIRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	// A body is optional: a window with nothing to say about itself still gets
	// a name.
	_ = json.NewDecoder(r.Body).Decode(&req)

	s := sessions.Register(req.Title)
	writeJSON(w, http.StatusOK, map[string]any{
		"name":        s.Name,
		"heartbeatMs": uisession.Heartbeat.Milliseconds(),
		"expiryMs":    uisession.Expiry.Milliseconds(),
	})
}

// handleUIState is the heartbeat, carrying what the window is showing.
//
// The two are one request because they answer the same question — is this
// window still there, and what is on it — and splitting them would mean a
// window could be alive while the listing showed what it was doing a minute
// ago.
func handleUIState(w http.ResponseWriter, r *http.Request) {
	var showing uisession.Showing
	if err := decodeJSON(w, r, &showing); err != nil {
		return
	}
	if !sessions.Touch(r.PathValue("name"), showing) {
		// Gone from the registry — expired while the laptop slept, or the
		// server restarted. 404 so the client registers again rather than
		// heartbeating forever into nothing.
		writeError(w, http.StatusNotFound, "this window is not registered")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleUIForget drops a window that closed cleanly, so the listing does not
// have to wait for it to expire.
func handleUIForget(w http.ResponseWriter, r *http.Request) {
	sessions.Forget(r.PathValue("name"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleUIList says who is connected and what each is showing.
func handleUIList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"windows": sessions.List()})
}

// handleUICommand points one window at something.
func handleUICommand(w http.ResponseWriter, r *http.Request) {
	var c uisession.Command
	if err := decodeJSON(w, r, &c); err != nil {
		return
	}
	switch c.Verb {
	case "query", "open", "notice":
	default:
		writeError(w, http.StatusBadRequest,
			"unknown verb %q (query, open or notice)", c.Verb)
		return
	}
	if err := sessions.Send(r.PathValue("name"), c); err != nil {
		// Not found rather than a generic failure: the overwhelmingly common
		// cause is a window that has closed, and saying so is the difference
		// between "try another one" and "something is broken".
		writeError(w, http.StatusNotFound, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

// handleUIEvents is a window's command channel.
func handleUIEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported here")
		return
	}

	commands, disconnect, ok := sessions.Connect(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "this window is not registered")
		return
	}
	defer disconnect()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// A comment line immediately, so the browser's EventSource reports the
	// connection open rather than waiting for the first command — which may be
	// hours away, and until then the indicator would say disconnected while it
	// was not.
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	// Keepalives, because an idle stream through any intermediary is a stream
	// that gets closed, and a command channel that quietly dies is worse than
	// one that was never there.
	ticker := time.NewTicker(uisession.Heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case c := <-commands:
			payload, err := json.Marshal(c)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}
