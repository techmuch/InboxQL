package sync

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/google/uuid"
	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

const DefaultMaxHostConnections = 5

// SyncManager manages synchronization for multiple accounts, handling concurrency limits.
type SyncManager struct {
	mu                 sync.Mutex
	hostConnections    map[string]chan struct{}
	MaxHostConnections int
}

// NewSyncManager creates a new SyncManager.
func NewSyncManager(maxHostConnections int) *SyncManager {
	if maxHostConnections <= 0 {
		maxHostConnections = DefaultMaxHostConnections
	}
	return &SyncManager{
		hostConnections:    make(map[string]chan struct{}),
		MaxHostConnections: maxHostConnections,
	}
}

// getHostConnectionLimiter returns the connection limiter for a given host.
func (sm *SyncManager) getHostConnectionLimiter(host string) chan struct{} {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if limiter, ok := sm.hostConnections[host]; ok {
		return limiter
	}
	limiter := make(chan struct{}, sm.MaxHostConnections)
	sm.hostConnections[host] = limiter
	return limiter
}

// StartSync initiates the synchronization process for a given account.
//
// It is normally run in its own goroutine. A panic here — a malformed IMAP
// response reaching a parser is the realistic cause — would otherwise take down
// the entire server process rather than just this account's sync, so the whole
// body runs under a recover that records the failure against the account and
// lets every other account carry on.
func (sm *SyncManager) StartSync(acc *account.Account) {
	defer func() {
		if r := recover(); r != nil {
			// The stack trace is the only way to find the offending parser, so
			// it is logged in full rather than just the panic value.
			log.Printf("PANIC during sync of account %s on host %s: %v\n%s",
				acc.ID, acc.Host, r, debug.Stack())
			store.UpdateAccountStatus(acc.ID, "error",
				fmt.Sprintf("internal error during sync: %v", r))
		}
	}()

	limiter := sm.getHostConnectionLimiter(acc.Host)
	limiter <- struct{}{}        // Acquire slot
	defer func() { <-limiter }() // Release slot

	log.Printf("Starting real sync for account %s on host %s", acc.ID, acc.Host)
	store.UpdateAccountStatus(acc.ID, "syncing", "")

	c, err := ConnectIMAP(acc)
	if err != nil {
		errMsg := fmt.Sprintf("failed to connect: %v", err)
		log.Printf("Failed to connect for account %s: %v", acc.ID, err)
		store.UpdateAccountStatus(acc.ID, "error", errMsg)
		return
	}
	defer c.Logout()

	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", mailboxes)
	}()

	var mboxes []string
	for mb := range mailboxes {
		name := mb.Name
		if strings.EqualFold(name, "INBOX") || strings.Contains(strings.ToUpper(name), "SENT") {
			mboxes = append(mboxes, name)
		}
	}

	if err := <-done; err != nil {
		errMsg := fmt.Sprintf("error listing mailboxes: %v", err)
		log.Printf("Error listing mailboxes: %v", err)
		store.UpdateAccountStatus(acc.ID, "error", errMsg)
		return
	}

	for _, name := range mboxes {
		log.Printf("Syncing mailbox: %s", name)
		if _, err := sm.syncMailbox(c, acc, name); err != nil {
			errMsg := fmt.Sprintf("error syncing %s: %v", name, err)
			log.Printf("Error syncing %s: %v", name, err)
			store.UpdateAccountStatus(acc.ID, "error", errMsg)
			return
		}
	}

	store.UpdateAccountStatus(acc.ID, "success", "")
}

