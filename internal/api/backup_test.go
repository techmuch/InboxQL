package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/backups"
	"github.com/user/inboxql/internal/store"
)

func backupCall(t *testing.T, method, path, body string, out any) int {
	t.Helper()
	mux := http.NewServeMux()
	registerBackupRoutes(mux)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if out != nil {
		json.Unmarshal(rr.Body.Bytes(), out)
	}
	return rr.Code
}

func TestBackupsOverHTTP(t *testing.T) {
	dir := t.TempDir()
	if _, err := store.InitDB(dir); err != nil {
		t.Fatal(err)
	}
	defer store.CloseDB()
	restarted := make(chan bool, 1)
	defer SetRuntime(getRuntime())
	SetRuntime(Runtime{DataDir: dir, Restart: func() error { restarted <- true; return nil }})

	for _, r := range []string{backups.ReasonUpdate, backups.ReasonManual} {
		if _, err := backups.Make(backups.Options{DataDir: dir, Reason: r, Version: "0.0.99"}); err != nil {
			t.Fatal(err)
		}
	}

	var listed struct {
		Backups    []backups.Backup `json:"backups"`
		FreeBytes  int64            `json:"freeBytes"`
		NeedBytes  int64            `json:"needBytes"`
		Policy     backups.Policy   `json:"policy"`
		CanRestart bool             `json:"canRestart"`
	}
	if code := backupCall(t, "GET", "/api/backups", "", &listed); code != 200 {
		t.Fatalf("list: %d", code)
	}
	if len(listed.Backups) != 2 || listed.FreeBytes == 0 || listed.NeedBytes == 0 || !listed.CanRestart ||
		listed.Policy.KeepUpdate != 2 {
		t.Fatalf("listed = %+v", listed)
	}

	// Keep rules: one of a kind at least, and a schedule that exists.
	if code := backupCall(t, "PUT", "/api/backups/policy", `{"keepUpdate":0,"keepScheduled":2,"keepRestore":2,"schedule":"off"}`, nil); code != 400 {
		t.Errorf("keeping none: %d, want 400", code)
	}
	var p backups.Policy
	if code := backupCall(t, "PUT", "/api/backups/policy", `{"keepUpdate":3,"keepScheduled":1,"keepRestore":2,"schedule":"weekly"}`, &p); code != 200 || p.Schedule != "weekly" || p.KeepUpdate != 3 {
		t.Errorf("saving the policy: %d %+v", code, p)
	}

	// Restore: staged, answered, then the server restarts to carry it out.
	mine := listed.Backups[0].Name
	var staged map[string]any
	if code := backupCall(t, "POST", "/api/backups/restore", `{"name":"`+mine+`"}`, &staged); code != 202 {
		t.Fatalf("restore: %d %v", code, staged)
	}
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("the server was not restarted to restore")
	}
	if backups.Staged(dir) == "" {
		t.Fatal("nothing staged")
	}
	// A backup waiting to be restored is not deleted from under it.
	if code := backupCall(t, "DELETE", "/api/backups?name="+mine, "", nil); code != 409 {
		t.Errorf("deleting the staged backup: %d, want 409", code)
	}
	if code := backupCall(t, "DELETE", "/api/backups/restore", "", nil); code != 200 || backups.Staged(dir) != "" {
		t.Errorf("cancelling the restore: %d", code)
	}
	if code := backupCall(t, "DELETE", "/api/backups?name="+mine, "", nil); code != 200 {
		t.Errorf("delete: %d", code)
	}
	if code := backupCall(t, "DELETE", "/api/backups?name=nothing-like-it", "", nil); code != 404 {
		t.Errorf("deleting what is not there: %d", code)
	}

	var pruned map[string]any
	if code := backupCall(t, "POST", "/api/backups/prune?dryRun=1", "", &pruned); code != 200 || pruned["dryRun"] != true {
		t.Errorf("prune: %d %v", code, pruned)
	}

	// No restart, no restore from the page: say how instead.
	SetRuntime(Runtime{DataDir: dir})
	if code := backupCall(t, "POST", "/api/backups/restore", `{"name":"inboxql-pre"}`, nil); code != 501 {
		t.Errorf("restore without restart: %d, want 501", code)
	}
}
