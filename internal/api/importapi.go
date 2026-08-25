package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/user/inboxql/internal/importer"
	"github.com/user/inboxql/internal/importer/applemail"
	"github.com/user/inboxql/internal/store"
)

// importManager is created on first use, once the data directory is known.
var (
	importManagerOnce sync.Once
	importManager     *importer.Manager
	importDataDir     string
)

// SetDataDir tells the API where the data directory is, for the blob store.
// Called by `iql start` before the listener starts.
func SetDataDir(dir string) { importDataDir = dir }

func manager() *importer.Manager {
	importManagerOnce.Do(func() { importManager = importer.NewManager(importDataDir) })
	return importManager
}

// knownSources is the whole registry the API will address.
//
// Deliberately a fixed list built server-side. No endpoint takes a filesystem
// path from the client: a handler that read `{"path": "..."}` would let any
// page on localhost read arbitrary files as the InboxQL user. Clients choose a
// source by id from this list, and mailbox ids are opaque handles this server
// minted during discovery.
func knownSources() []importer.Source {
	return []importer.Source{applemail.New()}
}

func sourceByID(id string) importer.Source {
	for _, s := range knownSources() {
		if s.ID() == id {
			return s
		}
	}
	return nil
}

// registerImportRoutes wires the import API onto an authenticated mux.
func registerImportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/import/sources", handleImportSources)
	mux.HandleFunc("GET /api/import/sources/{id}/mailboxes", handleImportMailboxes)
	mux.HandleFunc("POST /api/import/sources/{id}/scan", handleImportScan)
	mux.HandleFunc("GET /api/import/jobs", handleImportJobList)
	mux.HandleFunc("POST /api/import/jobs", handleImportJobCreate)
	mux.HandleFunc("GET /api/import/jobs/{id}", handleImportJobGet)
	mux.HandleFunc("GET /api/import/jobs/{id}/events", handleImportJobEvents)
	mux.HandleFunc("POST /api/import/jobs/{id}/cancel", handleImportJobCancel)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf(format, args...)})
}

func handleImportSources(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		importer.Detection
	}
	var out []entry
	for _, s := range knownSources() {
		out = append(out, entry{ID: s.ID(), Name: s.Name(), Detection: s.Detect()})
	}
	writeJSON(w, http.StatusOK, out)
}

func handleImportMailboxes(w http.ResponseWriter, r *http.Request) {
	src := sourceByID(r.PathValue("id"))
	if src == nil {
		writeError(w, http.StatusNotFound, "unknown source")
		return
	}

	d := src.Detect()
	if !d.Readable {
		// The permission case is not a server error and should not read like
		// one: the client needs the remedy, not a 500.
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     d.Detail,
			"remedy":    d.Remedy,
			"available": d.Available,
		})
		return
	}

	boxes, err := src.Mailboxes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source": src.ID(), "root": d.Root, "mailboxes": boxes,
	})
}

func handleImportScan(w http.ResponseWriter, r *http.Request) {
	src := sourceByID(r.PathValue("id"))
	if src == nil {
		writeError(w, http.StatusNotFound, "unknown source")
		return
	}

	var req struct {
		MailboxIDs []string `json:"mailboxIds"`
		Deep       bool     `json:"deep"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.MailboxIDs) == 0 {
		writeError(w, http.StatusBadRequest, "mailboxIds is required")
		return
	}

	depth := importer.ScanFast
	if req.Deep {
		depth = importer.ScanDeep
	}

	// A deep scan can run for minutes; the request context carries the client
	// disconnecting, so abandoning the page stops the work.
	var out []importer.Stats
	for _, id := range req.MailboxIDs {
		stats, err := src.Scan(r.Context(), id, depth, nil)
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			writeError(w, http.StatusInternalServerError, "%v", err)
			return
		}
		out = append(out, stats)
	}
	writeJSON(w, http.StatusOK, out)
}

func handleImportJobCreate(w http.ResponseWriter, r *http.Request) {
	var req importer.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	src := sourceByID(req.SourceID)
	if src == nil {
		writeError(w, http.StatusBadRequest, "unknown source %q", req.SourceID)
		return
	}
	if req.MaxAttachMB <= 0 {
		req.MaxAttachMB = 25
	}

	job, err := manager().Start(src, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func handleImportJobList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	jobs, err := store.ListImportJobs(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if jobs == nil {
		jobs = []*store.ImportJob{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func handleImportJobGet(w http.ResponseWriter, r *http.Request) {
	job, err := store.GetImportJob(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if job == nil {
		writeError(w, http.StatusNotFound, "no such job")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func handleImportJobCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if manager().Cancel(id) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
		return
	}
	// Not running: either finished already or never existed here. Report the
	// stored state rather than inventing an error.
	job, err := store.GetImportJob(id)
	if err != nil || job == nil {
		writeError(w, http.StatusNotFound, "no such job")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": job.Status})
}

// handleImportJobEvents streams job snapshots as Server-Sent Events.
//
// SSE rather than websockets because progress is one-way and this is the
// mechanism requirements.md 2.3 already specifies for the AI gateway — one
// streaming transport in the codebase, not two.
func handleImportJobEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported here")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Proxies that buffer would defeat the point of streaming.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, unsubscribe := manager().Subscribe(r.PathValue("id"))
	defer unsubscribe()

	send := func(job *store.ImportJob) bool {
		payload, err := json.Marshal(job)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case job, open := <-events:
			if !open {
				// The job finished. Send its final persisted state so a client
				// that connected late still learns the outcome.
				if final, err := store.GetImportJob(r.PathValue("id")); err == nil && final != nil {
					send(final)
				}
				fmt.Fprint(w, "event: done\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			if !send(job) {
				return
			}
		}
	}
}
