// Package elements is the catalog of what is worth knowing about mail later.
//
// # Why a catalog
//
// "Find the Home Depot receipt for anything made by DeWalt" failed on a
// mailbox that had three, because the receipts say `885911470902 FLEXVOLT
// 20-` and never "DeWalt". Knowing that is reading, not searching, and the
// reading is worth doing once and keeping. So is knowing who is waiting on
// whom, which no rule about who sent last gets right.
//
// An element is one such thing: receipts, travel, calendar events, tasks,
// waiting, importance, what kind of sender someone is, topics. Each is an
// annotator answered by the agent engine — an AI harness reading
// conversations with `iql work next` and answering with `iql work submit` —
// so it versions, re-runs, is corrected and is queried like every other
// annotator. This package says what each one asks and what an answer must
// look like; internal/work hands them out and checks what comes back.
package elements

import (
	"encoding/json"
	"fmt"

	"github.com/user/inboxql/internal/store"
)

// What an element is about.
const (
	UnitMessage = store.UnitMessage
	UnitThread  = store.UnitThread
	// UnitSender is a person or system who wrote in the conversation. It is
	// asked about only while they are unclassified.
	UnitSender = "sender"
)

// What shape an answer takes.
const (
	ShapeLevels  = "levels"
	ShapeRecords = "records"
	ShapeTags    = "tags"
)

// Where answers go besides the annotation store.
const (
	TargetTickets  = "tickets"
	TargetCalendar = "calendar"
	TargetContacts = "contacts"
	TargetTopics   = "topics"
)

// Field types for records.
const (
	TypeText     = "text"
	TypeNumber   = "number"
	TypeDate     = "date"     // 2026-10-20
	TypeDateTime = "datetime" // 2026-10-20T14:30 or with a zone: …-04:00
	TypeBool     = "bool"
	TypeList     = "list"
)

// Field is one field of a record.
type Field struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Describe string   `json:"describe"`
	Required bool     `json:"required,omitempty"`
	Enum     []string `json:"enum,omitempty"`
	// Of is a list's item fields.
	Of []Field `json:"of,omitempty"`
}

// Element is one thing worth knowing.
type Element struct {
	Name  string `json:"name"`
	About string `json:"about"`
	// Instructions is what the reader is asked, in full.
	Instructions string        `json:"instructions"`
	Unit         string        `json:"unit"`
	Shape        string        `json:"shape"`
	Levels       []store.Level `json:"levels,omitempty"`
	Fields       []Field       `json:"fields,omitempty"`
	Target       string        `json:"target,omitempty"`
	// Version is the annotator's, filled in from the store: an answer made
	// against an older version is refused.
	Version int `json:"version,omitempty"`
}

// schema is what the annotator keeps in schema_json. Unit and levels sit at
// the top so the label code reads them as it reads any label's.
type schema struct {
	Element bool          `json:"element"`
	Unit    string        `json:"unit"`
	Shape   string        `json:"shape"`
	Levels  []store.Level `json:"levels,omitempty"`
	Fields  []Field       `json:"fields,omitempty"`
	Target  string        `json:"target,omitempty"`
}

// Kind is the annotator kind an element is stored as: a levelled answer is a
// label, so `label:` and rulings work on it; records and tags are extractions.
func (e Element) Kind() string {
	if e.Shape == ShapeLevels {
		return store.KindLabel
	}
	return store.KindExtract
}

// SchemaJSON is the element as the annotator stores it.
func (e Element) SchemaJSON() string {
	unit := e.Unit
	b, _ := json.Marshal(schema{Element: true, Unit: unit, Shape: e.Shape,
		Levels: e.Levels, Fields: e.Fields, Target: e.Target})
	return string(b)
}

// HasLevel reports whether a level is one of the element's.
func (e Element) HasLevel(l string) bool {
	for _, x := range e.Levels {
		if x.Label == l {
			return true
		}
	}
	return false
}

// FromAnnotator reads an element back from its annotator, or false when the
// annotator is not one.
func FromAnnotator(a *store.Annotator) (Element, bool) {
	if a == nil || a.Engine != store.EngineAgent {
		return Element{}, false
	}
	var s schema
	if err := json.Unmarshal([]byte(a.SchemaJSON), &s); err != nil || !s.Element {
		return Element{}, false
	}
	return Element{Name: a.Name, Instructions: a.Instructions, Unit: s.Unit, Shape: s.Shape,
		Levels: s.Levels, Fields: s.Fields, Target: s.Target, Version: a.Version, About: about(a.Name)}, true
}

func about(name string) string {
	if e, ok := ByName(name); ok {
		return e.About
	}
	return ""
}

// Installed returns the elements that exist and are switched on, with their
// annotators.
func Installed() ([]Element, map[string]*store.Annotator, error) {
	list, err := store.ListAnnotators()
	if err != nil {
		return nil, nil, err
	}
	var out []Element
	byName := map[string]*store.Annotator{}
	for _, a := range list {
		if e, ok := FromAnnotator(a); ok && a.Enabled {
			out = append(out, e)
			byName[a.Name] = a
		}
	}
	return out, byName, nil
}

// ByName finds an element in the catalog.
func ByName(name string) (Element, bool) {
	for _, e := range Catalog {
		if e.Name == name {
			return e, true
		}
	}
	return Element{}, false
}

// Install creates the catalog's elements, or the named ones.
//
// A same-named annotator is replaced only when it has never answered
// anything — the laya and span starters most mailboxes installed and never
// ran — or when replace is set. Replacing keeps its id, so a version bump
// marks the old question's answers as answers to that question.
func Install(only []string, replace bool) (made, replaced, skipped []string, err error) {
	want := map[string]bool{}
	for _, n := range only {
		if _, ok := ByName(n); !ok {
			return nil, nil, nil, fmt.Errorf("no element named %q", n)
		}
		want[n] = true
	}
	for _, e := range Catalog {
		if len(want) > 0 && !want[e.Name] {
			continue
		}
		existing, err := store.GetAnnotator(e.Name)
		if err != nil {
			return made, replaced, skipped, err
		}
		if existing != nil {
			if _, ok := FromAnnotator(existing); ok && !replace {
				skipped = append(skipped, e.Name)
				continue
			}
			v, err := store.AnnotationVolumeOf(existing.ID)
			if err != nil {
				return made, replaced, skipped, err
			}
			if v.Records+v.Human > 0 && !replace {
				skipped = append(skipped, e.Name)
				continue
			}
		}
		// SaveAnnotator bumps the version when the engine, the instructions or
		// an element's schema changed, so earlier answers stay attributed to
		// the question they answered.
		a := &store.Annotator{Name: e.Name, Kind: e.Kind(), Engine: store.EngineAgent,
			Instructions: e.Instructions, SchemaJSON: e.SchemaJSON(), Trigger: store.TriggerManual}
		if err := store.SaveAnnotator(a); err != nil {
			return made, replaced, skipped, fmt.Errorf("creating %s: %w", e.Name, err)
		}
		if err := store.SetAnnotatorEnabled(e.Name, true); err != nil {
			return made, replaced, skipped, err
		}
		if existing != nil {
			replaced = append(replaced, e.Name)
		} else {
			made = append(made, e.Name)
		}
	}
	return made, replaced, skipped, nil
}
