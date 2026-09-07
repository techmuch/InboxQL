package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/vault"
)

// DefaultLLMEndpoints are used when an operator names a provider but no URL.
//
// This map lives here rather than in internal/llm because a profile's endpoint
// is resolved when it is saved, not when it is used. That single decision is
// what makes LLMProfile.IsRemote a pure string inspection instead of a lookup
// that has to agree with a second copy of these defaults somewhere else.
var DefaultLLMEndpoints = map[string]string{
	"ollama": "http://localhost:11434",
	"openai": "https://api.openai.com/v1",
	"swama":  "http://localhost:28100/v1",
}

// LLMProfile is a named, complete address for a model.
//
// # Why the whole address and not just a model name
//
// `annotate create --model gpt-4o` used to be the only way to point work at a
// different model, and it changed the model string while leaving the provider,
// endpoint and API key alone — so on a machine configured for a local runtime
// it asked that runtime for a model it had never heard of. A model name is not
// an address. This is.
//
// # Why scope is derived, not stored
//
// Whether a profile is remote follows from its endpoint, so it cannot drift out
// of agreement with it. A stored flag could say "local" about api.openai.com,
// and the consent check that protects message bodies would believe it.
type LLMProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Endpoint is always populated: an empty one is filled from
	// DefaultLLMEndpoints on save, so every reader sees a real URL.
	Endpoint string `json:"endpoint"`
	// APIKey is decrypted on read and encrypted on write, and is never
	// serialised — see MarshalJSON's absence and Redacted below.
	APIKey        string `json:"-"`
	IsDefault     bool   `json:"isDefault"`
	AutoStart     bool   `json:"autoStart"`
	LaunchMode    string `json:"launchMode"`
	LifecycleMode string `json:"lifecycleMode"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsRemote reports whether using this profile sends message bodies off this
// machine.
//
// The privacy guarantee in this product rests on this one function, so it is
// deliberately conservative: anything that is not recognisably a loopback
// address counts as remote. A profile pointed at another machine on the LAN is
// remote, because the mail did leave.
func (p LLMProfile) IsRemote() bool {
	if p.Provider == "" {
		return false
	}
	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = DefaultLLMEndpoints[p.Provider]
	}
	host := strings.ToLower(endpoint)
	for _, local := range []string{"://localhost", "://127.0.0.1", "://[::1]", "://0.0.0.0"} {
		if strings.Contains(host, local) {
			return false
		}
	}
	return true
}

// Scope names what IsRemote reports, for display.
func (p LLMProfile) Scope() string {
	if p.IsRemote() {
		return "remote"
	}
	return "local"
}

// HasAPIKey reports whether a key is stored, without revealing it.
func (p LLMProfile) HasAPIKey() bool { return p.APIKey != "" }

// Redacted returns a copy safe to print or serialise.
func (p LLMProfile) Redacted() map[string]any {
	return map[string]any{
		"id":            p.ID,
		"name":          p.Name,
		"provider":      p.Provider,
		"model":         p.Model,
		"endpoint":      p.Endpoint,
		"isDefault":     p.IsDefault,
		"autoStart":     p.AutoStart,
		"launchMode":    p.LaunchMode,
		"lifecycleMode": p.LifecycleMode,
		"scope":         p.Scope(),
		"hasApiKey":     p.HasAPIKey(),
		"createdAt":     p.CreatedAt,
		"updatedAt":     p.UpdatedAt,
	}
}

const llmProfileColumns = `id, name, provider, model, endpoint, COALESCE(api_key, ''),
	is_default, auto_start, launch_mode, lifecycle_mode, created_at, updated_at`

func scanLLMProfile(scan func(...any) error) (*LLMProfile, error) {
	p := &LLMProfile{}
	var sealed string
	var created, updated int64
	if err := scan(&p.ID, &p.Name, &p.Provider, &p.Model, &p.Endpoint, &sealed,
		&p.IsDefault, &p.AutoStart, &p.LaunchMode, &p.LifecycleMode,
		&created, &updated); err != nil {
		return nil, err
	}
	p.CreatedAt = millisToTime(created)
	p.UpdatedAt = millisToTime(updated)

	if sealed != "" {
		key, err := vault.Decrypt(sealed)
		if err != nil {
			// One unreadable key must not make every profile unlistable. The
			// profile is returned without it, and the caller finds out when it
			// tries to use it rather than when it tries to draw a list.
			return p, nil
		}
		p.APIKey = key
	}
	return p, nil
}

// ListLLMProfiles returns every profile, default first then by name.
func ListLLMProfiles() ([]*LLMProfile, error) {
	rows, err := db.Query("SELECT " + llmProfileColumns +
		" FROM llm_profiles ORDER BY is_default DESC, name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*LLMProfile{}
	for rows.Next() {
		p, err := scanLLMProfile(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetLLMProfile returns one profile by name, or (nil, nil) when there is none.
//
// By name rather than id because a name is what a person writes on an
// annotator and types into the CLI. Ids exist so a rename does not orphan
// anything.
func GetLLMProfile(name string) (*LLMProfile, error) {
	p, err := scanLLMProfile(db.QueryRow("SELECT "+llmProfileColumns+
		" FROM llm_profiles WHERE name = ?", name).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// DefaultLLMProfile returns the profile used when nothing names one.
//
// (nil, nil) means no LLM is configured at all, which is a supported state:
// analyze and draft fall back to emitting structured context.
func DefaultLLMProfile() (*LLMProfile, error) {
	p, err := scanLLMProfile(db.QueryRow("SELECT " + llmProfileColumns +
		" FROM llm_profiles WHERE is_default = 1").Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// ResolveLLMProfile returns the named profile, or the default when unnamed.
//
// The error for a missing name is deliberately not a silent fallback to the
// default: work that asked for a specific model and quietly got a different
// one is the kind of wrong answer nobody notices.
func ResolveLLMProfile(name string) (*LLMProfile, error) {
	if strings.TrimSpace(name) == "" {
		return DefaultLLMProfile()
	}
	p, err := GetLLMProfile(name)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("no model profile named %q", name)
	}
	return p, nil
}

// SaveLLMProfile creates or updates a profile.
func SaveLLMProfile(p *LLMProfile) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return fmt.Errorf("a model profile needs a name")
	}
	if p.Provider == "" {
		return fmt.Errorf("profile %q needs a provider", p.Name)
	}
	if p.Model == "" {
		return fmt.Errorf("profile %q needs a model", p.Name)
	}
	// Resolved here so every reader sees a real URL and IsRemote never has to
	// guess what an empty endpoint meant.
	if strings.TrimSpace(p.Endpoint) == "" {
		p.Endpoint = DefaultLLMEndpoints[p.Provider]
	}
	if p.LaunchMode == "" {
		p.LaunchMode = "headless"
	}
	if p.LifecycleMode == "" {
		p.LifecycleMode = "managed"
	}

	now := time.Now()
	if p.ID == "" {
		p.ID = uuid.New().String()
		p.CreatedAt = now
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	sealed := ""
	if p.APIKey != "" {
		var err error
		if sealed, err = vault.Encrypt(p.APIKey); err != nil {
			return fmt.Errorf("cannot encrypt the API key for %q: %w", p.Name, err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clearing the flag first is what makes the partial unique index a
	// constraint rather than an obstacle: without it, promoting a second
	// profile fails instead of demoting the first.
	if p.IsDefault {
		if _, err := tx.Exec("UPDATE llm_profiles SET is_default = 0 WHERE id != ?", p.ID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO llm_profiles (id, name, provider, model, endpoint, api_key,
			is_default, auto_start, launch_mode, lifecycle_mode, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, provider = excluded.provider, model = excluded.model,
			endpoint = excluded.endpoint, api_key = excluded.api_key,
			is_default = excluded.is_default, auto_start = excluded.auto_start,
			launch_mode = excluded.launch_mode, lifecycle_mode = excluded.lifecycle_mode,
			updated_at = excluded.updated_at`,
		p.ID, p.Name, p.Provider, p.Model, p.Endpoint, nullIfEmpty(sealed),
		p.IsDefault, p.AutoStart, p.LaunchMode, p.LifecycleMode,
		p.CreatedAt.UnixMilli(), p.UpdatedAt.UnixMilli()); err != nil {
		return err
	}

	if err := ensureOneDefault(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteLLMProfile removes a profile.
func DeleteLLMProfile(name string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec("DELETE FROM llm_profiles WHERE name = ?", name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no model profile named %q", name)
	}
	if err := ensureOneDefault(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// SetDefaultLLMProfile promotes a profile.
func SetDefaultLLMProfile(name string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE llm_profiles SET is_default = 0"); err != nil {
		return err
	}
	res, err := tx.Exec("UPDATE llm_profiles SET is_default = 1 WHERE name = ?", name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no model profile named %q", name)
	}
	return tx.Commit()
}

// ensureOneDefault promotes something when nothing is the default.
//
// Deleting the default would otherwise leave a configured system that reports
// no LLM configured — the same "looks empty, isn't" failure this project keeps
// running into.
func ensureOneDefault(tx *sql.Tx) error {
	var n int
	if err := tx.QueryRow("SELECT COUNT(*) FROM llm_profiles WHERE is_default = 1").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := tx.Exec(`
		UPDATE llm_profiles SET is_default = 1
		WHERE id = (SELECT id FROM llm_profiles ORDER BY created_at ASC LIMIT 1)`)
	return err
}
