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
	"strings"
	"testing"
	"time"
)

// serve stands in for the model host, publishing the checksum the way
// HuggingFace does: the SHA-256 of the LFS object in X-Linked-Etag.
func serve(t *testing.T, body []byte, etag string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestDownloadAcceptsAMatchingChecksum(t *testing.T) {
	body := []byte("the weights, such as they are")
	srv := serve(t, body, sha256Hex(body))
	dst := filepath.Join(t.TempDir(), "model.bin")

	if err := download(context.Background(), srv.Client(), srv.URL, dst, "model.bin", nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("file is %q, want %q", got, body)
	}
}

// The failure this exists to catch: a body that arrived intact as far as HTTP
// is concerned but is not the file the repository says it is. Without the
// check it surfaces much later, as an inscrutable parse error on 750MB.
func TestDownloadRejectsAChangedFile(t *testing.T) {
	published := []byte("what the repository says it serves")
	srv := serve(t, []byte("something else entirely"), sha256Hex(published))
	dst := filepath.Join(t.TempDir(), "model.bin")

	err := download(context.Background(), srv.Client(), srv.URL, dst, "model.bin", nil)
	if err == nil {
		t.Fatal("a file that does not match its published checksum was accepted")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error %q does not say what went wrong", err)
	}
}

// A host that publishes no checksum is not an error; it just means the check
// could not be made. Refusing would make the installer usable against exactly
// one host.
func TestDownloadWithoutAPublishedChecksum(t *testing.T) {
	body := []byte("unverifiable but fine")
	srv := serve(t, body, "")
	dst := filepath.Join(t.TempDir(), "model.bin")

	if err := download(context.Background(), srv.Client(), srv.URL, dst, "model.bin", nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != string(body) {
		t.Errorf("file is %q, want %q", got, body)
	}
}

// Progress is reported against the linked size, not the pointer file's
// Content-Length, or an 800MB download reports thousands of percent.
func TestDownloadReportsProgressAgainstTheLinkedSize(t *testing.T) {
	body := []byte(strings.Repeat("x", 4096))
	srv := serve(t, body, sha256Hex(body))
	dst := filepath.Join(t.TempDir(), "model.bin")

	var lastDone, lastTotal int64
	err := download(context.Background(), srv.Client(), srv.URL, dst, "model.bin",
		func(_ string, done, total int64) { lastDone, lastTotal = done, total })
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if lastTotal != int64(len(body)) {
		t.Errorf("total reported as %d, want %d", lastTotal, len(body))
	}
	if lastDone != int64(len(body)) {
		t.Errorf("finished at %d of %d", lastDone, lastTotal)
	}
}

func TestDownloadStopsWhenCancelled(t *testing.T) {
	body := []byte(strings.Repeat("x", 1<<20))
	srv := serve(t, body, sha256Hex(body))
	dst := filepath.Join(t.TempDir(), "model.bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := download(ctx, srv.Client(), srv.URL, dst, "model.bin", nil)
	if err == nil {
		t.Fatal("a cancelled download reported success")
	}
}

func TestDownloadReportsAnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	err := download(context.Background(), srv.Client(), srv.URL,
		filepath.Join(t.TempDir(), "model.bin"), "model.bin", nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want one naming the status", err)
	}
}

func TestInstallLeavesNoPartialFileBehind(t *testing.T) {
	// A body that will not survive the checksum check, so the install fails
	// after writing a temporary file.
	srv := serve(t, []byte("truncated"), sha256Hex([]byte("the whole thing")))
	dir := t.TempDir()

	client := &http.Client{Timeout: 10 * time.Second}
	err := download(context.Background(), client, srv.URL,
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
