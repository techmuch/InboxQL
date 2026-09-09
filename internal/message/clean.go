package message

import (
	"regexp"
	"strings"
)

// Markers that begin quoted or forwarded material.
//
// Deliberately conservative. Cutting too early loses the message; cutting too
// late leaves the previous one attached. Every pattern here is one a mail
// client writes deliberately, not a shape that happens to occur in prose.
var (
	// "---------- Forwarded message ---------" and its many dashes.
	forwardedMarker = regexp.MustCompile(`(?im)^-{2,}\s*forwarded message\s*-{2,}\s*$`)
	// "On Mon, 3 Mar 2026 at 10:04, Alice <a@x.com> wrote:", possibly wrapped
	// over two lines, which Gmail does constantly.
	attributionMarker = regexp.MustCompile(`(?im)^\s*on .{0,200}?\bwrote:\s*$`)
	// Outlook's block, in the several languages it ships in the wild.
	originalMarker = regexp.MustCompile(`(?im)^-{2,}\s*original message\s*-{2,}\s*$`)
	// Outlook's header block: "From: ... Sent: ... To: ... Subject: ..."
	outlookHeader = regexp.MustCompile(`(?im)^\s*from:\s.+\n(\s*sent:\s.+\n)?\s*to:\s`)
	// RFC 3676's signature delimiter: exactly "-- " on its own line.
	signatureMarker = regexp.MustCompile(`(?m)^--\s?$`)
)

// CleanBody returns the part of a message its sender actually wrote.
//
// # Why this exists
//
// Two thirds of a real archive is forwards and replies, and the quoted portion
// is usually longer than the new text. Anything that reads a body to decide
// what it is about — an extractor's prompt, an embedding — reads mostly the
// previous message unless it is cut. For a forward that is worse than noise:
// the content belongs to the original sender while the message belongs to the
// forwarder, so the conclusion is attached to the wrong person.
//
// # Why it is not the hashed body
//
// ContentHash is computed over the raw body and must stay that way. Changing
// what is hashed would give every message a new identity, and the same mail
// re-imported afterwards would no longer deduplicate against the copy already
// stored. This is a second, lossy view for readers that want the gist; the
// stored body is untouched.
//
// Returns the original when stripping would leave nothing, because a message
// that is *only* a forward still has to be about something.
func CleanBody(body string) string {
	trimmed := cut(body)
	trimmed = dropQuotedLines(trimmed)
	trimmed = dropSignature(trimmed)
	trimmed = strings.TrimSpace(collapseBlankLines(trimmed))

	// Fall back only when almost nothing survives.
	//
	// "FYI", "See below", "Thanks!" over a long quote are acknowledgements —
	// the substance really is in the quoted part, so returning it is right.
	// The bar has to be low, though: "Sounds good, let's do Friday at noon" is
	// a whole message at 37 characters, and a generous threshold would throw
	// every short reply back to the text it was replying to.
	if len(trimmed) < 15 {
		return strings.TrimSpace(body)
	}
	return trimmed
}

// cut truncates at the earliest marker that begins someone else's message.
func cut(body string) string {
	earliest := len(body)
	for _, re := range []*regexp.Regexp{
		forwardedMarker, attributionMarker, originalMarker, outlookHeader,
	} {
		if loc := re.FindStringIndex(body); loc != nil && loc[0] < earliest {
			earliest = loc[0]
		}
	}
	return body[:earliest]
}

// dropQuotedLines removes lines the sender is quoting rather than writing.
func dropQuotedLines(body string) string {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// dropSignature removes everything after the RFC 3676 delimiter.
//
// Only the last one: a quoted message can contain its own delimiter, and
// cutting at the first would discard text written above it.
func dropSignature(body string) string {
	locs := signatureMarker.FindAllStringIndex(body, -1)
	if len(locs) == 0 {
		return body
	}
	last := locs[len(locs)-1]
	// A delimiter in the first line or two is not a signature, it is the whole
	// message being a signature — which happens with automated mail.
	if last[0] < 40 {
		return body
	}
	return body[:last[0]]
}

var blankRun = regexp.MustCompile(`\n{3,}`)

func collapseBlankLines(s string) string {
	return blankRun.ReplaceAllString(s, "\n\n")
}
