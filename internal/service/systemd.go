package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UnitName is the systemd user unit.
const UnitName = "inboxql.service"

// systemd is a user unit.
//
// A user unit starts with the user's first login and stops after their last
// logout. `loginctl enable-linger <user>` keeps the user manager running from
// boot, which makes this the one platform where "start at boot" is a real
// option — the notes say so, and leave the choice to the person.
type systemd struct {
	run  Runner
	home string
}

func (s *systemd) Platform() string { return "systemd" }

func (s *systemd) File() string {
	return filepath.Join(s.home, ".config", "systemd", "user", UnitName)
}

// unit is the definition.
//
// Restart=on-failure, not always: `iql start` exits cleanly when another
// server already holds the mailbox, and that must not be retried forever.
func (s *systemd) unit(spec Spec) string {
	exec := quoteSystemd(spec.Binary) + " " + strings.Join(args(), " ")
	env := ""
	if spec.HomeOverride != "" {
		env = "Environment=" + quoteSystemd("INBOXQL_HOME="+spec.HomeOverride) + "\n"
	}
	return fmt.Sprintf(`[Unit]
Description=InboxQL — email for engineers
Documentation=https://techmuch.github.io/InboxQL/

[Service]
ExecStart=%s
%sRestart=on-failure
RestartSec=30
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=default.target
`, exec, env, LogFile(spec.Home), LogFile(spec.Home))
}

// quoteSystemd quotes a value for an ExecStart or Environment line.
func quoteSystemd(v string) string {
	if !strings.ContainsAny(v, " \t\"\\") {
		return v
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

func (s *systemd) ctl(args ...string) error {
	full := append([]string{"--user"}, args...)
	if out, err := s.run("systemctl", full...); err != nil {
		return fmt.Errorf("systemctl %s: %v: %s", strings.Join(full, " "), err, out)
	}
	return nil
}

func (s *systemd) Install(spec Spec) error {
	if err := os.MkdirAll(filepath.Dir(LogFile(spec.Home)), 0o755); err != nil {
		return err
	}
	if err := writeFile(s.File(), s.unit(spec), 0o644); err != nil {
		return err
	}
	if err := s.ctl("daemon-reload"); err != nil {
		return err
	}
	return s.ctl("enable", "--now", UnitName)
}

func (s *systemd) Uninstall() error {
	_ = s.ctl("disable", "--now", UnitName)
	if err := os.Remove(s.File()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.ctl("daemon-reload")
}

func (s *systemd) Start() error   { return s.ctl("start", UnitName) }
func (s *systemd) Stop() error    { return s.ctl("stop", UnitName) }
func (s *systemd) Restart() error { return s.ctl("restart", UnitName) }

func (s *systemd) Status() (Status, error) {
	st := Status{Platform: s.Platform(), File: s.File()}
	if _, err := os.Stat(s.File()); err == nil {
		st.Installed = true
	}
	// is-active exits non-zero for anything but active, and prints the state
	// either way, so the output is the answer and the error is not.
	out, _ := s.run("systemctl", "--user", "is-active", UnitName)
	st.Detail = strings.TrimSpace(out)
	st.Running = st.Detail == "active"
	return st, nil
}

func (s *systemd) Notes() []string {
	return []string{
		"Starts at login and stops at logout.",
		"To start it at boot instead: `loginctl enable-linger $USER`.",
	}
}
