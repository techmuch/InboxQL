package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// # Correcting one extracted value
//
// [SetHumanAnnotation] is label-shaped: it deletes every human row for an
// annotator and writes exactly one at seq 0, which can say "this message does
// not match" and nothing finer. An extractor writes one row per value, so
// ruling on a message that yielded twelve of them needs a way to say which.
//
// The storage was already ready for this. A row with source 'human' outranks
// the machine, survives a re-run — SaveAnnotations deletes `WHERE source !=
// 'human'` — and takes the message out of the pending queue. Only the writer
// was missing.
//
// # Why a correction rewrites the whole set
//
// Not a diff. A human ruling on a message is a statement about *all* of its
// values: these are the ones that are right. Applying edits one at a time
// would leave the machine's remaining rows and the human's sitting side by
// side, and a reader could not tell which set was being asserted.
//
// So a correction replaces the human rows wholesale, and the machine rows for
// that message are dropped with them: once a person has ruled, their answer is
// the answer. Re-running the annotator will not overwrite it, and clearing the
// correction brings the machine back on the next run.

// SpanCorrection is one value a person is asserting.
type SpanCorrection struct {
	Label string `json:"label"`
	Field string `json:"field"` // "subject", "body", or "attachment:<hash>"
	Start int    `json:"start"` // byte offset, inclusive
	End   int    `json:"end"`   // byte offset, exclusive
	Text  string `json:"text"`
}

// SetHumanSpans records a person's ruling on what a message contains.
//
// An empty set is meaningful and is not the same as not calling this: it says
// "the machine found things here and none of them are right", which is the
// correction a false positive needs.
func SetHumanSpans(annotatorName, messageID string, spans []SpanCorrection) error {
	a, err := GetAnnotator(annotatorName)
	if err != nil {
		return err
	}
	if a == nil {
		return fmt.Errorf("no annotator named %q", annotatorName)
	}

	for i, s := range spans {
		if s.Label == "" {
			return fmt.Errorf("correction %d has no label", i)
		}
		if !validSpanField(s.Field) {
			return fmt.Errorf(
				"correction %d: field must be subject, body or attachment:<hash>, not %q",
				i, s.Field)
		}
		if s.End <= s.Start || s.Start < 0 {
			return fmt.Errorf("correction %d: %d:%d is not a span", i, s.Start, s.End)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Everything this annotator has said about this message goes, machine
	// rows included. See the note above: a ruling is about the whole set.
	if _, err := tx.Exec(
		"DELETE FROM annotations WHERE message_id = ? AND annotator_id = ?",
		messageID, a.ID); err != nil {
		return err
	}

	// A person's ruling is certain by definition. The confidence column holds
	// how sure the *model* was, and there is no model here.
	conf := 1.0
	status := StatusEmpty
	if len(spans) > 0 {
		status = StatusOK
	}

	if len(spans) == 0 {
		// One row saying "looked, nothing here" — the same three-valued
		// distinction the machine records, so -extract:x stays honest.
		_, err := tx.Exec(`
			INSERT INTO annotations
				(id, message_id, annotator_id, annotator_version, seq, status, source,
				 data_json, confidence, created_at)
			VALUES (?, ?, ?, ?, 0, ?, ?, '{}', ?, ?)`,
			uuid.New().String(), messageID, a.ID, a.Version, status, SourceHuman,
			conf, time.Now().UnixMilli())
		if err != nil {
			return err
		}
		return tx.Commit()
	}

	for i, s := range spans {
		payload, err := json.Marshal(map[string]any{
			s.Label: s.Text,
			"field": s.Field,
			"start": s.Start,
			"end":   s.End,
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO annotations
				(id, message_id, annotator_id, annotator_version, seq, status, source,
				 data_json, confidence, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.New().String(), messageID, a.ID, a.Version, i, StatusOK, SourceHuman,
			string(payload), conf, time.Now().UnixMilli()); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ClearHumanSpans removes a person's ruling, letting the machine answer again
// on the next run.
//
// Distinct from correcting to an empty set, which asserts that there is
// nothing here. This says "forget that I ruled at all".
func ClearHumanSpans(annotatorName, messageID string) error {
	a, err := GetAnnotator(annotatorName)
	if err != nil {
		return err
	}
	if a == nil {
		return fmt.Errorf("no annotator named %q", annotatorName)
	}
	_, err = db.Exec(
		"DELETE FROM annotations WHERE message_id = ? AND annotator_id = ? AND source = ?",
		messageID, a.ID, SourceHuman)
	return err
}

// validSpanField reports whether a correction names a part of a message that
// can hold a span.
//
// The subject, the body, or one readable attachment by content hash. A bare
// index would not survive the file arriving again on another message, or the
// message being re-annotated in a different order, which is why the hash is
// the identity.
func validSpanField(field string) bool {
	switch field {
	case "subject", "body":
		return true
	}
	hash, ok := strings.CutPrefix(field, "attachment:")
	return ok && hash != ""
}
