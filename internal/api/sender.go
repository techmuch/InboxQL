package api

import (
	"fmt"
	"log"
	stdsync "sync"
	"time"

	"github.com/user/inboxql/internal/outbox"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/uisession"
)

// The sender: drafts somebody pressed Send on go out when their Undo window
// has passed. Checked every second, because that is the granularity a person
// watching an "Undo" countdown notices.

var (
	senderStop chan struct{}
	senderWG   stdsync.WaitGroup
)

// StartSender begins sending what is due.
func StartSender() {
	senderStop = make(chan struct{})
	senderWG.Add(1)
	go func() {
		defer senderWG.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-senderStop:
				return
			case <-t.C:
				sendDue()
			}
		}
	}()
}

// StopSender stops it, after anything being sent has finished.
func StopSender() {
	if senderStop == nil {
		return
	}
	close(senderStop)
	senderWG.Wait()
	senderStop = nil
}

func sendDue() {
	due, err := store.DueDrafts(time.Now())
	if err != nil || len(due) == 0 {
		return
	}
	for _, d := range due {
		if _, err := outbox.Deliver(d); err != nil {
			if err == outbox.ErrNotQueued {
				continue
			}
			log.Printf("[outbox] could not send %s: %v", d.ID, err)
			sessions.Broadcast(uisession.Command{Verb: "notice",
				Arg: fmt.Sprintf("Not sent: “%s” — %v. It is in Drafts.", d.Subject, err)})
			continue
		}
		log.Printf("[outbox] sent %s", d.ID)
		// The copy kept under Sent shows now; the account's worker pushes and
		// links the server's copy.
		sessions.Broadcast(uisession.Command{Verb: "mail", Arg: d.AccountID})
		if workers != nil {
			workers.WakePush(d.AccountID)
		}
	}
}
