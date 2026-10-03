package modelfetch

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
)

// # Why these moved here
//
// They were the span engine's, back when it was the only thing that downloaded
// weights. The verification they cover is the part worth sharing between
// engines — and a second copy of a checksum check is a second place for it to
// quietly stop being done — so the code moved and the tests came with it.

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

	if err := Download(context.Background(), srv.Client(), srv.URL, dst, "model.bin", nil); err != nil {
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

	err := Download(context.Background(), srv.Client(), srv.URL, dst, "model.bin", nil)
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

	if err := Download(context.Background(), srv.Client(), srv.URL, dst, "model.bin", nil); err != nil {
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
	err := Download(context.Background(), srv.Client(), srv.URL, dst, "model.bin",
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
	err := Download(ctx, srv.Client(), srv.URL, dst, "model.bin", nil)
	if err == nil {
		t.Fatal("a cancelled download reported success")
	}
}

func TestDownloadReportsAnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	err := Download(context.Background(), srv.Client(), srv.URL,
		filepath.Join(t.TempDir(), "model.bin"), "model.bin", nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want one naming the status", err)
	}
}
