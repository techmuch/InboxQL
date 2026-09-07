package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/message"
)

// Annotation status values.
//
// The three-valued distinction is the point. A message with no row was never
// evaluated; StatusEmpty means the annotator looked and found nothing. Without
// both, "-label:x" cannot tell "no" from "not yet" and silently answers with
// every message the annotator has not reached.
const (
	StatusOK     = "ok"
	StatusEmpty  = "empty"
	StatusFailed = "failed"
)

// Annotation sources, in increasing order of authority.
const (
	SourceRule  = "rule"
	SourceLLM   = "llm"
	SourceHuman = "human"
)

// Annotator kinds.
const (
	KindLabel   = "label"
	KindExtract = "extract"
)

// Annotator engines.
const (
	EngineRule = "rule"
	EngineLLM  = "llm"
)

// Annotator is a named, versioned instruction applied to messages.
//
// Labels and extractions are the same mechanism: a label is an annotator whose
// output schema is a boolean, an extraction is one whose schema has fields.
// They share versioning, incremental re-run, confidence, provenance and
// consent, which is the entire hard part, so they share a table.
type Annotator struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is label or extract. It selects the query sugar (`label:` versus
	// `extract:`) and the prompt shape, not the storage.
	Kind   string `json:"kind"`
	Engine string `json:"engine"`
	// Version invalidates prior results. Bumping it is how a changed
	// instruction gets re-applied without deleting the old answers, which stay
	// for provenance.
	Version      int    `json:"version"`
	Instructions string `json:"instructions"`
	// SchemaJSON declares the output shape. For extractors it may carry
	// "timeField", naming the extracted field that holds the record's own
	// timestamp — see annotatorTimeField.
	SchemaJSON string `json:"schemaJson"`
	// Profile names the LLM profile this annotator runs against. Empty means
	// the default profile, which is what an annotator created before profiles
	// existed meant.
	Profile string `json:"profile,omitempty"`
	// Model overrides the profile's model on the same gateway — a bigger or
	// smaller model at the same endpoint. It cannot move work to a different
	// provider; that is what Profile is for.
	Model string `json:"model,omitempty"`
	// AllowRemote records explicit consent to send message bodies to a
	// non-local provider. Default false, and the runner refuses without it.
	AllowRemote bool      `json:"allowRemote"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Annotation is one result.
type Annotation struct {
	ID               string    `json:"id"`
	MessageID        string    `json:"messageId"`
	AnnotatorID      string    `json:"annotatorId"`
	AnnotatorVersion int       `json:"annotatorVersion"`
	Seq              int       `json:"seq"`
	Status           string    `json:"status"`
	Source           string    `json:"source"`
	DataJSON         string    `json:"dataJson"`
	Confidence       *float64  `json:"confidence,omitempty"`
	Model            string    `json:"model,omitempty"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
}

// Data decodes the annotation payload.
func (a *Annotation) Data() map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(a.DataJSON), &out)
	return out
}

