package api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/user/inboxql/internal/store"
)

// Drafts from the interface: File → New message, and the Desk's Compose.
//
// A draft is saved, not sent. Mail leaves InboxQL from the outbox, approved at
// a terminal, until sending from the interface is built; the response says
// what to run.
func registerDraftRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/drafts", handleDraftList)
	mux.HandleFunc("POST /api/drafts", handleDraftCreate)
}

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
	if body.Queue {
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
	if err := store.SaveDraft(d); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	note := "Saved as a draft."
	if body.Queue {
		note = "Queued in the outbox. Nothing is sent until you run: iql outbox approve " + d.ID
	}
	writeJSON(w, http.StatusCreated, map[string]any{"draft": d, "note": note})
}
