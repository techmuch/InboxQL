// Package inboxql holds what has to be embedded from the top of the
// repository: the user documentation in docs/, and AGENTS.md, which is both
// the agent contract and one of the guides.
//
// It lives here because go:embed cannot reach a parent directory, and
// AGENTS.md belongs at the top of the repository where agents look for it.
package inboxql

import "embed"

//go:embed docs/*.md AGENTS.md
var Docs embed.FS
