package gliner

import (
	"os"
	"path/filepath"
	"testing"
)

// The name goes into annotations.model on every record, so it has to answer
// "which weights produced this", not just "which engine".
func TestNameCarriesProvenance(t *testing.T) {
	for _, tc := range []struct {
		name string
		card Card
		want string
	}{
		{
			name: "installed",
			card: Card{Repo: "onnx-community/gliner_base", Digest: "bc87c9602a588f2212345678"},
			want: "gliner onnx-community/gliner_base@bc87c9602a58",
		},
		{
			name: "repo but no digest",
			card: Card{Repo: "onnx-community/gliner_base"},
			want: "gliner onnx-community/gliner_base",
		},
		{
			// Dropped into the directory by hand. It may work perfectly; it
			// just cannot be traced, and saying so is better than a confident
			// name that identifies nothing.
			name: "no card",
			card: Card{},
			want: "gliner (unrecorded)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{card: tc.card}
			if got := m.Name(); got != tc.want {
				t.Errorf("Name() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCardRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if got := ReadCard(dir); got.Repo != "" || got.Digest != "" {
		t.Errorf("an empty directory gave a card: %+v", got)
	}

	if err := os.MkdirAll(Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(dir), "model.onnx"), []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeCard(dir, "some/repo"); err != nil {
		t.Fatal(err)
	}

	got := ReadCard(dir)
	if got.Repo != "some/repo" {
		t.Errorf("repo = %q, want some/repo", got.Repo)
	}
	// The digest is of the file, so it must match hashing it directly.
	want, err := Digest(dir, "model.onnx")
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != want {
		t.Errorf("digest = %q, want %q", got.Digest, want)
	}
	if got.Installed.IsZero() {
		t.Error("the card records no install time")
	}
}
