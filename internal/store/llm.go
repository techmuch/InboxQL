package store

import (
	"fmt"

	"github.com/user/inboxql/internal/vault"
)

// Setting keys for the optional LLM gateway.
const (
	settingLLMProvider = "llm_provider"
	settingLLMModel    = "llm_model"
	settingLLMEndpoint = "llm_endpoint"
	settingLLMAPIKey   = "llm_api_key"
)

// LLMConfig describes the configured completion provider.
//
// An empty Provider means no LLM is configured, which is a supported state:
// analyze and draft fall back to emitting structured context for an external
// agent to reason over.
type LLMConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Endpoint string `json:"endpoint"`
	// APIKey is decrypted on read and encrypted on write. It is never included
	// in JSON output — see LLMConfig.Redacted.
	APIKey string `json:"-"`
}

// HasAPIKey reports whether a key is stored, without revealing it.
func (c LLMConfig) HasAPIKey() bool { return c.APIKey != "" }

// Redacted returns a copy safe to print or serialise.
func (c LLMConfig) Redacted() map[string]any {
	return map[string]any{
		"provider":   c.Provider,
		"model":      c.Model,
		"endpoint":   c.Endpoint,
		"hasApiKey":  c.APIKey != "",
		"configured": c.Provider != "",
	}
}

// GetLLMConfig loads the provider settings, decrypting the API key.
func GetLLMConfig() (LLMConfig, error) {
	var cfg LLMConfig
	var err error

	if cfg.Provider, err = GetSetting(settingLLMProvider); err != nil {
		return cfg, err
	}
	if cfg.Model, err = GetSetting(settingLLMModel); err != nil {
		return cfg, err
	}
	if cfg.Endpoint, err = GetSetting(settingLLMEndpoint); err != nil {
		return cfg, err
	}

	stored, err := GetSetting(settingLLMAPIKey)
	if err != nil {
		return cfg, err
	}
	// The key is sealed with the same vault that protects account passwords,
	// so app_settings never holds a usable credential in plaintext.
	if cfg.APIKey, err = vault.Decrypt(stored); err != nil {
		return cfg, fmt.Errorf("cannot decrypt the stored LLM API key: %w", err)
	}
	return cfg, nil
}

// SaveLLMConfig persists the provider settings, encrypting the API key.
func SaveLLMConfig(cfg LLMConfig) error {
	encrypted, err := vault.Encrypt(cfg.APIKey)
	if err != nil {
		return fmt.Errorf("cannot encrypt the LLM API key: %w", err)
	}

	for key, value := range map[string]string{
		settingLLMProvider: cfg.Provider,
		settingLLMModel:    cfg.Model,
		settingLLMEndpoint: cfg.Endpoint,
		settingLLMAPIKey:   encrypted,
	} {
		if err := UpdateSetting(key, value); err != nil {
			return err
		}
	}
	return nil
}
