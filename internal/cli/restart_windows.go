//go:build windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// startDetached starts cmd with no console of its own and outside this
// process's group, so it survives this process being killed.
func startDetached(cmd *exec.Cmd, _ string) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | detachedProcess,
		HideWindow:    true,
	}
	return cmd.Start()
}

// replaceSelf starts a successor in this console and lets this process end.
// Windows has no exec, so the successor waits for this one's lock instead.
func replaceSelf(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), envRestartWait+"=1")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Start()
}
