package query

import "testing"

// name: is a contact's field, but in a file query it is the file's name —
// the alias AGENTS.md documents, which used to be refused.
func TestNameMeansFilenameInAttachmentQueries(t *testing.T) {
	f, ok := LookupFieldIn(EntityAttachment, "name")
	if !ok || f.Name != "filename" {
		t.Fatalf("in:attachments name: resolved to %v, %v", f, ok)
	}
	if f, ok := LookupFieldIn(EntityContact, "name"); !ok || f.Entity != EntityContact {
		t.Errorf("in:contacts name: resolved to %v, %v", f, ok)
	}
}
