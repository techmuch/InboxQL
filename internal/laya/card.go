package laya

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/user/inboxql/internal/modelfetch"
)

// # Provenance
//
// A result has to name what produced it, specifically enough that somebody
// reading a surprising answer can tell which weights said it. The first version
// of the span engine reported itself as "gliner (gliner)", which traced every
// record it ever wrote to nothing at all — a stored answer whose author is
// unidentifiable is not evidence.
//
// So the repository and a digest of the file are written at install time and
// carried onto every annotation.

// Card records which weights are installed.
type Card struct {
	Repo      string    `json:"repo"`
	Digest    string    `json:"digest"`
	Installed time.Time `json:"installed"`
}

const cardFile = "model.json"

// ReadCard reads the installed card, or a zero one when there is none.
func ReadCard(dataDir string) Card {
	var c Card
	blob, err := os.ReadFile(filepath.Join(Dir(dataDir), cardFile))
	if err != nil {
		return c
	}
	_ = json.Unmarshal(blob, &c)
	return c
}

// WriteCard records what was installed.
func WriteCard(dataDir, repo string) error {
	digest, err := Digest(dataDir, "model.onnx")
	if err != nil {
		return fmt.Errorf("checksumming the model: %w", err)
	}
	blob, err := json.MarshalIndent(
		Card{Repo: repo, Digest: digest, Installed: time.Now()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Dir(dataDir), cardFile), blob, 0o644)
}

// Digest is the SHA-256 of one installed file.
func Digest(dataDir, name string) (string, error) {
	return modelfetch.DigestFile(filepath.Join(Dir(dataDir), name))
}

// Name identifies the model for provenance, as stored on every annotation.
//
// The digest is the prepared file's, not the published one's: what ran is what
// should be named. The two differ here — the published export is float16 and is
// widened at install — so quoting the upstream checksum would name a file this
// process never executed.
func (m *Model) Name() string {
	switch {
	case m.card.Repo != "" && m.card.Digest != "":
		return fmt.Sprintf("laya %s@%s", m.card.Repo, m.card.Digest[:12])
	case m.card.Repo != "":
		return "laya " + m.card.Repo
	default:
		return "laya (unrecorded)"
	}
}
