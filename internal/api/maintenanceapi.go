package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/user/inboxql/internal/health"
	"github.com/user/inboxql/internal/maintenance"
	"github.com/user/inboxql/internal/store"
)

// The maintenance manager, created on first use once the data directory is
// known — the same shape as the import manager beside it.
var (
	maintenanceOnce    sync.Once
	maintenanceManager *maintenance.Manager
)

func maintenanceJobs() *maintenance.Manager {
	maintenanceOnce.Do(func() { maintenanceManager = maintenance.NewManager(importDataDir) })
	return maintenanceManager
}

// registerMaintenanceRoutes wires the health and job surface.
func registerMaintenanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", handleHealth)
	mux.HandleFunc("GET /api/maintenance/jobs", handleMaintenanceList)
	mux.HandleFunc("POST /api/maintenance/jobs", handleMaintenanceStart)
	mux.HandleFunc("GET /api/maintenance/jobs/{id}", handleMaintenanceGet)
	mux.HandleFunc("GET /api/maintenance/jobs/{id}/events", handleMaintenanceEvents)
	mux.HandleFunc("POST /api/maintenance/jobs/{id}/cancel", handleMaintenanceCancel)
}

// handleHealth runs the same checks `iql doctor` runs.
//
// The store is already open — the server opened it at startup — so no
// OpenStore is supplied. Network checks are skipped by default because this is
// a page someone is waiting on, and probing every IMAP server takes seconds
// each; `?network=1` asks for them.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	rep := health.Run(health.Options{
		DataDir:     importDataDir,
		DBPath:      filepath.Join(importDataDir, store.DBNAME),
		SkipNetwork: r.URL.Query().Get("network") == "",
	})
	writeJSON(w, http.StatusOK, rep)
}

func handleMaintenanceList(w http.ResponseWriter, r *http.Request) {
	jobs := maintenanceJobs().List()
	if jobs == nil {
		jobs = []*maintenance.Job{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

// handleMaintenanceStart begins a job.
//
// The consent flag rides on the request rather than being inferred, because
// the decision it records — that the contents of this mailbox may be sent to a
// model that is not on this machine — is the user's to make and must be made
// before the work starts, not discovered after it.
func handleMaintenanceStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind        string `json:"kind"`
		AllowRemote bool   `json:"allowRemote"`
		Limit       int    `json:"limit"`
		Redo        bool   `json:"redo"`
		Profile     string `json:"profile"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	job, err := maintenanceJobs().Start(req.Kind, maintenance.Options{
		AllowRemote: req.AllowRemote,
		Limit:       req.Limit,
		Redo:        req.Redo,
		Profile:     req.Profile,
	})
	if err != nil {
		var running *maintenance.ErrAlreadyRunning
		if errors.As(err, &running) {
			// Conflict rather than an error: the thing the caller wanted is
			// already happening, and the id lets them watch it instead.
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": err.Error(), "jobId": running.JobID,
			})
			return
		}
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func handleMaintenanceGet(w http.ResponseWriter, r *http.Request) {
	job := maintenanceJobs().Get(r.PathValue("id"))
	if job == nil {
		writeError(w, http.StatusNotFound, "no such job")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func handleMaintenanceCancel(w http.ResponseWriter, r *http.Request) {
	if maintenanceJobs().Cancel(r.PathValue("id")) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
		return
	}
	job := maintenanceJobs().Get(r.PathValue("id"))
	if job == nil {
		writeError(w, http.StatusNotFound, "no such job")
		return
	}
	// Already finished. Report the state rather than inventing an error.
	writeJSON(w, http.StatusOK, map[string]string{"status": job.Status})
}

// handleMaintenanceEvents streams job snapshots as Server-Sent Events.
//
// SSE for the same reason the importer uses it: progress is one-way, and one
// streaming mechanism in the codebase is better than two.
func handleMaintenanceEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported here")
		return
	}

	updates, stop := maintenanceJobs().Subscribe(r.PathValue("id"))
	defer stop()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Proxies that buffer would defeat the point of streaming.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case job, open := <-updates:
			if !open {
				return
			}
			payload, err := json.Marshal(job)
			if err != nil {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
			if !job.Running() {
				return
			}
		}
	}
}
