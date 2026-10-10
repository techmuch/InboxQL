package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/user/inboxql/internal/auth"
	"github.com/user/inboxql/internal/hostsfile"
	"github.com/user/inboxql/internal/machine"
	"github.com/user/inboxql/internal/serverlock"
	"github.com/user/inboxql/internal/store"
)

// PendingChange is one saved setting a running server is not yet using.
type PendingChange struct {
	Field   string `json:"field"`
	Running string `json:"running"`
	Saved   string `json:"saved"`
	// Applies is false when a restart would not pick the saved value up,
	// because a flag or environment variable outranks the file.
	Applies      bool   `json:"applies"`
	OverriddenBy string `json:"overriddenBy,omitempty"`
}

// pendingRestart compares the settings file with what this server is using.
//
// Against the running values rather than the file as it was at start: a
// server started on ./data with no settings is using ./data, and settings
// that name ./data change nothing.
func pendingRestart(_ *machine.Settings, file *machine.Settings) []PendingChange {
	run := getRuntime()
	out := []PendingChange{}
	if file == nil {
		return out
	}
	if !samePath(file.DataDir, run.DataDir) {
		c := PendingChange{Field: "dataDir", Running: run.DataDir, Saved: file.DataDir, Applies: true}
		switch run.DataSource {
		case "flag":
			c.Applies, c.OverriddenBy = false, "--data"
		case "env":
			c.Applies, c.OverriddenBy = false, "$INBOXQL_DATA"
		}
		out = append(out, c)
	}
	if file.Addr != run.Addr {
		c := PendingChange{Field: "addr", Running: run.Addr, Saved: file.Addr, Applies: true}
		switch run.AddrSource {
		case "flag":
			c.Applies, c.OverriddenBy = false, "--addr"
		case "env":
			c.Applies, c.OverriddenBy = false, "$INBOXQL_ADDR"
		}
		out = append(out, c)
	}
	startedModels := ""
	if run.Settings != nil {
		startedModels = run.Settings.Models
	}
	if !samePath(file.Models, startedModels) {
		out = append(out, PendingChange{Field: "models", Running: startedModels, Saved: file.Models, Applies: true})
	}
	return out
}

func samePath(a, b string) bool {
	return filepath.Clean(machine.Expand(a)) == filepath.Clean(machine.Expand(b))
}

// settingField describes when one field takes effect, for the editor to say
// beside it rather than leaving somebody to find out.
type settingField struct {
	Name    string `json:"name"`
	Applies string `json:"applies"` // "now" or "restart"
	Help    string `json:"help"`
}

var settingFields = []settingField{
	{"dataDir", "restart", "The mailbox: the database, its key, attachments and backups. Changed with “Use a different mailbox”."},
	{"addr", "restart", "Where the server listens. Keep it on 127.0.0.1 unless you mean other machines to reach it."},
	{"models", "restart", "Where model weights are kept, once for the whole machine."},
	{"hostname", "now", "A name for this site, such as inboxql.localhost. The hosts file needs administrator rights, so it is one command in a terminal."},
	{"heavyWorkOnBattery", "now", "Let the annotation pass after a sync run on battery. Off, it waits for mains power."},
}

func machineSettingsResponse(s *machine.Settings, found bool, problem error) map[string]any {
	out := map[string]any{
		"path": machine.Path(), "exists": found, "fields": settingFields,
		"defaults": machine.Defaults(),
	}
	if problem != nil {
		out["problem"] = problem.Error()
	}
	if s != nil {
		out["settings"] = s
		out["pendingRestart"] = pendingRestart(nil, s)
		if s.Hostname != "" {
			out["hostsCommand"] = hostsCommand(s.Hostname)
		}
	}
	return out
}

// hostsCommand is what the person runs to put a name in the hosts file. It
// is never run from here: the file belongs to the administrator, and a web
// page asking for an administrator password is the shape of an attack.
func hostsCommand(name string) string {
	if strings.HasPrefix(hostsfile.Path(), "C:") || os.PathSeparator == '\\' {
		return "iql hosts set " + name + "   (in a PowerShell opened with “Run as administrator”)"
	}
	return "sudo iql hosts set " + name
}

func handleGetMachineSettings(w http.ResponseWriter, r *http.Request) {
	s, found, err := machine.Load()
	writeJSON(w, http.StatusOK, machineSettingsResponse(s, found, err))
}

// settingsEdit is a change to the settings file. Pointers, so "not sent" and
// "set to empty" differ: an edit of one field leaves the others alone.
type settingsEdit struct {
	Addr               *string `json:"addr"`
	Models             *string `json:"models"`
	Hostname           *string `json:"hostname"`
	HeavyWorkOnBattery *bool   `json:"heavyWorkOnBattery"`
	// ConfirmPublic acknowledges that a non-loopback address makes the
	// mailbox reachable from the network and turns the password on.
	ConfirmPublic bool `json:"confirmPublic"`
}

// validAddr checks a listen address, and says whether it is loopback.
func validAddr(addr string) (loopback bool, err error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("%q is not host:port — for example 127.0.0.1:8420", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return false, fmt.Errorf("%q is not a port number", port)
	}
	host = strings.Trim(host, "[]")
	return host != "" && auth.IsLoopback(host), nil
}

