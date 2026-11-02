package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/inboxql/internal/ask"
	"github.com/user/inboxql/internal/store"
)

// # Ask in words, over HTTP
//
// A question in plain words, answered by Claude Code on this computer with
// read-only iql commands. Off until the person turns it on: asking sends the
// question, and whatever the commands return, to Anthropic under their
// account. See internal/ask.

const askCLISetting = "ask.cli"

func registerAskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ask", handleAskStatus)
	mux.HandleFunc("PUT /api/ask", handleAskSetting)
	mux.HandleFunc("POST /api/ask", handleAsk)
}

// askStatus is whether asking is on, and what would answer.
func askStatus() map[string]any {
	cli, _ := store.GetSetting(askCLISetting)
	bin := ask.Find()
	return map[string]any{
		"cli":       cli, // "" is off, "claude" is Claude Code
		"enabled":   cli == "claude" && bin != "",
		"available": map[string]any{"claude": bin},
		"sends":     "Your question, and what the iql commands it runs return, to Anthropic, by Claude Code under your own account.",
		"allowed":   ask.AllowedTools,
	}
}

func handleAskStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, askStatus())
}

func handleAskSetting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CLI string `json:"cli"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	switch req.CLI {
	case "", "claude":
	default:
		writeError(w, http.StatusBadRequest, "asking is answered by claude, or off; not %q", req.CLI)
		return
	}
	if req.CLI == "claude" && ask.Find() == "" {
		writeError(w, http.StatusBadRequest, "Claude Code is not installed on this computer")
		return
	}
	if err := store.UpdateSetting(askCLISetting, req.CLI); err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, askStatus())
}

func handleAsk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string `json:"question"`
		Session  string `json:"session"`
	}
	if decodeJSON(w, r, &req) != nil {
		return
	}
	if cli, _ := store.GetSetting(askCLISetting); cli != "claude" {
		writeError(w, http.StatusConflict, "asking in words is off; turn it on in Settings → AI and agents")
		return
	}
	iqlDir := ""
	if exe, err := os.Executable(); err == nil {
		iqlDir = filepath.Dir(exe)
	}
	a := ask.Ask(r.Context(), req.Question, ask.Options{
		Bin: ask.Find(), DataDir: getRuntime().DataDir, IQLDir: iqlDir, Session: strings.TrimSpace(req.Session),
	})
	writeJSON(w, http.StatusOK, a)
}
