package service

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Label is the launchd label, reverse-DNS on the repository's home.
const Label = "io.github.techmuch.inboxql"

// launchd is a LaunchAgent.
//
// # Why these keys
//
//   - RunAtLoad: start when the agent is loaded, which launchd does at login
//     because the plist is in ~/Library/LaunchAgents.
//   - KeepAlive / SuccessfulExit false: restart after a crash, and only then.
//     `iql start` exits 0 when another server already holds the mailbox, so a
//     clean exit means "nothing for me to do" and must not be retried forever.
//   - ThrottleInterval 30: a server that cannot start — the port is taken —
//     retries twice a minute rather than six times.
//   - ProcessType Standard: Background would throttle the CPU of the process
//     that serves the web pages, and the site has to feel instant.
//
// Nothing here keeps the machine awake. A listening socket does not hold off
// sleep; closing the lid sleeps the Mac and freezes this process with it.
type launchd struct {
	run  Runner
	home string
	uid  int
}

func (l *launchd) Platform() string { return "launchd" }

func (l *launchd) File() string {
	return filepath.Join(l.home, "Library", "LaunchAgents", Label+".plist")
}

func (l *launchd) target() string { return fmt.Sprintf("gui/%d/%s", l.uid, Label) }
func (l *launchd) domain() string { return fmt.Sprintf("gui/%d", l.uid) }

func (l *launchd) plist(s Spec) string {
	var argv strings.Builder
	for _, a := range append([]string{s.Binary}, args()...) {
		fmt.Fprintf(&argv, "\t\t<string>%s</string>\n", html.EscapeString(a))
	}
	env := ""
	if s.HomeOverride != "" {
		env = fmt.Sprintf(`	<key>EnvironmentVariables</key>
	<dict>
		<key>INBOXQL_HOME</key>
		<string>%s</string>
	</dict>
`, html.EscapeString(s.HomeOverride))
	}
	logPath := html.EscapeString(LogFile(s.Home))
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
%s	</array>
%s	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>30</integer>
	<key>ProcessType</key>
	<string>Standard</string>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, Label, argv.String(), env, logPath, logPath)
}

func (l *launchd) Install(s Spec) error {
	if err := os.MkdirAll(filepath.Dir(LogFile(s.Home)), 0o755); err != nil {
		return err
	}
	if err := writeFile(l.File(), l.plist(s), 0o644); err != nil {
		return err
	}
	// Replacing an installed agent: unload the old definition first, or
	// bootstrap refuses with "service already loaded". Its error is expected
	// on a first install and ignored.
	_, _ = l.run("launchctl", "bootout", l.target())
	if out, err := l.run("launchctl", "bootstrap", l.domain(), l.File()); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	return nil
}

func (l *launchd) Uninstall() error {
	_, _ = l.run("launchctl", "bootout", l.target())
	if err := os.Remove(l.File()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Start loads the agent if it is not loaded, and runs it.
func (l *launchd) Start() error {
	if _, err := os.Stat(l.File()); err != nil {
		return fmt.Errorf("not installed — run `iql service install`")
	}
	if _, err := l.run("launchctl", "print", l.target()); err != nil {
		if out, err := l.run("launchctl", "bootstrap", l.domain(), l.File()); err != nil {
			return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
		}
		return nil
	}
	if out, err := l.run("launchctl", "kickstart", l.target()); err != nil {
		return fmt.Errorf("launchctl kickstart: %v: %s", err, out)
	}
	return nil
}

// Stop unloads the agent.
//
// Not a kill: with KeepAlive, launchd restarts a process that dies on a
// signal. Unloading is the only stop that stays stopped — until the next
// login, when the agent loads again, which is what "stop" should mean for a
// login service.
func (l *launchd) Stop() error {
	if out, err := l.run("launchctl", "bootout", l.target()); err != nil &&
		!strings.Contains(out, "No such process") && !strings.Contains(out, "Could not find") {
		return fmt.Errorf("launchctl bootout: %v: %s", err, out)
	}
	return nil
}

func (l *launchd) Restart() error {
	if _, err := l.run("launchctl", "print", l.target()); err != nil {
		return l.Start()
	}
	if out, err := l.run("launchctl", "kickstart", "-k", l.target()); err != nil {
		return fmt.Errorf("launchctl kickstart -k: %v: %s", err, out)
	}
	return nil
}

var launchdState = regexp.MustCompile(`(?m)^\s*state = (\S+)`)
var launchdPID = regexp.MustCompile(`(?m)^\s*pid = (\d+)`)

func (l *launchd) Status() (Status, error) {
	st := Status{Platform: l.Platform(), File: l.File()}
	if _, err := os.Stat(l.File()); err == nil {
		st.Installed = true
	}
	out, err := l.run("launchctl", "print", l.target())
	if err != nil {
		st.Detail = "not loaded"
		return st, nil
	}
	state := "unknown"
	if m := launchdState.FindStringSubmatch(out); m != nil {
		state = m[1]
	}
	st.Running = state == "running"
	st.Detail = state
	// launchd reports the moment between "start" and "running" as the
	// process doing the spawning. It is starting, not stopped, and saying
	// "not running" straight after `iql service start` reads as a failure.
	if state == "xpcproxy" || state == "spawn scheduled" {
		st.Starting = true
		st.Detail = "starting"
	}
	if m := launchdPID.FindStringSubmatch(out); m != nil {
		st.Detail += ", pid " + m[1]
	}
	return st, nil
}

func (l *launchd) Notes() []string {
	return []string{
		"Starts at login and stops at logout. It does not keep the Mac awake.",
		"Importing from Apple Mail needs Full Disk Access for this binary; an unsigned binary loses that grant whenever it is replaced, so re-grant it after `iql update`.",
	}
}
