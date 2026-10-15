package sync

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/utf7"
	"github.com/user/inboxql/internal/store"
)

// # Changes made here, made there
//
// A change made in InboxQL — archive, trash, a label, read, starred — is
// applied locally at once and queued (store/pending.go). Push applies the
// queue to the server.
//
// It works from state, not from the queue's history: for each message with
// something queued, it looks at what the message is like here now, for the
// labels and flags the queue touched, and makes the server agree. Archiving
// and un-archiving before the push ran therefore sends nothing at all.
//
// **Gmail.** Labels are written with UID STORE ±X-GM-LABELS. Trash and Spam
// are folders there rather than labels, so binning a message is a UID MOVE
// into them, and restoring it a move back to the inbox.
//
// **Other servers.** A message lives in one folder, so the system labels pick
// the folder: Trash, then Spam, then INBOX, and otherwise the Archive folder.
// Changing them moves the message. A label of the user's own has no folder to
// be and stays here.
//
// **Never.** Nothing here deletes a message from a server. A move without the
// MOVE extension is a copy, then \Deleted and UID EXPUNGE of that one UID —
// the message exists in its new folder before it leaves the old one.

// Push applies queued changes. A failure on one message is recorded against
// its changes and the rest carry on; a failure of the connection stops.
func (s *Session) Push() (Report, error) {
	var r Report
	if s.gmail {
		for _, name := range store.LabelDeletes(s.acc.ID) {
			enc, _ := utf7.Encoding.NewEncoder().String(name)
			if err := s.c.Delete(enc); err != nil && !strings.Contains(strings.ToLower(err.Error()), "exist") {
				if isConnErr(err) {
					return r, err
				}
				log.Printf("Account %s: could not delete label %q on the server: %v", s.acc.ID, name, err)
				continue
			}
			store.LabelDeleted(s.acc.ID, name)
			r.Pushed++
		}
	}
	changes, err := store.PendingChanges(s.acc.ID, 500)
	if err != nil || len(changes) == 0 {
		return r, err
	}
	var order []string
	byMsg := map[string][]store.PendingChange{}
	for _, c := range changes {
		if _, seen := byMsg[c.MessageID]; !seen {
			order = append(order, c.MessageID)
		}
		byMsg[c.MessageID] = append(byMsg[c.MessageID], c)
	}
	historyDone := s.HistoryDone()
	for _, id := range order {
		cs := byMsg[id]
		err := s.pushMessage(id, cs, historyDone)
		switch {
		case err == nil:
			for _, c := range cs {
				store.CompletePendingChange(c.ID)
			}
			r.Pushed++
		case errors.Is(err, errNotYet):
			// Not on the server yet as far as this side knows; the next pass
			// will link it, and the change waits for that.
		case isConnErr(err):
			return r, err
		default:
			log.Printf("Account %s: could not apply a change to %s: %v", s.acc.ID, id, err)
			for _, c := range cs {
				store.FailPendingChange(c.ID, err.Error())
			}
			r.PushFailed++
		}
	}
	return r, nil
}

var errNotYet = errors.New("not linked to the server yet")

// touched lists what a set of changes is about.
func touched(cs []store.PendingChange) (labels, flags map[string]bool) {
	labels, flags = map[string]bool{}, map[string]bool{}
	for _, c := range cs {
		switch c.Op {
		case store.OpLabelAdd, store.OpLabelRemove:
			labels[c.Value] = true
		case store.OpFlagAdd, store.OpFlagRemove:
			flags[c.Value] = true
		}
	}
	return
}

func (s *Session) pushMessage(id string, cs []store.PendingChange, historyDone bool) error {
	m, err := store.ForPush(id)
	if err != nil {
		return err
	}
	if m == nil {
		return nil // deleted here; nothing to tell the server
	}
	if len(m.Locations) == 0 {
		if historyDone {
			// Read all of the server and never found it: an imported message
			// the server does not have. The change stays here.
			return nil
		}
		return errNotYet
	}
	labels, flags := touched(cs)
	has := map[string]bool{}
	for _, l := range m.Labels {
		has[strings.ToLower(l)] = true
	}
	hasFlag := func(f string) bool {
		for _, x := range m.Flags {
			if strings.EqualFold(x, f) {
				return true
			}
		}
		return false
	}

	if s.gmail {
		return s.pushGmail(m, labels, flags, has, hasFlag)
	}
	return s.pushFolders(m, labels, flags, has, hasFlag)
}

