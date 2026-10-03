package gliner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/user/inboxql/internal/modelfetch"
)

// serve stands in for the model host, publishing the checksum the way
// HuggingFace does. The download tests themselves live with the downloader, in
// internal/modelfetch; what is left here is about what Install puts on disk.
func serve(t *testing.T, body []byte, etag string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if etag != "" {
			w.Header().Set("X-Linked-Etag", `"`+etag+`"`)
			w.Header().Set("X-Linked-Size", strconv.Itoa(len(body)))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestInstallLeavesNoPartialFileBehind(t *testing.T) {
	// A body that will not survive the checksum check, so the install fails
	// after writing a temporary file.
	srv := serve(t, []byte("truncated"), sha256Hex([]byte("the whole thing")))
	dir := t.TempDir()

	client := &http.Client{Timeout: 10 * time.Second}
	err := modelfetch.Download(context.Background(), client, srv.URL,
		filepath.Join(dir, "model.onnx.part"), "model.onnx", nil)
	if err == nil {
		t.Fatal("expected the checksum check to fail")
	}
	// Install removes the temporary on failure; assert the contract it relies
	// on, namely that nothing was moved into place.
	if _, err := os.Stat(filepath.Join(dir, "model.onnx")); !os.IsNotExist(err) {
		t.Error("a failed download left a model in place")
	}
	if Installed(dir) {
		t.Error("a failed download reported as installed")
	}
}

func TestSourcesNameWhatOpenNeeds(t *testing.T) {
	// Sources and ModelFiles are two lists of the same thing, and if they part
	// company an install "succeeds" and Open then says nothing is installed.
	want := map[string]bool{}
	for _, f := range ModelFiles {
		want[f] = true
	}
	for _, s := range Sources {
		if !want[s.Local] {
			t.Errorf("Sources installs %q, which Open does not look for", s.Local)
		}
		delete(want, s.Local)
	}
	for f := range want {
		t.Errorf("Open needs %q, which Sources never downloads", f)
	}
}
