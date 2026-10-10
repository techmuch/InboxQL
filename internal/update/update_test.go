package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarGz(t *testing.T, name string, body []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// The release workflow archives `./*`, so names arrive with a ./ prefix.
	tw.WriteHeader(&tar.Header{Name: "./readme.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: "./" + name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.0", "0.0.75", true},
		{"v0.0.76", "0.0.75", true},
		{"0.0.75", "0.0.75", false},
		{"0.0.74", "0.0.75", false},
		{"1.2", "1.2.0", false},
		{"0.0.76", "dev", true},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVerifyRefusesAMismatchAndAMissingEntry(t *testing.T) {
	data := []byte("the archive")
	sums := []byte(sum(data) + "  iql-linux-amd64.tar.gz\n" + strings.Repeat("0", 64) + "  other.zip\n")
	if err := Verify(sums, "iql-linux-amd64.tar.gz", data); err != nil {
		t.Errorf("a matching archive was refused: %v", err)
	}
	if err := Verify(sums, "iql-linux-amd64.tar.gz", []byte("tampered")); err == nil {
		t.Error("a tampered archive was accepted")
	}
	if err := Verify(sums, "iql-darwin-universal.tar.gz", data); err == nil {
		t.Error("an archive with no checksum entry was accepted")
	}
}

func TestExtractFindsTheBinaryInEitherArchive(t *testing.T) {
	got, err := Extract(tarGz(t, "iql", []byte("binary")), "iql-linux-amd64.tar.gz", "iql")
	if err != nil || string(got) != "binary" {
		t.Errorf("tar.gz: %q, %v", got, err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("iql.exe")
	w.Write([]byte("exe"))
	zw.Close()
	got, err = Extract(buf.Bytes(), "iql-windows-amd64.zip", "iql.exe")
	if err != nil || string(got) != "exe" {
		t.Errorf("zip: %q, %v", got, err)
	}
}

func TestLatestReadsThePublishedRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{Tag: "v0.1.0", Assets: []Asset{{Name: "SHA256SUMS", URL: "x"}}})
	}))
	defer srv.Close()
	t.Setenv("INBOXQL_RELEASES_URL", srv.URL)

	r, err := Latest()
	if err != nil {
		t.Fatal(err)
	}
	if r.Version() != "0.1.0" {
		t.Errorf("version %q", r.Version())
	}
	if _, err := r.Find("SHA256SUMS"); err != nil {
		t.Error(err)
	}
}

func TestReplaceSwapsTheBinary(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "iql")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Replace(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("binary is %q", b)
	}
	if fi, _ := os.Stat(exe); fi.Mode()&0o111 == 0 {
		t.Error("the new binary is not executable")
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Error("the old binary was left behind")
	}
}

func TestEveryReleasedPlatformHasAnAsset(t *testing.T) {
	for _, p := range [][2]string{{"darwin", "arm64"}, {"darwin", "amd64"}, {"linux", "amd64"}, {"windows", "amd64"}} {
		if _, err := AssetName(p[0], p[1]); err != nil {
			t.Errorf("%s/%s: %v", p[0], p[1], err)
		}
	}
	if _, err := AssetName("linux", "arm64"); err == nil {
		t.Error("linux/arm64 has no release build and should say so")
	}
}
