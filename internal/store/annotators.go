package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
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
//
// EngineGLiNER extracts by scoring spans of the message rather than by asking
// a model to write JSON about it. It is a third engine rather than a flag on
// the LLM one because it is not a gateway: there is no endpoint, no key, and
// nothing leaves the machine, so the profile, model-override and consent
// fields that EngineLLM turns on do not apply to it.
const (
	EngineRule   = "rule"
	EngineLLM    = "llm"
	EngineGLiNER = "gliner"
	// EngineLaya decides rather than writes: it scores the options it is given
	// and returns a distribution over them. A fourth engine beside the span one
	// for the same reason that one is beside the LLM — it reaches no gateway,
	// so profile, model override and consent do not apply — and distinct from
	// it because it labels and cannot extract, which is exactly the gap the
	// span engine leaves.
	EngineLaya = "laya"
)

// Labels are the entity types a span extractor looks for.
//
// They are the schema's own field names: an extractor already declares what it
// produces, and a separate list of labels would be the same information
// written twice, free to disagree with itself.
//
// It follows that for this engine the schema is the question, not a
// description of the answer — so [SaveAnnotator] bumps the version when it
// changes, exactly as it does for changed instructions.
//
// "timeField" is excluded: it names one of the other fields rather than being
// one, and asking a model to find a "timeField" in a receipt would be asking
// for nothing.
func (a *Annotator) Labels() []string {
	var schema map[string]any
	if err := json.Unmarshal([]byte(a.SchemaJSON), &schema); err != nil {
		return nil
	}
	out := make([]string, 0, len(schema))
	for k := range schema {
		if k == "timeField" {
			continue
		}
		out = append(out, k)
	}
	// Stable, because the label order is the class order in the scores: an
	// unstable order would silently relabel every span from one run to the
	// next.
	sort.Strings(out)
	return out
}

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
	AllowRemote bool `json:"allowRemote"`
	// Scope narrows what this annotator runs over, as a query. Carried on the
	// annotator rather than only passed at run time so that a triggered run
	// knows what to run, and a starter remembers what it wanted narrowing to.
	Scope string `json:"scope,omitempty"`
	// Trigger is when to drain the queue PendingMessages already computes:
	// manual, after-sync or daily. It is a "when", never a second "what" —
	// Scope is the only filter.
	Trigger string `json:"trigger,omitempty"`
	// Enabled is whether this runs at all. Distinct from Trigger, which is
	// when: `manual` means only when told, and Disabled means not even then.
	//
	// Off is not deleted. Everything it has already said still counts and is
	// still queryable, because those are facts about messages that really were
	// observed — the same reason a version bump keeps the old answers. What
	// stops is new work.
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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
//
// It deliberately does not write Enabled. A bool's zero value is false, so any
// caller that built an Annotator without thinking about the field would switch
// it off on save — a silent stop, with nothing in the UI to explain it. New
// rows take the column's default of on, and [SetAnnotatorEnabled] is the only
// way to change it.
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
		// A span extractor's labels are its schema's field names, so for that
		// engine the schema is the question being asked and a change to it
		// invalidates the old answers just as a reworded prompt would. For the
		// other engines the schema describes the shape of a reply the model
		// was asked for in words, and the words are what count.
		if a.Engine == EngineGLiNER &&
			!sameSchema(existing.SchemaJSON, a.SchemaJSON) {
			a.Version = existing.Version + 1
		}
	} else {
		a.CreatedAt = now
	}
	a.UpdatedAt = now

	_, err = db.Exec(`
		INSERT INTO annotators (id, name, kind, engine, version, instructions, schema_json, profile, model, allow_remote, scope, trigger, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, kind = excluded.kind, engine = excluded.engine,
			version = excluded.version, instructions = excluded.instructions,
			schema_json = excluded.schema_json, profile = excluded.profile,
			model = excluded.model, scope = excluded.scope, trigger = excluded.trigger,
			allow_remote = excluded.allow_remote, updated_at = excluded.updated_at`,
		a.ID, a.Name, a.Kind, a.Engine, a.Version, a.Instructions, a.SchemaJSON,
		nullIfEmpty(a.Profile), nullIfEmpty(a.Model), boolToInt(a.AllowRemote),
		nullIfEmpty(a.Scope), triggerOrDefault(a.Trigger),
		a.CreatedAt.UnixMilli(), a.UpdatedAt.UnixMilli())
	return err
}

