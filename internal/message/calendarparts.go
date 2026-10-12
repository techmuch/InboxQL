package message

import (
	"bytes"
	"io"
	"strings"

	"github.com/emersion/go-message/mail"
)

// maxCalendarPart caps one calendar part. An invitation is a few kilobytes; a
// whole exported calendar attached to a message can be large, and is read up
// to this much.
const maxCalendarPart = 4 << 20

// CalendarParts returns the decoded bodies of every text/calendar part in a
// message, inline or attached.
//
// Outlook sends an invitation as an alternative body — text/plain, text/html
// and text/calendar side by side — rather than as an attachment, so walking
// only attachments would miss most invitations.
func CalendarParts(raw []byte) [][]byte {
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	var out [][]byte
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		var ct string
		switch h := part.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ = h.ContentType()
		case *mail.AttachmentHeader:
			ct, _, _ = h.ContentType()
			if ct == "application/octet-stream" || ct == "" {
				if name, _ := h.Filename(); strings.HasSuffix(strings.ToLower(name), ".ics") {
					ct = "text/calendar"
				}
			}
		}
		ct = strings.ToLower(ct)
		if ct != "text/calendar" && ct != "application/ics" {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(part.Body, maxCalendarPart))
		if err == nil && bytes.Contains(b, []byte("BEGIN:VCALENDAR")) {
			out = append(out, b)
		}
	}
	return out
}
