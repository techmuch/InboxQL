// Package modelfetch downloads model weights from HuggingFace, verified.
//
// Extracted when a second engine needed the same thing. The part worth sharing
// is not the HTTP — it is the verification: HuggingFace serves large files
// through LFS and publishes their SHA-256 in X-Linked-Etag, so a download can
// be checked against what the repository says it should be rather than merely
// against having arrived.
//
// That catches a truncated transfer, a proxy that mangled the body, and a file
// that changed under a tag. None are exotic, all would otherwise surface as an
// inscrutable failure while parsing a gigabyte of graph, and a second copy of
// the check is a second place for it to quietly stop being done.
package modelfetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Progress reports how a download is going. `total` is 0 when unknown.
type Progress func(file string, done, total int64)

// URL is where a file lives in a repository.
func URL(repo, path string) string {
	return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repo, path)
}

// Download fetches one file to dst, checking it against the published digest.
//
// A missing header is not an error. It means only that the check could not be
// made, which is the honest state for a host that does not publish one; the
// caller still hashes what landed and records it, so what was installed stays
// known.
func Download(ctx context.Context, client *http.Client, url, dst, name string, progress Progress) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", url, resp.Status)
	}
	want := strings.Trim(resp.Header.Get("X-Linked-Etag"), `"`)

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	sum := sha256.New()
	total := resp.ContentLength
	if n, err := strconv.ParseInt(resp.Header.Get("X-Linked-Size"), 10, 64); err == nil && n > 0 {
		// The body is the LFS object, so its length is the linked size rather
		// than the pointer file's.
		total = n
	}
	var done int64
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			sum.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(name, done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("downloading %s: %w", name, readErr)
		}
	}

	if got := hex.EncodeToString(sum.Sum(nil)); want != "" && got != want {
		return fmt.Errorf(
			"%s does not match the checksum the repository publishes.\n"+
				"  expected %s\n  received %s\n"+
				"The download was corrupted, or the file changed. Try again.",
			name, want, got)
	}
	return f.Sync()
}

// DigestFile returns the SHA-256 of a file on disk.
func DigestFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
