package store

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/user/inboxql/internal/message"
)

// Level is one answer a levelled label can give — "important", "low".
//
// A plain label answers yes or no. A levelled one answers which of several
// ordered levels a message is at, and Describe is the gloss the model is shown
// beside the name: "important: a person needs something from me" scores better
// than "important" alone.
type Level struct {
	Label    string `json:"label"`
	Describe string `json:"describe,omitempty"`
}

// labelSchema is what a label annotator keeps in schema_json.
//
// Kept in the schema rather than in columns because changing either is asking a
// different question, and a changed schema already bumps the version — so the
// old answers stay attributed to the question they answered.
type labelSchema struct {
	// Levels, best first. Empty for a plain yes/no label.
	Levels []Level `json:"levels,omitempty"`
	// Unit is "message" (the default) or "thread".
	Unit string `json:"unit,omitempty"`
}

func (a *Annotator) labelSchema() labelSchema {
	var s labelSchema
	if a.Kind != KindLabel || a.SchemaJSON == "" {
		return s
	}
	_ = json.Unmarshal([]byte(a.SchemaJSON), &s)
	return s
}

// Levels returns a levelled label's levels, best first, or nil for a yes/no one.
func (a *Annotator) Levels() []Level { return a.labelSchema().Levels }

// Units an annotator can be about.
const (
	UnitMessage = "message"
	UnitThread  = "thread"
)

// Unit is what the annotator judges: one message, or a whole conversation.
func (a *Annotator) Unit() string {
	if a.labelSchema().Unit == UnitThread {
		return UnitThread
	}
	return UnitMessage
}

// HasLevel reports whether level is one of this label's levels.
func (a *Annotator) HasLevel(level string) bool {
	for _, l := range a.Levels() {
		if l.Label == level {
			return true
		}
	}
	return false
}

// Verdict is one answer about a message: yes or no, and for a levelled label
// which level.
type Verdict struct {
	Matched bool     `json:"matched"`
	Level   string   `json:"level,omitempty"`
	Score   *float64 `json:"score,omitempty"`
	// Via is set on a person's ruling only: inflow or review, or empty for one
	// made before that was recorded.
	Via string `json:"via,omitempty"`
}

// MessageLabel is everything known about one label on one message: what the
// annotator said, and what the person ruled, either of which may be absent.
type MessageLabel struct {
	Annotator string   `json:"annotator"`
	Engine    string   `json:"engine"`
	Enabled   bool     `json:"enabled"`
	Unit      string   `json:"unit"`
	Levels    []Level  `json:"levels,omitempty"`
	Machine   *Verdict `json:"machine,omitempty"`
	Ruling    *Verdict `json:"ruling,omitempty"`
}

// MessageLabels returns every label annotator's verdict on a message.
//
// A label annotator appears here only when there is something to say: an
// answer from the machine or a ruling from a person. One that has never seen
// this message is absent rather than shown as "no", for the same reason
// `-label:x` excludes the unevaluated — "not looked at" is not "no".
//
// For a thread annotator the answer lives on the newest message of the
// conversation, so asking about an older message reads that one.
func MessageLabels(messageID string) ([]MessageLabel, error) {
	annotators, err := ListAnnotators()
	if err != nil {
		return nil, err
	}
	newest, err := newestInThread(messageID)
	if err != nil {
		return nil, err
	}

	var out []MessageLabel
	for _, a := range annotators {
		if a.Kind != KindLabel {
			continue
		}
		subject := messageID
		if a.Unit() == UnitThread {
			subject = newest
		}
		ml := MessageLabel{
			Annotator: a.Name, Engine: a.Engine, Enabled: a.Enabled,
			Unit: a.Unit(), Levels: a.Levels(),
		}

		rows, err := db.Query(`
			SELECT status, source, COALESCE(json_extract(data_json, '$.level'), ''),
			       confidence, COALESCE(ruled_via, '')
			FROM annotations
			WHERE message_id = ? AND annotator_id = ?
			  AND (source = ? OR annotator_version = ?)
			  AND status IN (?, ?)
			ORDER BY seq`, subject, a.ID, SourceHuman, a.Version, StatusOK, StatusEmpty)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var status, source, level, via string
			var conf *float64
			if err := rows.Scan(&status, &source, &level, &conf, &via); err != nil {
				rows.Close()
				return nil, err
			}
			v := &Verdict{Matched: status == StatusOK, Level: level}
			if source == SourceHuman {
				v.Via = via
				ml.Ruling = v
			} else if ml.Machine == nil {
				v.Score = conf
				ml.Machine = v
			}
		}
		rows.Close()
		if ml.Machine != nil || ml.Ruling != nil {
			out = append(out, ml)
		}
	}
	return out, nil
}

