// Package blobstore keeps attachment bytes on disk, addressed by content.
//
// # Why not SQLite
//
// A 25 MB attachment in a BLOB column would destroy the property that makes
// `iql backup` cheap — SQLite's online backup copies the whole database, so one
// video attachment turns a two-second backup into a two-minute one. Files on
// disk can also be served, exported and scanned without a round trip through
// the database.
//
// # Content addressing
//
// The key is the SHA-256 of the bytes, so the same PDF sent to five people is
// stored once and four of those messages simply reference it. That also makes
// writes idempotent: re-importing a mailbox rewrites nothing.
//
// The cost is that deletion is not a local decision — a blob may be referenced
// by any number of messages — which is why [Store.Delete] is deliberately not
// called from the import path. Reclaiming unreferenced blobs is a sweep, and
// belongs with the other maintenance operations.
package blobstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DirName is the blob directory inside the data directory.
const DirName = "attachments"

// Store is a content-addressed file store.
type Store struct {
	root string
}

// New returns the blob store for a data directory. The directory is created
// lazily on first write, so constructing a store never touches the disk.
func New(dataDir string) *Store {
	return &Store{root: filepath.Join(dataDir, DirName)}
}

// Root is the directory holding every blob. Backups need it.
func (s *Store) Root() string { return s.root }

// Hash returns the content address of some bytes without storing them.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ErrBadHash rejects a key that is not a content address.
var ErrBadHash = errors.New("not a content address")

// ValidHash reports whether a string is a SHA-256 content address.
//
// # Why this is not merely tidiness
//
// A blob's path is built by joining the key onto the store root. A key is
// normally a hash this package produced, but the moment one arrives from
// outside — a URL parameter naming a file to serve is the obvious case — a key
// like "../../../etc/passwd" would read whatever the process can read, and one
// containing a separator would write outside the store. Every path-building
// method checks this rather than trusting callers to, because the caller that
// forgets is exactly the one that is taking the key from a request.
func ValidHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for i := 0; i < len(hash); i++ {
		c := hash[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// path shards by the first byte of the hash, so no single directory ends up
// with hundreds of thousands of entries.
//
// Returns "" for anything that is not a content address, and every caller
// treats that as absent. Constructing a path from an unvalidated key is the
// one mistake in this package that would be a directory traversal rather than
// a bug.
func (s *Store) path(hash string) string {
	if !ValidHash(hash) {
		return ""
	}
	return filepath.Join(s.root, hash[:2], hash)
}

// Path is where a blob lives, whether or not it exists yet. Empty when the
// key is not a content address.
func (s *Store) Path(hash string) string { return s.path(hash) }

// Exists reports whether a blob is already stored.
func (s *Store) Exists(hash string) bool {
	p := s.path(hash)
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// Put stores bytes and returns their content address.
//
// Storing something already present is a no-op that still returns the hash, so
// callers do not need to check first.
func (s *Store) Put(data []byte) (string, error) {
	hash := Hash(data)
	dest := s.path(hash)

	if _, err := os.Stat(dest); err == nil {
		return hash, nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("cannot create the blob directory: %w", err)
	}

	// Write to a temporary file and rename, so a crash mid-write cannot leave a
	// truncated blob sitting at an address that claims to hold complete data.
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("cannot create a temporary blob: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("cannot write blob: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return "", fmt.Errorf("cannot commit blob: %w", err)
	}
	return hash, nil
}

// ErrNotFound is returned when a blob is absent.
var ErrNotFound = errors.New("blob not found")

// Open returns a reader over a stored blob.
func (s *Store) Open(hash string) (io.ReadCloser, error) {
	f, err := s.OpenFile(hash)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// OpenFile returns the blob as a file, for callers that need to seek.
//
// Serving over HTTP wants this rather than [Store.Open]: http.ServeContent
// needs a ReadSeeker to answer a Range request, and reading a 25 MB attachment
// into memory to hand it to the response writer is the alternative.
func (s *Store) OpenFile(hash string) (*os.File, error) {
	p := s.path(hash)
	if p == "" {
		return nil, ErrBadHash
	}
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return f, err
}

// Read returns a blob's bytes.
func (s *Store) Read(hash string) ([]byte, error) {
	r, err := s.Open(hash)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// Delete removes a blob.
//
// Only safe once nothing references it. Import never calls this; a sweep over
// unreferenced blobs does.
func (s *Store) Delete(hash string) error {
	p := s.path(hash)
	if p == "" {
		return ErrBadHash
	}
	err := os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Usage totals the blobs on disk.
func (s *Store) Usage() (count int, bytes int64, err error) {
	err = filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// An absent store is empty, not broken.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			count++
			bytes += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	return count, bytes, err
}
