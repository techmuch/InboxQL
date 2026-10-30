package docs

import (
	"regexp"
	"strings"
	"testing"
)

// Every release has its own heading, and its anchor is the version with
// hyphens — the address the version number in the application opens. The
// automatic anchor drops the dots, which would make 2026.1.11.1 and
// 2026.11.1.1 the same place.
func TestReleaseNotesNameEveryReleaseByItsOwnAnchor(t *testing.T) {
	p, err := Render("release-notes", LinkStyle{Guide: func(s, f string) string { return s + "#" + f }})
	if err != nil {
		t.Fatal(err)
	}
	version := regexp.MustCompile(`^(\d{4})\.(\d+)\.(\d+)\.(\d+)$`)
	seen := map[string]bool{}
	n := 0
	for _, h := range p.Headings {
		m := version.FindStringSubmatch(h.Text)
		if m == nil {
			continue
		}
		n++
		want := "v" + m[1] + "-" + m[2] + "-" + m[3] + "-" + m[4]
		if h.ID != want {
			t.Errorf("%s has anchor %q, want %q", h.Text, h.ID, want)
		}
		if seen[h.ID] {
			t.Errorf("two releases share the anchor %q", h.ID)
		}
		seen[h.ID] = true
	}
	if n < 89 {
		t.Errorf("%d releases have notes; every one should", n)
	}
}

// The notes for one release, as `iql version --notes` prints them and the
// release workflow publishes them.
func TestReleaseNotesForOneRelease(t *testing.T) {
	notes, ok := ReleaseNotes("2026.10.29.2")
	if !ok || !strings.Contains(notes, "was 0.0.112") || strings.Contains(notes, "## ") {
		t.Errorf("notes for 2026.10.29.2: %v %q", ok, notes)
	}
	if notes, ok := ReleaseNotes("v2026.8.23.1"); !ok || !strings.Contains(notes, "was 0.0.1*") {
		t.Errorf("a v prefix, and the first release: %v %q", ok, notes)
	}
	if _, ok := ReleaseNotes("1999.1.1.1"); ok {
		t.Error("a release with no entry was found")
	}
}
