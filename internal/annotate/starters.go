package annotate

import (
	"github.com/user/inboxql/internal/elements"
	"github.com/user/inboxql/internal/store"
)

// # Somewhere to start
//
// The pack is the elements catalog (internal/elements): receipts, travel,
// calendar events, tasks, waiting, importance, what kind of sender someone
// is, and topics — the things worth knowing about mail later.
//
// It used to be a decision model gating span extractors: "is this a purchase"
// at three seconds a message in front of a receipt extractor at twenty. Both
// read a few hundred tokens of one message, which is too little to know that
// "885911470902 FLEXVOLT 20-" is a DeWalt battery or that "I'll get back to you
// Monday" means you owe the reply. The elements are answered by an AI harness
// that reads whole conversations, through `iql work`.
//
// # They arrive inert
//
// Creating these runs nothing: the harness answers them when it is pointed at
// `iql work next`.

// StarterFloor is the confidence worth trusting a model's answers at.
//
// Not a knob on the annotator — `extract:x@0.6` asks the question at read
// time, against stored scores, without re-running anything.
const StarterFloor = 0.6

// Starter is one annotator worth having, and why.
type Starter struct {
	Name   string
	Kind   string
	Engine string
	// Instructions is what the reader is asked.
	Instructions string
	// Fields are a record's fields, by name, for a listing. Empty for a label.
	Fields map[string]string
	// Scope narrows what it runs over. The elements carry none: the harness
	// reads newest first and answers none where nothing applies.
	Scope string
	// About says what this is for, in one line, for a listing.
	About string
	// Needs names a starter this one is scoped by. Unused by the elements.
	Needs string
	// Schema is the stored schema.
	Schema string
}

// Starters are the pack: the elements catalog, as starters.
var Starters = func() []Starter {
	out := make([]Starter, 0, len(elements.Catalog))
	for _, e := range elements.Catalog {
		var fields map[string]string
		if len(e.Fields) > 0 {
			fields = map[string]string{}
			for _, f := range e.Fields {
				fields[f.Name] = f.Describe
			}
		}
		out = append(out, Starter{Name: e.Name, Kind: e.Kind(), Engine: store.EngineAgent,
			Instructions: e.Instructions, Fields: fields, About: e.About, Schema: e.SchemaJSON()})
	}
	return out
}()

// InstallStarters creates the pack, or the named starters.
//
// A same-named annotator is replaced only when it has never answered anything
// — the decision and span starters most mailboxes installed and never ran —
// and otherwise left alone: its results belong to whoever made it.
func InstallStarters(only []string) ([]string, []string, error) {
	made, replaced, skipped, err := elements.Install(only, false)
	return append(made, replaced...), skipped, err
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
