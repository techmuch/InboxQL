package sync

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/textproto"
	"sort"
	"strconv"
	"strings"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/responses"
	"github.com/emersion/go-imap/utf7"
	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
)

// # The mirror
//
// A full copy of an account, kept current both ways. One Session is one
// connection; the worker (worker.go) holds one per account and runs passes on
// it, and `iql account sync` runs a single pass to completion.
//
// **Gmail** is a pile of messages with labels, which its IMAP shows as one
// folder per label. Syncing every folder would download each message once per
// label, so a Gmail account is read through All Mail, plus Spam and Trash,
// which All Mail leaves out. Gmail's own extension gives each message a
// permanent id and its labels, and the id is the identity: a message moved to
// Trash on a phone turns up in Trash under a new UID and is still the same row.
//
// **Other servers** have folders, each of which is a label here. Every folder
// is read; a message in two of them is one row with two labels.
//
// **What is fetched.** Metadata first — UID, flags, Gmail's ids and labels,
// the Message-ID header. A message already stored, by Gmail id or by
// Message-ID, is only linked to its server copy and brought into agreement
// with it; its body is never downloaded again. That is what links an Apple
// Mail import to the Gmail account it came from without fetching it twice.
// Bodies are fetched for the rest.
//
// **In what order.** New mail first on every pass, then history, newest
// first, in batches. A folder remembers how far history has got (LowUID), so
// a pass interrupted halfway resumes rather than starts again.

// Batch sizes. Metadata is small; a body batch is bounded so a run of large
// messages does not hold hundreds of megabytes at once.
const (
	metaBatch    = 500
	bodyBatch    = 25
	historyBatch = 500
)

// Folder is one folder the mirror reads.
type Folder struct {
	Name    string
	Special string // the RFC 6154 attribute: \All \Trash \Junk \Sent ...
	// Label is the label being in this folder implies. Empty for All Mail,
	// whose messages' labels come from Gmail itself.
	Label string
}

// Report is what a pass did.
type Report struct {
	New        int // messages stored for the first time
	Linked     int // stored messages matched to their server copy
	Changed    int // messages whose labels or flags the server changed
	Removed    int // server copies that went away
	Orphaned   int // messages the server no longer has anywhere
	Pushed     int // local changes applied on the server
	PushFailed int
}

func (r *Report) add(o Report) {
	r.New += o.New
	r.Linked += o.Linked
	r.Changed += o.Changed
	r.Removed += o.Removed
	r.Orphaned += o.Orphaned
	r.Pushed += o.Pushed
	r.PushFailed += o.PushFailed
}

// Any reports whether the pass changed anything a window should show.
func (r Report) Any() bool {
	return r.New+r.Linked+r.Changed+r.Removed+r.Orphaned+r.Pushed > 0
}

// Session is one connection to one account's server.
type Session struct {
	sm        *SyncManager
	acc       *account.Account
	c         *client.Client
	gmail     bool
	condstore bool
	canMove   bool
	folders   []Folder
	// history caches each folder's UIDs below its LowUID, read once per
	// session rather than once per batch.
	history map[string][]uint32
	// removed collects messages that left a folder this pass; whether they
	// left the server is only known once every folder has been read.
	removed []string
	// selected is the folder currently open, and whether read-write.
	selected   string
	selectedRW bool
}

// Open connects and learns what the server can do and which folders to read.
func (sm *SyncManager) Open(acc *account.Account, updates chan<- client.Update) (*Session, error) {
	c, err := ConnectIMAP(acc)
	if err != nil {
		return nil, err
	}
	if updates != nil {
		c.Updates = updates
	}
	s := &Session{sm: sm, acc: acc, c: c, history: map[string][]uint32{}}
	caps, err := c.Capability()
	if err != nil {
		c.Logout()
		return nil, err
	}
	s.gmail = caps["X-GM-EXT-1"]
	s.condstore = caps["CONDSTORE"] || caps["QRESYNC"]
	s.canMove = caps["MOVE"]
	if err := s.listFolders(); err != nil {
		c.Logout()
		return nil, err
	}
	return s, nil
}

