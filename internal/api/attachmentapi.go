package api

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/store"
)

// # Serving attachment bytes
//
// This is the one endpoint that hands a browser bytes that arrived from
// strangers. Everything else in this API returns data InboxQL produced; here
// the payload is whatever anyone chose to email, and the browser will do what
// its content type tells it to. So the posture is: the response says exactly
// what it is, the browser is told not to guess, and nothing the request asks
// for can widen what is allowed.
//
// Five rules, each closing a specific door:
//
//  1. The content type is chosen from an allowlist keyed on what the *store*
//     recorded, never on what the request asked for and never by sniffing.
//     A type not on the list is served as application/octet-stream.
//  2. Content-Disposition is "attachment" unless the type is one of the few
//     that are safe to render, so a .html or .svg someone mailed downloads
//     rather than executes on this origin.
//  3. X-Content-Type-Options: nosniff, so a browser cannot decide our
//     octet-stream was really HTML after all — which is precisely how a
//     download becomes stored XSS.
//  4. Content-Security-Policy: sandbox, so even a document that does reach
//     top-level rendering has no script, no forms, and no same-origin access
//     to the session cookie sitting on this host.
//  5. The blob key must be a content address. It becomes a path component,
//     and blobstore.ValidHash is what stops "../../.." reading the vault key.
//
// The filename is the other injection surface: it came from a mail header, so
// it can contain quotes, newlines, and non-ASCII. It is emitted through
// mime.FormatMediaType, which quotes and escapes, plus an RFC 5987 form for
// anything outside ASCII.

// inlineTypes are the content types a browser may render in place.
//
// Deliberately short, and deliberately not "images and documents". Two
// omissions are the point:
//
//   - image/svg+xml is not an image as far as a browser is concerned; it is a
//     document that can carry script. It downloads.
//   - text/html obviously downloads. So does anything XML-shaped, which is the
//     same problem wearing a different type.
//
// application/pdf is here because previewing a mailed PDF is most of the value
// of previewing anything, and because a browser's PDF viewer is itself
// sandboxed. It is still served under CSP sandbox.
var inlineTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
	"image/avif": true,
	"text/plain": true,

	"application/pdf": true,
}

// safeContentType maps what the store recorded onto what will be sent.
//
// Returns the type to serve and whether it may be rendered inline. Anything
// unrecognised becomes application/octet-stream: the honest answer for bytes
// nothing here can vouch for, and the one that makes nosniff meaningful.
func safeContentType(stored string) (contentType string, inlineOK bool) {
	// Parameters on a stored type are dropped rather than passed through. A
	// charset or a boundary from a mail header has no business steering how
	// this response is interpreted.
	base := strings.ToLower(strings.TrimSpace(stored))
	if i := strings.IndexByte(base, ';'); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}

	if inlineTypes[base] {
		if base == "text/plain" {
			// An explicit charset, because without one a browser guesses, and
			// UTF-7 guessing was an XSS vector for years.
			return "text/plain; charset=utf-8", true
		}
		return base, true
	}
	return "application/octet-stream", false
}

// contentDisposition builds the header, with the filename escaped both ways.
//
// The name came out of a mail header written by a stranger, so it may contain
// quotes, semicolons, control characters or CRLF. mime.FormatMediaType handles
// quoting and refuses what it cannot represent; the ASCII fallback covers the
// case where it refuses, so a hostile name costs the download its name rather
// than costing the response its integrity.
func contentDisposition(kind, filename string) string {
	clean := sanitizeFilename(filename)
	if clean == "" {
		clean = "attachment"
	}
	if formatted := mime.FormatMediaType(kind, map[string]string{"filename": clean}); formatted != "" {
		// RFC 5987 alongside, so a non-ASCII name survives in browsers that
		// read it and degrades to the quoted form in those that do not.
		if hasNonASCII(clean) {
			return formatted + "; filename*=UTF-8''" + url.PathEscape(clean)
		}
		return formatted
	}
	return kind
}

