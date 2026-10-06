package api

import (
	"net/http"
	"strconv"

	"github.com/user/inboxql/internal/store"
)

// registerLabelRoutes adds the label verdicts a person can see and rule on.
//
// Separate from the span routes because the question is different. Spans are
// "where in the text"; a label is "what is this", and what a person needs from
// it is the verdict, who gave it, and a way to say it is wrong — or right.
func registerLabelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/messages/{id}/labels", handleMessageLabels)
	mux.HandleFunc("PUT /api/messages/{id}/labels/{annotator}", handleRuleOnLabel)
	mux.HandleFunc("DELETE /api/messages/{id}/labels/{annotator}", handleClearLabelRuling)
}

func handleMessageLabels(w http.ResponseWriter, r *http.Request) {
	labels, err := store.MessageLabels(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if labels == nil {
		labels = []store.MessageLabel{}
	}
	writeJSON(w, http.StatusOK, labels)
}

// ruleRequest is a ruling from the interface.
type ruleRequest struct {
	Matched bool   `json:"matched"`
	Level   string `json:"level,omitempty"`
	// Via defaults to inflow: only the review queue may claim review, because
	// only it draws at random.
	Via string `json:"via,omitempty"`
}

func handleRuleOnLabel(w http.ResponseWriter, r *http.Request) {
	var req ruleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if req.Via == "" {
		req.Via = store.RuledInflow
	}
	ruling := store.Ruling{Matched: req.Matched || req.Level != "", Level: req.Level, Via: req.Via}
	if err := store.RuleOnMessage(r.PathValue("annotator"), r.PathValue("id"), ruling); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	handleMessageLabels(w, r)
}

func handleClearLabelRuling(w http.ResponseWriter, r *http.Request) {
	if err := store.ClearRulingOnMessage(r.PathValue("annotator"), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	handleMessageLabels(w, r)
}

// handleAnnotatorReview returns a draw of messages to rule on for one
// annotator: ?name=loops&n=10.
func handleAnnotatorReview(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 50 {
		n = 10
	}
	items, err := store.ReviewSample(r.URL.Query().Get("name"), n)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if items == nil {
		items = []store.ReviewItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAnnotatorScore reports how often an annotator agrees with reviewed
// rulings: ?name=loops.
func handleAnnotatorScore(w http.ResponseWriter, r *http.Request) {
	s, err := store.ScoreAnnotator(r.URL.Query().Get("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}
