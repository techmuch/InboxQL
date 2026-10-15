// Package imaptest is a fake IMAP server for tests, Gmail-like when asked.
//
// The stock go-imap test backend was enough to prove a login works. It is
// not enough to test a mirror. It has no labels, no permanent message ids, no
// MODSEQ, never marks a message read when its body is fetched, and numbers
// every folder densely from one. Each of those is something real servers do
// and the sync has to get right, so this server does them:
//
//   - In Gmail mode, a message exists once and carries labels; every folder is
//     a view over those labels, with its own UIDs, as Gmail's IMAP is.
//   - Fetching BODY[] in a read-write SELECT sets \Seen; BODY.PEEK[] does not.
//   - Every change bumps a MODSEQ, and UID FETCH honours (CHANGEDSINCE n).
//   - IDLE reports EXISTS and EXPUNGE as the folder changes underneath it.
//   - UID MOVE, UID STORE ±X-GM-LABELS, APPEND and CREATE work.
//
// It is deliberately small: one user, no AUTHENTICATE, no SEARCH beyond what
// the sync uses, and nothing parsed that the client in this repository does
// not send.
package imaptest

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/utf7"
)

// Options choose what kind of server it is.
type Options struct {
	// Gmail makes the server label-based, with [Gmail]/ folders, X-GM-EXT-1
	// and the X-GM-* fetch items.
	Gmail bool
	// NoIdle and NoMove switch those capabilities off, for testing fallbacks.
	NoIdle bool
	NoMove bool
}

// Msg is one message on the server.
type Msg struct {
	ID     uint64 // X-GM-MSGID
	ThrID  uint64 // X-GM-THRID
	Raw    []byte
	Date   time.Time
	Flags  map[string]bool
	Labels map[string]bool // Gmail: \Inbox, \Sent, \Trash, \Spam, user labels. Otherwise: the one folder it is in.
	ModSeq uint64
	gone   bool
}

// MessageID is the message's Message-ID header, without angle brackets.
func (m *Msg) MessageID() string {
	if msg, err := mail.ReadMessage(strings.NewReader(string(m.Raw))); err == nil {
		return strings.Trim(msg.Header.Get("Message-Id"), "<>")
	}
	return ""
}

type folder struct {
	name        string
	special     string // \All \Trash \Junk \Sent \Drafts \Archive \Important \Flagged, or ""
	noselect    bool
	uidValidity uint32
	uidNext     uint32
	uids        map[uint64]uint32 // message id -> UID, while it is in the folder
}

// Server is a running fake.
type Server struct {
	Addr    string
	Port    int
	RootCAs *x509.CertPool
	User    string
	Pass    string

	opt      Options
	cert     tls.Certificate
	mu       sync.Mutex
	msgs     []*Msg
	folders  []*folder
	nextID   uint64
	modseq   uint64
	sessions map[*session]bool
	ln       net.Listener
	log      []string
	logins   int
}

// Start runs a server on loopback with implicit TLS. Close it when done.
func Start(opt Options) (*Server, error) {
	cert, pool, err := selfSigned()
	if err != nil {
		return nil, err
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		return nil, err
	}
	s := &Server{
		Addr: ln.Addr().String(), Port: ln.Addr().(*net.TCPAddr).Port,
		RootCAs: pool, User: "username", Pass: "password",
		opt: opt, cert: cert, nextID: 1000, modseq: 1, sessions: map[*session]bool{}, ln: ln,
	}
	if opt.Gmail {
		s.addFolder("INBOX", "")
		s.addFolder("[Gmail]", "").noselect = true
		s.addFolder("[Gmail]/All Mail", `\All`)
		s.addFolder("[Gmail]/Sent Mail", `\Sent`)
		s.addFolder("[Gmail]/Drafts", `\Drafts`)
		s.addFolder("[Gmail]/Important", `\Important`)
		s.addFolder("[Gmail]/Starred", `\Flagged`)
		s.addFolder("[Gmail]/Spam", `\Junk`)
		s.addFolder("[Gmail]/Trash", `\Trash`)
	} else {
		s.addFolder("INBOX", "")
		s.addFolder("Sent", `\Sent`)
		s.addFolder("Drafts", `\Drafts`)
		s.addFolder("Archive", `\Archive`)
		s.addFolder("Junk", `\Junk`)
		s.addFolder("Trash", `\Trash`)
	}
	go s.serve()
	return s, nil
}

// Close stops the server and drops every connection.
func (s *Server) Close() {
	s.ln.Close()
	s.mu.Lock()
	for ss := range s.sessions {
		ss.conn.Close()
	}
	s.mu.Unlock()
}

// Gmail-mode system labels, as X-GM-LABELS spells them.
const (
	Inbox     = `\Inbox`
	Sent      = `\Sent`
	Important = `\Important`
	Draft     = `\Draft`
	Trash     = `\Trash`
	Spam      = `\Spam`
)

