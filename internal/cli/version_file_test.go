package cli

import (
	"strings"
	"testing"

	inboxql "github.com/user/inboxql"
)

// The version is written in two places only because the linker needs a
// literal; they must never say different things.
func TestVersionMatchesTheVersionFile(t *testing.T) {
	if want := strings.TrimSpace(inboxql.VersionFile); Version != want {
		t.Errorf("cli.Version is %q but VERSION says %q; run scripts/version.sh", Version, want)
	}
}
