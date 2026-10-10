package cli

import (
	"os"
	"testing"
)

// TestMain points the machine settings at an empty folder for the whole
// package.
//
// Without it, a developer who has installed InboxQL — and so has a
// ~/.iql/settings.json — would run every test in this package against their
// real mailbox wherever a test omits --data. That is the worst thing a test can
// do, and it would only happen on the machines of the people most likely to run
// the tests.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "iql-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("INBOXQL_HOME", home)
	os.Unsetenv("INBOXQL_DATA")
	os.Unsetenv("INBOXQL_ADDR")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
