package message

import (
	"html"
	"regexp"
	"strings"
)

// Readable text, for a reader rather than for a hash.
//
// # Why not NormalizedBody
//
// NormalizedBody is part of the deduplication hash, so it cannot change
// without every HTML message hashing differently the next time it arrives —
// a duplicate of each. And it is a poor thing to read: it strips tags and
// keeps what was between them, which for a Home Depot receipt is 98,000
// characters starting `@charset "UTF-8"; html, body, div, …` with the items
// somewhere after the stylesheet, and `&lt;A&gt;` where the receipt said <A>.
//
// Readable keeps the plain-text part when there is one, as a mail client
// shows it, and otherwise renders the HTML: no style, script or head, entities
// decoded, and the breaks a table or a paragraph implies kept as line breaks so
// a receipt's rows stay rows.

var (
	invisibleBlock = regexp.MustCompile(`(?is)<(style|script|head|title|noscript|template)\b[^>]*>.*?</\s*(style|script|head|title|noscript|template)\s*>`)
	htmlComment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	lineBreakTag   = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/tr|/li|/h[1-6]|/table|/blockquote|hr)\b[^>]*>`)
	cellTag        = regexp.MustCompile(`(?i)<\s*/t[dh]\s*>`)
	itemTag        = regexp.MustCompile(`(?i)<\s*li\b[^>]*>`)
	anyTag         = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRun       = regexp.MustCompile(`[ \t\x{00a0}]+`)
	blankLines     = regexp.MustCompile(`\n\s*\n(\s*\n)+`)
	zeroWidth      = strings.NewReplacer(
		"\r\n", "\n",
		"\r", "\n",
		"\u200b", "", // zero-width space
		"\u200c", "", // zero-width non-joiner (marketing preheader padding)
		"\u200d", "", // zero-width joiner
		"\u2060", "", // word joiner
		"\ufeff", "", // BOM / zero-width no-break space
		"\u034f", "", // combining grapheme joiner (preheader filler)
		"\u00ad", "", // soft hyphen
	)
)

// Readable is the message's text as a person would read it.
func Readable(m *Message) string {
	if strings.TrimSpace(m.Body) != "" {
		if looksLikeHTML(m.Body) {
			return HTMLText(m.Body)
		}
		return cleanLines(m.Body)
	}
	return HTMLText(m.HTMLBody)
}

func looksLikeHTML(s string) bool {
	head := strings.ToLower(strings.TrimSpace(s))
	if len(head) > 512 {
		head = head[:512]
	}
	if strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html") {
		return true
	}
	return (strings.Contains(head, "<html") || strings.Contains(head, "<head") || strings.Contains(head, "<style")) &&
		strings.Contains(head, ">")
}

// HTMLText renders HTML as plain text.
func HTMLText(s string) string {
	if s == "" {
		return ""
	}
	s = invisibleBlock.ReplaceAllString(s, " ")
	s = htmlComment.ReplaceAllString(s, " ")
	s = itemTag.ReplaceAllString(s, "\n• ")
	s = lineBreakTag.ReplaceAllString(s, "\n")
	s = cellTag.ReplaceAllString(s, "  ")
	s = anyTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return cleanLines(s)
}

func cleanLines(s string) string {
	s = zeroWidth.Replace(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRun.ReplaceAllString(l, " "))
	}
	s = strings.Join(lines, "\n")
	s = blankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
