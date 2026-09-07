//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

type profileRow struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	Scope     string `json:"scope"`
	IsDefault bool   `json:"isDefault"`
	HasAPIKey bool   `json:"hasApiKey"`
}

func (e *env) profiles(t *testing.T) []profileRow {
	t.Helper()
	r := e.run("--json", "llm", "profile", "list")
	if r.ExitCode != 0 {
		t.Fatalf("llm profile list exited %d: %s%s", r.ExitCode, r.Stdout, r.Stderr)
	}
	var out []profileRow
	r.JSON(t, &out)
	return out
}

// A profile is the whole address of a model. Naming one moves the endpoint and
// the credential with it, which a bare model name never could.
func TestModelProfilesAreConfiguredIndependently(t *testing.T) {
	e := newEnv(t)

	if r := e.run("llm", "profile", "add", "local",
		"--provider", "swama", "--model", "gemma-3-4b", "--default"); r.ExitCode != 0 {
		t.Fatalf("profile add local: %s%s", r.Stdout, r.Stderr)
	}
	if r := e.runWithEnv(map[string]string{"INBOXQL_LLM_API_KEY": "sk-test"},
		"llm", "profile", "add", "cloud",
		"--provider", "openai", "--model", "gpt-4o-mini"); r.ExitCode != 0 {
		t.Fatalf("profile add cloud: %s%s", r.Stdout, r.Stderr)
	}

	got := e.profiles(t)
	if len(got) != 2 {
		t.Fatalf("got %d profiles, want 2: %+v", len(got), got)
	}

	byName := map[string]profileRow{}
	for _, p := range got {
		byName[p.Name] = p
	}

	local := byName["local"]
	if !local.IsDefault {
		t.Error("the profile added with --default is not the default")
	}
	if local.Scope != "local" {
		t.Errorf("swama on localhost is scoped %q", local.Scope)
	}
	// An omitted endpoint is resolved on save, so nothing downstream has to
	// decide what an empty one meant.
	if local.Endpoint == "" {
		t.Error("the endpoint was left empty")
	}

	cloud := byName["cloud"]
	if cloud.Scope != "remote" {
		t.Errorf("api.openai.com is scoped %q, want remote", cloud.Scope)
	}
	if !cloud.HasAPIKey {
		t.Error("the API key was not stored")
	}

	// Promoting demotes, so there is never more than one default.
	if r := e.run("llm", "profile", "default", "cloud"); r.ExitCode != 0 {
		t.Fatalf("profile default: %s%s", r.Stdout, r.Stderr)
	}
	defaults := 0
	for _, p := range e.profiles(t) {
		if p.IsDefault {
			defaults++
			if p.Name != "cloud" {
				t.Errorf("the default is %q, want cloud", p.Name)
			}
		}
	}
	if defaults != 1 {
		t.Errorf("%d profiles claim to be the default", defaults)
	}
}

