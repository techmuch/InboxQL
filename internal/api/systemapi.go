package api

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/user/inboxql/internal/gliner"
	"github.com/user/inboxql/internal/laya"
	"github.com/user/inboxql/internal/machine"
	"github.com/user/inboxql/internal/power"
	"github.com/user/inboxql/internal/service"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/update"
)

// Runtime is what the server knows about how it was started.
//
// Set once by `iql start`, which is the only place that knows: whether it was
// launched by a service manager, typed in a terminal or run under the --dev
// supervisor; which mailbox and why; what the settings said at that moment.
// The settings snapshot is what makes "saved, restart to apply" possible —
// the file can change under a running server, and the difference is exactly
// what a restart would apply.
type Runtime struct {
	// Mode is "service", "foreground" or "dev".
	Mode       string
	Binary     string
	PID        int
	Started    time.Time
	DataDir    string
	DataSource string
	Addr       string
	// AddrSource is where Addr came from: "flag", "env", "settings" or
	// "default". A flag or variable outranks the settings file, so a saved
	// address does not apply to a server started that way.
	AddrSource string
	URL        string
	AuthLocal  bool
	AuthReason string
	// Settings is what ~/.iql/settings.json said when the server started, or
	// nil when there was no file.
	Settings *machine.Settings
	// Restart restarts the server the way its mode requires. It is called
	// after the response has been sent, because it ends this process.
	Restart func() error
	// Update starts `iql update --yes` in a process of its own, writing its
	// output to logPath, and calls done when that process exits. Restarting
	// afterwards is the updater's job under a service manager and this
	// server's otherwise.
	Update func(logPath string, done func(error)) error
}

var (
	runtimeMu   sync.RWMutex
	currentRun  = Runtime{Mode: "foreground", Started: time.Now()}
	updateCache struct {
		sync.Mutex
		at      time.Time
		release *update.Release
		err     string
	}
)

// SetRuntime records how this server was started.
func SetRuntime(r Runtime) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	currentRun = r
}

func getRuntime() Runtime {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	return currentRun
}

func registerSystemRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/system", handleSystem)
	mux.HandleFunc("GET /api/system/update", handleSystemUpdateCheck)
	mux.HandleFunc("GET /api/system/settings", handleGetMachineSettings)
	mux.HandleFunc("PUT /api/system/settings", handlePutMachineSettings)
	mux.HandleFunc("POST /api/system/settings/create", handleCreateMachineSettings)
	mux.HandleFunc("POST /api/system/mailbox", handleSwitchMailbox)
	mux.HandleFunc("POST /api/system/restart", handleSystemRestart)
	mux.HandleFunc("POST /api/system/update", handleSystemUpdateRun)
	mux.HandleFunc("GET /api/system/update/log", handleSystemUpdateLog)
}

// SystemState is everything the System section shows.
type SystemState struct {
	Version  map[string]any `json:"version"`
	Run      map[string]any `json:"run"`
	Mailbox  map[string]any `json:"mailbox"`
	Address  map[string]any `json:"address"`
	Settings map[string]any `json:"settings"`
	Models   map[string]any `json:"models"`
	Service  map[string]any `json:"service"`
	Power    map[string]any `json:"power"`
	AI       map[string]any `json:"ai"`
}

func handleSystem(w http.ResponseWriter, r *http.Request) {
	run := getRuntime()
	vi := currentVersionInfo

	st := SystemState{
		Version: map[string]any{
			"version": vi.Version, "revision": vi.Revision, "go": runtime.Version(),
			"platform": runtime.GOOS + "/" + runtime.GOARCH, "binary": run.Binary,
			"homebrew": strings.Contains(run.Binary, "/Cellar/"),
		},
		Run: map[string]any{
			"mode": run.Mode, "pid": run.PID, "started": run.Started,
			"uptimeSeconds": int64(time.Since(run.Started).Seconds()),
			"instanceId":    vi.InstanceID, "canRestart": run.Restart != nil,
		},
		Mailbox: mailboxState(run),
		Address: addressState(run),
		Models:  modelsState(run.DataDir),
		Service: serviceState(),
		Power:   powerState(),
		AI:      aiState(),
	}

	file, found, err := machine.Load()
	st.Settings = map[string]any{"path": machine.Path(), "exists": found, "home": machine.Home()}
	if err != nil {
		st.Settings["problem"] = err.Error()
	}
	if found && file != nil {
		st.Settings["pendingRestart"] = pendingRestart(run.Settings, file)
	}

	writeJSON(w, http.StatusOK, st)
}

// sourceWhy is the reason a data directory is the one in use, in words.
func sourceWhy(source string) string {
	switch source {
	case "flag":
		return "named with --data when the server was started"
	case "env":
		return "named by $INBOXQL_DATA"
	case "settings":
		return "named by the machine settings"
	case "local":
		return "the ./data folder where the server was started, because there are no machine settings"
	default:
		return ""
	}
}

