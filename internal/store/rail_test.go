package store

import (
	"testing"
)

func openRailFixture(t *testing.T) {
	t.Helper()
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseDB)
}

// The migration seeds the six the rail used to hardcode, so an upgrade changes
// nothing visible. Removing them without seeding would regress the exact gap
// that block existed to close: contacts and files were reachable only by
// knowing to type `in:contacts`.
func TestTheRailSeedsWhatItUsedToHardcode(t *testing.T) {
	openRailFixture(t)

	got, err := ListSavedQueries()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*SavedQuery{}
	for _, q := range got {
		byName[q.Name] = q
	}
	for _, want := range []string{"tickets", "proposed", "files", "people", "systems", "unclassified"} {
		if byName[want] == nil {
			t.Errorf("%s is not in the rail; the migration did not seed it", want)
		}
	}
	if q := byName["files"]; q != nil && q.Query != "in:attachments" {
		t.Errorf("files queries %q", q.Query)
	}
	if q := byName["people"]; q != nil && q.Icon != "users" {
		t.Errorf("people has icon %q, want users", q.Icon)
	}
}

// Position supersedes pinned as the sort. Ordering by pinned meant the only way
// to move something was to rename it.
func TestOrderIsByPosition(t *testing.T) {
	openRailFixture(t)

	// Named so that alphabetical order and intended order disagree, which is
	// what makes the assertion mean anything.
	for _, n := range []string{"zulu", "alpha", "mike"} {
		if err := SaveQuery(&SavedQuery{Title: n, Query: "is:unread"}); err != nil {
			t.Fatal(err)
		}
	}

	if err := MoveSavedQuery("zulu", 0); err != nil {
		t.Fatal(err)
	}
	got, err := ListSavedQueries()
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "zulu" {
		names := []string{}
		for _, q := range got {
			names = append(names, q.Name)
		}
		t.Errorf("order is %v, want zulu first", names)
	}
}

