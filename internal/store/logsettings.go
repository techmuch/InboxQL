package store

import (
	"encoding/json"
	"strconv"
	"strings"
)

// # What the log records, as opposed to how much
//
// The level is a floor and lives where somebody stands when they want it — the
// Log tab. These are the set-once decisions: how slow is slow, which of the
// noisy categories are worth their volume, how much to keep, and whether the
// words of a query are written down.
//
// They are server-side, because they change what the server records for every
// window. That is why they are not in the General settings panel, which says in
// its own text that what it holds is per-browser.

// Settings keys.
const (
	slowQuerySetting  = "log.slowMs"
	categoriesSetting = "log.categories"
	retentionSetting  = "log.retain"
	queryTextSetting  = "log.queryText"
)

// DefaultSlowMs is when a query stops being ordinary.
//
// Half a second is a guess that should become the user's: somebody on a million
// messages will want it higher and somebody on ten thousand lower. It is a
// default rather than a constant for that reason.
const DefaultSlowMs = 500

// LoggableCategories are the ones that can be turned off.
//
// Only the noisy ones. A category nobody would want to silence does not need a
// switch, and a settings page with a row per subsystem is a page nobody reads.
var LoggableCategories = []string{"http", "query", "sync", "annotate"}

// LogSettings is what the log records.
type LogSettings struct {
	// SlowMs is the threshold above which a query is a warning rather than a
	// debug line.
	SlowMs int `json:"slowMs"`
	// Categories are the high-volume ones that are switched on.
	//
	// An allow-list on top of the level rather than part of it. Debug is
	// otherwise all-or-nothing: turn it up to chase one thing and ten thousand
	// request lines bury it.
	Categories map[string]bool `json:"categories"`
	// Retain is how many lines to keep.
	Retain int `json:"retain"`
	// QueryText is whether the words of a query are written down.
	//
	// A query can name a person or a subject somebody searched for, and the log
	// outlives the query. On by default because this is a local database, and a
	// visible switch because that should be a decision rather than something
	// discovered when a log is attached to a bug report.
	QueryText bool `json:"queryText"`
}

// DefaultLogSettings is the shape before anybody has chosen.
//
// HTTP off: it is the noisiest by an order of magnitude and the least often
// what somebody turning logging up is looking for.
func DefaultLogSettings() LogSettings {
	return LogSettings{
		SlowMs: DefaultSlowMs,
		Categories: map[string]bool{
			"http": false, "query": true, "sync": true, "annotate": true,
		},
		Retain:    MaxLogLines,
		QueryText: true,
	}
}

// LoadLogSettings reads what has been chosen, falling back per field.
//
// Per field rather than wholesale: a setting written by an older build knows
// nothing about a key added later, and returning the whole default because one
// key is missing would quietly undo the others.
func LoadLogSettings() LogSettings {
	s := DefaultLogSettings()

	if v, err := GetSetting(slowQuerySetting); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			s.SlowMs = n
		}
	}
	if v, err := GetSetting(retentionSetting); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.Retain = n
		}
	}
	if v, err := GetSetting(queryTextSetting); err == nil && v != "" {
		s.QueryText = v != "0" && !strings.EqualFold(v, "false")
	}
	if v, err := GetSetting(categoriesSetting); err == nil && v != "" {
		var got map[string]bool
		if json.Unmarshal([]byte(v), &got) == nil {
			for _, c := range LoggableCategories {
				if on, ok := got[c]; ok {
					s.Categories[c] = on
				}
			}
		}
	}
	return s
}

// SaveLogSettings records them.
func SaveLogSettings(s LogSettings) error {
	if s.SlowMs < 0 {
		s.SlowMs = 0
	}
	if s.Retain <= 0 {
		s.Retain = MaxLogLines
	}
	blob, err := json.Marshal(s.Categories)
	if err != nil {
		return err
	}
	for _, kv := range []struct{ k, v string }{
		{slowQuerySetting, strconv.Itoa(s.SlowMs)},
		{retentionSetting, strconv.Itoa(s.Retain)},
		{queryTextSetting, boolSetting(s.QueryText)},
		{categoriesSetting, string(blob)},
	} {
		if err := UpdateSetting(kv.k, kv.v); err != nil {
			return err
		}
	}
	return nil
}

func boolSetting(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
