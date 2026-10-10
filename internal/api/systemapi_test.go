package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/user/inboxql/internal/machine"
	"github.com/user/inboxql/internal/store"
)

// systemTest is a server running on a fresh mailbox with no machine settings,
// the state of a checkout started with `iql start`.
func systemTest(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	t.Setenv("INBOXQL_HOME", t.TempDir())
	setupTestDB(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, store.DBNAME), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	SetRuntime(Runtime{Mode: "foreground", DataDir: dir, DataSource: "local",
		Addr: "127.0.0.1:8420", AddrSource: "default", URL: "http://localhost:8420", Started: time.Now()})
	t.Cleanup(func() { SetRuntime(Runtime{Mode: "foreground", Started: time.Now()}) })
	mux := http.NewServeMux()
	registerSystemRoutes(mux)
	return mux, dir
}

func call(t *testing.T, mux *http.ServeMux, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// The panel says which mailbox and why, and that there are no settings.
func TestSystemStateNamesTheMailboxAndWhy(t *testing.T) {
	mux, dir := systemTest(t)
	rec, out := call(t, mux, "GET", "/api/system", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	mb := out["mailbox"].(map[string]any)
	if mb["dataDir"] != dir || mb["source"] != "local" || mb["why"] == "" {
		t.Errorf("mailbox = %v", mb)
	}
	if out["run"].(map[string]any)["mode"] != "foreground" {
		t.Errorf("run = %v", out["run"])
	}
	if out["settings"].(map[string]any)["exists"] != false {
		t.Errorf("settings = %v", out["settings"])
	}
}

// Creating settings from the panel describes what is already running, so it
// leaves nothing waiting for a restart.
func TestCreateSettingsAdoptsTheRunningMailbox(t *testing.T) {
	mux, dir := systemTest(t)
	rec, out := call(t, mux, "POST", "/api/system/settings/create", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	s, found, err := machine.Load()
	if err != nil || !found || s.DataDir != dir || s.Addr != "127.0.0.1:8420" {
		t.Fatalf("settings = %+v %v %v", s, found, err)
	}
	if p := out["pendingRestart"].([]any); len(p) != 1 || p[0].(map[string]any)["field"] != "models" {
		// The models folder is the one thing settings add: a server with no
		// settings keeps models in the mailbox.
		t.Errorf("pending = %v", p)
	}
	if rec, _ := call(t, mux, "POST", "/api/system/settings/create", nil); rec.Code != http.StatusConflict {
		t.Errorf("second create = %d, want 409", rec.Code)
	}
}

// A public address is not saved until it is confirmed, and says why.
func TestPublicAddressNeedsConfirmation(t *testing.T) {
	mux, _ := systemTest(t)
	call(t, mux, "POST", "/api/system/settings/create", nil)

	rec, out := call(t, mux, "PUT", "/api/system/settings", map[string]any{"addr": "0.0.0.0:8420"})
	if rec.Code != http.StatusConflict || out["confirmation"] != "public-address" {
		t.Fatalf("unconfirmed public address = %d %v", rec.Code, out)
	}
	if s, _, _ := machine.Load(); s.Addr != "127.0.0.1:8420" {
		t.Errorf("saved %q without confirmation", s.Addr)
	}

	rec, out = call(t, mux, "PUT", "/api/system/settings", map[string]any{"addr": "0.0.0.0:8420", "confirmPublic": true})
	if rec.Code != 200 {
		t.Fatalf("confirmed = %d %v", rec.Code, out)
	}
	var addr map[string]any
	for _, p := range out["pendingRestart"].([]any) {
		if m := p.(map[string]any); m["field"] == "addr" {
			addr = m
		}
	}
	if addr == nil || addr["applies"] != true {
		t.Errorf("pending addr = %v", addr)
	}

	if rec, _ := call(t, mux, "PUT", "/api/system/settings", map[string]any{"addr": "nonsense"}); rec.Code != 400 {
		t.Errorf("bad address = %d, want 400", rec.Code)
	}
}

// A saved address does not apply to a server started with --addr, and the
// panel says so rather than promising a restart will change it.
func TestPendingChangeOverriddenByFlag(t *testing.T) {
	mux, _ := systemTest(t)
	run := getRuntime()
	run.AddrSource = "flag"
	SetRuntime(run)
	call(t, mux, "POST", "/api/system/settings/create", nil)
	_, out := call(t, mux, "PUT", "/api/system/settings", map[string]any{"addr": "127.0.0.1:9000"})
	for _, p := range out["pendingRestart"].([]any) {
		if m := p.(map[string]any); m["field"] == "addr" {
			if m["applies"] != false || m["overriddenBy"] != "--addr" {
				t.Errorf("addr = %v", m)
			}
			return
		}
	}
	t.Error("no pending addr change")
}

func TestHostnameIsCheckedAndAppliesAtOnce(t *testing.T) {
	mux, _ := systemTest(t)
	call(t, mux, "POST", "/api/system/settings/create", nil)
	if rec, _ := call(t, mux, "PUT", "/api/system/settings", map[string]any{"hostname": "inbox.local"}); rec.Code != 400 {
		t.Errorf(".local = %d, want 400", rec.Code)
	}
	rec, out := call(t, mux, "PUT", "/api/system/settings", map[string]any{"hostname": "inboxql.localhost"})
	if rec.Code != 200 || out["hostsCommand"] == nil {
		t.Fatalf("hostname = %d %v", rec.Code, out)
	}
	if u := getRuntime().URL; u != "http://inboxql.localhost:8420" {
		t.Errorf("url = %q", u)
	}
}

// A misspelled field is refused, not silently ignored.
func TestUnknownSettingIsRefused(t *testing.T) {
	mux, _ := systemTest(t)
	call(t, mux, "POST", "/api/system/settings/create", nil)
	if rec, _ := call(t, mux, "PUT", "/api/system/settings", map[string]any{"adr": "x"}); rec.Code != 400 {
		t.Errorf("unknown field = %d, want 400", rec.Code)
	}
}

// Switching to a folder with no mailbox asks first; to one with a mailbox,
// records it for the next start.
func TestSwitchMailbox(t *testing.T) {
	mux, _ := systemTest(t)
	call(t, mux, "POST", "/api/system/settings/create", nil)

	empty := t.TempDir()
	rec, out := call(t, mux, "POST", "/api/system/mailbox", map[string]any{"dataDir": empty})
	if rec.Code != 404 || out["error"] != "no-mailbox" {
		t.Fatalf("empty folder = %d %v", rec.Code, out)
	}
	if rec, _ := call(t, mux, "POST", "/api/system/mailbox", map[string]any{"dataDir": "relative/path"}); rec.Code != 400 {
		t.Errorf("relative path = %d, want 400", rec.Code)
	}

	other := t.TempDir()
	os.WriteFile(filepath.Join(other, store.DBNAME), nil, 0o600)
	rec, out = call(t, mux, "POST", "/api/system/mailbox", map[string]any{"dataDir": other})
	if rec.Code != 200 {
		t.Fatalf("existing mailbox = %d %v", rec.Code, out)
	}
	if s, _, _ := machine.Load(); s.DataDir != other {
		t.Errorf("dataDir = %q", s.DataDir)
	}
	found := false
	for _, p := range out["pendingRestart"].([]any) {
		found = found || p.(map[string]any)["field"] == "dataDir"
	}
	if !found {
		t.Error("switching mailbox is not pending a restart")
	}
}

func TestRestartAnswersThenRestarts(t *testing.T) {
	mux, _ := systemTest(t)
	if rec, _ := call(t, mux, "POST", "/api/system/restart", nil); rec.Code != http.StatusNotImplemented {
		t.Errorf("no restart func = %d, want 501", rec.Code)
	}
	called := make(chan struct{}, 1)
	run := getRuntime()
	run.Restart = func() error { called <- struct{}{}; return nil }
	SetRuntime(run)
	if rec, _ := call(t, mux, "POST", "/api/system/restart", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("restart = %d", rec.Code)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Error("restart was never carried out")
	}
}

func TestUpdateCheckReportsNewer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v99.0.0","html_url":"https://example.invalid/r"}`))
	}))
	defer srv.Close()
	t.Setenv("INBOXQL_RELEASES_URL", srv.URL)
	mux, _ := systemTest(t)
	_, out := call(t, mux, "GET", "/api/system/update?refresh=1", nil)
	if out["latest"] != "99.0.0" || out["newer"] != true {
		t.Errorf("update = %v", out)
	}
}
