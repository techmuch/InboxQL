package power

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPmset(t *testing.T) {
	cases := map[string]State{
		"Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t83%; discharging": Battery,
		"Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t100%; charged":         Mains,
		"": Unknown,
	}
	for in, want := range cases {
		if got := parsePmset(in); got != want {
			t.Errorf("parsePmset(%q) = %v, want %v", in, got, want)
		}
	}
}

func supply(t *testing.T, root, name, kind, online string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "type"), []byte(kind+"\n"), 0o644)
	if online != "" {
		os.WriteFile(filepath.Join(dir, "online"), []byte(online+"\n"), 0o644)
	}
}

func TestLinuxLaptopOnBattery(t *testing.T) {
	root := t.TempDir()
	supply(t, root, "AC", "Mains", "0")
	supply(t, root, "BAT0", "Battery", "")
	if got := readLinux(root); got != Battery {
		t.Errorf("got %v, want battery", got)
	}
}

func TestLinuxLaptopPluggedIn(t *testing.T) {
	root := t.TempDir()
	supply(t, root, "AC", "Mains", "1")
	supply(t, root, "BAT0", "Battery", "")
	if got := readLinux(root); got != Mains {
		t.Errorf("got %v, want mains", got)
	}
}

// A desktop has no battery to save; it is on mains whatever else it reports.
func TestLinuxDesktopIsMains(t *testing.T) {
	root := t.TempDir()
	if got := readLinux(root); got != Mains {
		t.Errorf("got %v, want mains", got)
	}
}