// Deliver adds a message, as mail arriving. In Gmail mode the labels default
// to \Inbox — an empty, non-nil list means archived; otherwise the first
// label is the folder, default INBOX.
func (s *Server) Deliver(raw string, labels ...string) *Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	if labels == nil || (!s.opt.Gmail && len(labels) == 0) {
		if s.opt.Gmail {
			labels = []string{Inbox}
		} else {
			labels = []string{"INBOX"}
		}
	}
	m := s.newMsgLocked([]byte(raw), time.Now())
	for _, l := range labels {
		m.Labels[l] = true
		if !s.opt.Gmail {
			break
		}
		s.ensureLabelFolderLocked(l)
	}
	s.changedLocked()
	return m
}

// Message builds a minimal RFC 822 message.
func Message(id, subject, from string) string {
	return fmt.Sprintf("From: %s\r\nTo: me@example.com\r\nSubject: %s\r\nMessage-ID: <%s>\r\nDate: %s\r\n\r\nbody of %s\r\n",
		from, subject, id, time.Now().Format(time.RFC1123Z), subject)
}

// Find returns the message with this Message-ID (without brackets).
func (s *Server) Find(messageID string) *Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.msgs {
		if !m.gone && m.MessageID() == messageID {
			return m
		}
	}
	return nil
}

// Change runs f under the server's lock, as a change made on another device,
// then bumps the message's MODSEQ and tells idling clients.
func (s *Server) Change(m *Msg, f func(m *Msg)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(m)
	s.modseq++
	m.ModSeq = s.modseq
	if s.opt.Gmail {
		for l := range m.Labels {
			s.ensureLabelFolderLocked(l)
		}
	}
	s.changedLocked()
}

// Delete removes a message for good, as emptying the trash does.
func (s *Server) Delete(m *Msg) {
	s.Change(m, func(m *Msg) { m.gone = true })
}

// LabelsOf and FlagsOf report a message's current state, sorted.
func (s *Server) LabelsOf(m *Msg) []string { return s.keys(m.Labels) }
func (s *Server) FlagsOf(m *Msg) []string  { return s.keys(m.Flags) }

