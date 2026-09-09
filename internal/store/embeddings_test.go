package store

import (
	"math"
	"testing"
)

func unit(v ...float32) []float32 { return v }

func seedEmbedding(t *testing.T, id, model string, v []float32) {
	t.Helper()
	if err := SaveEmbedding(&Embedding{
		MessageID: id, Profile: "p", Model: model, Vector: v,
	}); err != nil {
		t.Fatalf("SaveEmbedding(%s): %v", id, err)
	}
}

// A vector has to survive the round trip through a BLOB intact, or every
// similarity computed from it is wrong in a way nothing would surface.
func TestVectorRoundTrip(t *testing.T) {
	original := []float32{0.5, -0.25, 1e-8, 3.14159, -0}
	got := decodeVector(encodeVector(original))
	if len(got) != len(original) {
		t.Fatalf("width changed: %d -> %d", len(original), len(got))
	}
	for i := range original {
		if got[i] != original[i] {
			t.Errorf("element %d: %v -> %v", i, original[i], got[i])
		}
	}
}

func TestSimilarityRanksByDistance(t *testing.T) {
	openQueryFixture(t)

	// m1 is the seed. m2 points the same way, m3 is at an angle, m4 opposite.
	seedEmbedding(t, "m1", "e", unit(1, 0, 0))
	seedEmbedding(t, "m2", "e", unit(0.99, 0.14, 0))
	seedEmbedding(t, "m3", "e", unit(0.7, 0.7, 0))
	seedEmbedding(t, "m4", "e", unit(-1, 0, 0))

	got, err := SimilarTo("m1", -1, 0)
	if err != nil {
		t.Fatalf("SimilarTo: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d neighbours, want 3 (the seed is not its own neighbour)", len(got))
	}
	if got[0].MessageID != "m2" || got[1].MessageID != "m3" || got[2].MessageID != "m4" {
		t.Errorf("order is %v, want m2, m3, m4", []string{
			got[0].MessageID, got[1].MessageID, got[2].MessageID})
	}
	// Magnitude must not matter: only direction does, so an unnormalised
	// vector twice as long is the same message.
	if math.Abs(got[0].Similarity-0.99) > 0.02 {
		t.Errorf("similarity to a near-identical vector is %.3f", got[0].Similarity)
	}
}

func TestSimilarityRespectsTheThreshold(t *testing.T) {
	openQueryFixture(t)
	seedEmbedding(t, "m1", "e", unit(1, 0, 0))
	seedEmbedding(t, "m2", "e", unit(0.99, 0.14, 0))
	seedEmbedding(t, "m3", "e", unit(0.7, 0.7, 0))

	tight, err := SimilarTo("m1", 0.95, 0)
	if err != nil {
		t.Fatalf("SimilarTo: %v", err)
	}
	if len(tight) != 1 || tight[0].MessageID != "m2" {
		t.Errorf("at 0.95 got %v, want only m2", tight)
	}
	loose, _ := SimilarTo("m1", 0.5, 0)
	if len(loose) != 2 {
		t.Errorf("at 0.50 got %d, want 2", len(loose))
	}
}

// Vectors from different models describe different spaces. Comparing them is
// not a worse answer, it is a meaningless one.
func TestVectorsFromDifferentModelsAreNeverCompared(t *testing.T) {
	openQueryFixture(t)
	seedEmbedding(t, "m1", "model-a", unit(1, 0, 0))
	seedEmbedding(t, "m2", "model-a", unit(1, 0, 0))
	seedEmbedding(t, "m3", "model-b", unit(1, 0, 0))

	got, err := SimilarTo("m1", -1, 0)
	if err != nil {
		t.Fatalf("SimilarTo: %v", err)
	}
	if len(got) != 1 || got[0].MessageID != "m2" {
		t.Errorf("got %v, want only the message embedded by the same model", got)
	}
}

// Switching models means re-embedding: a vector made by another model is not a
// vector for this one, however present it is.
func TestPendingIsPerModel(t *testing.T) {
	openQueryFixture(t)
	seedEmbedding(t, "m1", "model-a", unit(1, 0, 0))

	pendingA, err := PendingEmbeddings("model-a", "", 0)
	if err != nil {
		t.Fatalf("PendingEmbeddings: %v", err)
	}
	pendingB, _ := PendingEmbeddings("model-b", "", 0)

	if len(pendingB) != len(pendingA)+1 {
		t.Errorf("model-b has %d pending and model-a has %d; the already-embedded "+
			"message should be pending for b", len(pendingB), len(pendingA))
	}
	for _, id := range pendingA {
		if id == "m1" {
			t.Error("a message already embedded by this model is still pending for it")
		}
	}
}

// The number beside the slider. A threshold with no distribution is a dial
// with no markings — on a real archive every neighbour sat between 0.26 and
// 0.65, so a plausible 0.8 matched nothing.
func TestSimilarityHistogramDescribesTheSpread(t *testing.T) {
	openQueryFixture(t)
	seedEmbedding(t, "m1", "e", unit(1, 0, 0))
	seedEmbedding(t, "m2", "e", unit(0.99, 0.14, 0))
	seedEmbedding(t, "m3", "e", unit(0.7, 0.7, 0))
	seedEmbedding(t, "m4", "e", unit(0, 1, 0))

	hist, err := SimilarityHistogram("m1", []float64{0.9, 0.5, 0.1})
	if err != nil {
		t.Fatalf("SimilarityHistogram: %v", err)
	}
	if hist["0.90"] != 1 {
		t.Errorf("at 0.90 the histogram says %d, want 1", hist["0.90"])
	}
	if hist["0.50"] != 2 {
		t.Errorf("at 0.50 the histogram says %d, want 2", hist["0.50"])
	}
	// Monotonic by construction: a looser threshold can never match less.
	if hist["0.10"] < hist["0.50"] {
		t.Error("a looser threshold matched fewer messages")
	}
}
