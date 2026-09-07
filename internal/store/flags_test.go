package store

import "testing"

func flagsOf(t *testing.T, id string) []string {
	t.Helper()
	res, err := RunQuery("id:"+id, 1, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("id:%s matched %d messages", id, len(res.Messages))
	}
	return res.Messages[0].Flags
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

func TestSetMessageFlag(t *testing.T) {
	openQueryFixture(t)

	// m1 is unread in the fixture; m2 carries \Seen.
	if hasFlag(flagsOf(t, "m1"), FlagSeen) {
		t.Fatal("fixture assumption: m1 should start unread")
	}

	changed, err := SetMessageFlag([]string{"m1", "m2"}, FlagSeen, true)
	if err != nil {
		t.Fatalf("SetMessageFlag: %v", err)
	}
	// Only m1 changed; m2 already had it. The count is what lets a caller say
	// "1 marked read" instead of overstating what happened.
	if changed != 1 {
		t.Errorf("changed = %d, want 1", changed)
	}
	if !hasFlag(flagsOf(t, "m1"), FlagSeen) {
		t.Error("m1 was not marked read")
	}

	// And the query language agrees, which is the point: is:unread compares
	// exactly, so a differently-spelled flag would read as still unread.
	res, err := RunQuery("is:unread id:m1", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(res.Messages) != 0 {
		t.Error("m1 still matches is:unread after being marked read")
	}

	// Removing is symmetric, and leaves other flags alone.
	if _, err := SetMessageFlag([]string{"m1"}, FlagFlagged, true); err != nil {
		t.Fatalf("SetMessageFlag: %v", err)
	}
	if _, err := SetMessageFlag([]string{"m1"}, FlagSeen, false); err != nil {
		t.Fatalf("SetMessageFlag: %v", err)
	}
	flags := flagsOf(t, "m1")
	if hasFlag(flags, FlagSeen) {
		t.Error("unmarking read did not remove the flag")
	}
	if !hasFlag(flags, FlagFlagged) {
		t.Errorf("unmarking read dropped an unrelated flag: %v", flags)
	}
}

// A no-op reports zero rather than claiming work it did not do.
func TestSettingAFlagTwiceChangesNothing(t *testing.T) {
	openQueryFixture(t)

	if _, err := SetMessageFlag([]string{"m1"}, FlagSeen, true); err != nil {
		t.Fatalf("SetMessageFlag: %v", err)
	}
	changed, err := SetMessageFlag([]string{"m1"}, FlagSeen, true)
	if err != nil {
		t.Fatalf("SetMessageFlag: %v", err)
	}
	if changed != 0 {
		t.Errorf("a repeat set reported %d changed, want 0", changed)
	}
}

// \Deleted and \Draft describe what a message is to the server and the
// importer. Letting a checkbox write them would make the mailbox disagree with
// itself.
func TestOnlyOpinionFlagsAreSettable(t *testing.T) {
	openQueryFixture(t)

	for _, flag := range []string{`\Deleted`, `\Draft`, "arbitrary", ""} {
		if _, err := SetMessageFlag([]string{"m1"}, flag, true); err == nil {
			t.Errorf("setting %q was allowed", flag)
		}
	}
}
