// Package skills installs a short guide to `iql` where AI coding tools look
// for skills, so an agent the user runs already knows that `send` does not
// send, that `-label:x` leaves out what was never looked at, and that `me()`
// exists.
//
// # Where they go
//
// Claude Code, Codex and Gemini CLI all read the same format — a folder with a
// SKILL.md whose frontmatter has a name and a description — from different
// places. Claude Code reads ~/.claude/skills; Codex reads ~/.agents/skills;
// Gemini CLI reads ~/.gemini/skills and, as an alias, ~/.agents/skills. So
// two locations cover all three, and Gemini is served by the shared one
// rather than given a second copy it would also see.
//
// # What it will touch
//
// Only a folder named iql inside those skills directories, and only one it can
// show it wrote: a SKILL.md carrying the marker line. A folder of the same name
// without it is somebody else's and is left alone, install and remove alike.
// The text is fixed and embedded in this binary; nothing a caller sends ends up
// in an AI tool's instructions.
package skills

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed SKILL.md
var template string

// Name is the skill's folder and frontmatter name.
const Name = "iql"

const marker = "<!-- Installed by iql "

var versionLine = regexp.MustCompile(`<!-- Installed by iql (\S+)\.`)

// Content is the skill as this version writes it.
func Content(version string) string {
	return strings.Replace(template, "{{VERSION}}", version, 1)
}

// Location is one place skills are read from.
type Location struct {
	ID   string `json:"id"`
	Dir  string `json:"dir"` // the skills directory; the skill is Dir/iql
	Path string `json:"path"`
	// Tools are the tools that read this location.
	Tools []string `json:"tools"`
}

// Tool is one AI coding tool that may be on this machine.
type Tool struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	// Location is where its skill goes.
	Location string `json:"location"`
	// Evidence is what detection saw — the folder that says it is here.
	Evidence string `json:"evidence,omitempty"`
}

// State is a location's skill as found on disk.
type State struct {
	Location
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Current   bool   `json:"current"`
	// Foreign is a skill named iql that this program did not write.
	Foreign bool `json:"foreign,omitempty"`
}

// Locations are where skills are read from, for a home directory.
func Locations(home string) []Location {
	claude := filepath.Join(home, ".claude", "skills")
	agents := filepath.Join(home, ".agents", "skills")
	return []Location{
		{ID: "claude", Dir: claude, Path: filepath.Join(claude, Name, "SKILL.md"), Tools: []string{"claude-code"}},
		{ID: "agents", Dir: agents, Path: filepath.Join(agents, Name, "SKILL.md"), Tools: []string{"codex", "gemini"}},
	}
}

// Detect reports which tools are on this machine.
//
// By their folders in the home directory, not by a program on the PATH: the
// login service runs with a minimal PATH and would miss tools that are plainly
// installed. Detection reads; it never writes.
func Detect(home string) []Tool {
	tools := []Tool{
		{ID: "claude-code", Name: "Claude Code", Location: "claude", Evidence: filepath.Join(home, ".claude")},
		{ID: "codex", Name: "Codex", Location: "agents", Evidence: filepath.Join(home, ".codex")},
		{ID: "gemini", Name: "Gemini CLI", Location: "agents", Evidence: filepath.Join(home, ".gemini")},
	}
	for i := range tools {
		if fi, err := os.Stat(tools[i].Evidence); err == nil && fi.IsDir() {
			tools[i].Installed = true
		}
	}
	return tools
}

// Find returns a location by id.
func Find(home, id string) (Location, error) {
	for _, l := range Locations(home) {
		if l.ID == id {
			return l, nil
		}
	}
	return Location{}, fmt.Errorf("no skills location %q (try claude or agents)", id)
}

// Status reads what is installed at each location.
func Status(home, version string) []State {
	var out []State
	for _, l := range Locations(home) {
		out = append(out, stateOf(l, version))
	}
	return out
}

func stateOf(l Location, version string) State {
	st := State{Location: l}
	b, err := os.ReadFile(l.Path)
	if err != nil {
		if _, derr := os.Stat(filepath.Dir(l.Path)); derr == nil {
			st.Foreign = true // a folder named iql, not ours
		}
		return st
	}
	if !strings.Contains(string(b), marker) {
		st.Foreign = true
		return st
	}
	st.Installed = true
	if m := versionLine.FindStringSubmatch(string(b)); m != nil {
		st.Version = m[1]
	}
	st.Current = string(b) == Content(version)
	return st
}

// ErrForeign is a skill folder this program did not write.
var ErrForeign = errors.New("a skill named iql is already there and was not installed by iql; it has been left alone")

// Install writes the skill at a location, replacing an earlier one of ours.
func Install(l Location, version string) (State, error) {
	if st := stateOf(l, version); st.Foreign {
		return st, ErrForeign
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return State{}, err
	}
	tmp := l.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(Content(version)), 0o644); err != nil {
		return State{}, err
	}
	if err := os.Rename(tmp, l.Path); err != nil {
		return State{}, err
	}
	return stateOf(l, version), nil
}

// Remove deletes the skill at a location, if it is ours.
func Remove(l Location, version string) (State, error) {
	st := stateOf(l, version)
	if st.Foreign {
		return st, ErrForeign
	}
	if !st.Installed {
		return st, nil
	}
	if err := os.Remove(l.Path); err != nil {
		return st, err
	}
	// The folder too, if nothing else was put in it.
	_ = os.Remove(filepath.Dir(l.Path))
	return stateOf(l, version), nil
}

// Refresh rewrites every installed skill of ours that an earlier version
// wrote. Installing one was the person's decision; keeping it describing the
// program that is actually installed is upkeep. Returns what it rewrote.
func Refresh(home, version string) []string {
	var done []string
	for _, l := range Locations(home) {
		if st := stateOf(l, version); st.Installed && !st.Current {
			if _, err := Install(l, version); err == nil {
				done = append(done, l.Path)
			}
		}
	}
	return done
}
