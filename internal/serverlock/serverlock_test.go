package serverlock

import (
	"errors"
	"testing"
)

func TestOneServerPerDirectory(t *testing.T) {
	dir := t.TempDir()
	first, _, err := Acquire(dir, Info{PID: 1, URL: "http://127.0.0.1:8420", Schema: 40})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	_, other, err := Acquire(dir, Info{PID: 2})
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("second acquire: %v, want ErrHeld", err)
	}
	if other == nil || other.URL != "http://127.0.0.1:8420" || other.Schema != 40 {
		t.Errorf("the holder reads as %+v", other)
	}

	if info, held := Held(dir); !held || info.PID != 1 {
		t.Errorf("Held = %+v, %v; want the first server", info, held)
	}

	first.Release()
	if _, held := Held(dir); held {
		t.Error("still held after release")
	}
	if _, _, err := Acquire(dir, Info{PID: 3}); err != nil {
		t.Errorf("acquire after release: %v", err)
	}
}

// Asking whether a server holds the directory must not itself take the lock
// in a way that leaves it held.
func TestAskingDoesNotHold(t *testing.T) {
	dir := t.TempDir()
	if _, held := Held(dir); held {
		t.Fatal("an empty directory reads as held")
	}
	l, _, err := Acquire(dir, Info{PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
	for i := 0; i < 3; i++ {
		if _, held := Held(dir); held {
			t.Fatal("Held left the lock taken")
		}
	}
}
