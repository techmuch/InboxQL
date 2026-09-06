//go:build e2e && !sqlite_fts5

package e2e

// binaryTags is empty here: the suite was built without FTS5, so the binary it
// builds is too, and the fallback path is what gets exercised. Both are real
// configurations and both are run in CI.
const binaryTags = ""
