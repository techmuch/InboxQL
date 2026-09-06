//go:build e2e && sqlite_fts5

package e2e

// binaryTags are passed to the `go build` that produces the binary under test.
//
// The suite must compile iql the same way CI ships it. Without this the test
// binary carried the FTS5 tag while the binary it exercised did not, so every
// e2e run silently tested the substring fallback and never the index.
const binaryTags = "sqlite_fts5"
