package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reset clears package state so each test can Init against its own temp dir.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	aead = nil
	loaded = false
}

func initTemp(t *testing.T) string {
	t.Helper()
	reset()
	t.Cleanup(reset)
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return dir
}

func TestRoundTrip(t *testing.T) {
	initTemp(t)

	for _, plaintext := range []string{
		"hunter2",
		"a password with spaces and ünïcödé",
		strings.Repeat("long", 1000),
		" ", // whitespace is not the same as empty and must survive intact
	} {
		sealed, err := Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if sealed == plaintext {
			t.Errorf("Encrypt(%q) returned the plaintext unchanged", plaintext)
		}
		if !IsEncrypted(sealed) {
			t.Errorf("Encrypt(%q) produced a value without the envelope prefix: %q", plaintext, sealed)
		}
		if strings.Contains(sealed, plaintext) {
			t.Errorf("ciphertext for %q contains the plaintext", plaintext)
		}

		opened, err := Decrypt(sealed)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if opened != plaintext {
			t.Errorf("round trip changed the value: got %q, want %q", opened, plaintext)
		}
	}
}

func TestEmptyStringPassesThrough(t *testing.T) {
	initTemp(t)

	sealed, err := Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt(\"\"): %v", err)
	}
	if sealed != "" {
		t.Errorf("expected empty string to pass through, got %q", sealed)
	}

	opened, err := Decrypt("")
	if err != nil {
		t.Fatalf("Decrypt(\"\"): %v", err)
	}
	if opened != "" {
		t.Errorf("expected empty string to pass through, got %q", opened)
	}
}

// The migration path depends on this: rows written before the vault existed
// hold bare plaintext and must keep working until they are rewritten.
func TestDecryptPassesThroughLegacyPlaintext(t *testing.T) {
	initTemp(t)

	got, err := Decrypt("legacy-plaintext-password")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "legacy-plaintext-password" {
		t.Errorf("got %q, want the plaintext unchanged", got)
	}
}

func TestEncryptIsNotDeterministic(t *testing.T) {
	initTemp(t)

	first, err := Encrypt("same-password")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := Encrypt("same-password")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if first == second {
		t.Error("encrypting the same value twice produced identical ciphertext; " +
			"the nonce is not random, which leaks which accounts share a password")
	}
}

func TestDoubleEncryptIsANoOp(t *testing.T) {
	initTemp(t)

	once, err := Encrypt("hunter2")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	twice, err := Encrypt(once)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if once != twice {
		t.Error("encrypting an already-encrypted value wrapped it a second time")
	}
}

func TestKeyPersistsAcrossInit(t *testing.T) {
	dir := initTemp(t)

	sealed, err := Encrypt("hunter2")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Simulate a restart: drop in-memory state, re-init against the same dir.
	reset()
	if err := Init(dir); err != nil {
		t.Fatalf("re-Init: %v", err)
	}

	opened, err := Decrypt(sealed)
	if err != nil {
		t.Fatalf("Decrypt after restart: %v", err)
	}
	if opened != "hunter2" {
		t.Errorf("got %q after restart, want %q", opened, "hunter2")
	}
}

func TestWrongKeyFailsLoudly(t *testing.T) {
	dir := initTemp(t)

	sealed, err := Encrypt("hunter2")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Regenerate the key, as would happen if someone deleted vault.key.
	reset()
	if err := os.Remove(filepath.Join(dir, KeyFileName)); err != nil {
		t.Fatalf("remove key: %v", err)
	}
	if err := Init(dir); err != nil {
		t.Fatalf("Init with fresh key: %v", err)
	}

	if _, err := Decrypt(sealed); err == nil {
		t.Fatal("expected decryption with a regenerated key to fail, but it succeeded")
	}
}

func TestKeyFileIsNotWorldReadable(t *testing.T) {
	dir := initTemp(t)

	info, err := os.Stat(filepath.Join(dir, KeyFileName))
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("key file permissions are %#o; want no group/other access", perm)
	}
}

func TestOperationsBeforeInitFail(t *testing.T) {
	reset()
	t.Cleanup(reset)

	if _, err := Encrypt("hunter2"); err != ErrNotInitialised {
		t.Errorf("Encrypt before Init: got %v, want ErrNotInitialised", err)
	}
	// A value already in the envelope cannot be opened without a key either.
	if _, err := Decrypt(envelopePrefix + "AAAA"); err != ErrNotInitialised {
		t.Errorf("Decrypt before Init: got %v, want ErrNotInitialised", err)
	}
}

func TestCorruptKeyFileIsRejected(t *testing.T) {
	reset()
	t.Cleanup(reset)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), []byte("not-base64!!!"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := Init(dir); err == nil {
		t.Fatal("expected Init to reject a corrupt key file, but it succeeded")
	}
}

func TestShortKeyFileIsRejected(t *testing.T) {
	reset()
	t.Cleanup(reset)

	dir := t.TempDir()
	// Valid base64, but only 4 bytes rather than 32.
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), []byte("AAAAAA=="), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := Init(dir); err == nil {
		t.Fatal("expected Init to reject an undersized key, but it succeeded")
	}
}

func TestTamperedCiphertextIsRejected(t *testing.T) {
	initTemp(t)

	sealed, err := Encrypt("hunter2")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Flip the final character of the base64 payload. GCM is authenticated, so
	// this must be detected rather than yielding garbage plaintext.
	body := []byte(strings.TrimPrefix(sealed, envelopePrefix))
	if body[len(body)-1] == 'A' {
		body[len(body)-1] = 'B'
	} else {
		body[len(body)-1] = 'A'
	}

	if _, err := Decrypt(envelopePrefix + string(body)); err == nil {
		t.Fatal("expected tampered ciphertext to be rejected, but it decrypted")
	}
}
