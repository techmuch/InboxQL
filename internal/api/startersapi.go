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
	// GateOff names the label this one is scoped to when that label is
	// switched off. The extractor then still covers whatever the gate already
	// labelled, and will never reach a new message, because nothing is
	// labelling them any more. That is easy to do by accident and invisible
	// without being told.
	GateOff string `json:"gateOff,omitempty"`
	// Slow marks an engine measured in tens of seconds per message.
	Slow bool `json:"slow"`
	// Enabled is whether an installed one runs. Meaningless when Installed is
	// false, and the chooser treats absent and off the same way: unticked.
	Enabled bool `json:"enabled"`
	// Records and Corrections are what an installed one is holding, counted in
	// annotation rows rather than messages — rows are what a delete would
	// take, and on a worked mailbox they outnumber messages roughly nine to
	// one. Shown so the claim "nothing is lost" can be checked against a
	// number before the switch is touched.
	Records     int64 `json:"records"`
	Corrections int64 `json:"corrections"`
}

func registerStarterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/starters", handleListStarters)
	mux.HandleFunc("POST /api/starters", handleInstallStarters)
	mux.HandleFunc("PUT /api/starters/{name}", handleSetStarter)
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
		if a, ok := existing[s.Name]; ok {
			info.Installed, info.Enabled = true, a.Enabled
			if v, err := store.AnnotationVolumeOf(a.ID); err == nil {
				info.Records, info.Corrections = v.Records, v.Human
			}
		}

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
		if gate, ok := existing[s.Needs]; ok && !gate.Enabled {
			info.GateOff = s.Needs
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

// handleSetStarter switches one starter on or off, creating it if need be.
//
// # Why a toggle and not a tick-box over install
//
// The obvious reading of a chooser is that unticking removes. Removal here
// cascades: deleting an annotator deletes every annotation it ever wrote,
// including the human corrections somebody made by hand. Unticking `receipts`
// on a worked mailbox would cost 269 extracted values and 19 corrections, and
// re-ticking would buy back only the first — eleven minutes of CPU for the
// values, and nothing at all for the judgement.
//
// So the tick means *participation*, and both directions are one UPDATE. The
// one case that writes anything new is ticking something absent, which creates
// it inert, with nothing run.
//
// Deleting for real stays in the Annotators panel, where the record count is
// on screen and it is a deliberate act.
func handleSetStarter(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if _, ok := annotate.StarterByName(name); !ok {
		writeError(w, http.StatusNotFound, "no starter named %q", name)
		return
	}

	a, err := store.GetAnnotator(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "%v", err)
		return
	}

	created := false
	if a == nil {
		if !req.Enabled {
			// Switching off something that was never there is already true.
			writeJSON(w, http.StatusOK, map[string]any{
				"name": name, "enabled": false, "installed": false,
			})
			return
		}
		if _, _, err := annotate.InstallStarters([]string{name}); err != nil {
			writeError(w, http.StatusBadRequest, "%v", err)
			return
		}
		created = true
	} else if a.Enabled != req.Enabled {
		if err := store.SetAnnotatorEnabled(name, req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "%v", err)
			return
		}
	}

	out := map[string]any{
		"name": name, "enabled": req.Enabled, "installed": true, "created": created,
	}
	// What it is still holding, so the UI can say what switching off kept
	// rather than asking the reader to trust it. Rows, because rows are what a
	// delete would take.
	if fresh, err := store.GetAnnotator(name); err == nil && fresh != nil {
		if v, err := store.AnnotationVolumeOf(fresh.ID); err == nil {
			out["records"], out["corrections"] = v.Records, v.Human
		}
	}
	writeJSON(w, http.StatusOK, out)
}
