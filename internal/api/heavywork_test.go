package api

import (
	"testing"

	"github.com/user/inboxql/internal/machine"
)

// The machine settings can let heavy work run on battery; when they do, the
// power source is not even asked.
func TestHeavyWorkOnBatteryCanBeAllowed(t *testing.T) {
	t.Setenv("INBOXQL_HOME", t.TempDir())
	if err := machine.Save(&machine.Settings{DataDir: t.TempDir(), HeavyWorkOnBattery: true}); err != nil {
		t.Fatal(err)
	}
	if deferHeavyWork() {
		t.Error("heavy work was deferred although the settings allow it on battery")
	}
}