// sameSchema compares two schemas by content rather than by text.
//
// Reformatting a schema file, or writing the same fields in a different order,
// is not a change to the question — and treating it as one would invalidate a
// whole mailbox's results for a reindented JSON file.
func sameSchema(a, b string) bool {
	var av, bv map[string]any
	if json.Unmarshal([]byte(a), &av) != nil || json.Unmarshal([]byte(b), &bv) != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	if len(av) != len(bv) {
		return false
	}
	for k, x := range av {
		y, ok := bv[k]
		if !ok || fmt.Sprint(x) != fmt.Sprint(y) {
			return false
		}
	}
	return true
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const annotatorColumns = `id, name, kind, engine, version, instructions, schema_json,
	COALESCE(profile, ''), COALESCE(model, ''), allow_remote,
	COALESCE(scope, ''), COALESCE(trigger, 'manual'), COALESCE(enabled, 1),
	created_at, updated_at`

func scanAnnotator(scan func(...any) error) (*Annotator, error) {
	a := &Annotator{}
	var remote, enabled int
	var created, updated int64
	if err := scan(&a.ID, &a.Name, &a.Kind, &a.Engine, &a.Version, &a.Instructions,
		&a.SchemaJSON, &a.Profile, &a.Model, &remote, &a.Scope, &a.Trigger,
		&enabled, &created, &updated); err != nil {
		return nil, err
	}
	a.AllowRemote = remote != 0
	a.Enabled = enabled != 0
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

	// A human ruling is the answer, so the machine does not get to write over
	// it — and checking that here rather than only in the DELETE below is the
	// whole point.
	//
	// The DELETE spares `source != 'human'`, which reads as though a
	// correction survives. It did not: the rows below go in with INSERT OR
	// REPLACE against UNIQUE(message_id, annotator_id, annotator_version,
	// seq), so a machine result at seq 0 silently replaced a human one at
	// seq 0 immediately after being careful not to delete it. PendingMessages
	// hides this most of the time by not offering a ruled-on message to a run
	// at all, which is why it went unnoticed — but any path that re-saves
	// directly, a whole-mailbox rule pass among them, hit it.
	var ruled int
	if err := tx.QueryRow(`
		SELECT COUNT(*) FROM annotations
		WHERE message_id = ? AND annotator_id = ? AND annotator_version = ? AND source = ?`,
		messageID, annotatorID, version, SourceHuman).Scan(&ruled); err != nil {
		return err
	}
	if ruled > 0 {
		return nil
	}

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

	// One pass rather than four.
	//
	// This used to run a COUNT(DISTINCT) per status — evaluated, ok, empty,
	// failed — each a separate scan of the same join. With the total above
	// that is five queries per annotator, and the listing calls this for every
	// one: sixty round trips over the whole mailbox to draw a page with twelve
	// progress bars.
	//
	// Conditional aggregation gets all four from the join the first one had to
	// do anyway. The DISTINCT is kept per status because a message with three
	// records would otherwise count three times towards a number that claims
	// to count messages.
	full := append(append([]any{}, args...), a.ID, a.Version)
	if err := db.QueryRow(`
		SELECT COUNT(DISTINCT a.message_id),
		       COUNT(DISTINCT CASE WHEN a.status = 'ok'     THEN a.message_id END),
		       COUNT(DISTINCT CASE WHEN a.status = 'empty'  THEN a.message_id END),
		       COUNT(DISTINCT CASE WHEN a.status = 'failed' THEN a.message_id END)
		FROM messages m
		JOIN annotations a ON a.message_id = m.id
		WHERE `+where+` AND a.annotator_id = ? AND a.annotator_version = ?`,
		full...).Scan(&p.Evaluated, &p.Matched, &p.Empty, &p.Failed); err != nil {
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

// Annotator triggers: when to drain the queue, never what to run over.
//
// Scope is the only filter. A trigger that also filtered would be a second
// system for the same job, free to disagree with the first.
const (
	// TriggerManual is the default and today's behaviour: it runs when told.
	TriggerManual = "manual"
	// TriggerAfterSync drains what is pending once new mail has landed.
	TriggerAfterSync = "after-sync"
	// TriggerDaily drains on a timer.
	TriggerDaily = "daily"
)

// Triggers lists every kind, for validating a request.
var Triggers = []string{TriggerManual, TriggerAfterSync, TriggerDaily}

// ValidTrigger reports whether a trigger is one this knows how to honour.
func ValidTrigger(t string) bool {
	for _, k := range Triggers {
		if k == t {
			return true
		}
	}
	return false
}

// triggerOrDefault keeps the column non-empty.
//
// An annotator created before triggers existed means manual, which is also
// what an empty value means, so they are the same thing written twice.
func triggerOrDefault(t string) string {
	if !ValidTrigger(t) {
		return TriggerManual
	}
	return t
}

// SetAnnotatorEnabled switches an annotator on or off.
//
// Off is not deleted, and that is the whole point. Deleting cascades to every
// annotation it ever wrote — on a worked mailbox that is hundreds of extracted
// values and, worse, the human corrections somebody made by hand, which no
// amount of re-running recovers. Switching off costs one UPDATE and switching
// back on costs another.
//
// What stops is new work: triggers skip it, the viewer stops offering it, and
// running it by name refuses. What it has already said still counts, because
// those are facts about messages that really were observed.
func SetAnnotatorEnabled(name string, on bool) error {
	res, err := db.Exec(
		"UPDATE annotators SET enabled = ?, updated_at = ? WHERE name = ?",
		boolToInt(on), time.Now().UnixMilli(), name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no annotator named %q", name)
	}
	return nil
}

// AnnotationVolume is how much an annotator is holding.
//
// # Why this is not Progress
//
// [Progress] answers coverage — how far through the mailbox this annotator has
// got — and so counts DISTINCT messages. That is the right denominator for a
// progress bar and the wrong numerator for "what would be lost", because
// deleting an annotator deletes *rows*, and one message can hold many: a
// receipt with an amount, a date and three reference numbers is five.
//
// On this mailbox the two differ by an order of magnitude. `receipts` covers
// 32 messages and holds 269 records, 19 of them set by hand. Telling somebody
// that switching it off keeps "31 results and 1 correction" understates the
// second number nineteen-fold — and that number is the entire reason the
// switch exists rather than a delete.
type AnnotationVolume struct {
	// Records is every row, at every version. Earlier versions are counted
	// because they are what a delete would take too.
	Records int64 `json:"records"`
	// Human is rows a person set. Not recoverable by re-running at any price,
	// which is what makes it worth naming separately.
	Human int64 `json:"human"`
	// Messages is how many messages those rows are spread over.
	Messages int64 `json:"messages"`
}

// AnnotationVolumeOf counts what one annotator has written.
func AnnotationVolumeOf(annotatorID string) (*AnnotationVolume, error) {
	v := &AnnotationVolume{}
	err := db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN source = ? THEN 1 ELSE 0 END), 0),
		       COUNT(DISTINCT message_id)
		FROM annotations WHERE annotator_id = ?`,
		SourceHuman, annotatorID).Scan(&v.Records, &v.Human, &v.Messages)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// HumanRuling pairs a person's verdict with what the machine had said.
//
// The two are stored as separate rows — a human ruling outranks the machine
// result rather than overwriting it, which is what lets a re-run keep both —
// so comparing them is a join rather than a column read.
type HumanRuling struct {
	MessageID string
	// Said is what the machine answered: true when it produced a record.
	Said bool
	// Confidence is the machine's own score, which is the quantity a
	// calibration fit is about.
	Confidence float64
	// Ruled is what the person said.
	Ruled bool
}

// HumanRulings returns every message a person has ruled on for an annotator,
// paired with what the machine had said about it.
//
// Rows where the machine never answered are skipped: a ruling on a message the
// annotator has not evaluated is a judgement with nothing to compare against,
// and including it would make a calibration fit measure the wrong thing.
func HumanRulings(annotatorID string) ([]HumanRuling, error) {
	rows, err := db.Query(`
		SELECT h.message_id,
		       CASE WHEN mach.status = ? THEN 1 ELSE 0 END,
		       COALESCE(mach.confidence, 0),
		       CASE WHEN h.status = ? THEN 1 ELSE 0 END
		FROM annotations h
		JOIN annotations mach
		  ON mach.message_id = h.message_id
		 AND mach.annotator_id = h.annotator_id
		 AND mach.source != ?
		WHERE h.annotator_id = ? AND h.source = ?
		GROUP BY h.message_id`,
		StatusOK, StatusOK, SourceHuman, annotatorID, SourceHuman)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HumanRuling
	for rows.Next() {
		var r HumanRuling
		var said, ruled int
		if err := rows.Scan(&r.MessageID, &said, &r.Confidence, &ruled); err != nil {
			return nil, err
		}
		r.Said, r.Ruled = said == 1, ruled == 1
		out = append(out, r)
	}
	return out, rows.Err()
}
