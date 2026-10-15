package sync

import (
	"bytes"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/outbox"
)

func init() { outbox.AppendSent = AppendSent }

// AppendSent files a copy of mail just sent in the server's Sent folder, read
// and dated now, as every mail client does for servers that do not do it
// themselves. The folder is the one marked \Sent, or else one named like it;
// with neither, a "Sent" folder is created.
func AppendSent(acc *account.Account, raw []byte) error {
	c, err := ConnectIMAP(acc)
	if err != nil {
		return err
	}
	defer c.Logout()

	ch := make(chan *imap.MailboxInfo, 50)
	done := make(chan error, 1)
	go func() { done <- c.List("", "*", ch) }()
	var special, named string
	for mb := range ch {
		for _, a := range mb.Attributes {
			if strings.EqualFold(a, `\Sent`) {
				special = mb.Name
			}
		}
		switch strings.ToLower(mb.Name) {
		case "sent", "sent messages", "sent items", "sent mail", "inbox.sent":
			if named == "" {
				named = mb.Name
			}
		}
	}
	if err := <-done; err != nil {
		return err
	}
	folder := special
	if folder == "" {
		folder = named
	}
	if folder == "" {
		folder = "Sent"
		if err := c.Create(folder); err != nil && !strings.Contains(strings.ToLower(err.Error()), "exist") {
			return err
		}
	}
	return c.Append(folder, []string{imap.SeenFlag}, time.Now(), bytes.NewReader(raw))
}
