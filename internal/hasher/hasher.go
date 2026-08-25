// Package hasher computes the content hashes InboxQL uses to deduplicate messages.
package hasher

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// fieldSeparator delimits fields inside a hash input.
//
// A NUL byte cannot occur in any of the header values being joined, so no
// combination of field contents can forge a different field boundary — "ab" +
// "c" and "a" + "bc" hash differently, which plain concatenation would not
// guarantee.
const fieldSeparator = "\x00"

// NormalizeAndHashSHA256 normalizes a string (trims whitespace, converts to
// lowercase) and returns its SHA-256 hash.
//
// Deprecated: use [MessageHash] for message deduplication. Hashing a body alone
// gives every body-less message — calendar invites, attachment-only mail, HTML
// whose tag-strip leaves whitespace — the identical hash of the empty string,
// and the unique index then silently discards all but the first.
func NormalizeAndHashSHA256(input string) string {
	normalized := strings.ToLower(strings.TrimSpace(input))

	hasher := sha256.New()
	hasher.Write([]byte(normalized))
	return hex.EncodeToString(hasher.Sum(nil))
}

// MessageHash computes the deduplication hash for a message.
//
// Identity comes from the Message-ID where the sender supplied one, which is
// stable across every route the same message can reach InboxQL by — fetched over
// IMAP, read out of an Apple Mail .emlx, imported from a .eml file. The sender,
// subject and body are folded in so that messages without a Message-ID, and
// messages with no body at all, still separate from one another.
//
// The Date header is deliberately excluded: it is represented differently
// depending on the source, and a mismatch would defeat the dedup this exists
// for.
func MessageHash(messageID, from, subject, body string) string {
	h := sha256.New()
	for _, field := range []string{messageID, from, subject, body} {
		h.Write([]byte(normalize(field)))
		h.Write([]byte(fieldSeparator))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
