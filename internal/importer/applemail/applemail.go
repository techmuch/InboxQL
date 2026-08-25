// Package applemail imports from Mail.app's on-disk store.
//
// # Layout
//
//	~/Library/Mail/V<n>/<account-uuid>/<Mailbox>.mbox/<uuid>/Data/<shard>/<shard>/Messages/<id>.emlx
//
// V<n> tracks the macOS release, so the highest present is detected rather than
// hardcoded. Mailboxes nest — Archive.mbox/2024.mbox — which is why discovery
// produces a tree rather than a list.
//
// # Permissions
//
// ~/Library/Mail is TCC-protected. Without Full Disk Access the directory
// exists but every read fails with EPERM, and the raw error tells a person
// nothing useful. Detect separates that case from the store being absent,
// because the two need opposite advice.
//
// Everything here is read-only. Nothing is written, moved or deleted under
// ~/Library/Mail: someone importing their archive may be trusting InboxQL with the
// only copy they have.
package applemail

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/user/inboxql/internal/importer"
	"github.com/user/inboxql/internal/message"
)

// versionDir matches Mail's top-level container, V2 through V10 and onwards.
var versionDir = regexp.MustCompile(`^V(\d+)$`)

// Source reads Mail.app's store.
type Source struct {
	// root is the resolved ~/Library/Mail/V<n>, empty until Detect runs.
	root string
	// mailRoot is ~/Library/Mail, overridable for testing.
	mailRoot string
}

// New returns a source reading the current user's Mail store.
func New() *Source { return &Source{} }

// NewAt returns a source rooted at an explicit ~/Library/Mail equivalent.
// Used by tests against a synthetic store; not reachable from the API.
func NewAt(mailRoot string) *Source { return &Source{mailRoot: mailRoot} }

func (s *Source) ID() string   { return "apple-mail" }
func (s *Source) Name() string { return "Apple Mail" }

const fullDiskAccessRemedy = "Grant Full Disk Access to the program running InboxQL, then restart it:\n" +
	"  System Settings → Privacy & Security → Full Disk Access"

func (s *Source) mailDir() string {
	if s.mailRoot != "" {
		return s.mailRoot
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Mail")
}

// Detect locates the store and establishes whether it can actually be read.
func (s *Source) Detect() importer.Detection {
	dir := s.mailDir()
	if dir == "" {
		return importer.Detection{Detail: "cannot determine the current user's home directory"}
	}

	entries, err := os.ReadDir(dir)
	switch {
	case os.IsNotExist(err):
		return importer.Detection{
			Detail: fmt.Sprintf("%s does not exist — Apple Mail has never run for this user", dir),
		}
	case os.IsPermission(err):
		// The interesting case, and the one people lose an hour to.
		return importer.Detection{
			Available: true, Root: dir,
			Detail: fmt.Sprintf("%s exists but cannot be read", dir),
			Remedy: fullDiskAccessRemedy + "\n  Currently running as: " + executablePath(),
		}
	case err != nil:
		return importer.Detection{Available: true, Root: dir, Detail: err.Error()}
	}

	best, bestVersion := "", -1
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := versionDir.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if n, err := strconv.Atoi(m[1]); err == nil && n > bestVersion {
			bestVersion, best = n, filepath.Join(dir, e.Name())
		}
	}
	if best == "" {
		return importer.Detection{
			Available: true, Root: dir,
			Detail: fmt.Sprintf("no V<n> mail store found under %s", dir),
			Remedy: "This layout is not one InboxQL recognises. Export messages to .eml and use `iql import eml` instead.",
		}
	}

	// Readable at the top level does not mean readable throughout: TCC can
	// allow the listing and refuse the contents, so probe one level deeper.
	if _, err := os.ReadDir(best); os.IsPermission(err) {
		return importer.Detection{
			Available: true, Root: best,
			Detail: fmt.Sprintf("%s cannot be read", best),
			Remedy: fullDiskAccessRemedy + "\n  Currently running as: " + executablePath(),
		}
	}

	s.root = best
	return importer.Detection{
		Available: true, Readable: true, Root: best,
		Detail: fmt.Sprintf("Apple Mail store V%d", bestVersion),
	}
}

func executablePath() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return "the iql binary"
}

// ensureRoot resolves the store if Detect has not already run.
func (s *Source) ensureRoot() error {
	if s.root != "" {
		return nil
	}
	d := s.Detect()
	if !d.Available {
		return fmt.Errorf("%s", d.Detail)
	}
	if !d.Readable {
		return fmt.Errorf("%w: %s\n\n%s", importer.ErrPermissionDenied, d.Detail, d.Remedy)
	}
	return nil
}

// Mailboxes walks the .mbox tree. Filesystem work only — no message is opened.
func (s *Source) Mailboxes(ctx context.Context) ([]importer.Mailbox, error) {
	if err := s.ensureRoot(); err != nil {
		return nil, err
	}

	accounts, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}

	var boxes []importer.Mailbox
	for _, account := range accounts {
		// MailData holds indexes and settings, not mail.
		if !account.IsDir() || account.Name() == "MailData" {
			continue
		}
		accountDir := filepath.Join(s.root, account.Name())
		found, err := s.walkMailboxes(ctx, accountDir, account.Name(), "", "")
		if err != nil {
			return nil, err
		}
		boxes = append(boxes, found...)
	}

	importer.SortMailboxes(boxes)
	return boxes, nil
}

