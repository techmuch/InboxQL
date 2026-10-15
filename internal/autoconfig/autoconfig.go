// Package autoconfig works out an email address's mail servers.
//
// The nearest source first, stopping at the first that answers:
//
//  1. a built-in table of the large providers, by domain and by MX, so a
//     Google Workspace or Microsoft 365 domain is recognised as one;
//  2. the domain's own autoconfig file, in the format Thunderbird defined;
//  3. RFC 6186 SRV records;
//  4. Mozilla's ISP database — only when asked, because asking it tells
//     Mozilla the domain. The first three ask only the domain itself and DNS,
//     which the account is about to be connected to anyway.
//
// A server that offers only plaintext is left out rather than offered:
// InboxQL refuses to send a password unencrypted, so suggesting one would be
// suggesting a failure.
package autoconfig

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Server is one server's address. TLS means encrypted from the first byte
// (993, 465); otherwise the connection is upgraded with STARTTLS.
type Server struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	TLS  bool   `json:"tls"`
}

// Settings is what was found for an address.
type Settings struct {
	Provider string `json:"provider,omitempty"`
	// Source says where the answer came from: builtin, mx, autoconfig, srv,
	// ispdb.
	Source   string `json:"source"`
	IMAP     Server `json:"imap"`
	SMTP     Server `json:"smtp"`
	Username string `json:"username"`
	// Note is what the person should know before typing a password.
	Note string `json:"note,omitempty"`
	// PasswordURL is where an app password is made, for providers that need one.
	PasswordURL string `json:"passwordUrl,omitempty"`
	// Google says Sign in with Google will work for this address.
	Google bool `json:"google,omitempty"`
	// Unsupported, when set, says why this account cannot be connected with
	// a password at all.
	Unsupported string `json:"unsupported,omitempty"`
}

// Attempt is one place that was asked, and what it said.
type Attempt struct {
	Source string `json:"source"`
	Asked  string `json:"asked"`
	Result string `json:"result"`
}

// Options steer a lookup.
type Options struct {
	// ISPDB allows asking Mozilla's database, which tells Mozilla the domain.
	ISPDB bool
}

// ErrNotFound means nothing answered; the person enters the servers by hand.
var ErrNotFound = errors.New("no mail settings found for this domain")

// Replaceable for tests.
var (
	HTTPClient = &http.Client{Timeout: 8 * time.Second}
	LookupMX   = net.DefaultResolver.LookupMX
	LookupSRV  = net.DefaultResolver.LookupSRV
	// AutoconfigURLs are where a domain publishes its own settings.
	AutoconfigURLs = func(domain, email string) []string {
		q := url.Values{"emailaddress": {email}}.Encode()
		return []string{
			"https://autoconfig." + domain + "/mail/config-v1.1.xml?" + q,
			"https://" + domain + "/.well-known/autoconfig/mail/config-v1.1.xml?" + q,
		}
	}
	ISPDBBase = "https://autoconfig.thunderbird.net/v1.1/"
)

// Discover works out the servers for an address.
func Discover(ctx context.Context, email string, opt Options) (*Settings, []Attempt, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 || strings.ContainsAny(email, " /\\") {
		return nil, nil, fmt.Errorf("%q is not an email address", email)
	}
	domain := email[at+1:]
	var tried []Attempt

	if s := byDomain(domain); s != nil {
		tried = append(tried, Attempt{"builtin", domain, "known provider: " + s.Provider})
		return finish(s, "builtin", email), tried, nil
	}

	// MX names the provider for a custom domain on Google or Microsoft, which
	// publish no autoconfig of their own.
	mxs, err := LookupMX(ctx, domain)
	if err != nil {
		tried = append(tried, Attempt{"mx", domain, "no answer: " + shortErr(err)})
	} else {
		hosts := make([]string, 0, len(mxs))
		for _, mx := range mxs {
			hosts = append(hosts, strings.TrimSuffix(strings.ToLower(mx.Host), "."))
		}
		if s := byMX(hosts); s != nil {
			tried = append(tried, Attempt{"mx", domain, "mail is handled by " + s.Provider})
			return finish(s, "mx", email), tried, nil
		}
		tried = append(tried, Attempt{"mx", domain, "mail is handled by " + strings.Join(hosts, ", ")})
	}

	for _, u := range AutoconfigURLs(domain, email) {
		s, err := fetchConfig(ctx, u)
		if err != nil {
			tried = append(tried, Attempt{"autoconfig", redact(u), shortErr(err)})
			continue
		}
		tried = append(tried, Attempt{"autoconfig", redact(u), "found"})
		return finish(s, "autoconfig", email), tried, nil
	}

	if s, err := srv(ctx, domain); err == nil {
		tried = append(tried, Attempt{"srv", domain, "found"})
		return finish(s, "srv", email), tried, nil
	} else {
		tried = append(tried, Attempt{"srv", domain, shortErr(err)})
	}

	if opt.ISPDB {
		u := ISPDBBase + url.PathEscape(domain)
		s, err := fetchConfig(ctx, u)
		if err == nil {
			tried = append(tried, Attempt{"ispdb", u, "found"})
			return finish(s, "ispdb", email), tried, nil
		}
		tried = append(tried, Attempt{"ispdb", u, shortErr(err)})
	}
	return nil, tried, ErrNotFound
}

