package avatar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gravatar(t *testing.T) (*Store, *[]string) {
	t.Helper()
	asked := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.String())
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case Hash("alice@example.com"):
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG fake"))
		case Hash("svg@example.com"):
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte("<svg/>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	s := New(t.TempDir())
	s.Base = srv.URL + "/"
	return s, &asked
}

func TestFetchStoresThePicture(t *testing.T) {
	s, asked := gravatar(t)
	m, err := s.FetchGravatar(context.Background(), "  Alice@Example.com ")
	if err != nil || m.Status != Stored || m.ContentType != "image/png" {
		t.Fatalf("%+v %v", m, err)
	}
	if !strings.Contains((*asked)[0], "d=404") || !strings.Contains((*asked)[0], Hash("alice@example.com")) {
		t.Errorf("asked %v", *asked)
	}
	b, meta, err := s.Image("alice@example.com")
	if err != nil || string(b) != "\x89PNG fake" || meta.Source != "gravatar" {
		t.Errorf("image %q %+v %v", b, meta, err)
	}
	// Served from the store after that: nothing more is asked.
	s.Image("alice@example.com")
	if len(*asked) != 1 {
		t.Errorf("asked Gravatar %d times", len(*asked))
	}
}

func TestNoGravatarIsRemembered(t *testing.T) {
	s, _ := gravatar(t)
	m, err := s.FetchGravatar(context.Background(), "nobody@example.com")
	if err != nil || m.Status != None {
		t.Fatalf("%+v %v", m, err)
	}
	if m, _ := s.Meta("nobody@example.com"); m.Status != None {
		t.Errorf("meta %+v", m)
	}
	if err := s.Remove("nobody@example.com"); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.Meta("nobody@example.com"); m.Status != Absent {
		t.Errorf("after remove %+v", m)
	}
}

func TestRefusals(t *testing.T) {
	s, asked := gravatar(t)
	for _, bad := range []string{"", "not-an-address", "a@b.com/../x", "Alice <a@b.com>", "a b@c.com"} {
		if _, err := s.FetchGravatar(context.Background(), bad); err == nil {
			t.Errorf("%q was fetched", bad)
		}
	}
	if _, err := s.FetchGravatar(context.Background(), "svg@example.com"); err == nil {
		t.Error("an SVG was kept")
	}
	if len(*asked) != 1 {
		t.Errorf("asked %d times, want only the SVG one", len(*asked))
	}
}