func handlePutMachineSettings(w http.ResponseWriter, r *http.Request) {
	var edit settingsEdit
	if !decodeStrict(w, r, &edit) {
		return
	}
	s, found, err := machine.Load()
	if err != nil {
		// Not overwritten: an unreadable file is somebody's half-finished
		// edit, and this would replace it with defaults.
		writeError(w, http.StatusConflict, "%v — fix the file by hand first", err)
		return
	}
	if !found {
		writeError(w, http.StatusConflict, "there are no machine settings yet; create them first")
		return
	}

	warnings := []string{}
	if edit.Addr != nil {
		a := strings.TrimSpace(*edit.Addr)
		if a == "" {
			a = machine.DefaultAddr
		}
		loopback, err := validAddr(a)
		if err != nil {
			writeError(w, http.StatusBadRequest, "%v", err)
			return
		}
		if !loopback && !edit.ConfirmPublic {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":        "confirm",
				"confirmation": "public-address",
				"message": a + " listens beyond this machine: anyone on the network can reach the port, " +
					"and a password will be required to sign in. InboxQL has no TLS of its own, so put it " +
					"behind a reverse proxy first.",
			})
			return
		}
		s.Addr = a
	}
	if edit.Models != nil {
		m := strings.TrimSpace(*edit.Models)
		if m == "" {
			m = machine.Defaults().Models
		}
		m = machine.Expand(m)
		if !filepath.IsAbs(m) {
			writeError(w, http.StatusBadRequest, "the models folder must be a full path")
			return
		}
		s.Models = m
	}
	if edit.Hostname != nil {
		h := strings.ToLower(strings.TrimSpace(*edit.Hostname))
		if h != "" {
			warn, err := hostsfile.Check(h)
			if err != nil {
				writeError(w, http.StatusBadRequest, "%v", err)
				return
			}
			if warn != "" {
				warnings = append(warnings, warn)
			}
		}
		s.Hostname = h
		updateDisplayURL(h)
	}
	if edit.HeavyWorkOnBattery != nil {
		s.HeavyWorkOnBattery = *edit.HeavyWorkOnBattery
	}

	if err := machine.Save(s); err != nil {
		writeError(w, http.StatusInternalServerError, "writing %s: %v", machine.Path(), err)
		return
	}
	out := machineSettingsResponse(s, true, nil)
	out["warnings"] = warnings
	writeJSON(w, http.StatusOK, out)
}

// updateDisplayURL applies a hostname at once. Nothing in the server depends
// on it but the address it reports.
func updateDisplayURL(hostname string) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if currentRun.Settings != nil {
		copy := *currentRun.Settings
		copy.Hostname = hostname
		currentRun.Settings = &copy
	} else if hostname != "" {
		currentRun.Settings = &machine.Settings{Hostname: hostname}
	}
	host, port, err := net.SplitHostPort(currentRun.Addr)
	if err != nil || (host != "" && !auth.IsLoopback(strings.Trim(host, "[]"))) {
		return
	}
	if hostname != "" {
		currentRun.URL = "http://" + hostname + ":" + port
	} else {
		currentRun.URL = "http://localhost:" + port
	}
}

// handleCreateMachineSettings writes settings that describe what this server
// is already doing — the same mailbox, the same address — so creating them
// changes nothing until somebody edits them.
func handleCreateMachineSettings(w http.ResponseWriter, r *http.Request) {
	if _, found, _ := machine.Load(); found {
		writeError(w, http.StatusConflict, "%s already exists", machine.Path())
		return
	}
	run := getRuntime()
	s := machine.Defaults()
	if run.DataDir != "" {
		if abs, err := filepath.Abs(run.DataDir); err == nil {
			s.DataDir = abs
		}
	}
	if run.Addr != "" {
		s.Addr = run.Addr
	}
	if err := machine.Save(s); err != nil {
		writeError(w, http.StatusInternalServerError, "writing %s: %v", machine.Path(), err)
		return
	}
	writeJSON(w, http.StatusOK, machineSettingsResponse(s, true, nil))
}

