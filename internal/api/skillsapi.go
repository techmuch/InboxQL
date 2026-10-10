package api

import (
	"errors"
	"net/http"
	"os"

	"github.com/user/inboxql/internal/skills"
)

// The AI-assistants card: which coding tools are on this machine, and the iql
// skill for each. The body names a location and nothing else — the skill's
// text is fixed in the binary, because an endpoint that wrote what it was sent
// into an AI tool's instructions would be a way to give the user's agent
// orders.
func registerSkillsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/system/skills", handleSkillsList)
	mux.HandleFunc("POST /api/system/skills", handleSkillsChange)
}

func handleSkillsList(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	v := currentVersionInfo.Version
	writeJSON(w, http.StatusOK, map[string]any{
		"tools": skills.Detect(home), "locations": skills.Status(home, v), "version": v,
	})
}

func handleSkillsChange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action   string `json:"action"`   // install or remove
		Location string `json:"location"` // claude or agents
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	l, err := skills.Find(home, body.Location)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	var st skills.State
	switch body.Action {
	case "install":
		st, err = skills.Install(l, currentVersionInfo.Version)
	case "remove":
		st, err = skills.Remove(l, currentVersionInfo.Version)
	default:
		writeError(w, http.StatusBadRequest, `action is "install" or "remove"`)
		return
	}
	if errors.Is(err, skills.ErrForeign) {
		writeError(w, http.StatusConflict, "%s: %v", l.Path, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
