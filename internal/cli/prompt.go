package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// IsStdoutTerminal reports whether stdout is attached to a terminal.
//
// Progress lines rewrite themselves with carriage returns, which is right on a
// terminal and garbage in a pipe or a log file.
func IsStdoutTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// IsTerminal reports whether stdin is attached to a terminal.
//
// This is the mechanism behind the outbox approval gate: an agent invoking iql
// through a pipe cannot satisfy it, and no flag exists to override it. See
// [approveDraft].
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// readLine reads one line from the context's stdin.
func (c *Context) readLine() (string, error) {
	r := bufio.NewReader(c.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Confirm asks a yes/no question, defaulting to no.
//
// Anything other than an exact "yes" is a refusal: for an irreversible action
// a stray keystroke should not count as consent.
func (c *Context) Confirm(prompt string) bool {
	fmt.Fprintf(c.Stdout, "%s [type 'yes' to confirm]: ", prompt)
	answer, err := c.readLine()
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(answer), "yes")
}

// ReadSecret obtains a secret without echoing it, in this order:
//
//  1. the named environment variable, for unattended use;
//  2. stdin, when it is a pipe — one line, so secrets can be fed from a
//     password manager without ever reaching argv;
//  3. an interactive prompt with echo disabled.
//
// A command-line flag is deliberately not among the options. requirements.md
// 2.4 specifies `iql account add --pass`, but a password in argv is visible in
// shell history and to every user on the machine via ps.
func (c *Context) ReadSecret(envVar, prompt string) (string, error) {
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return v, nil
		}
	}

	if !IsTerminal() {
		line, err := c.readLine()
		if err != nil {
			return "", Fail(ExitUsage,
				"no terminal available to prompt for %s; set %s or pipe it on stdin",
				prompt, envVar)
		}
		if line == "" {
			return "", Fail(ExitUsage, "%s was empty", prompt)
		}
		return line, nil
	}

	fmt.Fprintf(c.Stdout, "%s: ", prompt)
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(c.Stdout)
	if err != nil {
		return "", Fail(ExitError, "failed to read %s: %v", prompt, err)
	}
	if len(secret) == 0 {
		return "", Fail(ExitUsage, "%s was empty", prompt)
	}
	return string(secret), nil
}

// ReadBody reads a message body from a file, a literal string, or stdin when
// the value is "-".
func (c *Context) ReadBody(value, file string) (string, error) {
	switch {
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", Fail(ExitError, "cannot read %s: %v", file, err)
		}
		return string(b), nil
	case value == "-":
		b, err := readAll(c.Stdin)
		if err != nil {
			return "", Fail(ExitError, "cannot read stdin: %v", err)
		}
		return string(b), nil
	default:
		return value, nil
	}
}
