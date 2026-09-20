package store

import (
	"strings"
)

// # The readable files on a message, as text an extractor can be shown
//
// An invoice arrives as a PDF more often than as a body. Until this, the span
// engine read the subject and the body and nothing else, which on a mailbox of
// receipts is most of the data missing — and it is why `content:invoice`
// matches no files here while the filenames say invoice.

// AttachmentDoc is one readable file's text, and what to call it.
type AttachmentDoc struct {
	// Hash identifies the file. It is the content hash rather than an
	// attachment row id because the entity is the bytes: the same document
	// sent to five people is one extraction, and an annotation about it should
	// not be five different annotations.
	Hash string
	// Filename is the name it arrived under, for showing. A file that arrived
	// under two names answers to whichever came first here; the identity is
	// the hash.
	Filename string
	Text     string
}

// Field is the name an annotation uses to say the text came from this file.
//
// "attachment:<hash>" rather than a bare index, so the reference survives the
// file being re-extracted, arriving again on another message, or the message
// being re-annotated in a different order.
func (d AttachmentDoc) Field() string { return "attachment:" + d.Hash }

// AttachmentTextForMessage returns the readable files carried by a message.
//
// # The join has to be reproducible
//
// Offsets index the string an extractor was shown, so this and the extractor
// have to agree exactly on how a file's pages become one text. Pages are
// joined with a single newline in page order, and that is the whole contract —
// if it ever changes, every stored offset against an attachment moves.
func AttachmentTextForMessage(messageID string) ([]AttachmentDoc, error) {
	rows, err := db.Query(`
		SELECT DISTINCT a.content_hash, COALESCE(a.filename, '')
		FROM attachments a
		JOIN attachment_extractions e ON e.content_hash = a.content_hash
		WHERE a.message_id = ? AND e.status = 'ok' AND a.content_hash != ''
		ORDER BY a.content_hash`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AttachmentDoc
	for rows.Next() {
		var d AttachmentDoc
		if err := rows.Scan(&d.Hash, &d.Filename); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		text, err := AttachmentTextJoined(out[i].Hash)
		if err != nil {
			return nil, err
		}
		out[i].Text = text
	}
	return out, nil
}

// AttachmentTextJoined is one file's pages as a single string.
//
// The one place the join is defined. See AttachmentTextForMessage: offsets
// depend on it, so both the extractor and anything that later displays a span
// must call this rather than joining pages themselves.
func AttachmentTextJoined(hash string) (string, error) {
	pages, err := GetAttachmentText(hash)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(pages))
	for _, p := range pages {
		parts = append(parts, p.Text)
	}
	return strings.Join(parts, "\n"), nil
}
