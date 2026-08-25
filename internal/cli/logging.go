package cli

import (
	"bytes"
	"io"
	"log"
	"strings"
)

// configureLogging decides what the standard logger does for this invocation.
//
// The packages underneath the CLI log through the standard logger, which meant
// every single command opened with three lines of database housekeeping —
//
//	2026/08/25 00:19:04 Initializing database at: /tmp/d/inboxql.db
//	2026/08/25 00:19:04 Current database schema version: 14
//	2026/08/25 00:19:04 Database schema is up to date (version 14).
//
// — before any of its own output. That is internals leaking into the interface:
// a person running `iql read m1` is told about schema versions they did not
// ask about, and it happens on every invocation.
//
// It cannot simply be discarded, though. Two kinds of line in that stream are
// things a user genuinely must see: a schema migration, which changes their
// database on disk and is not something to do silently, and any warning. Those
// pass through even when quiet; the routine chatter does not.
func configureLogging(verbose bool, stderr io.Writer) {
	log.SetFlags(0)
	if verbose {
		log.SetFlags(log.LstdFlags)
		log.SetOutput(stderr)
		return
	}
	log.SetOutput(&consequentialOnly{out: stderr})
}

// consequentialOnly passes through log lines that a user needs to see and
// drops the routine ones.
//
// Matching on message text is not elegant. The alternative — threading a
// logger through store, vault and sync — is a much larger change to packages
// this task is not otherwise touching, and would still need a decision about
// which lines matter. The patterns are narrow and the test pins them, so a
// message that stops matching fails a test rather than disappearing quietly.
type consequentialOnly struct {
	out io.Writer
	buf bytes.Buffer
}

// keep lists the markers that make a log line worth showing unasked.
var keep = []string{
	"migration", // the schema on disk is being changed
	"WARN",      // vault key permissions, and anything else shouted
	"Warning",
	"PANIC",
	"Encrypted", // passwords were rewritten at rest
}

func (c *consequentialOnly) Write(p []byte) (int, error) {
	// The logger writes a whole line per call, but buffer anyway so a split
	// write cannot produce a half-line decision.
	c.buf.Write(p)
	for {
		line, err := c.buf.ReadString('\n')
		if err != nil {
			// Incomplete line; put it back and wait for the rest.
			c.buf.WriteString(line)
			break
		}
		if consequential(line) {
			if _, err := io.WriteString(c.out, line); err != nil {
				return len(p), err
			}
		}
	}
	return len(p), nil
}

func consequential(line string) bool {
	for _, marker := range keep {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}
