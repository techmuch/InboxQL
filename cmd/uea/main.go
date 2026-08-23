// Command uea is the Universal Email Analytics server and administrative CLI.
//
// The same binary serves the dashboard (`uea serve`) and provides the command
// surface documented in AGENTS.md. Everything of substance lives in
// internal/cli; this is dispatch and nothing else.
package main

import (
	"os"

	"github.com/user/uea/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
