package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/user/inboxql/internal/store"
)

// Drafts from the interface: File → New message, the Desk's Compose, and
// the outbox.
//
// Send is a schedule, not a send: the draft goes out when its Undo window
// has passed (UndoWindow), which the sender loop in sender.go watches for.
// Until then Undo takes it back as a draft.
func registerDraftRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/drafts", handleDraftList)
	mux.HandleFunc("POST /api/drafts", handleDraftCreate)
	mux.HandleFunc("GET /api/drafts/{id}", handleDraftGet)
	mux.HandleFunc("POST /api/drafts/{id}/send", handleDraftSend)
	mux.HandleFunc("POST /api/drafts/{id}/undo", handleDraftUndo)
	mux.HandleFunc("POST /api/drafts/{id}/reject", handleDraftReject)
}

// handleDraftReject returns a queued draft to Drafts, as `iql outbox reject`
// does.
func handleDraftReject(w http.ResponseWriter, r *http.Request) {
	d, err := store.GetDraft(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "no draft %q", r.PathValue("id"))
		return
	}
	if d.Status != store.DraftStatusQueued {
		writeError(w, http.StatusConflict, "this message is %s, not waiting in the outbox", d.Status)
		return
	}
	d.Status, d.QueuedAt, d.SendAt = store.DraftStatusDraft, nil, nil
	if err := store.SaveDraft(d); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": d})
}

// UndoWindow is how long Send waits before the mail goes.
var UndoWindow = 10 * time.Second

func handleDraftList(w http.ResponseWriter, r *http.Request) {
	ds, err := store.ListDrafts(r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if ds == nil {
		ds = []*store.Draft{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": ds})
}

func addresses(list []string) []string {
	out := []string{}
	for _, a := range list {
		for _, part := range strings.FieldsFunc(a, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
			if p := strings.TrimSpace(part); strings.Contains(p, "@") {
				out = append(out, strings.ToLower(p))
			}
		}
	}
	return out
}

func handleDraftCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID string   `json:"accountId"`
		To        []string `json:"to"`
		Cc        []string `json:"cc"`
		Subject   string   `json:"subject"`
		Body      string   `json:"body"`
		InReplyTo string   `json:"inReplyTo"`
		Queue     bool     `json:"queue"`
		// Send: pressing Send, which approves it and starts the Undo window.
		Send bool `json:"send"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	if acc, err := store.GetAccount(body.AccountID); err != nil || acc == nil {
		writeError(w, http.StatusBadRequest, "choose the account to send from")
		return
	}
	d := &store.Draft{ID: uuid.New().String(), AccountID: body.AccountID, InReplyTo: body.InReplyTo,
		To: addresses(body.To), Cc: addresses(body.Cc), Subject: strings.TrimSpace(body.Subject), Body: body.Body,
		Origin: store.OriginHuman, Status: store.DraftStatusDraft}
	if body.Queue || body.Send {
		if len(d.To) == 0 {
			writeError(w, http.StatusBadRequest, "a message to queue needs someone to send it to")
			return
		}
		if d.Subject == "" {
			writeError(w, http.StatusBadRequest, "a message to queue needs a subject")
			return
		}
		d.Status = store.DraftStatusQueued
	}
	if body.Send {
		at := time.Now().Add(UndoWindow)
		d.SendAt = &at
	}
	if body.Send {
		if acc, _ := store.GetAccount(d.AccountID); acc != nil && acc.SMTPHost == "" {
			writeError(w, http.StatusBadRequest, "%s has no outgoing server; add one in Settings → Mail Accounts", acc.Name)
			return
		}
	}
	if err := store.SaveDraft(d); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	note := "Saved as a draft."
	switch {
	case body.Send:
		note = "Sending."
	case body.Queue:
		note = "Queued in the outbox, waiting for approval: Send it from Waiting for approval, or run: iql outbox approve " + d.ID
	}
	writeJSON(w, http.StatusCreated, map[string]any{"draft": d, "note": note, "undoMs": UndoWindow.Milliseconds()})
}

func handleDraftGet(w http.ResponseWriter, r *http.Request) {
	d, err := store.GetDraft(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "no draft %q", r.PathValue("id"))
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleDraftSend sends a saved or queued draft: from the outbox, or a draft
// opened from Drafts. It goes when its Undo window has passed.
func handleDraftSend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := store.GetDraft(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "no draft %q", id)
		return
	}
	if len(d.To) == 0 || strings.TrimSpace(d.Subject) == "" {
		writeError(w, http.StatusBadRequest, "a message needs someone to send it to and a subject")
		return
	}
	if acc, _ := store.GetAccount(d.AccountID); acc == nil || acc.SMTPHost == "" {
		writeError(w, http.StatusBadRequest, "the sending account has no outgoing server; add one in Settings → Mail Accounts")
		return
	}
	d, err = store.ScheduleSend(id, time.Now().Add(UndoWindow))
	if err != nil {
		writeError(w, http.StatusConflict, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": d, "undoMs": UndoWindow.Milliseconds()})
}

// handleDraftUndo takes a draft back inside its Undo window. 409 says it is
// too late.
func handleDraftUndo(w http.ResponseWriter, r *http.Request) {
	ok, err := store.CancelSend(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "too late to undo: the message has gone")
		return
	}
	d, _ := store.GetDraft(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{"draft": d})
}