func (s *Session) pushGmail(m *store.MessageForPush, labels, flags map[string]bool,
	has map[string]bool, hasFlag func(string) bool) error {
	loc := m.Locations[0]
	inTrash := s.folderLabel(loc.Mailbox) == store.LabelTrash
	inSpam := s.folderLabel(loc.Mailbox) == store.LabelSpam

	// Plain labels and flags first, on the copy where it is now.
	var add, remove []interface{}
	for l := range labels {
		if l == store.LabelTrash || l == store.LabelSpam {
			continue
		}
		if l == store.LabelSent || l == store.LabelDrafts {
			continue // what Gmail decides, not something to set
		}
		v := gmailLabelArg(l)
		if has[strings.ToLower(l)] {
			add = append(add, v)
		} else {
			remove = append(remove, v)
		}
	}
	if len(add)+len(remove) > 0 || len(flags) > 0 {
		if err := s.selectRW(loc.Mailbox); err != nil {
			return err
		}
		if len(add) > 0 {
			if err := s.uidStore(loc.UID, "+X-GM-LABELS", add); err != nil {
				return err
			}
		}
		if len(remove) > 0 {
			if err := s.uidStore(loc.UID, "-X-GM-LABELS", remove); err != nil {
				return err
			}
		}
		if err := s.pushFlags(loc, flags, hasFlag); err != nil {
			return err
		}
	}

	// Then Trash and Spam, which are moves.
	wantTrash, wantSpam := has[store.LabelTrash], has[store.LabelSpam]
	switch {
	case labels[store.LabelTrash] && wantTrash && !inTrash:
		return s.moveTo(loc, s.folderFor(store.LabelTrash))
	case labels[store.LabelSpam] && wantSpam && !inSpam && !wantTrash:
		return s.moveTo(loc, s.folderFor(store.LabelSpam))
	case (labels[store.LabelTrash] && !wantTrash && inTrash) || (labels[store.LabelSpam] && !wantSpam && inSpam):
		// Out of the bin. Moving to INBOX is how Gmail restores, and every
		// way out of the bin here puts the message back in the inbox too.
		return s.moveTo(loc, "INBOX")
	}
	return nil
}

func (s *Session) pushFolders(m *store.MessageForPush, labels, flags map[string]bool,
	has map[string]bool, hasFlag func(string) bool) error {
	for _, loc := range m.Locations {
		if len(flags) > 0 {
			if err := s.selectRW(loc.Mailbox); err != nil {
				return err
			}
			if err := s.pushFlags(loc, flags, hasFlag); err != nil {
				return err
			}
		}
	}

	moves := labels[store.LabelInbox] || labels[store.LabelTrash] || labels[store.LabelSpam]
	if !moves {
		return nil
	}
	// The one folder the system labels say it belongs in.
	var target string
	switch {
	case has[store.LabelTrash]:
		target = s.folderFor(store.LabelTrash)
	case has[store.LabelSpam]:
		target = s.folderFor(store.LabelSpam)
	case has[store.LabelInbox]:
		target = "INBOX"
	default:
		var err error
		if target, err = s.archiveFolder(); err != nil {
			return err
		}
	}
	if target == "" {
		return fmt.Errorf("the server has no folder for this")
	}
	// Move the copy that is in a system folder; a copy filed under a label of
	// the user's own stays where it is.
	for _, loc := range m.Locations {
		switch loc.Label {
		case store.LabelInbox, store.LabelTrash, store.LabelSpam, "":
		default:
			continue
		}
		if strings.EqualFold(loc.Mailbox, target) {
			return nil
		}
		return s.moveTo(loc, target)
	}
	return nil
}

