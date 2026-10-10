// Package modelpath decides where a model's weights live.
//
// Weights are one to two gigabytes each, and they are the same whichever
// mailbox uses them. They used to live inside each data directory, so a second
// mailbox meant a second download. With machine settings they live once, in
// the machine's models folder.
//
// An existing per-mailbox copy is still used when the machine folder does not
// have one: nobody should have to download 1.2 GB again because a setting
// appeared.
package modelpath

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	mu   sync.RWMutex
	root string
)

// SetRoot sets the machine models folder, or clears it with "".
func SetRoot(dir string) {
	mu.Lock()
	defer mu.Unlock()
	root = dir
}

// For is the folder a model named name lives in, for a data directory.
//
// Order: the machine folder when it holds this model; the mailbox's own copy
// when it holds one; otherwise the machine folder (which is where an install
// will put it), or the mailbox when there are no machine settings.
func For(dataDir, name string) string {
	mu.RLock()
	r := root
	mu.RUnlock()

	local := filepath.Join(dataDir, "models", name)
	if r == "" {
		return local
	}
	machine := filepath.Join(r, name)
	if exists(machine) {
		return machine
	}
	if exists(local) {
		return local
	}
	return machine
}

func exists(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}
