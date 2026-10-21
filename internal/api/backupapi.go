package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/user/inboxql/internal/backups"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/health"
	"github.com/user/inboxql/internal/maintenance"
)

// registerBackupRoutes is Settings → Data Management's backups: what there
// is, the room it takes, the keep rules, and making, checking, removing and
// restoring one. Making and checking are maintenance jobs — minutes on a
// large mailbox — and the page watches them like any other.
func registerBackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/backups", handleBackupsList)
	mux.HandleFunc("PUT /api/backups/policy", handleBackupPolicy)
	mux.HandleFunc("POST /api/backups", handleBackupStart)
	mux.HandleFunc("POST /api/backups/check", handleBackupCheck)
	mux.HandleFunc("DELETE /api/backups", handleBackupDelete)
	mux.HandleFunc("POST /api/backups/prune", handleBackupPrune)
	mux.HandleFunc("POST /api/backups/restore", handleBackupRestore)
	mux.HandleFunc("DELETE /api/backups/restore", handleBackupUnstage)
}

func backupDir() string { return getRuntime().DataDir }

func handleBackupsList(w http.ResponseWriter, _ *http.Request) {
	dir := backupDir()
	all, err := backups.List(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if all == nil {
		all = []backups.Backup{}
	}
	free, _ := backups.FreeBytes(dir)
	files, fileBytes, _ := blobstore.New(dir).Usage()
	run := getRuntime()
	writeJSON(w, http.StatusOK, map[string]any{
		"backups":         all,
		"totalBytes":      backups.Total(all),
		"freeBytes":       free,
		"databaseBytes":   backups.DatabaseBytes(dir),
		"needBytes":       backups.Need(dir, false),
		"attachmentFiles": files,
		"attachmentBytes": fileBytes,
		"policy":          backups.LoadPolicy(dir),
		"staged":          backups.Staged(dir),
		"canRestart":      run.Restart != nil,
		"jobs":            backupJobs(),
	})
}

// backupJobs are the backup jobs still running, so a page opened mid-backup
// shows it rather than offering to start another.
func backupJobs() []*maintenance.Job {
	out := []*maintenance.Job{}
	for _, j := range maintenanceJobs().List() {
		if (j.Kind == health.JobBackup || j.Kind == health.JobBackupCheck) && j.Running() {
			out = append(out, j)
		}
	}
	return out
}

func handleBackupPolicy(w http.ResponseWriter, r *http.Request) {
	var p backups.Policy
	if decodeJSON(w, r, &p) != nil {
		return
	}
	if err := backups.SavePolicy(backupDir(), p); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, backups.LoadPolicy(backupDir()))
}

func startBackupJob(w http.ResponseWriter, kind string, o maintenance.BackupOptions) {
	job, err := maintenanceJobs().Start(kind, maintenance.Options{Backup: o})
	if err != nil {
		var running *maintenance.ErrAlreadyRunning
		if errors.As(err, &running) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "jobId": running.JobID})
			return
		}
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func handleBackupStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key         bool `json:"key"`
		Attachments bool `json:"attachments"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	startBackupJob(w, health.JobBackup, maintenance.BackupOptions{Reason: backups.ReasonManual,
		Version: currentVersionInfo.Version, Key: req.Key, Attachments: req.Attachments})
}

func handleBackupCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	startBackupJob(w, health.JobBackupCheck, maintenance.BackupOptions{Name: req.Name})
}

func handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	b, err := backups.Find(backupDir(), r.URL.Query().Get("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "%v", err)
		return
	}
	if backups.Staged(backupDir()) == b.Path {
		writeError(w, http.StatusConflict, "a restore from this backup is waiting for a restart; cancel it first")
		return
	}
	if err := backups.Delete(*b); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": b.Name, "freed": b.Bytes + b.Extra})
}

func handleBackupPrune(w http.ResponseWriter, r *http.Request) {
	dry := r.URL.Query().Get("dryRun") == "1"
	gone, err := backups.Prune(backupDir(), dry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if gone == nil {
		gone = []backups.Backup{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": gone, "freed": backups.Total(gone), "dryRun": dry})
}

// handleBackupRestore stages a restore and restarts the server, which swaps
// the database in before it opens it — the one moment nothing has it open.
// The answer comes first, so the page can say what is happening and watch
// for the new instance.
func handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	run := getRuntime()
	if run.Restart == nil {
		writeError(w, http.StatusNotImplemented,
			"this server cannot restart itself: stop it, then run `iql restore %s`", req.Name)
		return
	}
	if len(backupJobs()) > 0 {
		writeError(w, http.StatusConflict, "a backup is being made or checked; restore when it has finished")
		return
	}
	b, err := backups.Find(run.DataDir, req.Name)
	if err != nil {
		writeError(w, http.StatusNotFound, "%v", err)
		return
	}
	if err := backups.Stage(run.DataDir, b.Path); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"restoring": b.Name, "restarting": true,
		"instanceId": currentVersionInfo.InstanceID})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := run.Restart(); err != nil {
			log.Printf("[backups] restart for restore failed: %v", err)
		}
	}()
}

func handleBackupUnstage(w http.ResponseWriter, _ *http.Request) {
	if err := backups.Unstage(backupDir()); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// RunBackupSchedule makes the scheduled backups while the server runs: when
// the newest backup is older than the schedule's interval, it starts one —
// unless the machine is on battery and heavy work is set to wait for mains,
// in which case it waits. A missed one is made at the next look, not lost.
func RunBackupSchedule(ctx context.Context) {
	// Not in the first minutes: a server just started is busy catching up.
	wait := time.NewTimer(3 * time.Minute)
	defer wait.Stop()
	select {
	case <-ctx.Done():
		return
	case <-wait.C:
	}
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		scheduledBackup()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func scheduledBackup() {
	dir := getRuntime().DataDir
	if dir == "" || !backups.Due(dir, time.Now()) || len(backupJobs()) > 0 {
		return
	}
	if deferHeavyWork() {
		log.Printf("[backups] scheduled backup waiting: on battery")
		return
	}
	if _, err := maintenanceJobs().Start(health.JobBackup, maintenance.Options{Backup: maintenance.BackupOptions{
		Reason: backups.ReasonScheduled, Version: currentVersionInfo.Version,
	}}); err != nil {
		log.Printf("[backups] scheduled backup: %v", err)
	}
}
