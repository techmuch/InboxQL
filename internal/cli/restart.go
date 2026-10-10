package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/user/inboxql/internal/serverlock"
	"github.com/user/inboxql/internal/service"
)

// exitRestart is how a --dev child asks its supervisor to start it again
// without the binary having changed. 75 is EX_TEMPFAIL: "try again".
const exitRestart = 75

// envRestartWait tells a successor that its predecessor is still shutting
// down, so a held lock means "wait", not "somebody else is serving".
const envRestartWait = "INBOXQL_RESTART_WAIT"

// acquireLock takes the server lock, waiting up to 30 seconds when this
// process is a restart's successor.
func acquireLock(dataDir string, info serverlock.Info) (*serverlock.Lock, *serverlock.Info, error) {
	waiting := os.Getenv(envRestartWait) != ""
	os.Unsetenv(envRestartWait)
	deadline := time.Now().Add(30 * time.Second)
	for {
		lock, other, err := serverlock.Acquire(dataDir, info)
		if !waiting || !errors.Is(err, serverlock.ErrHeld) || time.Now().After(deadline) {
			return lock, other, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// selfBinary is this program's real path, through any symlink.
func selfBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real
	}
	return exe
}

// restartViaService asks the service manager to restart the service, from a
// process of its own: the manager stops this one along the way, and a child
// in the same process group would go with it.
func restartViaService(exe string) error {
	if _, err := service.ForThisMachine(); err != nil {
		return err
	}
	cmd := exec.Command(exe, "service", "restart")
	cmd.Env = os.Environ()
	return startDetached(cmd, "")
}

// startUpdate runs `iql update --yes` as a process that outlives this one,
// writing its output to logPath.
//
// Under a service manager that matters: the updater stops the service, and
// the service is this process. On Linux a systemd user unit's stop takes its
// whole cgroup, so the updater is started as a transient unit of its own.
func startUpdate(exe, mode, dataDir, logPath string, done func(error)) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	fmt.Fprintf(f, "iql update — started %s from the settings page (%s mode)\n\n", time.Now().Format(time.RFC1123), mode)

	var cmd *exec.Cmd
	if mode == "service" && runtime.GOOS == "linux" {
		if run, err := exec.LookPath("systemd-run"); err == nil {
			cmd = exec.Command(run, "--user", "--collect", "--quiet", "--wait", "--pipe",
				"--unit=inboxql-update-"+fmt.Sprint(time.Now().Unix()), exe, "update", "--yes", "--no-color", "--data", dataDir)
		}
	}
	if cmd == nil {
		// --data, so the mailbox backed up is the one this server serves
		// even when it was started with a flag the updater would not see.
		cmd = exec.Command(exe, "update", "--yes", "--no-color", "--data", dataDir)
	}
	cmd.Env = os.Environ()
	cmd.Stdout, cmd.Stderr = f, f
	if err := startDetached(cmd, ""); err != nil {
		f.Close()
		return err
	}
	go func() {
		err := cmd.Wait()
		f.Close()
		done(err)
	}()
	return nil
}

// flagWasSet reports whether a flag was given, as opposed to defaulted.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