// Close logs out.
func (s *Session) Close() {
	if s.c != nil {
		s.c.Logout()
	}
}

// Gmail reports whether the server is Gmail.
func (s *Session) Gmail() bool { return s.gmail }

// Folders returns the folders this session reads.
func (s *Session) Folders() []Folder { return s.folders }

// IdleFolder is the folder to watch for new mail: All Mail on Gmail, which
// every message lands in, and INBOX elsewhere.
func (s *Session) IdleFolder() string {
	for _, f := range s.folders {
		if s.gmail && f.Special == `\All` {
			return f.Name
		}
	}
	return "INBOX"
}

func (s *Session) listFolders() error {
	ch := make(chan *imap.MailboxInfo, 50)
	done := make(chan error, 1)
	go func() { done <- s.c.List("", "*", ch) }()
	var all []Folder
	for mb := range ch {
		special := ""
		noselect := false
		for _, a := range mb.Attributes {
			switch strings.ToLower(a) {
			case `\noselect`, `\nonexistent`:
				noselect = true
			case `\all`, `\trash`, `\junk`, `\sent`, `\drafts`, `\archive`, `\flagged`, `\important`:
				special = a
			}
		}
		if noselect {
			continue
		}
		all = append(all, Folder{Name: mb.Name, Special: special})
	}
	if err := <-done; err != nil {
		return fmt.Errorf("listing folders: %w", err)
	}

	s.folders = nil
	if s.gmail {
		// All Mail, then Spam and Trash: the order matters, because a
		// message moved to Trash is found there before its All Mail copy is
		// found missing.
		for _, want := range []string{`\All`, `\Junk`, `\Trash`} {
			for _, f := range all {
				if strings.EqualFold(f.Special, want) {
					f.Label, _ = store.LabelForMailbox(f.Name, f.Special)
					s.folders = append(s.folders, f)
				}
			}
		}
		if len(s.folders) == 0 {
			return fmt.Errorf("this Gmail account shows no All Mail folder over IMAP; turn on \"Show in IMAP\" for All Mail in Gmail's settings")
		}
		return nil
	}
	for _, f := range all {
		// A virtual folder of everything, or of everything starred, would
		// download each message twice for nothing.
		if strings.EqualFold(f.Special, `\All`) || strings.EqualFold(f.Special, `\Flagged`) {
			continue
		}
		f.Label, _ = store.LabelForMailbox(f.Name, f.Special)
		s.folders = append(s.folders, f)
	}
	// Binned folders last, for the same reason as on Gmail.
	sort.SliceStable(s.folders, func(i, j int) bool {
		return !binned(s.folders[i]) && binned(s.folders[j])
	})
	return nil
}

func binned(f Folder) bool { return f.Label == store.LabelTrash || f.Label == store.LabelSpam }

