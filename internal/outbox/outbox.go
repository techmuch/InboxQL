// Package outbox puts mail on the wire.
//
// One way out, whoever asks: the server sending a draft whose Undo window has
// passed, and a person approving one with `iql outbox approve`. Both go
// through Deliver, which claims the draft first so the two can never both
// send it.
package outbox

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/mailer"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// AppendSent files a copy of sent mail in the server's Sent folder. Set by
// the sync package, which has the IMAP connection; nil leaves it out.
var AppendSent func(acc *account.Account, raw []byte) error

// ErrNotQueued says somebody else is sending the draft, or it was taken back.
var ErrNotQueued = fmt.Errorf("this message is not in the outbox: it has been sent, is being sent, or was taken back")

// Deliver sends a queued draft. On success the draft is marked sent, a copy
// is kept here under Sent, and — for servers that do not do it themselves —
// a copy is filed in the server's Sent folder.
func Deliver(d *store.Draft) (*mailer.Message, error) {
	ok, err := store.ClaimDraft(d.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotQueued
	}
	fail := func(err error) (*mailer.Message, error) {
		d.Status = store.DraftStatusFailed
		d.LastError = err.Error()
		d.SendAt = nil
		store.SaveDraft(d)
		return nil, err
	}

	acc, err := store.GetAccount(d.AccountID)
	if err != nil {
		return fail(err)
	}
	if acc == nil {
		return fail(fmt.Errorf("the account %s no longer exists", d.AccountID))
	}
	if acc.Disconnected {
		return fail(fmt.Errorf("%s is disconnected; connect it again to send from it", acc.Name))
	}
	if acc.SMTPHost == "" {
		return fail(fmt.Errorf("%s has no outgoing server; add one in Settings → Mail Accounts", acc.Name))
	}
	msg, err := mailer.FromDraftWithCalendar(d, acc)
	if err != nil {
		return fail(err)
	}
	if err := mailer.Send(acc, msg); err != nil {
		return fail(fmt.Errorf("delivery failed: %w", err))
	}

	now := time.Now()
	d.Status = store.DraftStatusSent
	d.SentAt = &now
	d.SendAt = nil
	d.LastError = ""
	if err := store.SaveDraft(d); err != nil {
		// The mail has gone; losing the record is bad but not a reason to
		// say it was not sent.
		log.Printf("[outbox] %s was sent but the draft could not be updated: %v", d.ID, err)
	}

	raw := msg.Render()
	if err := keepCopy(acc, raw); err != nil {
		log.Printf("[outbox] %s was sent; keeping a copy here failed: %v", d.ID, err)
	}
	if AppendSent != nil && !filesItsOwnSent(acc) {
		if err := AppendSent(acc, raw); err != nil {
			log.Printf("[outbox] %s was sent; filing it in the server's Sent folder failed: %v", d.ID, err)
		}
	}
	return msg, nil
}

// keepCopy stores what was sent, under Sent, so it shows at once. The copy
// that lands in the server's Sent folder carries the same Message-ID, and the
// mirror links the two rather than storing it twice.
func keepCopy(acc *account.Account, raw []byte) error {
	m, _ := message.ParseRFC822(raw)
	if m == nil {
		return fmt.Errorf("could not read back the sent message")
	}
	m.ID = uuid.New().String()
	m.AccountID = acc.ID
	m.Flags = []string{`\Seen`}
	m.Labels = []string{store.LabelSent}
	m.LabelSource = store.LabelSourceHuman
	m.Size = uint32(len(raw))
	m.InternalDate = time.Now()
	if m.Date.IsZero() {
		m.Date = m.InternalDate
	}
	return store.SaveMessage(m)
}

// filesItsOwnSent reports whether the server puts mail sent through it in
// Sent by itself, so a second copy is not filed there. Gmail does.
func filesItsOwnSent(acc *account.Account) bool {
	h := strings.ToLower(acc.SMTPHost + " " + acc.Host)
	return acc.UsesGoogle() || strings.Contains(h, "gmail.com") || strings.Contains(h, "googlemail.com") ||
		store.AccountIsGmail(acc.ID)
}