// handleSwitchMailbox points the settings at another mailbox.
//
// An explicit action rather than a text field in the editor, because a
// mistyped path is not a setting but a different, empty mailbox — the
// target is checked, and made only when asked.
func handleSwitchMailbox(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DataDir string `json:"dataDir"`
		Create  bool   `json:"create"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	dir := machine.Expand(strings.TrimSpace(body.DataDir))
	if dir == "" || !filepath.IsAbs(dir) {
		writeError(w, http.StatusBadRequest, "give the mailbox's full path")
		return
	}
	dir = filepath.Clean(dir)
	s, found, err := machine.Load()
	if err != nil {
		writeError(w, http.StatusConflict, "%v — fix the file by hand first", err)
		return
	}
	if !found {
		writeError(w, http.StatusConflict, "there are no machine settings yet; create them first")
		return
	}
	run := getRuntime()

	_, statErr := os.Stat(filepath.Join(dir, store.DBNAME))
	exists := statErr == nil
	result := map[string]any{"dataDir": dir, "existed": exists}

	if !exists {
		if !body.Create {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error": "no-mailbox", "dataDir": dir,
				"message": "There is no mailbox at " + dir + ". Create a new, empty one there?",
			})
			return
		}
		// A child process, so the new database is made by `iql init` exactly
		// as on the command line and this server's own connection is never
		// pointed elsewhere.
		if run.Binary == "" {
			writeError(w, http.StatusInternalServerError, "cannot find the iql binary to create a mailbox with")
			return
		}
		cmd := exec.Command(run.Binary, "--json", "init", "--data", dir)
		cmd.Env = os.Environ()
		out, err := cmd.Output()
		if err != nil {
			msg := err.Error()
			var ee *exec.ExitError
			if errors.As(err, &ee) && len(ee.Stderr) > 0 {
				msg = strings.TrimSpace(string(ee.Stderr))
			}
			writeError(w, http.StatusInternalServerError, "creating a mailbox at %s: %s", dir, msg)
			return
		}
		var made map[string]any
		_ = json.Unmarshal(out, &made)
		// The generated administrator password is shown once, here, as
		// `iql init` shows it once in a terminal.
		if pw, ok := made["adminPassword"].(string); ok && pw != "" {
			result["adminUser"] = made["adminUser"]
			result["adminPassword"] = pw
		}
		result["created"] = true
	} else if info, held := serverlock.Held(dir); held && !samePath(dir, run.DataDir) {
		writeError(w, http.StatusConflict, "another InboxQL server is already using %s (%s)", dir, info.URL)
		return
	}

	s.DataDir = dir
	if err := machine.Save(s); err != nil {
		writeError(w, http.StatusInternalServerError, "writing %s: %v", machine.Path(), err)
		return
	}
	resp := machineSettingsResponse(s, true, nil)
	for k, v := range result {
		resp[k] = v
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSystemRestart answers first and restarts after, so the page gets an
// answer and then watches for the new instance.
func handleSystemRestart(w http.ResponseWriter, r *http.Request) {
	run := getRuntime()
	if run.Restart == nil {
		writeError(w, http.StatusNotImplemented, "this server cannot restart itself")
		return
	}
	if updateRunning() {
		writeError(w, http.StatusConflict, "an update is running; it restarts the server when it is done")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"restarting": true, "mode": run.Mode,
		"instanceId": currentVersionInfo.InstanceID})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := run.Restart(); err != nil {
			log.Printf("[system] restart failed: %v", err)
		}
	}()
}

var updateJob struct {
	sync.Mutex
	running  bool
	started  time.Time
	finished time.Time
	err      string
	log      string
}

func updateRunning() bool {
	updateJob.Lock()
	defer updateJob.Unlock()
	return updateJob.running
}

// UpdateLogPath is where a started update writes what it did.
func UpdateLogPath() string { return filepath.Join(machine.Home(), "logs", "update.log") }

func handleSystemUpdateRun(w http.ResponseWriter, r *http.Request) {
	run := getRuntime()
	if run.Update == nil {
		writeError(w, http.StatusNotImplemented, "this server cannot update itself")
		return
	}
	if strings.Contains(run.Binary, "/Cellar/") {
		writeError(w, http.StatusConflict, "installed by Homebrew — run `brew upgrade inboxql` in a terminal")
		return
	}
	updateJob.Lock()
	if updateJob.running {
		updateJob.Unlock()
		writeError(w, http.StatusConflict, "an update is already running")
		return
	}
	updateJob.running, updateJob.started, updateJob.finished, updateJob.err = true, time.Now(), time.Time{}, ""
	updateJob.log = UpdateLogPath()
	updateJob.Unlock()

	err := run.Update(UpdateLogPath(), func(err error) {
		updateJob.Lock()
		updateJob.running, updateJob.finished = false, time.Now()
		if err != nil {
			updateJob.err = err.Error()
		}
		updateJob.Unlock()
	})
	if err != nil {
		updateJob.Lock()
		updateJob.running, updateJob.err = false, err.Error()
		updateJob.Unlock()
		writeError(w, http.StatusInternalServerError, "starting the update: %v", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "log": UpdateLogPath(),
		"mode": run.Mode, "instanceId": currentVersionInfo.InstanceID})
}

// handleSystemUpdateLog reports the update's progress: whether it is still
// running, and the tail of what it printed.
func handleSystemUpdateLog(w http.ResponseWriter, r *http.Request) {
	updateJob.Lock()
	out := map[string]any{"running": updateJob.running, "log": UpdateLogPath()}
	if !updateJob.started.IsZero() {
		out["started"] = updateJob.started
	}
	if !updateJob.finished.IsZero() {
		out["finished"] = updateJob.finished
	}
	if updateJob.err != "" {
		out["error"] = updateJob.err
	}
	updateJob.Unlock()
	if b, err := os.ReadFile(UpdateLogPath()); err == nil {
		if len(b) > 16<<10 {
			b = b[len(b)-16<<10:]
		}
		out["output"] = string(b)
	}
	writeJSON(w, http.StatusOK, out)
}
