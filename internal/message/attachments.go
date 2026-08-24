package message

import (
	"bytes"
	"io"
	"strings"

	"github.com/emersion/go-message/mail"
)

// Attachment is one non-body part of a message, with its decoded bytes.
type Attachment struct {
	Filename    string
	ContentType string
	// Data is the decoded content. Nil when the part exceeded the caller's
	// size limit — Size still reports how big it was.
	Data []byte
	Size int64
	// Inline marks a part displayed within the message, typically an image
	// referenced by ContentID rather than listed as an attachment.
	Inline    bool
	ContentID string
	// Skipped explains why Data is nil, empty when the part was captured.
	Skipped string
}

// ExtractAttachments returns a message's attachment parts.
//
// Separate from [ParseRFC822] on purpose: parsing happens for every message the
// sync engine sees, and decoding attachment bytes on that path would allocate
// gigabytes to produce something nothing was going to look at. Extraction is
// only worth doing when a caller has decided to store the result.
//
// maxBytes caps an individual part. A part over the cap is still reported —
// with its real size and a Skipped reason — so "this message had a 400 MB video
// we did not keep" is recorded rather than invisible.
func ExtractAttachments(raw []byte, maxBytes int64) ([]Attachment, error) {
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	var out []Attachment
	for {
		part, err := reader.NextPart()
		if err != nil {
			// io.EOF, or a malformed part. Whatever was collected still stands.
			break
		}

		var att Attachment
		switch header := part.Header.(type) {
		case *mail.AttachmentHeader:
			att.Filename, _ = header.Filename()
			att.ContentType, _, _ = header.ContentType()

		case *mail.InlineHeader:
			contentType, _, _ := header.ContentType()
			// text/* inline parts are the message body, which ParseRFC822
			// already handled.
			if strings.HasPrefix(contentType, "text/") {
				continue
			}
			att.Inline = true
			att.ContentType = contentType
			att.ContentID = strings.Trim(header.Get("Content-Id"), "<>")
			// InlineHeader has no Filename accessor; the name, when there is
			// one, lives in the Content-Disposition parameters.
			if _, params, err := header.ContentDisposition(); err == nil {
				att.Filename = params["filename"]
			}

		default:
			continue
		}

		// Read one byte past the cap: enough to know the part is oversized
		// without pulling a 400 MB video into memory to find out.
		limit := maxBytes
		if limit <= 0 {
			limit = defaultMaxAttachmentBytes
		}
		data, err := io.ReadAll(io.LimitReader(part.Body, limit+1))
		if err != nil {
			continue
		}

		if int64(len(data)) > limit {
			// Drain the rest to get a true size, discarding as we go.
			rest, _ := io.Copy(io.Discard, part.Body)
			att.Size = int64(len(data)) + rest
			att.Skipped = "larger than the configured limit"
		} else {
			att.Data = data
			att.Size = int64(len(data))
		}

		if att.Filename == "" {
			att.Filename = defaultFilename(att.ContentType)
		}
		out = append(out, att)
	}
	return out, nil
}

// defaultMaxAttachmentBytes bounds a single part when no limit was given.
const defaultMaxAttachmentBytes = 25 << 20 // 25 MB

// defaultFilename names a part that arrived without one, so the UI has
// something to show and an export has something to write.
func defaultFilename(contentType string) string {
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return "image" + extensionFor(contentType)
	case contentType == "application/pdf":
		return "document.pdf"
	default:
		return "attachment" + extensionFor(contentType)
	}
}

func extensionFor(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	case "text/calendar":
		return ".ics"
	case "application/zip":
		return ".zip"
	default:
		return ".bin"
	}
}
