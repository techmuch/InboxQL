package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The bookkeeping that lets the work loop be measured (internal/work).
//
// # Why provenance is the first thing recorded
//
// `work next --template` fills every answer in advance with a guess, so a
// harness only edits what differs. A template sent back untouched used to be
// stored exactly as though the model had read the conversation — and a guessed
// "none" closed the question for good. Every number about the loop was then a
// blend of what models concluded and what a keyword rule guessed. Answers that
// went back exactly as pre-filled are now marked `prefilled`, so they can be
// measured apart, asked again by a deeper read, and kept out of what a
// sender's history teaches the next template.

func migrateWorkQuality(db *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE annotations ADD COLUMN prefilled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE work_leases ADD COLUMN batch_id TEXT`,
		`ALTER TABLE work_leases ADD COLUMN prefill TEXT`,
		// One `work next` or `work audit next`: who took it, how much, and
		// what reading it cost, when the harness says.
		`CREATE TABLE IF NOT EXISTS work_batches (
			id             TEXT PRIMARY KEY,
			kind           TEXT NOT NULL,
			model          TEXT,
			items          INTEGER NOT NULL,
			created_at     INTEGER NOT NULL,
			input_tokens   INTEGER NOT NULL DEFAULT 0,
			output_tokens  INTEGER NOT NULL DEFAULT 0,
			cost_usd       REAL NOT NULL DEFAULT 0,
			seconds        REAL NOT NULL DEFAULT 0,
			usage_reported INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_batches_created ON work_batches(created_at)`,
		// Every submission, stored or refused: refusals are rework, and what
		// they were refused for is what the guide should say better.
		`CREATE TABLE IF NOT EXISTS work_submissions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			batch_id     TEXT,
			kind         TEXT NOT NULL,
			thread_key   TEXT NOT NULL,
			model        TEXT,
			leased_at    INTEGER,
			submitted_at INTEGER NOT NULL,
			ok           INTEGER NOT NULL,
			problems     TEXT,
			answers      INTEGER NOT NULL DEFAULT 0,
			prefilled    INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_submissions_at ON work_submissions(submitted_at)`,
		// What a submission did to the mailbox — labels, archiving, proposed
		// cards and events — so a person undoing it is counted as a
		// correction, the one signal they give without being asked.
		`CREATE TABLE IF NOT EXISTS work_actions (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			thread_key TEXT,
			message_id TEXT,
			kind       TEXT NOT NULL,
			ref        TEXT NOT NULL,
			model      TEXT,
			created_at INTEGER NOT NULL,
			undone_at  INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_actions_message ON work_actions(message_id, kind, ref)`,
		`CREATE INDEX IF NOT EXISTS idx_work_actions_created ON work_actions(created_at)`,
		// Conversations to read again properly: a check disagreed, or a
		// person asked.
		`CREATE TABLE IF NOT EXISTS work_flags (
			thread_key TEXT PRIMARY KEY,
			reason     TEXT NOT NULL,
			source     TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS work_audit_leases (
			thread_key TEXT PRIMARY KEY,
			token      TEXT NOT NULL,
			asked      TEXT NOT NULL,
			batch_id   TEXT,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		// One answer read a second time without sight of the first, and
		// whether the two agree. agree is NULL when they cannot be compared
		// (either skipped).
		`CREATE TABLE IF NOT EXISTS work_audits (
			id                  TEXT PRIMARY KEY,
			batch_id            TEXT,
			thread_key          TEXT NOT NULL,
			message_id          TEXT NOT NULL,
			target              TEXT NOT NULL,
			element             TEXT NOT NULL,
			stratum             TEXT NOT NULL,
			reasons             TEXT,
			sender              TEXT,
			original            TEXT NOT NULL,
			original_model      TEXT,
			original_prefilled  INTEGER NOT NULL DEFAULT 0,
			original_confidence REAL,
			checker             TEXT NOT NULL,
			checker_model       TEXT,
			agree               INTEGER,
			created_at          INTEGER NOT NULL,
			resolved_at         INTEGER,
			resolution          TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_audits_element ON work_audits(element, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_work_audits_thread ON work_audits(thread_key)`,
		`CREATE INDEX IF NOT EXISTS idx_work_audits_sender ON work_audits(sender, created_at)`,
		// How right each element has to be, and how much a wrong answer
		// costs. Absent rows take the defaults in internal/work.
		`CREATE TABLE IF NOT EXISTS work_targets (
			element    TEXT PRIMARY KEY,
			target     REAL NOT NULL,
			cost       TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}

// Work batch kinds.
const (
	WorkKindRead  = "read"
	WorkKindDeep  = "deep"
	WorkKindAudit = "audit"
)

// WorkUsage is what reading a batch cost, as the harness reports it.
type WorkUsage struct {
	InputTokens  int     `json:"inputTokens,omitempty"`
	OutputTokens int     `json:"outputTokens,omitempty"`
	CostUSD      float64 `json:"costUsd,omitempty"`
	Seconds      float64 `json:"seconds,omitempty"`
}

// Empty reports whether nothing was reported.
func (u WorkUsage) Empty() bool {
	return u.InputTokens == 0 && u.OutputTokens == 0 && u.CostUSD == 0 && u.Seconds == 0
}

// NewWorkBatchID makes an id for a batch, which is recorded once it is known
// to hold anything.
func NewWorkBatchID() string { return "wb_" + uuid.New().String()[:13] }

// CreateWorkBatch records a batch handed out.
func CreateWorkBatch(id, kind, model string, items int) (string, error) {
	if id == "" {
		id = NewWorkBatchID()
	}
	_, err := db.Exec(`INSERT INTO work_batches (id, kind, model, items, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, kind, nullIfEmpty(model), items, time.Now().UnixMilli())
	return id, err
}

// AddBatchUsage adds what a harness says reading a batch cost.
func AddBatchUsage(batchID string, u WorkUsage) error {
	if batchID == "" || u.Empty() {
		return nil
	}
	_, err := db.Exec(`UPDATE work_batches SET input_tokens = input_tokens + ?, output_tokens = output_tokens + ?,
		cost_usd = cost_usd + ?, seconds = seconds + ?, usage_reported = 1 WHERE id = ?`,
		u.InputTokens, u.OutputTokens, u.CostUSD, u.Seconds, batchID)
	return err
}

// SetBatchModel names the model of a batch taken without one, from its first
// submission.
func SetBatchModel(batchID, model string) error {
	if batchID == "" || model == "" {
		return nil
	}
	_, err := db.Exec(`UPDATE work_batches SET model = ? WHERE id = ? AND model IS NULL`, model, batchID)
	return err
}

// TakeLeaseIn is TakeLease for a conversation handed out in a batch.
func TakeLeaseIn(threadKey, asked, batchID string, d time.Duration) (*WorkLease, error) {
	l, err := TakeLease(threadKey, asked, d)
	if err != nil {
		return nil, err
	}
	l.BatchID = batchID
	_, err = db.Exec(`UPDATE work_leases SET batch_id = ?, prefill = NULL WHERE thread_key = ? AND token = ?`,
		nullIfEmpty(batchID), threadKey, l.Token)
	return l, err
}

// SetLeasePrefill keeps what the template pre-filled for a conversation, so
// the submission can be compared with it.
func SetLeasePrefill(threadKey, token, prefill string) error {
	_, err := db.Exec(`UPDATE work_leases SET prefill = ? WHERE thread_key = ? AND token = ?`,
		prefill, threadKey, token)
	return err
}

// WorkSubmissionRecord is one submission, as the metrics count it.
type WorkSubmissionRecord struct {
	BatchID   string
	Kind      string
	Thread    string
	Model     string
	LeasedAt  time.Time
	OK        bool
	Problems  []string
	Answers   int
	Prefilled int
}

// RecordWorkSubmission notes a submission, stored or refused.
func RecordWorkSubmission(r WorkSubmissionRecord) error {
	var probs any
	if len(r.Problems) > 0 {
		b, _ := json.Marshal(r.Problems)
		probs = string(b)
	}
	var leased any
	if !r.LeasedAt.IsZero() {
		leased = r.LeasedAt.UnixMilli()
	}
	_, err := db.Exec(`INSERT INTO work_submissions
		(batch_id, kind, thread_key, model, leased_at, submitted_at, ok, problems, answers, prefilled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullIfEmpty(r.BatchID), r.Kind, r.Thread, nullIfEmpty(r.Model), leased, time.Now().UnixMilli(),
		boolToInt(r.OK), probs, r.Answers, r.Prefilled)
	return err
}

// Work action kinds.
const (
	WorkActionLabel   = "label"
	WorkActionArchive = "archive"
	WorkActionTicket  = "ticket"
	WorkActionEvent   = "event"
)

// RecordWorkAction notes something a submission did, so undoing it counts.
func RecordWorkAction(threadKey, messageID, kind, ref, model string) error {
	_, err := db.Exec(`INSERT INTO work_actions (thread_key, message_id, kind, ref, model, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, nullIfEmpty(threadKey), nullIfEmpty(messageID), kind, ref,
		nullIfEmpty(model), time.Now().UnixMilli())
	return err
}

// noteUndone marks an agent's label or archive as taken back by a person.
// Called inside changeLabels for a person's changes only: a label the server
// drops, or the agent itself changes, is not a correction.
func noteUndone(tx *sql.Tx, messageID string, removed, added []string, now int64) error {
	for _, l := range removed {
		if _, err := tx.Exec(`UPDATE work_actions SET undone_at = ?
			WHERE message_id = ? AND kind = ? AND ref = ? AND undone_at IS NULL`,
			now, messageID, WorkActionLabel, l); err != nil {
			return err
		}
	}
	for _, l := range added {
		if l != LabelInbox {
			continue
		}
		if _, err := tx.Exec(`UPDATE work_actions SET undone_at = ?
			WHERE message_id = ? AND kind = ? AND undone_at IS NULL`,
			now, messageID, WorkActionArchive); err != nil {
			return err
		}
	}
	return nil
}

// Flags.

// WorkFlag is a conversation marked for a deeper read.
type WorkFlag struct {
	Thread    string    `json:"thread"`
	Reason    string    `json:"reason"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
}

// FlagThread marks a conversation to be read again properly. A second flag
// keeps the first reason.
func FlagThread(threadKey, reason, source string) error {
	_, err := db.Exec(`INSERT INTO work_flags (thread_key, reason, source, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(thread_key) DO NOTHING`, threadKey, reason, source, time.Now().UnixMilli())
	return err
}

// UnflagThread clears a flag.
func UnflagThread(threadKey string) error {
	_, err := db.Exec(`DELETE FROM work_flags WHERE thread_key = ?`, threadKey)
	return err
}

// ThreadFlag returns a conversation's flag, or nil.
func ThreadFlag(threadKey string) (*WorkFlag, error) {
	var f WorkFlag
	var at int64
	err := db.QueryRow(`SELECT thread_key, reason, source, created_at FROM work_flags WHERE thread_key = ?`,
		threadKey).Scan(&f.Thread, &f.Reason, &f.Source, &at)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.CreatedAt = time.UnixMilli(at)
	return &f, nil
}

// FlaggedThreads lists flagged conversations, oldest flag first.
func FlaggedThreads(limit int) ([]WorkFlag, error) {
	rows, err := db.Query(`SELECT thread_key, reason, source, created_at FROM work_flags
		ORDER BY created_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkFlag
	for rows.Next() {
		var f WorkFlag
		var at int64
		if err := rows.Scan(&f.Thread, &f.Reason, &f.Source, &at); err != nil {
			return nil, err
		}
		f.CreatedAt = time.UnixMilli(at)
		out = append(out, f)
	}
	return out, rows.Err()
}

// CountFlags counts flagged conversations.
func CountFlags() (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM work_flags`).Scan(&n)
	return n, err
}

// AnsweredBy reports whether a message has an answer that settles it for a
// read of this depth. A deep read does not count an answer that went back
// exactly as the template guessed it: nobody concluded it.
func AnsweredBy(a *Annotator, messageID string, deep bool) (bool, error) {
	if !deep {
		return Answered(a, messageID)
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM annotations
		WHERE message_id = ? AND annotator_id = ?
		  AND (source = ? OR (annotator_version = ? AND status IN (?, ?, ?) AND prefilled = 0))`,
		messageID, a.ID, SourceHuman, a.Version, StatusOK, StatusEmpty, StatusFailed).Scan(&n)
	return n > 0, err
}

// HumanRuled reports whether a person has ruled on a message for an annotator.
func HumanRuled(a *Annotator, messageID string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM annotations WHERE message_id = ? AND annotator_id = ? AND source = ?`,
		messageID, a.ID, SourceHuman).Scan(&n)
	return n > 0, err
}

// Answers on a conversation, for a check.

// AgentAnswerRow is one stored agent annotation.
type AgentAnswerRow struct {
	Element    string
	MessageID  string
	Seq        int
	Status     string
	DataJSON   string
	Confidence *float64
	Model      string
	Prefilled  bool
	CreatedAt  time.Time
}

// AgentAnswersOn returns the agent's current answers on a conversation that
// no person has ruled on — what a check can compare against.
func AgentAnswersOn(threadKey string) ([]AgentAnswerRow, error) {
	rows, err := db.Query(`
		SELECT an.name, x.message_id, x.seq, x.status, x.data_json, x.confidence, COALESCE(x.model, ''),
		       x.prefilled, x.created_at
		FROM annotations x
		JOIN messages m ON m.id = x.message_id
		JOIN annotators an ON an.id = x.annotator_id AND an.version = x.annotator_version
		WHERE m.thread_key = ? AND x.source = ?
		  AND NOT EXISTS (SELECT 1 FROM annotations h WHERE h.message_id = x.message_id
		                  AND h.annotator_id = x.annotator_id AND h.source = ?)
		ORDER BY an.name, x.message_id, x.seq`, threadKey, SourceAgent, SourceHuman)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentAnswerRow
	for rows.Next() {
		var r AgentAnswerRow
		var conf sql.NullFloat64
		var pre int
		var at int64
		if err := rows.Scan(&r.Element, &r.MessageID, &r.Seq, &r.Status, &r.DataJSON, &conf, &r.Model, &pre, &at); err != nil {
			return nil, err
		}
		if conf.Valid {
			c := conf.Float64
			r.Confidence = &c
		}
		r.Prefilled, r.CreatedAt = pre == 1, time.UnixMilli(at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// AuditCandidates returns conversations with agent answers given since a time
// and never checked, not being read or checked now: chosen at random when
// random, else the most recent first.
func AuditCandidates(since time.Time, limit int, random bool) ([]string, error) {
	order := "MAX(x.created_at) DESC"
	if random {
		order = "RANDOM()"
	}
	now := time.Now().UnixMilli()
	rows, err := db.Query(`
		SELECT m.thread_key FROM annotations x
		JOIN messages m ON m.id = x.message_id
		JOIN annotators an ON an.id = x.annotator_id AND an.version = x.annotator_version
		WHERE x.source = ? AND x.created_at >= ? AND m.thread_key IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM work_audits w WHERE w.thread_key = m.thread_key)
		  AND NOT EXISTS (SELECT 1 FROM work_audit_leases l WHERE l.thread_key = m.thread_key AND l.expires_at >= ?)
		  AND NOT EXISTS (SELECT 1 FROM work_leases l WHERE l.thread_key = m.thread_key AND l.expires_at >= ?)
		GROUP BY m.thread_key
		ORDER BY `+order+` LIMIT ?`, SourceAgent, since.UnixMilli(), now, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Audit leases, kept apart from reading leases: a conversation can be read
// again while an old answer on it is being checked, and the two must not
// cancel each other.

// AuditLeaseFor returns the check lease on a conversation, or nil.
func AuditLeaseFor(threadKey string) (*WorkLease, error) {
	var l WorkLease
	var exp, at int64
	var batch sql.NullString
	err := db.QueryRow(`SELECT thread_key, token, asked, expires_at, created_at, batch_id FROM work_audit_leases WHERE thread_key = ?`,
		threadKey).Scan(&l.ThreadKey, &l.Token, &l.Asked, &exp, &at, &batch)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.Expires, l.CreatedAt, l.BatchID = time.UnixMilli(exp), time.UnixMilli(at), batch.String
	return &l, nil
}

// TakeAuditLease hands a conversation to a checker.
func TakeAuditLease(threadKey, asked, batchID string, d time.Duration) (*WorkLease, error) {
	now := time.Now()
	l := &WorkLease{ThreadKey: threadKey, Token: uuid.New().String(), Asked: asked, Expires: now.Add(d),
		CreatedAt: now, BatchID: batchID}
	res, err := db.Exec(`
		INSERT INTO work_audit_leases (thread_key, token, asked, batch_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(thread_key) DO UPDATE SET
			token = excluded.token, asked = excluded.asked, batch_id = excluded.batch_id,
			expires_at = excluded.expires_at, created_at = excluded.created_at
		WHERE work_audit_leases.expires_at < ?`,
		threadKey, l.Token, asked, nullIfEmpty(batchID), l.Expires.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("conversation %s is being checked by another run", threadKey)
	}
	return l, nil
}

// DropAuditLease ends a check lease.
func DropAuditLease(threadKey string) error {
	_, err := db.Exec(`DELETE FROM work_audit_leases WHERE thread_key = ?`, threadKey)
	return err
}

// Audits.

// Audit strata. Random checks measure; targeted ones find mistakes. Only the
// random ones estimate how often the loop is wrong — the targeted ones were
// chosen because they looked likely to be.
const (
	StratumRandom   = "random"
	StratumTargeted = "targeted"
)

// Resolutions of a disagreement.
const (
	ResolvedOriginal = "original"
	ResolvedChecker  = "checker"
	ResolvedNeither  = "neither"
)

// WorkAudit is one answer checked.
type WorkAudit struct {
	ID                 string    `json:"id"`
	BatchID            string    `json:"batchId,omitempty"`
	Thread             string    `json:"thread"`
	MessageID          string    `json:"messageId"`
	Target             string    `json:"target"`
	Element            string    `json:"element"`
	Stratum            string    `json:"stratum"`
	Reasons            []string  `json:"reasons,omitempty"`
	Sender             string    `json:"sender,omitempty"`
	Original           string    `json:"original"`
	OriginalModel      string    `json:"originalModel,omitempty"`
	OriginalPrefilled  bool      `json:"originalPrefilled,omitempty"`
	OriginalConfidence *float64  `json:"originalConfidence,omitempty"`
	Checker            string    `json:"checker"`
	CheckerModel       string    `json:"checkerModel,omitempty"`
	Agree              *bool     `json:"agree,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	ResolvedAt         time.Time `json:"resolvedAt,omitempty"`
	Resolution         string    `json:"resolution,omitempty"`

	// For showing it: the message the answer is about.
	Subject string    `json:"subject,omitempty"`
	From    string    `json:"from,omitempty"`
	Date    time.Time `json:"date,omitempty"`
	Snippet string    `json:"snippet,omitempty"`
}

// SaveAudit stores a check.
func SaveAudit(a *WorkAudit) error {
	if a.ID == "" {
		a.ID = "wa_" + uuid.New().String()[:13]
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	var agree any
	if a.Agree != nil {
		agree = boolToInt(*a.Agree)
	}
	var reasons any
	if len(a.Reasons) > 0 {
		b, _ := json.Marshal(a.Reasons)
		reasons = string(b)
	}
	_, err := db.Exec(`INSERT INTO work_audits (id, batch_id, thread_key, message_id, target, element, stratum,
		reasons, sender, original, original_model, original_prefilled, original_confidence, checker, checker_model,
		agree, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, nullIfEmpty(a.BatchID), a.Thread, a.MessageID, a.Target, a.Element, a.Stratum, reasons,
		nullIfEmpty(a.Sender), a.Original, nullIfEmpty(a.OriginalModel), boolToInt(a.OriginalPrefilled),
		a.OriginalConfidence, a.Checker, nullIfEmpty(a.CheckerModel), agree, a.CreatedAt.UnixMilli())
	return err
}

const auditColumns = `w.id, COALESCE(w.batch_id, ''), w.thread_key, w.message_id, w.target, w.element, w.stratum,
	COALESCE(w.reasons, ''), COALESCE(w.sender, ''), w.original, COALESCE(w.original_model, ''), w.original_prefilled,
	w.original_confidence, w.checker, COALESCE(w.checker_model, ''), w.agree, w.created_at,
	COALESCE(w.resolved_at, 0), COALESCE(w.resolution, ''),
	COALESCE(m.subject, ''), COALESCE(m.from_addr, ''), COALESCE(m.date, 0), COALESCE(SUBSTR(COALESCE(NULLIF(m.body, ''), m.normalized_body), 1, 280), '')`

func scanAudit(scan func(...any) error) (*WorkAudit, error) {
	var a WorkAudit
	var reasons string
	var pre int
	var conf sql.NullFloat64
	var agree sql.NullInt64
	var at, resolved, date int64
	if err := scan(&a.ID, &a.BatchID, &a.Thread, &a.MessageID, &a.Target, &a.Element, &a.Stratum,
		&reasons, &a.Sender, &a.Original, &a.OriginalModel, &pre, &conf, &a.Checker, &a.CheckerModel,
		&agree, &at, &resolved, &a.Resolution, &a.Subject, &a.From, &date, &a.Snippet); err != nil {
		return nil, err
	}
	if reasons != "" {
		_ = json.Unmarshal([]byte(reasons), &a.Reasons)
	}
	a.OriginalPrefilled = pre == 1
	if conf.Valid {
		c := conf.Float64
		a.OriginalConfidence = &c
	}
	if agree.Valid {
		v := agree.Int64 == 1
		a.Agree = &v
	}
	a.CreatedAt = time.UnixMilli(at)
	if resolved > 0 {
		a.ResolvedAt = time.UnixMilli(resolved)
	}
	if date > 0 {
		a.Date = time.UnixMilli(date)
	}
	a.Snippet = strings.Join(strings.Fields(a.Snippet), " ")
	return &a, nil
}

// GetAudit returns one check, or nil.
func GetAudit(id string) (*WorkAudit, error) {
	row := db.QueryRow(`SELECT `+auditColumns+` FROM work_audits w LEFT JOIN messages m ON m.id = w.message_id
		WHERE w.id = ?`, id)
	a, err := scanAudit(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// Disagreements lists checks that disagreed, open ones (no person has said
// which was right) or all, newest first.
func Disagreements(open bool, limit int) ([]*WorkAudit, error) {
	cond := ""
	if open {
		cond = " AND w.resolved_at IS NULL"
	}
	rows, err := db.Query(`SELECT `+auditColumns+` FROM work_audits w LEFT JOIN messages m ON m.id = w.message_id
		WHERE w.agree = 0`+cond+` ORDER BY w.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WorkAudit
	for rows.Next() {
		a, err := scanAudit(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountOpenDisagreements counts disagreements waiting for a person.
func CountOpenDisagreements() (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM work_audits WHERE agree = 0 AND resolved_at IS NULL`).Scan(&n)
	return n, err
}

// ResolveAudit records which answer a person said was right.
func ResolveAudit(id, resolution string) error {
	switch resolution {
	case ResolvedOriginal, ResolvedChecker, ResolvedNeither:
	default:
		return fmt.Errorf("a disagreement is resolved %q, %q or %q, not %q",
			ResolvedOriginal, ResolvedChecker, ResolvedNeither, resolution)
	}
	res, err := db.Exec(`UPDATE work_audits SET resolution = ?, resolved_at = ? WHERE id = ? AND agree = 0`,
		resolution, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// OpenDisagreementsOn counts a conversation's unresolved disagreements.
func OpenDisagreementsOn(threadKey string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM work_audits WHERE thread_key = ? AND agree = 0 AND resolved_at IS NULL`,
		threadKey).Scan(&n)
	return n, err
}

// AuditOutcome is one checked answer reduced to what the estimates use.
type AuditOutcome struct {
	Element    string
	Stratum    string
	Sender     string
	Model      string
	Prefilled  bool
	Confidence *float64
	// Right is whether the first answer stands: the check agreed, or a
	// person said the first was right. Known is false for a disagreement
	// nobody has settled yet — counted as neither right nor wrong.
	Right bool
	Known bool
	At    time.Time
}

// AuditOutcomes returns every comparable check since a time, optionally for
// one model's answers, newest first.
func AuditOutcomes(since time.Time, model string) ([]AuditOutcome, error) {
	q := `SELECT element, stratum, COALESCE(sender, ''), COALESCE(original_model, ''), original_prefilled,
		original_confidence, agree, COALESCE(resolution, ''), created_at
		FROM work_audits WHERE agree IS NOT NULL AND created_at >= ?`
	args := []any{since.UnixMilli()}
	if model != "" {
		q += ` AND original_model = ?`
		args = append(args, model)
	}
	rows, err := db.Query(q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditOutcome
	for rows.Next() {
		var o AuditOutcome
		var pre, agree int
		var conf sql.NullFloat64
		var res string
		var at int64
		if err := rows.Scan(&o.Element, &o.Stratum, &o.Sender, &o.Model, &pre, &conf, &agree, &res, &at); err != nil {
			return nil, err
		}
		o.Prefilled, o.At = pre == 1, time.UnixMilli(at)
		if conf.Valid {
			c := conf.Float64
			o.Confidence = &c
		}
		switch {
		case agree == 1:
			o.Right, o.Known = true, true
		case res == ResolvedOriginal:
			o.Right, o.Known = true, true
		case res == ResolvedChecker || res == ResolvedNeither:
			o.Known = true
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SenderRecord is how a sender's answers have fared when checked.
type SenderRecord struct {
	// Streak is how many checks in a row, newest first, found the first
	// answer right. An open disagreement ends it, as a wrong answer would.
	Streak int
	Checks int
}

// SenderRecords returns the check record of each sender asked about.
func SenderRecords(senders []string) (map[string]SenderRecord, error) {
	out := map[string]SenderRecord{}
	if len(senders) == 0 {
		return out, nil
	}
	rows, err := db.Query(`SELECT sender, agree, COALESCE(resolution, '') FROM work_audits
		WHERE agree IS NOT NULL AND sender IN (`+placeholders(len(senders))+`)
		ORDER BY sender, created_at DESC`, anySlice(senders)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	broken := map[string]bool{}
	for rows.Next() {
		var s, res string
		var agree int
		if err := rows.Scan(&s, &agree, &res); err != nil {
			return nil, err
		}
		r := out[s]
		r.Checks++
		if agree == 1 || res == ResolvedOriginal {
			if !broken[s] {
				r.Streak++
			}
		} else {
			broken[s] = true
		}
		out[s] = r
	}
	return out, rows.Err()
}

// Targets.

// WorkTarget is how right an element has to be, and what a wrong answer costs.
type WorkTarget struct {
	Element string  `json:"element"`
	Target  float64 `json:"target"`
	Cost    string  `json:"cost"`
}

// WorkTargets returns the targets set, by element.
func WorkTargets() (map[string]WorkTarget, error) {
	rows, err := db.Query(`SELECT element, target, cost FROM work_targets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]WorkTarget{}
	for rows.Next() {
		var t WorkTarget
		if err := rows.Scan(&t.Element, &t.Target, &t.Cost); err != nil {
			return nil, err
		}
		out[t.Element] = t
	}
	return out, rows.Err()
}

// SetWorkTarget sets an element's target and cost.
func SetWorkTarget(t WorkTarget) error {
	_, err := db.Exec(`INSERT INTO work_targets (element, target, cost, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(element) DO UPDATE SET target = excluded.target, cost = excluded.cost, updated_at = excluded.updated_at`,
		t.Element, t.Target, t.Cost, time.Now().UnixMilli())
	return err
}

// Measures.

// WorkDay is one day of the loop.
type WorkDay struct {
	Day     string `json:"day"`
	Stored  int    `json:"stored"`
	Refused int    `json:"refused"`
	Checked int    `json:"checked"`
}

// WorkProblemCount is a reason submissions were refused, and how often.
type WorkProblemCount struct {
	Problem string `json:"problem"`
	Count   int    `json:"count"`
}

// WorkSubmissionStats is what the submissions since a time say about effort.
type WorkSubmissionStats struct {
	Stored        int                `json:"stored"`
	Refused       int                `json:"refused"`
	MedianSeconds float64            `json:"medianSeconds"`
	Days          []WorkDay          `json:"days"`
	Problems      []WorkProblemCount `json:"problems"`
}

func modelClause(col, model string, args []any) (string, []any) {
	if model == "" {
		return "", args
	}
	return " AND " + col + " = ?", append(args, model)
}

// SubmissionStats summarises reading submissions since a time.
func SubmissionStats(since time.Time, model string) (*WorkSubmissionStats, error) {
	s := &WorkSubmissionStats{Days: []WorkDay{}, Problems: []WorkProblemCount{}}
	cond, args := modelClause("model", model, []any{WorkKindAudit, since.UnixMilli()})
	rows, err := db.Query(`SELECT ok, COALESCE(problems, ''), COALESCE(leased_at, 0), submitted_at
		FROM work_submissions WHERE kind != ? AND submitted_at >= ?`+cond+` ORDER BY submitted_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	days := map[string]*WorkDay{}
	var order []string
	day := func(at int64) *WorkDay {
		d := time.UnixMilli(at).Format("2006-01-02")
		if days[d] == nil {
			days[d] = &WorkDay{Day: d}
			order = append(order, d)
		}
		return days[d]
	}
	var secs []float64
	probs := map[string]int{}
	for rows.Next() {
		var ok int
		var problems string
		var leased, at int64
		if err := rows.Scan(&ok, &problems, &leased, &at); err != nil {
			return nil, err
		}
		if ok == 1 {
			s.Stored++
			day(at).Stored++
			if leased > 0 && at >= leased {
				secs = append(secs, float64(at-leased)/1000)
			}
			continue
		}
		s.Refused++
		day(at).Refused++
		var list []string
		_ = json.Unmarshal([]byte(problems), &list)
		seen := map[string]bool{}
		for _, p := range list {
			if k := problemKind(p); !seen[k] {
				seen[k] = true
				probs[k]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Checks per day, beside the reads.
	crows, err := db.Query(`SELECT created_at FROM work_audits WHERE created_at >= ?`, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var at int64
		if err := crows.Scan(&at); err != nil {
			crows.Close()
			return nil, err
		}
		day(at).Checked++
	}
	crows.Close()
	sort.Strings(order)
	for _, d := range order {
		s.Days = append(s.Days, *days[d])
	}
	if len(secs) > 0 {
		sort.Float64s(secs)
		s.MedianSeconds = secs[len(secs)/2]
	}
	for k, n := range probs {
		s.Problems = append(s.Problems, WorkProblemCount{Problem: k, Count: n})
	}
	sort.Slice(s.Problems, func(i, j int) bool {
		if s.Problems[i].Count != s.Problems[j].Count {
			return s.Problems[i].Count > s.Problems[j].Count
		}
		return s.Problems[i].Problem < s.Problems[j].Problem
	})
	if len(s.Problems) > 8 {
		s.Problems = s.Problems[:8]
	}
	return s, nil
}

// problemKind reduces a problem to its kind, so "quote "x" is not in the
// conversation" and "quote "y" is…" count together. The element stays: which
// question trips harnesses up is the useful part.
func problemKind(p string) string {
	el, rest := "", p
	if i := strings.Index(p, ": "); i > 0 && !strings.Contains(p[:i], " ") {
		el, rest = p[:i], p[i+2:]
	}
	switch {
	case strings.Contains(rest, "is not in the conversation"):
		rest = "quote not found in the conversation"
	case strings.Contains(rest, "quote is required"):
		rest = "quote missing"
	case strings.Contains(rest, "not answered"):
		rest = "not answered"
	case strings.Contains(rest, "lease"):
		rest = "lease missing or taken"
	case strings.Contains(rest, "instructions changed"):
		rest = "instructions changed since taken"
	case strings.Contains(rest, "was not asked about"), strings.Contains(rest, "not asked about this conversation"):
		rest = "answered something not asked"
	case strings.Contains(rest, "exactly one of"):
		rest = "not exactly one of level, records, tags, none or skip"
	default:
		if len(rest) > 60 {
			rest = rest[:60] + "…"
		}
	}
	if el != "" {
		return el + ": " + rest
	}
	return rest
}

// WorkBatchStats is what the batches since a time say about effort.
type WorkBatchStats struct {
	Batches int `json:"batches"`
	Leased  int `json:"leased"`
	// Expired counts conversations leased and never stored, in batches old
	// enough that their leases have run out.
	Expired int `json:"expired"`
	// Usage is summed over the batches that reported it, and Conversations
	// is how many conversations those batches stored.
	UsageBatches  int       `json:"usageBatches"`
	Conversations int       `json:"usageConversations"`
	Usage         WorkUsage `json:"usage"`
}

// BatchStats summarises reading batches since a time.
func BatchStats(since time.Time, model string, leaseFor time.Duration) (*WorkBatchStats, error) {
	s := &WorkBatchStats{}
	cond, args := modelClause("b.model", model, []any{WorkKindAudit, since.UnixMilli()})
	rows, err := db.Query(`SELECT b.id, b.items, b.created_at, b.usage_reported, b.input_tokens, b.output_tokens,
		b.cost_usd, b.seconds,
		(SELECT COUNT(DISTINCT s.thread_key) FROM work_submissions s WHERE s.batch_id = b.id AND s.ok = 1)
		FROM work_batches b WHERE b.kind != ? AND b.created_at >= ?`+cond, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cutoff := time.Now().Add(-leaseFor).UnixMilli()
	for rows.Next() {
		var id string
		var items, reported, stored int
		var at int64
		var u WorkUsage
		if err := rows.Scan(&id, &items, &at, &reported, &u.InputTokens, &u.OutputTokens, &u.CostUSD, &u.Seconds, &stored); err != nil {
			return nil, err
		}
		s.Batches++
		s.Leased += items
		if at < cutoff && items > stored {
			s.Expired += items - stored
		}
		if reported == 1 {
			s.UsageBatches++
			s.Conversations += stored
			s.Usage.InputTokens += u.InputTokens
			s.Usage.OutputTokens += u.OutputTokens
			s.Usage.CostUSD += u.CostUSD
			s.Usage.Seconds += u.Seconds
		}
	}
	return s, rows.Err()
}

// ElementAnswerStat is how much of one element was answered, and how.
type ElementAnswerStat struct {
	Element string `json:"element"`
	// Answered counts answers (one per message, or per sender) since the time.
	Answered int `json:"answered"`
	// Prefilled counts those that went back exactly as the template guessed.
	Prefilled int `json:"prefilled"`
	// Found counts answers with something in them: a level, records, tags.
	Found int `json:"found"`
	// WithConfidence counts answers that said how sure they were.
	WithConfidence int `json:"withConfidence"`
}

// ElementAnswerStats counts agent answers by element since a time.
func ElementAnswerStats(since time.Time, model string) (map[string]ElementAnswerStat, error) {
	cond, args := modelClause("x.model", model, []any{SourceAgent, since.UnixMilli()})
	rows, err := db.Query(`SELECT an.name, COUNT(*), SUM(x.prefilled), SUM(CASE WHEN x.status = 'ok' THEN 1 ELSE 0 END),
		SUM(CASE WHEN x.confidence IS NOT NULL THEN 1 ELSE 0 END)
		FROM annotations x JOIN annotators an ON an.id = x.annotator_id
		WHERE x.source = ? AND x.seq = 0 AND x.created_at >= ?`+cond+` GROUP BY an.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ElementAnswerStat{}
	for rows.Next() {
		var s ElementAnswerStat
		if err := rows.Scan(&s.Element, &s.Answered, &s.Prefilled, &s.Found, &s.WithConfidence); err != nil {
			return nil, err
		}
		out[s.Element] = s
	}
	return out, rows.Err()
}

// WorkOutcome counts what became of things an agent proposed or did.
type WorkOutcome struct {
	Total int `json:"total"`
	// Kept: approved, accepted, left alone. Undone: rejected or taken back.
	// Open: still waiting for a person.
	Kept   int `json:"kept"`
	Undone int `json:"undone"`
	Open   int `json:"open"`
}

// WorkActionOutcomes counts what became of an agent's labels, archiving,
// proposed cards and proposed events since a time.
func WorkActionOutcomes(since time.Time, model string) (map[string]WorkOutcome, error) {
	cond, args := modelClause("w.model", model, []any{since.UnixMilli()})
	rows, err := db.Query(`SELECT w.kind,
		CASE
		  WHEN w.kind IN ('label', 'archive') THEN CASE WHEN w.undone_at IS NULL THEN 'kept' ELSE 'undone' END
		  WHEN w.kind = 'ticket' THEN COALESCE((SELECT CASE t.status WHEN 'proposed' THEN 'open'
		       WHEN 'rejected' THEN 'undone' ELSE 'kept' END FROM tickets t WHERE t.id = w.ref), 'undone')
		  WHEN w.kind = 'event' THEN COALESCE((SELECT CASE c.state WHEN 'proposed' THEN 'open'
		       WHEN 'rejected' THEN 'undone' ELSE 'kept' END FROM calendar_changes c WHERE c.id = w.ref), 'undone')
		END
		FROM work_actions w WHERE w.created_at >= ?`+cond, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]WorkOutcome{}
	for rows.Next() {
		var kind, state string
		if err := rows.Scan(&kind, &state); err != nil {
			return nil, err
		}
		o := out[kind]
		o.Total++
		switch state {
		case "kept":
			o.Kept++
		case "undone":
			o.Undone++
		case "open":
			o.Open++
		}
		out[kind] = o
	}
	return out, rows.Err()
}

// WorkModels lists the models that have answered through the loop.
func WorkModels() ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT model FROM annotations WHERE source = ? AND model IS NOT NULL AND model != ''
		UNION SELECT DISTINCT model FROM work_batches WHERE model IS NOT NULL ORDER BY 1`, SourceAgent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AnswersSince counts conversations stored by the loop since a time.
func AnswersSince(since time.Time) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(DISTINCT thread_key) FROM work_submissions WHERE ok = 1 AND kind != ? AND submitted_at >= ?`,
		WorkKindAudit, since.UnixMilli()).Scan(&n)
	return n, err
}

// ChecksSince counts conversations checked since a time.
func ChecksSince(since time.Time) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(DISTINCT thread_key) FROM work_audits WHERE created_at >= ?`,
		since.UnixMilli()).Scan(&n)
	return n, err
}

// RecordElementRuling stores a person's verdict on an element as the rows a
// machine answer would have — a level, tags, records, or none — so an
// extractor's ruling can carry several records. Made from a check's
// disagreement it is RuledAudit: evidence about that message, not a random
// draw, so it is never scored as one.
func RecordElementRuling(annotatorID string, version int, messageID string, anns []*Annotation, via string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM annotations WHERE message_id = ? AND annotator_id = ? AND source = ?`,
		messageID, annotatorID, SourceHuman); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for i, a := range anns {
		conf := 1.0
		if a.DataJSON == "" {
			a.DataJSON = "{}"
		}
		if a.ID == "" {
			a.ID = uuid.New().String()
		}
		a.Source, a.Seq = SourceHuman, i
		if _, err := tx.Exec(`INSERT OR REPLACE INTO annotations
			(id, message_id, annotator_id, annotator_version, seq, status, source, data_json, confidence, created_at, ruled_via)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, messageID, annotatorID, version, i, a.Status, SourceHuman, a.DataJSON, conf, now, via); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ThreadMatches reports whether any of a conversation's mail matches a filter.
func ThreadMatches(threadKey, filter string) (bool, error) {
	where, args, err := filterClause(filter)
	if err != nil {
		return false, err
	}
	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM messages m WHERE m.thread_key = ? AND `+where,
		append([]any{threadKey}, args...)...).Scan(&n)
	return n > 0, err
}

// BatchKind returns a batch's kind; read when it is unknown.
func BatchKind(id string) (string, error) {
	if id == "" {
		return WorkKindRead, nil
	}
	var k string
	err := db.QueryRow(`SELECT kind FROM work_batches WHERE id = ?`, id).Scan(&k)
	if err == sql.ErrNoRows {
		return WorkKindRead, nil
	}
	return k, err
}
