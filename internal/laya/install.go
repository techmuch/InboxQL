package laya

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/user/inboxql/internal/modelfetch"
)

// # Getting the weights
//
// Not in the binary, for the same reason the span model's are not: a gigabyte
// of weights in a Go executable is not a single-binary story, it is a bad one,
// and most people will never turn this on.
//
// What arrives is a published export, which is then widened on this machine —
// see [Prepare]. Shipping a pre-prepared file from somewhere else would mean
// asking people to trust a model binary from a host with no standing to be
// trusted; downloading what the publisher published and transforming it here
// is both more honest and easier to check.

// DefaultRepo is the checkpoint this installs.
//
// The multilingual one rather than the English one, which is not the obvious
// choice and is the right one twice over: it is smaller (322M against 421M),
// it is three times faster on CPU in the project's own benchmarks, it takes a
// 1024-token context rather than 512 — and mail is not reliably English.
//
// This is a community ONNX export rather than an upstream one, because upstream
// publishes safetensors. The checksum is verified against what the repository
// publishes and the digest of what actually ran is recorded on every
// annotation, so a result can always be traced to the file that produced it.
const DefaultRepo = "mizchi/laya-multilingual-onnx"

// Source is one file to fetch.
type Source struct {
	Remote   string
	Local    string
	Prepared bool
}

// Sources are the files a working model needs.
var Sources = []Source{
	{Remote: "model.onnx", Local: "model.onnx", Prepared: true},
	{Remote: "tokenizer/tokenizer.json", Local: "tokenizer.json"},
}

// Progress reports how a download is going.
type Progress = modelfetch.Progress

// Install downloads and prepares a model into a data directory.
//
// Safe to re-run: each file is fetched to a temporary name and moved into place
// only once it is complete, so an interrupted install leaves the previous model
// — or nothing — rather than a truncated one that fails confusingly later.
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
		tmp := filepath.Join(dir, src.Local+".part")
		if err := modelfetch.Download(ctx, client,
			modelfetch.URL(repo, src.Remote), tmp, src.Local, progress); err != nil {
			os.Remove(tmp)
			return err
		}

		if src.Prepared {
			// Widened into a second temporary rather than in place, so a
			// failure here does not leave a half-converted graph behind.
			out := filepath.Join(dir, src.Local+".prepared")
			if err := Prepare(tmp, out); err != nil {
				os.Remove(tmp)
				os.Remove(out)
				return fmt.Errorf("preparing %s: %w", src.Local, err)
			}
			os.Remove(tmp)
			tmp = out
		}
		if err := os.Rename(tmp, filepath.Join(dir, src.Local)); err != nil {
			return fmt.Errorf("installing %s: %w", src.Local, err)
		}
	}
	return WriteCard(dataDir, repo)
}

// Remove deletes an installed model.
func Remove(dataDir string) error {
	return os.RemoveAll(Dir(dataDir))
}
