// Package gcal is Google Calendar, as a calendsync provider: the calendar
// list, incremental sync with Google's sync tokens, and changes going up with
// ETags so an edit made elsewhere is noticed rather than overwritten.
//
// It speaks the Calendar API, not Google's CalDAV, because the things people
// use Google Calendar for — Meet links, working location, focus time, event
// types — exist only there.
package gcal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/store"
)

// Endpoints, overridable for tests.
var (
	AuthURL  = envOr("INBOXQL_GOOGLE_AUTH_URL", "https://accounts.google.com/o/oauth2/v2/auth")
	TokenURL = envOr("INBOXQL_GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token")
	APIBase  = envOr("INBOXQL_GOOGLE_API_URL", "https://www.googleapis.com/calendar/v3")
	UserInfo = envOr("INBOXQL_GOOGLE_USERINFO_URL", "https://openidconnect.googleapis.com/v1/userinfo")
)

// Scopes asked for: the calendar, and the address of the account, so the
// account can be named and its invitations recognised as "me".
const Scopes = "https://www.googleapis.com/auth/calendar openid email"

// SyncWindow is how far back the first sync of a calendar reaches. Later
// syncs are incremental and carry whatever changes, however old.
var SyncWindow = 365 * 24 * time.Hour

func envOr(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

var client = &http.Client{Timeout: 60 * time.Second}

// Provider is one Google account.
type Provider struct {
	acct store.CalendarAccount

	mu      sync.Mutex
	access  string
	expires time.Time
}

// New is the provider for an account; registered with the engine as "google".
func New(acct store.CalendarAccount) (calsync.Provider, error) {
	if acct.Config["clientId"] == "" {
		return nil, errors.New("this Google account has no client ID; connect it again in Settings → Calendars")
	}
	return &Provider{acct: acct}, nil
}

// --- tokens -----------------------------------------------------------------

// Token is what Google's token endpoint returns.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// Exchange trades an authorisation code for tokens, with the PKCE verifier.
func Exchange(ctx context.Context, clientID, clientSecret, code, verifier, redirect string) (*Token, error) {
	return tokenRequest(ctx, url.Values{"client_id": {clientID}, "client_secret": {clientSecret}, "code": {code},
		"code_verifier": {verifier}, "redirect_uri": {redirect}, "grant_type": {"authorization_code"}})
}

func tokenRequest(ctx context.Context, form url.Values) (*Token, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching Google: %w", err)
	}
	defer resp.Body.Close()
	var t Token
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("Google's token answer: %w", err)
	}
	if t.Error != "" {
		if t.Error == "invalid_grant" {
			return nil, errors.New("Google no longer accepts this sign-in (revoked, expired or the password changed); connect the account again")
		}
		return nil, fmt.Errorf("Google refused: %s %s", t.Error, t.Description)
	}
	return &t, nil
}

// Email reads the account's address with an access token.
func Email(ctx context.Context, access string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, UserInfo, nil)
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var u struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", err
	}
	return strings.ToLower(u.Email), nil
}

func (p *Provider) token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.access != "" && time.Until(p.expires) > time.Minute {
		return p.access, nil
	}
	refresh, err := store.CalendarAccountSecret(p.acct.ID)
	if err != nil || refresh == "" {
		return "", errors.New("this Google account is not signed in; connect it again in Settings → Calendars")
	}
	t, err := tokenRequest(ctx, url.Values{"client_id": {p.acct.Config["clientId"]},
		"client_secret": {p.acct.Config["clientSecret"]}, "refresh_token": {refresh}, "grant_type": {"refresh_token"}})
	if err != nil {
		return "", err
	}
	p.access, p.expires = t.AccessToken, time.Now().Add(time.Duration(t.ExpiresIn)*time.Second)
	return p.access, nil
}

// apiError is a non-2xx answer.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	var g struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(e.Body), &g) == nil && g.Error.Message != "" {
		return fmt.Sprintf("Google: %s (%d)", g.Error.Message, e.Status)
	}
	return fmt.Sprintf("Google answered %d", e.Status)
}

