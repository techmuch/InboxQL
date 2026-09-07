package annotate

import (
	"context"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// openAnnotateFixture opens an empty store.
//
// Empty on purpose: these tests are about which gateway a run would use and
// whether it is allowed to, which is decided before a single message is read.
// With no mail to send, an allowed run completes without touching a network.
func openAnnotateFixture(t *testing.T) {
	t.Helper()
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

// Models wrap JSON in prose and code fences often enough that failing the row
// would mean re-running a whole job over a formatting habit.
func TestParseResponseToleratesModelHabits(t *testing.T) {
	label := &store.Annotator{Kind: store.KindLabel}

	matched := []string{
		`{"matched": true, "confidence": 0.9}`,
		"```json\n{\"matched\": true, \"confidence\": 0.9}\n```",
		"```\n{\"matched\": true}\n```",
		"Sure! Here is the result:\n{\"matched\": true, \"confidence\": 0.9}",
		"  \n{\"matched\":true}\n  ",
	}
	for _, raw := range matched {
		got, err := parseResponse(label, raw)
		if err != nil {
			t.Errorf("parseResponse(%q): %v", raw, err)
			continue
		}
		if len(got) != 1 || !got[0].Matched {
			t.Errorf("parseResponse(%q) = %+v, want one match", raw, got)
		}
	}

	// A negative answer is not an error and not a match: it is a recorded
	// "no", which is what makes -label:x mean something.
	got, err := parseResponse(label, `{"matched": false, "confidence": 0.2}`)
	if err != nil {
		t.Fatalf("a negative answer errored: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a false match produced %d results, want 0", len(got))
	}

	for _, raw := range []string{"", "not json at all", "{unclosed"} {
		if _, err := parseResponse(label, raw); err == nil {
			t.Errorf("parseResponse(%q) succeeded, want an error", raw)
		}
	}
}

// One message can carry several records, and every one has to survive.
func TestParseResponseKeepsEveryExtractedRecord(t *testing.T) {
	extract := &store.Annotator{Kind: store.KindExtract}

	got, err := parseResponse(extract, `{"records":[
		{"day":"2026-01-05","signups":100},
		{"day":"2026-01-06","signups":150}
	],"confidence":0.8}`)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0].Data["signups"].(float64) != 100 {
		t.Errorf("first record = %v", got[0].Data)
	}
	if got[0].Confidence == nil || *got[0].Confidence != 0.8 {
		t.Error("confidence was dropped")
	}

	// An empty list means "looked, found nothing" — a real answer.
	empty, err := parseResponse(extract, `{"records":[]}`)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d records, want 0", len(empty))
	}

	// Blank objects are noise, not records.
	sparse, err := parseResponse(extract, `{"records":[{},{"a":1}]}`)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(sparse) != 1 {
		t.Errorf("got %d records, want 1 (the empty object dropped)", len(sparse))
	}
}