// SaveAnnotator creates or updates an annotator.
//
// Changing the instructions bumps the version, because results produced by the
// old wording are no longer answers to the new question. Everything else is
// edited in place.
func SaveAnnotator(a *Annotator) error {
	now := time.Now()
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	if a.Version == 0 {
		a.Version = 1
	}
	if a.SchemaJSON == "" {
		a.SchemaJSON = "{}"
	}
	if !json.Valid([]byte(a.SchemaJSON)) {
		return fmt.Errorf("annotator %s: schema is not valid JSON", a.Name)
	}

	existing, err := GetAnnotator(a.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		a.ID, a.CreatedAt = existing.ID, existing.CreatedAt
		a.Version = existing.Version
		if strings.TrimSpace(existing.Instructions) != strings.TrimSpace(a.Instructions) {
			a.Version = existing.Version + 1
		}
	} else {
		a.CreatedAt = now
	}
	a.UpdatedAt = now

	_, err = db.Exec(`
		INSERT INTO annotators (id, name, kind, engine, version, instructions, schema_json, profile, model, allow_remote, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, kind = excluded.kind, engine = excluded.engine,
			version = excluded.version, instructions = excluded.instructions,
			schema_json = excluded.schema_json, profile = excluded.profile,
			model = excluded.model,
			allow_remote = excluded.allow_remote, updated_at = excluded.updated_at`,
		a.ID, a.Name, a.Kind, a.Engine, a.Version, a.Instructions, a.SchemaJSON,
		nullIfEmpty(a.Profile), nullIfEmpty(a.Model), boolToInt(a.AllowRemote),
		a.CreatedAt.UnixMilli(), a.UpdatedAt.UnixMilli())
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const annotatorColumns = `id, name, kind, engine, version, instructions, schema_json,
	COALESCE(profile, ''), COALESCE(model, ''), allow_remote, created_at, updated_at`

func scanAnnotator(scan func(...any) error) (*Annotator, error) {
	a := &Annotator{}
	var remote int
	var created, updated int64
	if err := scan(&a.ID, &a.Name, &a.Kind, &a.Engine, &a.Version, &a.Instructions,
		&a.SchemaJSON, &a.Profile, &a.Model, &remote, &created, &updated); err != nil {
		return nil, err
	}
	a.AllowRemote = remote != 0
	a.CreatedAt = time.UnixMilli(created)
	a.UpdatedAt = time.UnixMilli(updated)
	return a, nil
}

// GetAnnotator looks one up by name. Returns nil when absent.
func GetAnnotator(name string) (*Annotator, error) {
	a, err := scanAnnotator(db.QueryRow("SELECT "+annotatorColumns+" FROM annotators WHERE name = ?", name).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// ListAnnotators returns every annotator, newest first.
func ListAnnotators() ([]*Annotator, error) {
	rows, err := db.Query("SELECT " + annotatorColumns + " FROM annotators ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Annotator{}
	for rows.Next() {
		a, err := scanAnnotator(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAnnotator removes an annotator and every result it produced.
func DeleteAnnotator(name string) error {
	res, err := db.Exec("DELETE FROM annotators WHERE name = ?", name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no annotator named %q", name)
	}
	return nil
}

// SaveAnnotations writes one message's results, replacing any previous machine
// result for the same annotator version.
//
// Human corrections are never touched. A re-run that overwrote them would
// destroy the only record of where the annotator was wrong — which is both the
// user's work and the evaluation set for judging whether a prompt change was
// an improvement.
func SaveAnnotations(annotatorID string, version int, messageID string, anns []*Annotation) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		DELETE FROM annotations
		WHERE message_id = ? AND annotator_id = ? AND annotator_version = ? AND source != ?`,
		messageID, annotatorID, version, SourceHuman); err != nil {
		return err
	}

	for i, a := range anns {
		if a.ID == "" {
			a.ID = uuid.New().String()
		}
		a.MessageID, a.AnnotatorID, a.AnnotatorVersion = messageID, annotatorID, version
		if a.Seq == 0 {
			a.Seq = i
		}
		if a.DataJSON == "" {
			a.DataJSON = "{}"
		}
		if a.CreatedAt.IsZero() {
			a.CreatedAt = time.Now()
		}
		if _, err := tx.Exec(`
			INSERT OR REPLACE INTO annotations
				(id, message_id, annotator_id, annotator_version, seq, status, source, data_json, confidence, model, error, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.MessageID, a.AnnotatorID, a.AnnotatorVersion, a.Seq, a.Status, a.Source,
			a.DataJSON, a.Confidence, nullIfEmpty(a.Model), nullIfEmpty(a.Error),
			a.CreatedAt.UnixMilli()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PendingMessages returns messages this annotator has not evaluated at its
// current version.
//
// A message a human has already ruled on at any version is not pending: their
// answer stands, and re-asking a model about it wastes tokens to produce a
// result that will be ignored.
func PendingMessages(a *Annotator, filter string, limit int) ([]*message.Message, error) {
	where, args, err := filterClause(filter)
	if err != nil {
		return nil, err
	}

	sql := `SELECT ` + aliasedMessageColumns() + ` FROM messages m
		WHERE ` + where + `
		  AND NOT EXISTS (
			SELECT 1 FROM annotations x
			WHERE x.message_id = m.id AND x.annotator_id = ?
			  AND (x.annotator_version = ? OR x.source = ?)
		  )
		ORDER BY m.date DESC LIMIT ?`

	args = append(args, a.ID, a.Version, SourceHuman, limit)

	rows, err := db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*message.Message{}
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AnnotatorProgress reports how far an annotator has got.
type AnnotatorProgress struct {
	Name      string `json:"name"`
	Version   int    `json:"version"`
	Total     int64  `json:"total"`
	Evaluated int64  `json:"evaluated"`
	Matched   int64  `json:"matched"`
	Empty     int64  `json:"empty"`
	Failed    int64  `json:"failed"`
	Human     int64  `json:"humanCorrections"`
}

// Progress counts an annotator's coverage of the mailbox.
func Progress(a *Annotator, filter string) (*AnnotatorProgress, error) {
	where, args, err := filterClause(filter)
	if err != nil {
		return nil, err
	}

	p := &AnnotatorProgress{Name: a.Name, Version: a.Version}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages m WHERE `+where, args...).Scan(&p.Total); err != nil {
		return nil, err
	}

	countBy := func(extra string, more ...any) (int64, error) {
		var n int64
		q := `SELECT COUNT(DISTINCT m.id) FROM messages m
			JOIN annotations a ON a.message_id = m.id
			WHERE ` + where + ` AND a.annotator_id = ? AND a.annotator_version = ? ` + extra
		full := append(append([]any{}, args...), a.ID, a.Version)
		full = append(full, more...)
		err := db.QueryRow(q, full...).Scan(&n)
		return n, err
	}

	if p.Evaluated, err = countBy(""); err != nil {
		return nil, err
	}
	if p.Matched, err = countBy("AND a.status = ?", StatusOK); err != nil {
		return nil, err
	}
	if p.Empty, err = countBy("AND a.status = ?", StatusEmpty); err != nil {
		return nil, err
	}
	if p.Failed, err = countBy("AND a.status = ?", StatusFailed); err != nil {
		return nil, err
	}

	if err := db.QueryRow(`
		SELECT COUNT(DISTINCT message_id) FROM annotations
		WHERE annotator_id = ? AND source = ?`, a.ID, SourceHuman).Scan(&p.Human); err != nil {
		return nil, err
	}
	return p, nil
}

// ApplyRule evaluates a rule annotator over the whole mailbox in one pass.
//
// Rules are a query expression, so this is set arithmetic the database can do
// itself: matching messages get an "ok" row and everything else in scope gets
// an "empty" one. Per-message evaluation would be N round trips to answer a
// question one statement already answers, and rules exist partly to be fast
// enough to re-run casually.
func ApplyRule(a *Annotator, filter string) (matched, empty int64, err error) {
	scopeWhere, scopeArgs, err := filterClause(filter)
	if err != nil {
		return 0, 0, err
	}
	ruleWhere, ruleArgs, err := filterClause(a.Instructions)
	if err != nil {
		return 0, 0, fmt.Errorf("rule %q: %w", a.Instructions, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	// Machine results only: a human ruling survives a re-run.
	if _, err := tx.Exec(`
		DELETE FROM annotations
		WHERE annotator_id = ? AND annotator_version = ? AND source != ?`,
		a.ID, a.Version, SourceHuman); err != nil {
		return 0, 0, err
	}

	insert := func(status, cond string, condArgs []any) (int64, error) {
		q := `INSERT OR IGNORE INTO annotations
				(id, message_id, annotator_id, annotator_version, seq, status, source, data_json, confidence, created_at)
			SELECT lower(hex(randomblob(16))), m.id, ?, ?, 0, ?, ?, '{}', 1.0, ?
			FROM messages m
			WHERE ` + scopeWhere + ` AND ` + cond + `
			  AND NOT EXISTS (
				SELECT 1 FROM annotations h
				WHERE h.message_id = m.id AND h.annotator_id = ? AND h.source = ?
			  )`
		args := []any{a.ID, a.Version, status, SourceRule, time.Now().UnixMilli()}
		args = append(args, scopeArgs...)
		args = append(args, condArgs...)
		args = append(args, a.ID, SourceHuman)
		res, err := tx.Exec(q, args...)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}

	if matched, err = insert(StatusOK, ruleWhere, ruleArgs); err != nil {
		return 0, 0, err
	}
	if empty, err = insert(StatusEmpty, "NOT ("+ruleWhere+")", ruleArgs); err != nil {
		return 0, 0, err
	}

	return matched, empty, tx.Commit()
}

// SetHumanAnnotation records a person's ruling, which outranks every machine
// result and survives version bumps.
func SetHumanAnnotation(name, messageID string, matched bool, data map[string]any) error {
	a, err := GetAnnotator(name)
	if err != nil {
		return err
	}
	if a == nil {
		return fmt.Errorf("no annotator named %q", name)
	}

	payload := "{}"
	if len(data) > 0 {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		payload = string(b)
	}

	status := StatusEmpty
	if matched {
		status = StatusOK
	}
	conf := 1.0

	if _, err := db.Exec(`
		DELETE FROM annotations
		WHERE message_id = ? AND annotator_id = ? AND source = ?`,
		messageID, a.ID, SourceHuman); err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT OR REPLACE INTO annotations
			(id, message_id, annotator_id, annotator_version, seq, status, source, data_json, confidence, created_at)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
		uuid.New().String(), messageID, a.ID, a.Version, status, SourceHuman, payload, conf,
		time.Now().UnixMilli())
	return err
}

// ListAnnotations returns everything recorded for one message.
func ListAnnotations(messageID string) ([]*Annotation, error) {
	rows, err := db.Query(`
		SELECT a.id, a.message_id, a.annotator_id, a.annotator_version, a.seq, a.status,
		       a.source, a.data_json, a.confidence, COALESCE(a.model, ''), COALESCE(a.error, ''), a.created_at
		FROM annotations a
		JOIN annotators an ON an.id = a.annotator_id AND an.version = a.annotator_version
		WHERE a.message_id = ?
		ORDER BY an.name, a.seq`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Annotation{}
	for rows.Next() {
		a := &Annotation{}
		var created int64
		if err := rows.Scan(&a.ID, &a.MessageID, &a.AnnotatorID, &a.AnnotatorVersion, &a.Seq,
			&a.Status, &a.Source, &a.DataJSON, &a.Confidence, &a.Model, &a.Error, &created); err != nil {
			return nil, err
		}
		a.CreatedAt = time.UnixMilli(created)
		out = append(out, a)
	}
	return out, rows.Err()
}