// The point of the whole change: whether mail leaves the machine is a property
// of the annotator, not of the machine.
func TestConsentIsDecidedPerAnnotator(t *testing.T) {
	e := newEnv(t)
	e.seedMailbox(t)

	if r := e.run("llm", "profile", "add", "local",
		"--provider", "ollama", "--model", "llama3", "--default"); r.ExitCode != 0 {
		t.Fatalf("profile add local: %s%s", r.Stdout, r.Stderr)
	}
	if r := e.run("llm", "profile", "add", "cloud",
		"--provider", "openai", "--model", "gpt-4o-mini"); r.ExitCode != 0 {
		t.Fatalf("profile add cloud: %s%s", r.Stdout, r.Stderr)
	}

	// Local profile: no consent needed.
	if r := e.run("annotate", "create", "local-one", "--engine", "llm",
		"--instructions", "Is this billing?"); r.ExitCode != 0 {
		t.Fatalf("create local-one: %s%s", r.Stdout, r.Stderr)
	}

	// Cloud profile without consent: created, but refuses to run.
	if r := e.run("annotate", "create", "cloud-one", "--engine", "llm",
		"--profile", "cloud", "--instructions", "Is this billing?"); r.ExitCode != 0 {
		t.Fatalf("create cloud-one: %s%s", r.Stdout, r.Stderr)
	}
	r := e.run("annotate", "run", "cloud-one")
	if r.ExitCode == 0 {
		t.Fatal("a remote annotator with no consent ran anyway")
	}
	if !strings.Contains(r.Stderr+r.Stdout, "no consent recorded") {
		t.Errorf("the refusal does not explain itself: %s%s", r.Stdout, r.Stderr)
	}
	// It names the profile, so the fix is obvious from the message.
	if !strings.Contains(r.Stderr+r.Stdout, `"cloud"`) {
		t.Errorf("the refusal does not name the profile: %s%s", r.Stdout, r.Stderr)
	}

	// A dry run must say the same thing in advance, rather than reporting that
	// a run which will refuse looks fine.
	var plan struct {
		Remote         bool `json:"remote"`
		ConsentMissing bool `json:"consentMissing"`
	}
	r = e.run("--json", "annotate", "plan", "cloud-one")
	if r.ExitCode != 0 {
		t.Fatalf("annotate plan: %s%s", r.Stdout, r.Stderr)
	}
	r.JSON(t, &plan)
	if !plan.Remote || !plan.ConsentMissing {
		t.Errorf("the plan reports remote=%v consentMissing=%v, want both true",
			plan.Remote, plan.ConsentMissing)
	}

	// The local annotator is unaffected by the cloud profile existing.
	//
	// A fresh struct, not the one above: both fields are omitempty, so
	// decoding into a reused value keeps the previous answer when the new
	// response omits them.
	var localPlan struct {
		Remote         bool `json:"remote"`
		ConsentMissing bool `json:"consentMissing"`
	}
	r = e.run("--json", "annotate", "plan", "local-one")
	if r.ExitCode != 0 {
		t.Fatalf("annotate plan local-one: %s%s", r.Stdout, r.Stderr)
	}
	r.JSON(t, &localPlan)
	if localPlan.Remote || localPlan.ConsentMissing {
		t.Errorf("a local annotator reports remote=%v consentMissing=%v",
			localPlan.Remote, localPlan.ConsentMissing)
	}
}

// Deleting a profile out from under an annotator would leave it failing at run
// time, so it is refused with the list of what depends on it.
func TestRemovingAProfileInUseIsRefused(t *testing.T) {
	e := newEnv(t)

	if r := e.run("llm", "profile", "add", "cloud",
		"--provider", "openai", "--model", "gpt-4o-mini", "--default"); r.ExitCode != 0 {
		t.Fatalf("profile add: %s%s", r.Stdout, r.Stderr)
	}
	if r := e.run("annotate", "create", "receipts", "--engine", "llm",
		"--profile", "cloud", "--allow-remote", "--instructions", "Pull the amount."); r.ExitCode != 0 {
		t.Fatalf("annotate create: %s%s", r.Stdout, r.Stderr)
	}

	r := e.run("llm", "profile", "remove", "cloud")
	if r.ExitCode == 0 {
		t.Fatal("removing a profile in use succeeded")
	}
	if !strings.Contains(r.Stderr+r.Stdout, "receipts") {
		t.Errorf("the refusal does not name what depends on it: %s%s", r.Stdout, r.Stderr)
	}
}

// An annotator naming a profile that does not exist is a usage error at
// creation, not an annotator that exists and cannot run.
func TestAnnotatorCannotNameAMissingProfile(t *testing.T) {
	e := newEnv(t)

	r := e.run("annotate", "create", "oops", "--engine", "llm",
		"--profile", "nope", "--instructions", "x")
	if r.ExitCode == 0 {
		t.Fatal("creating an annotator with an unknown profile succeeded")
	}
	if !strings.Contains(r.Stderr+r.Stdout, "nope") {
		t.Errorf("the error does not name the missing profile: %s%s", r.Stdout, r.Stderr)
	}
}
