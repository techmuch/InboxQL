package message

import (
	"html"
	"regexp"
	"strings"
)

// Preview is the first words of a message as a list shows them: what a
// person would read, without what only a mail client or a tracker would.
//
// Readable already turns HTML into text. A list line needs more taken out,
// because it is one line and every character of it is read:
//
//   - entities a sender wrote into the plain-text part, which arrive as
//     literal text — `&zwnj; &zwnj; &zwnj;`, the padding marketing mail puts
//     ahead of the real first line;
//   - invisible characters, the same padding once decoded;
//   - links, which on bulk mail are long tracking addresses
//     (`https://click.email…/?qs=ABp7…`) and say nothing in a preview;
//   - stylesheet selectors that leaked out of a template
//     (`.o_layout .o_mail_wrapper a.btn.btn-secondary {`).
//
// n is how many characters to keep, at most.
func Preview(m *Message, n int) string {
	s := Readable(m)
	// Twice: plain-text parts sometimes carry `&amp;zwnj;`.
	s = html.UnescapeString(html.UnescapeString(s))
	s = invisible.ReplaceAllString(s, "")
	s = cssBlock.ReplaceAllString(s, " ")
	s = bracketedURL.ReplaceAllString(s, " ")
	s = bareURL.ReplaceAllString(s, " ")
	s = selectorRun.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n]))
	}
	return s
}

var (
	// Zero-width spaces and joiners, the combining grapheme joiner, soft
	// hyphens and the byte-order mark: all padding, none of it text.
	invisible    = regexp.MustCompile(`[\x{200b}-\x{200f}\x{2060}-\x{2064}\x{034f}\x{00ad}\x{feff}]`)
	bracketedURL = regexp.MustCompile(`<(?:https?|mailto):[^>\s]*>`)
	bareURL      = regexp.MustCompile(`(?:https?://|www\.)\S+`)
	cssBlock     = regexp.MustCompile(`\{[^{}]*\}`)
	// Two or more selectors in a row — `.o_layout .o_mail_wrapper`,
	// `a.btn.btn-secondary` — with any `{` that followed them. One dotted word
	// alone is left: it may be a file name or a domain in a sentence.
	selectorRun = regexp.MustCompile(`(?:[a-z0-9]*[.#][A-Za-z_][\w-]*(?:\.[\w-]+)*\s+){1,}[a-z0-9]*[.#][A-Za-z_][\w-]*(?:\.[\w-]+)*\s*\{?`)
)