func (sm *SyncManager) syncMailbox(c *client.Client, acc *account.Account, mailboxName string) (int, error) {
	mbox, err := c.Select(mailboxName, false)
	if err != nil {
		return 0, err
	}

	mailboxID := fmt.Sprintf("%s-%s", acc.ID, mailboxName)
	syncState, _ := store.GetMailboxSyncState(mailboxID)
	if syncState == nil {
		syncState = &store.MailboxSyncState{ID: mailboxID, AccountID: acc.ID, Name: mailboxName}
	}

	fromUID := syncState.LastUID + 1
	log.Printf("Mailbox %s has %d messages. Fetching from UID %d", mailboxName, mbox.Messages, fromUID)
	if fromUID > mbox.Messages && mbox.Messages > 0 {
		return 0, nil
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(fromUID, 0xffffffff)

	items := []imap.FetchItem{
		imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate,
		imap.FetchRFC822Size, imap.FetchItem("BODY[]"), imap.FetchItem("UID"),
	}

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	count := 0
	for imapMsg := range messages {
		if imapMsg.Uid > syncState.LastUID {
			syncState.LastUID = imapMsg.Uid
		}

		parsed, err := parseIMAPMessage(acc.ID, imapMsg)
		if err != nil {
			log.Printf("Error parsing msg %d: %v", imapMsg.Uid, err)
			continue
		}

		// Which folder this came from is only known here, not in the parser.
		parsed.Mailbox = mailboxName

		if exists, _ := store.MessageExistsByMessageID(parsed.MessageID); exists {
			continue
		}

		if err := store.SaveMessage(parsed); err == nil {
			count++
		} else {
			log.Printf("ERROR saving message %d: %v", imapMsg.Uid, err)
		}
	}

	if err := <-done; err != nil {
		return count, err
	}

	store.SaveMailboxSyncState(syncState)
	return count, nil
}

// parseIMAPMessage converts a fetched IMAP message into InboxQL's representation.
//
// The MIME walk lives in message.ParseRFC822, shared with the importers. This
// function's job is only the part unique to IMAP: overlaying the transport
// fields, and preferring the server's parsed envelope over our own header
// parsing where the server supplied one.
func parseIMAPMessage(accountID string, imapMsg *imap.Message) (*message.Message, error) {
	var msg *message.Message

	section, _ := imap.ParseBodySectionName("BODY[]")
	if body := imapMsg.GetBody(section); body != nil {
		raw, err := io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("cannot read message body: %w", err)
		}
		// A parse error still yields a usable message carrying the raw bytes,
		// so malformed mail is stored rather than dropped.
		msg, _ = message.ParseRFC822(raw)
	}
	if msg == nil {
		msg = &message.Message{Header: []byte("No Header")}
	}

	msg.ID = uuid.New().String()
	msg.AccountID = accountID
	msg.UID = imapMsg.Uid
	msg.Flags = imapMsg.Flags
	msg.Size = imapMsg.Size
	msg.InternalDate = imapMsg.InternalDate

	// The server already parsed these; trust its envelope over our own reading
	// of the same headers, but never let an empty envelope field blank out
	// something we did manage to parse.
	if env := imapMsg.Envelope; env != nil {
		if env.Subject != "" {
			msg.Subject = env.Subject
		}
		if env.MessageId != "" {
			msg.MessageID = env.MessageId
		}
		if !env.Date.IsZero() {
			msg.Date = env.Date
		}
		if len(env.From) > 0 {
			f := env.From[0]
			if f.MailboxName != "" && f.HostName != "" {
				msg.From = fmt.Sprintf("%s@%s", f.MailboxName, f.HostName)
			}
		}
		if len(env.To) > 0 {
			var to []string
			for _, a := range env.To {
				if a.MailboxName != "" && a.HostName != "" {
					to = append(to, fmt.Sprintf("%s@%s", a.MailboxName, a.HostName))
				}
			}
			if len(to) > 0 {
				msg.To = to
			}
		}
	}

	if len(msg.Header) == 0 {
		msg.Header = []byte("No Header")
	}

	// The envelope may have replaced fields the hash covers, so recompute.
	msg.Rehash()
	return msg, nil
}

func ConnectIMAP(acc *account.Account) (*client.Client, error) {
	addr := fmt.Sprintf("%s:%d", acc.Host, acc.Port)
	var c *client.Client
	var err error
	if acc.SSL {
		// Certificate verification is mandatory. This previously passed
		// InsecureSkipVerify:true, which silently accepted any certificate and
		// left every login and every message body open to a trivial MITM.
		// ServerName is set explicitly so verification is against the host the
		// user configured rather than whatever the connection resolved to.
		c, err = client.DialTLS(addr, &tls.Config{
			ServerName: acc.Host,
			MinVersion: tls.VersionTLS12,
		})
	} else {
		c, err = client.Dial(addr)
	}
	if err != nil {
		return nil, err
	}
	if err := c.Login(acc.User, acc.Password); err != nil {
		// Close the connection rather than leaking it: the caller only defers
		// Logout on success, so a failed login would otherwise hold the socket
		// open until GC.
		c.Logout()
		return nil, err
	}
	return c, nil
}
