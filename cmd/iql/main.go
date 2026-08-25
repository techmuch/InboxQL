// Command iql is the InboxQL server and administrative CLI.
//
// The same binary serves the dashboard (`iql serve`) and provides the command
// surface documented in AGENTS.md. Everything of substance lives in
// internal/cli; this is dispatch and nothing else.
package main

import (
	"os"

	"github.com/user/inboxql/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