// walkMailboxes recurses through nested .mbox directories.
func (s *Source) walkMailboxes(ctx context.Context, dir, account, parentID, parentPath string) ([]importer.Mailbox, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// One unreadable folder should not sink the whole listing.
		return nil, nil //nolint:nilerr
	}

	var boxes []importer.Mailbox
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".mbox") {
			continue
		}

		name := strings.TrimSuffix(e.Name(), ".mbox")
		path := name
		if parentPath != "" {
			path = parentPath + "/" + name
		}
		full := filepath.Join(dir, e.Name())

		// The id is the path relative to the store root. Opaque to callers,
		// resolvable only by this source, and never a path a client supplied.
		id, err := filepath.Rel(s.root, full)
		if err != nil {
			continue
		}

		count, bytes := countMessages(full)
		boxes = append(boxes, importer.Mailbox{
			ID: id, Name: name, Path: path, ParentID: parentID,
			Account: account, Messages: count, Bytes: bytes,
		})

		nested, err := s.walkMailboxes(ctx, full, account, id, path)
		if err != nil {
			return nil, err
		}
		boxes = append(boxes, nested...)
	}
	return boxes, nil
}

// countMessages totals the .emlx files belonging to one mailbox, excluding any
// nested child mailbox so counts are not double-reported up the tree.
func countMessages(mailboxDir string) (count int, bytes int64) {
	for _, path := range messageFiles(mailboxDir) {
		if info, err := os.Stat(path); err == nil {
			count++
			bytes += info.Size()
		}
	}
	return count, bytes
}

// messageFiles lists the .emlx files owned by a mailbox.
//
// Apple shards messages across numbered directories under Data/, so this walks
// for Messages/ directories rather than assuming a depth. Nested .mbox children
// are pruned: they are separate mailboxes with their own counts.
func messageFiles(mailboxDir string) []string {
	var out []string
	_ = filepath.WalkDir(mailboxDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr
		}
		if d.IsDir() {
			if path != mailboxDir && strings.HasSuffix(d.Name(), ".mbox") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".emlx") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// isPartial reports whether a file holds a message Mail never fully downloaded.
func isPartial(path string) bool {
	return strings.HasSuffix(path, ".partial.emlx")
}

func (s *Source) Scan(ctx context.Context, mailboxID string, depth importer.ScanDepth, progress importer.ProgressFunc) (importer.Stats, error) {
	if err := s.ensureRoot(); err != nil {
		return importer.Stats{}, err
	}
	dir := filepath.Join(s.root, mailboxID)
	files := messageFiles(dir)

	stats := importer.Stats{MailboxID: mailboxID, Depth: depth, Messages: len(files)}
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			stats.Bytes += info.Size()
		}
		if isPartial(f) {
			stats.Partial++
		}
	}
	if depth != importer.ScanDeep {
		return stats, nil
	}

	// The deep pass is the expensive one: every message is read and its MIME
	// structure walked, which is minutes on a large mailbox. Hence progress,
	// and hence never being the default.
	contacts := map[string]struct{}{}
	stats.Unread = 0
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		progress.Report(importer.Progress{
			Mailbox: mailboxID, Current: i + 1, Total: len(files),
			Message: filepath.Base(f),
		})

		if isPartial(f) {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			stats.Unreadable++
			continue
		}
		raw, plist, ok := importer.SplitEMLX(data)
		if !ok {
			raw = data
		}
		msg, err := message.ParseRFC822(raw)
		if msg == nil {
			stats.Unreadable++
			continue
		}
		_ = err
		msg.Flags = importer.EMLXFlags(plist)
		importer.AccumulateStats(&stats, msg, raw, contacts)
	}
	stats.Contacts = len(contacts)
	return stats, nil
}

func (s *Source) Open(ctx context.Context, mailboxID string, sel importer.Selection) (importer.MessageIter, error) {
	if err := s.ensureRoot(); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.root, mailboxID)
	files := messageFiles(dir)

	// Newest first, so a --limit takes the most recent mail rather than an
	// arbitrary slice of the archive. Apple's ids ascend with time, and the
	// sorted order is by shard path, so reversing is a good enough proxy
	// without stat-ing every file.
	for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
		files[i], files[j] = files[j], files[i]
	}

	return &iter{files: files, index: -1, mailbox: mailboxID}, nil
}

type iter struct {
	files   []string
	index   int
	mailbox string
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

		if isPartial(path) {
			i.current = &importer.RawMessage{
				SourceID: filepath.Base(path), Mailbox: i.mailbox, Partial: true,
			}
			return true
		}

		data, err := os.ReadFile(path)
		if err != nil {
			// Mail.app may be rewriting this file right now. Skip it rather
			// than failing the run.
			continue
		}
		raw, plist, ok := importer.SplitEMLX(data)
		if !ok {
			raw = data
		}
		i.current = &importer.RawMessage{
			SourceID: filepath.Base(path),
			Raw:      raw,
			Flags:    importer.EMLXFlags(plist),
			Mailbox:  i.mailbox,
		}
		return true
	}
}

func (i *iter) Message() *importer.RawMessage { return i.current }
func (i *iter) Err() error                    { return i.err }
func (i *iter) Close() error                  { return nil }
