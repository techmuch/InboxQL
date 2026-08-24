package message

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"github.com/emersion/go-message/mail"
	"github.com/user/uea/internal/hasher"
)

// htmlTag matches an HTML element so a markup body can be reduced to something
// worth hashing and searching. Deliberately crude: this is not sanitisation and
// the result is never rendered, only indexed.
var htmlTag = regexp.MustCompile("<[^>]*>")

// ParseRFC822 parses a raw message into UEA's representation.
//
// This is the single MIME entry point, shared by the IMAP sync engine and every
// import source. Two parsers would drift, and whichever one saw less traffic
// would quietly rot — a message that imports differently from how it syncs is a
// bug nobody notices until the hashes disagree and the same mail is stored
// twice.
//
// Fields that only a transport knows — the IMAP UID, flags, internal date — are
// left zero for the caller to overlay.
func ParseRFC822(raw []byte) (*Message, error) {
	msg := &Message{Header: raw}

	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		// Not valid MIME. Rather than discarding the message, keep the raw
		// bytes: a malformed message is still evidence, and the caller can
		// decide whether to store or skip it.
		return msg, err
	}

	header := reader.Header
	msg.Subject, _ = header.Subject()
	if id, err := header.Text("Message-Id"); err == nil {
		msg.MessageID = strings.TrimSpace(id)
	}
	if date, err := header.Date(); err == nil {
		msg.Date = date
	}
	if from, err := header.AddressList("From"); err == nil && len(from) > 0 {
		msg.From = from[0].Address
	}
	msg.To = addresses(header, "To")
	msg.Cc = addresses(header, "Cc")
	msg.Bcc = addresses(header, "Bcc")

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// A truncated or malformed part ends the walk, but whatever was
			// read before it is kept.
			break
		}

		inline, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			// An attachment. Bodies are all this parser collects; attachment
			// extraction is a separate concern with its own storage.
			continue
		}

		contentType, _, _ := inline.ContentType()
		switch {
		case strings.HasPrefix(contentType, "text/plain") && msg.Body == "":
			if b, err := io.ReadAll(part.Body); err == nil {
				msg.Body = string(b)
			}
		case strings.HasPrefix(contentType, "text/html") && msg.HTMLBody == "":
			if b, err := io.ReadAll(part.Body); err == nil {
				msg.HTMLBody = string(b)
			}
		}
	}

	msg.NormalizedBody = HashableBody(msg)
	msg.ContentHash = hasher.MessageHash(msg.MessageID, msg.From, msg.Subject, msg.NormalizedBody)
	return msg, nil
}

// HashableBody returns the text a message should be deduplicated on: the plain
// body when there is one, otherwise the HTML with its tags stripped.
func HashableBody(m *Message) string {
	if strings.TrimSpace(m.Body) != "" {
		return m.Body
	}
	if m.HTMLBody != "" {
		return strings.TrimSpace(htmlTag.ReplaceAllString(m.HTMLBody, " "))
	}
	return ""
}

// Rehash recomputes ContentHash from the message's current fields. Call after
// overlaying transport data that changes any hashed field.
func (m *Message) Rehash() {
	m.NormalizedBody = HashableBody(m)
	m.ContentHash = hasher.MessageHash(m.MessageID, m.From, m.Subject, m.NormalizedBody)
}

func addresses(h mail.Header, field string) []string {
	list, err := h.AddressList(field)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		if a.Address != "" {
			out = append(out, a.Address)
		}
	}
	return out
}
