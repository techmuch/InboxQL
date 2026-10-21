package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/user/inboxql/internal/store"
)

// registerBoardRoutes is everything the board does to cards beyond creating
// and editing them: where they sit, their labels, checklists and comments,
// and the lists, boards and rules around them.
//
// Each is a small verb rather than a POST of the whole card. Two people (or a
// person and a rule) touching different parts of one card should not undo
// each other, and saving a card somebody had open must not move it back.
func registerBoardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/board/move", handleCardMove)
	mux.HandleFunc("POST /api/board/archive", handleCardArchive)

	mux.HandleFunc("GET /api/board/labels", handleBoardLabels)
	mux.HandleFunc("POST /api/board/labels", handleCardLabels)
	mux.HandleFunc("POST /api/board/labels/color", handleLabelColor)

	mux.HandleFunc("POST /api/board/checklist", handleChecklistAdd)
	mux.HandleFunc("PATCH /api/board/checklist", handleChecklistEdit)
	mux.HandleFunc("DELETE /api/board/checklist", handleChecklistDelete)
	mux.HandleFunc("POST /api/board/checklist/rename", handleChecklistRename)

	mux.HandleFunc("POST /api/board/comments", handleCommentAdd)
	mux.HandleFunc("PATCH /api/board/comments", handleCommentEdit)
	mux.HandleFunc("DELETE /api/board/comments", handleCommentDelete)

	mux.HandleFunc("GET /api/board/lists", handleListsGet)
	mux.HandleFunc("POST /api/board/lists", handleListCreate)
	mux.HandleFunc("PATCH /api/board/lists", handleListEdit)
	mux.HandleFunc("DELETE /api/board/lists", handleListDelete)

	mux.HandleFunc("GET /api/board/boards", handleBoardsGet)
	mux.HandleFunc("POST /api/board/boards", handleBoardCreate)
	mux.HandleFunc("PATCH /api/board/boards", handleBoardEdit)
	mux.HandleFunc("DELETE /api/board/boards", handleBoardDelete)

	mux.HandleFunc("GET /api/board/rules", handleRulesGet)
	mux.HandleFunc("POST /api/board/rules", handleRuleCreate)
	mux.HandleFunc("PATCH /api/board/rules", handleRuleEdit)
	mux.HandleFunc("DELETE /api/board/rules", handleRuleDelete)
	mux.HandleFunc("POST /api/board/rules/run", handleRulesRun)

	mux.HandleFunc("GET /api/board/due", handleCardsDue)
}

// answer writes the result of a store call: 400 with its reason, or ok.
func answer(w http.ResponseWriter, err error, ok any) {
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if ok == nil {
		ok = map[string]bool{"ok": true}
	}
	writeJSON(w, http.StatusOK, ok)
}

// card answers with the card as its panel shows it, after a change.
func card(w http.ResponseWriter, id string, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	d, err := store.GetTicketDetail(id)
	if err == nil && d == nil {
		writeError(w, http.StatusNotFound, "no ticket %q", id)
		return
	}
	answer(w, err, d)
}

func handleCardMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string `json:"id"`
		List  string `json:"list"`
		After string `json:"after"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	card(w, req.ID, store.MoveTicket(req.ID, req.List, req.After))
}

func handleCardArchive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Archived bool   `json:"archived"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	card(w, req.ID, store.ArchiveTicket(req.ID, req.Archived))
}

func handleBoardLabels(w http.ResponseWriter, _ *http.Request) {
	labels, err := store.BoardLabels()
	if labels == nil {
		labels = []store.LabelInfo{}
	}
	answer(w, err, map[string]any{"labels": labels, "colors": store.LabelColors})
}

func handleCardLabels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string   `json:"id"`
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
		Set    []string `json:"set"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	var err error
	if req.Set != nil {
		err = store.SetTicketLabels(req.ID, req.Set)
	} else {
		if err = store.AddTicketLabels(req.ID, req.Add); err == nil {
			err = store.RemoveTicketLabels(req.ID, req.Remove)
		}
	}
	card(w, req.ID, err)
}

func handleLabelColor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
		Color string `json:"color"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	answer(w, store.SetLabelColor(req.Label, req.Color), nil)
}

func handleChecklistAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketID  string `json:"ticketId"`
		Checklist string `json:"checklist"`
		Text      string `json:"text"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	_, err := store.AddChecklistItem(req.TicketID, req.Checklist, req.Text)
	card(w, req.TicketID, err)
}

func handleChecklistEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string  `json:"id"`
		Done *bool   `json:"done"`
		Text *string `json:"text"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	var err error
	if req.Text != nil {
		err = store.EditChecklistItem(req.ID, *req.Text)
	}
	if err == nil && req.Done != nil {
		err = store.SetChecklistItemDone(req.ID, *req.Done)
	}
	answer(w, err, nil)
}

func handleChecklistDelete(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if id := q.Get("id"); id != "" {
		answer(w, store.DeleteChecklistItem(id), nil)
		return
	}
	answer(w, store.DeleteChecklist(q.Get("ticketId"), q.Get("checklist")), nil)
}

func handleChecklistRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketID string `json:"ticketId"`
		From     string `json:"from"`
		To       string `json:"to"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	card(w, req.TicketID, store.RenameChecklist(req.TicketID, req.From, req.To))
}

func handleCommentAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketID string `json:"ticketId"`
		Body     string `json:"body"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	_, err := store.AddTicketComment(req.TicketID, req.Body, store.OriginHumanTicket)
	card(w, req.TicketID, err)
}

func handleCommentEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Body string `json:"body"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	answer(w, store.EditTicketComment(req.ID, req.Body), nil)
}

func handleCommentDelete(w http.ResponseWriter, r *http.Request) {
	answer(w, store.DeleteTicketComment(r.URL.Query().Get("id")), nil)
}

func handleListsGet(w http.ResponseWriter, r *http.Request) {
	lists, err := store.TicketLists(r.URL.Query().Get("archived") == "1")
	answer(w, err, lists)
}

func handleListCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Kind  string `json:"kind"`
		After string `json:"after"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	l, err := store.CreateTicketList(req.Title, req.Kind, req.After)
	answer(w, err, l)
}

func handleListEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string  `json:"id"`
		Title    *string `json:"title"`
		Kind     *string `json:"kind"`
		After    *string `json:"after"`
		Archived *bool   `json:"archived"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	var err error
	if req.Title != nil {
		err = store.RenameTicketList(req.ID, *req.Title)
	}
	if err == nil && req.Kind != nil {
		err = store.SetTicketListKind(req.ID, *req.Kind)
	}
	if err == nil && req.After != nil {
		err = store.MoveTicketList(req.ID, *req.After)
	}
	if err == nil && req.Archived != nil {
		err = store.ArchiveTicketList(req.ID, *req.Archived)
	}
	answer(w, err, nil)
}

func handleListDelete(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	answer(w, store.DeleteTicketList(q.Get("id"), q.Get("moveTo")), nil)
}

func handleBoardsGet(w http.ResponseWriter, _ *http.Request) {
	boards, err := store.TicketBoards()
	answer(w, err, boards)
}

func handleBoardCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title  string `json:"title"`
		Filter string `json:"filter"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	b, err := store.CreateTicketBoard(req.Title, req.Filter)
	answer(w, err, b)
}

func handleBoardEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string  `json:"id"`
		Title  *string `json:"title"`
		Filter *string `json:"filter"`
		After  *string `json:"after"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	b, err := store.GetTicketBoard(req.ID)
	if err == nil && (req.Title != nil || req.Filter != nil) {
		title, filter := b.Title, b.Filter
		if req.Title != nil {
			title = *req.Title
		}
		if req.Filter != nil {
			filter = *req.Filter
		}
		err = store.UpdateTicketBoard(b.ID, title, filter)
	}
	if err == nil && req.After != nil {
		err = store.MoveTicketBoard(req.ID, *req.After)
	}
	answer(w, err, nil)
}

func handleBoardDelete(w http.ResponseWriter, r *http.Request) {
	answer(w, store.DeleteTicketBoard(r.URL.Query().Get("id")), nil)
}

func handleRulesGet(w http.ResponseWriter, _ *http.Request) {
	rules, err := store.TicketRules()
	answer(w, err, map[string]any{"rules": rules, "actions": store.RuleActions,
		"triggers": []string{store.RuleEnterList, store.RuleMail}})
}

func handleRuleCreate(w http.ResponseWriter, r *http.Request) {
	var rule store.TicketRule
	if decodeJSON(w, r, &rule) != nil {
		return
	}
	rule.Since = time.Time{}
	err := store.CreateTicketRule(&rule)
	answer(w, err, rule)
}

func handleRuleEdit(w http.ResponseWriter, r *http.Request) {
	var rule store.TicketRule
	if decodeJSON(w, r, &rule) != nil {
		return
	}
	err := store.UpdateTicketRule(&rule)
	answer(w, err, rule)
}

func handleRuleDelete(w http.ResponseWriter, r *http.Request) {
	answer(w, store.DeleteTicketRule(r.URL.Query().Get("id")), nil)
}

func handleRulesRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Backfill bool   `json:"backfill"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	out, err := store.RunMailRules(req.ID, req.Backfill)
	answer(w, err, out)
}

// handleCardsDue is the cards due in a range, for the calendar.
func handleCardsDue(w http.ResponseWriter, r *http.Request) {
	from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "from and to are RFC 3339 times")
		return
	}
	cards, err := store.TicketsDue(from, to)
	answer(w, err, cards)
}

// cardPatch is a partial edit of a card's own fields. A date is absent (left
// alone), null (cleared), or a value: an RFC 3339 time, a day, or anything
// due: accepts, such as 3d.
type cardPatch struct {
	Title    *string         `json:"title"`
	Body     *string         `json:"body"`
	Priority *string         `json:"priority"`
	Status   *string         `json:"status"`
	DueAt    json.RawMessage `json:"dueAt"`
	StartAt  json.RawMessage `json:"startAt"`
}

func (p cardPatch) apply(t *store.Ticket) error {
	if p.Title != nil {
		t.Title = *p.Title
	}
	if p.Body != nil {
		t.Body = *p.Body
	}
	if p.Priority != nil {
		t.Priority = strings.TrimSpace(*p.Priority)
	}
	if p.Status != nil {
		t.Status = *p.Status
	}
	var err error
	if t.DueAt, err = patchDate(p.DueAt, t.DueAt); err != nil {
		return err
	}
	t.StartAt, err = patchDate(p.StartAt, t.StartAt)
	return err
}

func patchDate(raw json.RawMessage, current *time.Time) (*time.Time, error) {
	if len(raw) == 0 {
		return current, nil
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s = strings.TrimSpace(s); s == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t, nil
	}
	return store.ParseTicketDue(s)
}
