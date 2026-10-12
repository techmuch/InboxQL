package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/user/inboxql/internal/calendar"
	"github.com/user/inboxql/internal/vault"
)

// Calendar providers.
const (
	ProviderLocal  = "local"  // lives only in this mailbox
	ProviderMail   = "mail"   // invitations that arrived by mail
	ProviderGoogle = "google" // Google Calendar
	ProviderEWS    = "ews"    // Exchange Server, over EWS
	ProviderICS    = "ics"    // a subscribed or imported file
)

// The two calendars every mailbox has.
const (
	LocalCalendarID = "local"
	MailCalendarID  = "invitations"
)

// OccurrenceWindow is how far the expansion of recurring events reaches,
// either side of now. Further than this, a series is expanded on request.
var OccurrenceWindow = struct{ Back, Ahead time.Duration }{365 * 24 * time.Hour, 2 * 365 * 24 * time.Hour}

// CalendarAccount is a connection to a calendar provider.
type CalendarAccount struct {
	ID        string            `json:"id"`
	Provider  string            `json:"provider"`
	Name      string            `json:"name"`
	Email     string            `json:"email,omitempty"`
	Config    map[string]string `json:"config"`
	Status    string            `json:"status"`
	LastError string            `json:"lastError,omitempty"`
	SyncedAt  *time.Time        `json:"syncedAt,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	HasSecret bool              `json:"hasSecret"`
}

// CalendarInfo is one calendar.
type CalendarInfo struct {
	ID        string     `json:"id"`
	AccountID string     `json:"accountId,omitempty"`
	Provider  string     `json:"provider"`
	RemoteID  string     `json:"remoteId,omitempty"`
	Name      string     `json:"name"`
	Color     string     `json:"color,omitempty"`
	TimeZone  string     `json:"timeZone,omitempty"`
	Access    string     `json:"access"`
	Hidden    bool       `json:"hidden"`
	Primary   bool       `json:"primary"`
	SyncToken string     `json:"-"`
	SyncedAt  *time.Time `json:"syncedAt,omitempty"`
	Events    int        `json:"events"`
}

// Writable reports whether events can be created or changed in it.
func (c CalendarInfo) Writable() bool {
	return (c.Access == "owner" || c.Access == "writer") && c.Provider != ProviderICS && c.Provider != ProviderMail
}

// StoredEvent is an event as kept, with where it lives.
type StoredEvent struct {
	ID              string              `json:"id"`
	CalendarID      string              `json:"calendarId"`
	UID             string              `json:"uid"`
	RecurrenceID    *time.Time          `json:"recurrenceId,omitempty"`
	RemoteID        string              `json:"remoteId,omitempty"`
	ETag            string              `json:"-"`
	Summary         string              `json:"summary"`
	Description     string              `json:"description,omitempty"`
	Location        string              `json:"location,omitempty"`
	ConferenceURL   string              `json:"conferenceUrl,omitempty"`
	Start           time.Time           `json:"start"`
	End             time.Time           `json:"end"`
	AllDay          bool                `json:"allDay"`
	TZ              string              `json:"tz,omitempty"`
	Recurrence      []string            `json:"recurrence,omitempty"`
	Repeats         string              `json:"repeats,omitempty"`
	Status          string              `json:"status"`
	ShowAs          string              `json:"showAs"`
	Private         bool                `json:"private"`
	Organizer       string              `json:"organizer,omitempty"`
	MyStatus        string              `json:"myStatus,omitempty"`
	Sequence        int                 `json:"sequence"`
	Alarms          []int               `json:"alarms"`
	Attendees       []calendar.Attendee `json:"attendees"`
	Raw             string              `json:"-"`
	SourceMessageID string              `json:"sourceMessageId,omitempty"`
	ThreadKey       string              `json:"threadKey,omitempty"`
	Origin          string              `json:"origin"`
	Proposed        bool                `json:"proposed"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
}

