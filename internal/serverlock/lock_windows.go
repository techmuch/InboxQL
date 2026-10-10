//go:build windows

package serverlock

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset is where the locked byte sits: far past anything written.
//
// Windows locks are byte ranges, and a locked range cannot be read through any
// other handle. Locking byte 0 would make the server's own description of
// itself unreadable for exactly as long as it is running — which is the only
// time anybody wants to read it.
const lockOffset = 1 << 30

func overlapped() *windows.Overlapped {
	return &windows.Overlapped{Offset: lockOffset}
}

func tryLock(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped())
}

func unlock(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlapped())
}
