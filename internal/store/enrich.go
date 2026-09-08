package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Where a topic came from.
const (
	TopicFromSubject = "subject"
	TopicFromModel   = "llm"
	TopicFromHuman   = "human"
)

// EnrichResult reports what an enrichment pass did.
type EnrichResult struct {
	Annotator string `json:"annotator"`
	Read      int64  `json:"read"`
	Contacts  int64  `json:"contacts"`
	Topics    int64  `json:"topics"`
	DryRun    bool   `json:"dryRun"`
}

// EnrichContacts folds an extractor's output into contacts and topics.
//
// # Why this is a mapping and not a second pipeline
//
// Everything hard about asking a model something — versioned instructions, a
// model profile, local-or-remote scope, per-annotator consent, incremental
// re-run, confidence, provenance — already exists in the annotator machinery.
// This does the one thing that machinery deliberately does not: decide what an
// extraction *means*. It is the same shape as ProposeTickets.
//
// # Field names
//
// Several spellings of each are accepted because an extractor is defined by a
// user writing a prompt, and the key a model returns depends on how the
// instruction was phrased. Insisting on an exact schema would make the feature
// fail for the reason least worth failing for.
func EnrichContacts(annotatorName string, dryRun bool) (*EnrichResult, error) {
	a, err := GetAnnotator(annotatorName)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("no annotator named %q", annotatorName)
	}
	if a.Kind != KindExtract {
		return nil, fmt.Errorf(
			"%q is a %s; enriching contacts needs fields to be extracted, so use --kind extract",
			a.Name, a.Kind)
	}

	rows, err := db.Query(`
		SELECT an.message_id, an.data_json, an.confidence,
		       (SELECT p.address FROM message_participants p
		         WHERE p.message_id = an.message_id AND p.role = 'from' LIMIT 1)
		FROM annotations an
		WHERE an.annotator_id = ? AND an.annotator_version = ? AND an.status = ?`,
		a.ID, a.Version, StatusOK)
	if err != nil {
		return nil, err
	}

	type record struct {
		messageID, data, sender string
		confidence              *float64
	}
	var records []record
	for rows.Next() {
		var r record
		var sender *string
		if err := rows.Scan(&r.messageID, &r.data, &r.confidence, &sender); err != nil {
			rows.Close()
			return nil, err
		}
		if sender != nil {
			r.sender = *sender
		}
		records = append(records, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := &EnrichResult{Annotator: a.Name, DryRun: dryRun}

	for _, r := range records {
		out.Read++

		// Topics are about the message, so they land whether or not the
		// extraction said anything about who sent it.
		topics := topicList(r.data)
		if len(topics) > 0 {
			out.Topics += int64(len(topics))
			if !dryRun {
				if err := SetMessageTopics(r.messageID, topics, TopicFromModel, r.confidence); err != nil {
					return nil, err
				}
			}
		}

		// Contact fields are about the sender. An extraction that named an
		// address explicitly wins over the envelope, because a signature block
		// may be someone else's.
		address := ticketField(r.data, "email", "address", "contactEmail", "contact_email")
		if address == "" {
			address = r.sender
		}
		if address == "" {
			continue
		}

		c := &Contact{
			Address:    address,
			FirstName:  ticketField(r.data, "firstName", "first_name", "first", "givenName"),
			LastName:   ticketField(r.data, "lastName", "last_name", "last", "surname", "familyName"),
			Phone:      ticketField(r.data, "phone", "phoneNumber", "phone_number", "tel", "mobile"),
			Org:        ticketField(r.data, "org", "organisation", "organization", "company", "employer"),
			Title:      ticketField(r.data, "title", "jobTitle", "job_title", "role"),
			Kind:       normaliseKind(ticketField(r.data, "kind", "type", "contactType")),
			EnrichedBy: a.Name,
			Confidence: r.confidence,
		}
		if c.FirstName == "" && c.LastName == "" && c.Phone == "" && c.Org == "" &&
			c.Title == "" && c.Kind == "" {
			continue
		}

		out.Contacts++
		if dryRun {
			continue
		}
		if err := SaveContact(c, ContactFromModel); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// normaliseKind maps whatever a model called it onto the three kinds.
func normaliseKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "person", "human", "individual":
		return KindPerson
	case "organization", "organisation", "company", "business":
		return KindOrganization
	case "system", "automated", "bot", "noreply", "no-reply", "machine":
		return KindSystem
	}
	return ""
}

// topicList pulls topics out of extracted JSON.
//
// Accepts a list or a single string, because both are what models return when
// asked for "topics" and rejecting one of them would be rejecting the answer
// over its punctuation.
func topicList(data string) []string {
	var fields map[string]any
	if err := json.Unmarshal([]byte(data), &fields); err != nil {
		return nil
	}

	var raw any
	for _, key := range []string{"topics", "topic", "tags", "subjects", "categories"} {
		if v, ok := fields[key]; ok {
			raw = v
			break
		}
	}

	var out []string
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && len(s) <= 64 {
			out = append(out, s)
		}
	}
	switch v := raw.(type) {
	case string:
		for _, part := range strings.Split(v, ",") {
			add(part)
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				add(s)
			}
		}
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// SetMessageTopics replaces the topics recorded for a message by one source.
//
// Scoped to the source so a model re-run cannot delete a topic a person added,
// and a person's correction cannot be undone by the next run.
func SetMessageTopics(messageID string, topics []string, source string, confidence *float64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM message_topics WHERE message_id = ? AND source = ?",
		messageID, source); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, t := range topics {
		if _, err := tx.Exec(`
			INSERT INTO message_topics (message_id, topic, source, confidence, created_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(message_id, topic) DO UPDATE SET
				source = excluded.source, confidence = excluded.confidence`,
			messageID, t, source, confidence, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ContactTopic is one topic a contact is associated with.
type ContactTopic struct {
	Topic string `json:"topic"`
	// Messages is how many of their messages carry it.
	Messages int64 `json:"messages"`
	// Share is that as a fraction of everything they appear on.
	Share float64 `json:"share"`
	// Lift is how much more this contact discusses it than the mailbox does.
	//
	// The number that makes the feature work. Ranked by raw count, whoever you
	// exchange the most mail with is the top contact for every topic — on a
	// real archive one address appeared on 185 of 188 messages, so counting
	// would have answered "david" to every question. Lift asks a different
	// question: who talks about this *disproportionately*.
	Lift float64 `json:"lift"`
}

// TopicsFor returns what a contact is associated with, most distinctive first.
func TopicsFor(address string, limit int) ([]ContactTopic, error) {
	addr, _ := NormaliseAddress(address)
	if addr == "" {
		addr = strings.ToLower(strings.TrimSpace(address))
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := db.Query(`
		WITH theirs AS (
			SELECT DISTINCT p.message_id FROM message_participants p WHERE p.address = ?
		),
		corpus AS (SELECT COUNT(*) AS n FROM messages),
		mine AS (SELECT COUNT(*) AS n FROM theirs)
		SELECT t.topic,
		       COUNT(DISTINCT t.message_id) AS mentions,
		       (SELECT n FROM mine) AS theirs_total,
		       (SELECT COUNT(DISTINCT t2.message_id) FROM message_topics t2 WHERE t2.topic = t.topic) AS corpus_mentions,
		       (SELECT n FROM corpus) AS corpus_total
		FROM message_topics t
		JOIN theirs ON theirs.message_id = t.message_id
		GROUP BY t.topic
		ORDER BY mentions DESC
		LIMIT ?`, addr, limit*4)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ContactTopic{}
	for rows.Next() {
		var topic string
		var mentions, theirsTotal, corpusMentions, corpusTotal int64
		if err := rows.Scan(&topic, &mentions, &theirsTotal, &corpusMentions, &corpusTotal); err != nil {
			return nil, err
		}
		ct := ContactTopic{Topic: topic, Messages: mentions}
		if theirsTotal > 0 {
			ct.Share = float64(mentions) / float64(theirsTotal)
		}
		// Lift is their share over the corpus share. Above 1 means this topic
		// is more theirs than it is everyone's.
		if corpusMentions > 0 && corpusTotal > 0 && ct.Share > 0 {
			corpusShare := float64(corpusMentions) / float64(corpusTotal)
			if corpusShare > 0 {
				ct.Lift = ct.Share / corpusShare
			}
		}
		out = append(out, ct)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Most distinctive first, and only then most frequent — the whole point is
	// not to answer every question with whoever you email most.
	sortContactTopics(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func sortContactTopics(in []ContactTopic) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0; j-- {
			a, b := in[j-1], in[j]
			if b.Lift > a.Lift || (b.Lift == a.Lift && b.Messages > a.Messages) {
				in[j-1], in[j] = in[j], in[j-1]
				continue
			}
			break
		}
	}
}

// ContactsForTopic answers "who do I talk to about this".
//
// Ranked by lift rather than volume, for the same reason TopicsFor is.
func ContactsForTopic(topic string, limit int) ([]*Contact, error) {
	if limit <= 0 {
		limit = 10
	}
	topic = strings.ToLower(strings.TrimSpace(topic))

	rows, err := db.Query(`
		SELECT `+ContactSelectList+`,
		       (SELECT COUNT(DISTINCT t.message_id) FROM message_topics t
		         JOIN message_participants p ON p.message_id = t.message_id
		         WHERE p.address = c.address AND t.topic = ?) AS hits
		FROM contacts c
		WHERE hits > 0
		ORDER BY CAST(hits AS REAL) / MAX(messages, 1) DESC, hits DESC
		LIMIT ?`, topic, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Contact{}
	for rows.Next() {
		var hits int64
		c, err := scanContact(func(dest ...any) error {
			return rows.Scan(append(dest, &hits)...)
		})
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
