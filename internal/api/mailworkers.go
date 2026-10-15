package api

import (
	"log"
	"net/http"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/health"
	"github.com/user/inboxql/internal/machine"
	"github.com/user/inboxql/internal/maintenance"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/sync"
	"github.com/user/inboxql/internal/uisession"
)

// workers keeps every account live while the server runs; nil until
// StartMailWorkers, and in tests.
var workers *sync.Workers

// StartMailWorkers begins live sync for every connected account: new mail
// in seconds, changes made here sent to the server, no Sync button needed.
func StartMailWorkers() {
	workers = sync.NewWorkers(syncManager)
	workers.Changed = mailChanged
	store.PendingNotify = workers.WakePush
	workers.Start()
}

// StopMailWorkers ends them, for a clean shutdown.
func StopMailWorkers() {
	if workers == nil {
		return
	}
	store.PendingNotify = nil
	workers.StopAll()
	workers = nil
}

func restartWorker(id string) {
	if workers != nil {
		go workers.Restart(id)
	}
}

// mailChanged tells every open window to refresh, and runs what new mail
// sets off.
func mailChanged(acc *account.Account, r sync.Report) {
	sessions.Broadcast(uisession.Command{Verb: "mail", Arg: acc.ID})
	if r.New > 0 {
		afterNewMail()
	}
}

// afterNewMail is what new mail sets off: the calendar reads invitations now
// rather than at its next pass, and annotators asking for after-sync run.
//
// The sweep is a job rather than run here: it takes minutes, and the
// maintenance surface is what can show progress and be told to stop. A
// failure to start means a sweep is already running, which is the outcome
// wanted anyway. Not on battery unless the machine settings allow it: a
// sweep can be twenty minutes of extraction nobody asked for, and nothing is
// lost by waiting — what is pending stays pending.
func afterNewMail() {
	engine().Kick()
	if deferHeavyWork() {
		log.Printf("[annotate] after-sync sweep deferred: on battery (heavyWorkOnBattery is off in %s)", machine.Path())
		return
	}
	_, _ = maintenanceJobs().Start(health.JobAnnotators, maintenance.Options{
		Profile: store.TriggerAfterSync,
	})
}

// handleAccountsLive reports, per account, its connection, how much of it is
// here, and the changes waiting to reach its server.
func handleAccountsLive(w http.ResponseWriter, _ *http.Request) {
	accs, err := store.ListAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	out := map[string]any{}
	for _, a := range accs {
		st := sync.WorkerStatus{State: "stopped"}
		if workers != nil {
			st, _ = workers.Status(a.ID)
		}
		progress, _ := store.MirrorProgressFor(a.ID)
		pending, _ := store.PendingSummaryFor(a.ID)
		out[a.ID] = map[string]any{"worker": st, "progress": progress, "pending": pending}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAccountRetry gives changes that kept failing another go.
func handleAccountRetry(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	n, err := store.RetryPendingChanges(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"retrying": n})
}