// Editing a query's text must not move it. An edit that silently reordered the
// rail is the kind of surprise this feature exists to remove.
func TestEditingDoesNotMove(t *testing.T) {
	openRailFixture(t)

	for _, n := range []string{"one", "two", "three"} {
		if err := SaveQuery(&SavedQuery{Title: n, Query: "is:unread"}); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := ListSavedQueries()
	var at int
	for i, q := range before {
		if q.Name == "two" {
			at = i
		}
	}

	if err := SaveQuery(&SavedQuery{Title: "two", Query: "is:read"}); err != nil {
		t.Fatal(err)
	}

	after, _ := ListSavedQueries()
	if after[at].Name != "two" {
		t.Errorf("editing moved it from %d; order is now %v", at, names(after))
	}
	if after[at].Query != "is:read" {
		t.Errorf("the edit did not take: %q", after[at].Query)
	}
}

// Clamped rather than refused: "move it to the top" from the top is not an
// error, it is a thing that is already true.
func TestMovingOutOfRangeIsClamped(t *testing.T) {
	openRailFixture(t)
	for _, n := range []string{"a", "b"} {
		if err := SaveQuery(&SavedQuery{Title: n, Query: "is:unread"}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := ListSavedQueries()
	first := all[0].Name

	if err := MoveSavedQuery(first, -5); err != nil {
		t.Errorf("moving above the top failed: %v", err)
	}
	if err := MoveSavedQuery(first, 999); err != nil {
		t.Errorf("moving below the bottom failed: %v", err)
	}
	after, _ := ListSavedQueries()
	if after[len(after)-1].Name != first {
		t.Errorf("a move past the end did not land at the end: %v", names(after))
	}
}

func TestMovingSomethingThatIsNotThere(t *testing.T) {
	openRailFixture(t)
	if err := MoveSavedQuery("nosuch", 0); err == nil {
		t.Error("moving a query that does not exist succeeded")
	}
}

// A name with no component renders as nothing — a rail row with no icon and no
// error — so the vocabulary is closed and checked where it is set.
func TestIconsAreAFixedVocabulary(t *testing.T) {
	openRailFixture(t)
	if err := SaveQuery(&SavedQuery{Title: "x", Query: "is:unread", Icon: "skull"}); err == nil {
		t.Error("an icon with no component was accepted")
	}
	if err := SaveQuery(&SavedQuery{Title: "y", Query: "is:unread", Icon: "clock"}); err != nil {
		t.Errorf("a real icon was refused: %v", err)
	}
	// No icon is legal; the rail falls back to a bookmark.
	if err := SaveQuery(&SavedQuery{Title: "z", Query: "is:unread"}); err != nil {
		t.Errorf("no icon was refused: %v", err)
	}
	if err := SetSavedQueryIcon("nosuch", "clock"); err == nil {
		t.Error("setting an icon on a query that does not exist succeeded")
	}
}

// # Folders are hidden, never deleted
//
// They carry live unread counts and are the mailbox itself. Hiding Spam is
// reasonable; losing Inbox with no obvious way back is a bad afternoon.
func TestFoldersHideAndComeBack(t *testing.T) {
	openRailFixture(t)

	if err := SetFolderHidden("spam", true); err != nil {
		t.Fatal(err)
	}
	if got := HiddenFolders(); len(got) != 1 || got[0] != "spam" {
		t.Errorf("hidden = %v, want [spam]", got)
	}

	// Hiding twice must not list it twice, or showing it once would leave it
	// hidden.
	if err := SetFolderHidden("spam", true); err != nil {
		t.Fatal(err)
	}
	if got := HiddenFolders(); len(got) != 1 {
		t.Errorf("hiding twice gave %v", got)
	}

	if err := SetFolderHidden("spam", false); err != nil {
		t.Fatal(err)
	}
	if got := HiddenFolders(); len(got) != 0 {
		t.Errorf("showing it again left %v", got)
	}

	// The way back, which must not require remembering what was hidden.
	SetFolderHidden("spam", true)
	SetFolderHidden("trash", true)
	if err := ShowAllFolders(); err != nil {
		t.Fatal(err)
	}
	if got := HiddenFolders(); len(got) != 0 {
		t.Errorf("reset left %v hidden", got)
	}
}

func TestOnlyRealFoldersCanBeHidden(t *testing.T) {
	openRailFixture(t)
	if err := SetFolderHidden("archive", true); err == nil {
		t.Error("a folder the rail does not have was accepted")
	}
}

// Installing skips what is already there: a name that exists may have been
// edited, and the edit is the user's.
func TestInstallingDefaultsDoesNotOverwrite(t *testing.T) {
	openRailFixture(t)

	if err := SaveQuery(&SavedQuery{
		Name: "files", Title: "My files", Query: "in:attachments larger:5mb",
	}); err != nil {
		t.Fatal(err)
	}

	added, skipped, err := InstallRailDefaults([]string{"files", "unread"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "unread" {
		t.Errorf("added %v, want just unread", added)
	}
	if len(skipped) != 1 || skipped[0] != "files" {
		t.Errorf("skipped %v, want files", skipped)
	}

	got, _ := GetSavedQuery("files")
	if got.Title != "My files" || got.Query != "in:attachments larger:5mb" {
		t.Errorf("the edit was overwritten: %+v", got)
	}
}

// Every default has to compile, or installing one produces a rail row that
// fails from wherever it is clicked.
func TestEveryDefaultCompiles(t *testing.T) {
	openRailFixture(t)
	for _, d := range RailDefaults {
		if err := ValidateQuery(d.Query); err != nil {
			t.Errorf("%s: %q does not compile: %v", d.Name, d.Query, err)
		}
		if !ValidIcon(d.Icon) {
			t.Errorf("%s names icon %q, which the rail cannot draw", d.Name, d.Icon)
		}
	}
}

func names(qs []*SavedQuery) []string {
	out := make([]string, len(qs))
	for i, q := range qs {
		out[i] = q.Name
	}
	return out
}