// sanitizeFilename strips what must never reach a header or a filesystem.
//
// Path separators go because the name is a suggestion to the browser's
// downloader about what to call a file, and "../.bashrc" is not a filename.
// Control characters go because CR and LF in a header value is response
// splitting.
//
// Quotes and semicolons go too, and that one is belt-and-braces rather than
// strictly required: mime.FormatMediaType escapes them correctly, and a
// compliant parser reads `filename="a\"; filename=\"b.exe"` as the single name
// it is. But the name is only ever a suggestion for what to call a download,
// so dropping the two characters that could end a parameter early costs
// nothing real and removes the need for every parser downstream to get
// quoted-pair handling right.
func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return -1
		case r == '/' || r == '\\':
			return '_'
		case r == '"' || r == ';':
			return '_'
		}
		return r
	}, name)

	name = strings.TrimSpace(name)
	// A leading dot would make the download hidden on a Unix desktop, and
	// "." and ".." are not names at all.
	name = strings.TrimLeft(name, ".")
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

func hasNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return true
		}
	}
	return false
}

// registerAttachmentRoutes wires the file surface onto an authenticated mux.
func registerAttachmentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/attachments/file", handleAttachmentFile)
	mux.HandleFunc("GET /api/attachments/occurrences", handleAttachmentOccurrences)
	mux.HandleFunc("GET /api/attachments/content", handleAttachmentContent)
}

// handleAttachmentFile returns one file's metadata by key.
func handleAttachmentFile(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	file, err := store.GetAttachmentFile(key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if file == nil {
		writeError(w, http.StatusNotFound, "no such file")
		return
	}
	writeJSON(w, http.StatusOK, file)
}

// handleAttachmentOccurrences lists the messages a file arrived on.
func handleAttachmentOccurrences(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	list, err := store.ListAttachmentOccurrences(key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleAttachmentContent serves a file's bytes.
//
// See the posture note at the top of this file. In short: the type is chosen
// from an allowlist over what the store recorded, the disposition defaults to
// download, and nothing in the request can widen either.
func handleAttachmentContent(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if !blobstore.ValidHash(key) {
		// One answer for "malformed" and "absent". A key that is not an address
		// cannot name anything here, and distinguishing the two would only tell
		// a prober which of its guesses were the right shape.
		writeError(w, http.StatusNotFound, "no such file")
		return
	}

	file, err := store.GetAttachmentFile(key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	// A file this mailbox has no row for is not served even if the bytes
	// happen to be on disk. The database is the authority on what exists;
	// otherwise a blob left behind by a deleted message stays readable forever
	// to anyone who kept its hash.
	if file == nil || !file.Stored() {
		writeError(w, http.StatusNotFound, "no such file")
		return
	}

	f, err := blobstore.New(importDataDir).OpenFile(key)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) || errors.Is(err, blobstore.ErrBadHash) {
			writeError(w, http.StatusNotFound, "no such file")
			return
		}
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}

	contentType, inlineOK := safeContentType(file.MimeType)
	disposition := "attachment"
	if inlineOK && r.URL.Query().Get("disposition") == "inline" {
		// The request may only ask for inline, never grant it: inlineOK is
		// decided from the stored type before this is consulted.
		disposition = "inline"
	}

	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", contentDisposition(disposition, file.Filename))
	h.Set("X-Content-Type-Options", "nosniff")
	// sandbox with no allow-* tokens: no script, no forms, no plugins, and an
	// opaque origin, so a rendered document cannot reach this host's session.
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Referrer-Policy", "no-referrer")
	// The bytes are immutable — the URL is their hash — but they are also
	// private, so the cache directive says both.
	h.Set("Cache-Control", "private, max-age=31536000, immutable")

	// ServeContent rather than io.Copy: it answers Range requests, which is
	// what makes a browser's PDF viewer able to open a large document without
	// downloading all of it first. The name is passed empty on purpose, so it
	// cannot re-derive a content type from the extension and overwrite the one
	// chosen above.
	http.ServeContent(w, r, "", info.ModTime(), f)
}
