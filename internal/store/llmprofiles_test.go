package store

import (
	"testing"

	"github.com/user/inboxql/internal/vault"
)

func openLLMFixture(t *testing.T) {
	t.Helper()
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)
}

func profileNames(t *testing.T) []string {
	t.Helper()
	ps, err := ListLLMProfiles()
	if err != nil {
		t.Fatalf("ListLLMProfiles: %v", err)
	}
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

// A model name is not an address. The whole point of a profile is that pointing
// work at a cloud model also points it at the cloud endpoint and the key that
// reaches it.
func TestProfileCarriesTheWholeAddress(t *testing.T) {
	openLLMFixture(t)

	local := &LLMProfile{Name: "local-fast", Provider: "swama", Model: "gemma-4-e4b-it-4bit"}
	if err := SaveLLMProfile(local); err != nil {
		t.Fatalf("SaveLLMProfile: %v", err)
	}
	cloud := &LLMProfile{Name: "cloud", Provider: "openai", Model: "gpt-4o-mini", APIKey: "sk-test"}
	if err := SaveLLMProfile(cloud); err != nil {
		t.Fatalf("SaveLLMProfile: %v", err)
	}

	// An omitted endpoint is filled in on save, so no reader has to know what
	// an empty one meant.
	got, err := GetLLMProfile("local-fast")
	if err != nil {
		t.Fatalf("GetLLMProfile: %v", err)
	}
	if got.Endpoint != DefaultLLMEndpoints["swama"] {
		t.Errorf("endpoint = %q, want the swama default", got.Endpoint)
	}
	if got.IsRemote() {
		t.Error("a localhost runtime was classified as remote")
	}

	got, err = GetLLMProfile("cloud")
	if err != nil {
		t.Fatalf("GetLLMProfile: %v", err)
	}
	if !got.IsRemote() {
		t.Error("api.openai.com was classified as local")
	}
	if got.APIKey != "sk-test" {
		t.Errorf("the key did not survive the vault round trip: %q", got.APIKey)
	}
	// And it never leaves in a serialised form.
	if _, leaked := got.Redacted()["apiKey"]; leaked {
		t.Error("Redacted exposed the key")
	}
	if got.Redacted()["hasApiKey"] != true {
		t.Error("Redacted should still say a key exists")
	}
}

// The privacy guarantee rests on IsRemote, so it errs toward remote. A model on
// another machine on the LAN is remote: the mail did leave.
func TestScopeIsConservative(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:11434":     false,
		"http://127.0.0.1:28100/v1":  false,
		"http://[::1]:11434":         false,
		"https://api.openai.com/v1":  true,
		"http://192.168.1.50:11434":  true,
		"http://gpu-box.local:11434": true,
	}
	for endpoint, wantRemote := range cases {
		p := LLMProfile{Provider: "openai", Model: "m", Endpoint: endpoint}
		if got := p.IsRemote(); got != wantRemote {
			t.Errorf("IsRemote(%q) = %v, want %v", endpoint, got, wantRemote)
		}
	}
	// No provider at all is not remote; it is not anything.
	if (LLMProfile{}).IsRemote() {
		t.Error("an unconfigured profile claimed to be remote")
	}
}

// Exactly one default, enforced by the database rather than by whoever writes
// the next handler. Two defaults is a state no UI can fix.
func TestExactlyOneDefault(t *testing.T) {
	openLLMFixture(t)

	// The first profile becomes the default on its own, so a freshly
	// configured system is never "configured but nothing selected".
	first := &LLMProfile{Name: "one", Provider: "ollama", Model: "llama3"}
	if err := SaveLLMProfile(first); err != nil {
		t.Fatalf("SaveLLMProfile: %v", err)
	}
	def, err := DefaultLLMProfile()
	if err != nil || def == nil {
		t.Fatalf("DefaultLLMProfile: %v, %v", def, err)
	}
	if def.Name != "one" {
		t.Errorf("default is %q, want the only profile", def.Name)
	}

	second := &LLMProfile{Name: "two", Provider: "openai", Model: "gpt-4o-mini", IsDefault: true}
	if err := SaveLLMProfile(second); err != nil {
		t.Fatalf("SaveLLMProfile: %v", err)
	}

	var defaults int
	if err := db.QueryRow("SELECT COUNT(*) FROM llm_profiles WHERE is_default = 1").Scan(&defaults); err != nil {
		t.Fatalf("count: %v", err)
	}
	if defaults != 1 {
		t.Fatalf("%d profiles claim to be the default", defaults)
	}
	if def, _ := DefaultLLMProfile(); def.Name != "two" {
		t.Errorf("promoting did not demote: default is %q", def.Name)
	}

	// Deleting the default promotes something, rather than leaving a
	// configured system reporting that no LLM is configured.
	if err := DeleteLLMProfile("two"); err != nil {
		t.Fatalf("DeleteLLMProfile: %v", err)
	}
	def, err = DefaultLLMProfile()
	if err != nil || def == nil {
		t.Fatalf("deleting the default left nothing selected: %v, %v", def, err)
	}
	if def.Name != "one" {
		t.Errorf("default is %q, want the remaining profile", def.Name)
	}
}

