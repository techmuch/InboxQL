package sync

import (
	"fmt"
	"io"
	"log"
	"net"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/google/uuid"
	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

const DefaultMaxHostConnections = 5

// SyncManager manages synchronization for multiple accounts, handling concurrency limits.
type SyncManager struct {
	mu                 sync.Mutex
	hostConnections    map[string]chan struct{}
	MaxHostConnections int

	// Blobs receives attachment bytes. Nil means the caller never told the
	// manager where the data directory is, and sync stores no attachments —
	// which is what it did unconditionally until this field existed.
	Blobs *blobstore.Store

	// MaxAttachmentBytes caps a single part; zero takes the extractor's default.
	MaxAttachmentBytes int64
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
	store.MarkAccountSynced(acc.ID)
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

	// The saved position is a UID, so it is compared with UIDNEXT and fetched
	// with UID FETCH. It used to be compared with the message count and
	// fetched as a sequence number, which only agree on a folder nothing has
	// ever left: on a real Gmail inbox the first sync worked and every later
	// one reported success having fetched nothing.
	//
	// A saved position at or past UIDNEXT means the server renumbered the
	// folder (a new UIDVALIDITY), so start again from the beginning; messages
	// already stored are skipped by Message-ID below.
	if mbox.UidNext > 0 && syncState.LastUID >= mbox.UidNext {
		log.Printf("Mailbox %s: saved UID %d is past the server's next UID %d; the folder was renumbered, starting again",
			mailboxName, syncState.LastUID, mbox.UidNext)
		syncState.LastUID = 0
	}
	fromUID := syncState.LastUID + 1
	log.Printf("Mailbox %s has %d messages. Fetching from UID %d", mailboxName, mbox.Messages, fromUID)
	if mbox.Messages == 0 || (mbox.UidNext > 0 && fromUID >= mbox.UidNext) {
		return 0, nil
	}

	// "n:*" always includes the highest UID, even when it is below n, so the
	// loop below also skips anything at or before the saved position.
	seqset := new(imap.SeqSet)
	seqset.AddRange(fromUID, 0)

	items := []imap.FetchItem{
		imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate,
		imap.FetchRFC822Size, imap.FetchItem("BODY[]"), imap.FetchItem("UID"),
	}

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.UidFetch(seqset, items, messages)
	}()

	count, attachments := 0, 0
	for imapMsg := range messages {
		if imapMsg.Uid < fromUID {
			continue
		}
		if imapMsg.Uid > syncState.LastUID {
			syncState.LastUID = imapMsg.Uid
		}

		parsed, raw, err := parseIMAPMessage(acc.ID, imapMsg)
		if err != nil {
			log.Printf("Error parsing msg %d: %v", imapMsg.Uid, err)
			continue
		}

		// Which folder this came from is only known here, not in the parser.
		parsed.Mailbox = mailboxName

		// Per account, as the importer and the content-hash index are: a
		// message between two of your own accounts belongs to both. A message
		// with no Message-ID is left to that index, since a blank id would
		// otherwise match every other message without one.
		if parsed.MessageID != "" {
			if exists, _ := store.MessageExistsByMessageIDForAccount(acc.ID, parsed.MessageID); exists {
				continue
			}
		}

		if err := store.SaveMessage(parsed); err != nil {
			log.Printf("ERROR saving message %d: %v", imapMsg.Uid, err)
			continue
		}
		count++

		// Attachments come after the message row exists — they are foreign-keyed
		// to it — and a failure here costs the attachments, not the message.
		if sm.Blobs != nil {
			counts, err := store.StoreAttachments(parsed.ID, raw, sm.Blobs, sm.MaxAttachmentBytes)
			if err != nil {
				log.Printf("ERROR storing attachments for msg %d: %v", imapMsg.Uid, err)
			}
			attachments += counts.Stored
		}
	}

	if err := <-done; err != nil {
		return count, err
	}

	store.SaveMailboxSyncState(syncState)
	if attachments > 0 {
		log.Printf("Mailbox %s: stored %d messages, %d attachments", mailboxName, count, attachments)
	}
	return count, nil
}

