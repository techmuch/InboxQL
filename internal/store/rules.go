package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/query"
)

// Automation, kept small: a rule is a trigger and some actions.
//
// Two triggers, because they are the two things that happen to a personal
// board. A card enters a list — moving it to Done should tick its checklist —
// and mail arrives that is really a task — every invoice from the landlord is
// "pay rent". Nothing here moves a card between lists, so a rule can never
// set off another rule, or itself, in a loop.

// Rule triggers.
const (
	// RuleEnterList fires when a card enters the rule's list: moved there,
	// or made there.
	RuleEnterList = "enter-list"
	// RuleMail fires on mail matching the rule's query, making a card in the
	// rule's list — one per conversation.
	RuleMail = "mail"
)

// ActorRule is who a rule's changes are recorded as.
const ActorRule = "rule"

// OriginRule is the origin of a card a mail rule made.
const OriginRule = "rule"

// Rule actions.
const (
	ActionCheckAll    = "check-all"
	ActionAddLabel    = "add-label"
	ActionRemoveLabel = "remove-label"
	ActionSetDue      = "set-due"
	ActionClearDue    = "clear-due"
	ActionSetPriority = "set-priority"
	ActionArchive     = "archive"
)

// RuleActions are the actions a rule may take, in the order they are listed
// to people.
var RuleActions = []string{ActionCheckAll, ActionAddLabel, ActionRemoveLabel,
	ActionSetDue, ActionClearDue, ActionSetPriority, ActionArchive}

// RuleAction is one thing a rule does.
type RuleAction struct {
	Do    string `json:"do"`
	Value string `json:"value,omitempty"`
}

// TicketRule is one automation rule.
type TicketRule struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Trigger string       `json:"trigger"`
	List    string       `json:"list"`
	Query   string       `json:"query,omitempty"`
	Actions []RuleAction `json:"actions"`
	Enabled bool         `json:"enabled"`
	// Since is the oldest mail a mail rule acts on: when it was made, so a
	// new rule does not turn ten years of mail into cards.
	Since     time.Time  `json:"since"`
	CreatedAt time.Time  `json:"createdAt"`
	LastRunAt *time.Time `json:"lastRunAt,omitempty"`
}

const ruleColumns = `id, title, trigger, list, query, actions, enabled, since, created_at, last_run_at`

func scanRule(scan func(...any) error) (*TicketRule, error) {
	r := &TicketRule{}
	var actions string
	var since, created int64
	var last sql.NullInt64
	if err := scan(&r.ID, &r.Title, &r.Trigger, &r.List, &r.Query, &actions, &r.Enabled,
		&since, &created, &last); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(actions), &r.Actions); err != nil || r.Actions == nil {
		r.Actions = []RuleAction{}
	}
	r.Since, r.CreatedAt = time.UnixMilli(since), time.UnixMilli(created)
	r.LastRunAt = optionalTime(last)
	return r, nil
}

