package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/update"
)

func releaseServer(t *testing.T, rel update.Release) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(rel)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("INBOXQL_RELEASES_URL", srv.URL)
}

func TestUpdateCheckReportsANewerRelease(t *testing.T) {
	withHome(t, nil)
	releaseServer(t, update.Release{Tag: "v99.0.0", URL: "https://example.test/r"})

	var out bytes.Buffer
	if code := Execute([]string{"--json", "update", "--check"}, nil, &out, &bytes.Buffer{}); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got map[string]any
	json.Unmarshal(out.Bytes(), &got)
	if got["newer"] != true || got["latest"] != "99.0.0" {
		t.Errorf("got %v", got)
	}
}

func TestUpdateSaysWhenUpToDate(t *testing.T) {
	withHome(t, nil)
	releaseServer(t, update.Release{Tag: "v0.0.1"})

	var out bytes.Buffer
	Execute([]string{"update"}, nil, &out, &bytes.Buffer{})
	if !strings.Contains(out.String(), "Up to date") {
		t.Errorf("output %q", out.String())
	}
}

// A release without checksums is refused before anything is downloaded or
// replaced — the check exists for exactly the case where something is wrong.
func TestUpdateRefusesAReleaseWithoutChecksums(t *testing.T) {
	withHome(t, nil)
	name, err := update.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	releaseServer(t, update.Release{Tag: "v99.0.0", Assets: []update.Asset{{Name: name, URL: "http://unused"}}})

	var errOut bytes.Buffer
	code := Execute([]string{"update", "--yes"}, nil, &bytes.Buffer{}, &errOut)
	if code == ExitOK || !strings.Contains(errOut.String(), "unverified") {
		t.Errorf("exit %d, %q; want a refusal", code, errOut.String())
	}
}

// Not at a terminal, an update needs --yes: it replaces the binary and
// upgrades the mailbox, and a script should say so explicitly.
func TestUpdateWithoutATerminalNeedsYes(t *testing.T) {
	withHome(t, nil)
	name, err := update.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	releaseServer(t, update.Release{Tag: "v99.0.0", Assets: []update.Asset{
		{Name: name, URL: "http://unused"}, {Name: "SHA256SUMS", URL: "http://unused"},
	}})

	var errOut bytes.Buffer
	code := Execute([]string{"update"}, strings.NewReader(""), &bytes.Buffer{}, &errOut)
	if code != ExitUsage || !strings.Contains(errOut.String(), "--yes") {
		t.Errorf("exit %d, %q", code, errOut.String())
	}
}