// parseIMAPMessage converts a fetched IMAP message into InboxQL's representation.
//
// The MIME walk lives in message.ParseRFC822, shared with the importers. This
// function's job is only the part unique to IMAP: overlaying the transport
// fields, and preferring the server's parsed envelope over our own header
// parsing where the server supplied one.
//
// The raw bytes are returned alongside because the caller needs them for
// attachment extraction. Reading them back off the returned message would
// happen to work — Header currently holds the whole raw message — but that is
// an accident of storage no caller should be invited to depend on.
func parseIMAPMessage(accountID string, imapMsg *imap.Message) (*message.Message, []byte, error) {
	var msg *message.Message
	var raw []byte

	section, _ := imap.ParseBodySectionName("BODY[]")
	if body := imapMsg.GetBody(section); body != nil {
		var err error
		raw, err = io.ReadAll(body)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read message body: %w", err)
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
		// Names are recorded beside the addresses, never inside them: From
		// feeds the content hash, so folding a display name into it would make
		// the same message fail to deduplicate against the copy already stored.
		msg.Names = map[string]string{}
		noteAddr := func(a *imap.Address) string {
			if a == nil || a.MailboxName == "" || a.HostName == "" {
				return ""
			}
			addr := strings.ToLower(a.MailboxName + "@" + a.HostName)
			if name := strings.TrimSpace(a.PersonalName); name != "" {
				if _, seen := msg.Names[addr]; !seen {
					msg.Names[addr] = name
				}
			}
			return a.MailboxName + "@" + a.HostName
		}

		if len(env.From) > 0 {
			if addr := noteAddr(env.From[0]); addr != "" {
				msg.From = addr
			}
		}
		if len(env.To) > 0 {
			var to []string
			for _, a := range env.To {
				if addr := noteAddr(a); addr != "" {
					to = append(to, addr)
				}
			}
			if len(to) > 0 {
				msg.To = to
			}
		}
		for _, a := range env.Cc {
			noteAddr(a)
		}
		if len(msg.Names) == 0 {
			msg.Names = nil
		}
	}

	if len(msg.Header) == 0 {
		msg.Header = []byte("No Header")
	}

	// The envelope may have replaced fields the hash covers, so recompute.
	msg.Rehash()
	return msg, raw, nil
}

// AccessToken, when set, gives an access token for an account that signs in
// with Google. Set by the package that holds the Google sign-in, which this
// one cannot import without a cycle.
var AccessToken func(acc *account.Account) (string, error)

// ConnectIMAP connects and signs in.
//
// Encryption is mandatory. With SSL on, the connection is TLS from the first
// byte; with it off, the server must offer STARTTLS before any credential is
// sent, as it already must for SMTP. Turning SSL off used to send the
// password in plaintext.
func ConnectIMAP(acc *account.Account) (*client.Client, error) {
	if acc.Disconnected {
		return nil, fmt.Errorf("account %s is disconnected: it keeps its mail but has no login; edit it to connect again", acc.ID)
	}
	addr := net.JoinHostPort(acc.Host, fmt.Sprint(acc.Port))
	dialer := &net.Dialer{Timeout: account.DialTimeout}
	var c *client.Client
	var err error
	if acc.SSL {
		// Certificate verification is mandatory, against the host the user
		// configured rather than whatever the connection resolved to.
		c, err = client.DialWithDialerTLS(dialer, addr, account.TLSConfig(acc.Host))
	} else {
		c, err = client.DialWithDialer(dialer, addr)
		if err == nil {
			if ok, _ := c.SupportStartTLS(); !ok {
				c.Logout()
				return nil, fmt.Errorf("%s does not offer STARTTLS; refusing to send a password in plaintext", addr)
			}
			if err = c.StartTLS(account.TLSConfig(acc.Host)); err != nil {
				c.Logout()
				return nil, fmt.Errorf("STARTTLS failed: %w", err)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	// A server can accept the connection and then stall; the deadline covers
	// the sign-in too. Lifted afterwards, because a sync legitimately holds
	// the connection for a long time.
	c.Timeout = account.DialTimeout
	if acc.UsesGoogle() {
		err = loginGoogle(c, acc)
	} else {
		err = c.Login(acc.User, acc.Password)
	}
	if err != nil {
		// Close the connection rather than leaking it: the caller only defers
		// Logout on success, so a failed login would otherwise hold the socket
		// open until GC.
		c.Logout()
		return nil, err
	}
	c.Timeout = 0
	return c, nil
}

func loginGoogle(c *client.Client, acc *account.Account) error {
	if AccessToken == nil {
		return fmt.Errorf("Sign in with Google is not available in this build")
	}
	tok, err := AccessToken(acc)
	if err != nil {
		return err
	}
	user := acc.User
	if user == "" {
		user = acc.Email
	}
	return c.Authenticate(account.XOAuth2(user, tok))
}
