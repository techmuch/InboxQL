package blobstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutIsContentAddressedAndDeduplicates(t *testing.T) {
	s := New(t.TempDir())

	// The same PDF sent to five people should occupy one file.
	first, err := s.Put([]byte("the same attachment"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	second, err := s.Put([]byte("the same attachment"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if first != second {
		t.Errorf("identical content produced different addresses: %s vs %s", first, second)
	}

	count, _, err := s.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if count != 1 {
		t.Errorf("stored %d blobs for identical content, want 1", count)
	}
}

func TestDifferentContentDoesNotCollide(t *testing.T) {
	s := New(t.TempDir())

	a, _ := s.Put([]byte("one"))
	b, _ := s.Put([]byte("two"))
	if a == b {
		t.Fatal("different content shares an address")
	}

	got, err := s.Read(a)
	if err != nil || string(got) != "one" {
		t.Errorf("Read(a) = %q, %v", got, err)
	}
}

func TestRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	payload := make([]byte, 1<<16)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	hash, err := s.Put(payload)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Read(hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(payload) {
		t.Fatalf("read %d bytes, wrote %d", len(got), len(payload))
	}
	for i := range payload {
		if got[i] != payload[i] {
			t.Fatalf("byte %d differs", i)
		}
	}
}

// A blob is written to a temporary file and renamed, so a crash mid-write can
// never leave truncated bytes at an address claiming to hold the whole thing.
func TestNoTemporaryFilesRemain(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if _, err := s.Put([]byte("content")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var stray []string
	filepath.Walk(s.Root(), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && len(info.Name()) > 0 && info.Name()[0] == '.' {
			stray = append(stray, path)
		}
		return nil
	})
	if len(stray) > 0 {
		t.Errorf("temporary files left behind: %v", stray)
	}
}

func TestMissingBlob(t *testing.T) {
	s := New(t.TempDir())
	// A real content address that nothing was ever stored under. It used to be
	// "deadbeef" here, which is now rejected as malformed before the store ever
	// looks — a different answer to a different question, so the fixture has to
	// be a well-formed address for this to still be testing absence.
	absent := strings.Repeat("a", 64)
	if s.Exists(absent) {
		t.Error("Exists reported an absent blob as present")
	}
	if _, err := s.Open(absent); err != ErrNotFound {
		t.Errorf("Open on a missing blob = %v, want ErrNotFound", err)
	}
}

// A key is a path component, so a key that is not a content address is a
// directory traversal waiting for the first caller who takes one from a URL.
func TestKeysThatAreNotContentAddresses(t *testing.T) {
	s := New(t.TempDir())

	for _, key := range []string{
		"../../../etc/passwd",
		"..",
		"/etc/passwd",
		"ab/../../../etc/passwd",
		"deadbeef",              // too short
		strings.Repeat("a", 63), // one short of an address
		strings.Repeat("a", 65), // one over
		strings.Repeat("A", 64), // hex, but not the lowercase we emit
		strings.Repeat("g", 64), // right length, not hex
		"",
	} {
		if ValidHash(key) {
			t.Errorf("ValidHash accepted %q", key)
		}
		if got := s.Path(key); got != "" {
			t.Errorf("Path(%q) built %q; a non-address must yield no path at all", key, got)
		}
		if s.Exists(key) {
			t.Errorf("Exists(%q) reported present", key)
		}
		if _, err := s.Open(key); err != ErrBadHash {
			t.Errorf("Open(%q) = %v, want ErrBadHash", key, err)
		}
		if err := s.Delete(key); err != ErrBadHash {
			t.Errorf("Delete(%q) = %v, want ErrBadHash", key, err)
		}
	}

	// And the hashes this package itself produces are still accepted.
	hash, err := s.Put([]byte("hello"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !ValidHash(hash) {
		t.Errorf("ValidHash rejected a hash this store produced: %q", hash)
	}
}

// An empty store is a legitimate state, not a broken one.
func TestUsageOnEmptyStore(t *testing.T) {
	count, bytes, err := New(t.TempDir()).Usage()
	if err != nil {
		t.Fatalf("Usage on an empty store: %v", err)
	}
	if count != 0 || bytes != 0 {
		t.Errorf("empty store reports %d blobs, %d bytes", count, bytes)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	s := New(t.TempDir())
	hash, _ := s.Put([]byte("x"))

	if err := s.Delete(hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Deleting again must not error: a sweep may race with another sweep.
	if err := s.Delete(hash); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}
