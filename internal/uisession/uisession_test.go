package uisession

import (
	"testing"
	"time"
)

// Names are typed, so they have to be short and they have to differ.
func TestNamesAreShortAndUnique(t *testing.T) {
	r := New()
	a := r.Register("one")
	b := r.Register("two")

	if a.Name == b.Name {
		t.Fatalf("two windows got the same name %q", a.Name)
	}
	for _, n := range []string{a.Name, b.Name} {
		if len(n) > 4 {
			t.Errorf("name %q is too long to type; that is the whole reason it is not a UUID", n)
		}
	}
}

// # The failure this package exists to avoid
//
// A command that appears to work and does nothing. Somebody points a window at
// a query, sees no error, and is then confused about why the screen did not
// change — and an agent reports success for something that never happened.
func TestSendingToAWindowThatIsGoneFails(t *testing.T) {
	r := New()
	s := r.Register("one")
	r.Connect(s.Name)

	r.Forget(s.Name)

	if err := r.Send(s.Name, Command{Verb: "query", Arg: "x"}); err == nil {
		t.Fatal("sending to a forgotten window reported success")
	}
}

// Registered but with no live stream is a real state — the tab is open, the
// connection dropped — and it must not read as success either.
func TestSendingToADisconnectedWindowFails(t *testing.T) {
	r := New()
	s := r.Register("one")

	_, disconnect, ok := r.Connect(s.Name)
	if !ok {
		t.Fatal("could not connect")
	}
	disconnect()

	err := r.Send(s.Name, Command{Verb: "query", Arg: "x"})
	if err == nil {
		t.Fatal("sending to a disconnected window reported success")
	}
	if got := err.Error(); got == "" {
		t.Error("the error says nothing")
	}
}

func TestSendDeliversToTheChannel(t *testing.T) {
	r := New()
	s := r.Register("one")
	ch, _, _ := r.Connect(s.Name)

	if err := r.Send(s.Name, Command{Verb: "query", Arg: "from:stripe", Note: "the invoices"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if got.Verb != "query" || got.Arg != "from:stripe" || got.Note != "the invoices" {
			t.Errorf("got %+v", got)
		}
		if got.At.IsZero() {
			t.Error("the command carries no time")
		}
	case <-time.After(time.Second):
		t.Fatal("nothing arrived")
	}

	// Recorded, because a window showing something nobody in front of it asked
	// for is otherwise a mystery.
	if cur := r.Get(s.Name); cur == nil || cur.LastCommand == nil {
		t.Error("the command was not recorded against the window")
	}
}

// A listing full of windows that are not there is worse than an empty one:
// every command to a ghost appears to succeed.
func TestAWindowThatStopsBeatingIsForgotten(t *testing.T) {
	r := New()
	s := r.Register("one")

	r.mu.Lock()
	r.byID[s.Name].LastSeen = time.Now().Add(-Expiry - time.Second)
	r.mu.Unlock()

	if got := r.List(); len(got) != 0 {
		t.Errorf("a window last seen %v ago is still listed", Expiry+time.Second)
	}
}

// One missed beat is not a death. A laptop that slept for a moment has not
// closed its window, and dropping it would make the listing flicker.
func TestAWindowThatMissedOneBeatSurvives(t *testing.T) {
	r := New()
	s := r.Register("one")

	r.mu.Lock()
	r.byID[s.Name].LastSeen = time.Now().Add(-Heartbeat - time.Second)
	r.mu.Unlock()

	if got := r.List(); len(got) != 1 {
		t.Errorf("a window that missed one beat was forgotten")
	}
}

// What a window is showing is what it last said, which is what makes the
// listing answer "what am I looking at" rather than "what was open once".
func TestTouchRecordsWhatIsOnScreen(t *testing.T) {
	r := New()
	s := r.Register("one")

	if !r.Touch(s.Name, Showing{Query: "folder:inbox | timeline", Tabs: []string{"desk", "log"}, Active: "desk"}) {
		t.Fatal("touching a registered window failed")
	}
	got := r.Get(s.Name)
	if got.Showing.Query != "folder:inbox | timeline" {
		t.Errorf("query = %q", got.Showing.Query)
	}
	if len(got.Showing.Tabs) != 2 || got.Showing.Active != "desk" {
		t.Errorf("tabs = %v active = %q", got.Showing.Tabs, got.Showing.Active)
	}

	if r.Touch("nosuch", Showing{}) {
		t.Error("touching a window that is not registered reported success")
	}
}

// The listing is serialised while heartbeats are still arriving, so it must
// not hand out the live struct.
func TestListReturnsCopies(t *testing.T) {
	r := New()
	s := r.Register("one")
	r.Touch(s.Name, Showing{Query: "a"})

	got := r.List()[0]
	got.Showing.Query = "mutated"

	if r.Get(s.Name).Showing.Query != "a" {
		t.Error("mutating a listed window changed the registry")
	}
}

// Reconnecting is ordinary — a stream drops and the browser retries — and must
// not produce a second window.
func TestReconnectingDoesNotDuplicate(t *testing.T) {
	r := New()
	s := r.Register("one")

	_, disconnect, _ := r.Connect(s.Name)
	disconnect()
	if _, _, ok := r.Connect(s.Name); !ok {
		t.Fatal("could not reconnect")
	}

	if got := r.List(); len(got) != 1 {
		t.Errorf("reconnecting produced %d windows", len(got))
	}
	if !r.Get(s.Name).Connected {
		t.Error("the window is not marked connected after reconnecting")
	}
}

// Oldest first, because the names are assigned in that order and a list whose
// numbers jump around is harder to read than one that does not.
func TestListIsOldestFirst(t *testing.T) {
	r := New()
	first := r.Register("a")
	time.Sleep(2 * time.Millisecond)
	second := r.Register("b")

	got := r.List()
	if len(got) != 2 || got[0].Name != first.Name || got[1].Name != second.Name {
		t.Errorf("order is %v", []string{got[0].Name, got[1].Name})
	}
}
