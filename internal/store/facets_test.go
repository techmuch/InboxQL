package store

import "testing"

// The rail's counts are what clicking each row would leave you with: a label
// within the frame, a folder with the additions inside it.
func TestFacetsPreviewTheClick(t *testing.T) {
	openQueryFixture(t)
	if err := CreateLabel("banking"); err != nil {
		t.Fatal(err)
	}
	// banking on m1, m3 and m4; m4 then archived, m3 starred.
	if _, err := ChangeLabels([]string{"m1", "m3", "m4"}, LabelChange{Add: []string{"banking"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ChangeLabels([]string{"m4"}, MoveArchive); err != nil {
		t.Fatal(err)
	}
	if _, err := SetMessageFlag([]string{"m3"}, `\Flagged`, true); err != nil {
		t.Fatal(err)
	}

	label := func(f *Facets, name string) FacetCount {
		for _, c := range f.Labels {
			if c.Name == name {
				return c
			}
		}
		t.Fatalf("no label %q in %+v", name, f.Labels)
		return FacetCount{}
	}

	// The inbox: four messages, two of them banking, both unread.
	f, err := MessageFacets("folder:inbox", "")
	if err != nil {
		t.Fatal(err)
	}
	if f.Total != 4 {
		t.Errorf("inbox total = %d, want 4", f.Total)
	}
	if c := label(f, "banking"); c.Total != 2 || c.Unread != 2 {
		t.Errorf("banking in inbox = %+v, want 2 (2 unread)", c)
	}
	if f.Folders != nil {
		t.Errorf("nothing added, so no scoped folders; got %+v", f.Folders)
	}

	// Starred: one message, and it carries banking.
	f, err = MessageFacets("folder:starred", "")
	if err != nil {
		t.Fatal(err)
	}
	if c := label(f, "banking"); c.Total != 1 {
		t.Errorf("banking in starred = %d, want 1", c.Total)
	}
	// A label nothing in the frame carries is still listed, at zero.
	if c := label(f, "inbox"); c.Total != 1 {
		t.Errorf("inbox in starred = %d, want 1", c.Total)
	}

	// Inbox › banking: each folder counts banking inside it.
	f, err = MessageFacets("folder:inbox label:banking", "label:banking")
	if err != nil {
		t.Fatal(err)
	}
	folders := map[string]int{}
	for _, c := range f.Folders {
		folders[c.Name] = c.Total
	}
	if folders["inbox"] != 2 || folders["starred"] != 1 || folders["allmail"] != 3 {
		t.Errorf("folders with banking = %v, want inbox 2, starred 1, allmail 3", folders)
	}
	if _, ok := folders["drafts"]; ok {
		t.Error("drafts keep their own count")
	}
	// A label already in the query shows the list's total.
	if c := label(f, "banking"); c.Total != f.Total {
		t.Errorf("banking in a banking query = %d, want the total %d", c.Total, f.Total)
	}

	if _, err := MessageFacets("in:tickets", ""); err == nil {
		t.Error("a query about cards has no mail facets")
	}
}