// TicketRules returns every rule, oldest first.
func TicketRules() ([]*TicketRule, error) {
	rows, err := db.Query("SELECT " + ruleColumns + " FROM ticket_rules ORDER BY created_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TicketRule{}
	for rows.Next() {
		r, err := scanRule(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetTicketRule finds a rule by id, id prefix or title.
func GetTicketRule(ref string) (*TicketRule, error) {
	rules, err := TicketRules()
	if err != nil {
		return nil, err
	}
	var hits []*TicketRule
	for _, r := range rules {
		if r.ID == ref {
			return r, nil
		}
		if strings.HasPrefix(r.ID, ref) || strings.EqualFold(r.Title, ref) {
			hits = append(hits, r)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return nil, fmt.Errorf("no rule %q", ref)
	}
	return nil, fmt.Errorf("%q matches more than one rule", ref)
}

// check validates a rule before it is stored, so a broken one is refused
// where it is written rather than failing quietly after every sync.
func (r *TicketRule) check() error {
	r.Title = strings.TrimSpace(r.Title)
	r.Query = strings.TrimSpace(r.Query)
	switch r.Trigger {
	case RuleEnterList:
		if r.List == "" {
			return fmt.Errorf("which list? a rule fires when a card enters one")
		}
	case RuleMail:
		if r.Query == "" {
			return fmt.Errorf("which mail? a mail rule needs a query")
		}
		q, err := query.Parse(r.Query)
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}
		if q.Entity() != query.EntityMessage {
			return fmt.Errorf("query: a mail rule's query is about mail, not %s", q.Entity())
		}
		if _, _, err := filterClause(r.Query); err != nil {
			return fmt.Errorf("query: %w", err)
		}
		if r.List == "" {
			r.List = ProposedList()
		}
	default:
		return fmt.Errorf("a rule fires on %s or %s, not %q", RuleEnterList, RuleMail, r.Trigger)
	}
	l, err := GetTicketList(r.List)
	if err != nil {
		return err
	}
	r.List = l.ID
	if len(r.Actions) == 0 && r.Trigger == RuleEnterList {
		return fmt.Errorf("a rule needs something to do (%s)", strings.Join(RuleActions, ", "))
	}
	for i := range r.Actions {
		a := &r.Actions[i]
		a.Do = strings.ToLower(strings.TrimSpace(a.Do))
		a.Value = strings.TrimSpace(a.Value)
		switch a.Do {
		case ActionCheckAll, ActionClearDue, ActionArchive:
			a.Value = ""
		case ActionAddLabel, ActionRemoveLabel:
			if a.Value = NormalizeCardLabel(a.Value); a.Value == "" {
				return fmt.Errorf("%s needs a label", a.Do)
			}
		case ActionSetDue:
			if _, err := ParseTicketDue(a.Value); err != nil {
				return fmt.Errorf("set-due %q: %w (try 3d, 2w, or a date)", a.Value, err)
			}
		case ActionSetPriority:
			if a.Value == "" {
				return fmt.Errorf("set-priority needs a priority")
			}
		default:
			return fmt.Errorf("%q is not something a rule can do (%s)", a.Do, strings.Join(RuleActions, ", "))
		}
	}
	if r.Title == "" {
		r.Title = r.describe()
	}
	return nil
}

// describe is a rule's default title, said the way a person would.
func (r *TicketRule) describe() string {
	var does []string
	for _, a := range r.Actions {
		if a.Value != "" {
			does = append(does, a.Do+" "+a.Value)
		} else {
			does = append(does, a.Do)
		}
	}
	if r.Trigger == RuleMail {
		s := "Mail matching " + r.Query + " → a card in " + r.List
		if len(does) > 0 {
			s += ", " + strings.Join(does, ", ")
		}
		return s
	}
	return "When a card enters " + r.List + ": " + strings.Join(does, ", ")
}

// CreateTicketRule stores a new rule, enabled.
func CreateTicketRule(r *TicketRule) error {
	if err := r.check(); err != nil {
		return err
	}
	now := time.Now()
	r.ID = uuid.New().String()
	r.Enabled = true
	r.CreatedAt = now
	if r.Since.IsZero() {
		r.Since = now
	}
	actions, _ := json.Marshal(r.Actions)
	_, err := db.Exec(`INSERT INTO ticket_rules (id, title, trigger, list, query, actions, enabled,
		since, created_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		r.ID, r.Title, r.Trigger, r.List, r.Query, string(actions), r.Since.UnixMilli(), now.UnixMilli())
	return err
}

// UpdateTicketRule rewrites a rule's title, list, query, actions and whether
// it is on.
func UpdateTicketRule(r *TicketRule) error {
	if err := r.check(); err != nil {
		return err
	}
	actions, _ := json.Marshal(r.Actions)
	return execOne(`UPDATE ticket_rules SET title = ?, trigger = ?, list = ?, query = ?, actions = ?,
		enabled = ? WHERE id = ?`, "rule", r.ID,
		r.Title, r.Trigger, r.List, r.Query, string(actions), r.Enabled, r.ID)
}

// DeleteTicketRule removes a rule. Cards it made stay.
func DeleteTicketRule(id string) error {
	return execOne("DELETE FROM ticket_rules WHERE id = ?", "rule", id, id)
}

// runEntryRules applies every enabled rule for a list to a card that has
// just entered it.
func runEntryRules(ticketID, list string) error {
	rows, err := db.Query("SELECT "+ruleColumns+` FROM ticket_rules
		WHERE trigger = ? AND list = ? AND enabled = 1 ORDER BY created_at`, RuleEnterList, list)
	if err != nil {
		return err
	}
	var rules []*TicketRule
	for rows.Next() {
		r, err := scanRule(rows.Scan)
		if err != nil {
			rows.Close()
			return err
		}
		rules = append(rules, r)
	}
	rows.Close()
	for _, r := range rules {
		if err := applyRuleActions(ticketID, r.Actions); err != nil {
			return fmt.Errorf("rule %q: %w", r.Title, err)
		}
	}
	return nil
}

// applyRuleActions does a rule's actions to one card, as the rule.
func applyRuleActions(ticketID string, actions []RuleAction) error {
	for _, a := range actions {
		var err error
		switch a.Do {
		case ActionCheckAll:
			err = checkAll(ticketID, ActorRule)
		case ActionAddLabel:
			err = addTicketLabels(ticketID, []string{a.Value}, ActorRule)
		case ActionRemoveLabel:
			err = removeTicketLabels(ticketID, []string{a.Value}, ActorRule)
		case ActionArchive:
			err = archiveTicket(ticketID, true, ActorRule)
		case ActionSetDue, ActionClearDue, ActionSetPriority:
			var t *Ticket
			if t, err = loadTicket(ticketID); err != nil || t == nil {
				break
			}
			switch a.Do {
			case ActionSetDue:
				t.DueAt, err = ParseTicketDue(a.Value)
			case ActionClearDue:
				t.DueAt = nil
			case ActionSetPriority:
				t.Priority = a.Value
			}
			if err == nil {
				err = saveTicket(t, ActorRule)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// RuleRun is what running the mail rules did.
type RuleRun struct {
	Rules    int `json:"rules"`
	Matched  int `json:"matched"`
	Created  int `json:"created"`
	Attached int `json:"attached"`
	// Errors names rules that could not run; the others still did.
	Errors []string `json:"errors,omitempty"`
}

// mailRuleBatch caps one rule's pass, so a broad query on a large mailbox
// cannot turn one sync into minutes of card-making. What is left is picked
// up next time, because the card for a conversation is found, not remade.
const mailRuleBatch = 500

// RunMailRules runs the enabled mail rules (or the one named), making a card
// for each matching conversation that has none from that rule yet and adding
// newer mail to the ones that do. backfill also covers mail from before the
// rule was made.
func RunMailRules(only string, backfill bool) (*RuleRun, error) {
	rules, err := TicketRules()
	if err != nil {
		return nil, err
	}
	out := &RuleRun{}
	for _, r := range rules {
		if r.Trigger != RuleMail {
			continue
		}
		if only != "" {
			if r.ID != only {
				continue
			}
		} else if !r.Enabled {
			continue
		}
		out.Rules++
		if err := runMailRule(r, backfill, out); err != nil {
			out.Errors = append(out.Errors, r.Title+": "+err.Error())
			log.Printf("[board] rule %q: %v", r.Title, err)
		}
	}
	return out, nil
}

func runMailRule(r *TicketRule, backfill bool, out *RuleRun) error {
	where, args, err := filterClause(r.Query)
	if err != nil {
		return err
	}
	since := r.Since.UnixMilli()
	if backfill {
		since = 0
	}
	// Mail not already behind one of this rule's cards, newest first.
	rows, err := db.Query(`SELECT m.id, COALESCE(m.thread_key, ''), COALESCE(m.subject, '')
		FROM messages m
		WHERE `+where+` AND m.internal_date >= ?
		  AND NOT EXISTS (SELECT 1 FROM ticket_sources ts JOIN tickets t ON t.id = ts.ticket_id
		                  WHERE ts.message_id = m.id AND t.rule_id = ?)
		ORDER BY m.date DESC LIMIT ?`,
		append(args, since, r.ID, mailRuleBatch)...)
	if err != nil {
		return err
	}
	type hit struct{ id, thread, subject string }
	var hits []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.thread, &h.subject); err != nil {
			rows.Close()
			return err
		}
		hits = append(hits, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Oldest first, so a conversation's card is titled by how it began.
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		out.Matched++
		existing, err := ticketForRule(h.thread, r.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := AttachSource(existing.ID, h.id, ""); err != nil {
				return err
			}
			out.Attached++
			continue
		}
		title := strings.TrimSpace(h.subject)
		if title == "" {
			title = "(no subject)"
		}
		t := &Ticket{ThreadKey: h.thread, Title: title, Status: r.List, Origin: OriginRule, RuleID: r.ID}
		if err := saveTicket(t, ActorRule); err != nil {
			return err
		}
		if err := AttachSource(t.ID, h.id, ""); err != nil {
			return err
		}
		if err := applyRuleActions(t.ID, r.Actions); err != nil {
			return err
		}
		out.Created++
	}
	_, err = db.Exec("UPDATE ticket_rules SET last_run_at = ? WHERE id = ?", time.Now().UnixMilli(), r.ID)
	return err
}

func ticketForRule(threadKey, ruleID string) (*Ticket, error) {
	if threadKey == "" {
		return nil, nil
	}
	t, err := scanTicket(db.QueryRow("SELECT "+ticketColumns+
		" FROM tickets WHERE thread_key = ? AND rule_id = ?", threadKey, ruleID).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}