// Work that asked for a specific model must not quietly get a different one.
func TestResolvingAnUnknownProfileIsAnError(t *testing.T) {
	openLLMFixture(t)

	if err := SaveLLMProfile(&LLMProfile{
		Name: "local", Provider: "ollama", Model: "llama3"}); err != nil {
		t.Fatalf("SaveLLMProfile: %v", err)
	}

	if _, err := ResolveLLMProfile("typo"); err == nil {
		t.Error("resolving a name that does not exist fell back to the default")
	}
	// An empty name is a different thing: it means "whatever is configured".
	p, err := ResolveLLMProfile("")
	if err != nil || p == nil || p.Name != "local" {
		t.Errorf("ResolveLLMProfile(\"\") = %v, %v", p, err)
	}
}

// Nothing configured is a supported state, not an error: analyze and draft
// fall back to emitting context.
func TestNoProfilesIsNotAnError(t *testing.T) {
	openLLMFixture(t)

	p, err := DefaultLLMProfile()
	if err != nil {
		t.Fatalf("DefaultLLMProfile: %v", err)
	}
	if p != nil {
		t.Fatalf("a fresh database has a profile: %+v", p)
	}
	cfg, err := GetLLMConfig()
	if err != nil {
		t.Fatalf("GetLLMConfig: %v", err)
	}
	if cfg.Provider != "" {
		t.Errorf("GetLLMConfig invented a provider: %q", cfg.Provider)
	}
	if names := profileNames(t); len(names) != 0 {
		t.Errorf("ListLLMProfiles returned %v", names)
	}
}

// Every pre-v22 caller writes through LLMConfig and must keep working.
func TestLLMConfigIsAViewOfTheDefaultProfile(t *testing.T) {
	openLLMFixture(t)

	if err := SaveLLMConfig(LLMConfig{
		Provider: "ollama", Model: "llama3", APIKey: "k"}); err != nil {
		t.Fatalf("SaveLLMConfig: %v", err)
	}

	if names := profileNames(t); len(names) != 1 || names[0] != DefaultProfileName {
		t.Fatalf("SaveLLMConfig produced profiles %v", names)
	}

	cfg, err := GetLLMConfig()
	if err != nil {
		t.Fatalf("GetLLMConfig: %v", err)
	}
	if cfg.Provider != "ollama" || cfg.Model != "llama3" || cfg.APIKey != "k" {
		t.Errorf("round trip gave %+v", cfg)
	}
	if cfg.Profile != DefaultProfileName {
		t.Errorf("config does not name its profile: %q", cfg.Profile)
	}
	if cfg.Endpoint != DefaultLLMEndpoints["ollama"] {
		t.Errorf("endpoint = %q, want it resolved", cfg.Endpoint)
	}

	// Disabling removes the profile rather than leaving an unusable entry in
	// the list.
	if err := SaveLLMConfig(LLMConfig{}); err != nil {
		t.Fatalf("SaveLLMConfig(zero): %v", err)
	}
	if names := profileNames(t); len(names) != 0 {
		t.Errorf("disabling left %v behind", names)
	}
}

// A machine that had a provider configured before v22 must come back with it
// configured, not with an empty AI settings page.
func TestUpgradeCarriesTheOldSettingsForward(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitDB(dir); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	// Put the database back in the shape v21 left it: settings rows, no
	// profiles table content.
	sealed, err := vault.Encrypt("sk-secret")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	for k, v := range map[string]string{
		settingLLMProvider:   "openai",
		settingLLMModel:      "gpt-4o-mini",
		settingLLMEndpoint:   "", // deliberately unset, as an operator may leave it
		settingLLMAPIKey:     sealed,
		settingLLMAutoStart:  "true",
		settingLLMLaunchMode: "menubar",
	} {
		if err := UpdateSetting(k, v); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	if _, err := db.Exec("DELETE FROM llm_profiles"); err != nil {
		t.Fatalf("clear profiles: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 21;"); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	CloseDB()

	// Reopening runs the migration, which is the path a real upgrade takes.
	if _, err := InitDB(dir); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(CloseDB)

	p, err := DefaultLLMProfile()
	if err != nil {
		t.Fatalf("DefaultLLMProfile: %v", err)
	}
	if p == nil {
		t.Fatal("the upgrade lost the configured provider")
	}
	if p.Name != DefaultProfileName {
		t.Errorf("profile name = %q", p.Name)
	}
	if p.Provider != "openai" || p.Model != "gpt-4o-mini" {
		t.Errorf("profile = %+v", p)
	}
	// The endpoint was blank in settings and is resolved by the migration, so
	// IsRemote has a real URL to judge.
	if p.Endpoint != DefaultLLMEndpoints["openai"] {
		t.Errorf("endpoint = %q, want it resolved during migration", p.Endpoint)
	}
	if !p.IsRemote() {
		t.Error("a migrated OpenAI profile was classified as local")
	}
	if !p.AutoStart || p.LaunchMode != "menubar" {
		t.Errorf("launch settings were dropped: autoStart=%v launchMode=%q", p.AutoStart, p.LaunchMode)
	}
	// The sealed key is carried across without a decrypt/re-encrypt round
	// trip, so an upgrade on a machine with a missing vault key cannot destroy
	// the credential.
	if p.APIKey != "sk-secret" {
		t.Errorf("the API key did not survive the upgrade: %q", p.APIKey)
	}
}

// A machine that never configured a provider must not gain an empty profile
// that looks like a configuration.
func TestUpgradeWithNothingConfiguredCreatesNothing(t *testing.T) {
	openLLMFixture(t)
	if names := profileNames(t); len(names) != 0 {
		t.Errorf("a fresh database has profiles: %v", names)
	}
}
