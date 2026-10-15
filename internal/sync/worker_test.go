package sync

import (
	stdsync "sync"
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/imaptest"
	"github.com/user/inboxql/internal/store"
)

// eventually polls until f is true or the deadline passes.
func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Nobody presses Sync: mail that arrives shows up, and a change made here
// reaches the server, in seconds.
func TestWorkerKeepsAnAccountLive(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{Gmail: true})
	srv.Deliver(imaptest.Message("first@x", "first", "a@example.com"))

	ws := NewWorkers(NewSyncManager(2))
	ws.FullEvery = time.Hour
	ws.QuickEvery = time.Hour
	ws.Reconcile = 50 * time.Millisecond
	var mu stdsync.Mutex
	changes := 0
	ws.Changed = func(_ *account.Account, r Report) { mu.Lock(); changes++; mu.Unlock() }
	store.PendingNotify = ws.WakePush
	defer func() { store.PendingNotify = nil }()
	ws.Start()
	defer ws.StopAll()

	count := func() int { return len(labelsByMessageID(t)) }
	eventually(t, "the first sync", func() bool { return count() == 1 })
	eventually(t, "the worker to go live", func() bool {
		st, _ := ws.Status(acc.ID)
		return st.State == "live"
	})

	// Arrives while the worker is idling.
	srv.Deliver(imaptest.Message("second@x", "second", "a@example.com"))
	eventually(t, "new mail to appear", func() bool { return count() == 2 })

	// Archived here: Gmail hears of it without a Sync.
	id := messageIDOf(t, "second@x")
	if _, err := store.ChangeLabels([]string{id}, store.MoveArchive); err != nil {
		t.Fatal(err)
	}
	m := srv.Find("second@x")
	eventually(t, "the archive to reach Gmail", func() bool { return !hasFlag(srv.LabelsOf(m), imaptest.Inbox) })

	// Read on another device while idling: the server says so, and it is
	// here in seconds.
	srv.Change(m, func(m *imaptest.Msg) { m.Flags[`\Seen`] = true })
	eventually(t, "a flag change to arrive", func() bool {
		msg, _ := store.GetMessageByID(id)
		return hasFlag(msg.Flags, `\Seen`)
	})

	// A label the server does not announce comes with the next look.
	ws.Stop(acc.ID)
	ws.QuickEvery = 100 * time.Millisecond
	ws.Restart(acc.ID)
	srv.Change(m, func(m *imaptest.Msg) { m.Labels["Travel"] = true })
	eventually(t, "a server-side label to arrive", func() bool {
		ls := labelsByMessageID(t)["second@x"]
		return len(ls) == 1 && ls[0] == "Travel"
	})

	mu.Lock()
	if changes == 0 {
		t.Error("windows were never told mail changed")
	}
	mu.Unlock()

	// Disconnecting the account stops its worker.
	if err := store.DisconnectAccount(acc.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the worker to stop", func() bool {
		_, ok := ws.Status(acc.ID)
		return !ok
	})
}

// An idle account sends the server next to nothing. Its own EXAMINE once
// counted as news, and the worker asked STATUS and EXAMINE in a loop as fast
// as the server answered — a core of CPU here, and a hammering for Gmail.
func TestWorkerIsQuietWhenNothingHappens(t *testing.T) {
	srv, acc := fakeAccount(t, imaptest.Options{Gmail: true})
	srv.Deliver(imaptest.Message("a@x", "a", "a@example.com"))
	ws := NewWorkers(NewSyncManager(1))
	ws.FullEvery, ws.QuickEvery = time.Hour, time.Hour
	ws.Start()
	defer ws.StopAll()
	eventually(t, "the worker to go live", func() bool {
		st, _ := ws.Status(acc.ID)
		return st.State == "live"
	})
	before := len(srv.Commands())
	time.Sleep(time.Second)
	if n := len(srv.Commands()) - before; n > 2 {
		t.Errorf("an idle worker sent %d commands in a second: %v", n, srv.Commands()[before:])
	}
}
