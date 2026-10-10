// Package update fetches a newer InboxQL release and verifies it.
//
// It never applies anything on its own. Finding, downloading and verifying are
// here; replacing the binary, and the order the mailbox is backed up and the
// service stopped in, belong to the command that a person runs and confirms.
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// Repository is where releases are published.
const Repository = "techmuch/InboxQL"

// LatestURL is the GitHub API endpoint for the newest published release.
//
// Drafts and pre-releases are not returned by it, which is what is wanted: a
// release becomes something `iql update` offers when somebody publishes it.
// $INBOXQL_RELEASES_URL replaces it, for tests and for mirrors.
func LatestURL() string {
	if u := strings.TrimSpace(os.Getenv("INBOXQL_RELEASES_URL")); u != "" {
		return u
	}
	return "https://api.github.com/repos/" + Repository + "/releases/latest"
}

// Release is a published release.
type Release struct {
	Tag    string  `json:"tag_name"`
	Name   string  `json:"name"`
	URL    string  `json:"html_url"`
	Assets []Asset `json:"assets"`
}

// Asset is one downloadable file of a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Version is the release's version, without the leading v.
func (r *Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

var client = &http.Client{Timeout: 5 * time.Minute}

// Latest asks for the newest published release.
func Latest() (*Release, error) {
	req, err := http.NewRequest(http.MethodGet, LatestURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking for the latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("no published release yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asking for the latest release: %s", resp.Status)
	}
	r := &Release{}
	if err := json.NewDecoder(resp.Body).Decode(r); err != nil {
		return nil, fmt.Errorf("reading the release: %w", err)
	}
	return r, nil
}

// AssetName is the archive built for a platform, as the release workflow
// names it.
func AssetName(goos, goarch string) (string, error) {
	switch {
	case goos == "darwin":
		// One universal binary serves both architectures.
		return "iql-darwin-universal.tar.gz", nil
	case goos == "linux" && goarch == "amd64":
		return "iql-linux-amd64.tar.gz", nil
	case goos == "windows" && goarch == "amd64":
		return "iql-windows-amd64.zip", nil
	default:
		return "", fmt.Errorf("no release is built for %s/%s; build from source", goos, goarch)
	}
}

// Find returns the named asset.
func (r *Release) Find(name string) (*Asset, error) {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("release %s has no %s", r.Tag, name)
}

// Newer reports whether version a is newer than b. Versions are dotted
// numbers; anything that does not parse compares as older, so a build with
// no real version is always offered the release.
func Newer(a, b string) bool {
	pa, pb := parts(a), parts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func parts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// Download fetches an asset into memory.
//
// In memory because it is verified before any of it touches the disk where the
// binary lives; release archives are tens of megabytes.
func Download(a *Asset) ([]byte, error) {
	resp, err := client.Get(a.URL)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", a.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", a.Name, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}

// Verify checks data against the line for name in a SHA256SUMS file.
//
// A release with no SHA256SUMS, or none for this file, is refused rather than
// trusted: the check exists for the case where something is wrong, and
// skipping it when the evidence is missing is skipping it exactly then.
func Verify(sums []byte, name string, data []byte) error {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 || strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != strings.ToLower(f[0]) {
			return fmt.Errorf("%s does not match its published checksum — not installing it", name)
		}
		return nil
	}
	return fmt.Errorf("SHA256SUMS has no entry for %s — not installing an unverified binary", name)
}

// Extract returns the iql binary inside a release archive.
func Extract(archive []byte, assetName, binary string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) == binary {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s has no %s", assetName, binary)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s has no %s", assetName, binary)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == binary {
			return io.ReadAll(tr)
		}
	}
}

// Replace puts a new binary where the running one is.
//
// The new file is written beside the old one and renamed over it, so the
// binary is never half-written. The old one is moved aside first rather than
// overwritten, because Windows will not replace a running executable but will
// rename one; it is removed afterwards where the platform allows, and left as
// iql.old where it does not.
func Replace(exe string, data []byte) error {
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("writing the new binary: %w", err)
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("moving the old binary aside: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		// Put the old one back: a missing binary is worse than an old one.
		_ = os.Rename(old, exe)
		return fmt.Errorf("putting the new binary in place: %w", err)
	}
	_ = os.Remove(old)
	return nil
}
