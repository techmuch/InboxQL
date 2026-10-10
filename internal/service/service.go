// Package service installs InboxQL as a per-user background service.
//
// # Per user, at login — not a system daemon at boot
//
// Mail is a person's, and so is everything the server needs to read it: their
// home folder, their keychain, their accounts. A system service starting at
// boot runs outside all of that. So each platform gets its per-user mechanism,
// which starts at login and stops at logout:
//
//	macOS    a LaunchAgent, restarted by launchd if it crashes
//	Linux    a systemd user unit (`loginctl enable-linger` makes it start at boot)
//	Windows  a task that runs at logon
//
// # The service reads the machine settings
//
// None of these definitions names a data directory. The service runs
// `iql start --service` and finds the mailbox in ~/.iql/settings.json, the
// same way a command typed in a terminal does — so there is one answer to
// "which mailbox", and changing it is editing one file.
//
// # Testable from anywhere
//
// Every platform is selected by name rather than by build tag, and every
// command it runs goes through a Runner. So the launchd definition is tested
// on Linux and the scheduled task on macOS, which is the only way the two
// platforms nobody is sitting in front of get tested at all.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Spec is what a service is installed to run.
type Spec struct {
	// Binary is the absolute path of the iql executable to run.
	Binary string
	// Home is the machine folder, where logs go.
	Home string
	// HomeOverride is set when $INBOXQL_HOME points somewhere other than
	// ~/.iql, so the service reads the same settings as the shell that
	// installed it.
	HomeOverride string
}

// Status is what is known about an installed service.
type Status struct {
	Platform  string `json:"platform"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	// Starting is the moment between being told to start and running.
	Starting bool   `json:"starting,omitempty"`
	File     string `json:"file,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Runner runs a command and returns its combined output.
type Runner func(name string, args ...string) (string, error)

// Exec is the real Runner.
func Exec(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Manager is one platform's service mechanism.
type Manager interface {
	Platform() string
	// File is where the service definition is written.
	File() string
	Install(Spec) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (Status, error)
	// Notes are anything worth telling the person after an install.
	Notes() []string
}

// For returns the manager for an operating system.
func For(goos string, run Runner, home string) (Manager, error) {
	if run == nil {
		run = Exec
	}
	switch goos {
	case "darwin":
		return &launchd{run: run, home: home, uid: os.Getuid()}, nil
	case "linux":
		return &systemd{run: run, home: home}, nil
	case "windows":
		return &schtasks{run: run, home: home}, nil
	default:
		return nil, fmt.Errorf("installing a service is not supported on %s; run `iql start` yourself", goos)
	}
}

// ForThisMachine is For(runtime.GOOS, Exec, the user's home).
func ForThisMachine() (Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return For(runtime.GOOS, Exec, home)
}

// LogFile is where the service's own output goes.
func LogFile(machineHome string) string {
	return filepath.Join(machineHome, "logs", "service.log")
}

// args are what every platform runs.
func args() []string { return []string{"start", "--service"} }

func writeFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}
