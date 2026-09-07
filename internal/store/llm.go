package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Setting keys for the LLM gateway, as it was stored before v22.
//
// Kept only so the migration can read them. Nothing writes these any more —
// the profile table is the source of truth.
const (
	settingLLMProvider      = "llm_provider"
	settingLLMModel         = "llm_model"
	settingLLMEndpoint      = "llm_endpoint"
	settingLLMAPIKey        = "llm_api_key"
	settingLLMAutoStart     = "llm_auto_start"
	settingLLMLaunchMode    = "llm_launch_mode"
	settingLLMLifecycleMode = "llm_lifecycle_mode"
)

// DefaultProfileName is the profile the pre-v22 settings become.
const DefaultProfileName = "default"

// LLMConfig describes the completion provider a piece of work will use.
//
// # What this is now
//
// A view of one LLMProfile — the default one, unless a caller resolved a
// different profile and converted it. It stays as a type because a dozen call
// sites take it, and because "which model am I about to use" is a genuinely
// different question from "which models are configured".
//
// An empty Provider means no LLM is configured, which is a supported state:
// analyze and draft fall back to emitting structured context for an external
// agent to reason over.
type LLMConfig struct {
	// Profile names the profile this came from, so an error can say which
	// configuration was at fault rather than just which model.
	Profile       string `json:"profile"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	Endpoint      string `json:"endpoint"`
	AutoStart     bool   `json:"autoStart"`
	LaunchMode    string `json:"launchMode"`    // "headless" | "menubar"
	LifecycleMode string `json:"lifecycleMode"` // "managed" | "external"
	// APIKey is decrypted on read and encrypted on write. It is never included
	// in JSON output — see LLMConfig.Redacted.
	APIKey string `json:"-"`
}

// HasAPIKey reports whether a key is stored, without revealing it.
func (c LLMConfig) HasAPIKey() bool { return c.APIKey != "" }

// IsRemote reports whether using this config sends message bodies off this
// machine. One implementation, shared with LLMProfile.
func (c LLMConfig) IsRemote() bool { return c.profile().IsRemote() }

// Scope names what IsRemote reports, for display.
func (c LLMConfig) Scope() string { return c.profile().Scope() }

func (c LLMConfig) profile() LLMProfile {
	return LLMProfile{Provider: c.Provider, Model: c.Model, Endpoint: c.Endpoint}
}

// Redacted returns a copy safe to print or serialise.
func (c LLMConfig) Redacted() map[string]any {
	launchMode := c.LaunchMode
	if launchMode == "" {
		launchMode = "headless"
	}
	lifecycleMode := c.LifecycleMode
	if lifecycleMode == "" {
		lifecycleMode = "managed"
	}
	return map[string]any{
		"profile":       c.Profile,
		"provider":      c.Provider,
		"model":         c.Model,
		"endpoint":      c.Endpoint,
		"autoStart":     c.AutoStart,
		"launchMode":    launchMode,
		"lifecycleMode": lifecycleMode,
		"scope":         c.Scope(),
		"hasApiKey":     c.APIKey != "",
		"configured":    c.Provider != "",
	}
}

// Config renders a profile as the config a caller runs against.
func (p *LLMProfile) Config() LLMConfig {
	if p == nil {
		return LLMConfig{}
	}
	return LLMConfig{
		Profile: p.Name, Provider: p.Provider, Model: p.Model, Endpoint: p.Endpoint,
		AutoStart: p.AutoStart, LaunchMode: p.LaunchMode, LifecycleMode: p.LifecycleMode,
		APIKey: p.APIKey,
	}
}

// GetLLMConfig loads the default profile as a config.
//
// Returns a zero config, not an error, when nothing is configured — callers
// treat that as a normal branch.
func GetLLMConfig() (LLMConfig, error) {
	p, err := DefaultLLMProfile()
	if err != nil {
		return LLMConfig{}, err
	}
	return p.Config(), nil
}

// GetLLMConfigFor loads a named profile as a config, or the default when the
// name is empty.
func GetLLMConfigFor(profile string) (LLMConfig, error) {
	p, err := ResolveLLMProfile(profile)
	if err != nil {
		return LLMConfig{}, err
	}
	return p.Config(), nil
}

// SaveLLMConfig writes the config back to the profile it came from.
//
// An empty Provider clears that profile, which is what `iql llm disable`
// means. A config with no Profile name targets the default, which is what
// every pre-v22 caller intends.
func SaveLLMConfig(cfg LLMConfig) error {
	name := cfg.Profile
	if name == "" {
		name = DefaultProfileName
		if p, err := DefaultLLMProfile(); err == nil && p != nil {
			name = p.Name
		}
	}

	if cfg.Provider == "" {
		// Disabling. Removing the row is right: a profile with no provider is
		// not a configuration, and leaving one behind would list an entry that
		// cannot be used.
		if err := DeleteLLMProfile(name); err != nil && !strings.Contains(err.Error(), "no model profile") {
			return err
		}
		return nil
	}

	existing, err := GetLLMProfile(name)
	if err != nil {
		return err
	}
	p := &LLMProfile{Name: name, IsDefault: true}
	if existing != nil {
		p = existing
		p.IsDefault = true
	}
	p.Provider, p.Model, p.Endpoint = cfg.Provider, cfg.Model, cfg.Endpoint
	p.AutoStart, p.LaunchMode, p.LifecycleMode = cfg.AutoStart, cfg.LaunchMode, cfg.LifecycleMode
	p.APIKey = cfg.APIKey
	return SaveLLMProfile(p)
}

// migrateLLMSettingsToProfile folds the pre-v22 app_settings rows into a
// profile, and is a no-op when no provider was ever configured.
//
// Runs inside the v22 migration, against the raw handle rather than the
// package one, because the package handle is not yet published during open.
func migrateLLMSettingsToProfile(db *sql.DB) error {
	get := func(key string) string {
		var v sql.NullString
		db.QueryRow("SELECT value FROM app_settings WHERE key = ?", key).Scan(&v)
		return v.String
	}

	provider := strings.TrimSpace(get(settingLLMProvider))
	if provider == "" {
		// Nothing was configured, so there is nothing to carry forward. The
		// first profile someone creates becomes the default on its own.
		return nil
	}

	endpoint := strings.TrimSpace(get(settingLLMEndpoint))
	if endpoint == "" {
		endpoint = DefaultLLMEndpoints[provider]
	}
	launchMode := get(settingLLMLaunchMode)
	if launchMode == "" {
		launchMode = "headless"
	}
	lifecycleMode := get(settingLLMLifecycleMode)
	if lifecycleMode == "" {
		lifecycleMode = "managed"
	}

	// The sealed key is carried across as-is. Decrypting and re-encrypting it
	// here would fail on a machine whose vault key is missing, and turn an
	// upgrade into a lost credential.
	sealed := get(settingLLMAPIKey)

	now := time.Now().UnixMilli()
	_, err := db.Exec(`
		INSERT INTO llm_profiles (id, name, provider, model, endpoint, api_key,
			is_default, auto_start, launch_mode, lifecycle_mode, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO NOTHING`,
		uuid.New().String(), DefaultProfileName, provider, get(settingLLMModel), endpoint,
		nullIfEmpty(sealed), get(settingLLMAutoStart) == "true",
		launchMode, lifecycleMode, now, now)
	return err
}
