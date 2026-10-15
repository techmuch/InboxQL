package sync

import (
	"errors"
	"fmt"
	"log"
	stdsync "sync"
	"time"

	"github.com/emersion/go-imap/client"
	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
)

// # Live accounts
//
// Workers holds one connection per account, for as long as the server runs,
// so nobody has to press Sync.
//
// A worker connects, pushes what is queued, reads every folder, and then
// reads history in batches until it is all here — checking for new mail
// between batches, so a message that arrives during a day-long first sync
// shows up in seconds rather than at the end. Once history is done it waits
// with IMAP IDLE on All Mail (Gmail) or INBOX, which the server answers the
// moment something arrives. It wakes for that, for a change made here that
// needs pushing, for Sync now, and every few minutes regardless, to look at
// the other folders and for what was deleted.
//
// A dropped connection, a laptop waking from sleep, a server that refuses:
// the worker reconnects with a growing pause, and says why on the account.

// Workers runs the live accounts.
type Workers struct {
	sm *SyncManager
	mu stdsync.Mutex
	w  map[string]*worker
	// Changed is told after a pass that changed mail, so windows refresh.
	Changed func(acc *account.Account, r Report)

	// How often to look at every folder and for deletions, and how long to
	// IDLE before starting it again (servers drop an IDLE after ~30 min).
	FullEvery   time.Duration
	IdleRestart time.Duration
	// QuickEvery is how often to ask what changed while idling. IDLE reports
	// new mail reliably and changes made elsewhere — a label on a phone —
	// not always, and asking (CHANGEDSINCE) is cheap.
	QuickEvery time.Duration
	// Reconcile is how often accounts are checked for having been added,
	// edited or removed by a path that did not say so.
	Reconcile time.Duration

	stop chan struct{}
}

// NewWorkers makes the set. Start begins it.
func NewWorkers(sm *SyncManager) *Workers {
	return &Workers{sm: sm, w: map[string]*worker{},
		FullEvery: 10 * time.Minute, IdleRestart: 9 * time.Minute, QuickEvery: 2 * time.Minute, Reconcile: time.Minute,
		stop: make(chan struct{})}
}

// WorkerStatus is what an account's card shows about its connection.
type WorkerStatus struct {
	State     string    `json:"state"` // connecting, live, history, waiting, stopped
	Since     time.Time `json:"since"`
	LastError string    `json:"lastError,omitempty"`
	NextTry   time.Time `json:"nextTry,omitempty"`
}

type worker struct {
	acc         *account.Account
	fingerprint string
	wake        chan struct{} // Sync now: everything
	push        chan struct{} // a change made here: push it, and look quickly
	stop        chan struct{}
	done        chan struct{}
	mu          stdsync.Mutex
	status      WorkerStatus
	// hangUp ends the current connection at once, so a stop does not wait
	// for a batch of history to finish downloading.
	hangUp func()
}

// fingerprint changes when anything the connection depends on does.
func fingerprint(a *account.Account) string {
	return fmt.Sprintf("%s|%d|%s|%v|%s|%v|%s|%s", a.Host, a.Port, a.User, a.SSL, a.Auth, a.Disconnected, a.Password, a.Email)
}

func live(a *account.Account) bool { return a != nil && a.Host != "" && !a.Disconnected }

// Start runs a worker for every live account, and keeps the set in step
// with the accounts until Stop.
func (ws *Workers) Start() {
	ws.reconcile()
	go func() {
		t := time.NewTicker(ws.Reconcile)
		defer t.Stop()
		for {
			select {
			case <-ws.stop:
				return
			case <-t.C:
				ws.reconcile()
			}
		}
	}()
}

