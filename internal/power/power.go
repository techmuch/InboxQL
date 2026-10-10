// Package power answers one question: is this machine running on battery?
//
// Asked before starting heavy work nobody asked for — the annotation sweep
// after a sync can be twenty minutes of span extraction, which on a laptop's
// battery is a noticeable fraction of the afternoon.
//
// The answer can be "unknown", and unknown is treated as mains power: a desktop
// with no battery, a container, a platform this does not read. Refusing work
// whenever the question cannot be answered would stop it on exactly the
// machines that have no battery to save.
package power

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// State is what is known about the power source.
type State int

const (
	Unknown State = iota
	Mains
	Battery
)

func (s State) String() string {
	switch s {
	case Mains:
		return "mains"
	case Battery:
		return "battery"
	default:
		return "unknown"
	}
}

// Source reports the current power source.
func Source() State {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("pmset", "-g", "batt").Output()
		if err != nil {
			return Unknown
		}
		return parsePmset(string(out))
	case "linux":
		return readLinux("/sys/class/power_supply")
	case "windows":
		return windowsSource()
	default:
		return Unknown
	}
}

// OnBattery is Source() == Battery.
func OnBattery() bool { return Source() == Battery }

// parsePmset reads the first line of `pmset -g batt`:
//
//	Now drawing from 'Battery Power'
//	Now drawing from 'AC Power'
func parsePmset(out string) State {
	switch {
	case strings.Contains(out, "'Battery Power'"):
		return Battery
	case strings.Contains(out, "'AC Power'"):
		return Mains
	default:
		return Unknown
	}
}

// readLinux looks for a mains supply that is online. A machine with a battery
// and no online mains supply is on battery; one with no battery at all is on
// mains, whatever else it reports.
func readLinux(root string) State {
	entries, err := os.ReadDir(root)
	if err != nil {
		return Unknown
	}
	hasBattery, mainsOnline, sawMains := false, false, false
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		kind := readTrim(filepath.Join(dir, "type"))
		switch kind {
		case "Battery":
			hasBattery = true
		case "Mains", "USB", "USB_C", "USB_PD":
			sawMains = true
			if readTrim(filepath.Join(dir, "online")) == "1" {
				mainsOnline = true
			}
		}
	}
	switch {
	case mainsOnline:
		return Mains
	case hasBattery && sawMains:
		return Battery
	case hasBattery:
		// A battery and no mains supply described at all: some laptops
		// report only the battery. Its status says whether it is draining.
		return Unknown
	default:
		return Mains
	}
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
