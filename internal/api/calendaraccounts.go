package api

import (
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"

	"github.com/user/inboxql/internal/gcal"
)

// Connecting calendar accounts.

var googleFlows gcal.Flows

func registerCalendarAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/calendar/accounts/google/start", handleGoogleStart)
	mux.HandleFunc("POST /api/calendar/accounts/ews", handleEWSConnect)
}

// GoogleCallbackPath is where Google sends the browser back. Registered
// outside the auth middleware — the request is a navigation from google.com,
// which the middleware rightly treats as cross-site — and protected instead
// by its state, which is random, single-use and expires in ten minutes.
const GoogleCallbackPath = "/oauth/google/callback"

// RegisterOAuthRoutes mounts the callback on the top-level mux.
func RegisterOAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+GoogleCallbackPath, handleGoogleCallback)
}

// googleRedirect is the loopback address Google will send the browser back
// to: 127.0.0.1 on this server's port. Google requires a loopback IP for a
// desktop client, which is also why it is not the site name.
func googleRedirect() string {
	port := "8420"
	if _, p, err := net.SplitHostPort(getRuntime().Addr); err == nil && p != "" {
		port = p
	}
	return "http://127.0.0.1:" + port + GoogleCallbackPath
}

func handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	if !strings.HasSuffix(strings.TrimSpace(body.ClientID), ".apps.googleusercontent.com") {
		writeError(w, http.StatusBadRequest, "that does not look like a Google OAuth client ID (it ends in .apps.googleusercontent.com)")
		return
	}
	f := gcal.NewFlow(body.ClientID, body.ClientSecret, googleRedirect())
	googleFlows.Add(f)
	writeJSON(w, http.StatusOK, map[string]any{"authUrl": f.URL(), "redirect": f.Redirect})
}

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	page := func(status int, title, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%s</title>
<body style="font-family:system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem;line-height:1.5">
<h1 style="font-size:1.3rem">%s</h1><p>%s</p></body>`, html.EscapeString(title), html.EscapeString(title), body)
	}
	if e := r.URL.Query().Get("error"); e != "" {
		page(http.StatusOK, "Google calendar not connected", "Google said: "+html.EscapeString(e)+". You can close this tab.")
		return
	}
	f, ok := googleFlows.Take(r.URL.Query().Get("state"))
	if !ok {
		page(http.StatusBadRequest, "This sign-in has expired", "Start again from Settings → Calendars. You can close this tab.")
		return
	}
	acct, err := f.Finish(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		page(http.StatusBadGateway, "Google calendar not connected", html.EscapeString(err.Error()))
		return
	}
	engine().Kick()
	page(http.StatusOK, "Connected "+acct.Email, "InboxQL is reading your calendars now. You can close this tab and return to InboxQL.")
}
