package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/work"
)

// The Work loop page: how efficient and how accurate the AI harness reading
// mail is, the disagreements its checks found, and the targets that decide
// where it reads carefully. See internal/work.
func registerWorkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/work/metrics", handleWorkMetrics)
	mux.HandleFunc("GET /api/work/backlog", handleWorkBacklog)
	mux.HandleFunc("GET /api/work/disagreements", handleWorkDisagreements)
	mux.HandleFunc("POST /api/work/disagreements/resolve", handleWorkResolve)
	mux.HandleFunc("GET /api/work/targets", handleWorkTargets)
	mux.HandleFunc("PUT /api/work/targets", handleWorkSetTarget)
	mux.HandleFunc("GET /api/work/flags", handleWorkFlags)
	mux.HandleFunc("POST /api/work/flags", handleWorkFlag)
	mux.HandleFunc("DELETE /api/work/flags", handleWorkUnflag)
}

// sinceParam reads ?since=7d|30d|90d|all; 30 days when absent.
func sinceParam(r *http.Request) (time.Time, error) {
	switch s := strings.TrimSpace(r.URL.Query().Get("since")); s {
	case "":
		return time.Now().AddDate(0, 0, -30), nil
	case "all":
		return time.Time{}, nil
	default:
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || days <= 0 || days > 3650 {
			return time.Time{}, errors.New("since is a number of days like 30d, or all")
		}
		return time.Now().AddDate(0, 0, -days), nil
	}
}

func handleWorkMetrics(w http.ResponseWriter, r *http.Request) {
	since, err := sinceParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	m, err := work.Measure(since, r.URL.Query().Get("model"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func handleWorkBacklog(w http.ResponseWriter, r *http.Request) {
	rows, err := work.Backlog()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if rows == nil {
		rows = []work.BacklogRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"elements": rows})
}

// disagreement is a check that disagreed, as the page shows it: both answers
// decoded and described, with what the element asks.
type disagreement struct {
	*store.WorkAudit
	About        string          `json:"about"`
	Question     string          `json:"question"`
	Shape        string          `json:"shape"`
	Unit         string          `json:"unit"`
	OriginalSays string          `json:"originalSays"`
	CheckerSays  string          `json:"checkerSays"`
	OriginalJSON json.RawMessage `json:"originalAnswer"`
	CheckerJSON  json.RawMessage `json:"checkerAnswer"`
}

func handleWorkDisagreements(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1"
	list, err := store.Disagreements(!all, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	_, byName, err := elements.Installed()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	out := make([]disagreement, 0, len(list))
	for _, a := range list {
		d := disagreement{WorkAudit: a, OriginalSays: work.Describe(a.Original), CheckerSays: work.Describe(a.Checker),
			OriginalJSON: json.RawMessage(a.Original), CheckerJSON: json.RawMessage(a.Checker)}
		if ann := byName[a.Element]; ann != nil {
			if el, ok := elements.FromAnnotator(ann); ok {
				d.About, d.Shape, d.Unit = el.About, el.Shape, el.Unit
				d.Question = el.Instructions
			}
		}
		out = append(out, d)
	}
	open, err := store.CountOpenDisagreements()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disagreements": out, "open": open})
}

func handleWorkResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Choice string `json:"choice"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	a, err := work.Resolve(req.ID, req.Choice, engine())
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func handleWorkTargets(w http.ResponseWriter, r *http.Request) {
	rt, err := work.LoadRouting()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

func handleWorkSetTarget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Element string  `json:"element"`
		Target  float64 `json:"target"`
		Cost    string  `json:"cost"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if err := work.SetTarget(req.Element, req.Target, req.Cost); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	handleWorkTargets(w, r)
}

func handleWorkFlags(w http.ResponseWriter, r *http.Request) {
	list, err := store.FlaggedThreads(500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if list == nil {
		list = []store.WorkFlag{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": list})
}

func handleWorkFlag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Thread string `json:"thread"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Thread) == "" {
		writeError(w, http.StatusBadRequest, "thread is required")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "flagged by hand"
	}
	if err := store.FlagThread(req.Thread, reason, store.SourceHuman); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": req.Thread, "flagged": true})
}

func handleWorkUnflag(w http.ResponseWriter, r *http.Request) {
	thread := r.URL.Query().Get("thread")
	if thread == "" {
		writeError(w, http.StatusBadRequest, "thread is required")
		return
	}
	if err := store.UnflagThread(thread); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread, "flagged": false})
}