// Model turns a stored event back into the calendar model.
func (e StoredEvent) Model() calendar.Event {
	ev := calendar.Event{
		UID: e.UID, Summary: e.Summary, Description: e.Description, Location: e.Location,
		ConferenceURL: e.ConferenceURL, Start: e.Start, End: e.End, AllDay: e.AllDay, TZ: e.TZ,
		// Copies, not the stored slices: a caller editing the model must not
		// rewrite the event it came from.
		Recurrence: append([]string(nil), e.Recurrence...), Status: e.Status, ShowAs: e.ShowAs, Private: e.Private,
		Organizer: e.Organizer, Attendees: append([]calendar.Attendee(nil), e.Attendees...), Sequence: e.Sequence,
		Alarms: append([]int(nil), e.Alarms...),
	}
	if e.RecurrenceID != nil {
		ev.RecurrenceID = *e.RecurrenceID
	}
	return ev
}

// EventMeta is what an event carries besides the iCalendar itself.
type EventMeta struct {
	RemoteID        string
	ETag            string
	MyStatus        string
	Raw             string
	SourceMessageID string
	ThreadKey       string
	Origin          string
	Proposed        bool
}

// EventID is the stable id of an event: the same calendar, UID and
// occurrence always get the same one, so a re-sync updates rather than adds.
func EventID(calendarID, uid string, recurrenceID time.Time) string {
	rid := int64(0)
	if !recurrenceID.IsZero() {
		rid = recurrenceID.UTC().UnixMilli()
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", calendarID, uid, rid)))
	return "ev_" + hex.EncodeToString(sum[:10])
}

// EnsureDefaultCalendars makes the two calendars every mailbox has.
func EnsureDefaultCalendars() error {
	now := time.Now().UnixMilli()
	_, err := db.Exec(`
		INSERT OR IGNORE INTO calendars (id, provider, name, color, access, is_primary, created_at)
		VALUES (?, 'local', 'My calendar', '#2563eb', 'owner', 1, ?),
		       (?, 'mail', 'Invitations', '#9333ea', 'reader', 0, ?)`,
		LocalCalendarID, now, MailCalendarID, now)
	return err
}

// --- accounts -------------------------------------------------------------

// SaveCalendarAccount creates or updates an account. A non-empty secret is
// sealed with the vault; an empty one leaves the stored secret as it was.
func SaveCalendarAccount(a CalendarAccount, secret string) error {
	cfg, _ := json.Marshal(a.Config)
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if a.Status == "" {
		a.Status = "new"
	}
	if _, err := db.Exec(`
		INSERT INTO calendar_accounts (id, provider, name, email, config_json, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, email = excluded.email,
			config_json = excluded.config_json, status = excluded.status`,
		a.ID, a.Provider, a.Name, a.Email, string(cfg), a.Status, a.CreatedAt.UnixMilli()); err != nil {
		return err
	}
	if secret != "" {
		sealed, err := vault.Encrypt(secret)
		if err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE calendar_accounts SET secret = ? WHERE id = ?`, sealed, a.ID); err != nil {
			return err
		}
	}
	return nil
}

// CalendarAccountSecret returns an account's decrypted secret: a refresh
// token, or a password.
func CalendarAccountSecret(id string) (string, error) {
	var sealed sql.NullString
	if err := db.QueryRow(`SELECT secret FROM calendar_accounts WHERE id = ?`, id).Scan(&sealed); err != nil {
		return "", err
	}
	if !sealed.Valid || sealed.String == "" {
		return "", nil
	}
	return vault.Decrypt(sealed.String)
}

// SetCalendarAccountStatus records how the last sync went.
func SetCalendarAccountStatus(id, status, lastError string, synced bool) error {
	q := `UPDATE calendar_accounts SET status = ?, last_error = ? WHERE id = ?`
	args := []any{status, nullIfEmpty(lastError), id}
	if synced {
		q = `UPDATE calendar_accounts SET status = ?, last_error = ?, synced_at = ? WHERE id = ?`
		args = []any{status, nullIfEmpty(lastError), time.Now().UnixMilli(), id}
	}
	_, err := db.Exec(q, args...)
	return err
}

// ListCalendarAccounts returns every account, secrets omitted.
func ListCalendarAccounts() ([]CalendarAccount, error) {
	rows, err := db.Query(`SELECT id, provider, name, COALESCE(email,''), config_json, status,
		COALESCE(last_error,''), synced_at, created_at, secret IS NOT NULL AND secret != ''
		FROM calendar_accounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarAccount
	for rows.Next() {
		var a CalendarAccount
		var cfg string
		var synced sql.NullInt64
		var created int64
		if err := rows.Scan(&a.ID, &a.Provider, &a.Name, &a.Email, &cfg, &a.Status, &a.LastError,
			&synced, &created, &a.HasSecret); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(cfg), &a.Config)
		if a.Config == nil {
			a.Config = map[string]string{}
		}
		a.CreatedAt = time.UnixMilli(created)
		if synced.Valid {
			t := time.UnixMilli(synced.Int64)
			a.SyncedAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetCalendarAccount returns one account.
func GetCalendarAccount(id string) (*CalendarAccount, error) {
	all, err := ListCalendarAccounts()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, sql.ErrNoRows
}

// DeleteCalendarAccount removes an account, its calendars and their events.
// Nothing at the provider is touched.
func DeleteCalendarAccount(id string) error {
	_, err := db.Exec(`DELETE FROM calendar_accounts WHERE id = ?`, id)
	return err
}

// --- calendars --------------------------------------------------------------

// SaveCalendar creates or updates a calendar.
func SaveCalendar(c CalendarInfo) error {
	if c.Access == "" {
		c.Access = "owner"
	}
	_, err := db.Exec(`
		INSERT INTO calendars (id, account_id, provider, remote_id, name, color, time_zone, access, hidden, is_primary, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, color = COALESCE(excluded.color, calendars.color),
			time_zone = excluded.time_zone, access = excluded.access, is_primary = excluded.is_primary`,
		c.ID, nullIfEmpty(c.AccountID), c.Provider, nullIfEmpty(c.RemoteID), c.Name, nullIfEmpty(c.Color),
		nullIfEmpty(c.TimeZone), c.Access, c.Hidden, c.Primary, time.Now().UnixMilli())
	return err
}

// SetCalendarSyncToken records where an incremental sync got to.
func SetCalendarSyncToken(id, token string) error {
	_, err := db.Exec(`UPDATE calendars SET sync_token = ?, synced_at = ? WHERE id = ?`,
		nullIfEmpty(token), time.Now().UnixMilli(), id)
	return err
}

// SetCalendarHidden shows or hides a calendar in the views.
func SetCalendarHidden(id string, hidden bool) error {
	res, err := db.Exec(`UPDATE calendars SET hidden = ? WHERE id = ?`, hidden, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetCalendarColor sets a calendar's colour.
func SetCalendarColor(id, color string) error {
	_, err := db.Exec(`UPDATE calendars SET color = ? WHERE id = ?`, color, id)
	return err
}

// ListCalendars returns every calendar with its event count.
func ListCalendars() ([]CalendarInfo, error) {
	rows, err := db.Query(`SELECT c.id, COALESCE(c.account_id,''), c.provider, COALESCE(c.remote_id,''), c.name,
		COALESCE(c.color,''), COALESCE(c.time_zone,''), c.access, c.hidden, c.is_primary,
		COALESCE(c.sync_token,''), c.synced_at,
		(SELECT COUNT(*) FROM events e WHERE e.calendar_id = c.id)
		FROM calendars c ORDER BY c.is_primary DESC, c.provider = 'mail', c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarInfo
	for rows.Next() {
		var c CalendarInfo
		var synced sql.NullInt64
		if err := rows.Scan(&c.ID, &c.AccountID, &c.Provider, &c.RemoteID, &c.Name, &c.Color, &c.TimeZone,
			&c.Access, &c.Hidden, &c.Primary, &c.SyncToken, &synced, &c.Events); err != nil {
			return nil, err
		}
		if synced.Valid {
			t := time.UnixMilli(synced.Int64)
			c.SyncedAt = &t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCalendar returns one calendar.
func GetCalendar(id string) (*CalendarInfo, error) {
	all, err := ListCalendars()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, sql.ErrNoRows
}

// CalendarByRemote finds an account's calendar by the provider's id.
func CalendarByRemote(accountID, remoteID string) (*CalendarInfo, error) {
	var id string
	if err := db.QueryRow(`SELECT id FROM calendars WHERE account_id = ? AND remote_id = ?`, accountID, remoteID).Scan(&id); err != nil {
		return nil, err
	}
	return GetCalendar(id)
}

// --- events -----------------------------------------------------------------

// UpsertEvent stores an event in a calendar, replacing the one with the same
// UID and occurrence, and re-expands its series. It returns the event's id.
func UpsertEvent(calendarID string, ev calendar.Event, meta EventMeta) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	id, err := upsertEventTx(tx, calendarID, ev, meta)
	if err != nil {
		return "", err
	}
	if err := rebuildSeriesTx(tx, calendarID, ev.UID); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func upsertEventTx(tx *sql.Tx, calendarID string, ev calendar.Event, meta EventMeta) (string, error) {
	id := EventID(calendarID, ev.UID, ev.RecurrenceID)
	rid := int64(0)
	if !ev.RecurrenceID.IsZero() {
		rid = ev.RecurrenceID.UTC().UnixMilli()
	}
	alarms, _ := json.Marshal(nonNilInts(ev.Alarms))
	if meta.Origin == "" {
		meta.Origin = "human"
	}
	status := ev.Status
	if status == "" {
		status = "confirmed"
	}
	showAs := ev.ShowAs
	if showAs == "" {
		showAs = "busy"
	}
	now := time.Now().UnixMilli()
	_, err := tx.Exec(`
		INSERT INTO events (id, calendar_id, uid, recurrence_id, remote_id, etag, summary, description,
			location, conference_url, start_at, end_at, all_day, tz, rrule, status, show_as, private,
			organizer, my_status, sequence, alarms, raw, source_message_id, thread_key, origin, proposed,
			created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET remote_id = COALESCE(excluded.remote_id, events.remote_id),
			etag = COALESCE(excluded.etag, events.etag), summary = excluded.summary,
			description = excluded.description, location = excluded.location,
			conference_url = excluded.conference_url, start_at = excluded.start_at, end_at = excluded.end_at,
			all_day = excluded.all_day, tz = excluded.tz, rrule = excluded.rrule, status = excluded.status,
			show_as = excluded.show_as, private = excluded.private, organizer = excluded.organizer,
			my_status = CASE WHEN excluded.my_status != '' THEN excluded.my_status ELSE events.my_status END,
			sequence = excluded.sequence, alarms = excluded.alarms,
			raw = CASE WHEN excluded.raw != '' THEN excluded.raw ELSE events.raw END,
			source_message_id = COALESCE(excluded.source_message_id, events.source_message_id),
			thread_key = COALESCE(excluded.thread_key, events.thread_key),
			proposed = excluded.proposed, updated_at = excluded.updated_at`,
		id, calendarID, ev.UID, rid, nullIfEmpty(meta.RemoteID), nullIfEmpty(meta.ETag), ev.Summary,
		ev.Description, ev.Location, ev.ConferenceURL, ev.Start.UnixMilli(), ev.End.UnixMilli(), ev.AllDay,
		ev.TZ, strings.Join(ev.Recurrence, "\n"), status, showAs, ev.Private, ev.Organizer, meta.MyStatus,
		ev.Sequence, string(alarms), meta.Raw, nullIfEmpty(meta.SourceMessageID), nullIfEmpty(meta.ThreadKey),
		meta.Origin, meta.Proposed, now, now)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`DELETE FROM event_attendees WHERE event_id = ?`, id); err != nil {
		return "", err
	}
	for _, a := range ev.Attendees {
		if a.Address == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO event_attendees (event_id, address, name, role, status)
			VALUES (?, ?, ?, ?, ?)`, id, strings.ToLower(a.Address), a.Name, orDefault(a.Role, "required"),
			orDefault(a.Status, "needs-action")); err != nil {
			return "", err
		}
	}
	return id, nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func nonNilInts(xs []int) []int {
	if xs == nil {
		return []int{}
	}
	return xs
}

// DeleteEvent removes an event — a whole series when recurrenceID is zero —
// and its occurrences.
func DeleteEvent(calendarID, uid string, recurrenceID time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if recurrenceID.IsZero() {
		if _, err := tx.Exec(`DELETE FROM events WHERE calendar_id = ? AND uid = ?`, calendarID, uid); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(`DELETE FROM events WHERE id = ?`, EventID(calendarID, uid, recurrenceID)); err != nil {
			return err
		}
		if err := rebuildSeriesTx(tx, calendarID, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteEventByRemote removes the event a provider knows by its own id.
func DeleteEventByRemote(calendarID, remoteID string) error {
	var uid string
	var rid int64
	err := db.QueryRow(`SELECT uid, recurrence_id FROM events WHERE calendar_id = ? AND remote_id = ?`,
		calendarID, remoteID).Scan(&uid, &rid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var t time.Time
	if rid != 0 {
		t = time.UnixMilli(rid).UTC()
	}
	return DeleteEvent(calendarID, uid, t)
}

const eventColumns = `e.id, e.calendar_id, e.uid, e.recurrence_id, COALESCE(e.remote_id,''), COALESCE(e.etag,''),
	e.summary, e.description, e.location, e.conference_url, e.start_at, e.end_at, e.all_day, e.tz, e.rrule,
	e.status, e.show_as, e.private, e.organizer, e.my_status, e.sequence, e.alarms, e.raw,
	COALESCE(e.source_message_id,''), COALESCE(e.thread_key,''), e.origin, e.proposed, e.created_at, e.updated_at`

func scanEvent(row interface{ Scan(...any) error }) (StoredEvent, error) {
	var e StoredEvent
	var rid, start, end, created, updated int64
	var rrule, alarms string
	if err := row.Scan(&e.ID, &e.CalendarID, &e.UID, &rid, &e.RemoteID, &e.ETag, &e.Summary, &e.Description,
		&e.Location, &e.ConferenceURL, &start, &end, &e.AllDay, &e.TZ, &rrule, &e.Status, &e.ShowAs, &e.Private,
		&e.Organizer, &e.MyStatus, &e.Sequence, &alarms, &e.Raw, &e.SourceMessageID, &e.ThreadKey, &e.Origin,
		&e.Proposed, &created, &updated); err != nil {
		return e, err
	}
	if rid != 0 {
		t := time.UnixMilli(rid).UTC()
		e.RecurrenceID = &t
	}
	e.Start, e.End = time.UnixMilli(start).UTC(), time.UnixMilli(end).UTC()
	e.CreatedAt, e.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
	if rrule != "" {
		e.Recurrence = strings.Split(rrule, "\n")
		e.Repeats = calendar.Describe(e.Recurrence)
	}
	_ = json.Unmarshal([]byte(alarms), &e.Alarms)
	if e.Alarms == nil {
		e.Alarms = []int{}
	}
	e.Attendees = []calendar.Attendee{}
	return e, nil
}

func loadAttendees(events []StoredEvent) error {
	for i := range events {
		rows, err := db.Query(`SELECT address, name, role, status FROM event_attendees WHERE event_id = ? ORDER BY role = 'optional', address`, events[i].ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a calendar.Attendee
			if err := rows.Scan(&a.Address, &a.Name, &a.Role, &a.Status); err != nil {
				rows.Close()
				return err
			}
			events[i].Attendees = append(events[i].Attendees, a)
		}
		rows.Close()
	}
	return nil
}

// GetEvent returns one event with its attendees.
func GetEvent(id string) (*StoredEvent, error) {
	e, err := scanEvent(db.QueryRow(`SELECT `+eventColumns+` FROM events e WHERE e.id = ?`, id))
	if err != nil {
		return nil, err
	}
	list := []StoredEvent{e}
	if err := loadAttendees(list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// EventsByUID returns every stored copy of an event — the series and its
// overrides — in one calendar, or in every calendar when calendarID is "".
func EventsByUID(calendarID, uid string) ([]StoredEvent, error) {
	q := `SELECT ` + eventColumns + ` FROM events e WHERE e.uid = ?`
	args := []any{uid}
	if calendarID != "" {
		q += ` AND e.calendar_id = ?`
		args = append(args, calendarID)
	}
	rows, err := db.Query(q+` ORDER BY e.recurrence_id`, args...)
	if err != nil {
		return nil, err
	}
	var out []StoredEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	rows.Close()
	return out, loadAttendees(out)
}

// EventsForMessage returns the events an invitation message produced.
func EventsForMessage(messageID string) ([]StoredEvent, error) {
	rows, err := db.Query(`SELECT `+eventColumns+` FROM events e WHERE e.source_message_id = ?`, messageID)
	if err != nil {
		return nil, err
	}
	var out []StoredEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	rows.Close()
	return out, loadAttendees(out)
}

// SetMyStatus records the user's own answer to an invitation, on every copy
// of the event they hold.
func SetMyStatus(uid, status string, me []string) error {
	if _, err := db.Exec(`UPDATE events SET my_status = ?, updated_at = ? WHERE uid = ?`,
		status, time.Now().UnixMilli(), uid); err != nil {
		return err
	}
	for _, addr := range me {
		if _, err := db.Exec(`UPDATE event_attendees SET status = ? WHERE address = ? AND event_id IN
			(SELECT id FROM events WHERE uid = ?)`, status, strings.ToLower(addr), uid); err != nil {
			return err
		}
	}
	return nil
}

// --- occurrences ------------------------------------------------------------

// rebuildSeriesTx re-expands one series — its master and every override —
// into event_occurrences.
func rebuildSeriesTx(tx *sql.Tx, calendarID, uid string) error {
	rows, err := tx.Query(`SELECT `+eventColumns+` FROM events e WHERE e.calendar_id = ? AND e.uid = ?`, calendarID, uid)
	if err != nil {
		return err
	}
	var all []StoredEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return err
		}
		all = append(all, e)
	}
	rows.Close()
	ids := []any{}
	for _, e := range all {
		ids = append(ids, e.ID)
	}
	if len(ids) > 0 {
		// The charge on an occurrence is a person's ruling; keep it across
		// the rebuild, matched on the occurrence's start.
		if _, err := tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS occ_keep (event_id TEXT, start_at INTEGER, charge TEXT, charge_source TEXT)`); err != nil {
			return err
		}
		tx.Exec(`DELETE FROM occ_keep`)
		ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		if _, err := tx.Exec(`INSERT INTO occ_keep SELECT event_id, start_at, charge, charge_source FROM event_occurrences
			WHERE event_id IN (`+ph+`) AND charge IS NOT NULL`, ids...); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM event_occurrences WHERE event_id IN (`+ph+`)`, ids...); err != nil {
			return err
		}
	}
	var master *StoredEvent
	var overrides []calendar.Event
	byRID := map[int64]string{}
	for i := range all {
		if all[i].RecurrenceID == nil {
			master = &all[i]
		} else {
			overrides = append(overrides, all[i].Model())
			byRID[all[i].RecurrenceID.Unix()] = all[i].ID
		}
	}
	now := time.Now()
	from, to := now.Add(-OccurrenceWindow.Back), now.Add(OccurrenceWindow.Ahead)
	insert := func(eventID string, o calendar.Occurrence) error {
		_, err := tx.Exec(`INSERT OR REPLACE INTO event_occurrences (event_id, start_at, end_at, all_day, charge, charge_source)
			VALUES (?, ?, ?, ?, (SELECT charge FROM occ_keep WHERE event_id = ? AND start_at = ?),
			                    (SELECT charge_source FROM occ_keep WHERE event_id = ? AND start_at = ?))`,
			eventID, o.Start.UnixMilli(), o.End.UnixMilli(), o.AllDay,
			eventID, o.Start.UnixMilli(), eventID, o.Start.UnixMilli())
		return err
	}
	if master == nil {
		// Overrides whose series this calendar does not hold — an invitation
		// to one occurrence. Each stands on its own.
		for _, o := range overrides {
			if o.Status == "cancelled" {
				continue
			}
			if err := insert(byRID[o.RecurrenceID.Unix()], calendar.Occurrence{Start: o.Start, End: o.End, AllDay: o.AllDay}); err != nil {
				return err
			}
		}
		return nil
	}
	occ, err := calendar.Expand(master.Model(), overrides, from, to)
	if err != nil {
		// A rule this program cannot read still has a first occurrence.
		occ = []calendar.Occurrence{{Start: master.Start, End: master.End, AllDay: master.AllDay}}
	}
	for _, o := range occ {
		id := master.ID
		if o.Override != nil {
			id = byRID[o.Override.RecurrenceID.Unix()]
		}
		if err := insert(id, o); err != nil {
			return err
		}
	}
	return nil
}

// RebuildAllOccurrences re-expands every series, which moves the window
// forward. Run daily and after a migration.
func RebuildAllOccurrences() error {
	rows, err := db.Query(`SELECT DISTINCT calendar_id, uid FROM events`)
	if err != nil {
		return err
	}
	type key struct{ cal, uid string }
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.cal, &k.uid); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	rows.Close()
	for _, k := range keys {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := rebuildSeriesTx(tx, k.cal, k.uid); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// OccurrenceView is one occurrence with what a view needs to draw it.
type OccurrenceView struct {
	EventID       string    `json:"eventId"`
	CalendarID    string    `json:"calendarId"`
	UID           string    `json:"uid"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	AllDay        bool      `json:"allDay"`
	Summary       string    `json:"summary"`
	Location      string    `json:"location,omitempty"`
	ConferenceURL string    `json:"conferenceUrl,omitempty"`
	Status        string    `json:"status"`
	ShowAs        string    `json:"showAs"`
	MyStatus      string    `json:"myStatus,omitempty"`
	Recurring     bool      `json:"recurring"`
	Proposed      bool      `json:"proposed"`
	Color         string    `json:"color,omitempty"`
	Attendees     int       `json:"attendees"`
	Charge        string    `json:"charge,omitempty"`
	// Set by queries; the calendar views do not need them.
	CalendarName    string `json:"calendarName,omitempty"`
	Organizer       string `json:"organizer,omitempty"`
	ThreadKey       string `json:"threadKey,omitempty"`
	SourceMessageID string `json:"sourceMessageId,omitempty"`
}

// ListOccurrences returns the occurrences overlapping [from, to) in visible
// calendars, or in the given ones.
//
// An invitation that arrived by mail and is also in a synced calendar is
// shown once — from the synced calendar, which is the one that can change.
func ListOccurrences(from, to time.Time, calendarIDs []string) ([]OccurrenceView, error) {
	q := `SELECT o.event_id, e.calendar_id, e.uid, o.start_at, o.end_at, o.all_day, e.summary, e.location,
		e.conference_url, e.status, e.show_as, e.my_status, (e.rrule != '' OR e.recurrence_id != 0), e.proposed,
		COALESCE(c.color,''), (SELECT COUNT(*) FROM event_attendees a WHERE a.event_id = e.id), COALESCE(o.charge,'')
		FROM event_occurrences o JOIN events e ON e.id = o.event_id JOIN calendars c ON c.id = e.calendar_id
		WHERE o.end_at > ? AND o.start_at < ? AND e.status != 'cancelled'
		AND NOT (c.provider = 'mail' AND EXISTS (SELECT 1 FROM events d JOIN calendars dc ON dc.id = d.calendar_id
			WHERE d.uid = e.uid AND dc.provider != 'mail'))`
	args := []any{from.UnixMilli(), to.UnixMilli()}
	if len(calendarIDs) > 0 {
		q += ` AND e.calendar_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(calendarIDs)), ",") + `)`
		for _, id := range calendarIDs {
			args = append(args, id)
		}
	} else {
		q += ` AND c.hidden = 0`
	}
	rows, err := db.Query(q+` ORDER BY o.start_at, o.end_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OccurrenceView{}
	for rows.Next() {
		var v OccurrenceView
		var s, e int64
		if err := rows.Scan(&v.EventID, &v.CalendarID, &v.UID, &s, &e, &v.AllDay, &v.Summary, &v.Location,
			&v.ConferenceURL, &v.Status, &v.ShowAs, &v.MyStatus, &v.Recurring, &v.Proposed, &v.Color,
			&v.Attendees, &v.Charge); err != nil {
			return nil, err
		}
		v.Start, v.End = time.UnixMilli(s).UTC(), time.UnixMilli(e).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

// --- the outgoing queue -----------------------------------------------------

// CalendarChange is a change waiting to reach a provider, or waiting for a
// person to approve it.
type CalendarChange struct {
	ID         string          `json:"id"`
	EventID    string          `json:"eventId,omitempty"`
	CalendarID string          `json:"calendarId"`
	Op         string          `json:"op"` // create, update, delete, rsvp
	Payload    json.RawMessage `json:"payload"`
	State      string          `json:"state"` // proposed, pending, done, failed, conflict
	Origin     string          `json:"origin"`
	Error      string          `json:"error,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// QueueCalendarChange records a change. An agent's change starts as
// proposed; anyone else's as pending.
func QueueCalendarChange(c CalendarChange) (string, error) {
	if c.ID == "" {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d", c.CalendarID, c.EventID, c.Op, time.Now().UnixNano())))
		c.ID = "chg_" + hex.EncodeToString(sum[:8])
	}
	if c.State == "" {
		c.State = "pending"
		if c.Origin == "agent" {
			c.State = "proposed"
		}
	}
	if c.Origin == "" {
		c.Origin = "human"
	}
	if len(c.Payload) == 0 {
		c.Payload = json.RawMessage("{}")
	}
	now := time.Now().UnixMilli()
	_, err := db.Exec(`INSERT INTO calendar_changes (id, event_id, calendar_id, op, payload, state, origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, nullIfEmpty(c.EventID), c.CalendarID, c.Op, string(c.Payload),
		c.State, c.Origin, now, now)
	return c.ID, err
}

// SetCalendarChangeState moves a change along, with an error when it failed.
func SetCalendarChangeState(id, state, errText string) error {
	res, err := db.Exec(`UPDATE calendar_changes SET state = ?, error = ?, updated_at = ? WHERE id = ?`,
		state, nullIfEmpty(errText), time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListCalendarChanges returns changes in the given states, oldest first.
func ListCalendarChanges(states ...string) ([]CalendarChange, error) {
	q := `SELECT id, COALESCE(event_id,''), calendar_id, op, payload, state, origin, COALESCE(error,''), created_at, updated_at
		FROM calendar_changes`
	var args []any
	if len(states) > 0 {
		q += ` WHERE state IN (` + strings.TrimSuffix(strings.Repeat("?,", len(states)), ",") + `)`
		for _, s := range states {
			args = append(args, s)
		}
	}
	rows, err := db.Query(q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarChange
	for rows.Next() {
		var c CalendarChange
		var payload string
		var created, updated int64
		if err := rows.Scan(&c.ID, &c.EventID, &c.CalendarID, &c.Op, &payload, &c.State, &c.Origin, &c.Error,
			&created, &updated); err != nil {
			return nil, err
		}
		c.Payload = json.RawMessage(payload)
		c.CreatedAt, c.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCalendarChange returns one change.
func GetCalendarChange(id string) (*CalendarChange, error) {
	all, err := ListCalendarChanges()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, sql.ErrNoRows
}

// PruneCalendar removes the events a full pull did not return — they are
// gone at the provider. Events with a change still waiting to go up are kept.
func PruneCalendar(calendarID string, keepRemote map[string]bool) error {
	rows, err := db.Query(`SELECT uid, recurrence_id, COALESCE(remote_id,'') FROM events WHERE calendar_id = ?
		AND id NOT IN (SELECT event_id FROM calendar_changes WHERE state IN ('pending','proposed') AND event_id IS NOT NULL)`, calendarID)
	if err != nil {
		return err
	}
	type gone struct {
		uid string
		rid int64
	}
	var drop []gone
	for rows.Next() {
		var g gone
		var remote string
		if err := rows.Scan(&g.uid, &g.rid, &remote); err != nil {
			rows.Close()
			return err
		}
		if remote != "" && !keepRemote[remote] {
			drop = append(drop, g)
		}
	}
	rows.Close()
	for _, g := range drop {
		var t time.Time
		if g.rid != 0 {
			t = time.UnixMilli(g.rid).UTC()
		}
		if err := DeleteEvent(calendarID, g.uid, t); err != nil {
			return err
		}
	}
	return nil
}

// EventsInCalendar returns every event in a calendar, with attendees.
func EventsInCalendar(calendarID string) ([]StoredEvent, error) {
	rows, err := db.Query(`SELECT `+eventColumns+` FROM events e WHERE e.calendar_id = ? AND e.proposed = 0
		ORDER BY e.uid, e.recurrence_id`, calendarID)
	if err != nil {
		return nil, err
	}
	var out []StoredEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	rows.Close()
	return out, loadAttendees(out)
}
