package api

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/user/inboxql/internal/gcal"
	"github.com/user/inboxql/internal/store"
)

func TestGoogleSignIn(t *testing.T) {
	mux := calendarTest(t)
	RegisterOAuthRoutes(mux)
	var challenge string
	google := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.Form.Get("code") != "the-code" {
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
		case "/userinfo":
			w.Write([]byte(`{"email":"Me@Gmail.com"}`))
		}
	}))
	defer google.Close()
	gcal.TokenURL, gcal.UserInfo = google.URL+"/token", google.URL+"/userinfo"

	if rec, _ := call(t, mux, "POST", "/api/calendar/accounts/google/start", map[string]any{"clientId": "nonsense"}); rec.Code != 400 {
		t.Errorf("bad client id = %d", rec.Code)
	}
	_, out := call(t, mux, "POST", "/api/calendar/accounts/google/start", map[string]any{
		"clientId": "123-abc.apps.googleusercontent.com", "clientSecret": "s"})
	u, _ := url.Parse(out["authUrl"].(string))
	q := u.Query()
	challenge = q.Get("code_challenge")
	if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" ||
		!strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") || !strings.Contains(q.Get("scope"), "auth/calendar") {
		t.Fatalf("auth url %v", q)
	}
	state := q.Get("state")

	req := httptest.NewRequest("GET", GoogleCallbackPath+"?code=the-code&state="+state, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Connected me@gmail.com") {
		t.Fatalf("callback %d %s", rec.Code, rec.Body)
	}
	accts, _ := store.ListCalendarAccounts()
	if len(accts) != 1 || accts[0].Email != "me@gmail.com" || !accts[0].HasSecret {
		t.Fatalf("accounts %+v", accts)
	}
	if s, _ := store.CalendarAccountSecret(accts[0].ID); s != "rt" {
		t.Errorf("refresh token %q", s)
	}
	// A state is good once.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", GoogleCallbackPath+"?code=the-code&state="+state, nil))
	if rec.Code != 400 {
		t.Errorf("replayed state = %d", rec.Code)
	}
	// A made-up state is refused.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", GoogleCallbackPath+"?code=x&state=forged", nil))
	if rec.Code != 400 {
		t.Errorf("forged state = %d", rec.Code)
	}
}