// reconcile starts, stops and restarts workers to match the accounts.
func (ws *Workers) reconcile() {
	accs, err := store.ListAccounts()
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, a := range accs {
		seen[a.ID] = true
		ws.mu.Lock()
		cur := ws.w[a.ID]
		ws.mu.Unlock()
		switch {
		case !live(a) && cur != nil:
			ws.Stop(a.ID)
		case live(a) && cur == nil:
			ws.startWorker(a)
		case live(a) && cur.fingerprint != fingerprint(a):
			ws.Stop(a.ID)
			ws.startWorker(a)
		}
	}
	ws.mu.Lock()
	var gone []string
	for id := range ws.w {
		if !seen[id] {
			gone = append(gone, id)
		}
	}
	ws.mu.Unlock()
	for _, id := range gone {
		ws.Stop(id)
	}
}

// Restart picks up an account that was just added or edited.
func (ws *Workers) Restart(id string) {
	ws.Stop(id)
	a, err := store.GetAccount(id)
	if err == nil && live(a) {
		ws.startWorker(a)
	}
}

func (ws *Workers) startWorker(a *account.Account) {
	wk := &worker{acc: a, fingerprint: fingerprint(a), wake: make(chan struct{}, 1), push: make(chan struct{}, 1),
		stop: make(chan struct{}), done: make(chan struct{}),
		status: WorkerStatus{State: "connecting", Since: time.Now()}}
	ws.mu.Lock()
	ws.w[a.ID] = wk
	ws.mu.Unlock()
	go func() {
		defer close(wk.done)
		ws.run(wk)
	}()
}

// Stop ends an account's worker and waits for it to let go.
func (ws *Workers) Stop(id string) {
	ws.mu.Lock()
	wk := ws.w[id]
	delete(ws.w, id)
	ws.mu.Unlock()
	if wk == nil {
		return
	}
	close(wk.stop)
	wk.mu.Lock()
	hang := wk.hangUp
	wk.mu.Unlock()
	if hang != nil {
		hang()
	}
	select {
	case <-wk.done:
	case <-time.After(30 * time.Second):
		log.Printf("[sync] account %s did not stop within 30s", id)
	}
}

// StopAll ends every worker.
func (ws *Workers) StopAll() {
	select {
	case <-ws.stop:
	default:
		close(ws.stop)
	}
	ws.mu.Lock()
	var ids []string
	for id := range ws.w {
		ids = append(ids, id)
	}
	ws.mu.Unlock()
	for _, id := range ids {
		ws.Stop(id)
	}
}

// Wake asks an account's worker to look now. False means there is none.
func (ws *Workers) Wake(id string) bool {
	ws.mu.Lock()
	wk := ws.w[id]
	ws.mu.Unlock()
	if wk == nil {
		return false
	}
	select {
	case wk.wake <- struct{}{}:
	default:
	}
	return true
}

// WakePush asks an account's worker to push queued changes now.
func (ws *Workers) WakePush(id string) {
	ws.mu.Lock()
	wk := ws.w[id]
	ws.mu.Unlock()
	if wk == nil {
		return
	}
	select {
	case wk.push <- struct{}{}:
	default:
	}
}

// Status reports an account's worker; ok is false when it has none.
func (ws *Workers) Status(id string) (WorkerStatus, bool) {
	ws.mu.Lock()
	wk := ws.w[id]
	ws.mu.Unlock()
	if wk == nil {
		return WorkerStatus{State: "stopped"}, false
	}
	wk.mu.Lock()
	defer wk.mu.Unlock()
	return wk.status, true
}

func (wk *worker) set(state, lastErr string, next time.Time) {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	if wk.status.State != state {
		wk.status.Since = time.Now()
	}
	wk.status.State, wk.status.LastError, wk.status.NextTry = state, lastErr, next
}

func (wk *worker) stopped() bool {
	select {
	case <-wk.stop:
		return true
	default:
		return false
	}
}

var errStopped = errors.New("stopped")

