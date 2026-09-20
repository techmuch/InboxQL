package annotate

import (
	"encoding/json"
	"fmt"

	"github.com/user/inboxql/internal/store"
)

// # Somewhere to start
//
// An annotator is the most capable thing here and the hardest to begin using:
// you must invent a schema, choose an engine, write a scope and run a batch
// job before seeing a single result. These are a first draft of the six
// annotators most mailboxes want, meant to be edited rather than obeyed.
//
// # The mix is the lesson
//
// Every expensive extractor ships paired with a cheap rule label that scopes
// it. The rule is a query — free, instant, deterministic — and it narrows a
// job costing fifteen to twenty seconds per message down to the messages that
// could possibly match.
//
// A pack where everything was a span extractor would teach the opposite
// instinct and cost an hour of CPU on a small mailbox, a day on a real one.
// The pairing is the part worth copying.
//
// # They arrive inert
//
// Creating these runs nothing. They appear with coverage at zero and wait to
// be told, because six extractors over a mailbox is not a thing to start by
// accident.

// StarterFloor is the confidence worth trusting these at.
//
// Not a knob on the annotator — `extract:x@0.6` asks the question at read
// time, against stored scores, without re-running anything. It is here because
// the pack should say it once rather than have six extractors each rediscover
// it.
//
// 0.6 is not arbitrary. Below it, on a real mailbox, a span extractor asked
// for reference numbers starts returning card-shaped strings: an EMV terminal
// identifier at 0.53, a transaction certificate at 0.52, a fifteen-digit
// number at 0.54. Above it they disappear and the real values stay.
const StarterFloor = 0.6

// Starter is one annotator worth having, and why.
type Starter struct {
	Name string
	Kind string
	// Engine is rule for the labels, gliner for the extractors.
	Engine string
	// Instructions is a query for a rule, prose for anything else.
	Instructions string
	// Fields are a span extractor's labels. Empty for a label.
	Fields map[string]string
	// Scope narrows an extractor to what a cheap label already found.
	Scope string
	// About says what this is for, in one line, for a listing.
	About string
	// Needs names the starter this one is scoped by, so a pack can be created
	// in an order where the scope already exists.
	Needs string
}

