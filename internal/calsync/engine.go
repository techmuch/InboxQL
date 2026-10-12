// Package calsync keeps the calendars current: it reads invitations out of
// new mail, pulls connected accounts (Google, Exchange), pushes changes made
// here, and keeps recurring events expanded.
//
// One engine, run by the server, so every calendar moves on the same clock
// and a change made in the interface reaches its provider the same way
// whether it was a click or a sync.
package calsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/store"
)

// RemoteEvent is an event as a provider gave it.
type RemoteEvent struct {
	Event    calendar.Event
	RemoteID string
	ETag     string
	MyStatus string
	Raw      string
}

// PullResult is one incremental pull of one calendar.
type PullResult struct {
	Upserts []RemoteEvent
	// Deletes are provider ids that no longer exist.
	Deletes []string
	// NextToken is where the next pull starts.
	NextToken string
	// Full means the provider sent everything: anything held locally and not
	// in Upserts is gone at the provider.
	Full bool
}

// PushResult is the provider's version of an event after a change.
type PushResult struct {
	RemoteID string
	ETag     string
	Event    *calendar.Event
}

// ErrConflict is a change refused because the event changed at the provider
// since it was read.
var ErrConflict = errors.New("the event was changed elsewhere since it was read")

// ErrExpiredToken is a sync token the provider no longer accepts; the pull is
// repeated from the start.
var ErrExpiredToken = errors.New("the sync token has expired")

// Provider is one calendar service, for one account.
type Provider interface {
	// Calendars lists the account's calendars as the provider has them.
	Calendars(ctx context.Context) ([]store.CalendarInfo, error)
	// Pull returns what changed in one calendar since token ("" for all).
	Pull(ctx context.Context, cal store.CalendarInfo, token string) (*PullResult, error)
	// Push applies one change: create, update, delete or rsvp.
	Push(ctx context.Context, cal store.CalendarInfo, change store.CalendarChange, ev *store.StoredEvent) (*PushResult, error)
}

// Factory makes the provider for an account.
type Factory func(acct store.CalendarAccount) (Provider, error)

// Engine runs calendar sync.
type Engine struct {
	mu        sync.Mutex
	factories map[string]Factory
	kick      chan struct{}
	lastDaily time.Time
	// Interval between passes when nothing kicks it.
	Interval time.Duration
}

// New is an engine with no providers registered.
func New() *Engine {
	return &Engine{factories: map[string]Factory{}, kick: make(chan struct{}, 1), Interval: 5 * time.Minute}
}

// Register says how to reach a provider.
func (e *Engine) Register(provider string, f Factory) { e.factories[provider] = f }

// Kick asks for a pass now: after a mail sync, after a change.
func (e *Engine) Kick() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// Run passes until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	e.Pass(ctx)
	t := time.NewTicker(e.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.kick:
		}
		e.Pass(ctx)
	}
}

// Pass does everything once. Safe to call concurrently; passes queue.
func (e *Engine) Pass(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Invitations: in batches, so a first pass over a large mailbox does not
	// hold the database for minutes at a stretch.
	for i := 0; i < 1000; i++ {
		rep, err := store.ProcessMailInvitations(500)
		if err != nil {
			log.Printf("[calendar] reading invitations: %v", err)
			break
		}
		if rep.Events+rep.Cancelled+rep.Replies > 0 {
			log.Printf("[calendar] invitations: %d events, %d cancelled, %d replies", rep.Events, rep.Cancelled, rep.Replies)
		}
		if rep.Done || ctx.Err() != nil {
			break
		}
	}
	accounts, err := store.ListCalendarAccounts()
	if err != nil {
		log.Printf("[calendar] %v", err)
		return
	}
	for _, a := range accounts {
		if ctx.Err() != nil {
			return
		}
		if a.Status == "disconnected" || a.Status == "new" {
			continue
		}
		if err := e.syncAccount(ctx, a); err != nil {
			log.Printf("[calendar] %s (%s): %v", a.Name, a.Provider, err)
		}
	}
	if time.Since(e.lastDaily) > 24*time.Hour {
		if err := store.RebuildAllOccurrences(); err != nil {
			log.Printf("[calendar] expanding recurring events: %v", err)
		}
		e.lastDaily = time.Now()
	}
}

// SyncAccount syncs one account now, and reports how it went.
func (e *Engine) SyncAccount(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	a, err := store.GetCalendarAccount(id)
	if err != nil {
		return err
	}
	return e.syncAccount(ctx, *a)
}

func (e *Engine) provider(a store.CalendarAccount) (Provider, error) {
	f, ok := e.factories[a.Provider]
	if !ok {
		return nil, fmt.Errorf("no provider for %q", a.Provider)
	}
	return f(a)
}

func (e *Engine) syncAccount(ctx context.Context, a store.CalendarAccount) error {
	p, err := e.provider(a)
	if err != nil {
		store.SetCalendarAccountStatus(a.ID, "error", err.Error(), false)
		return err
	}
	if err := e.syncCalendars(ctx, a, p); err != nil {
		store.SetCalendarAccountStatus(a.ID, "error", err.Error(), false)
		return err
	}
	// Changes go up before the pull, so the pull brings back the provider's
	// version of what was just sent rather than racing it.
	pushErr := e.pushAccount(ctx, a, p)
	pullErr := e.pullAccount(ctx, a, p)
	switch {
	case pullErr != nil:
		store.SetCalendarAccountStatus(a.ID, "error", pullErr.Error(), false)
		return pullErr
	case pushErr != nil:
		store.SetCalendarAccountStatus(a.ID, "ok", "some changes could not be sent: "+pushErr.Error(), true)
		return pushErr
	}
	return store.SetCalendarAccountStatus(a.ID, "ok", "", true)
}

