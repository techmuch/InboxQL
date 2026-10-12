package api

import (
	"net/http"

	"github.com/user/inboxql/internal/ews"
)

// handleEWSConnect tests an Exchange Server sign-in and, if it works, saves
// the account. Nothing is saved for a sign-in that fails.
func handleEWSConnect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
		Auth     string `json:"auth"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	acct, err := ews.Connect(r.Context(), body.URL, body.Username, body.Password, body.Email, body.Auth)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	engine().Kick()
	writeJSON(w, http.StatusOK, acct)
}
