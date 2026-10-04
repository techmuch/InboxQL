package query

import (
	"strings"
	"testing"
)

func compileMail(src string) (string, error) {
	q, err := Parse(src)
	if err != nil {
		return "", err
	}
	plan, err := Build(q, Options{}, "m.id")
	if err != nil {
		return "", err
	}
	return plan.SQL, nil
}

// # The direction that had no spelling
//
// `in:attachments` lists each distinct file once, however many messages carried
// it — that is the point of addressing them by content. The other direction,
// "which mail carried this file", could not be written at all: the file row
// said "2 messages" and there was no way to ask which two.
func TestAttachedSelectsMailNotFiles(t *testing.T) {
	q, err := Parse("attached:80202167")
	if err != nil {
		t.Fatal(err)
	}
	if got := q.Entity(); got != EntityMessage {
		t.Errorf("entity = %q, want messages — this asks about mail, not files", got)
	}

	sql, err := compileMail("attached:80202167")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "FROM attachments att") || !strings.Contains(sql, "att.message_id = m.id") {
		t.Errorf("did not join the attachments to the mail: %s", sql)
	}
}

// # Why it is not called file:
//
// `file:` already belongs to an attachment query, where it means the name a
// file arrived under. Giving one word two meanings made a bare `file:contract`
// silently answer about files when the caller meant mail — which is what the
// first version of this did.
func TestFileStillMeansFilenameOnAnAttachmentQuery(t *testing.T) {
	q, err := Parse("in:attachments file:contract")
	if err != nil {
		t.Fatal(err)
	}
	if got := q.Entity(); got != EntityAttachment {
		t.Errorf("entity = %q, want attachments — file: is its filename field", got)
	}
}

// A full SHA-256 is sixty-four characters and nobody types one, so a prefix
// from a listing has to work.
func TestAHashPrefixIsEnough(t *testing.T) {
	sql, err := compileMail("attached:80202167")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "content_hash LIKE") {
		t.Errorf("a hash prefix did not compile to a prefix match: %s", sql)
	}
}

// Anything that is not hexadecimal is a filename, because "every message that
// carried contract.pdf" is the other question asked here and a second field for
// it would be a second thing to learn.
func TestANonHexValueIsAFilename(t *testing.T) {
	sql, err := compileMail("attached:contract.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "content_hash LIKE") {
		t.Errorf("a filename was read as a hash: %s", sql)
	}
	if !strings.Contains(sql, "att.filename") {
		t.Errorf("a filename did not match on the name: %s", sql)
	}
}

// Short values are filenames even when they look hexadecimal. "doc" is not a
// hash prefix anybody meant, and a file genuinely called "deadbeef" stays
// reachable with a glob.
func TestShortValuesAreNames(t *testing.T) {
	sql, err := compileMail("attached:abc")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "content_hash LIKE") {
		t.Errorf("three characters were read as a hash: %s", sql)
	}
}

// `attached:*.pdf` is the obvious thing to reach for, and half the uses are a
// name rather than a hash.
func TestAttachedTakesAGlob(t *testing.T) {
	if _, err := compileMail("attached:*.pdf"); err != nil {
		t.Errorf("a glob was refused: %v", err)
	}
}

// "No attachment of this message is that file", like every other multi-valued
// field here. The other reading would match almost every message with more
// than one attachment.
func TestNegationIsOverTheWholeMessage(t *testing.T) {
	sql, err := compileMail("-attached:80202167")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "NOT EXISTS") && !strings.Contains(sql, "NOT (EXISTS") {
		t.Errorf("negation is not an anti-join: %s", sql)
	}
}

func TestAttachedNeedsAValue(t *testing.T) {
	if _, err := compileMail("attached:"); err == nil {
		t.Error("an empty value was accepted")
	}
}
