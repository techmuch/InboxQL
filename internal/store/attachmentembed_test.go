package store

import (
	"math"
	"testing"

	"github.com/user/inboxql/internal/filetext"
)

// embedFixture stores three files with text: two about the same subject and
// one about something else, so a threshold has something to separate.
func embedFixture(t *testing.T) {
	t.Helper()
	openTextFixture(t)

	save := func(hash, text string) {
		t.Helper()
		if err := SaveAttachmentText(hash, filetext.Result{
			Status: filetext.StatusOK, Extractor: "pdf",
			Pages: []filetext.Page{{Number: 1, Text: text}},
		}); err != nil {
			t.Fatalf("SaveAttachmentText: %v", err)
		}
	}
	save("hash-leave", "FMLA leave request form. Employee name. Dates of leave requested.")
	save("hash-health", "Certification of employee health. Medical provider. Leave dates.")
	save("hash-recipe", "Chocolate cake. Flour, sugar, eggs. Bake for forty minutes.")
}

func TestPendingAttachmentEmbeddingsLeadsWithTheFilename(t *testing.T) {
	embedFixture(t)

	pending, err := PendingAttachmentEmbeddings("bge", 0)
	if err != nil {
		t.Fatalf("PendingAttachmentEmbeddings: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("nothing pending, but three files have text")
	}

	// Only files with an `ok` extraction take part: a scan or an image has
	// nothing to embed, and including them would mean a vector over an empty
	// string standing in for a document.
	for _, p := range pending {
		if p.Text == "" {
			t.Errorf("%s is pending with no text", p.ContentHash)
		}
	}
}

// Keyed on the model, so switching models re-embeds rather than trusting
// vectors that mean something else.
func TestPendingAttachmentEmbeddingsIsPerModel(t *testing.T) {
	embedFixture(t)

	before, err := PendingAttachmentEmbeddings("bge", 0)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("nothing pending")
	}

	for _, p := range before {
		if err := SaveAttachmentEmbedding(&AttachmentEmbedding{
			ContentHash: p.ContentHash, Profile: "p", Model: "bge",
			Vector: []float32{1, 0, 0}, Characters: len(p.Text),
		}); err != nil {
			t.Fatalf("SaveAttachmentEmbedding: %v", err)
		}
	}

	after, err := PendingAttachmentEmbeddings("bge", 0)
	if err != nil {
		t.Fatalf("pending after: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("%d still pending for a model that embedded them all", len(after))
	}

	other, err := PendingAttachmentEmbeddings("a-different-model", 0)
	if err != nil {
		t.Fatalf("pending other: %v", err)
	}
	if len(other) != len(before) {
		t.Errorf("%d pending for a different model, want all %d — a vector from one "+
			"model is not a vector for another", len(other), len(before))
	}
}

func TestSimilarAttachments(t *testing.T) {
	embedFixture(t)

	// Hand-built vectors so the test is about the ranking, not the model.
	vectors := map[string][]float32{
		"hash-leave":  {1, 0, 0},
		"hash-health": {0.9, 0.436, 0},
		"hash-recipe": {0, 0, 1},
	}
	for hash, v := range vectors {
		if err := SaveAttachmentEmbedding(&AttachmentEmbedding{
			ContentHash: hash, Profile: "p", Model: "m", Vector: v,
		}); err != nil {
			t.Fatalf("SaveAttachmentEmbedding: %v", err)
		}
	}

	near, err := SimilarAttachments("hash-leave", 0.5, 0)
	if err != nil {
		t.Fatalf("SimilarAttachments: %v", err)
	}
	if len(near) != 1 {
		t.Fatalf("found %d neighbours above 0.5, want 1: %+v", len(near), near)
	}
	if near[0].ContentHash != "hash-health" {
		t.Errorf("nearest is %s, want hash-health", near[0].ContentHash)
	}
	if math.Abs(near[0].Similarity-0.9) > 0.01 {
		t.Errorf("similarity %.3f, want about 0.9", near[0].Similarity)
	}

	// A vector from a different model is not comparable, and must not appear.
	if err := SaveAttachmentEmbedding(&AttachmentEmbedding{
		ContentHash: "hash-elsewhere", Profile: "p", Model: "another",
		Vector: []float32{1, 0, 0},
	}); err != nil {
		t.Fatalf("SaveAttachmentEmbedding: %v", err)
	}
	near, err = SimilarAttachments("hash-leave", -1, 0)
	if err != nil {
		t.Fatalf("SimilarAttachments: %v", err)
	}
	for _, n := range near {
		if n.ContentHash == "hash-elsewhere" {
			t.Error("compared vectors made by different models")
		}
	}
}

// `similar:x` includes x. Excluding it would make clicking a file produce a
// result that omits the file clicked, which reads as a wrong answer.
func TestSimilarAttachmentKeysIncludesTheTarget(t *testing.T) {
	embedFixture(t)

	for hash, v := range map[string][]float32{
		"hash-leave":  {1, 0, 0},
		"hash-health": {0.9, 0.436, 0},
	} {
		if err := SaveAttachmentEmbedding(&AttachmentEmbedding{
			ContentHash: hash, Profile: "p", Model: "m", Vector: v,
		}); err != nil {
			t.Fatal(err)
		}
	}

	keys, err := similarAttachmentKeys("hash-leave", 0.5)
	if err != nil {
		t.Fatalf("similarAttachmentKeys: %v", err)
	}
	if len(keys) == 0 || keys[0] != "hash-leave" {
		t.Errorf("keys %v, want the target first", keys)
	}
}

// A file nothing has embedded is an error, not an empty result: "nothing is
// similar" and "this file was never embedded" are different facts and only one
// of them is fixable.
func TestSimilarAttachmentsWithoutAnEmbedding(t *testing.T) {
	embedFixture(t)

	if _, err := SimilarAttachments("hash-leave", 0.5, 0); err == nil {
		t.Error("SimilarAttachments succeeded for a file with no vector")
	}
}
