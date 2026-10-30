// Package inboxql holds what has to be embedded from the top of the
// repository: the user documentation in docs/ with its screenshots,
// AGENTS.md, which is both the agent contract and one of the guides, and
// VERSION, the one place the release version is written.
//
// It lives here because go:embed cannot reach a parent directory, and
// AGENTS.md belongs at the top of the repository where agents look for it.
package inboxql

import "embed"

//go:embed docs/*.md docs/images/*.webp AGENTS.md
var Docs embed.FS

// VersionFile is VERSION as committed: the release this source is, named
// year.month.day.n. A release build stamps the same value from its tag, and
// the frontend's build reads the file too, so the two halves agree.
//
//go:embed VERSION
var VersionFile string
