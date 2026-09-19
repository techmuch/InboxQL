package gliner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// # Getting the weights
//
// They are not in the binary. A few hundred megabytes of model in a Go
// executable is not a single-binary story, it is a bad one, and most people
// running InboxQL will never turn this on. So they are downloaded once, on
// purpose, by a command that says what it is about to do.
//
// What arrives is a stock GLiNER export, which is then rewritten on this
// machine — see [Prepare]. The alternative, shipping a pre-rewritten file from
// somewhere, would mean asking people to trust a model binary from a host with
// no standing to be trusted. Downloading what the publisher published and
// transforming it here is both more honest and easier to check.

// DefaultRepo is the model this installs.
//
// gliner_base rather than one of the large variants: it is the size the spike
// was judged at, it reads a message in about three seconds on a laptop CPU,
// and the larger ones cost proportionally more for an accuracy difference
// nobody has measured on mail.
const DefaultRepo = "onnx-community/gliner_base"

// Source is one file to fetch.
type Source struct {
	// Remote is the path within the repository.
	Remote string
	// Local is the name it takes in the model directory.
	Local string
	// Prepared is true when the file must be rewritten after download.
	Prepared bool
}

// Sources are the files a working model needs.
var Sources = []Source{
	{Remote: "onnx/model.onnx", Local: "model.onnx", Prepared: true},
	{Remote: "spm.model", Local: "spm.model"},
}

// Progress reports how a download is going.
type Progress func(file string, done, total int64)

// Install downloads and prepares a model into a data directory.
//
// Safe to re-run: each file is fetched to a temporary name and moved into
// place only once it is complete, so an interrupted install leaves the
// previous model — or nothing — rather than a truncated one that fails
// confusingly later.
func Install(ctx context.Context, dataDir, repo string, progress Progress) error {
	if repo == "" {
		repo = DefaultRepo
	}
	dir := Dir(dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", dir, err)
	}

	client := &http.Client{Timeout: 60 * time.Minute}
	for _, src := range Sources {
		url := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repo, src.Remote)
		tmp := filepath.Join(dir, src.Local+".part")
		if err := download(ctx, client, url, tmp, src.Local, progress); err != nil {
			os.Remove(tmp)
			return err
		}

		final := filepath.Join(dir, src.Local)
		if src.Prepared {
			// Rewritten into a second temporary rather than in place, so a
			// failure here does not leave a half-edited graph behind.
			out := filepath.Join(dir, src.Local+".prepared")
			if err := Prepare(tmp, out); err != nil {
				os.Remove(tmp)
				os.Remove(out)
				return err
			}
			os.Remove(tmp)
			tmp = out
		}
		if err := os.Rename(tmp, final); err != nil {
			return fmt.Errorf("installing %s: %w", src.Local, err)
		}
	}
	return nil
}

func download(ctx context.Context, client *http.Client, url, dst, name string, progress Progress) error {
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

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
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
	return f.Sync()
}

// Digest returns the SHA-256 of an installed file.
//
// Recorded alongside a run so a surprising result can always be traced to the
// weights that produced it — the same reason OCR text is marked as OCR.
func Digest(dataDir, name string) (string, error) {
	f, err := os.Open(filepath.Join(Dir(dataDir), name))
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
