// Package ews is Exchange Server, over Exchange Web Services, as a calsync
// provider.
//
// # On-premises only
//
// Microsoft is retiring EWS in Exchange Online (blocked from 1 October 2026,
// gone by 1 April 2027), and keeping it, fully supported, in Exchange Server.
// This is for the second: a server an organisation runs itself. Exchange
// Online needs Microsoft Graph, which is another provider.
//
// # Reading
//
// SyncFolderItems says what changed since the last sync state; GetItem then
// asks for each changed item's MimeContent, which for a calendar item is its
// iCalendar — series, exceptions, attendees and all. So an Exchange event is
// parsed by the same code as an invitation in mail, and the Windows zone
// names Exchange writes are already understood there.
//
// # Writing
//
// Creates and edits are EWS item operations, with the item's ChangeKey as
// the guard against overwriting someone else's edit. Exchange sends the
// invitations, updates and cancellations itself.
package ews

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	ntlmssp "github.com/Azure/go-ntlmssp"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/calsync"
	"github.com/user/inboxql/internal/store"
)

// Provider is one Exchange mailbox.
type Provider struct {
	acct     store.CalendarAccount
	endpoint string
	user     string
	password string
	client   *http.Client
}

// New is the provider for an account; registered with the engine as "ews".
func New(acct store.CalendarAccount) (calsync.Provider, error) {
	pw, err := store.CalendarAccountSecret(acct.ID)
	if err != nil {
		return nil, err
	}
	return newProvider(acct, pw)
}

func newProvider(acct store.CalendarAccount, password string) (*Provider, error) {
	ep := strings.TrimSpace(acct.Config["url"])
	u, err := url.Parse(ep)
	if err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && acct.Config["allowHTTP"] == "1")) || u.Host == "" {
		return nil, errors.New("the Exchange address must be https://…/EWS/Exchange.asmx")
	}
	var rt http.RoundTripper = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		Proxy: http.ProxyFromEnvironment}
	if acct.Config["auth"] != "basic" {
		rt = ntlmssp.Negotiator{RoundTripper: rt}
	}
	return &Provider{acct: acct, endpoint: ep, user: acct.Config["username"], password: password,
		client: &http.Client{Timeout: 90 * time.Second, Transport: rt}}, nil
}

// Check signs in and reads the calendar folder: what connecting tests.
func Check(ctx context.Context, acct store.CalendarAccount, password string) error {
	p, err := newProvider(acct, password)
	if err != nil {
		return err
	}
	_, err = p.Calendars(ctx)
	return err
}

// --- SOAP -------------------------------------------------------------------

const envelope = `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
 xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types"
 xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
<soap:Header><t:RequestServerVersion Version="Exchange2010_SP2"/></soap:Header>
<soap:Body>%s</soap:Body></soap:Envelope>`

// ErrAuth is a sign-in the server refused.
var ErrAuth = errors.New("Exchange refused the sign-in: check the username, password and sign-in method")

