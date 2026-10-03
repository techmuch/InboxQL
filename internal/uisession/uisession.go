// Package uisession tracks the open frontends and carries commands to them.
//
// # What this is, and what it is not
//
// Not a sync. There is no canonical state being replicated and no conflict to
// resolve, because only one side ever writes a given fact: a window owns what
// it is showing, and the server owns the list of which windows exist.
//
// A command is an instruction to a live window, not a new source of truth about
// it. Point a window at a query from the command line, reload the window, and
// it comes back showing whatever it had persisted for itself. The CLI points;
// it does not drive.
//
// # Why it is in memory
//
// A connection is not a fact that should outlive the process holding it. A
// registry restored from disk is a list of windows that are not there, and
// every command sent to one of them would appear to succeed.
//
// # Why names are short
//
// Because they are typed. `iql ui query 2 "from:stripe"` is the point; a UUID
// in a listing is a thing to copy and paste, which is the same as not having a
// listing. The window shows the same name, so "which one is 2" has an answer
// you can see rather than deduce.
package uisession

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Expiry is how long a window may go unheard from before it is forgotten.
//
// Longer than the heartbeat by enough that one missed beat is not a death —
// a laptop that slept for ten seconds has not closed its window — and short
// enough that a closed tab leaves the listing promptly. A stale entry is worse
// than a missing one: a command to it reports success and does nothing.
const (
	Heartbeat = 10 * time.Second
	Expiry    = 35 * time.Second
)

// Command is one instruction for a window.
type Command struct {
	// Verb is query, open or notice.
	Verb string `json:"verb"`
	// Arg is the query text, the thing to open, or the message.
	Arg string `json:"arg"`
	// Note is why, shown beside whatever changed.
	//
	// Carried on every verb rather than only on `notice`: a screen that
	// reorganises itself with no explanation is alarming rather than helpful,
	// and the explanation belongs in the same message as the change.
	Note string `json:"note,omitempty"`
	// At is when the server accepted it.
	At time.Time `json:"at"`
}

// Session is one connected frontend.
type Session struct {
	// Name is what a person types to address this window: "1", "2".
	Name string `json:"name"`
	// Title is what the window calls itself — the browser, usually.
	Title string `json:"title,omitempty"`
	// Showing is what is on screen: the query, and which tabs are open.
	Showing Showing `json:"showing"`
	// Connected is whether its command channel is live. A window that has
	// registered but whose stream has dropped is still listed, because it is
	// still open — it just cannot be commanded.
	Connected bool      `json:"connected"`
	Since     time.Time `json:"since"`
	LastSeen  time.Time `json:"lastSeen"`
	// LastCommand is what it was last told, so a listing can explain why a
	// window is showing something nobody in front of it asked for.
	LastCommand *Command `json:"lastCommand,omitempty"`

	commands chan Command
}

// Showing is what a window reports about itself.
//
// Deliberately small. This is "what am I looking at", answered well enough for
// a person reading a list or an agent deciding where to put something — not a
// serialisation of the interface.
type Showing struct {
	// Query is the Desk's current query, when a Desk is open.
	Query string `json:"query,omitempty"`
	// Tabs are the open tab ids, in order.
	Tabs []string `json:"tabs,omitempty"`
	// Active is the tab in front.
	Active string `json:"active,omitempty"`
}

// Registry holds the open windows.
type Registry struct {
	mu   sync.Mutex
	byID map[string]*Session
	next int
}

// New makes an empty registry.
func New() *Registry {
	return &Registry{byID: map[string]*Session{}, next: 1}
}

// Register adds a window and returns the name it was given.
//
// The name is assigned here rather than chosen by the window, because it has to
// be unique among windows and short enough to type, and a browser tab knows
// neither what else is open nor what is short.
func (r *Registry) Register(title string) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(time.Now())

	name := strconv.Itoa(r.next)
	r.next++

	now := time.Now()
	s := &Session{
		Name: name, Title: title, Since: now, LastSeen: now,
		// Buffered: a command for a window whose reader is momentarily busy
		// should wait briefly rather than be dropped, and a window that has
		// stopped reading entirely is about to expire anyway.
		commands: make(chan Command, 8),
	}
	r.byID[name] = s
	return s
}

// Touch records that a window is alive and what it is showing.
func (r *Registry) Touch(name string, showing Showing) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[name]
	if !ok {
		return false
	}
	s.LastSeen = time.Now()
	s.Showing = showing
	return true
}

// Connect marks a window's command channel live and returns it.
//
// Separate from Register because they are separate events: a window registers
// once and may reconnect its stream many times, and a window whose stream has
// dropped is still open — it just cannot be told anything.
func (r *Registry) Connect(name string) (<-chan Command, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[name]
	if !ok {
		return nil, nil, false
	}
	s.Connected = true
	s.LastSeen = time.Now()
	ch := s.commands

	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if cur, ok := r.byID[name]; ok && cur == s {
			cur.Connected = false
		}
	}, true
}

// Send delivers a command to one window.
//
// Fails rather than succeeding quietly when the window is gone or not reading.
// A command that appears to work and does nothing is the failure mode this
// whole package has to avoid: somebody types a query at a window, sees no
// error, and is then confused about why the screen did not change.
func (r *Registry) Send(name string, c Command) error {
	r.mu.Lock()
	s, ok := r.byID[name]
	if ok {
		r.expireOneLocked(s, time.Now())
		_, ok = r.byID[name]
	}
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("no window named %q is open (`iql ui list` shows which are)", name)
	}
	if !s.Connected {
		return fmt.Errorf("window %q is open but its command channel is not connected", name)
	}

	c.At = time.Now()
	select {
	case s.commands <- c:
		r.mu.Lock()
		s.LastCommand = &c
		r.mu.Unlock()
		return nil
	default:
		return fmt.Errorf("window %q is not reading its commands", name)
	}
}

// List returns the open windows, oldest first.
//
// Oldest first because the names are assigned in that order, so the list reads
// in the order the numbers do.
func (r *Registry) List() []*Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(time.Now())

	out := make([]*Session, 0, len(r.byID))
	for _, s := range r.byID {
		// Copied: the caller is about to serialise this while heartbeats are
		// still arriving, and handing out the live struct is a data race that
		// only shows up under load.
		c := *s
		c.commands = nil
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// Get returns one window, or nil.
func (r *Registry) Get(name string) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[name]
	if !ok {
		return nil
	}
	c := *s
	c.commands = nil
	return &c
}

// Forget removes a window, for a tab that closed cleanly.
func (r *Registry) Forget(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, name)
}

func (r *Registry) expireLocked(now time.Time) {
	for name, s := range r.byID {
		if now.Sub(s.LastSeen) > Expiry {
			delete(r.byID, name)
		}
	}
}

func (r *Registry) expireOneLocked(s *Session, now time.Time) {
	if now.Sub(s.LastSeen) > Expiry {
		delete(r.byID, s.Name)
	}
}