// RuleOnMessage records a ruling about a message, on whichever message the
// annotator's answer actually lives on.
//
// The caller names the message the person is looking at. For a thread label
// that is not necessarily where the verdict is stored — it is on the newest
// message of the conversation — and a ruling stored anywhere else would never
// be compared with anything.
func RuleOnMessage(name, messageID string, r Ruling) error {
	a, err := GetAnnotator(name)
	if err != nil {
		return err
	}
	if a == nil {
		return fmt.Errorf("no annotator named %q", name)
	}
	if r.Level != "" && !a.HasLevel(r.Level) {
		return fmt.Errorf("%q is not a level of %s", r.Level, name)
	}
	subject := messageID
	if a.Unit() == UnitThread {
		if subject, err = newestInThread(messageID); err != nil {
			return err
		}
	}
	return RecordRuling(name, subject, r)
}

// ClearRulingOnMessage is ClearRuling, resolved the way RuleOnMessage is.
func ClearRulingOnMessage(name, messageID string) error {
	a, err := GetAnnotator(name)
	if err != nil {
		return err
	}
	if a == nil {
		return fmt.Errorf("no annotator named %q", name)
	}
	subject := messageID
	if a.Unit() == UnitThread {
		if subject, err = newestInThread(messageID); err != nil {
			return err
		}
	}
	return ClearRuling(name, subject)
}

// newestInThread returns the newest message in the conversation messageID
// belongs to — messageID itself when it is the newest, or has no thread.
func newestInThread(messageID string) (string, error) {
	var id string
	err := db.QueryRow(`
		SELECT n.id FROM messages m
		JOIN messages n ON n.thread_key = m.thread_key
		WHERE m.id = ?
		ORDER BY n.date DESC, n.id DESC LIMIT 1`, messageID).Scan(&id)
	if err != nil {
		// No such message, or no thread key: the message stands for itself.
		return messageID, nil
	}
	return id, nil
}

// ReviewItem is one message put in front of a person to rule on.
type ReviewItem struct {
	MessageID string    `json:"messageId"`
	From      string    `json:"from"`
	Subject   string    `json:"subject"`
	Date      time.Time `json:"date"`
	Snippet   string    `json:"snippet"`
	// Machine is what the annotator said. The review interface does not show
	// it until the person has answered — see ReviewSample.
	Machine Verdict `json:"machine"`
}

// ReviewSample draws messages an annotator has answered and nobody has ruled
// on, for a person to rule on.
//
// # Spread across the scores, not drawn from the doubtful end
//
// The obvious queue is "the ones it was least sure of", and it measures the
// wrong thing: rulings from there say how the model does on hard cases, and a
// calibration fitted to them is wrong everywhere else. Accuracy and calibration
// need a sample of the model as it is used, so the candidates are split into n
// equal bands by score and one is drawn at random from each.
//
// # Why the machine's answer is withheld from the person
//
// It is returned so the interface can show it afterwards, but a reviewer shown
// the answer before giving their own agrees with it more often than they would
// have — which inflates exactly the number this queue exists to measure.
func ReviewSample(name string, n int) ([]ReviewItem, error) {
	a, err := GetAnnotator(name)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("no annotator named %q", name)
	}
	if n <= 0 {
		n = 10
	}
	rows, err := db.Query(`
		WITH candidates AS (
			SELECT an.message_id, an.status,
			       COALESCE(json_extract(an.data_json, '$.level'), '') AS level,
			       COALESCE(an.confidence, 0.5) AS score
			FROM annotations an
			WHERE an.annotator_id = ? AND an.annotator_version = ?
			  AND an.source != ? AND an.status IN (?, ?)
			  AND NOT EXISTS (
				SELECT 1 FROM annotations h
				WHERE h.annotator_id = an.annotator_id AND h.message_id = an.message_id
				  AND h.source = ?)
			GROUP BY an.message_id
		),
		banded AS (
			SELECT *, NTILE(?) OVER (ORDER BY score) AS band FROM candidates
		),
		drawn AS (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY band ORDER BY random()) AS pick FROM banded
		)
		SELECT d.message_id, d.status, d.level, d.score,
		       COALESCE(m.from_addr, ''), COALESCE(m.subject, ''), m.date,
		       substr(COALESCE(m.body, ''), 1, 400)
		FROM drawn d JOIN messages m ON m.id = d.message_id
		WHERE d.pick = 1
		ORDER BY random()`,
		a.ID, a.Version, SourceHuman, StatusOK, StatusEmpty, SourceHuman, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ReviewItem
	for rows.Next() {
		var it ReviewItem
		var status string
		var score float64
		var date int64
		if err := rows.Scan(&it.MessageID, &status, &it.Machine.Level, &score,
			&it.From, &it.Subject, &date, &it.Snippet); err != nil {
			return nil, err
		}
		it.Machine.Matched = status == StatusOK
		it.Machine.Score = &score
		it.Date = time.UnixMilli(date)
		out = append(out, it)
	}
	return out, rows.Err()
}

