package backups

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/user/inboxql/internal/store"
)

func openMailbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := store.InitDB(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)
	return dir
}

func reasons(all []Backup) []string {
	var out []string
	for _, b := range all {
		out = append(out, b.Reason)
	}
	return out
}

// Backups from before the catalogue are recognised by their names.
func TestOlderBackupsAreRecognised(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(Dir(dir), 0o755)
	touch := func(path string, at time.Time) {
		os.WriteFile(path, []byte("x"), 0o600)
		os.Chtimes(path, at, at)
	}
	now := time.Now()
	touch(filepath.Join(Dir(dir), "inboxql-pre-0.0.87-20261014-202942.db"), now.Add(-3*time.Hour))
	touch(filepath.Join(Dir(dir), "iql-20261001T120000Z.db"), now.Add(-2*time.Hour))
	touch(filepath.Join(Dir(dir), "iql-20261001T120000Z.db.vault.key"), now)
	touch(filepath.Join(dir, "inboxql.db.20261002T100000Z.bak"), now.Add(-1*time.Hour))
	touch(filepath.Join(dir, "inboxql.db"), now) // the live one is not a backup

	all, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("listed %d, want 3: %+v", len(all), all)
	}
	if got := reasons(all); got[0] != ReasonRestore || got[1] != ReasonManual || got[2] != ReasonUpdate {
		t.Errorf("reasons = %v", got)
	}
	if all[2].Version != "0.0.87" || !all[1].Key || !all[2].Legacy {
		t.Errorf("details = %+v", all)
	}
}