func (s *Server) keys(set map[string]bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k, v := range set {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Commands returns every command received, for asserting on what a client
// did — that it never expunged, say.
func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

// Logins counts successful logins.
func (s *Server) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

// Renumber gives a folder a new UIDVALIDITY, as a server rebuilding it does.
func (s *Server) Renumber(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.folderLocked(name); f != nil {
		f.uidValidity++
		f.uids = map[uint64]uint32{}
		f.uidNext = 1
		s.refreshLocked(f)
	}
}

func (s *Server) newMsgLocked(raw []byte, date time.Time) *Msg {
	s.nextID++
	s.modseq++
	m := &Msg{ID: s.nextID, ThrID: s.nextID, Raw: raw, Date: date,
		Flags: map[string]bool{}, Labels: map[string]bool{}, ModSeq: s.modseq}
	s.msgs = append(s.msgs, m)
	return m
}

func (s *Server) addFolder(name, special string) *folder {
	f := &folder{name: name, special: special, uidValidity: uint32(len(s.folders) + 1), uidNext: 1, uids: map[uint64]uint32{}}
	s.folders = append(s.folders, f)
	return f
}

func (s *Server) folderLocked(name string) *folder {
	for _, f := range s.folders {
		if strings.EqualFold(f.name, name) || (strings.EqualFold(name, "INBOX") && f.name == "INBOX") {
			return f
		}
	}
	return nil
}

// ensureLabelFolderLocked gives a user label its folder, as Gmail does.
func (s *Server) ensureLabelFolderLocked(label string) {
	if strings.HasPrefix(label, `\`) || s.folderLocked(label) != nil {
		return
	}
	s.addFolder(label, "")
}

// member reports whether a message is in a folder.
func (s *Server) member(f *folder, m *Msg) bool {
	if m.gone || f.noselect {
		return false
	}
	if !s.opt.Gmail {
		return m.Labels[f.name]
	}
	binned := m.Labels[Trash] || m.Labels[Spam]
	switch f.special {
	case `\All`:
		return !binned
	case `\Trash`:
		return m.Labels[Trash]
	case `\Junk`:
		return m.Labels[Spam] && !m.Labels[Trash]
	case `\Sent`:
		return m.Labels[Sent] && !binned
	case `\Drafts`:
		return m.Labels[Draft] && !binned
	case `\Important`:
		return m.Labels[Important] && !binned
	case `\Flagged`:
		return m.Flags[`\Flagged`] && !binned
	}
	if f.name == "INBOX" {
		return m.Labels[Inbox] && !binned
	}
	return m.Labels[f.name] && !binned
}

// ownLabel is the label a folder stands for, which Gmail leaves out of
// X-GM-LABELS while that folder is selected.
func (s *Server) ownLabel(f *folder) string {
	switch {
	case f.name == "INBOX":
		return Inbox
	case f.special == `\Sent`:
		return Sent
	case f.special == `\Important`:
		return Important
	case f.special == `\Drafts`:
		return Draft
	case f.special == "":
		return f.name
	}
	return ""
}

// refreshLocked assigns UIDs to messages that have joined a folder and drops
// the ones that have left. UIDs are never reused.
func (s *Server) refreshLocked(f *folder) {
	for _, m := range s.msgs {
		_, in := f.uids[m.ID]
		switch member := s.member(f, m); {
		case member && !in:
			f.uids[m.ID] = f.uidNext
			f.uidNext++
		case !member && in:
			delete(f.uids, m.ID)
		}
	}
}

// order is a folder's messages by UID, which is sequence-number order.
func (s *Server) orderLocked(f *folder) []uint64 {
	s.refreshLocked(f)
	ids := make([]uint64, 0, len(f.uids))
	for id := range f.uids {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return f.uids[ids[i]] < f.uids[ids[j]] })
	return ids
}

func (s *Server) byIDLocked(id uint64) *Msg {
	for _, m := range s.msgs {
		if m.ID == id {
			return m
		}
	}
	return nil
}

func (s *Server) changedLocked() {
	for ss := range s.sessions {
		select {
		case ss.notify <- struct{}{}:
		default:
		}
	}
}

func (s *Server) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		ss := &session{s: s, conn: c, w: bufio.NewWriter(c), notify: make(chan struct{}, 1)}
		s.mu.Lock()
		s.sessions[ss] = true
		s.mu.Unlock()
		go ss.run()
	}
}

// ---- one connection ----

type session struct {
	s        *Server
	conn     net.Conn
	wmu      sync.Mutex
	w        *bufio.Writer
	authed   bool
	sel      *folder
	readOnly bool
	view     []uint64          // what this client believes the folder holds, by sequence number
	seenMod  map[uint64]uint64 // the MODSEQ each message had when this client last heard of it
	notify   chan struct{}
}

type command struct {
	tag  string
	name string
	args []any
	err  error
}

// quoted marks a string that arrived quoted, so it is never mistaken for an
// atom such as NIL.
type quoted string

func (ss *session) send(format string, a ...any) {
	ss.wmu.Lock()
	defer ss.wmu.Unlock()
	fmt.Fprintf(ss.w, format+"\r\n", a...)
	ss.w.Flush()
}

func (ss *session) sendRaw(b []byte) {
	ss.wmu.Lock()
	defer ss.wmu.Unlock()
	ss.w.Write(b)
	ss.w.Flush()
}

func (ss *session) run() {
	defer func() {
		ss.conn.Close()
		ss.s.mu.Lock()
		delete(ss.s.sessions, ss)
		ss.s.mu.Unlock()
	}()
	ss.send("* OK fake IMAP ready")

	cmds := make(chan command)
	r := bufio.NewReader(ss.conn)
	go func() {
		defer close(cmds)
		for {
			c, err := ss.readCommand(r)
			if err != nil {
				return
			}
			cmds <- c
		}
	}()

	for c := range cmds {
		ss.s.mu.Lock()
		ss.s.log = append(ss.s.log, strings.TrimSpace(c.name+" "+fmtArgs(c.args)))
		ss.s.mu.Unlock()
		if c.err != nil {
			ss.send("%s BAD %v", c.tag, c.err)
			continue
		}
		if c.name == "IDLE" {
			if !ss.idle(c, cmds) {
				return
			}
			continue
		}
		if !ss.handle(c) {
			return
		}
	}
}

// readCommand reads one command, answering literal continuations as it goes.
func (ss *session) readCommand(r *bufio.Reader) (command, error) {
	var buf []byte
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return command{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if i := strings.LastIndexByte(line, '{'); i >= 0 && strings.HasSuffix(line, "}") {
			nstr := strings.TrimSuffix(line[i+1:len(line)-1], "+")
			if n, err := strconv.Atoi(nstr); err == nil {
				if !strings.HasSuffix(line, "+}") {
					ss.send("+ Ready")
				}
				lit := make([]byte, n)
				if _, err := io.ReadFull(r, lit); err != nil {
					return command{}, err
				}
				buf = append(buf, line[:i]...)
				buf = append(buf, fmt.Sprintf("\x00%d\x00", n)...)
				buf = append(buf, lit...)
				continue
			}
		}
		buf = append(buf, line...)
		break
	}
	toks, err := tokenize(buf)
	c := command{err: err}
	if len(toks) > 0 {
		c.tag, _ = toks[0].(string)
	}
	if len(toks) > 1 {
		name, _ := toks[1].(string)
		c.name = strings.ToUpper(name)
		c.args = toks[2:]
		if c.name == "UID" && len(c.args) > 0 {
			sub, _ := c.args[0].(string)
			c.name = "UID " + strings.ToUpper(sub)
			c.args = c.args[1:]
		}
	}
	if c.tag == "" && c.err == nil {
		c.err = fmt.Errorf("no tag")
	}
	return c, nil
}

// tokenize splits a command into atoms, quoted strings, lists and literals.
// A literal was spliced in by readCommand as NUL, its length, NUL, its bytes.
func tokenize(b []byte) ([]any, error) {
	var stack [][]any
	var cur []any
	i := 0
	for i < len(b) {
		switch ch := b[i]; {
		case ch == ' ':
			i++
		case ch == '(':
			stack = append(stack, cur)
			cur = nil
			i++
		case ch == ')':
			if len(stack) == 0 {
				return nil, fmt.Errorf("unbalanced )")
			}
			list := cur
			cur = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cur = append(cur, list)
			i++
		case ch == '"':
			var sb strings.Builder
			i++
			for i < len(b) && b[i] != '"' {
				if b[i] == '\\' && i+1 < len(b) {
					i++
				}
				sb.WriteByte(b[i])
				i++
			}
			i++
			cur = append(cur, quoted(sb.String()))
		case ch == 0:
			j := i + 1
			for j < len(b) && b[j] != 0 {
				j++
			}
			n, _ := strconv.Atoi(string(b[i+1 : j]))
			lit := b[j+1 : j+1+n]
			cur = append(cur, append([]byte(nil), lit...))
			i = j + 1 + n
		default:
			j := i
			depth := 0
			for j < len(b) {
				c := b[j]
				if c == '[' {
					depth++
				} else if c == ']' {
					depth--
				} else if depth == 0 && (c == ' ' || c == '(' || c == ')') {
					break
				}
				j++
			}
			cur = append(cur, string(b[i:j]))
			i = j
		}
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("unbalanced (")
	}
	return cur, nil
}

func fmtArgs(args []any) string {
	var parts []string
	for _, a := range args {
		switch v := a.(type) {
		case string:
			parts = append(parts, v)
		case quoted:
			parts = append(parts, strconv.Quote(string(v)))
		case []byte:
			parts = append(parts, fmt.Sprintf("{%d}", len(v)))
		case []any:
			parts = append(parts, "("+fmtArgs(v)+")")
		}
	}
	return strings.Join(parts, " ")
}

func str(a any) string {
	switch v := a.(type) {
	case string:
		return v
	case quoted:
		return string(v)
	}
	return ""
}

func list(a any) []any {
	if l, ok := a.([]any); ok {
		return l
	}
	return []any{a}
}

func (ss *session) capabilities() string {
	caps := "IMAP4rev1 UIDPLUS CONDSTORE ENABLE AUTH=PLAIN"
	if !ss.s.opt.NoIdle {
		caps += " IDLE"
	}
	if !ss.s.opt.NoMove {
		caps += " MOVE"
	}
	if ss.s.opt.Gmail {
		caps += " X-GM-EXT-1"
	}
	return caps
}

// handle runs one command; false ends the connection.
func (ss *session) handle(c command) bool {
	s := ss.s
	needAuth := map[string]bool{"CAPABILITY": false, "LOGIN": false, "LOGOUT": false, "NOOP": false}
	if must, listed := needAuth[c.name]; (!listed || must) && !ss.authed {
		ss.send("%s NO not authenticated", c.tag)
		return true
	}
	switch c.name {
	case "CAPABILITY":
		ss.send("* CAPABILITY %s", ss.capabilities())
		ss.send("%s OK CAPABILITY done", c.tag)
	case "LOGIN":
		if len(c.args) == 2 && str(c.args[0]) == s.User && str(c.args[1]) == s.Pass {
			ss.authed = true
			s.mu.Lock()
			s.logins++
			s.mu.Unlock()
			ss.send("%s OK [CAPABILITY %s] logged in", c.tag, ss.capabilities())
		} else {
			ss.send("%s NO [AUTHENTICATIONFAILED] Invalid credentials", c.tag)
		}
	case "LOGOUT":
		ss.send("* BYE logging out")
		ss.send("%s OK LOGOUT done", c.tag)
		return false
	case "NOOP", "CHECK":
		ss.flushUpdates()
		ss.send("%s OK %s done", c.tag, c.name)
	case "ENABLE":
		ss.send("* ENABLED CONDSTORE")
		ss.send("%s OK ENABLE done", c.tag)
	case "LIST", "LSUB":
		s.mu.Lock()
		for _, f := range s.folders {
			attrs := []string{}
			if f.noselect {
				attrs = append(attrs, `\Noselect`, `\HasChildren`)
			} else {
				attrs = append(attrs, `\HasNoChildren`)
			}
			if f.special != "" {
				attrs = append(attrs, f.special)
			}
			ss.send(`* %s (%s) "/" %s`, c.name, strings.Join(attrs, " "), strconv.Quote(f.name))
		}
		s.mu.Unlock()
		ss.send("%s OK %s done", c.tag, c.name)
	case "CREATE":
		s.mu.Lock()
		if s.folderLocked(str(c.args[0])) == nil {
			s.addFolder(str(c.args[0]), "")
		}
		s.mu.Unlock()
		ss.send("%s OK CREATE done", c.tag)
	case "DELETE":
		name := str(c.args[0])
		if d, err := utf7.Encoding.NewDecoder().String(name); err == nil {
			name = d
		}
		s.mu.Lock()
		found := false
		for i, f := range s.folders {
			if strings.EqualFold(f.name, name) && f.special == "" && f.name != "INBOX" {
				s.folders = append(s.folders[:i], s.folders[i+1:]...)
				found = true
				break
			}
		}
		if found && s.opt.Gmail {
			// Deleting a Gmail label takes it off every message.
			for _, m := range s.msgs {
				delete(m.Labels, name)
			}
		}
		s.changedLocked()
		s.mu.Unlock()
		if !found {
			ss.send("%s NO [NONEXISTENT] no such mailbox", c.tag)
			return true
		}
		ss.send("%s OK DELETE done", c.tag)
	case "SELECT", "EXAMINE":
		s.mu.Lock()
		f := s.folderLocked(str(c.args[0]))
		if f == nil || f.noselect {
			s.mu.Unlock()
			ss.send("%s NO no such mailbox", c.tag)
			return true
		}
		ss.sel, ss.readOnly = f, c.name == "EXAMINE"
		ss.view = s.orderLocked(f)
		ss.seenMod = map[uint64]uint64{}
		for _, id := range ss.view {
			ss.seenMod[id] = s.byIDLocked(id).ModSeq
		}
		n, uv, un, ms := len(ss.view), f.uidValidity, f.uidNext, s.modseq
		s.mu.Unlock()
		ss.send(`* FLAGS (\Answered \Flagged \Draft \Deleted \Seen)`)
		ss.send("* %d EXISTS", n)
		ss.send("* 0 RECENT")
		ss.send("* OK [UIDVALIDITY %d] UIDs valid", uv)
		ss.send("* OK [UIDNEXT %d] next UID", un)
		ss.send("* OK [HIGHESTMODSEQ %d] modseq", ms)
		mode := "READ-WRITE"
		if ss.readOnly {
			mode = "READ-ONLY"
		}
		ss.send("%s OK [%s] %s done", c.tag, mode, c.name)
	case "STATUS":
		s.mu.Lock()
		f := s.folderLocked(str(c.args[0]))
		if f == nil || f.noselect {
			s.mu.Unlock()
			ss.send("%s NO no such mailbox", c.tag)
			return true
		}
		order := s.orderLocked(f)
		unseen := 0
		for _, id := range order {
			if !s.byIDLocked(id).Flags[`\Seen`] {
				unseen++
			}
		}
		var items []string
		for _, it := range list(c.args[1]) {
			switch strings.ToUpper(str(it)) {
			case "MESSAGES":
				items = append(items, fmt.Sprintf("MESSAGES %d", len(order)))
			case "UIDNEXT":
				items = append(items, fmt.Sprintf("UIDNEXT %d", f.uidNext))
			case "UIDVALIDITY":
				items = append(items, fmt.Sprintf("UIDVALIDITY %d", f.uidValidity))
			case "UNSEEN":
				items = append(items, fmt.Sprintf("UNSEEN %d", unseen))
			case "HIGHESTMODSEQ":
				items = append(items, fmt.Sprintf("HIGHESTMODSEQ %d", s.modseq))
			}
		}
		s.mu.Unlock()
		ss.send("* STATUS %s (%s)", strconv.Quote(f.name), strings.Join(items, " "))
		ss.send("%s OK STATUS done", c.tag)
	case "CLOSE", "UNSELECT":
		ss.sel, ss.view = nil, nil
		ss.send("%s OK %s done", c.tag, c.name)
	case "APPEND":
		ss.append(c)
	case "FETCH", "UID FETCH":
		ss.fetch(c)
	case "UID SEARCH", "SEARCH":
		ss.search(c)
	case "UID STORE", "STORE":
		ss.store(c)
	case "UID MOVE", "MOVE", "UID COPY", "COPY":
		ss.move(c)
	case "EXPUNGE", "UID EXPUNGE":
		ss.expunge(c)
	default:
		ss.send("%s BAD unknown command %s", c.tag, c.name)
	}
	return true
}

// idle waits for DONE, reporting changes to the selected folder as they
// happen. False ends the connection.
func (ss *session) idle(c command, cmds <-chan command) bool {
	if ss.s.opt.NoIdle {
		ss.send("%s BAD IDLE not supported", c.tag)
		return true
	}
	ss.send("+ idling")
	for {
		select {
		case <-ss.notify:
			ss.flushUpdates()
		case next, ok := <-cmds:
			if !ok {
				return false
			}
			// The reader saw "DONE" as a command whose tag is DONE.
			if strings.EqualFold(next.tag, "DONE") {
				ss.flushUpdates()
				ss.send("%s OK IDLE done", c.tag)
				return true
			}
			ss.send("%s BAD expected DONE", next.tag)
		}
	}
}

// flushUpdates tells this client what changed in its selected folder:
// EXPUNGE for what left, EXISTS for what arrived, FETCH for new flags.
func (ss *session) flushUpdates() {
	if ss.sel == nil {
		return
	}
	s := ss.s
	s.mu.Lock()
	now := s.orderLocked(ss.sel)
	present := map[uint64]bool{}
	for _, id := range now {
		present[id] = true
	}
	var lines []string
	for i := len(ss.view) - 1; i >= 0; i-- {
		if !present[ss.view[i]] {
			lines = append(lines, fmt.Sprintf("* %d EXPUNGE", i+1))
			ss.view = append(ss.view[:i], ss.view[i+1:]...)
		}
	}
	known := map[uint64]bool{}
	for _, id := range ss.view {
		known[id] = true
	}
	grew := false
	for _, id := range now {
		if !known[id] {
			ss.view = append(ss.view, id)
			grew = true
		}
	}
	if grew {
		lines = append(lines, fmt.Sprintf("* %d EXISTS", len(ss.view)))
	}
	// Flags changed elsewhere, as a server reports them while idling.
	if ss.seenMod == nil {
		ss.seenMod = map[uint64]uint64{}
	}
	for i, id := range ss.view {
		m := s.byIDLocked(id)
		if prev, ok := ss.seenMod[id]; ok && m.ModSeq > prev {
			lines = append(lines, fmt.Sprintf("* %d FETCH (UID %d FLAGS (%s))", i+1, ss.sel.uids[id], strings.Join(sortedKeys(m.Flags), " ")))
		}
		ss.seenMod[id] = m.ModSeq
	}
	s.mu.Unlock()
	for _, l := range lines {
		ss.send("%s", l)
	}
}

func (ss *session) append(c command) {
	s := ss.s
	name := str(c.args[0])
	var raw []byte
	var flags []string
	for _, a := range c.args[1:] {
		switch v := a.(type) {
		case []byte:
			raw = v
		case []any:
			for _, f := range v {
				flags = append(flags, str(f))
			}
		}
	}
	s.mu.Lock()
	f := s.folderLocked(name)
	if f == nil || f.noselect {
		s.mu.Unlock()
		ss.send("%s NO [TRYCREATE] no such mailbox", c.tag)
		return
	}
	m := s.newMsgLocked(raw, time.Now())
	for _, fl := range flags {
		m.Flags[fl] = true
	}
	if s.opt.Gmail {
		if l := s.ownLabel(f); l != "" {
			m.Labels[l] = true
		}
	} else {
		m.Labels[f.name] = true
	}
	s.refreshLocked(f)
	uid := f.uids[m.ID]
	s.changedLocked()
	s.mu.Unlock()
	ss.send("%s OK [APPENDUID %d %d] APPEND done", c.tag, f.uidValidity, uid)
}

// resolveSet turns a sequence set into message ids in the selected folder.
func (ss *session) resolveSet(set string, uid bool) []uint64 {
	s := ss.s
	f := ss.sel
	max := uint32(0)
	if uid {
		for _, id := range ss.view {
			if u := f.uids[id]; u > max {
				max = u
			}
		}
	} else {
		max = uint32(len(ss.view))
	}
	_ = s
	in := func(n uint32) bool {
		for _, part := range strings.Split(set, ",") {
			lo, hi, isRange := strings.Cut(part, ":")
			parse := func(x string) uint32 {
				if x == "*" {
					return max
				}
				v, _ := strconv.ParseUint(x, 10, 32)
				return uint32(v)
			}
			a := parse(lo)
			b := a
			if isRange {
				b = parse(hi)
			}
			if a > b {
				a, b = b, a
			}
			if n >= a && n <= b {
				return true
			}
		}
		return false
	}
	var out []uint64
	for i, id := range ss.view {
		n := uint32(i + 1)
		if uid {
			n = f.uids[id]
			if n == 0 {
				continue // left the folder; this client has not been told yet
			}
		}
		if in(n) {
			out = append(out, id)
		}
	}
	return out
}

func (ss *session) seqOf(id uint64) int {
	for i, v := range ss.view {
		if v == id {
			return i + 1
		}
	}
	return 0
}

func (ss *session) fetch(c command) {
	if ss.sel == nil {
		ss.send("%s NO no mailbox selected", c.tag)
		return
	}
	s := ss.s
	isUID := strings.HasPrefix(c.name, "UID")
	var since uint64
	if len(c.args) > 2 {
		mods := list(c.args[2])
		for i := 0; i+1 < len(mods); i += 2 {
			if strings.EqualFold(str(mods[i]), "CHANGEDSINCE") {
				since, _ = strconv.ParseUint(str(mods[i+1]), 10, 64)
			}
		}
	}
	var items []string
	for _, it := range list(c.args[1]) {
		items = append(items, strings.ToUpper(str(it)))
	}
	if isUID {
		items = append([]string{"UID"}, items...)
	}
	if since > 0 {
		items = append(items, "MODSEQ")
	}

	s.mu.Lock()
	s.refreshLocked(ss.sel)
	ids := ss.resolveSet(str(c.args[0]), isUID)
	var out [][]byte
	for _, id := range ids {
		m := s.byIDLocked(id)
		if m == nil || (since > 0 && m.ModSeq <= since) {
			continue
		}
		// Each item is text, or text then a literal; joined with spaces.
		type part struct {
			text string
			lit  []byte
		}
		var parts []part
		add := func(t string) { parts = append(parts, part{text: t}) }
		seen := map[string]bool{}
		for _, it := range items {
			if seen[it] {
				continue
			}
			seen[it] = true
			switch {
			case it == "UID":
				add(fmt.Sprintf("UID %d", ss.sel.uids[id]))
			case it == "FLAGS":
				add("FLAGS (" + strings.Join(sortedKeys(m.Flags), " ") + ")")
			case it == "INTERNALDATE":
				add(`INTERNALDATE "` + m.Date.Format("02-Jan-2006 15:04:05 -0700") + `"`)
			case it == "RFC822.SIZE":
				add(fmt.Sprintf("RFC822.SIZE %d", len(m.Raw)))
			case it == "MODSEQ":
				add(fmt.Sprintf("MODSEQ (%d)", m.ModSeq))
			case it == "X-GM-MSGID" && s.opt.Gmail:
				add(fmt.Sprintf("X-GM-MSGID %d", m.ID))
			case it == "X-GM-THRID" && s.opt.Gmail:
				add(fmt.Sprintf("X-GM-THRID %d", m.ThrID))
			case it == "X-GM-LABELS" && s.opt.Gmail:
				own := s.ownLabel(ss.sel)
				var ls []string
				for _, l := range sortedKeys(m.Labels) {
					if l == own || l == Trash || l == Spam {
						continue
					}
					if strings.HasPrefix(l, `\`) {
						ls = append(ls, l)
					} else {
						enc, _ := utf7.Encoding.NewEncoder().String(l)
						ls = append(ls, strconv.Quote(enc))
					}
				}
				add("X-GM-LABELS (" + strings.Join(ls, " ") + ")")
			case strings.Contains(it, "[HEADER.FIELDS"):
				// Only the fields asked for, as a server returns them.
				open := strings.Index(it, "(")
				close := strings.Index(it, ")")
				want := map[string]bool{}
				for _, f := range strings.Fields(it[open+1 : close]) {
					want[strings.ToLower(f)] = true
				}
				var hdr strings.Builder
				for _, line := range strings.Split(string(m.Raw), "\r\n") {
					if line == "" {
						break
					}
					if k, _, ok := strings.Cut(line, ":"); ok && want[strings.ToLower(k)] {
						hdr.WriteString(line + "\r\n")
					}
				}
				hdr.WriteString("\r\n")
				parts = append(parts, part{text: "BODY[HEADER.FIELDS (" + it[open+1:close] + ")]", lit: []byte(hdr.String())})
			case it == "BODY[]" || it == "BODY.PEEK[]" || it == "RFC822":
				parts = append(parts, part{text: "BODY[]", lit: m.Raw})
				if it != "BODY.PEEK[]" && !ss.readOnly && !m.Flags[`\Seen`] {
					m.Flags[`\Seen`] = true
					s.modseq++
					m.ModSeq = s.modseq
				}
			}
		}
		b := []byte(fmt.Sprintf("* %d FETCH (", ss.seqOf(id)))
		for i, p := range parts {
			if i > 0 {
				b = append(b, ' ')
			}
			b = append(b, p.text...)
			if p.lit != nil {
				b = append(b, fmt.Sprintf(" {%d}\r\n", len(p.lit))...)
				b = append(b, p.lit...)
			}
		}
		b = append(b, ")\r\n"...)
		out = append(out, b)
	}
	s.mu.Unlock()
	for _, b := range out {
		ss.sendRaw(b)
	}
	ss.send("%s OK FETCH done", c.tag)
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (ss *session) search(c command) {
	if ss.sel == nil {
		ss.send("%s NO no mailbox selected", c.tag)
		return
	}
	s := ss.s
	isUID := strings.HasPrefix(c.name, "UID")
	s.mu.Lock()
	s.refreshLocked(ss.sel)
	ids := ss.view
	// SEARCH ALL, or SEARCH UID <set>; nothing else is used.
	if len(c.args) >= 2 && strings.EqualFold(str(c.args[0]), "UID") {
		ids = ss.resolveSet(str(c.args[1]), true)
	}
	var nums []string
	for _, id := range ids {
		if isUID {
			if u := ss.sel.uids[id]; u > 0 {
				nums = append(nums, strconv.Itoa(int(u)))
			}
		} else {
			nums = append(nums, strconv.Itoa(ss.seqOf(id)))
		}
	}
	s.mu.Unlock()
	ss.send("* SEARCH %s", strings.Join(nums, " "))
	ss.send("%s OK SEARCH done", c.tag)
}

func (ss *session) store(c command) {
	if ss.sel == nil || ss.readOnly {
		ss.send("%s NO not allowed", c.tag)
		return
	}
	s := ss.s
	isUID := strings.HasPrefix(c.name, "UID")
	item := strings.ToUpper(str(c.args[1]))
	var values []string
	for _, v := range list(c.args[2]) {
		values = append(values, str(v))
	}
	silent := strings.HasSuffix(item, ".SILENT")
	item = strings.TrimSuffix(item, ".SILENT")
	op := byte('=')
	if item[0] == '+' || item[0] == '-' {
		op = item[0]
		item = item[1:]
	}

	s.mu.Lock()
	s.refreshLocked(ss.sel)
	ids := ss.resolveSet(str(c.args[0]), isUID)
	var out []string
	for _, id := range ids {
		m := s.byIDLocked(id)
		target := m.Flags
		if item == "X-GM-LABELS" {
			if !s.opt.Gmail {
				s.mu.Unlock()
				ss.send("%s BAD no labels here", c.tag)
				return
			}
			target = m.Labels
		}
		if op == '=' {
			for k := range target {
				delete(target, k)
			}
		}
		for _, v := range values {
			if item == "X-GM-LABELS" && !strings.HasPrefix(v, `\`) {
				// Gmail takes a label's name in modified UTF-7, like a
				// folder's, and keeps it decoded.
				if d, err := utf7.Encoding.NewDecoder().String(v); err == nil {
					v = d
				}
			}
			if op == '-' {
				delete(target, v)
			} else {
				target[v] = true
				if item == "X-GM-LABELS" {
					s.ensureLabelFolderLocked(v)
				}
			}
		}
		s.modseq++
		m.ModSeq = s.modseq
		if !silent && item == "FLAGS" {
			out = append(out, fmt.Sprintf("* %d FETCH (UID %d FLAGS (%s))", ss.seqOf(id), ss.sel.uids[id], strings.Join(sortedKeys(m.Flags), " ")))
		}
	}
	s.changedLocked()
	s.mu.Unlock()
	for _, l := range out {
		ss.send("%s", l)
	}
	ss.send("%s OK STORE done", c.tag)
}

// move implements MOVE and COPY. In Gmail mode moving into Trash or Spam
// bins the message, moving out of them restores it, and moving between
// labels swaps the label; COPY adds a label. Otherwise a message changes
// folder, and COPY makes a new message.
func (ss *session) move(c command) {
	if ss.sel == nil || ss.readOnly {
		ss.send("%s NO not allowed", c.tag)
		return
	}
	s := ss.s
	isUID := strings.HasPrefix(c.name, "UID")
	isMove := strings.HasSuffix(c.name, "MOVE")
	if isMove && s.opt.NoMove {
		ss.send("%s BAD MOVE not supported", c.tag)
		return
	}
	s.mu.Lock()
	s.refreshLocked(ss.sel)
	dest := s.folderLocked(str(c.args[1]))
	if dest == nil || dest.noselect {
		s.mu.Unlock()
		ss.send("%s NO [TRYCREATE] no such mailbox", c.tag)
		return
	}
	ids := ss.resolveSet(str(c.args[0]), isUID)
	for _, id := range ids {
		m := s.byIDLocked(id)
		if !s.opt.Gmail {
			if isMove {
				m.Labels = map[string]bool{dest.name: true}
			} else {
				cp := s.newMsgLocked(m.Raw, m.Date)
				for k, v := range m.Flags {
					cp.Flags[k] = v
				}
				cp.Labels[dest.name] = true
			}
		} else {
			src := ss.sel
			switch dest.special {
			case `\Trash`:
				m.Labels[Trash] = true
			case `\Junk`:
				m.Labels[Spam] = true
			case `\All`:
				// Moving to All Mail removes the source label: Gmail's archive.
			default:
				if l := s.ownLabel(dest); l != "" {
					m.Labels[l] = true
				}
			}
			if isMove {
				switch src.special {
				case `\Trash`:
					delete(m.Labels, Trash)
				case `\Junk`:
					delete(m.Labels, Spam)
				default:
					if l := s.ownLabel(src); l != "" {
						delete(m.Labels, l)
					}
				}
			}
		}
		s.modseq++
		m.ModSeq = s.modseq
	}
	s.changedLocked()
	s.mu.Unlock()
	if isMove {
		ss.flushUpdates()
	}
	ss.send("%s OK %s done", c.tag, c.name)
}

func (ss *session) expunge(c command) {
	if ss.sel == nil || ss.readOnly {
		ss.send("%s NO not allowed", c.tag)
		return
	}
	s := ss.s
	s.mu.Lock()
	only := map[uint64]bool(nil)
	if c.name == "UID EXPUNGE" && len(c.args) > 0 {
		// RFC 4315: only these UIDs, never another client's \Deleted mail.
		only = map[uint64]bool{}
		s.refreshLocked(ss.sel)
		for _, id := range ss.resolveSet(str(c.args[0]), true) {
			only[id] = true
		}
	}
	for _, id := range s.orderLocked(ss.sel) {
		m := s.byIDLocked(id)
		if m.Flags[`\Deleted`] && (only == nil || only[id]) {
			m.gone = true
		}
	}
	s.changedLocked()
	s.mu.Unlock()
	ss.flushUpdates()
	ss.send("%s OK EXPUNGE done", c.tag)
}

func selfSigned() (tls.Certificate, *x509.CertPool, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool, nil
}