// Score is how often an annotator agrees with a person.
type Score struct {
	Annotator string `json:"annotator"`
	Engine    string `json:"engine"`
	// Reviewed counts rulings from the review queue — a random draw — and is
	// the only set accuracy is measured on.
	Reviewed int `json:"reviewed"`
	// Inflow counts rulings made while reading. Reported, never scored: they
	// are mostly corrections, so they would say the model is mostly wrong.
	Inflow int `json:"inflow"`
	// Right is how many reviewed rulings agreed with the answer as given.
	Right    int     `json:"right"`
	Accuracy float64 `json:"accuracy"`
	// Cutoff is, for a yes/no label, the score above which "yes" would have
	// agreed with the most reviewed rulings, and AtCutoff how many that is.
	// The answer as given uses the model's own midpoint, which an uncalibrated
	// model has no reason to have put in the right place.
	Cutoff   *float64 `json:"cutoff,omitempty"`
	AtCutoff int      `json:"atCutoff,omitempty"`
}

// MinScoreRulings is the fewest reviewed rulings a score is reported on with a
// straight face. Below it the number is shown, with that warning.
const MinScoreRulings = 20

// ScoreAnnotator measures an annotator against the reviewed rulings.
//
// # Why only reviewed rulings
//
// Rulings made while reading are made because something looked wrong. Scoring
// on them measures what a person noticed, not what the model does.
//
// # Why a better cutoff, and not calibration, is the lever
//
// Calibration cannot change an answer: it rescales scores without reordering
// them. A cutoff can. On the open-loop question Laya ranked a clear request
// above a "thanks!" correctly and still answered "no" to both, because 0.5 is
// the wrong place to cut an uncalibrated score. The cutoff reported here is the
// one these rulings say would have been right most often.
func ScoreAnnotator(name string) (*Score, error) {
	a, err := GetAnnotator(name)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("no annotator named %q", name)
	}
	rulings, err := HumanRulings(a.ID)
	if err != nil {
		return nil, err
	}

	s := &Score{Annotator: a.Name, Engine: a.Engine}
	var reviewed []HumanRuling
	for _, r := range rulings {
		if r.Unbiased() {
			reviewed = append(reviewed, r)
		} else {
			s.Inflow++
		}
	}
	s.Reviewed = len(reviewed)
	if s.Reviewed == 0 {
		return s, nil
	}
	for _, r := range reviewed {
		if r.Agrees() {
			s.Right++
		}
	}
	s.Accuracy = float64(s.Right) / float64(s.Reviewed)

	// A cutoff means something only for a yes/no label, where the stored score
	// is the probability of "yes".
	if len(a.Levels()) == 0 {
		best, bestRight := 0.5, -1
		for _, cand := range candidateCutoffs(reviewed) {
			right := 0
			for _, r := range reviewed {
				if (r.Confidence >= cand) == r.Ruled {
					right++
				}
			}
			if right > bestRight {
				best, bestRight = cand, right
			}
		}
		s.Cutoff, s.AtCutoff = &best, bestRight
	}
	return s, nil
}

// candidateCutoffs is every place a cutoff could change an answer: each score
// seen, plus one above them all ("never yes").
func candidateCutoffs(rs []HumanRuling) []float64 {
	seen := map[float64]bool{}
	out := []float64{1.01}
	for _, r := range rs {
		if !seen[r.Confidence] {
			seen[r.Confidence] = true
			out = append(out, r.Confidence)
		}
	}
	return out
}

// unitClause narrows an annotator's candidates to what it judges.
//
// A thread annotator is evaluated once per conversation, on the newest message
// — which is also why a reply makes the conversation pending again: the new
// message is the newest one, and it has no answer. Written as NOT EXISTS against
// the indexed thread key rather than a MAX() subquery, which SQLite would
// evaluate per row without the index.
func unitClause(a *Annotator) string {
	if a.Unit() != UnitThread {
		return ""
	}
	return ` AND NOT EXISTS (
		SELECT 1 FROM messages newer
		WHERE newer.thread_key = m.thread_key
		  AND (newer.date > m.date OR (newer.date = m.date AND newer.id > m.id)))`
}

// ThreadDigest is what a conversation looks like from above.
//
// # Why a digest and not the conversation
//
// The decision model reads about 200 tokens of anything, which is one message's
// opening paragraphs. A conversation does not fit and raising the limit makes
// every question four times slower. So the facts the database knows exactly —
// how many messages, who sent the last one, whether you have ever replied — are
// stated in a line ahead of the newest message, and the model judges only what
// the text says.
type ThreadDigest struct {
	Messages     int
	Participants int
	// LastFromMe is whether the newest message was sent by one of your
	// accounts. It is what decides which way an open loop points: a question
	// from them is waiting on you, one from you is waiting on them.
	LastFromMe bool
	// YouReplied is whether any message in the conversation is yours.
	YouReplied bool
	LastFrom   string
	Started    time.Time
}

