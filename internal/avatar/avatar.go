// Package avatar keeps pictures of people, fetched from Gravatar when someone
// asks for one and served from the mailbox ever after.
//
// # Why on request, and why stored
//
// A Gravatar lookup tells Gravatar the address being looked up — as a hash,
// but a hash of a common address is reversed by a list of common addresses.
// Loaded by the page, a picture would make that request on every view, for
// everyone on screen: a running log, at Gravatar, of whose mail you read.
//
// So nothing is fetched until a person presses a button for one address, the
// server makes that one request, and the picture is kept in the mailbox. The
// page only ever loads it from here. "There is no Gravatar for this address"
// is kept too, so nothing retries behind anybody's back.
//
// The server builds the Gravatar URL itself, from the address. Nothing a page
// sends becomes a URL this server fetches.
package avatar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status of an address's picture.
const (
	Stored = "stored" // a picture is kept
	None   = "none"   // Gravatar was asked and has none
	Absent = "absent" // nobody has asked
)

// MaxBytes is the largest picture kept. Gravatar serves a 256px image at a
// few kilobytes; anything near this is not one.
const MaxBytes = 1 << 20

// Meta is what is known about an address's picture.
type Meta struct {
	Address     string    `json:"address"`
	Status      string    `json:"status"`
	Source      string    `json:"source,omitempty"`
	FetchedAt   time.Time `json:"fetchedAt,omitzero"`
	ContentType string    `json:"contentType,omitempty"`
}

// ErrAddress is an address that is not one.
var ErrAddress = errors.New("not an email address")

// Store is the avatars folder of one mailbox.
type Store struct {
	Dir string
	// Base is Gravatar's avatar endpoint; tests point it elsewhere.
	Base   string
	Client *http.Client
}

// New is the store for a data directory.
func New(dataDir string) *Store {
	base := "https://gravatar.com/avatar/"
	if b := strings.TrimSpace(os.Getenv("INBOXQL_GRAVATAR_URL")); b != "" {
		base = b
	}
	return &Store{Dir: filepath.Join(dataDir, "avatars"), Base: base,
		Client: &http.Client{Timeout: 15 * time.Second}}
}

// Normalise lowercases and trims an address, refusing anything that is not
// a bare address.
func Normalise(address string) (string, error) {
	a := strings.ToLower(strings.TrimSpace(address))
	if a == "" || len(a) > 254 || strings.ContainsAny(a, " <>\"/\\\t\r\n") {
		return "", ErrAddress
	}
	parsed, err := mail.ParseAddress(a)
	if err != nil || parsed.Address != a || !strings.Contains(a, "@") {
		return "", ErrAddress
	}
	return a, nil
}

// Hash is Gravatar's key for an address: SHA-256 of it, lowercased and
// trimmed. It also names the files here, so the folder lists no addresses.
func Hash(address string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(address))))
	return hex.EncodeToString(sum[:])
}

func (s *Store) paths(address string) (img, meta string) {
	h := Hash(address)
	return filepath.Join(s.Dir, h+".img"), filepath.Join(s.Dir, h+".json")
}

// Meta reports what is known about an address.
func (s *Store) Meta(address string) (Meta, error) {
	a, err := Normalise(address)
	if err != nil {
		return Meta{}, err
	}
	_, mp := s.paths(a)
	b, err := os.ReadFile(mp)
	if errors.Is(err, os.ErrNotExist) {
		return Meta{Address: a, Status: Absent}, nil
	}
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return Meta{Address: a, Status: Absent}, nil
	}
	m.Address = a
	return m, nil
}

// Image returns a stored picture and its content type.
func (s *Store) Image(address string) ([]byte, Meta, error) {
	m, err := s.Meta(address)
	if err != nil {
		return nil, m, err
	}
	if m.Status != Stored {
		return nil, m, os.ErrNotExist
	}
	ip, _ := s.paths(m.Address)
	b, err := os.ReadFile(ip)
	return b, m, err
}

// FetchGravatar asks Gravatar for an address's picture, once, and keeps the
// answer — the picture, or that there is none.
func (s *Store) FetchGravatar(ctx context.Context, address string) (Meta, error) {
	a, err := Normalise(address)
	if err != nil {
		return Meta{}, err
	}
	url := s.Base + Hash(a) + "?d=404&s=256"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Meta{}, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return Meta{}, fmt.Errorf("asking Gravatar: %w", err)
	}
	defer resp.Body.Close()

	m := Meta{Address: a, Source: "gravatar", FetchedAt: time.Now().UTC()}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		m.Status = None
		_ = s.removeImage(a)
		return m, s.writeMeta(m)
	case resp.StatusCode != http.StatusOK:
		return Meta{}, fmt.Errorf("Gravatar answered %s", resp.Status)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") || strings.Contains(ct, "svg") {
		// SVG is an image that can carry script; Gravatar does not serve it,
		// and a picture is not worth the question.
		return Meta{}, fmt.Errorf("Gravatar sent %q, not a picture", ct)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return Meta{}, err
	}
	if len(body) > MaxBytes {
		return Meta{}, fmt.Errorf("the picture is over %d bytes", MaxBytes)
	}
	m.Status, m.ContentType = Stored, ct
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Meta{}, err
	}
	ip, _ := s.paths(a)
	if err := writeAtomic(ip, body); err != nil {
		return Meta{}, err
	}
	return m, s.writeMeta(m)
}

// Remove forgets an address's picture, and that one was ever asked for.
func (s *Store) Remove(address string) error {
	a, err := Normalise(address)
	if err != nil {
		return err
	}
	_, mp := s.paths(a)
	if err := s.removeImage(a); err != nil {
		return err
	}
	if err := os.Remove(mp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) removeImage(a string) error {
	ip, _ := s.paths(a)
	if err := os.Remove(ip); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) writeMeta(m Meta) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	_, mp := s.paths(m.Address)
	return writeAtomic(mp, b)
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Stored lists the addresses with a picture kept, with when each was fetched,
// so a page can ask for exactly those rather than for everyone on screen.
func (s *Store) Stored() (map[string]time.Time, error) {
	out := map[string]time.Time{}
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		var m Meta
		if json.Unmarshal(b, &m) == nil && m.Status == Stored && m.Address != "" {
			out[m.Address] = m.FetchedAt
		}
	}
	return out, nil
}
