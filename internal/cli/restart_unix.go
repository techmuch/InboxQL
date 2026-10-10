//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

// startDetached starts cmd in a session of its own, so it survives this
// process and whatever stops it.
func startDetached(cmd *exec.Cmd, _ string) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// replaceSelf becomes a fresh copy of the binary at exe, with the same
// arguments, keeping this process's id and terminal. Called after the lock
// and the database are released; every other descriptor is close-on-exec.
func replaceSelf(exe string) error {
	return syscall.Exec(exe, os.Args, os.Environ())
}
