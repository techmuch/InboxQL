package gliner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	return writeCard(dataDir, repo)
}

// Card is what was installed, written beside the weights.
//
// Its job is provenance. Every annotation records the model that produced it,
// and "gliner" is not an answer to which model — two runs months apart may
// have been different weights. The digest is computed once, here, rather than
// on every Open, because hashing 750 MB to start a job that is about to hash
// nothing else is a second of work for a string that cannot have changed.
type Card struct {
	Repo      string    `json:"repo"`
	Digest    string    `json:"digest"`
	Installed time.Time `json:"installed"`
}

const cardFile = "model.json"

func writeCard(dataDir, repo string) error {
	digest, err := Digest(dataDir, "model.onnx")
	if err != nil {
		return fmt.Errorf("checksumming the model: %w", err)
	}
	card := Card{Repo: repo, Digest: digest, Installed: time.Now()}
	blob, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Dir(dataDir), cardFile), blob, 0o644)
}

// ReadCard reports what is installed. The zero Card means "nothing recorded",
// which is what a model put there by hand looks like.
func ReadCard(dataDir string) Card {
	var card Card
	blob, err := os.ReadFile(filepath.Join(Dir(dataDir), cardFile))
	if err != nil {
		return card
	}
	_ = json.Unmarshal(blob, &card)
	return card
}

// download fetches one file, checking it against the digest the host publishes.
//
// HuggingFace serves large files through LFS and puts their SHA-256 in
// X-Linked-Etag, so the file can be verified against what the repository says
// it should be rather than merely against having arrived. That catches a
// truncated transfer, a proxy that mangled the body, and a file that changed
// under a tag — none of which are exotic, and all of which would otherwise
// surface as an inscrutable failure while parsing a 750 MB graph.
//
// A missing header is not an error. Only that the check could not be made,
// which is the honest state for a host that does not publish one; the file is
// still hashed afterwards and recorded, so what was installed stays known.
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