func (s *Session) pushFlags(loc store.Location, flags map[string]bool, hasFlag func(string) bool) error {
	var add, remove []interface{}
	for f := range flags {
		if hasFlag(f) {
			add = append(add, imap.RawString(f))
		} else {
			remove = append(remove, imap.RawString(f))
		}
	}
	if len(add) > 0 {
		if err := s.uidStore(loc.UID, imap.FormatFlagsOp(imap.AddFlags, true), add); err != nil {
			return err
		}
	}
	if len(remove) > 0 {
		if err := s.uidStore(loc.UID, imap.FormatFlagsOp(imap.RemoveFlags, true), remove); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) uidStore(uid uint32, item imap.StoreItem, values []interface{}) error {
	set := new(imap.SeqSet)
	set.AddNum(uid)
	return s.c.UidStore(set, item, values, nil)
}

func (s *Session) selectRW(name string) error {
	if s.selected == name && s.selectedRW {
		return nil
	}
	if _, err := s.c.Select(name, false); err != nil {
		return err
	}
	s.selected, s.selectedRW = name, true
	return nil
}

// moveTo moves one message, and forgets where it was: the next pass finds it
// in its new folder by its id.
func (s *Session) moveTo(loc store.Location, dest string) error {
	if err := s.selectRW(loc.Mailbox); err != nil {
		return err
	}
	set := new(imap.SeqSet)
	set.AddNum(loc.UID)
	if s.canMove {
		if err := s.c.UidMove(set, dest); err != nil {
			return err
		}
	} else {
		// Copy, then remove this one UID. The message is in the destination
		// before it leaves here.
		if err := s.c.UidCopy(set, dest); err != nil {
			return err
		}
		if err := s.uidStore(loc.UID, imap.FormatFlagsOp(imap.AddFlags, true), []interface{}{imap.RawString(imap.DeletedFlag)}); err != nil {
			return err
		}
		if err := s.uidExpunge(loc.UID); err != nil {
			return err
		}
	}
	_, err := store.RemoveLocations(s.acc.ID, loc.Mailbox, []uint32{loc.UID})
	return err
}

// uidExpunge expunges one UID (RFC 4315), never the whole folder: other
// clients' \Deleted messages are not ours to remove.
func (s *Session) uidExpunge(uid uint32) error {
	set := new(imap.SeqSet)
	set.AddNum(uid)
	status, err := s.c.Execute(&uidExpungeCmd{set: set}, nil)
	if err != nil {
		return err
	}
	return status.Err()
}

type uidExpungeCmd struct{ set *imap.SeqSet }

func (c *uidExpungeCmd) Command() *imap.Command {
	return &imap.Command{Name: "UID", Arguments: []interface{}{imap.RawString("EXPUNGE"), c.set}}
}

// folderLabel is the label a folder of this session stands for.
func (s *Session) folderLabel(name string) string {
	for _, f := range s.folders {
		if f.Name == name {
			return f.Label
		}
	}
	l, _ := store.LabelForMailbox(name, "")
	return l
}

// folderFor is the folder holding a system label: Trash, Spam.
func (s *Session) folderFor(label string) string {
	for _, f := range s.folders {
		if f.Label == label {
			return f.Name
		}
	}
	return ""
}

// archiveFolder is the server's Archive folder, created if it has none.
func (s *Session) archiveFolder() (string, error) {
	for _, f := range s.folders {
		if strings.EqualFold(f.Special, `\Archive`) {
			return f.Name, nil
		}
	}
	for _, f := range s.folders {
		if strings.EqualFold(f.Name, "Archive") {
			return f.Name, nil
		}
	}
	if err := s.c.Create("Archive"); err != nil && !strings.Contains(strings.ToLower(err.Error()), "exist") {
		return "", err
	}
	s.folders = append(s.folders, Folder{Name: "Archive", Special: `\Archive`})
	return "Archive", nil
}

// gmailLabelArg is a label as STORE X-GM-LABELS takes it: a system label as
// the atom Gmail names it, anything else as a quoted string in modified
// UTF-7.
//
// Quoted here rather than left to go-imap, whose UidStore turns every string
// it is given into a bare atom — right for flags, and for a label called
// "Old stuff" two labels, "Old" and "stuff".
func gmailLabelArg(label string) interface{} {
	n := store.GmailLabelName(label)
	if strings.HasPrefix(n, `\`) {
		return imap.RawString(n)
	}
	if enc, err := utf7.Encoding.NewEncoder().String(n); err == nil {
		n = enc
	}
	return imap.RawString(`"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(n) + `"`)
}

// isConnErr reports whether an error means the connection is gone, as
// opposed to the server refusing one command.
func isConnErr(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection") || strings.Contains(msg, "eof") ||
		strings.Contains(msg, "closed") || strings.Contains(msg, "broken pipe")
}