func TestBackupsAreCheckedAndPrunedByTheKeepRules(t *testing.T) {
	dir := openMailbox(t)
	for i := 0; i < 3; i++ {
		if _, err := Make(Options{DataDir: dir, Reason: ReasonUpdate, Version: "0.0.9" + string(rune('0'+i))}); err != nil {
			t.Fatalf("Make %d: %v", i, err)
		}
	}
	mine, err := Make(Options{DataDir: dir, Reason: ReasonManual})
	if err != nil {
		t.Fatal(err)
	}
	if mine.Backup.Checked != "ok" || mine.Backup.Schema != store.SchemaVersion {
		t.Errorf("a new backup was not checked: %+v", mine.Backup)
	}

	all, _ := List(dir)
	n := map[string]int{}
	for _, b := range all {
		n[b.Reason]++
	}
	if n[ReasonUpdate] != 2 || n[ReasonManual] != 1 {
		t.Errorf("kept %v, want 2 update and 1 manual", n)
	}

	// Yours are never pruned, however many.
	if err := SavePolicy(dir, Policy{KeepUpdate: 1, KeepScheduled: 1, KeepRestore: 1, Schedule: "daily"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := Make(Options{DataDir: dir}); err != nil {
			t.Fatal(err)
		}
	}
	// A new backup prunes as it finishes: one update backup now, all three of yours.
	all, _ = List(dir)
	n = map[string]int{}
	for _, b := range all {
		n[b.Reason]++
	}
	if n[ReasonUpdate] != 1 || n[ReasonManual] != 3 {
		t.Errorf("kept %v, want 1 update and 3 manual", n)
	}
	if gone, _ := Prune(dir, true); len(gone) != 0 {
		t.Errorf("nothing should be left to prune: %+v", gone)
	}
	for _, b := range all {
		for _, side := range []string{".json"} {
			if _, err := os.Stat(b.Path + side); err != nil {
				t.Errorf("%s has no sidecar", b.Name)
			}
		}
	}
	if err := SavePolicy(dir, Policy{KeepUpdate: 0}); err == nil {
		t.Error("keeping none of a kind was accepted")
	}
}

// A full disk is found out before anything is written, and the room the keep
// rules would make anyway is made first.
func TestRoomIsMadeOrTheBackupRefused(t *testing.T) {
	dir := openMailbox(t)
	first, err := Make(Options{DataDir: dir, Reason: ReasonUpdate, Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Make(Options{DataDir: dir, Reason: ReasonUpdate, Version: "2"})
	if err != nil {
		t.Fatal(err)
	}

	need := Need(dir, false)
	defer func(f func(string) (int64, error)) { freeSpace = f }(freeSpace)

	// Free space plus the oldest update backup is enough: it goes first.
	freeSpace = func(string) (int64, error) { return need - first.Backup.Bytes/2, nil }
	made, err := Make(Options{DataDir: dir, Reason: ReasonUpdate, Version: "3"})
	if err != nil {
		t.Fatalf("Make with room to make: %v", err)
	}
	if len(made.MadeRoom) != 1 || made.MadeRoom[0].Name != first.Backup.Name {
		t.Errorf("made room by removing %+v, want the oldest", made.MadeRoom)
	}

	// Not enough even then: refused, and nothing written or removed.
	freeSpace = func(string) (int64, error) { return 1 << 20, nil }
	before, _ := List(dir)
	_, err = Make(Options{DataDir: dir, Reason: ReasonManual})
	var nr *NoRoom
	if !errors.As(err, &nr) {
		t.Fatalf("err = %v, want NoRoom", err)
	}
	after, _ := List(dir)
	if len(after) != len(before) {
		t.Errorf("a refused backup changed the folder: %d → %d", len(before), len(after))
	}
	_ = second
}

func TestRestoreSetsTheCurrentDatabaseAside(t *testing.T) {
	dir := openMailbox(t)
	if err := store.UpdateSetting("marker", "before"); err != nil {
		t.Fatal(err)
	}
	b, err := Make(Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSetting("marker", "after"); err != nil {
		t.Fatal(err)
	}
	store.CloseDB()

	// Staged, the way the page asks for it, and applied before opening.
	if err := Stage(dir, b.Backup.Path); err != nil {
		t.Fatal(err)
	}
	if Staged(dir) != b.Backup.Path {
		t.Fatal("not staged")
	}
	out, err := ApplyStaged(dir, "test")
	if err != nil {
		t.Fatalf("ApplyStaged: %v", err)
	}
	if Staged(dir) != "" {
		t.Error("the staged restore was not cleared")
	}
	if out.SetAside == "" {
		t.Fatal("the current database was not set aside")
	}

	if _, err := store.InitDB(dir); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.GetSetting("marker"); v != "before" {
		t.Errorf("after restore marker = %q, want before", v)
	}
	all, _ := List(dir)
	var aside *Backup
	for i := range all {
		if all[i].Path == out.SetAside {
			aside = &all[i]
		}
	}
	if aside == nil || aside.Reason != ReasonRestore {
		t.Fatalf("set-aside database is not listed as one: %+v", all)
	}
	// What was set aside is whole: the write-ahead log was folded in first.
	store.CloseDB()
	if _, err := RestoreNow(dir, aside.Path, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InitDB(dir); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.GetSetting("marker"); v != "after" {
		t.Errorf("restoring what was set aside gave marker = %q, want after", v)
	}
}

func TestANewerBackupIsRefused(t *testing.T) {
	dir := openMailbox(t)
	b, err := Make(Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	store.CloseDB()
	db, err := os.OpenFile(b.Backup.Path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	// user_version lives at byte 60 of the header.
	db.WriteAt([]byte{0, 0, 3, 0xe7}, 60)
	db.Close()
	if _, err := CanRestore(b.Backup.Path); err == nil {
		t.Error("a backup from a newer schema was accepted")
	}
}

func TestDueFollowsTheNewestBackup(t *testing.T) {
	dir := openMailbox(t)
	if Due(dir, time.Now()) {
		t.Error("due with the schedule off")
	}
	SavePolicy(dir, Policy{KeepUpdate: 2, KeepScheduled: 2, KeepRestore: 2, Schedule: ScheduleDaily})
	if !Due(dir, time.Now()) {
		t.Error("not due with no backups at all")
	}
	if _, err := Make(Options{DataDir: dir}); err != nil {
		t.Fatal(err)
	}
	if Due(dir, time.Now()) {
		t.Error("due just after a backup")
	}
	if !Due(dir, time.Now().Add(25*time.Hour)) {
		t.Error("not due a day later")
	}
}
