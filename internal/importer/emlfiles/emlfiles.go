// Package emlfiles imports a directory of .eml files.
//
// This is the source that needs no special permission. Selecting messages in
// Mail.app and dragging them to a Finder folder exports them as .eml, which
// makes this the fastest route to a working import — and the fallback whenever
// a macOS update moves Apple's private storage around.
package emlfiles

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/user/inboxql/internal/importer"
	"github.com/user/inboxql/internal/message"
)

// Source reads .eml files from a directory tree.
type Source struct {
	root string
}

// New returns a source rooted at a directory or a single file.
//
// The path comes from the command line, never from an HTTP request: a server
// endpoint that reads a client-supplied path is a directory traversal hole.
func New(root string) *Source {
	return &Source{root: filepath.Clean(root)}
}

func (s *Source) ID() string   { return "eml" }
func (s *Source) Name() string { return "Email files (.eml)" }

func (s *Source) Detect() importer.Detection {
	info, err := os.Stat(s.root)
	switch {
	case os.IsNotExist(err):
		return importer.Detection{
			Detail: fmt.Sprintf("%s does not exist", s.root),
			Remedy: "Export messages from your mail client into a folder, then point --path at it.",
		}
	case os.IsPermission(err):
		return importer.Detection{
			Available: true, Root: s.root,
			Detail: fmt.Sprintf("%s cannot be read", s.root),
			Remedy: "Check the folder's permissions.",
		}
	case err != nil:
		return importer.Detection{Detail: err.Error()}
	}

	files, err := s.files()
	if err != nil {
		return importer.Detection{Available: true, Root: s.root, Detail: err.Error()}
	}
	if len(files) == 0 {
		return importer.Detection{
			Available: true, Readable: true, Root: s.root,
			Detail: fmt.Sprintf("%s contains no .eml files", s.root),
			Remedy: "In Mail.app, select the messages you want and drag them into this folder.",
		}
	}

	kind := "folder"
	if !info.IsDir() {
		kind = "file"
	}
	return importer.Detection{
		Available: true, Readable: true, Root: s.root,
		Detail: fmt.Sprintf("%d .eml file(s) in this %s", len(files), kind),
	}
}

// files lists every .eml under the root, sorted for a stable order.
func (s *Source) files() ([]string, error) {
	info, err := os.Stat(s.root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if isEML(s.root) {
			return []string{s.root}, nil
		}
		return nil, nil
	}

	var out []string
	err = filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory should cost that subtree, not the walk.
			return nil //nolint:nilerr
		}
		if !d.IsDir() && isEML(path) {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func isEML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".eml" || ext == ".emlx"
}

// Mailboxes reports the directory as a single mailbox. A folder of exported
// messages has no folder structure worth reconstructing.
func (s *Source) Mailboxes(ctx context.Context) ([]importer.Mailbox, error) {
	files, err := s.files()
	if err != nil {
		return nil, err
	}
	var bytes int64
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			bytes += info.Size()
		}
	}
	return []importer.Mailbox{{
		ID:       ".",
		Name:     filepath.Base(s.root),
		Path:     filepath.Base(s.root),
		Messages: len(files),
		Bytes:    bytes,
	}}, nil
}

func (s *Source) Scan(ctx context.Context, mailboxID string, depth importer.ScanDepth, progress importer.ProgressFunc) (importer.Stats, error) {
	files, err := s.files()
	if err != nil {
		return importer.Stats{}, err
	}

	stats := importer.Stats{MailboxID: mailboxID, Depth: depth, Messages: len(files)}
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			stats.Bytes += info.Size()
		}
	}
	if depth != importer.ScanDeep {
		return stats, nil
	}

	contacts := map[string]struct{}{}
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		progress.Report(importer.Progress{Mailbox: mailboxID, Current: i + 1, Total: len(files), Message: filepath.Base(f)})

		raw, err := os.ReadFile(f)
		if err != nil {
			stats.Unreadable++
			continue
		}
		msg, err := message.ParseRFC822(raw)
		if err != nil && msg == nil {
			stats.Unreadable++
			continue
		}
		importer.AccumulateStats(&stats, msg, raw, contacts)
	}
	stats.Contacts = len(contacts)
	return stats, nil
}

func (s *Source) Open(ctx context.Context, mailboxID string, sel importer.Selection) (importer.MessageIter, error) {
	files, err := s.files()
	if err != nil {
		return nil, err
	}
	return &iter{files: files, index: -1}, nil
}

type iter struct {
	files   []string
	index   int
	current *importer.RawMessage
	err     error
}

func (i *iter) Next() bool {
	for {
		i.index++
		if i.index >= len(i.files) {
			return false
		}
		path := i.files[i.index]
		raw, err := os.ReadFile(path)
		if err != nil {
			// Skip rather than abort; the engine counts what it never saw via
			// the file total.
			continue
		}
		// A .emlx dragged out of Mail keeps its length prefix, so strip it if
		// present rather than feeding the prefix line to the MIME parser.
		if body, _, ok := importer.SplitEMLX(raw); ok {
			raw = body
		}
		i.current = &importer.RawMessage{
			SourceID: filepath.Base(path),
			Raw:      raw,
			Mailbox:  filepath.Base(filepath.Dir(path)),
		}
		return true
	}
}

func (i *iter) Message() *importer.RawMessage { return i.current }
func (i *iter) Err() error                    { return i.err }
func (i *iter) Close() error                  { return nil }
