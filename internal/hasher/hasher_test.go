package hasher

import "testing"

// The bug this replaced: every body-less message hashed to SHA-256 of the empty
// string, so a global unique index kept the first calendar invite and silently
// discarded every one after it.
func TestBodylessMessagesDoNotCollide(t *testing.T) {
	invite := MessageHash("<invite-1@example.com>", "alice@example.com", "Standup", "")
	receipt := MessageHash("<receipt-9@shop.example>", "billing@shop.example", "Your receipt", "")

	if invite == receipt {
		t.Fatal("two different body-less messages produced the same hash")
	}
	if invite == NormalizeAndHashSHA256("") {
		t.Error("a body-less message still hashes as the empty string")
	}
}

// Two copies of the same message reaching InboxQL by different routes — fetched
// over IMAP and read out of an .emlx — must agree, or importing duplicates
// everything already synced.
func TestSameMessageFromDifferentSourcesAgrees(t *testing.T) {
	viaIMAP := MessageHash("<abc@example.com>", "Alice@Example.com", "Lunch", "Thursday works.")
	viaImport := MessageHash("<abc@example.com>", "alice@example.com", "lunch  ", "  Thursday works.  ")

	if viaIMAP != viaImport {
		t.Error("the same message hashed differently depending on its source")
	}
}

func TestDistinctFieldsChangeTheHash(t *testing.T) {
	base := MessageHash("<a@x>", "alice@x", "Subject", "Body")

	cases := map[string]string{
		"message id": MessageHash("<b@x>", "alice@x", "Subject", "Body"),
		"sender":     MessageHash("<a@x>", "bob@x", "Subject", "Body"),
		"subject":    MessageHash("<a@x>", "alice@x", "Other", "Body"),
		"body":       MessageHash("<a@x>", "alice@x", "Subject", "Different"),
	}
	for field, h := range cases {
		if h == base {
			t.Errorf("changing the %s did not change the hash", field)
		}
	}
}

// Fields are separated by a byte that cannot occur in any of them, so no
// shifting of content across a boundary can produce a collision.
func TestFieldBoundariesCannotBeForged(t *testing.T) {
	a := MessageHash("ab", "c", "d", "e")
	b := MessageHash("a", "bc", "d", "e")

	if a == b {
		t.Error("field boundaries are not encoded; concatenation collides")
	}
}

func TestEmptyMessageIsStable(t *testing.T) {
	if MessageHash("", "", "", "") != MessageHash("", "", "", "") {
		t.Error("hashing is not deterministic")
	}
}
