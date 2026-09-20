package annotate

import (
	"context"
	"fmt"
	"time"

	"github.com/user/inboxql/internal/store"
)

// # Triggers: when to drain a queue that already exists
//
// Scope is the "what" and already works. A trigger is only the "when", and a
// trigger that also filtered would be a second system for the same job, free
// to disagree with the first.
//
// # Not a hook
//
// At fifteen to twenty seconds a message, a trigger cannot run inline: on an
// import of ten thousand messages that is days, inside whatever called it. It
// has to enqueue.
//
// And the queue is already there. [store.PendingMessages] computes, for any
// annotator, exactly which messages it has not evaluated at its current
// version — the three-valued status is what makes that possible. So a trigger
// is a policy for when to drain it, which is a much smaller thing than it
// sounds.
//
// # The runaway job is the risk
//
// Triggers plus an expensive engine is how a laptop spends a night at 100%.
// Every triggered run is capped, reports as it goes, and stops when asked.

// TriggeredLimit caps one triggered pass.
//
// A number rather than "everything" because nobody is watching: a trigger that
// found forty thousand pending messages and started on all of them would be
// indistinguishable from a hang. What is left stays pending and the next
// trigger picks it up, so the work still finishes — in visible instalments.
const TriggeredLimit = 200

// Due lists the annotators a trigger should run now.
//
// `when` is the trigger that fired. An annotator is due if it asked for that
// trigger and has something pending; one with nothing pending is skipped
// rather than started, because a job that evaluates nothing is noise in every
// log and progress bar it touches.
func Due(when string) ([]*store.Annotator, error) {
	if !store.ValidTrigger(when) {
		return nil, fmt.Errorf("unknown trigger %q", when)
	}

	list, err := store.ListAnnotators()
	if err != nil {
		return nil, err
	}

	var due []*store.Annotator
	for _, a := range list {
		if a.Trigger != when {
			continue
		}
		p, err := store.Progress(a, scopeFor(a, ""))
		if err != nil {
			// A scope that no longer compiles — a label it referenced was
			// deleted, say. Skipped rather than failing the whole sweep,
			// because one broken annotator should not stop the others.
			continue
		}
		if p.Total-p.Evaluated > 0 {
			due = append(due, a)
		}
	}
	return due, nil
}

// Swept is what one trigger pass did.
type Swept struct {
	Trigger   string     `json:"trigger"`
	Ran       []*Outcome `json:"ran"`
	Skipped   []string   `json:"skipped,omitempty"`
	Failed    []string   `json:"failed,omitempty"`
	Remaining int64      `json:"remaining"`
	Duration  string     `json:"duration"`
}

// Sweep runs every annotator due for a trigger, in order, capped.
//
// Sequential on purpose. Two span runs at once would contend for the same CPU
// and finish no sooner, and the progress of one job at a time is something a
// person can read.
func Sweep(ctx context.Context, when, dataDir string, progress func(name string, done, total int64)) (*Swept, error) {
	started := time.Now()
	out := &Swept{Trigger: when}

	due, err := Due(when)
	if err != nil {
		return nil, err
	}

	for _, a := range due {
		if err := ctx.Err(); err != nil {
			// Cancelled. What is done is done and what is not stays pending,
			// which is the same state an interrupted manual run leaves.
			break
		}

		outcome, err := Run(ctx, a.Name, Options{
			Limit:   TriggeredLimit,
			DataDir: dataDir,
			Progress: func(done, total int64) {
				if progress != nil {
					progress(a.Name, done, total)
				}
			},
		})
		if err != nil {
			// Recorded, not fatal. An annotator whose model is missing should
			// not stop the rule labels behind it in the queue.
			out.Failed = append(out.Failed, fmt.Sprintf("%s: %v", a.Name, err))
			continue
		}
		out.Ran = append(out.Ran, outcome)
	}

	// What is still waiting, so a caller can say whether another pass is
	// worth making rather than guessing.
	for _, a := range due {
		if p, err := store.Progress(a, scopeFor(a, "")); err == nil {
			if left := p.Total - p.Evaluated; left > 0 {
				out.Remaining += left
			}
		}
	}

	out.Duration = time.Since(started).Round(time.Millisecond).String()
	return out, nil
}