func (ws *Workers) run(wk *worker) {
	backoff := 5 * time.Second
	for !wk.stopped() {
		start := time.Now()
		err := ws.session(wk)
		if err == nil || errors.Is(err, errStopped) || wk.stopped() {
			return
		}
		// A session that lasted a while was working; start the pause again.
		if time.Since(start) > 5*time.Minute {
			backoff = 5 * time.Second
		}
		next := time.Now().Add(backoff)
		wk.set("waiting", err.Error(), next)
		store.UpdateAccountStatus(wk.acc.ID, "error", err.Error())
		log.Printf("[sync] account %s: %v; trying again in %s", wk.acc.ID, err, backoff)
		select {
		case <-wk.stop:
			return
		case <-wk.wake:
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Minute)
	}
}

// signals is what the server told us while we were idling.
//
// Only while idling: go-imap reports the EXISTS a server sends in answer to
// our own EXAMINE as a mailbox update, and the expunges and flag changes our
// own MOVE and STORE cause as updates too. Counted outside IDLE, every pass
// set off the next one, and the worker sent STATUS and EXAMINE in a loop as
// fast as the server would answer.
type signals struct {
	mu        stdsync.Mutex
	listening bool
	baseline  uint32 // the message count IDLE started from
	arrived   bool
	expunged  bool
	changed   bool
	poke      chan struct{}
}

func (sg *signals) listen(on bool, baseline uint32) {
	sg.mu.Lock()
	sg.listening, sg.baseline = on, baseline
	sg.mu.Unlock()
}

func (sg *signals) mark(f func() bool) {
	sg.mu.Lock()
	if !sg.listening || !f() {
		sg.mu.Unlock()
		return
	}
	sg.mu.Unlock()
	select {
	case sg.poke <- struct{}{}:
	default:
	}
}

func (sg *signals) take() (arrived, expunged, changed bool) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	arrived, expunged, changed = sg.arrived, sg.expunged, sg.changed
	sg.arrived, sg.expunged, sg.changed = false, false, false
	return
}