func (e *Engine) syncCalendars(ctx context.Context, a store.CalendarAccount, p Provider) error {
	remote, err := p.Calendars(ctx)
	if err != nil {
		return err
	}
	for _, rc := range remote {
		rc.AccountID, rc.Provider = a.ID, a.Provider
		if existing, err := store.CalendarByRemote(a.ID, rc.RemoteID); err == nil {
			rc.ID, rc.Hidden = existing.ID, existing.Hidden
			if existing.Color != "" {
				rc.Color = existing.Color
			}
		} else {
			rc.ID = calendarID(a.ID, rc.RemoteID)
		}
		if err := store.SaveCalendar(rc); err != nil {
			return err
		}
	}
	return nil
}

func calendarID(accountID, remoteID string) string {
	return store.EventID(accountID, remoteID, time.Time{})[3:] // a stable short id
}

func (e *Engine) pullAccount(ctx context.Context, a store.CalendarAccount, p Provider) error {
	cals, err := store.ListCalendars()
	if err != nil {
		return err
	}
	// Each calendar on its own: one that cannot be read — a shared calendar
	// whose owner withdrew it — does not stop the rest from syncing.
	var failed []string
	for _, c := range cals {
		if c.AccountID != a.ID || c.Hidden {
			continue
		}
		if err := e.pullCalendar(ctx, p, c); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", c.Name, err))
		}
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "; "))
	}
	return nil
}

func (e *Engine) pullCalendar(ctx context.Context, p Provider, c store.CalendarInfo) error {
	res, err := p.Pull(ctx, c, c.SyncToken)
	if errors.Is(err, ErrExpiredToken) {
		res, err = p.Pull(ctx, c, "")
	}
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, re := range res.Upserts {
		if re.Event.Status == "cancelled" && re.Event.RecurrenceID.IsZero() {
			if err := store.DeleteEventByRemote(c.ID, re.RemoteID); err != nil {
				return err
			}
			continue
		}
		if _, err := store.UpsertEvent(c.ID, re.Event, store.EventMeta{RemoteID: re.RemoteID, ETag: re.ETag,
			MyStatus: re.MyStatus, Raw: re.Raw, Origin: "sync"}); err != nil {
			return err
		}
		seen[re.RemoteID] = true
	}
	for _, id := range res.Deletes {
		if err := store.DeleteEventByRemote(c.ID, id); err != nil {
			return err
		}
	}
	if res.Full {
		if err := store.PruneCalendar(c.ID, seen); err != nil {
			return err
		}
	}
	return store.SetCalendarSyncToken(c.ID, res.NextToken)
}

func (e *Engine) pushAccount(ctx context.Context, a store.CalendarAccount, p Provider) error {
	changes, err := store.ListCalendarChanges("pending")
	if err != nil {
		return err
	}
	var firstErr error
	for _, ch := range changes {
		cal, err := store.GetCalendar(ch.CalendarID)
		if err != nil || cal.AccountID != a.ID {
			continue
		}
		if err := e.push(ctx, p, *cal, ch); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (e *Engine) push(ctx context.Context, p Provider, cal store.CalendarInfo, ch store.CalendarChange) error {
	var ev *store.StoredEvent
	if ch.EventID != "" {
		ev, _ = store.GetEvent(ch.EventID)
	}
	if ev == nil && ch.Op != "delete" {
		return store.SetCalendarChangeState(ch.ID, "failed", "the event no longer exists here")
	}
	res, err := p.Push(ctx, cal, ch, ev)
	switch {
	case errors.Is(err, ErrConflict):
		// Not forced. The pull that follows brings the provider's version
		// in; the change stays, marked, so the person can apply it again.
		return store.SetCalendarChangeState(ch.ID, "conflict", err.Error())
	case err != nil:
		store.SetCalendarChangeState(ch.ID, "failed", err.Error())
		return err
	}
	if ch.Op == "delete" && ev != nil && ev.Status == "cancelled" && ev.RecurrenceID == nil {
		// Gone at the provider; now gone here.
		if err := store.DeleteEvent(cal.ID, ev.UID, time.Time{}); err != nil {
			return err
		}
		return store.SetCalendarChangeState(ch.ID, "done", "")
	}
	if res != nil && ev != nil {
		model := ev.Model()
		if res.Event != nil {
			model = *res.Event
		}
		if _, err := store.UpsertEvent(cal.ID, model, store.EventMeta{RemoteID: res.RemoteID, ETag: res.ETag,
			MyStatus: ev.MyStatus, Origin: ev.Origin}); err != nil {
			return err
		}
	}
	return store.SetCalendarChangeState(ch.ID, "done", "")
}

// Payload helpers for changes.

// RSVP is the payload of an rsvp change.
type RSVP struct {
	Status  string `json:"status"`
	Comment string `json:"comment,omitempty"`
}

// DecodeRSVP reads an rsvp change's payload.
func DecodeRSVP(ch store.CalendarChange) (RSVP, error) {
	var r RSVP
	err := json.Unmarshal(ch.Payload, &r)
	return r, err
}
