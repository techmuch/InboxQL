package autoconfig

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDNS answers nothing unless told; no test reaches a real resolver.
func fakeDNS(t *testing.T, mx map[string][]string, srvs map[string]*net.SRV) {
	t.Helper()
	oldMX, oldSRV, oldURLs, oldBase := LookupMX, LookupSRV, AutoconfigURLs, ISPDBBase
	t.Cleanup(func() { LookupMX, LookupSRV, AutoconfigURLs, ISPDBBase = oldMX, oldSRV, oldURLs, oldBase })
	notFound := &net.DNSError{Err: "no such host", IsNotFound: true}
	LookupMX = func(_ context.Context, d string) ([]*net.MX, error) {
		var out []*net.MX
		for _, h := range mx[d] {
			out = append(out, &net.MX{Host: h + "."})
		}
		if out == nil {
			return nil, notFound
		}
		return out, nil
	}
	LookupSRV = func(_ context.Context, service, _ string, d string) (string, []*net.SRV, error) {
		if s, ok := srvs["_"+service+"._tcp."+d]; ok {
			return "", []*net.SRV{s}, nil
		}
		return "", nil, notFound
	}
	AutoconfigURLs = func(string, string) []string { return nil }
}

const config = `<?xml version="1.0"?>
<clientConfig version="1.1"><emailProvider id="example.org">
  <displayName>Example Mail</displayName>
  <incomingServer type="imap"><hostname>plain.example.org</hostname><port>143</port><socketType>plain</socketType></incomingServer>
  <incomingServer type="imap"><hostname>mail.example.org</hostname><port>143</port><socketType>STARTTLS</socketType>
    <username>%EMAILLOCALPART%</username></incomingServer>
  <incomingServer type="imap"><hostname>imap.example.org</hostname><port>993</port><socketType>SSL</socketType>
    <username>%EMAILLOCALPART%</username></incomingServer>
  <outgoingServer type="smtp"><hostname>smtp.example.org</hostname><port>587</port><socketType>STARTTLS</socketType></outgoingServer>
</emailProvider></clientConfig>`

func TestKnownProviderNeedsNoLookup(t *testing.T) {
	fakeDNS(t, nil, nil)
	LookupMX = func(context.Context, string) ([]*net.MX, error) {
		t.Fatal("asked DNS for a known domain")
		return nil, nil
	}
	s, _, err := Discover(context.Background(), "Ada@Gmail.com", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.IMAP.Host != "imap.gmail.com" || !s.Google || s.Username != "ada@gmail.com" || s.Source != "builtin" {
		t.Fatalf("got %+v", s)
	}
}

func TestCustomDomainRecognisedByMX(t *testing.T) {
	fakeDNS(t, map[string][]string{
		"acme.com":  {"aspmx.l.google.com"},
		"corp.com":  {"corp-com.mail.protection.outlook.com"},
		"other.org": {"mx.other.org"},
	}, nil)
	s, _, err := Discover(context.Background(), "bob@acme.com", Options{})
	if err != nil || s.Provider != "Google Workspace" || !s.Google || s.Source != "mx" {
		t.Fatalf("workspace: %+v %v", s, err)
	}
	s, _, err = Discover(context.Background(), "bob@corp.com", Options{})
	if err != nil || s.Unsupported == "" {
		t.Fatalf("microsoft 365 should be named and refused: %+v %v", s, err)
	}
	if _, tried, err := Discover(context.Background(), "bob@other.org", Options{}); !errors.Is(err, ErrNotFound) || len(tried) == 0 {
		t.Fatalf("unknown domain: %v %v", tried, err)
	}
}

func TestDomainsOwnConfigPrefersTLSAndRefusesPlaintext(t *testing.T) {
	fakeDNS(t, nil, nil)
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		asked = r.URL.RawQuery
		w.Write([]byte(config))
	}))
	defer srv.Close()
	AutoconfigURLs = func(_, email string) []string {
		return []string{srv.URL + "/missing", srv.URL + "/mail/config-v1.1.xml?emailaddress=" + email}
	}
	s, tried, err := Discover(context.Background(), "carol@example.org", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.IMAP != (Server{"imap.example.org", 993, true}) || s.SMTP != (Server{"smtp.example.org", 587, false}) {
		t.Fatalf("servers: %+v", s)
	}
	if s.Username != "carol" || s.Source != "autoconfig" || s.Provider != "Example Mail" {
		t.Fatalf("got %+v", s)
	}
	if !strings.Contains(asked, "carol") {
		t.Fatalf("the domain's own server is asked with the address: %q", asked)
	}
	for _, a := range tried {
		if strings.Contains(a.Asked, "carol") {
			t.Fatalf("the address is kept out of the report: %+v", a)
		}
	}
}

func TestSRVRecords(t *testing.T) {
	fakeDNS(t, nil, map[string]*net.SRV{
		"_imaps._tcp.srv.net":        {Target: "imap.srv.net.", Port: 993},
		"_submission._tcp.srv.net":   {Target: "smtp.srv.net.", Port: 587},
		"_submissions._tcp.none.net": {Target: ".", Port: 0},
	})
	s, _, err := Discover(context.Background(), "d@srv.net", Options{})
	if err != nil || s.IMAP != (Server{"imap.srv.net", 993, true}) || s.SMTP != (Server{"smtp.srv.net", 587, false}) || s.Source != "srv" {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestMozillaIsAskedOnlyWhenAllowed(t *testing.T) {
	fakeDNS(t, nil, nil)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(config))
	}))
	defer srv.Close()
	ISPDBBase = srv.URL + "/v1.1/"
	if _, _, err := Discover(context.Background(), "e@isp.net", Options{}); !errors.Is(err, ErrNotFound) || hits != 0 {
		t.Fatalf("without permission: err %v, hits %d", err, hits)
	}
	s, _, err := Discover(context.Background(), "e@isp.net", Options{ISPDB: true})
	if err != nil || hits != 1 || s.Source != "ispdb" {
		t.Fatalf("with permission: %+v %v %d", s, err, hits)
	}
}

func TestNotAnAddress(t *testing.T) {
	for _, bad := range []string{"", "nobody", "@x.com", "a@", "a b@x.com"} {
		if _, _, err := Discover(context.Background(), bad, Options{}); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
