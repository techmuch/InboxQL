package gcal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend/memory"
	imapserver "github.com/emersion/go-imap/server"
	"github.com/emersion/go-sasl"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/store"
	imapsync "github.com/user/inboxql/internal/sync"
)

// fakeGoogleAuth issues tokens: the first access token on sign-in, a new one
// on each refresh.
func fakeGoogleAuth(t *testing.T) *int {
	t.Helper()
	refreshes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				w.Write([]byte(`{"access_token":"at-first","refresh_token":"rt-mail","expires_in":3600}`))
			case "refresh_token":
				if r.Form.Get("refresh_token") != "rt-mail" {
					w.Write([]byte(`{"error":"invalid_grant"}`))
					return
				}
				refreshes++
				w.Write([]byte(`{"access_token":"at-refreshed","expires_in":3600}`))
			}
		case "/userinfo":
			w.Write([]byte(`{"email":"Ada@Gmail.com"}`))
		}
	}))
	t.Cleanup(srv.Close)
	oldT, oldU := TokenURL, UserInfo
	TokenURL, UserInfo = srv.URL+"/token", srv.URL+"/userinfo"
	t.Cleanup(func() { TokenURL, UserInfo = oldT, oldU })
	return &refreshes
}

func TestSignInWithGoogleForGmail(t *testing.T) {
	if _, err := store.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.CloseDB)
	refreshes := fakeGoogleAuth(t)

	f := NewMailFlow("cid.apps.googleusercontent.com", "cs", "http://127.0.0.1:8420/oauth/google/callback", true, "")
	u, _ := url.Parse(f.URL())
	scope := u.Query().Get("scope")
	if !strings.Contains(scope, MailScope) || !strings.Contains(scope, "auth/calendar") {
		t.Fatalf("scope %q", scope)
	}

	got, err := f.FinishMail(context.Background(), "code")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mail.Password != "" {
		t.Fatal("the refresh token was handed back in the result")
	}
	acc, _ := store.GetAccount(got.Mail.ID)
	if acc == nil || !acc.UsesGoogle() || acc.Password != "rt-mail" || acc.Host != "imap.gmail.com" ||
		acc.Email != "ada@gmail.com" || acc.OAuthClientID != "cid.apps.googleusercontent.com" {
		t.Fatalf("mail account %+v", acc)
	}
	if got.Calendar == nil {
		t.Fatal("asked for the calendar too, and did not get it")
	}
	if id, _ := SavedClient(); id != "cid.apps.googleusercontent.com" {
		t.Fatalf("saved client %q", id)
	}

	// The access token from sign-in is used while it lasts; then refreshed.
	if tok, _ := MailAccessToken(acc); tok != "at-first" || *refreshes != 0 {
		t.Fatalf("first token %q, refreshes %d", tok, *refreshes)
	}
	tokens = map[string]cached{}
	if tok, _ := MailAccessToken(acc); tok != "at-refreshed" || *refreshes != 1 {
		t.Fatalf("refreshed token %q, refreshes %d", tok, *refreshes)
	}

	// A second sign-in for the same address updates that account, not a new one.
	again := NewMailFlow("cid.apps.googleusercontent.com", "cs", f.Redirect, false, "")
	if got2, err := again.FinishMail(context.Background(), "code"); err != nil || got2.Mail.ID != acc.ID || got2.Calendar != nil {
		t.Fatalf("second sign-in %+v %v", got2, err)
	}
	accs, _ := store.ListAccounts()
	if len(accs) != 1 {
		t.Fatalf("%d accounts", len(accs))
	}
}

// xoauth2Server checks the one message XOAUTH2 consists of.
type xoauth2Server struct {
	conn imapserver.Conn
	want string
}

func (s *xoauth2Server) Next(resp []byte) ([]byte, bool, error) {
	if string(resp) != s.want {
		return nil, true, errors.New("invalid credentials")
	}
	user, err := memory.New().Login(s.conn.Info(), "username", "password")
	if err != nil {
		return nil, true, err
	}
	ctx := s.conn.Context()
	ctx.State, ctx.User = imap.AuthenticatedState, user
	return nil, true, nil
}

var _ sasl.Server = (*xoauth2Server)(nil)

func TestGmailSignsInWithXOAuth2(t *testing.T) {
	https := httptest.NewTLSServer(http.NotFoundHandler())
	defer https.Close()
	pool := x509.NewCertPool()
	pool.AddCert(https.Certificate())
	account.TestRoots = pool
	defer func() { account.TestRoots = nil }()

	srv := imapserver.New(memory.New())
	srv.EnableAuth("XOAUTH2", func(c imapserver.Conn) sasl.Server {
		return &xoauth2Server{conn: c, want: "user=ada@gmail.com\x01auth=Bearer good-token\x01\x01"}
	})
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: https.TLS.Certificates})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()

	acc := &account.Account{ID: "g", Email: "ada@gmail.com", User: "ada@gmail.com", Host: "127.0.0.1",
		Port: ln.Addr().(*net.TCPAddr).Port, SSL: true, Auth: account.AuthGoogle, Password: "rt", OAuthClientID: "cid"}
	tokens = map[string]cached{"g": {token: "good-token", expires: farFuture}}
	c, err := imapsync.ConnectIMAP(acc)
	if err != nil {
		t.Fatalf("XOAUTH2 sign-in: %v", err)
	}
	c.Logout()

	tokens = map[string]cached{"g": {token: "stale-token", expires: farFuture}}
	_, err = imapsync.ConnectIMAP(acc)
	if kind, _ := account.Classify(err, acc.Host); kind != account.ProblemAuth {
		t.Fatalf("a refused token should read as a sign-in problem: %v (%s)", err, kind)
	}
}

var farFuture = time.Now().Add(24 * time.Hour)
