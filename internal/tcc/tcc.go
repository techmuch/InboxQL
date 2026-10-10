// Package tcc is what InboxQL can do about macOS privacy permissions, which is
// less than one would like: Full Disk Access has no prompt. An app can ask
// for the camera or the contacts and get an "Allow?" dialog; for Full Disk
// Access the only path is the person adding the program in System Settings.
//
// So this package does the two things that make that easy — opening the
// exact Settings pane, and showing the file to add in Finder — and, more
// usefully, says *which* program to add.
//
// # The responsible process
//
// macOS does not ask whether InboxQL may read ~/Library/Mail. It asks whether
// the process *responsible* for InboxQL may, and the responsible process is
// the app that started it: launched by the login service, that is the iql
// binary itself; typed into Terminal, it is Terminal. A grant to iql does
// nothing for a server started in iTerm, which is the hour people lose here.
package tcc

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// SettingsURL opens Privacy & Security → Full Disk Access.
const SettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"

// Grantee is the program that needs Full Disk Access for this process to read
// protected folders.
type Grantee struct {
	// Name is what System Settings lists it as.
	Name string `json:"name"`
	// Path is the file to add: an .app bundle, or the iql binary.
	Path string `json:"path"`
	// Kind is "binary" when it is this program, "app" when it is the app
	// that started it, and "unsure" when neither can be known — a server
	// started inside tmux, screen or an SSH session, whose server process was
	// reparented to launchd. Then the app that started the multiplexer is
	// responsible, and nothing visible from here names it.
	Kind string `json:"kind"`
	// Binary is this program, which can always be added, harmlessly, as well.
	Binary string `json:"binary"`
}

// asService is set by `iql start --service`. Under the login service launchd
// started the process, so the program itself is responsible, for certain.
var asService bool

// SetService records that this process is the login service.
func SetService(on bool) { asService = on }

// parentOf reads one step of the process tree: the parent's pid and the
// executable path of pid. A variable so tests can supply a tree.
var parentOf = defaultParentOf

func defaultParentOf(pid int) (ppid int, exe string, ok bool) {
	out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, "", false
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return 0, "", false
	}
	p, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", false
	}
	return p, strings.Join(fields[1:], " "), true
}

// Responsible works out which program macOS will hold responsible for this
// process, by walking up the process tree to the first application bundle.
// Reaching launchd without one means nothing started it but the system, and
// the program itself is responsible — the login-service case.
//
// macOS has a function that answers this directly,
// responsibility_get_pid_responsible_for_pid, but it is private, and tried on
// this machine it answered "itself" for every process — so it is not used.
func Responsible(self string) Grantee {
	own := Grantee{Name: filepath.Base(self), Path: self, Kind: "binary", Binary: self}
	if runtime.GOOS != "darwin" || asService {
		return own
	}
	pid := os.Getppid()
	for i := 0; i < 32 && pid > 1; i++ {
		ppid, exe, ok := parentOf(pid)
		if !ok {
			break
		}
		if app := bundleOf(exe); app != "" {
			return Grantee{Name: strings.TrimSuffix(filepath.Base(app), ".app"), Path: app, Kind: "app", Binary: self}
		}
		pid = ppid
	}
	// Up to launchd without an app, and not the login service: started from
	// something that detached — a multiplexer, an SSH session.
	return Grantee{Name: "the app you started InboxQL from", Path: self, Kind: "unsure", Binary: self}
}

// bundleOf is the outermost .app bundle an executable lives in, or "". The
// outermost, because a helper inside Visual Studio Code.app/…/Code Helper.app
// is not what System Settings lists.
func bundleOf(exe string) string {
	i := strings.Index(exe, ".app/")
	if i < 0 {
		return ""
	}
	return exe[:i+len(".app")]
}

// OpenSettings opens the Full Disk Access pane.
func OpenSettings() error {
	if runtime.GOOS != "darwin" {
		return errNotMac
	}
	return exec.Command("open", SettingsURL).Run()
}

// Reveal shows a file selected in Finder, ready to drag into the list.
func Reveal(path string) error {
	if runtime.GOOS != "darwin" {
		return errNotMac
	}
	return exec.Command("open", "-R", path).Run()
}

type notMac struct{}

func (notMac) Error() string { return "Full Disk Access is a macOS setting" }

var errNotMac error = notMac{}
