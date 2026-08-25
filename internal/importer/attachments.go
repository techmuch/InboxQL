package importer

import (
	"bytes"
	"io"
	"strings"

	"github.com/emersion/go-message/mail"
)

// CountAttachments reports how many attachment parts a message carries and
// their combined decoded size.
//
// Counting only: InboxQL has no attachment storage yet, so this exists to make a
// deep scan able to say "1,204 attachments, 2.1 GB" before anyone decides
// whether importing them is worth it. When attachment storage lands, extraction
// walks the same structure.
func CountAttachments(raw []byte) (count int, bytesTotal int64) {
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return 0, 0
	}

	for {
		part, err := reader.NextPart()
		if err != nil {
			// io.EOF, or a malformed part that ends the walk. Whatever was
			// counted before it still stands.
			break
		}

		header, ok := part.Header.(*mail.AttachmentHeader)
		if !ok {
			// Inline images carry a filename too and are attachments in every
			// sense a person means; count them when they are not the message
			// body itself.
			inline, isInline := part.Header.(*mail.InlineHeader)
			if !isInline {
				continue
			}
			contentType, _, _ := inline.ContentType()
			if strings.HasPrefix(contentType, "text/") {
				continue
			}
			n, _ := io.Copy(io.Discard, part.Body)
			count++
			bytesTotal += n
			continue
		}

		n, _ := io.Copy(io.Discard, part.Body)
		count++
		bytesTotal += n
		_ = header
	}
	return count, bytesTotal
}
