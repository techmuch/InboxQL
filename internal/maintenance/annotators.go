package maintenance

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/annotate"
	"github.com/user/inboxql/internal/store"
)

// runAnnotators drains the queue for every annotator asking for a trigger.
//
// A job rather than a hook, for the reason the plan gives: at fifteen to
// twenty seconds a message a trigger cannot run inline, and this is already
// the machinery for work that takes minutes — start, watch, cancel, one at a
// time per kind.
//
// One at a time matters more here than elsewhere. Two span runs at once
// contend for the same CPU and finish no sooner, and a single progress bar is
// something a person can read.
func runAnnotators(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error) {
	when := opts.Profile // reused to carry the trigger; the UI leaves it empty
	if when == "" {
		when = store.TriggerAfterSync
	}
	if !store.ValidTrigger(when) {
		return "", fmt.Errorf("unknown trigger %q", when)
	}

	out, err := annotate.Sweep(ctx, when, dataDir, func(name string, done, total int64) {
		run.progress(int(done), int(total), name)
	})
	if err != nil {
		return "", err
	}

	if len(out.Ran) == 0 && len(out.Failed) == 0 {
		return "nothing was waiting", nil
	}

	var evaluated, records int64
	names := make([]string, 0, len(out.Ran))
	for _, o := range out.Ran {
		evaluated += o.Evaluated
		records += o.Records
		names = append(names, o.Annotator)
	}

	msg := fmt.Sprintf("%s over %d message(s), %d record(s)",
		strings.Join(names, ", "), evaluated, records)
	// Said plainly: a capped pass leaves work behind on purpose, and a caller
	// who does not know that will think it failed.
	if out.Remaining > 0 {
		msg += fmt.Sprintf("; %d still pending", out.Remaining)
	}
	if len(out.Failed) > 0 {
		msg += "; failed: " + strings.Join(out.Failed, "; ")
	}
	return msg, nil
}