// An extractor reads html_body: the table behind a chart is the data, and the
// plain-text fallback InboxQL stores has had exactly that structure stripped.
func TestExtractorsPreferHTML(t *testing.T) {
	m := &message.Message{
		From: "a@b.com", Subject: "Digest",
		Body:     "Signups 100 150 200",
		HTMLBody: "<table><tr><td>Mon</td><td>100</td></tr></table>",
	}

	extract := renderMessage(&store.Annotator{Kind: store.KindExtract}, m)
	if !strings.Contains(extract, "<table>") {
		t.Error("an extractor was given the stripped text rather than the HTML")
	}

	// A label is reading prose, so the plain text is the better input.
	label := renderMessage(&store.Annotator{Kind: store.KindLabel}, m)
	if strings.Contains(label, "<table>") {
		t.Error("a label prompt carried HTML markup")
	}

	// Headers are part of the question either way.
	for _, want := range []string{"From: a@b.com", "Subject: Digest"} {
		if !strings.Contains(extract, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}

func TestPromptStatesTheOutputContract(t *testing.T) {
	label := buildSystemPrompt(&store.Annotator{Kind: store.KindLabel, Instructions: "Is it urgent?"})
	if !strings.Contains(label, "Is it urgent?") {
		t.Error("the instruction is missing from the prompt")
	}
	if !strings.Contains(label, `"matched"`) {
		t.Error("the label prompt does not state its output shape")
	}

	extract := buildSystemPrompt(&store.Annotator{
		Kind: store.KindExtract, Instructions: "Pull the numbers.",
		SchemaJSON: `{"signups":"number"}`,
	})
	if !strings.Contains(extract, `"records"`) {
		t.Error("the extractor prompt does not state its output shape")
	}
	if !strings.Contains(extract, "signups") {
		t.Error("the declared schema is not shown to the model")
	}
}

// Whether a run sends mail off the machine is the fact consent hangs on, so it
// is decided by the endpoint rather than by the provider's name.
//
// The rule itself now lives on the profile, since a profile is what an
// annotator names — but this package is where consent is enforced, so the
// behaviour is still asserted from here.
func TestRemoteDetection(t *testing.T) {
	cases := []struct {
		cfg    store.LLMConfig
		remote bool
	}{
		{store.LLMConfig{}, false},
		{store.LLMConfig{Provider: "ollama"}, false},
		{store.LLMConfig{Provider: "ollama", Endpoint: "http://localhost:11434"}, false},
		{store.LLMConfig{Provider: "ollama", Endpoint: "http://127.0.0.1:11434"}, false},
		// A local provider name pointed at someone else's machine is remote.
		{store.LLMConfig{Provider: "ollama", Endpoint: "https://ollama.example.com"}, true},
		{store.LLMConfig{Provider: "openai"}, true},
		{store.LLMConfig{Provider: "openai", Endpoint: "http://localhost:8000/v1"}, false},
	}
	for _, c := range cases {
		if got := c.cfg.IsRemote(); got != c.remote {
			t.Errorf("IsRemote(%s %s) = %v, want %v",
				c.cfg.Provider, c.cfg.Endpoint, got, c.remote)
		}
	}
}

// The point of profiles: consent is a property of the annotator, not of the
// machine. One annotator may reach a cloud model while every other stays local.
func TestConsentIsPerAnnotatorNotPerMachine(t *testing.T) {
	openAnnotateFixture(t)

	if err := store.SaveLLMProfile(&store.LLMProfile{
		Name: "local", Provider: "ollama", Model: "llama3", IsDefault: true}); err != nil {
		t.Fatalf("SaveLLMProfile: %v", err)
	}
	if err := store.SaveLLMProfile(&store.LLMProfile{
		Name: "cloud", Provider: "openai", Model: "gpt-4o-mini", APIKey: "sk-test"}); err != nil {
		t.Fatalf("SaveLLMProfile: %v", err)
	}

	cases := []struct {
		name        string
		profile     string
		allowRemote bool
		wantRefusal bool
		why         string
	}{
		{"local-no-consent", "local", false, false,
			"a local profile needs no consent"},
		{"cloud-no-consent", "cloud", false, true,
			"a cloud profile without consent must refuse"},
		{"cloud-with-consent", "cloud", true, false,
			"a cloud profile with consent may run"},
		{"default-no-consent", "", false, false,
			"an unnamed profile means the default, which here is local"},
	}

	for _, c := range cases {
		a := &store.Annotator{
			Name: c.name, Kind: store.KindLabel, Engine: store.EngineLLM,
			Instructions: "Is this about billing?",
			Profile:      c.profile, AllowRemote: c.allowRemote,
		}
		if err := store.SaveAnnotator(a); err != nil {
			t.Fatalf("SaveAnnotator(%s): %v", c.name, err)
		}

		// A real run, because consent is checked at send time — the mailbox
		// is empty, so an allowed run completes without a network call.
		_, err := Run(context.Background(), c.name, Options{})
		refused := err != nil && strings.Contains(err.Error(), "no consent recorded")
		if refused != c.wantRefusal {
			t.Errorf("%s: refused=%v want=%v (%s); err=%v",
				c.name, refused, c.wantRefusal, c.why, err)
		}
		if !refused && err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
		}

		// And the plan says so in advance, so --dry-run does not report that
		// a run which will refuse looks fine.
		plan, err := Describe(c.name, "")
		if err != nil {
			t.Fatalf("%s: Describe: %v", c.name, err)
		}
		if plan.ConsentMissing != c.wantRefusal {
			t.Errorf("%s: plan.ConsentMissing=%v, want %v",
				c.name, plan.ConsentMissing, c.wantRefusal)
		}
	}
}

// A run that names a profile which does not exist must fail rather than fall
// back to whatever is configured. Work that asked for a specific gateway and
// quietly used a different one is a wrong answer nobody notices.
func TestAnUnknownProfileIsAnErrorNotAFallback(t *testing.T) {
	openAnnotateFixture(t)

	if err := store.SaveLLMProfile(&store.LLMProfile{
		Name: "local", Provider: "ollama", Model: "llama3", IsDefault: true}); err != nil {
		t.Fatalf("SaveLLMProfile: %v", err)
	}
	a := &store.Annotator{
		Name: "typo", Kind: store.KindLabel, Engine: store.EngineLLM,
		Instructions: "anything", Profile: "clod",
	}
	if err := store.SaveAnnotator(a); err != nil {
		t.Fatalf("SaveAnnotator: %v", err)
	}

	if _, err := Describe("typo", ""); err == nil {
		t.Fatal("describing an annotator with an unknown profile succeeded")
	} else if !strings.Contains(err.Error(), "clod") {
		t.Errorf("the error does not name the missing profile: %v", err)
	}
}

// A per-annotator model overrides the profile's model on the same gateway. It
// must not silently move the work to a different provider.
func TestModelOverridesTheModelNotTheGateway(t *testing.T) {
	openAnnotateFixture(t)

	if err := store.SaveLLMProfile(&store.LLMProfile{
		Name: "local", Provider: "ollama", Model: "llama3", IsDefault: true}); err != nil {
		t.Fatalf("SaveLLMProfile: %v", err)
	}
	a := &store.Annotator{
		Name: "bigger", Kind: store.KindLabel, Engine: store.EngineLLM,
		Instructions: "anything", Model: "llama3.3:70b",
	}
	if err := store.SaveAnnotator(a); err != nil {
		t.Fatalf("SaveAnnotator: %v", err)
	}

	plan, err := Describe("bigger", "")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if plan.Model != "llama3.3:70b" {
		t.Errorf("model = %q, want the override", plan.Model)
	}
	if plan.Provider != "ollama" {
		t.Errorf("provider = %q, want the profile's", plan.Provider)
	}
	if plan.Remote {
		t.Error("overriding the model made the run look remote")
	}
}
