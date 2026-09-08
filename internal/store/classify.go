package store

import (
	"bufio"
	"bytes"
	"net/textproto"
	"strings"
)

// automationHeaders are the headers that say a message was sent by a machine.
//
// These are near-certain and free. RFC 3834 defines Auto-Submitted, RFC 2369
// defines the List-* family, and Precedence is the de facto convention every
// bulk mailer follows. Reaching for a model before reading these would be
// paying for a guess at something the message states.
var automationHeaders = []string{
	"List-Unsubscribe",
	"List-Id",
	"Auto-Submitted",
	"X-Auto-Response-Suppress",
	"X-Autoreply",
	"X-Mailer-Daemon",
}

// automationLocalParts are local parts that exist to refuse replies.
var automationLocalParts = []string{
	"no-reply", "noreply", "donotreply", "do-not-reply", "do_not_reply",
	"mailer-daemon", "postmaster", "bounce", "bounces", "notification",
	"notifications", "automated", "auto-confirm", "alerts",
}

// IsAutomatedHeader reports whether a raw header block marks the message as
// machine-generated, and which header said so.
func IsAutomatedHeader(header []byte) (bool, string) {
	if len(header) == 0 {
		return false, ""
	}
	buf := bytes.NewReader(append(bytes.TrimRight(header, "\r\n"), '\r', '\n', '\r', '\n'))
	h, err := textproto.NewReader(bufio.NewReader(buf)).ReadMIMEHeader()
	if err != nil && len(h) == 0 {
		return false, ""
	}

	for _, name := range automationHeaders {
		if h.Get(name) != "" {
			return true, name
		}
	}
	// Precedence carries ordinary values too; only the bulk ones count.
	switch strings.ToLower(strings.TrimSpace(h.Get("Precedence"))) {
	case "bulk", "list", "junk", "auto_reply":
		return true, "Precedence"
	}
	return false, ""
}

// IsAutomatedAddress reports whether an address exists to send and not receive.
func IsAutomatedAddress(addr string) bool {
	local := addr
	if at := strings.Index(addr, "@"); at > 0 {
		local = addr[:at]
	}
	local = strings.ToLower(local)
	for _, pattern := range automationLocalParts {
		if local == pattern || strings.HasPrefix(local, pattern+"+") ||
			strings.Contains(local, pattern) {
			return true
		}
	}
	return false
}

// ClassifyResult reports what a classification pass concluded.
type ClassifyResult struct {
	Examined int64 `json:"examined"`
	System   int64 `json:"system"`
	Person   int64 `json:"person"`
	Skipped  int64 `json:"skipped"`
	DryRun   bool  `json:"dryRun"`
}

// ClassifyContacts marks contacts as system or person from evidence that costs
// nothing.
//
// # The three signals, strongest first
//
//  1. Your own addresses are a person, whatever the traffic looks like.
//  2. A message from this address carried an automation header. Near-certain.
//  3. The local part is one that exists to refuse replies. Near-certain.
//  4. Mail went both ways. The clearest evidence of a person short of reading
//     anything.
//
// # The rule that is deliberately absent
//
// "Sends and is never written back to" looks like a good fourth signal and is
// a trap: it is also the exact shape of the mailbox owner in a Sent-folder
// archive. Tried against a real 188-message archive it classified the account
// holder as a system — with the accounts table carrying a different address, so
// the self-address guard above could not save him. A signal whose worst case is
// "tells you you are a robot" is not worth the contacts it would catch, and
// headers catch those anyway.
//
// Nothing here decides "organization" — that needs to read a body, which is
// the model's job. Leaving it unknown is more useful than guessing.
func ClassifyContacts(dryRun bool) (*ClassifyResult, error) {
	out := &ClassifyResult{DryRun: dryRun}

	// Your own addresses are a person, whatever the traffic looks like.
	//
	// Without this the mailbox owner is the single worst false positive
	// available: in a Sent-folder archive they send everything and receive
	// nothing, which is exactly the shape of a newsletter. Marking the account
	// holder as a system is the kind of first impression a feature does not
	// recover from.
	self := map[string]bool{}
	if mine, err := selfAddresses(); err == nil {
		for _, addr := range mine {
			self[strings.ToLower(addr)] = true
		}
	}

	rows, err := db.Query(`
		SELECT c.address, c.kind, COALESCE(c.kind_source, ''),
		       (SELECT COUNT(*) FROM message_participants p
		         WHERE p.address = c.address AND p.role = 'from'),
		       (SELECT COUNT(*) FROM message_participants p
		         WHERE p.address = c.address AND p.role != 'from'),
		       (SELECT COUNT(*) FROM message_participants p
		         JOIN messages m ON m.id = p.message_id
		         WHERE p.address = c.address AND p.role = 'from'
		           AND m.header IS NOT NULL
		           AND (m.header LIKE '%List-Unsubscribe%'
		             OR m.header LIKE '%List-Id:%'
		             OR m.header LIKE '%Auto-Submitted%'
		             OR m.header LIKE '%X-Auto-Response-Suppress%'
		             OR m.header LIKE '%Precedence: bulk%'
		             OR m.header LIKE '%Precedence: list%'))
		FROM contacts c`)
	if err != nil {
		return nil, err
	}

	type verdict struct {
		address, kind, source string
	}
	var verdicts []verdict

	for rows.Next() {
		var addr, kind, source string
		var sent, received, automated int64
		if err := rows.Scan(&addr, &kind, &source, &sent, &received, &automated); err != nil {
			rows.Close()
			return nil, err
		}
		out.Examined++

		// A ruling a person made is never revisited by a rule.
		if source == ContactFromHuman {
			out.Skipped++
			continue
		}

		// A proportion, not a presence.
		//
		// One message from this address carrying List-Unsubscribe proves
		// nothing: forwarding a newsletter carries the newsletter's headers,
		// and the forwarder is a person. Tried against a real archive, the
		// presence test classified the mailbox owner as a system on the
		// strength of a single forward out of 185.
		//
		// A system sends automated mail characteristically. A strict majority
		// is the line: a sender whose only message is automated is caught,
		// and someone who forwarded one newsletter out of two — or out of 185
		// — is not. Half exactly would let the one-in-two case through, which
		// is still a person.
		characteristic := sent > 0 && automated*2 > sent

		switch {
		case self[strings.ToLower(addr)]:
			verdicts = append(verdicts, verdict{addr, KindPerson, ContactFromRule})
		case characteristic:
			verdicts = append(verdicts, verdict{addr, KindSystem, ContactFromHeader})
		case IsAutomatedAddress(addr):
			verdicts = append(verdicts, verdict{addr, KindSystem, ContactFromRule})
		case received > 0 && sent > 0:
			// Correspondence in both directions is the clearest evidence of a
			// person short of reading anything.
			verdicts = append(verdicts, verdict{addr, KindPerson, ContactFromRule})
		default:
			out.Skipped++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, v := range verdicts {
		if v.kind == KindSystem {
			out.System++
		} else {
			out.Person++
		}
		if dryRun {
			continue
		}
		if err := SetContactKind(v.address, v.kind, v.source); err != nil {
			return nil, err
		}
	}
	return out, nil
}
