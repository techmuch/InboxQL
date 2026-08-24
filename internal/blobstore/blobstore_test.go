package blobstore

import (
	"os"
	"path/filepath"
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
	if s.Exists("deadbeef") {
		t.Error("Exists reported an absent blob as present")
	}
	if _, err := s.Open("deadbeef"); err != ErrNotFound {
		t.Errorf("Open on a missing blob = %v, want ErrNotFound", err)
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