func (p *Provider) call(ctx context.Context, body string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, strings.NewReader(fmt.Sprintf(envelope, body)))
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.SetBasicAuth(p.user, p.password) // Negotiator turns this into NTLM when the server asks for it
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("reaching Exchange: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrAuth
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusInternalServerError {
		return fmt.Errorf("Exchange answered %d", resp.StatusCode)
	}
	var env struct {
		Body struct {
			Inner []byte `xml:",innerxml"`
			Fault *struct {
				String string `xml:"faultstring"`
			} `xml:"Fault"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("Exchange's answer was not SOAP: %w", err)
	}
	if env.Body.Fault != nil {
		return fmt.Errorf("Exchange: %s", env.Body.Fault.String)
	}
	if out != nil {
		return xml.Unmarshal(env.Body.Inner, out)
	}
	return nil
}

// responseMessage is the status every EWS response message carries.
type responseMessage struct {
	Class string `xml:"ResponseClass,attr"`
	Code  string `xml:"ResponseCode"`
	Text  string `xml:"MessageText"`
}

func (r responseMessage) err() error {
	if r.Class == "Success" || r.Class == "" {
		return nil
	}
	switch r.Code {
	case "ErrorIrresolvableConflict", "ErrorChangeKeyRequiredForWriteOperations", "ErrorStaleObject":
		return calsync.ErrConflict
	case "ErrorInvalidSyncStateData":
		return calsync.ErrExpiredToken
	case "ErrorItemNotFound":
		return errNotFound
	}
	return fmt.Errorf("Exchange: %s (%s)", r.Text, r.Code)
}

var errNotFound = errors.New("Exchange has no such item")

func esc(s string) string { return html.EscapeString(s) }

// --- calendars --------------------------------------------------------------

type folder struct {
	ID          itemID `xml:"FolderId"`
	DisplayName string `xml:"DisplayName"`
}

// Calendars lists the default calendar and the calendars filed under it.
func (p *Provider) Calendars(ctx context.Context) ([]store.CalendarInfo, error) {
	var got struct {
		Messages []struct {
			responseMessage
			Folders []folder `xml:"Folders>CalendarFolder"`
		} `xml:"ResponseMessages>GetFolderResponseMessage"`
	}
	if err := p.call(ctx, `<m:GetFolder><m:FolderShape><t:BaseShape>Default</t:BaseShape></m:FolderShape>
<m:FolderIds><t:DistinguishedFolderId Id="calendar"/></m:FolderIds></m:GetFolder>`, &got); err != nil {
		return nil, err
	}
	if len(got.Messages) == 0 || len(got.Messages[0].Folders) == 0 {
		if len(got.Messages) > 0 {
			if err := got.Messages[0].err(); err != nil {
				return nil, err
			}
		}
		return nil, errors.New("Exchange returned no calendar folder")
	}
	root := got.Messages[0].Folders[0]
	out := []store.CalendarInfo{{RemoteID: root.ID.ID, Name: orDefault(root.DisplayName, "Calendar"), Access: "owner", Primary: true}}

	var found struct {
		Messages []struct {
			responseMessage
			Folders []folder `xml:"RootFolder>Folders>CalendarFolder"`
		} `xml:"ResponseMessages>FindFolderResponseMessage"`
	}
	if err := p.call(ctx, `<m:FindFolder Traversal="Deep"><m:FolderShape><t:BaseShape>Default</t:BaseShape></m:FolderShape>
<m:ParentFolderIds><t:DistinguishedFolderId Id="calendar"/></m:ParentFolderIds></m:FindFolder>`, &found); err == nil && len(found.Messages) > 0 {
		for _, f := range found.Messages[0].Folders {
			out = append(out, store.CalendarInfo{RemoteID: f.ID.ID, Name: f.DisplayName, Access: "owner"})
		}
	}
	return out, nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// --- reading ----------------------------------------------------------------

type itemID struct {
	ID        string `xml:"Id,attr"`
	ChangeKey string `xml:"ChangeKey,attr"`
}

// Pull returns what changed in a calendar folder since the sync state.
func (p *Provider) Pull(ctx context.Context, cal store.CalendarInfo, token string) (*calsync.PullResult, error) {
	res := &calsync.PullResult{Full: token == ""}
	state := token
	var changed []itemID
	for {
		var out struct {
			Messages []struct {
				responseMessage
				SyncState string `xml:"SyncState"`
				Last      bool   `xml:"IncludesLastItemInRange"`
				Changes   struct {
					Create []struct {
						Item itemID `xml:"CalendarItem>ItemId"`
					} `xml:"Create"`
					Update []struct {
						Item itemID `xml:"CalendarItem>ItemId"`
					} `xml:"Update"`
					Delete []struct {
						Item itemID `xml:"ItemId"`
					} `xml:"Delete"`
				} `xml:"Changes"`
			} `xml:"ResponseMessages>SyncFolderItemsResponseMessage"`
		}
		body := `<m:SyncFolderItems><m:ItemShape><t:BaseShape>IdOnly</t:BaseShape></m:ItemShape>
<m:SyncFolderId><t:FolderId Id="` + esc(cal.RemoteID) + `"/></m:SyncFolderId>`
		if state != "" {
			body += `<m:SyncState>` + esc(state) + `</m:SyncState>`
		}
		body += `<m:MaxChangesReturned>512</m:MaxChangesReturned></m:SyncFolderItems>`
		if err := p.call(ctx, body, &out); err != nil {
			return nil, err
		}
		if len(out.Messages) == 0 {
			return nil, errors.New("Exchange returned nothing for the sync")
		}
		m := out.Messages[0]
		if err := m.err(); err != nil {
			return nil, err
		}
		for _, c := range m.Changes.Create {
			changed = append(changed, c.Item)
		}
		for _, c := range m.Changes.Update {
			changed = append(changed, c.Item)
		}
		for _, c := range m.Changes.Delete {
			res.Deletes = append(res.Deletes, c.Item.ID)
		}
		state = m.SyncState
		if m.Last {
			break
		}
	}
	for i := 0; i < len(changed); i += 50 {
		end := i + 50
		if end > len(changed) {
			end = len(changed)
		}
		items, err := p.getItems(ctx, changed[i:end])
		if err != nil {
			return nil, err
		}
		res.Upserts = append(res.Upserts, items...)
	}
	res.NextToken = state
	return res, nil
}

// getItems reads items as iCalendar, with the fields iCalendar does not carry.
func (p *Provider) getItems(ctx context.Context, ids []itemID) ([]calsync.RemoteEvent, error) {
	var b strings.Builder
	b.WriteString(`<m:GetItem><m:ItemShape><t:BaseShape>IdOnly</t:BaseShape><t:IncludeMimeContent>true</t:IncludeMimeContent>
<t:AdditionalProperties><t:FieldURI FieldURI="calendar:MyResponseType"/><t:FieldURI FieldURI="calendar:IsCancelled"/>
<t:FieldURI FieldURI="calendar:UID"/></t:AdditionalProperties></m:ItemShape><m:ItemIds>`)
	for _, id := range ids {
		fmt.Fprintf(&b, `<t:ItemId Id="%s" ChangeKey="%s"/>`, esc(id.ID), esc(id.ChangeKey))
	}
	b.WriteString(`</m:ItemIds></m:GetItem>`)
	var out struct {
		Messages []struct {
			responseMessage
			Items []struct {
				ID          itemID `xml:"ItemId"`
				Mime        string `xml:"MimeContent"`
				MyResponse  string `xml:"MyResponseType"`
				IsCancelled bool   `xml:"IsCancelled"`
				UID         string `xml:"UID"`
			} `xml:"Items>CalendarItem"`
		} `xml:"ResponseMessages>GetItemResponseMessage"`
	}
	if err := p.call(ctx, b.String(), &out); err != nil {
		return nil, err
	}
	var events []calsync.RemoteEvent
	for _, m := range out.Messages {
		if m.err() != nil {
			continue // one unreadable item does not stop the rest
		}
		for _, it := range m.Items {
			raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(it.Mime))
			if err != nil {
				continue
			}
			cal, err := calendar.Parse(icsOf(raw))
			if err != nil {
				continue
			}
			my := map[string]string{"Accept": "accepted", "Tentative": "tentative", "Decline": "declined",
				"NoResponseReceived": "needs-action", "Organizer": "organizer"}[it.MyResponse]
			for _, ev := range cal.Events {
				if it.IsCancelled && ev.RecurrenceID.IsZero() {
					ev.Status = "cancelled"
				}
				re := calsync.RemoteEvent{Event: ev, ETag: it.ID.ChangeKey, MyStatus: my, Raw: string(raw)}
				// The series (or single event) is the item; exceptions inside
				// it are stored alongside, under the same UID.
				if ev.RecurrenceID.IsZero() {
					re.RemoteID = it.ID.ID
				}
				events = append(events, re)
			}
		}
	}
	return events, nil
}

// icsOf finds the iCalendar in MimeContent: Exchange returns it bare for a
// calendar item, or inside a MIME message for a meeting request.
func icsOf(raw []byte) []byte {
	i := bytes.Index(raw, []byte("BEGIN:VCALENDAR"))
	j := bytes.LastIndex(raw, []byte("END:VCALENDAR"))
	if i < 0 || j < i {
		return raw
	}
	return raw[i : j+len("END:VCALENDAR")]
}

// --- writing ----------------------------------------------------------------

// Push sends one change.
func (p *Provider) Push(ctx context.Context, cal store.CalendarInfo, ch store.CalendarChange, ev *store.StoredEvent) (*calsync.PushResult, error) {
	switch ch.Op {
	case "create":
		if ev.RecurrenceID != nil {
			return p.update(ctx, cal, ev)
		}
		return p.create(ctx, cal, ev)
	case "update":
		return p.update(ctx, cal, ev)
	case "delete":
		return p.delete(ctx, cal, ev)
	case "rsvp":
		r, err := calsync.DecodeRSVP(ch)
		if err != nil {
			return nil, err
		}
		return p.respond(ctx, cal, ev, r)
	}
	return nil, fmt.Errorf("unknown change %q", ch.Op)
}

func sendMode(ev calendar.Event) string {
	if len(ev.Attendees) > 0 {
		return "SendToAllAndSaveCopy"
	}
	return "SendToNone"
}

func (p *Provider) create(ctx context.Context, cal store.CalendarInfo, stored *store.StoredEvent) (*calsync.PushResult, error) {
	ev := stored.Model()
	fields, err := itemXML(ev)
	if err != nil {
		return nil, err
	}
	body := `<m:CreateItem SendMeetingInvitations="` + sendMode(ev) + `"><m:SavedItemFolderId><t:FolderId Id="` + esc(cal.RemoteID) +
		`"/></m:SavedItemFolderId><m:Items><t:CalendarItem>` + fields + `</t:CalendarItem></m:Items></m:CreateItem>`
	var out struct {
		Messages []struct {
			responseMessage
			ID itemID `xml:"Items>CalendarItem>ItemId"`
		} `xml:"ResponseMessages>CreateItemResponseMessage"`
	}
	if err := p.call(ctx, body, &out); err != nil {
		return nil, err
	}
	if len(out.Messages) == 0 {
		return nil, errors.New("Exchange did not say whether it created the event")
	}
	if err := out.Messages[0].err(); err != nil {
		return nil, err
	}
	return &calsync.PushResult{RemoteID: out.Messages[0].ID.ID, ETag: out.Messages[0].ID.ChangeKey}, nil
}

// target is the item a change applies to: the event's own, or — for one
// occurrence of a series — the occurrence, found by looking at the calendar
// at that moment, since EWS addresses occurrences by an index it does not
// otherwise say.
func (p *Provider) target(ctx context.Context, cal store.CalendarInfo, ev *store.StoredEvent) (itemID, error) {
	if ev.RemoteID != "" && ev.RecurrenceID == nil {
		return itemID{ID: ev.RemoteID, ChangeKey: ev.ETag}, nil
	}
	if ev.RecurrenceID == nil {
		return itemID{}, errors.New("this event has not reached Exchange yet")
	}
	at := *ev.RecurrenceID
	var out struct {
		Messages []struct {
			responseMessage
			Items []struct {
				ID            itemID `xml:"ItemId"`
				Start         string `xml:"Start"`
				OriginalStart string `xml:"OriginalStart"`
				UID           string `xml:"UID"`
			} `xml:"RootFolder>Items>CalendarItem"`
		} `xml:"ResponseMessages>FindItemResponseMessage"`
	}
	body := `<m:FindItem Traversal="Shallow"><m:ItemShape><t:BaseShape>IdOnly</t:BaseShape><t:AdditionalProperties>
<t:FieldURI FieldURI="calendar:Start"/><t:FieldURI FieldURI="calendar:OriginalStart"/><t:FieldURI FieldURI="calendar:UID"/></t:AdditionalProperties></m:ItemShape>
<m:CalendarView StartDate="` + at.Add(-time.Minute).UTC().Format(time.RFC3339) + `" EndDate="` + at.Add(24*time.Hour).UTC().Format(time.RFC3339) +
		`"/><m:ParentFolderIds><t:FolderId Id="` + esc(cal.RemoteID) + `"/></m:ParentFolderIds></m:FindItem>`
	if err := p.call(ctx, body, &out); err != nil {
		return itemID{}, err
	}
	for _, m := range out.Messages {
		for _, it := range m.Items {
			if it.UID != "" && !strings.EqualFold(it.UID, ev.UID) {
				continue
			}
			// OriginalStart is when the occurrence would have been before
			// any move, which is what a recurrence id names; Start is the
			// fallback for a server that leaves it out.
			if t, err := time.Parse(time.RFC3339, it.OriginalStart); err == nil && t.Equal(at) {
				return it.ID, nil
			}
			if t, err := time.Parse(time.RFC3339, it.Start); err == nil && it.OriginalStart == "" && t.Equal(at) {
				return it.ID, nil
			}
		}
	}
	return itemID{}, fmt.Errorf("Exchange has no occurrence of this event at %s", at.Format(time.RFC3339))
}

func (p *Provider) update(ctx context.Context, cal store.CalendarInfo, stored *store.StoredEvent) (*calsync.PushResult, error) {
	id, err := p.target(ctx, cal, stored)
	if err != nil {
		return nil, err
	}
	ev := stored.Model()
	updates, err := setFields(ev, stored.RecurrenceID != nil)
	if err != nil {
		return nil, err
	}
	body := `<m:UpdateItem ConflictResolution="NeverOverwrite" SendMeetingInvitationsOrCancellations="` +
		sendMode(ev) + `"><m:ItemChanges><t:ItemChange>` +
		fmt.Sprintf(`<t:ItemId Id="%s"%s/>`, esc(id.ID), changeKeyAttr(id.ChangeKey)) +
		`<t:Updates>` + updates + `</t:Updates></t:ItemChange></m:ItemChanges></m:UpdateItem>`
	var out struct {
		Messages []struct {
			responseMessage
			ID itemID `xml:"Items>CalendarItem>ItemId"`
		} `xml:"ResponseMessages>UpdateItemResponseMessage"`
	}
	if err := p.call(ctx, body, &out); err != nil {
		return nil, err
	}
	if len(out.Messages) == 0 {
		return nil, errors.New("Exchange did not say whether it changed the event")
	}
	if err := out.Messages[0].err(); err != nil {
		return nil, err
	}
	res := &calsync.PushResult{ETag: out.Messages[0].ID.ChangeKey}
	if stored.RecurrenceID == nil {
		res.RemoteID = out.Messages[0].ID.ID
	}
	return res, nil
}

func changeKeyAttr(k string) string {
	if k == "" {
		return ""
	}
	return ` ChangeKey="` + esc(k) + `"`
}

func (p *Provider) delete(ctx context.Context, cal store.CalendarInfo, stored *store.StoredEvent) (*calsync.PushResult, error) {
	id, err := p.target(ctx, cal, stored)
	if err != nil {
		return nil, err
	}
	mode := "SendToNone"
	if stored.MyStatus == "organizer" && len(stored.Attendees) > 0 {
		mode = "SendToAllAndSaveCopy"
	}
	body := `<m:DeleteItem DeleteType="MoveToDeletedItems" SendMeetingCancellations="` + mode + `"><m:ItemIds>` +
		fmt.Sprintf(`<t:ItemId Id="%s"%s/>`, esc(id.ID), changeKeyAttr(id.ChangeKey)) + `</m:ItemIds></m:DeleteItem>`
	var out struct {
		Messages []responseMessage `xml:"ResponseMessages>DeleteItemResponseMessage"`
	}
	if err := p.call(ctx, body, &out); err != nil {
		return nil, err
	}
	if len(out.Messages) > 0 {
		if err := out.Messages[0].err(); err != nil && !errors.Is(err, errNotFound) {
			return nil, err
		}
	}
	return &calsync.PushResult{}, nil
}

func (p *Provider) respond(ctx context.Context, cal store.CalendarInfo, stored *store.StoredEvent, r calsync.RSVP) (*calsync.PushResult, error) {
	id, err := p.target(ctx, cal, stored)
	if err != nil {
		return nil, err
	}
	kind := map[string]string{"accepted": "AcceptItem", "tentative": "TentativelyAcceptItem", "declined": "DeclineItem"}[r.Status]
	if kind == "" {
		return nil, fmt.Errorf("%q is not an answer", r.Status)
	}
	note := ""
	if r.Comment != "" {
		note = `<t:Body BodyType="Text">` + esc(r.Comment) + `</t:Body>`
	}
	body := `<m:CreateItem MessageDisposition="SendAndSaveCopy"><m:Items><t:` + kind + `>` + note +
		fmt.Sprintf(`<t:ReferenceItemId Id="%s"%s/>`, esc(id.ID), changeKeyAttr(id.ChangeKey)) +
		`</t:` + kind + `></m:Items></m:CreateItem>`
	var out struct {
		Messages []responseMessage `xml:"ResponseMessages>CreateItemResponseMessage"`
	}
	if err := p.call(ctx, body, &out); err != nil {
		return nil, err
	}
	if len(out.Messages) > 0 {
		if err := out.Messages[0].err(); err != nil {
			return nil, err
		}
	}
	return &calsync.PushResult{RemoteID: stored.RemoteID, ETag: stored.ETag}, nil
}

// Connect tests an Exchange sign-in and saves the account.
func Connect(ctx context.Context, rawURL, username, password, email, auth string) (*store.CalendarAccount, error) {
	if auth != "basic" {
		auth = "ntlm"
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" && strings.Contains(username, "@") {
		email = strings.ToLower(username)
	}
	if email == "" {
		return nil, errors.New("give your email address, so invitations to it are recognised as yours")
	}
	acct := store.CalendarAccount{ID: "ews-" + strings.NewReplacer("@", "-", ".", "-").Replace(email), Provider: store.ProviderEWS,
		Name: email, Email: email, Status: "ok",
		Config: map[string]string{"url": strings.TrimSpace(rawURL), "username": strings.TrimSpace(username), "auth": auth}}
	if err := Check(ctx, acct, password); err != nil {
		return nil, err
	}
	if err := store.SaveCalendarAccount(acct, password); err != nil {
		return nil, err
	}
	return &acct, nil
}
