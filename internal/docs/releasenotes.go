package docs

import (
	"strings"

	inboxql "github.com/user/inboxql"
)

// ReleaseNotes is the Markdown under one release's heading in
// docs/release-notes.md — what changed in it — without the heading itself,
// or false when the release has no entry (a build from source between
// releases, say).
func ReleaseNotes(version string) (string, bool) {
	b, err := inboxql.Docs.ReadFile("docs/release-notes.md")
	if err != nil {
		return "", false
	}
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	var out []string
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if i := strings.Index(title, " {#"); i >= 0 {
				title = title[:i]
			}
			in = title == version
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	if !in && len(out) == 0 {
		return "", false
	}
	return strings.TrimSpace(strings.Join(out, "\n")), true
}