func finish(s *Settings, source, email string) *Settings {
	out := *s
	out.Source = source
	switch out.Username {
	case "", "%EMAILADDRESS%":
		out.Username = email
	case "%EMAILLOCALPART%":
		out.Username = email[:strings.LastIndex(email, "@")]
	}
	return &out
}

// redact keeps the address out of what is reported back.
func redact(u string) string {
	if i := strings.Index(u, "?"); i >= 0 {
		return u[:i]
	}
	return u
}

func shortErr(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		if dns.IsNotFound {
			return "nothing published"
		}
		return "DNS did not answer"
	}
	msg := err.Error()
	if len(msg) > 120 {
		msg = msg[:120] + "…"
	}
	return msg
}

// --- autoconfig XML ---------------------------------------------------------

type xmlServer struct {
	Type       string `xml:"type,attr"`
	Hostname   string `xml:"hostname"`
	Port       int    `xml:"port"`
	SocketType string `xml:"socketType"`
	Username   string `xml:"username"`
}

type xmlConfig struct {
	Provider struct {
		DisplayName string      `xml:"displayName"`
		Incoming    []xmlServer `xml:"incomingServer"`
		Outgoing    []xmlServer `xml:"outgoingServer"`
	} `xml:"emailProvider"`
}

func fetchConfig(ctx context.Context, u string) (*Settings, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		var dns *net.DNSError
		if errors.As(err, &dns) {
			return nil, err
		}
		return nil, errors.New("no answer")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nothing published (HTTP %d)", resp.StatusCode)
	}
	return parseConfig(io.LimitReader(resp.Body, 256<<10))
}

func parseConfig(r io.Reader) (*Settings, error) {
	var c xmlConfig
	if err := xml.NewDecoder(r).Decode(&c); err != nil {
		return nil, fmt.Errorf("not a mail configuration: %v", err)
	}
	in, ok := pick(c.Provider.Incoming, "imap")
	if !ok {
		return nil, errors.New("no encrypted IMAP server listed")
	}
	s := &Settings{Provider: c.Provider.DisplayName, IMAP: in.server(), Username: in.Username}
	if out, ok := pick(c.Provider.Outgoing, "smtp"); ok {
		s.SMTP = out.server()
	}
	return s, nil
}

func (x xmlServer) server() Server {
	return Server{Host: strings.TrimSpace(x.Hostname), Port: x.Port, TLS: strings.EqualFold(x.SocketType, "SSL")}
}

// pick chooses the best encrypted server of a type: implicit TLS before
// STARTTLS; plaintext never.
func pick(list []xmlServer, typ string) (xmlServer, bool) {
	var ok []xmlServer
	for _, s := range list {
		st := strings.ToUpper(s.SocketType)
		if strings.EqualFold(s.Type, typ) && s.Hostname != "" && s.Port > 0 && (st == "SSL" || st == "STARTTLS") {
			ok = append(ok, s)
		}
	}
	if len(ok) == 0 {
		return xmlServer{}, false
	}
	sort.SliceStable(ok, func(i, j int) bool {
		return strings.EqualFold(ok[i].SocketType, "SSL") && !strings.EqualFold(ok[j].SocketType, "SSL")
	})
	return ok[0], true
}

// --- SRV --------------------------------------------------------------------

func srv(ctx context.Context, domain string) (*Settings, error) {
	first := func(service string) (Server, bool) {
		_, addrs, err := LookupSRV(ctx, service, "tcp", domain)
		if err != nil || len(addrs) == 0 {
			return Server{}, false
		}
		a := addrs[0]
		host := strings.TrimSuffix(a.Target, ".")
		if host == "" { // "." means the service is deliberately not offered
			return Server{}, false
		}
		return Server{Host: host, Port: int(a.Port), TLS: service == "imaps" || service == "submissions"}, true
	}
	in, ok := first("imaps")
	if !ok {
		in, ok = first("imap")
	}
	if !ok {
		return nil, errors.New("no IMAP SRV record")
	}
	s := &Settings{IMAP: in}
	if out, ok := first("submissions"); ok {
		s.SMTP = out
	} else if out, ok := first("submission"); ok {
		s.SMTP = out
	}
	return s, nil
}