// session is one connection's life: it returns when the connection fails
// or the worker is stopped.
func (ws *Workers) session(wk *worker) error {
	limiter := ws.sm.getHostConnectionLimiter(wk.acc.Host)
	select {
	case limiter <- struct{}{}:
	case <-wk.stop:
		return errStopped
	}
	defer func() { <-limiter }()

	wk.set("connecting", "", time.Time{})
	updates := make(chan client.Update, 64)
	sg := &signals{poke: make(chan struct{}, 1)}
	s, err := ws.sm.Open(wk.acc, updates)
	if err != nil {
		return err
	}
	defer s.Close()
	wk.mu.Lock()
	wk.hangUp = func() { s.c.Terminate() }
	wk.mu.Unlock()
	defer func() {
		wk.mu.Lock()
		wk.hangUp = nil
		wk.mu.Unlock()
	}()
	if wk.stopped() {
		return errStopped
	}
	// Drained until the connection is gone: go-imap's reader blocks on a
	// full channel, and it is the reader that ends the connection.
	go func() {
		for {
			select {
			case u := <-updates:
				switch u := u.(type) {
				case *client.MailboxUpdate:
					sg.mark(func() bool {
						if u.Mailbox == nil || u.Mailbox.Messages == sg.baseline {
							return false // RECENT, or nothing new
						}
						sg.arrived = true
						return true
					})
				case *client.ExpungeUpdate:
					sg.mark(func() bool { sg.expunged = true; return true })
				case *client.MessageUpdate:
					sg.mark(func() bool { sg.changed = true; return true })
				}
			case <-s.c.LoggedOut():
				return
			}
		}
	}()

	report := func(r Report) {
		if r.Any() && ws.Changed != nil {
			ws.Changed(wk.acc, r)
		}
	}
	pass := func(full bool) error {
		var r Report
		pr, err := s.Push()
		r.add(pr)
		if err != nil {
			report(r)
			return err
		}
		var fr Report
		if full {
			fr, err = s.FullPass()
		} else {
			fr, err = s.QuickPass()
		}
		r.add(fr)
		report(r)
		if err != nil {
			return err
		}
		store.UpdateAccountStatus(wk.acc.ID, "success", "")
		store.MarkAccountSynced(wk.acc.ID)
		return nil
	}

	store.UpdateAccountStatus(wk.acc.ID, "syncing", "")
	sg.take()
	if err := pass(true); err != nil {
		return err
	}
	lastFull := time.Now()

	for {
		if wk.stopped() {
			return errStopped
		}
		// Whatever happened since the last look: our own changes queued,
		// the server's news, or Sync now.
		arrived, expunged, changed := sg.take()
		woke, pushing := false, false
		select {
		case <-wk.wake:
			woke = true
		default:
		}
		select {
		case <-wk.push:
			pushing = true
		default:
		}
		if expunged || woke || time.Since(lastFull) >= ws.FullEvery {
			if err := pass(true); err != nil {
				return err
			}
			lastFull = time.Now()
			continue
		}
		if arrived || changed || pushing {
			if err := pass(false); err != nil {
				return err
			}
			continue
		}

		if !s.HistoryDone() {
			wk.set("history", "", time.Time{})
			r, _, err := s.HistoryStep()
			report(r)
			if err != nil {
				return err
			}
			continue
		}

		wk.set("live", "", time.Time{})
		mbox, err := s.examine(s.IdleFolder())
		if err != nil {
			return err
		}
		// Mail that arrived between the last pass and now is not news to
		// IDLE; the folder's next UID says whether there is any.
		if st, err := store.GetMailboxState(wk.acc.ID, s.IdleFolder()); err == nil &&
			st.UIDValidity == mbox.UidValidity && mbox.UidNext > 0 && mbox.UidNext-1 > st.LastUID {
			if err := pass(false); err != nil {
				return err
			}
			continue
		}
		sg.listen(true, mbox.Messages)
		reason, err := ws.idle(wk, s, sg, min(ws.QuickEvery, ws.FullEvery-time.Since(lastFull)))
		sg.listen(false, 0)
		if err != nil {
			return err
		}
		if reason == "stop" {
			return errStopped
		}
		// Sync now and a change to push are put back for the top of the
		// loop to see.
		switch reason {
		case "timer":
			if time.Since(lastFull) < ws.FullEvery {
				if err := pass(false); err != nil {
					return err
				}
			}
		case "wake":
			select {
			case wk.wake <- struct{}{}:
			default:
			}
		case "push":
			select {
			case wk.push <- struct{}{}:
			default:
			}
		}
	}
}

// idle waits on the server until something happens, and says what.
func (ws *Workers) idle(wk *worker, s *Session, sg *signals, until time.Duration) (string, error) {
	if until <= 0 {
		return "timer", nil
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.c.Idle(stop, &client.IdleOptions{LogoutTimeout: ws.IdleRestart, PollInterval: time.Minute})
	}()
	finish := func(reason string) (string, error) {
		close(stop)
		if err := <-done; err != nil && reason != "stop" {
			return reason, err
		}
		return reason, nil
	}

	timer := time.NewTimer(until)
	defer timer.Stop()
	// A machine that slept has a connection that only looks alive. The wall
	// clock jumping is how that shows. The same tick looks for changes queued
	// by another process — `iql mail archive` at a terminal — which cannot
	// wake this one directly.
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-wk.stop:
			return finish("stop")
		case <-wk.wake:
			return finish("wake")
		case <-wk.push:
			return finish("push")
		case <-sg.poke:
			return finish("server")
		case <-timer.C:
			return finish("timer")
		case err := <-done:
			if err == nil {
				err = errors.New("the server ended IDLE")
			}
			return "", err
		case now := <-tick.C:
			if now.Sub(last) > 2*time.Minute {
				close(stop)
				<-done
				return "", errors.New("the computer was asleep; reconnecting")
			}
			last = now
			if p, err := store.PendingSummaryFor(wk.acc.ID); err == nil && p.Waiting > 0 {
				return finish("push")
			}
		}
	}
}