// Starters are the pack.
//
// Chosen against a real mailbox rather than imagined: receipts and purchases
// were its largest cluster, then files, then bills, tickets and travel. The
// rules are deliberately broad — a label that misses is worse than one that
// over-includes, because the extractor it scopes will simply find nothing in
// the extras.
var Starters = []Starter{
	{
		Name: "money", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: `subject:receipt OR subject:invoice OR subject:payment OR subject:order OR subject:purchase`,
		About:        "mail about a purchase — the cheap gate for `receipts`",
	},
	{
		Name: "receipts", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Scope: "label:money", Needs: "money",
		About: "what you paid, to whom, and the reference for it",
		Fields: map[string]string{
			"amount":       "a monetary amount, as written",
			"merchant":     "the shop or company",
			"order number": "an order or confirmation number, as written",
		},
		Instructions: "Amounts, merchants and order numbers from receipts and purchase mail.",
	},

	{
		Name: "billing", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: `subject:bill OR subject:statement OR subject:"due" OR subject:balance OR subject:overdue`,
		About:        "a bill or statement — the cheap gate for `bills`",
	},
	{
		Name: "bills", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Scope: "label:billing", Needs: "billing",
		About: "what is owed and by when",
		Fields: map[string]string{
			"amount":   "the amount owed, as written",
			"due date": "the date payment is due, as written",
			"merchant": "who is billing",
		},
		Instructions: "Amounts and due dates from bills and statements.",
	},

	{
		Name: "travel", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: `subject:reservation OR subject:confirmation OR subject:flight OR subject:trip OR subject:booking OR subject:itinerary`,
		About:        "a booking — the cheap gate for `bookings`",
	},
	{
		Name: "bookings", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Scope: "label:travel", Needs: "travel",
		About: "where you are going, when, and under what reference",
		Fields: map[string]string{
			"confirmation number": "the booking reference, as written",
			"location":            "the place being travelled to or stayed at",
			"date":                "a date of travel or stay, as written",
		},
		Instructions: "Confirmation numbers, places and dates from bookings and itineraries.",
	},

	{
		Name: "shipping", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: `subject:shipped OR subject:shipment OR subject:delivery OR subject:tracking OR subject:dispatched OR subject:parcel OR subject:pickup`,
		About:        "something in transit — the cheap gate for `parcels`",
	},
	{
		Name: "parcels", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Scope: "label:shipping", Needs: "shipping",
		About: "what is coming and how to follow it",
		Fields: map[string]string{
			"tracking number": "the carrier's tracking number, as written",
			"merchant":        "who sent it",
			"date":            "an expected or dispatched date, as written",
		},
		Instructions: "Tracking numbers and carriers from shipping mail.",
	},

	{
		Name: "support", Kind: store.KindLabel, Engine: store.EngineRule,
		Instructions: `subject:ticket OR subject:support OR subject:"case" OR subject:incident OR subject:"ref #"`,
		About:        "a support thread — the cheap gate for `cases`",
	},
	{
		Name: "cases", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		Scope: "label:support", Needs: "support",
		About: "the reference a support thread hangs on",
		Fields: map[string]string{
			"ticket number": "the case or ticket reference, as written",
			"merchant":      "the company providing support",
		},
		Instructions: "Ticket references from support mail.",
	},

	{
		Name: "paperwork", Kind: store.KindExtract, Engine: store.EngineGLiNER,
		// Deliberately scoped by an attachment rather than a label: on a real
		// mailbox most of the extractable values live inside the PDFs, and
		// `has:attachment` is a cheaper and truer gate than any subject rule.
		Scope: "has:attachment",
		About: "amounts and dates inside attached documents",
		Fields: map[string]string{
			"amount":   "a monetary amount, as written",
			"due date": "a date something is due, as written",
			"merchant": "the company the document is from",
		},
		Instructions: "Amounts, dates and companies from attached documents.",
	},
}

// InstallStarters creates the pack, skipping anything already named.
//
// Creating is not running. They appear with coverage at zero and wait; six
// extractors over a mailbox is an hour of CPU on a small one and a day on a
// real one, which is not a thing to begin by accident.
func InstallStarters(only []string) ([]string, []string, error) {
	want := map[string]bool{}
	for _, n := range only {
		want[n] = true
	}

	existing := map[string]bool{}
	list, err := store.ListAnnotators()
	if err != nil {
		return nil, nil, err
	}
	for _, a := range list {
		existing[a.Name] = true
	}

	var made, skipped []string
	for _, s := range Starters {
		if len(want) > 0 && !want[s.Name] {
			continue
		}
		if existing[s.Name] {
			// Never overwrite: an annotator with this name may have been
			// edited, and its results belong to whoever edited it.
			skipped = append(skipped, s.Name)
			continue
		}

		schema := "{}"
		if len(s.Fields) > 0 {
			blob, err := json.Marshal(s.Fields)
			if err != nil {
				return made, skipped, err
			}
			schema = string(blob)
		}

		a := &store.Annotator{
			Name: s.Name, Kind: s.Kind, Engine: s.Engine,
			Instructions: s.Instructions, SchemaJSON: schema,
			// The scope is carried, not merely suggested: an extractor that
			// needs narrowing to a cheap label should not depend on whoever
			// runs it remembering to say so.
			Scope:   s.Scope,
			Trigger: store.TriggerManual,
		}
		if err := store.SaveAnnotator(a); err != nil {
			return made, skipped, fmt.Errorf("creating %s: %w", s.Name, err)
		}
		made = append(made, s.Name)
		existing[s.Name] = true
	}
	return made, skipped, nil
}

// StarterByName finds one, for reporting what it is scoped by.
func StarterByName(name string) (Starter, bool) {
	for _, s := range Starters {
		if s.Name == name {
			return s, true
		}
	}
	return Starter{}, false
}
