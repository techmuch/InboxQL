// Package filetext pulls readable text out of attachment bytes.
//
// # What this is for
//
// An attachment is opaque until something reads it. A mailbox where the
// invoice total, the contract clause and the scanned form are all invisible to
// search is a mailbox where the answer is present and unfindable — the file is
// listed, its name is listed, and the thing inside it is not.
//
// # Why per page
//
// Text comes back a page at a time, and is stored a page at a time. Two
// reasons, and the second matters more than it looks:
//
//   - A hit can say where it is. "Page 7" is a usable answer; "somewhere in
//     this 90-page PDF" is not.
//   - A whole document as one row matches every query that any part of it
//     satisfies, so relevance across a mixed corpus becomes meaningless — one
//     long file outranks everything simply by containing more words.
//
// # What it deliberately does not do
//
// No OCR. A PDF that is a photograph of paper extracts to nothing, and this
// package says so — [StatusEmpty] rather than an error — so a later pass can
// find exactly those files and do the expensive thing to them. Conflating "no
// text layer" with "extraction failed" would make that set unfindable.
package filetext

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Status is the outcome of an extraction attempt.
type Status string

const (
	// StatusOK means text was found.
	StatusOK Status = "ok"
	// StatusEmpty means the format was understood and held no text. A scanned
	// PDF is the common case, and it is a fact about the file rather than a
	// failure — the row records that we looked, so nothing looks again.
	StatusEmpty Status = "empty"
	// StatusUnsupported means nothing here can read this format.
	StatusUnsupported Status = "unsupported"
	// StatusFailed means the format should have been readable and was not.
	StatusFailed Status = "failed"
)

// Page is one page of extracted text.
type Page struct {
	// Number is 1-based, or 0 for a format with no pages.
	Number int
	Text   string
}

// Result is what an extraction produced.
type Result struct {
	Status Status
	Detail string
	// Extractor names what read the file, so a later OCR pass is
	// distinguishable from this one and either can be re-run alone.
	Extractor string
	Pages     []Page
}

// Characters totals the extracted text, for reporting.
func (r Result) Characters() int {
	n := 0
	for _, p := range r.Pages {
		n += len(p.Text)
	}
	return n
}

// Extract reads whatever text a file holds.
//
// mimeType steers the choice of reader; the bytes decide the outcome. A
// mismatch between the two is common in mail — a scanner that labels a PDF
// application/octet-stream is in this very mailbox — so the sniffing below
// looks at the content when the label is unhelpful.
func Extract(mimeType, filename string, data []byte) Result {
	switch kind(mimeType, filename, data) {
	case kindPlain:
		return extractPlain(data)
	case kindPDF:
		return extractPDF(data)
	default:
		return Result{
			Status:    StatusUnsupported,
			Detail:    fmt.Sprintf("no reader for %s", mimeType),
			Extractor: "none",
		}
	}
}

type fileKind int

const (
	kindUnknown fileKind = iota
	kindPlain
	kindPDF
)

// kind decides what a file is, preferring evidence over labels.
func kind(mimeType, filename string, data []byte) fileKind {
	base := strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.IndexByte(base, ';'); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	name := strings.ToLower(filename)

	// The magic number outranks the declared type. A PDF is a PDF whatever a
	// sending mail client decided to call it, and this mailbox contains
	// examples of both directions.
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return kindPDF
	}

	switch {
	case base == "application/pdf" || strings.HasSuffix(name, ".pdf"):
		return kindPDF
	case strings.HasPrefix(base, "text/"):
		return kindPlain
	case base == "application/json" || base == "application/xml":
		return kindPlain
	case strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".md") ||
		strings.HasSuffix(name, ".csv") || strings.HasSuffix(name, ".log"):
		return kindPlain
	}

	// An octet-stream that is really text: worth a look, because "unknown" is
	// what a lot of servers say about anything they were not sure of.
	if base == "" || base == "application/octet-stream" {
		if looksLikeText(data) {
			return kindPlain
		}
	}
	return kindUnknown
}

// looksLikeText decides whether bytes are readable text.
//
// Valid UTF-8 with no NULs and few control characters. Deliberately
// conservative: a false positive puts binary noise in the search index, where
// it is worse than the absence it replaces.
func looksLikeText(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	if !utf8.Valid(sample) {
		// A truncated final rune is not evidence of binary, so retry without
		// the tail that the sampling itself may have cut.
		if len(sample) < len(data) {
			trimmed := bytes.ToValidUTF8(sample, nil)
			if len(trimmed) < len(sample)*9/10 {
				return false
			}
		} else {
			return false
		}
	}

	control := 0
	for _, r := range string(sample) {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			control++
		}
	}
	return control*100 < len(sample)
}

func extractPlain(data []byte) Result {
	text := clean(string(data))
	if text == "" {
		return Result{Status: StatusEmpty, Extractor: "plain", Detail: "no readable text"}
	}
	return Result{
		Status:    StatusOK,
		Extractor: "plain",
		// One page: a text file has no pagination, and inventing one would
		// make "page 3" mean nothing a reader could check.
		Pages: []Page{{Number: 0, Text: text}},
	}
}

// clean normalises extracted text for storage and indexing.
//
// Collapsing runs of whitespace matters more for PDFs than it looks: text
// operators emit position-driven spacing, so a table comes out with runs of
// dozens of spaces and a tokenizer indexes the gaps.
func clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	var lastSpace, lastNewline bool
	for _, r := range s {
		switch {
		case r == '\n':
			if !lastNewline {
				b.WriteRune('\n')
			}
			lastNewline, lastSpace = true, false
		case unicode.IsSpace(r):
			if !lastSpace && !lastNewline {
				b.WriteRune(' ')
			}
			lastSpace = true
		case unicode.IsControl(r):
			// Dropped rather than kept: a control character in the index is
			// never what anyone is searching for.
		default:
			b.WriteRune(r)
			lastSpace, lastNewline = false, false
		}
	}
	return strings.TrimSpace(b.String())
}
