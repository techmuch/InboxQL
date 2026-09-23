package api

import (
	"net/http"
	"sort"

	"github.com/user/inboxql/internal/annotate"
	"github.com/user/inboxql/internal/store"
)

// # The starter pack, over HTTP
//
// `iql annotate starters` was CLI-only, which made the pack invisible to
// anyone who found the Annotators panel first — the people it is most for.
//
// Listing reports how much of *this* mailbox each one reaches, because a rule
// matching nothing here is not worth installing, and an extractor gated by a
// label that has not run matches nothing *yet* — which reads as "this finds
// nothing" when it means "nobody has asked the gate".

// StarterInfo is one starter as a chooser shows it.
type StarterInfo struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Engine string `json:"engine"`
	About  string `json:"about"`
	Scope  string `json:"scope,omitempty"`
	// Fields are a span extractor's labels, so the chooser can say what it
	// will look for without opening the editor.
	Fields []string `json:"fields,omitempty"`
	// Installed is true when an annotator of this name already exists. It is
	// never overwritten: it may have been edited, and its results belong to
	// whoever edited it.
	Installed bool `json:"installed"`
	// Reach is how many of this mailbox's messages it would cover.
	Reach int64 `json:"reach"`
	// Gate names the label this one waits on, when that label has not run.
	// Reach is then the gate's reach rather than this one's.
	Gate string `json:"gate,omitempty"`
	// Slow marks an engine measured in tens of seconds per message.
	Slow bool `json:"slow"`
}

func registerStarterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/starters", handleListStarters)
	mux.HandleFunc("POST /api/starters", handleInstallStarters)
}

func handleListStarters(w http.ResponseWriter, r *http.Request) {
	existing := map[string]*store.Annotator{}
	if list, err := store.ListAnnotators(); err == nil {
		for _, a := range list {
			existing[a.Name] = a
		}
	}

	out := []StarterInfo{}
	for _, s := range annotate.Starters {
		info := StarterInfo{
			Name: s.Name, Kind: s.Kind, Engine: s.Engine,
			About: s.About, Scope: s.Scope,
			Slow: s.Engine != store.EngineRule,
		}
		for f := range s.Fields {
			info.Fields = append(info.Fields, f)
		}
		sort.Strings(info.Fields)
		_, info.Installed = existing[s.Name]

		probe := s.Scope
		if s.Engine == store.EngineRule {
			probe = s.Instructions
		}
		// An extractor whose gate has not run reaches nothing yet. Reporting
		// that as its reach would read as "this finds nothing"; reporting the
		// gate's reach, and saying so, is the true answer.
		if s.Needs != "" && !hasEvaluated(existing[s.Needs]) {
			if gate, ok := annotate.StarterByName(s.Needs); ok {
				probe, info.Gate = gate.Instructions, s.Needs
			}
		}
		if probe != "" {
			if n, err := store.CountQuery(probe); err == nil {
				info.Reach = n
			}
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleInstallStarters creates the chosen ones.
//
// Creating is not running. Six extractors over a mailbox is an hour of CPU on
// a small one and a day on a real one, so they arrive with coverage at zero
// and the caller decides what to start.
func handleInstallStarters(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Names []string `json:"names"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	made, skipped, err := annotate.InstallStarters(req.Names)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"created": made, "skipped": skipped,
	})
}

// hasEvaluated reports whether an annotator has looked at anything yet.
//
// For the purpose of gating something else, an installed annotator that has
// never run is the same as one that does not exist.
func hasEvaluated(a *store.Annotator) bool {
	if a == nil {
		return false
	}
	p, err := store.Progress(a, "")
	return err == nil && p.Evaluated > 0
}