// FullPass reads every folder: new mail, changes, and what went away.
func (s *Session) FullPass() (Report, error) {
	var r Report
	s.removed = nil
	for _, f := range s.folders {
		fr, err := s.syncFolder(f, true)
		r.add(fr)
		if err != nil {
			return r, fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	if len(s.removed) > 0 {
		n, err := store.Orphan(s.removed)
		if err != nil {
			return r, err
		}
		r.Orphaned = n
	}
	return r, nil
}

// QuickPass reads new mail and changes, without looking for what went away.
func (s *Session) QuickPass() (Report, error) {
	var r Report
	for _, f := range s.folders {
		fr, err := s.syncFolder(f, false)
		r.add(fr)
		if err != nil {
			return r, fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return r, nil
}

// HistoryDone reports whether every folder's history has been read.
func (s *Session) HistoryDone() bool {
	for _, f := range s.folders {
		st, err := store.GetMailboxState(s.acc.ID, f.Name)
		if err != nil || !st.BackfillDone {
			return false
		}
	}
	return true
}

// HistoryStep reads one batch of history from the first folder that has
// some left. It returns false when there is none left anywhere.
func (s *Session) HistoryStep() (Report, bool, error) {
	for _, f := range s.folders {
		st, err := store.GetMailboxState(s.acc.ID, f.Name)
		if err != nil {
			return Report{}, false, err
		}
		if st.BackfillDone || st.UIDValidity == 0 {
			continue
		}
		r, err := s.historyBatch(f, st)
		return r, true, err
	}
	return Report{}, false, nil
}

func (s *Session) examine(name string) (*imap.MailboxStatus, error) {
	mb, err := s.c.Select(name, true)
	if err == nil {
		s.selected, s.selectedRW = name, false
	}
	return mb, err
}

func (s *Session) syncFolder(f Folder, full bool) (Report, error) {
	var r Report
	items := []imap.StatusItem{imap.StatusMessages, imap.StatusUidNext, imap.StatusUidValidity}
	if s.condstore {
		items = append(items, imap.StatusItem("HIGHESTMODSEQ"))
	}
	status, err := s.c.Status(f.Name, items)
	if err != nil {
		return r, err
	}
	var highest uint64
	if v, ok := status.Items["HIGHESTMODSEQ"]; ok {
		highest = parseUint64(v)
	}

	st, err := store.GetMailboxState(s.acc.ID, f.Name)
	if err != nil {
		return r, err
	}
	renumbered := st.UIDValidity != 0 && st.UIDValidity != status.UidValidity
	// A saved position past the next UID is the same thing, said by a
	// server that kept its UIDVALIDITY — or by the sync before v43, which
	// kept no UIDVALIDITY at all.
	behind := status.UidNext > 0 && st.LastUID >= status.UidNext
	if renumbered || behind {
		log.Printf("Mailbox %s: the server renumbered it; reading it again", f.Name)
		if err := store.ForgetFolder(s.acc.ID, f.Name); err != nil {
			return r, err
		}
		st = &store.MailboxState{AccountID: s.acc.ID, Name: f.Name}
		delete(s.history, f.Name)
	}
	first := st.UIDValidity == 0 || (st.LowUID == 0 && !st.BackfillDone)
	if first {
		// Everything already there is history, read newest first by the
		// history steps; everything from here on is new mail.
		st.UIDValidity = status.UidValidity
		st.LastUID = sub1(status.UidNext)
		st.LowUID = status.UidNext
		st.BackfillDone = status.Messages == 0
		st.LastModSeq = 0
	}
	st.ServerCount = int(status.Messages)

	newMail := status.UidNext > 0 && sub1(status.UidNext) > st.LastUID
	changes := !first && st.LastModSeq > 0 && highest > st.LastModSeq
	flagScan := !first && full && !s.condstore
	deletions := !first && full
	if !newMail && !changes && !flagScan && !deletions {
		if first || highest != st.LastModSeq {
			st.LastModSeq = highest
			return r, store.SaveMailboxState(st)
		}
		return r, nil
	}

	if _, err := s.examine(f.Name); err != nil {
		return r, err
	}

	if changes {
		cr, err := s.fetchChanges(f, st.LastModSeq)
		r.add(cr)
		if err != nil {
			return r, err
		}
	} else if flagScan && st.LastUID > 0 {
		cr, err := s.flagScan(f, st)
		r.add(cr)
		if err != nil {
			return r, err
		}
	}

	if newMail {
		uids, err := s.uidsIn(st.LastUID+1, 0)
		if err != nil {
			return r, err
		}
		var fresh []uint32
		for _, u := range uids {
			if u > st.LastUID {
				fresh = append(fresh, u)
			}
		}
		for start := 0; start < len(fresh); start += metaBatch {
			batch := fresh[start:min(start+metaBatch, len(fresh))]
			br, err := s.fetchUIDs(f, st.UIDValidity, batch)
			r.add(br)
			if err != nil {
				return r, err
			}
			st.LastUID = batch[len(batch)-1]
			if err := store.SaveMailboxState(st); err != nil {
				return r, err
			}
		}
		// Whatever the search found, nothing below UIDNEXT is new any more.
		if u := sub1(status.UidNext); u > st.LastUID {
			st.LastUID = u
		}
	}

	if deletions {
		dr, err := s.findRemoved(f)
		r.add(dr)
		if err != nil {
			return r, err
		}
	}

	if s.condstore && highest > 0 {
		st.LastModSeq = highest
	}
	return r, store.SaveMailboxState(st)
}

// historyBatch reads the newest batch of history below a folder's LowUID.
func (s *Session) historyBatch(f Folder, st *store.MailboxState) (Report, error) {
	var r Report
	if _, err := s.examine(f.Name); err != nil {
		return r, err
	}
	hist, ok := s.history[f.Name]
	if !ok {
		if st.LowUID <= 1 {
			st.BackfillDone = true
			return r, store.SaveMailboxState(st)
		}
		uids, err := s.uidsIn(1, st.LowUID-1)
		if err != nil {
			return r, err
		}
		hist = uids
	}
	// Only what is still below the floor; the cache may predate a batch.
	for len(hist) > 0 && hist[len(hist)-1] >= st.LowUID {
		hist = hist[:len(hist)-1]
	}
	if len(hist) == 0 {
		st.BackfillDone = true
		delete(s.history, f.Name)
		return r, store.SaveMailboxState(st)
	}
	n := min(historyBatch, len(hist))
	batch := append([]uint32(nil), hist[len(hist)-n:]...)
	// Newest first, so the most recent history appears first.
	sort.Slice(batch, func(i, j int) bool { return batch[i] > batch[j] })
	br, err := s.fetchUIDs(f, st.UIDValidity, batch)
	r.add(br)
	if err != nil {
		return r, err
	}
	s.history[f.Name] = hist[:len(hist)-n]
	st.LowUID = batch[len(batch)-1]
	if len(s.history[f.Name]) == 0 {
		st.BackfillDone = true
	}
	return r, store.SaveMailboxState(st)
}

// uidsIn returns the UIDs between lo and hi (0 meaning "the highest"),
// ascending.
func (s *Session) uidsIn(lo, hi uint32) ([]uint32, error) {
	set := new(imap.SeqSet)
	set.AddRange(lo, hi)
	crit := imap.NewSearchCriteria()
	crit.Uid = set
	uids, err := s.c.UidSearch(crit)
	if err != nil {
		return nil, err
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	var out []uint32
	for _, u := range uids {
		if u >= lo && (hi == 0 || u <= hi) {
			out = append(out, u)
		}
	}
	return out, nil
}

// fetchUIDs brings messages at these UIDs into the mirror: links what is
// already stored, downloads what is not.
func (s *Session) fetchUIDs(f Folder, uidValidity uint32, uids []uint32) (Report, error) {
	var r Report
	if len(uids) == 0 {
		return r, nil
	}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchFlags, imap.FetchInternalDate, imap.FetchRFC822Size,
		imap.FetchItem("BODY.PEEK[HEADER.FIELDS (MESSAGE-ID)]")}
	items = append(items, s.gmailItems()...)
	metas, err := s.fetch(uids, items)
	if err != nil {
		return r, err
	}
	var unknown []uint32
	for _, m := range metas {
		gm := gmailOf(m)
		targets, err := store.SyncTargets(s.acc.ID, gm.msgID, headerMessageID(m))
		if err != nil {
			return r, err
		}
		if len(targets) == 0 {
			unknown = append(unknown, m.Uid)
			continue
		}
		if err := s.link(f, uidValidity, m, targets); err != nil {
			return r, err
		}
		r.Linked++
	}

	sort.Slice(unknown, func(i, j int) bool { return unknown[i] > unknown[j] })
	for start := 0; start < len(unknown); start += bodyBatch {
		batch := unknown[start:min(start+bodyBatch, len(unknown))]
		items := []imap.FetchItem{imap.FetchUid, imap.FetchFlags, imap.FetchInternalDate, imap.FetchRFC822Size,
			imap.FetchEnvelope, imap.FetchItem("BODY.PEEK[]")}
		items = append(items, s.gmailItems()...)
		msgs, err := s.fetch(batch, items)
		if err != nil {
			return r, err
		}
		for _, m := range msgs {
			stored, err := s.store(f, uidValidity, m)
			if err != nil {
				log.Printf("Mailbox %s: message %d: %v", f.Name, m.Uid, err)
				continue
			}
			if stored {
				r.New++
			} else {
				r.Linked++
			}
		}
	}
	return r, nil
}

func (s *Session) gmailItems() []imap.FetchItem {
	if !s.gmail {
		return nil
	}
	return []imap.FetchItem{"X-GM-MSGID", "X-GM-THRID", "X-GM-LABELS"}
}

// fetch runs a UID FETCH and collects the results.
func (s *Session) fetch(uids []uint32, items []imap.FetchItem) ([]*imap.Message, error) {
	set := new(imap.SeqSet)
	set.AddNum(uids...)
	ch := make(chan *imap.Message, 64)
	done := make(chan error, 1)
	go func() { done <- s.c.UidFetch(set, items, ch) }()
	want := map[uint32]bool{}
	for _, u := range uids {
		want[u] = true
	}
	var out []*imap.Message
	for m := range ch {
		if want[m.Uid] {
			out = append(out, m)
		}
	}
	return out, <-done
}

// serverState is what this folder's copy of a message says its labels and
// flags are.
func (s *Session) serverState(f Folder, m *imap.Message) store.ServerState {
	st := store.ServerState{Flags: serverFlags(m.Flags)}
	if s.gmail {
		st.Replace = true
		st.Labels = gmailLabels(m)
		switch f.Label {
		case store.LabelTrash, store.LabelSpam:
			st.Labels = append(st.Labels, f.Label)
		}
		return st
	}
	if f.Label != "" {
		st.Labels = []string{f.Label}
	}
	return st
}

// serverFlags keeps the flags that mean something here. \Recent is the
// server's business, and \Deleted is a message on its way out of a folder.
func serverFlags(flags []string) []string {
	out := []string{}
	for _, f := range flags {
		switch strings.ToLower(f) {
		case `\recent`, `\deleted`:
			continue
		}
		out = append(out, f)
	}
	return out
}

func (s *Session) link(f Folder, uidValidity uint32, m *imap.Message, targets []string) error {
	gm := gmailOf(m)
	loc := store.Location{AccountID: s.acc.ID, Mailbox: f.Name, UIDValidity: uidValidity, UID: m.Uid, Label: f.Label}
	if err := store.LinkLocation(targets, loc, gm.msgID, gm.thrID); err != nil {
		return err
	}
	return store.ApplyServerState(targets, s.serverState(f, m))
}

// store saves a downloaded message, or links it when its content turns out to
// be stored already.
func (s *Session) store(f Folder, uidValidity uint32, m *imap.Message) (bool, error) {
	parsed, raw, err := parseIMAPMessage(s.acc.ID, m)
	if err != nil {
		return false, err
	}
	gm := gmailOf(m)
	st := s.serverState(f, m)
	parsed.Mailbox = f.Name
	parsed.Labels = st.Labels
	if parsed.Labels == nil {
		parsed.Labels = []string{}
	}
	parsed.LabelSource = store.LabelSourceServer
	parsed.Flags = st.Flags
	parsed.GmMsgID, parsed.GmThrID = gm.msgID, gm.thrID

	inserted, err := store.SaveMessageInserted(parsed)
	if err != nil {
		return false, err
	}
	if !inserted {
		// The same content is stored already under another id.
		targets, err := store.SyncTargets(s.acc.ID, gm.msgID, parsed.MessageID)
		if err != nil || len(targets) == 0 {
			return false, err
		}
		return false, s.link(f, uidValidity, m, targets)
	}
	loc := store.Location{AccountID: s.acc.ID, Mailbox: f.Name, UIDValidity: uidValidity, UID: m.Uid, Label: f.Label}
	if err := store.LinkLocation([]string{parsed.ID}, loc, 0, 0); err != nil {
		return true, err
	}
	if s.sm.Blobs != nil {
		if _, err := store.StoreAttachments(parsed.ID, raw, s.sm.Blobs, s.sm.MaxAttachmentBytes); err != nil {
			log.Printf("storing attachments for %s: %v", parsed.ID, err)
		}
	}
	return true, nil
}

// fetchChanges asks what changed since a MODSEQ and applies it to the
// messages already stored.
func (s *Session) fetchChanges(f Folder, since uint64) (Report, error) {
	var r Report
	known, err := store.FolderUIDs(s.acc.ID, f.Name)
	if err != nil || len(known) == 0 {
		return r, err
	}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchFlags}
	if s.gmail {
		items = append(items, "X-GM-LABELS")
	}
	set := new(imap.SeqSet)
	set.AddRange(1, 0)
	ch := make(chan *imap.Message, 64)
	done := make(chan error, 1)
	go func() {
		cmd := &uidFetchChangedSince{set: set, items: items, since: since}
		status, err := s.c.Execute(cmd, &responses.Fetch{Messages: ch, SeqSet: set, Uid: true})
		close(ch)
		if err == nil {
			err = status.Err()
		}
		done <- err
	}()
	for m := range ch {
		ids := known[m.Uid]
		if len(ids) == 0 {
			continue // not read yet; history will bring it as it is now
		}
		if err := store.ApplyServerState(ids, s.serverState(f, m)); err != nil {
			<-done
			return r, err
		}
		r.Changed++
	}
	return r, <-done
}

// flagScan reads every stored message's flags, for a server that cannot say
// what changed.
func (s *Session) flagScan(f Folder, st *store.MailboxState) (Report, error) {
	var r Report
	known, err := store.FolderUIDs(s.acc.ID, f.Name)
	if err != nil || len(known) == 0 {
		return r, err
	}
	set := new(imap.SeqSet)
	set.AddRange(1, 0)
	ch := make(chan *imap.Message, 64)
	done := make(chan error, 1)
	go func() { done <- s.c.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchFlags}, ch) }()
	for m := range ch {
		if ids := known[m.Uid]; len(ids) > 0 {
			if err := store.ApplyServerState(ids, store.ServerState{Flags: serverFlags(m.Flags)}); err != nil {
				<-done
				return r, err
			}
		}
	}
	return r, <-done
}

// findRemoved compares the folder's UIDs with the stored ones and forgets the
// server copies that are gone.
func (s *Session) findRemoved(f Folder) (Report, error) {
	var r Report
	known, err := store.FolderUIDs(s.acc.ID, f.Name)
	if err != nil || len(known) == 0 {
		return r, err
	}
	present, err := s.uidsIn(1, 0)
	if err != nil {
		return r, err
	}
	here := make(map[uint32]bool, len(present))
	for _, u := range present {
		here[u] = true
	}
	var gone []uint32
	for u := range known {
		if !here[u] {
			gone = append(gone, u)
		}
	}
	if len(gone) == 0 {
		return r, nil
	}
	affected, err := store.RemoveLocations(s.acc.ID, f.Name, gone)
	if err != nil {
		return r, err
	}
	r.Removed = len(gone)
	if !s.gmail && f.Label != "" {
		if err := store.RemoveFolderLabels(affected, []string{f.Label}); err != nil {
			return r, err
		}
	}
	s.removed = append(s.removed, affected...)
	return r, nil
}

// uidFetchChangedSince is UID FETCH with CONDSTORE's CHANGEDSINCE modifier,
// which go-imap's client does not have a method for.
type uidFetchChangedSince struct {
	set   *imap.SeqSet
	items []imap.FetchItem
	since uint64
}

func (c *uidFetchChangedSince) Command() *imap.Command {
	items := make([]interface{}, len(c.items))
	for i, it := range c.items {
		items[i] = imap.RawString(it)
	}
	return &imap.Command{
		Name: "UID",
		Arguments: []interface{}{
			imap.RawString("FETCH"), c.set, items,
			[]interface{}{imap.RawString("CHANGEDSINCE"), imap.RawString(strconv.FormatUint(c.since, 10))},
		},
	}
}

type gmailIDs struct{ msgID, thrID uint64 }

func gmailOf(m *imap.Message) gmailIDs {
	return gmailIDs{msgID: parseUint64(m.Items["X-GM-MSGID"]), thrID: parseUint64(m.Items["X-GM-THRID"])}
}

// gmailLabels reads X-GM-LABELS as stored labels. Gmail encodes a label's
// name as it does a folder's, in modified UTF-7.
func gmailLabels(m *imap.Message) []string {
	raw, ok := m.Items["X-GM-LABELS"].([]interface{})
	out := []string{}
	if !ok {
		return out
	}
	dec := utf7.Encoding.NewDecoder()
	for _, v := range raw {
		var name string
		switch x := v.(type) {
		case string:
			name = x
		case imap.RawString:
			name = string(x)
		default:
			continue
		}
		if !strings.HasPrefix(name, `\`) {
			if d, err := dec.String(name); err == nil {
				name = d
			}
		}
		if n := store.NormalizeLabel(name); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// headerMessageID reads the Message-ID out of a HEADER.FIELDS fetch.
func headerMessageID(m *imap.Message) string {
	for _, lit := range m.Body {
		if lit == nil {
			continue
		}
		b, _ := io.ReadAll(lit)
		r := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(b, "\r\n"...))))
		h, _ := r.ReadMIMEHeader()
		if id := strings.TrimSpace(h.Get("Message-Id")); id != "" {
			return id
		}
	}
	return ""
}

func parseUint64(v interface{}) uint64 {
	switch x := v.(type) {
	case string:
		n, _ := strconv.ParseUint(x, 10, 64)
		return n
	case imap.RawString:
		n, _ := strconv.ParseUint(string(x), 10, 64)
		return n
	case uint32:
		return uint64(x)
	case uint64:
		return x
	case int:
		return uint64(x)
	case []interface{}:
		if len(x) == 1 {
			return parseUint64(x[0])
		}
	}
	return 0
}

func sub1(n uint32) uint32 {
	if n == 0 {
		return 0
	}
	return n - 1
}

// RunToCompletion connects, runs a full pass, pushes queued changes and reads
// all history. It is what `iql account sync` does, so a script that calls it
// gets a complete mailbox when it returns.
func (sm *SyncManager) RunToCompletion(acc *account.Account, history bool, progress func(store.MirrorProgress)) (Report, error) {
	s, err := sm.Open(acc, nil)
	if err != nil {
		return Report{}, err
	}
	defer s.Close()
	var r Report
	pr, err := s.Push()
	r.add(pr)
	if err != nil {
		return r, err
	}
	fr, err := s.FullPass()
	r.add(fr)
	if err != nil {
		return r, err
	}
	for history {
		hr, more, err := s.HistoryStep()
		r.add(hr)
		if err != nil {
			return r, err
		}
		if progress != nil {
			if p, err := store.MirrorProgressFor(acc.ID); err == nil {
				progress(p)
			}
		}
		if !more {
			break
		}
	}
	return r, nil
}
