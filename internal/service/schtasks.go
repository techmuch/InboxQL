package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TaskName is the Windows scheduled task.
const TaskName = "InboxQL"

// schtasks is a task that runs at logon.
//
// # Why a task and not a Windows Service
//
// A Windows Service runs as a system account from boot, outside the user's
// profile — the same reason macOS gets an agent rather than a daemon. A logon
// task runs as the user, and the processes it starts end when they log off.
//
// # Why through a script
//
// `iql.exe` is a console program, and a task that starts it directly opens a
// console window at every logon. A two-line Windows Script Host file starts it
// with no window at all, which is the only way to get none without building a
// second, GUI-subsystem binary.
//
// # What it cannot do
//
// Restart after a crash: the task's own retry settings are only reachable
// through its XML definition, and the task has already exited by the time iql
// has started — the script hands off and returns. So stopping goes by the
// server's own PID, read from its lock, and a crash stays down until the next
// logon or `iql service start`. The notes say so.
type schtasks struct {
	run       Runner
	home      string
	serverPID func() int
}

// SetServerPID tells the manager how to find the running server, which is
// how a scheduled task is stopped.
func (w *schtasks) SetServerPID(fn func() int) { w.serverPID = fn }

func (w *schtasks) Platform() string { return "schtasks" }

// File is the launcher script the task runs.
func (w *schtasks) File() string {
	return filepath.Join(w.home, ".iql", "start-hidden.vbs")
}

// script starts iql with no window. In VBScript a literal quote inside a
// string is doubled, and the command line itself needs the binary quoted.
func (w *schtasks) script(s Spec) string {
	cmd := `"` + s.Binary + `" ` + strings.Join(args(), " ")
	vb := strings.ReplaceAll(cmd, `"`, `""`)
	env := ""
	if s.HomeOverride != "" {
		env = fmt.Sprintf("shell.Environment(\"PROCESS\")(\"INBOXQL_HOME\") = \"%s\"\r\n",
			strings.ReplaceAll(s.HomeOverride, `"`, `""`))
	}
	return "' Starts InboxQL with no console window. Written by `iql service install`.\r\n" +
		"Set shell = CreateObject(\"WScript.Shell\")\r\n" +
		env +
		fmt.Sprintf("shell.Run \"%s\", 0, False\r\n", vb)
}

func (w *schtasks) Install(s Spec) error {
	if err := writeFile(w.File(), w.script(s), 0o644); err != nil {
		return err
	}
	tr := `wscript.exe "` + w.File() + `"`
	if out, err := w.run("schtasks", "/Create", "/F", "/TN", TaskName,
		"/TR", tr, "/SC", "ONLOGON", "/RL", "LIMITED"); err != nil {
		return fmt.Errorf("schtasks /Create: %v: %s", err, out)
	}
	return w.Start()
}

func (w *schtasks) Uninstall() error {
	_ = w.Stop()
	_, _ = w.run("schtasks", "/Delete", "/F", "/TN", TaskName)
	if err := os.Remove(w.File()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (w *schtasks) Start() error {
	if out, err := w.run("schtasks", "/Run", "/TN", TaskName); err != nil {
		return fmt.Errorf("schtasks /Run: %v: %s", err, out)
	}
	return nil
}

func (w *schtasks) Stop() error {
	if w.serverPID == nil {
		return fmt.Errorf("cannot find the running server")
	}
	pid := w.serverPID()
	if pid <= 0 {
		return nil // nothing running
	}
	if out, err := w.run("taskkill", "/PID", strconv.Itoa(pid), "/F"); err != nil {
		return fmt.Errorf("taskkill: %v: %s", err, out)
	}
	return nil
}

func (w *schtasks) Restart() error {
	if err := w.Stop(); err != nil {
		return err
	}
	return w.Start()
}

func (w *schtasks) Status() (Status, error) {
	st := Status{Platform: w.Platform(), File: w.File()}
	if _, err := w.run("schtasks", "/Query", "/TN", TaskName); err == nil {
		st.Installed = true
	}
	if w.serverPID != nil {
		if pid := w.serverPID(); pid > 0 {
			st.Running, st.Detail = true, "pid "+strconv.Itoa(pid)
		}
	}
	if !st.Running {
		st.Detail = "not running"
	}
	return st, nil
}

func (w *schtasks) Notes() []string {
	return []string{
		"Starts at logon, with no window, and ends when you log off.",
		"It is not restarted after a crash; `iql service start` starts it again.",
	}
}
