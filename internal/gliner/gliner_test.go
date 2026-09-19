package gliner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitWordsKeepsOffsets(t *testing.T) {
	text := "  Invoice #605845319\tfrom\nCoolray  "
	words := SplitWords(text)

	want := []string{"Invoice", "#605845319", "from", "Coolray"}
	if len(words) != len(want) {
		t.Fatalf("got %d words, want %d: %+v", len(words), len(want), words)
	}
	for i, w := range words {
		if w.Text != want[i] {
			t.Errorf("word %d = %q, want %q", i, w.Text, want[i])
		}
		// The offsets are the whole point: a span is only as good as its
		// ability to point back at the characters a human would highlight.
		if got := text[w.Start:w.End]; got != w.Text {
			t.Errorf("word %d: text[%d:%d] = %q, but the word is %q",
				i, w.Start, w.End, got, w.Text)
		}
	}
}

func TestSplitWordsHandlesEdges(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"all space", " \t\n ", 0},
		{"no space", "single", 1},
		{"leading space", "  a b", 2},
		{"trailing space", "a b  ", 2},
		{"unicode", "café — naïve", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			words := SplitWords(tc.text)
			if len(words) != tc.want {
				t.Errorf("got %d words, want %d: %+v", len(words), tc.want, words)
			}
			for _, w := range words {
				if tc.text[w.Start:w.End] != w.Text {
					t.Errorf("offsets do not round-trip for %q", w.Text)
				}
			}
		})
	}
}

// A multi-byte word must not be cut mid-rune, which is what would happen if
// the splitter counted runes where it should count bytes.
func TestSplitWordsIsByteAccurate(t *testing.T) {
	text := "montré à café"
	for _, w := range SplitWords(text) {
		if !strings.Contains(text, w.Text) {
			t.Errorf("word %q is not a substring of the source", w.Text)
		}
		if text[w.Start:w.End] != w.Text {
			t.Errorf("text[%d:%d] = %q, want %q", w.Start, w.End, text[w.Start:w.End], w.Text)
		}
	}
}

func TestInstalledRequiresEveryFile(t *testing.T) {
	dir := t.TempDir()
	if Installed(dir) {
		t.Error("an empty directory reported as installed")
	}

	modelDir := Dir(dir)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// One file present is not an installation, and reporting it as one turns a
	// clear "not installed" into a confusing failure later.
	if err := os.WriteFile(filepath.Join(modelDir, "spm.model"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Installed(dir) {
		t.Error("a half-installed directory reported as installed")
	}

	// An empty file is not a model either: an interrupted download leaves one.
	if err := os.WriteFile(filepath.Join(modelDir, "model.onnx"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if Installed(dir) {
		t.Error("a zero-length model reported as installed")
	}

	if err := os.WriteFile(filepath.Join(modelDir, "model.onnx"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Installed(dir) {
		t.Error("a complete directory did not report as installed")
	}
}

func TestOpenSaysWhatIsMissing(t *testing.T) {
	_, err := Open(t.TempDir())
	if err == nil {
		t.Fatal("opening an empty data directory succeeded")
	}
	// The error has to carry the way out, because "no such file" for a path
	// the user never chose is not actionable.
	if !strings.Contains(err.Error(), "iql gliner install") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}

func TestFlattenPrefersHigherScoresAndRefusesOverlap(t *testing.T) {
	text := "pay Coolray Heating today"
	window := SplitWords(text)

	// "Coolray Heating" (words 1-2) against "Heating" (word 2): the longer,
	// better-scoring span must win and the weaker one must not also appear,
	// or the same words come back as two entities.
	cands := []Span{
		{Label: "vendor", Start: window[1].Start, End: window[2].End, Score: 0.9},
		{Label: "vendor", Start: window[2].Start, End: window[2].End, Score: 0.6},
		{Label: "date", Start: window[3].Start, End: window[3].End, Score: 0.8},
	}

	kept := flatten(cands, window, text)
	if len(kept) != 2 {
		t.Fatalf("kept %d spans, want 2: %+v", len(kept), kept)
	}
	if kept[0].Text != "Coolray Heating" || kept[0].Score != 0.9 {
		t.Errorf("first span = %+v, want the higher-scoring pair", kept[0])
	}
	if kept[1].Text != "today" {
		t.Errorf("second span = %+v, want the non-overlapping one", kept[1])
	}
}

// Every span's text must be a slice of the source at its own offsets. This is
// the property the package exists for, so it is asserted rather than assumed.
func TestFlattenTakesTextFromTheSource(t *testing.T) {
	text := "invoice 605845319 due"
	window := SplitWords(text)
	kept := flatten([]Span{
		{Label: "invoice number", Start: window[1].Start, End: window[1].End, Score: 0.7},
	}, window, text)

	if len(kept) != 1 {
		t.Fatalf("kept %d spans, want 1", len(kept))
	}
	if kept[0].Text != text[kept[0].Start:kept[0].End] {
		t.Errorf("span text %q is not text[%d:%d] = %q",
			kept[0].Text, kept[0].Start, kept[0].End, text[kept[0].Start:kept[0].End])
	}
	if kept[0].Text != "605845319" {
		t.Errorf("span text = %q, want %q", kept[0].Text, "605845319")
	}
}
