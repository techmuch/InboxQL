// Package ask answers a question in words by running the agent CLI the person
// already uses — Claude Code — on this computer, with the iql skill and
// read-only iql commands only.
//
// # What leaves the computer
//
// Claude Code sends the question, and whatever the commands it runs return, to
// Anthropic under the person's own account. That is why asking is off until
// the person turns it on, and why every answer lists the commands it ran.
//
// # What the agent may do
//
// Read. Its allowed tools are iql's read commands; anything else is refused by
// Claude Code itself, without asking, because nobody is there to ask. Changes
// it would suggest come back as words for a person to act on.
package ask

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AllowedTools are the commands the agent may run: iql's read commands, in
// both the --json-first and command-first spellings.
var AllowedTools = func() []string {
	var out []string
	for _, c := range []string{"query", "read", "search", "contact show", "contact responsiveness", "calendar agenda", "calendar show", "ticket list", "ticket show", "sql", "saved list", "saved show", "where", "elements list", "help"} {
		out = append(out, "Bash(iql --json "+c+":*)", "Bash(iql "+c+":*)")
	}
	return out
}()

// DisallowedTools are refused outright, belt and braces.
var DisallowedTools = []string{"Edit", "Write", "NotebookEdit", "WebFetch", "WebSearch"}

// SystemPrompt is appended to Claude Code's own.
const SystemPrompt = `You are answering a question about the person's own mailbox through InboxQL.
Use only iql read commands (iql --json query, read, search, contact show, calendar agenda, ticket list, sql). Run "iql help agents" only if you must.
Never send, archive, label, trash, or change anything; if a change would help, say so in words.
Answer in a few plain sentences first, with the figures that matter, then say briefly what the answer rests on.`

// Command is one command the agent ran.
type Command struct {
	Command string `json:"command"`
	// Refused is set when Claude Code would not run it.
	Refused bool `json:"refused,omitempty"`
}

// Answer is what came back.
type Answer struct {
	Question string    `json:"question"`
	Text     string    `json:"text"`
	Commands []Command `json:"commands"`
	Session  string    `json:"session,omitempty"`
	Seconds  float64   `json:"seconds"`
	CostUSD  float64   `json:"costUsd,omitempty"`
	CLI      string    `json:"cli"`
	Error    string    `json:"error,omitempty"`
}

// Find locates the claude binary: on PATH, or where its installer puts it —
// a service started at login has a short PATH.
func Find() string {
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// Runner runs the CLI; replaced in tests.
var Runner = func(ctx context.Context, bin string, args []string, env []string, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

// Options say where the mailbox is and which iql to put on PATH.
type Options struct {
	Bin     string // the claude binary
	DataDir string // INBOXQL_DATA for the commands it runs
	IQLDir  string // directory holding the iql binary
	Session string // a session to continue, for a follow-up
	Timeout time.Duration
}

// Ask runs one question.
func Ask(ctx context.Context, question string, o Options) Answer {
	a := Answer{Question: question, CLI: "Claude Code", Commands: []Command{}}
	question = strings.TrimSpace(question)
	if question == "" {
		a.Error = "ask a question"
		return a
	}
	if o.Bin == "" {
		a.Error = "Claude Code is not installed on this computer"
		return a
	}
	if o.Timeout == 0 {
		o.Timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	args := []string{"-p", question, "--output-format", "stream-json", "--verbose",
		"--append-system-prompt", SystemPrompt,
		"--allowedTools", strings.Join(AllowedTools, ","),
		"--disallowedTools", strings.Join(DisallowedTools, ",")}
	if o.Session != "" {
		args = append(args, "--resume", o.Session)
	}
	env := os.Environ()
	if o.DataDir != "" {
		env = append(env, "INBOXQL_DATA="+o.DataDir)
	}
	if o.IQLDir != "" {
		env = append(env, "PATH="+o.IQLDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	start := time.Now()
	out, err := Runner(ctx, o.Bin, args, env, os.TempDir())
	a.Seconds = time.Since(start).Round(100 * time.Millisecond).Seconds()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			a.Error = fmt.Sprintf("no answer after %s; stopped", o.Timeout)
		} else {
			a.Error = err.Error()
		}
		return a
	}
	parse(out, &a)
	return a
}

// parse reads Claude Code's stream-json: the Bash commands it ran, and the
// result.
func parse(out []byte, a *Answer) {
	type block struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	type line struct {
		Type    string  `json:"type"`
		Subtype string  `json:"subtype"`
		Result  string  `json:"result"`
		Session string  `json:"session_id"`
		Cost    float64 `json:"total_cost_usd"`
		IsError bool    `json:"is_error"`
		Message struct {
			Content []block `json:"content"`
		} `json:"message"`
		Denials []struct {
			ToolInput struct {
				Command string `json:"command"`
			} `json:"tool_input"`
		} `json:"permission_denials"`
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if l.Session != "" {
			a.Session = l.Session
		}
		switch l.Type {
		case "assistant":
			for _, b := range l.Message.Content {
				if b.Type == "tool_use" && b.Name == "Bash" {
					var in struct {
						Command string `json:"command"`
					}
					if json.Unmarshal(b.Input, &in) == nil && in.Command != "" {
						a.Commands = append(a.Commands, Command{Command: in.Command})
					}
				}
			}
		case "result":
			a.Text = strings.TrimSpace(l.Result)
			a.CostUSD = l.Cost
			if l.IsError && a.Text == "" {
				a.Error = "Claude Code could not answer (" + l.Subtype + ")"
			}
			for _, d := range l.Denials {
				for i := range a.Commands {
					if a.Commands[i].Command == d.ToolInput.Command {
						a.Commands[i].Refused = true
					}
				}
			}
		}
	}
	if a.Text == "" && a.Error == "" {
		a.Error = "Claude Code returned no answer"
	}
}