func (p *Provider) do(ctx context.Context, method, path string, query url.Values, body any, etag string, out any) error {
	tok, err := p.token(ctx)
	if err != nil {
		return err
	}
	u := APIBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, u, rd)
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reaching Google: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	switch {
	case resp.StatusCode == http.StatusPreconditionFailed:
		return calsync.ErrConflict
	case resp.StatusCode == http.StatusGone:
		return calsync.ErrExpiredToken
	case resp.StatusCode >= 300:
		return &apiError{Status: resp.StatusCode, Body: string(data)}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// --- calendars --------------------------------------------------------------

// Calendars lists the account's calendars.
func (p *Provider) Calendars(ctx context.Context) ([]store.CalendarInfo, error) {
	var out []store.CalendarInfo
	page := ""
	for {
		q := url.Values{"maxResults": {"250"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var res struct {
			Items []struct {
				ID              string `json:"id"`
				Summary         string `json:"summary"`
				SummaryOverride string `json:"summaryOverride"`
				BackgroundColor string `json:"backgroundColor"`
				TimeZone        string `json:"timeZone"`
				AccessRole      string `json:"accessRole"`
				Primary         bool   `json:"primary"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := p.do(ctx, http.MethodGet, "/users/me/calendarList", q, nil, "", &res); err != nil {
			return nil, err
		}
		for _, c := range res.Items {
			if c.AccessRole == "freeBusyReader" {
				// Busy blocks without details: nothing here could show them.
				continue
			}
			name := c.Summary
			if c.SummaryOverride != "" {
				name = c.SummaryOverride
			}
			access := "reader"
			switch c.AccessRole {
			case "owner":
				access = "owner"
			case "writer":
				access = "writer"
			}
			out = append(out, store.CalendarInfo{RemoteID: c.ID, Name: name, Color: c.BackgroundColor,
				TimeZone: c.TimeZone, Access: access, Primary: c.Primary})
		}
		if res.NextPageToken == "" {
			return out, nil
		}
		page = res.NextPageToken
	}
}

// --- events -----------------------------------------------------------------

type gTime struct {
	Date     string `json:"date,omitempty"`
	DateTime string `json:"dateTime,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type gAttendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName,omitempty"`
	Optional       bool   `json:"optional,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Self           bool   `json:"self,omitempty"`
	Organizer      bool   `json:"organizer,omitempty"`
	Comment        string `json:"comment,omitempty"`
}

// gEvent is the slice of Google's event resource this program reads and writes.
type gEvent struct {
	ID                string      `json:"id,omitempty"`
	ICalUID           string      `json:"iCalUID,omitempty"`
	ETag              string      `json:"etag,omitempty"`
	Status            string      `json:"status,omitempty"`
	Summary           string      `json:"summary,omitempty"`
	Description       string      `json:"description,omitempty"`
	Location          string      `json:"location,omitempty"`
	Start             *gTime      `json:"start,omitempty"`
	End               *gTime      `json:"end,omitempty"`
	Recurrence        []string    `json:"recurrence,omitempty"`
	RecurringEventID  string      `json:"recurringEventId,omitempty"`
	OriginalStartTime *gTime      `json:"originalStartTime,omitempty"`
	Transparency      string      `json:"transparency,omitempty"`
	Visibility        string      `json:"visibility,omitempty"`
	Attendees         []gAttendee `json:"attendees,omitempty"`
	Organizer         *struct {
		Email string `json:"email"`
		Self  bool   `json:"self"`
	} `json:"organizer,omitempty"`
	Sequence       int    `json:"sequence,omitempty"`
	HangoutLink    string `json:"hangoutLink,omitempty"`
	ConferenceData *struct {
		EntryPoints []struct {
			EntryPointType string `json:"entryPointType"`
			URI            string `json:"uri"`
		} `json:"entryPoints"`
	} `json:"conferenceData,omitempty"`
	Reminders *struct {
		UseDefault bool `json:"useDefault"`
		Overrides  []struct {
			Method  string `json:"method"`
			Minutes int    `json:"minutes"`
		} `json:"overrides,omitempty"`
	} `json:"reminders,omitempty"`
}

func parseGTime(t *gTime) (time.Time, bool, string, error) {
	if t == nil {
		return time.Time{}, false, "", errors.New("no time")
	}
	if t.Date != "" {
		d, err := time.ParseInLocation("2006-01-02", t.Date, time.UTC)
		return d, true, "", err
	}
	v, err := time.Parse(time.RFC3339, t.DateTime)
	if err != nil {
		return v, false, "", err
	}
	tz := ""
	if t.TimeZone != "" {
		if loc, ok := calendar.LoadZone(t.TimeZone); ok {
			tz = loc.String()
			v = v.In(loc)
		}
	}
	return v.UTC(), false, tz, nil
}

// toModel turns Google's event into the calendar model.
func toModel(g gEvent, calendarTZ string) (calsync.RemoteEvent, error) {
	ev := calendar.Event{UID: g.ICalUID, Summary: g.Summary, Description: g.Description, Location: g.Location,
		Status: g.Status, ShowAs: "busy", Sequence: g.Sequence}
	if ev.UID == "" {
		ev.UID = g.ID
	}
	if ev.Status == "" {
		ev.Status = "confirmed"
	}
	if g.Transparency == "transparent" {
		ev.ShowAs = "free"
	}
	ev.Private = g.Visibility == "private" || g.Visibility == "confidential"
	var err error
	if g.Status != "cancelled" || g.Start != nil {
		if ev.Start, ev.AllDay, ev.TZ, err = parseGTime(g.Start); err != nil {
			return calsync.RemoteEvent{}, fmt.Errorf("event %s: start: %w", g.ID, err)
		}
		if ev.End, _, _, err = parseGTime(g.End); err != nil {
			ev.End = ev.Start
		}
	}
	if g.RecurringEventID != "" && g.OriginalStartTime != nil {
		if ev.RecurrenceID, _, _, err = parseGTime(g.OriginalStartTime); err != nil {
			return calsync.RemoteEvent{}, fmt.Errorf("event %s: original start: %w", g.ID, err)
		}
		if ev.Start.IsZero() {
			// A cancelled instance carries only its original start.
			ev.Start, ev.End = ev.RecurrenceID, ev.RecurrenceID
		}
	}
	tz := ev.TZ
	if tz == "" {
		tz = calendarTZ
	}
	ev.Recurrence = normaliseRecurrence(g.Recurrence, tz)
	if ev.TZ == "" && len(ev.Recurrence) > 0 && !ev.AllDay {
		ev.TZ = tz
	}
	switch {
	case g.HangoutLink != "":
		ev.ConferenceURL = g.HangoutLink
	case g.ConferenceData != nil:
		for _, ep := range g.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" {
				ev.ConferenceURL = ep.URI
				break
			}
		}
	}
	myStatus := ""
	if g.Organizer != nil {
		ev.Organizer = strings.ToLower(g.Organizer.Email)
		if g.Organizer.Self {
			myStatus = "organizer"
		}
	}
	for _, a := range g.Attendees {
		st := map[string]string{"needsAction": "needs-action", "accepted": "accepted", "declined": "declined",
			"tentative": "tentative"}[a.ResponseStatus]
		if st == "" {
			st = "needs-action"
		}
		role := "required"
		if a.Optional {
			role = "optional"
		}
		ev.Attendees = append(ev.Attendees, calendar.Attendee{Address: strings.ToLower(a.Email), Name: a.DisplayName, Role: role, Status: st})
		if a.Self && myStatus != "organizer" {
			myStatus = st
		}
	}
	if g.Reminders != nil {
		for _, o := range g.Reminders.Overrides {
			if o.Method == "popup" {
				ev.Alarms = append(ev.Alarms, o.Minutes)
			}
		}
	}
	raw, _ := json.Marshal(g)
	return calsync.RemoteEvent{Event: ev, RemoteID: g.ID, ETag: g.ETag, MyStatus: myStatus, Raw: string(raw)}, nil
}

// normaliseRecurrence turns Google's recurrence lines — RRULE, and EXDATE or
// RDATE with or without a zone — into the model's form: rules as they are,
// dates as UTC instants.
func normaliseRecurrence(lines []string, tz string) []string {
	if len(lines) == 0 {
		return nil
	}
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:x\r\nDTSTART:20000101T000000Z\r\n" +
		strings.Join(lines, "\r\n") + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if tz != "" {
		// A date with no zone is in the event's zone; say so to the parser.
		var fixed []string
		for _, l := range lines {
			name, value, _ := strings.Cut(l, ":")
			if (strings.HasPrefix(name, "EXDATE") || strings.HasPrefix(name, "RDATE")) && !strings.Contains(name, "TZID") &&
				!strings.HasSuffix(value, "Z") && !strings.Contains(name, "VALUE=DATE") {
				name += ";TZID=" + tz
			}
			fixed = append(fixed, name+":"+value)
		}
		ics = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:x\r\nDTSTART:20000101T000000Z\r\n" +
			strings.Join(fixed, "\r\n") + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	}
	cal, err := calendar.Parse([]byte(ics))
	if err != nil || len(cal.Events) == 0 {
		return lines
	}
	return cal.Events[0].Recurrence
}

// Pull returns what changed in a calendar since token.
func (p *Provider) Pull(ctx context.Context, cal store.CalendarInfo, token string) (*calsync.PullResult, error) {
	res := &calsync.PullResult{Full: token == ""}
	page := ""
	path := "/calendars/" + url.PathEscape(cal.RemoteID) + "/events"
	for {
		q := url.Values{"maxResults": {"2500"}}
		if token != "" {
			q.Set("syncToken", token)
		} else {
			q.Set("timeMin", time.Now().Add(-SyncWindow).Format(time.RFC3339))
		}
		if page != "" {
			q.Set("pageToken", page)
		}
		var out struct {
			Items         []gEvent `json:"items"`
			NextPageToken string   `json:"nextPageToken"`
			NextSyncToken string   `json:"nextSyncToken"`
		}
		if err := p.do(ctx, http.MethodGet, path, q, nil, "", &out); err != nil {
			return nil, err
		}
		for _, g := range out.Items {
			if g.Status == "cancelled" && g.RecurringEventID == "" {
				res.Deletes = append(res.Deletes, g.ID)
				continue
			}
			re, err := toModel(g, cal.TimeZone)
			if err != nil {
				continue
			}
			res.Upserts = append(res.Upserts, re)
		}
		if out.NextPageToken == "" {
			res.NextToken = out.NextSyncToken
			return res, nil
		}
		page = out.NextPageToken
	}
}

// fromModel is the event resource to send.
func fromModel(ev calendar.Event) gEvent {
	g := gEvent{Summary: ev.Summary, Description: ev.Description, Location: ev.Location, Recurrence: denormalise(ev),
		Status: ev.Status}
	if ev.Status == "" {
		g.Status = "confirmed"
	}
	if ev.AllDay {
		g.Start = &gTime{Date: ev.Start.UTC().Format("2006-01-02")}
		g.End = &gTime{Date: ev.End.UTC().Format("2006-01-02")}
	} else {
		tz := ev.TZ
		if tz == "" {
			tz = "UTC"
		}
		loc, _ := calendar.LoadZone(tz)
		if loc == nil {
			loc = time.UTC
		}
		g.Start = &gTime{DateTime: ev.Start.In(loc).Format(time.RFC3339), TimeZone: loc.String()}
		g.End = &gTime{DateTime: ev.End.In(loc).Format(time.RFC3339), TimeZone: loc.String()}
	}
	if ev.ShowAs == "free" {
		g.Transparency = "transparent"
	} else {
		g.Transparency = "opaque"
	}
	if ev.Private {
		g.Visibility = "private"
	} else {
		g.Visibility = "default"
	}
	for _, a := range ev.Attendees {
		st := map[string]string{"needs-action": "needsAction", "accepted": "accepted", "declined": "declined",
			"tentative": "tentative"}[a.Status]
		g.Attendees = append(g.Attendees, gAttendee{Email: a.Address, DisplayName: a.Name, Optional: a.Role == "optional",
			ResponseStatus: st})
	}
	g.Reminders = &struct {
		UseDefault bool `json:"useDefault"`
		Overrides  []struct {
			Method  string `json:"method"`
			Minutes int    `json:"minutes"`
		} `json:"overrides,omitempty"`
	}{}
	for _, m := range ev.Alarms {
		g.Reminders.Overrides = append(g.Reminders.Overrides, struct {
			Method  string `json:"method"`
			Minutes int    `json:"minutes"`
		}{"popup", m})
	}
	g.Reminders.UseDefault = len(ev.Alarms) == 0
	return g
}

// denormalise writes the model's recurrence the way Google reads it: rules
// as they are, excluded and added dates in UTC.
func denormalise(ev calendar.Event) []string {
	var out []string
	for _, l := range ev.Recurrence {
		name, value, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		if ev.AllDay && (name == "EXDATE" || name == "RDATE") {
			var dates []string
			for _, v := range strings.Split(value, ",") {
				if t, err := time.Parse("20060102T150405Z", v); err == nil {
					dates = append(dates, t.Format("20060102"))
				}
			}
			out = append(out, name+";VALUE=DATE:"+strings.Join(dates, ","))
			continue
		}
		out = append(out, l)
	}
	return out
}

// instanceID finds the provider's id of one occurrence of a series, by its
// original start. Google does not document the form instance ids take, so
// it is asked rather than built.
func (p *Provider) instanceID(ctx context.Context, calID, masterID string, original time.Time, allDay bool) (string, string, error) {
	q := url.Values{"maxResults": {"5"}}
	if allDay {
		q.Set("originalStart", original.UTC().Format("2006-01-02"))
	} else {
		q.Set("originalStart", original.UTC().Format(time.RFC3339))
	}
	var out struct {
		Items []gEvent `json:"items"`
	}
	if err := p.do(ctx, http.MethodGet, "/calendars/"+url.PathEscape(calID)+"/events/"+url.PathEscape(masterID)+"/instances", q, nil, "", &out); err != nil {
		return "", "", err
	}
	for _, it := range out.Items {
		if it.OriginalStartTime != nil {
			if t, _, _, err := parseGTime(it.OriginalStartTime); err == nil && t.Equal(original.UTC()) {
				return it.ID, it.ETag, nil
			}
		}
	}
	if len(out.Items) == 1 {
		return out.Items[0].ID, out.Items[0].ETag, nil
	}
	return "", "", fmt.Errorf("Google has no occurrence of this event at %s", original.Format(time.RFC3339))
}

// target is the provider id (and ETag) an event change applies to.
func (p *Provider) target(ctx context.Context, cal store.CalendarInfo, ev *store.StoredEvent) (string, string, error) {
	if ev.RemoteID != "" {
		return ev.RemoteID, ev.ETag, nil
	}
	if ev.RecurrenceID != nil {
		copies, err := store.EventsByUID(cal.ID, ev.UID)
		if err != nil {
			return "", "", err
		}
		for _, c := range copies {
			if c.RecurrenceID == nil && c.RemoteID != "" {
				return p.instanceID(ctx, cal.RemoteID, c.RemoteID, *ev.RecurrenceID, ev.AllDay)
			}
		}
	}
	return "", "", errors.New("this event has not reached Google yet")
}

// Push sends one change.
func (p *Provider) Push(ctx context.Context, cal store.CalendarInfo, ch store.CalendarChange, ev *store.StoredEvent) (*calsync.PushResult, error) {
	base := "/calendars/" + url.PathEscape(cal.RemoteID) + "/events"
	// Google sends the invitations and updates to attendees itself.
	q := url.Values{"sendUpdates": {"all"}}
	switch ch.Op {
	case "create":
		model := ev.Model()
		body := fromModel(model)
		body.ICalUID = model.UID
		if ev.RecurrenceID != nil {
			return p.updateInstance(ctx, cal, ev, body, q)
		}
		var out gEvent
		if err := p.do(ctx, http.MethodPost, base, q, body, "", &out); err != nil {
			return nil, err
		}
		return result(out, cal)
	case "update":
		body := fromModel(ev.Model())
		if ev.RecurrenceID != nil && ev.RemoteID == "" {
			return p.updateInstance(ctx, cal, ev, body, q)
		}
		id, etag, err := p.target(ctx, cal, ev)
		if err != nil {
			return nil, err
		}
		var out gEvent
		if err := p.do(ctx, http.MethodPut, base+"/"+url.PathEscape(id), q, body, etag, &out); err != nil {
			return nil, err
		}
		return result(out, cal)
	case "delete":
		id, etag, err := p.target(ctx, cal, ev)
		if err != nil {
			return nil, err
		}
		err = p.do(ctx, http.MethodDelete, base+"/"+url.PathEscape(id), q, nil, etag, nil)
		var ae *apiError
		if errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusGone) {
			return &calsync.PushResult{}, nil // already gone
		}
		if err != nil {
			return nil, err
		}
		return &calsync.PushResult{}, nil
	case "rsvp":
		r, err := calsync.DecodeRSVP(ch)
		if err != nil {
			return nil, err
		}
		id, _, err := p.target(ctx, cal, ev)
		if err != nil {
			return nil, err
		}
		// The whole attendee list, with only your own answer changed: PATCH
		// replaces the list, and leaving anyone out would uninvite them.
		var current gEvent
		if err := p.do(ctx, http.MethodGet, base+"/"+url.PathEscape(id), nil, nil, "", &current); err != nil {
			return nil, err
		}
		found := false
		st := map[string]string{"accepted": "accepted", "declined": "declined", "tentative": "tentative"}[r.Status]
		for i := range current.Attendees {
			if current.Attendees[i].Self {
				current.Attendees[i].ResponseStatus = st
				current.Attendees[i].Comment = r.Comment
				found = true
			}
		}
		if !found {
			return nil, errors.New("you are not on this event's list of attendees in Google")
		}
		var out gEvent
		if err := p.do(ctx, http.MethodPatch, base+"/"+url.PathEscape(id), q,
			map[string]any{"attendees": current.Attendees}, "", &out); err != nil {
			return nil, err
		}
		return result(out, cal)
	}
	return nil, fmt.Errorf("unknown change %q", ch.Op)
}

func (p *Provider) updateInstance(ctx context.Context, cal store.CalendarInfo, ev *store.StoredEvent, body gEvent, q url.Values) (*calsync.PushResult, error) {
	id, etag, err := p.target(ctx, cal, ev)
	if err != nil {
		return nil, err
	}
	body.Recurrence = nil
	var out gEvent
	if err := p.do(ctx, http.MethodPut, "/calendars/"+url.PathEscape(cal.RemoteID)+"/events/"+url.PathEscape(id), q, body, etag, &out); err != nil {
		return nil, err
	}
	return result(out, cal)
}

func result(g gEvent, cal store.CalendarInfo) (*calsync.PushResult, error) {
	re, err := toModel(g, cal.TimeZone)
	if err != nil {
		return &calsync.PushResult{RemoteID: g.ID, ETag: g.ETag}, nil
	}
	return &calsync.PushResult{RemoteID: g.ID, ETag: g.ETag, Event: &re.Event}, nil
}
