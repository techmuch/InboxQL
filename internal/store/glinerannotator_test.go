package store

import (
	"testing"
)

func TestLabelsComeFromTheSchema(t *testing.T) {
	a := &Annotator{SchemaJSON: `{"amount":"x","due date":"y","order number":"z"}`}

	got := a.Labels()
	want := []string{"amount", "due date", "order number"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Sorted, because the label order is the class order in the scores: an
	// unstable order silently relabels every span from one run to the next.
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (order matters)", got, want)
		}
	}
}

// timeField names one of the other fields rather than being one, so asking a
// model to find a "timeField" in a receipt would be asking for nothing.
func TestLabelsExcludeTimeField(t *testing.T) {
	a := &Annotator{SchemaJSON: `{"amount":"x","timeField":"amount"}`}

	got := a.Labels()
	if len(got) != 1 || got[0] != "amount" {
		t.Errorf("got %v, want just [amount]", got)
	}
}

func TestLabelsOfNothing(t *testing.T) {
	for _, schema := range []string{"", "{}", "not json", "[1,2]"} {
		a := &Annotator{SchemaJSON: schema}
		if got := a.Labels(); len(got) != 0 {
			t.Errorf("schema %q gave labels %v, want none", schema, got)
		}
	}
}

func TestSchemaChangeVersionsASpanExtractor(t *testing.T) {
	openQueryFixture(t)

	a := &Annotator{
		Name: "money", Kind: KindExtract, Engine: EngineGLiNER,
		Instructions: "find the money", SchemaJSON: `{"amount":"x"}`,
	}
	if err := SaveAnnotator(a); err != nil {
		t.Fatal(err)
	}
	if a.Version != 1 {
		t.Fatalf("new annotator is at version %d, want 1", a.Version)
	}

	// Reformatting is not a change to the question. Treating it as one would
	// invalidate a whole mailbox's results for a reindented file.
	same := &Annotator{
		Name: "money", Kind: KindExtract, Engine: EngineGLiNER,
		Instructions: "find the money", SchemaJSON: "{\n  \"amount\": \"x\"\n}",
	}
	if err := SaveAnnotator(same); err != nil {
		t.Fatal(err)
	}
	if same.Version != 1 {
		t.Errorf("reformatting the schema bumped the version to %d", same.Version)
	}

	// Adding a field is a different question, so the old answers no longer
	// answer it.
	changed := &Annotator{
		Name: "money", Kind: KindExtract, Engine: EngineGLiNER,
		Instructions: "find the money", SchemaJSON: `{"amount":"x","due date":"y"}`,
	}
	if err := SaveAnnotator(changed); err != nil {
		t.Fatal(err)
	}
	if changed.Version != 2 {
		t.Errorf("adding a label left the version at %d, want 2", changed.Version)
	}
}

// For the other engines the schema describes the shape of a reply that was
// asked for in words, and the words are what count — so this behaviour is
// deliberately not extended to them.
func TestSchemaChangeDoesNotVersionAnLLMExtractor(t *testing.T) {
	openQueryFixture(t)

	a := &Annotator{
		Name: "money", Kind: KindExtract, Engine: EngineLLM,
		Instructions: "find the money", SchemaJSON: `{"amount":"x"}`,
	}
	if err := SaveAnnotator(a); err != nil {
		t.Fatal(err)
	}

	changed := &Annotator{
		Name: "money", Kind: KindExtract, Engine: EngineLLM,
		Instructions: "find the money", SchemaJSON: `{"amount":"x","due date":"y"}`,
	}
	if err := SaveAnnotator(changed); err != nil {
		t.Fatal(err)
	}
	if changed.Version != 1 {
		t.Errorf("version bumped to %d for an LLM annotator's schema change", changed.Version)
	}
}
