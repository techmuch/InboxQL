// Package vault provides encryption at rest for sensitive values — currently
// IMAP/SMTP account passwords, which were previously stored as plaintext.
//
// # Threat model
//
// The key is machine-local: a random 256-bit key held in the data directory as
// vault.key with 0600 permissions. This protects credentials against the
// realistic leak paths for a self-hosted app — a stolen or copied uea.db, an
// unencrypted backup, a database file handed to someone for debugging. Anyone
// holding only the database cannot recover the passwords.
//
// It deliberately does NOT protect against an attacker who can already read the
// data directory as the user running UEA, since the key sits next to the
// database. Closing that gap requires a user-supplied passphrase (Argon2id),
// which is what requirements.md section 2.1.4 ultimately calls for. The "v1"
// tag in the ciphertext envelope exists so that upgrade can be rolled out
// without a flag day: a future v2 reader can recognise and re-wrap v1 values.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// KeyFileName is the name of the key file inside the data directory.
	KeyFileName = "vault.key"

	// envelopePrefix tags encrypted values so plaintext left over from before
	// the vault existed is distinguishable from ciphertext at a glance, both in
	// code and when eyeballing the database.
	envelopePrefix = "enc:v1:"

	keyLen = 32 // AES-256
)

var (
	mu     sync.RWMutex
	aead   cipher.AEAD
	loaded bool
)

// ErrNotInitialised is returned when Encrypt or Decrypt is called before Init.
var ErrNotInitialised = errors.New("vault: not initialised; call vault.Init first")

// Init loads the encryption key from dataDir, generating one on first run.
//
// It is safe to call more than once; subsequent calls are no-ops. The key file
// is created with 0600 permissions, and Init warns (but does not fail) if an
// existing key file is readable by anyone else, since refusing to start would
// be a worse outcome than a loud warning for a self-hosted tool.
func Init(dataDir string) error {
	mu.Lock()
	defer mu.Unlock()

	if loaded {
		return nil
	}

	keyPath := filepath.Join(dataDir, KeyFileName)
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("vault: failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("vault: failed to create GCM: %w", err)
	}

	aead = gcm
	loaded = true
	return nil
}

func loadOrCreateKey(keyPath string) ([]byte, error) {
	info, err := os.Stat(keyPath)
	switch {
	case err == nil:
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			log.Printf("WARN: vault key %s has permissions %#o; it should be 0600. "+
				"Anyone able to read it can decrypt stored account passwords.", keyPath, perm)
		}
		key, readErr := os.ReadFile(keyPath)
		if readErr != nil {
			return nil, fmt.Errorf("vault: failed to read key file: %w", readErr)
		}
		key = []byte(strings.TrimSpace(string(key)))
		decoded, decErr := base64.StdEncoding.DecodeString(string(key))
		if decErr != nil {
			return nil, fmt.Errorf("vault: key file %s is corrupt (not valid base64): %w", keyPath, decErr)
		}
		if len(decoded) != keyLen {
			return nil, fmt.Errorf("vault: key file %s is corrupt: expected a %d-byte key, got %d", keyPath, keyLen, len(decoded))
		}
		return decoded, nil

	case os.IsNotExist(err):
		key := make([]byte, keyLen)
		if _, randErr := rand.Read(key); randErr != nil {
			return nil, fmt.Errorf("vault: failed to generate key: %w", randErr)
		}
		encoded := base64.StdEncoding.EncodeToString(key)
		// O_EXCL so a key created by a concurrent start is never clobbered —
		// overwriting it would silently orphan every password already stored.
		f, createErr := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			if os.IsExist(createErr) {
				return loadOrCreateKey(keyPath)
			}
			return nil, fmt.Errorf("vault: failed to create key file: %w", createErr)
		}
		defer f.Close()
		if _, writeErr := f.WriteString(encoded); writeErr != nil {
			return nil, fmt.Errorf("vault: failed to write key file: %w", writeErr)
		}
		log.Printf("Generated new credential vault key at %s (keep this file safe; "+
			"without it, stored account passwords cannot be recovered).", keyPath)
		return key, nil

	default:
		return nil, fmt.Errorf("vault: failed to stat key file: %w", err)
	}
}

// IsEncrypted reports whether a stored value is already in the vault envelope.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, envelopePrefix)
}

// Encrypt seals a plaintext value into the vault envelope.
//
// The empty string is passed through unchanged: an account with no password
// stored should not become an opaque blob, and encrypting it would leak the
// fact that it is empty via a distinguishable constant anyway.
// Already-encrypted values are returned unchanged so callers can be careless
// about double-encryption.
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" || IsEncrypted(plaintext) {
		return plaintext, nil
	}

	mu.RLock()
	defer mu.RUnlock()
	if !loaded {
		return "", ErrNotInitialised
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("vault: failed to generate nonce: %w", err)
	}

	sealed := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return envelopePrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a vault envelope.
//
// Values without the envelope prefix are returned unchanged. That is what makes
// the migration from the old plaintext column transparent: rows written before
// the vault existed keep working, and MigrateAccountPasswords rewrites them in
// place on the next start.
func Decrypt(value string) (string, error) {
	if !IsEncrypted(value) {
		return value, nil
	}

	mu.RLock()
	defer mu.RUnlock()
	if !loaded {
		return "", ErrNotInitialised
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, envelopePrefix))
	if err != nil {
		return "", fmt.Errorf("vault: ciphertext is not valid base64: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("vault: ciphertext is too short to contain a nonce")
	}

	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Almost always means the key file was replaced or the row was written
		// with a different key. Say so, because "message authentication failed"
		// on its own sends people looking in the wrong place.
		return "", fmt.Errorf("vault: failed to decrypt (wrong or regenerated %s?): %w", KeyFileName, err)
	}
	return string(plaintext), nil
}