// DigestThread summarises the conversation a message belongs to.
func DigestThread(messageID string) (*ThreadDigest, error) {
	d := &ThreadDigest{}
	var started int64
	var mine int
	err := db.QueryRow(`
		SELECT COUNT(*), MIN(t.date),
		       (SELECT COUNT(DISTINCT p.address) FROM message_participants p
		         JOIN messages tm ON tm.id = p.message_id
		         WHERE tm.thread_key = m.thread_key),
		       COALESCE(SUM(CASE WHEN `+mineClause("t")+` THEN 1 ELSE 0 END), 0)
		FROM messages m JOIN messages t ON t.thread_key = m.thread_key
		WHERE m.id = ?`, messageID).Scan(&d.Messages, &started, &d.Participants, &mine)
	if err != nil {
		return nil, err
	}
	d.Started = time.UnixMilli(started)
	d.YouReplied = mine > 0

	newest, err := newestInThread(messageID)
	if err != nil {
		return nil, err
	}
	var lastMine int
	if err := db.QueryRow(`
		SELECT CASE WHEN `+mineClause("t")+` THEN 1 ELSE 0 END, COALESCE(t.from_addr, '')
		FROM messages t WHERE t.id = ?`, newest).Scan(&lastMine, &d.LastFrom); err != nil {
		return nil, err
	}
	d.LastFromMe = lastMine == 1
	return d, nil
}

// mineClause is whether a message alias was sent by one of your accounts.
//
// The same test the Sent folder uses for its sender half, through participant
// edges rather than from_addr — which holds the raw header, so "Me
// <me@example.com>" never equalled the account address.
func mineClause(alias string) string {
	return `EXISTS (
		SELECT 1 FROM message_participants mp
		WHERE mp.message_id = ` + alias + `.id AND mp.role = 'from'
		  AND mp.address IN (
			SELECT LOWER(email) FROM accounts WHERE email IS NOT NULL AND email != ''
			UNION
			SELECT LOWER(user) FROM accounts WHERE user IS NOT NULL AND user != ''))`
}

// ThreadMessages returns the conversation a message belongs to, oldest first.
func ThreadMessages(messageID string) ([]*message.Message, error) {
	rows, err := db.Query(`SELECT `+aliasedMessageColumns()+`
		FROM messages m
		WHERE m.thread_key = (SELECT thread_key FROM messages WHERE id = ?)
		ORDER BY m.date ASC, m.id ASC`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*message.Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ScopeDependents names the annotators whose scope reads a label.
//
// # Why deleting needs to ask
//
// An extractor scoped to `label:billing` runs only on what billing found. Delete
// billing and the scope no longer compiles — and the annotator card already
// warns that a run with a broken scope covers the whole mailbox. For a span
// extractor that is fifteen to twenty seconds a message across all of it, which
// is not a consequence anybody chose by deleting a different annotator.
//
// Matched textually on `label:<name>` and `unlabeled:<name>`, with or without
// a confidence floor, as a whole word: a scope that merely mentions the name in
// some other field is not a dependent.
func ScopeDependents(name string) ([]string, error) {
	list, err := ListAnnotators()
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`(^|[\s(-])(label|unlabeled):` + regexp.QuoteMeta(name) + `(@[0-9.]+)?($|[\s)])`)
	var out []string
	for _, a := range list {
		if a.Name != name && re.MatchString(a.Scope) {
			out = append(out, a.Name)
		}
	}
	return out, nil
}

// DeleteAnnotatorChecked deletes an annotator unless something is scoped on it.
//
// With disableDependents, those annotators are switched off in the same step —
// not deleted, so everything they found is kept and they can be re-scoped and
// switched back on. Without it, the deletion is refused and the error names
// them.
func DeleteAnnotatorChecked(name string, disableDependents bool) ([]string, error) {
	deps, err := ScopeDependents(name)
	if err != nil {
		return nil, err
	}
	if len(deps) > 0 && !disableDependents {
		return nil, fmt.Errorf("%s gates %s: their scope reads it, and without it a run would cover the whole mailbox.\n"+
			"Switch them off in the same step with --disable-dependents, or re-scope them first",
			name, strings.Join(deps, ", "))
	}
	for _, d := range deps {
		if err := SetAnnotatorEnabled(d, false); err != nil {
			return nil, fmt.Errorf("switching off %s: %w", d, err)
		}
	}
	return deps, DeleteAnnotator(name)
}
