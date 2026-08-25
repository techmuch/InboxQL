// Package importer reads mail out of a desktop client's local store and into
// InboxQL, without modifying the source.
//
// # Shape
//
// A [Source] is one client — Apple Mail, a directory of .eml files, later
// Thunderbird. Sources are strictly read-only: they open files O_RDONLY, never
// write, and never move or delete anything. Someone importing their mail
// archive is trusting InboxQL with the only copy they have.
//
// # Scanning and importing are separate
//
// [Source.Scan] answers "what is in there" and writes nothing. [Run] writes.
// Keeping them apart means looking at a mailbox cannot accidentally import
// forty thousand messages, and it lets the caller show real numbers before
// anyone commits to anything.
package importer

import (
	"context"
	"errors"
	"time"
)

// ErrPermissionDenied is returned by [Source.Detect] when a client's store is
// present but unreadable.
//
// On macOS this is nearly always TCC: ~/Library/Mail requires Full Disk Access,
// and the raw syscall error tells a person nothing about how to fix it. This is
// distinct from the store being absent, because the two need opposite advice.
var ErrPermissionDenied = errors.New("permission denied reading the mail store")

// Detection reports whether a source is usable on this machine.
type Detection struct {
	// Available means the client's store was found on disk.
	Available bool `json:"available"`
	// Readable means it can actually be opened. Available && !Readable is the
	// permission case.
	Readable bool `json:"readable"`
	// Root is where the store lives, for display.
	Root string `json:"root,omitempty"`
	// Detail describes the state in one line.
	Detail string `json:"detail"`
	// Remedy is actionable advice when unusable, empty otherwise.
	Remedy string `json:"remedy,omitempty"`
}

// Mailbox is one folder in a source. Mailboxes nest, so this is a flat list
// carrying parent links rather than a tree the caller has to walk.
type Mailbox struct {
	// ID is opaque and source-defined. It is what the API and CLI pass back;
	// it is deliberately never a filesystem path supplied by a client.
	ID string `json:"id"`
	// Name is the leaf name, e.g. "INBOX".
	Name string `json:"name"`
	// Path is the display path, e.g. "Archive/2024".
	Path string `json:"path"`
	// ParentID links to the enclosing mailbox, empty at the top level.
	ParentID string `json:"parentId,omitempty"`
	// Account groups mailboxes by the source's own account, where it has one.
	Account string `json:"account,omitempty"`
	// Messages and Bytes come from the filesystem and are always populated.
	Messages int   `json:"messages"`
	Bytes    int64 `json:"bytes"`
}

// ScanDepth selects how much work a scan does.
type ScanDepth string

const (
	// ScanFast counts files and sums their sizes. Effectively instant, and
	// enough to render a mailbox list.
	ScanFast ScanDepth = "fast"
	// ScanDeep parses every message for attachment counts, participants and
	// the duplicate preview. Minutes on a large mailbox, so it is never the
	// default and always reports progress.
	ScanDeep ScanDepth = "deep"
)

// Stats describes a mailbox. Fields only a deep scan can fill are zero after a
// fast scan; Depth says which was run so a caller never presents an
// unpopulated field as a real zero.
type Stats struct {
	MailboxID string    `json:"mailboxId"`
	Depth     ScanDepth `json:"depth"`

	Messages int   `json:"messages"`
	Bytes    int64 `json:"bytes"`

	// Unread, Attachments, AttachmentBytes and Contacts require a deep scan.
	Unread          int   `json:"unread"`
	Attachments     int   `json:"attachments"`
	AttachmentBytes int64 `json:"attachmentBytes"`
	Contacts        int   `json:"contacts"`

	// Partial counts messages whose body was never downloaded by the client.
	// Importing one stores an empty message, so they are reported rather than
	// silently included.
	Partial int `json:"partial"`

	// Unreadable counts files that could not be opened or parsed.
	Unreadable int `json:"unreadable"`

	// Oldest and Newest bound the mailbox in time.
	Oldest time.Time `json:"oldest,omitempty"`
	Newest time.Time `json:"newest,omitempty"`

	// AlreadyPresent is how many of these messages InboxQL already holds, matched
	// on Message-ID. The most useful number on a scan and the one that turns a
	// frightening import into an informed one. Deep scan only.
	AlreadyPresent int `json:"alreadyPresent"`
}

// Selection narrows which messages an import takes.
type Selection struct {
	// Limit caps the number of messages, newest first. Zero means no cap.
	Limit int `json:"limit,omitempty"`
	// Since and Until bound the message date, inclusive. Zero means unbounded.
	Since time.Time `json:"since,omitempty"`
	Until time.Time `json:"until,omitempty"`
}

// Wants reports whether a message dated at t falls inside the selection's date
// bounds. The limit is applied by the caller, which knows how many it has taken.
func (s Selection) Wants(t time.Time) bool {
	if !s.Since.IsZero() && t.Before(s.Since) {
		return false
	}
	if !s.Until.IsZero() && t.After(s.Until) {
		return false
	}
	return true
}

// RawMessage is one message as it sits in the source.
type RawMessage struct {
	// SourceID identifies the message within its source, for error reporting.
	SourceID string
	// Raw is the RFC822 bytes.
	Raw []byte
	// Flags carries IMAP-style flags the source could determine, e.g. \Seen.
	Flags []string
	// Mailbox is the display path the message came from.
	Mailbox string
	// Partial marks a message the client never fully downloaded.
	Partial bool
}

// MessageIter walks a mailbox's messages. Close must be called.
type MessageIter interface {
	// Next advances, reporting false at the end or on a fatal error.
	Next() bool
	// Message returns the current message.
	Message() *RawMessage
	// Err reports a fatal error that ended iteration early. Per-message
	// failures are not fatal: they are counted and skipped.
	Err() error
	Close() error
}

// Progress reports how far through a long operation a source has got.
type Progress struct {
	Mailbox string
	Current int
	Total   int
	Message string
}

// ProgressFunc receives progress updates. May be nil.
type ProgressFunc func(Progress)

// Report delivers an update, tolerating a nil func so callers never have to
// guard every call site.
func (f ProgressFunc) Report(p Progress) {
	if f != nil {
		f(p)
	}
}

// Source is one mail client InboxQL can read from.
type Source interface {
	// ID is stable and machine-facing, e.g. "apple-mail".
	ID() string
	// Name is human-facing, e.g. "Apple Mail".
	Name() string
	// Detect reports whether this client's store is present and readable.
	Detect() Detection
	// Mailboxes enumerates the folder tree with filesystem-cheap statistics.
	Mailboxes(ctx context.Context) ([]Mailbox, error)
	// Scan produces statistics at the requested depth.
	Scan(ctx context.Context, mailboxID string, depth ScanDepth, progress ProgressFunc) (Stats, error)
	// Open iterates the messages of one mailbox.
	Open(ctx context.Context, mailboxID string, sel Selection) (MessageIter, error)
}