func mailboxState(run Runtime) map[string]any {
	m := map[string]any{
		"dataDir": run.DataDir, "source": run.DataSource, "why": sourceWhy(run.DataSource),
		"schema": store.SchemaVersion,
	}
	if fi, err := os.Stat(filepath.Join(run.DataDir, store.DBNAME)); err == nil {
		m["dbBytes"] = fi.Size()
	}
	// The newest backup, because "when was this last backed up" is the
	// question a person has right before clicking Update.
	if entries, err := os.ReadDir(filepath.Join(run.DataDir, "backups")); err == nil {
		var newest os.FileInfo
		for _, e := range entries {
			if fi, err := e.Info(); err == nil && !fi.IsDir() && (newest == nil || fi.ModTime().After(newest.ModTime())) {
				newest = fi
			}
		}
		if newest != nil {
			m["lastBackup"] = map[string]any{"name": newest.Name(), "at": newest.ModTime(), "bytes": newest.Size()}
		}
	}
	return m
}

func addressState(run Runtime) map[string]any {
	a := map[string]any{
		"addr": run.Addr, "url": run.URL,
		"auth": map[string]any{"passwordless": run.AuthLocal, "reason": run.AuthReason},
	}
	if run.Settings != nil && run.Settings.Hostname != "" {
		name := run.Settings.Hostname
		a["hostname"] = name
		resolves := false
		if addrs, err := net.LookupHost(name); err == nil {
			for _, s := range addrs {
				if ip := net.ParseIP(s); ip != nil && ip.IsLoopback() {
					resolves = true
				}
			}
		}
		a["hostnameResolves"] = resolves
	}
	return a
}

func modelsState(dataDir string) map[string]any {
	installed := []string{}
	for name, dir := range map[string]string{"laya": laya.Dir(dataDir), "gliner": gliner.Dir(dataDir)} {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			installed = append(installed, name)
		}
	}
	sort.Strings(installed)
	m := map[string]any{"installed": installed, "laya": laya.Dir(dataDir), "gliner": gliner.Dir(dataDir)}
	return m
}

func serviceState() map[string]any {
	mgr, err := service.ForThisMachine()
	if err != nil {
		return map[string]any{"supported": false, "detail": err.Error()}
	}
	st, err := mgr.Status()
	if err != nil {
		return map[string]any{"supported": true, "platform": mgr.Platform(), "detail": err.Error()}
	}
	return map[string]any{"supported": true, "platform": st.Platform, "installed": st.Installed,
		"running": st.Running, "starting": st.Starting, "detail": st.Detail, "file": st.File}
}

func powerState() map[string]any {
	src := power.Source()
	allowed := false
	if s, found, err := machine.Load(); err == nil && found {
		allowed = s.HeavyWorkOnBattery
	}
	return map[string]any{"source": src.String(), "heavyWorkOnBattery": allowed,
		"deferring": src == power.Battery && !allowed}
}

// aiState says which provider is configured and, above all, whether it is on
// another machine: that is the one fact on this page about mail leaving it.
func aiState() map[string]any {
	cfg, err := store.GetLLMConfig()
	if err != nil || cfg.Provider == "" {
		return map[string]any{"provider": "", "remote": false}
	}
	return map[string]any{"provider": cfg.Provider, "model": cfg.Model,
		"endpoint": cfg.Endpoint, "remote": cfg.IsRemote()}
}

// handleSystemUpdateCheck asks for the newest release, at most hourly.
//
// Cached because the panel is opened often and GitHub's unauthenticated API
// allows sixty requests an hour per address; ?refresh=1 asks again now.
func handleSystemUpdateCheck(w http.ResponseWriter, r *http.Request) {
	updateCache.Lock()
	defer updateCache.Unlock()
	if r.URL.Query().Get("refresh") == "1" || time.Since(updateCache.at) > time.Hour {
		rel, err := update.Latest()
		updateCache.at, updateCache.release, updateCache.err = time.Now(), rel, ""
		if err != nil {
			updateCache.err = err.Error()
		}
	}
	current := currentVersionInfo.Version
	out := map[string]any{"current": current, "checkedAt": updateCache.at}
	if updateCache.err != "" {
		out["error"] = updateCache.err
	}
	if rel := updateCache.release; rel != nil {
		out["latest"] = rel.Version()
		out["url"] = rel.URL
		out["newer"] = update.Newer(rel.Version(), current)
	}
	run := getRuntime()
	out["homebrew"] = strings.Contains(run.Binary, "/Cellar/")
	writeJSON(w, http.StatusOK, out)
}

// decodeStrict decodes a JSON body, refusing fields it does not know: a
// misspelled setting silently ignored is a setting that does not apply.
func decodeStrict(w http.ResponseWriter, r *http.Request, into any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "expected Content-Type: application/json", http.StatusUnsupportedMediaType)
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return false
	}
	return true
}
