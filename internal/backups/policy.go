package backups

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Schedules.
const (
	ScheduleOff    = "off"
	ScheduleDaily  = "daily"
	ScheduleWeekly = "weekly"
)

// Policy is which backups to keep, and how often to make one.
//
// Kept in the backups folder rather than the database, so it can be read
// without opening the mailbox and survives restoring an older one.
type Policy struct {
	// Keep is how many of each kind to keep; yours (manual) are never pruned.
	KeepUpdate    int `json:"keepUpdate"`
	KeepScheduled int `json:"keepScheduled"`
	KeepRestore   int `json:"keepRestore"`
	// Schedule is off, daily or weekly. Off by default: each backup is a
	// full copy of the mailbox, and that is a decision about disk space.
	Schedule string `json:"schedule"`
}

// DefaultPolicy keeps two of each: the newest, and one before it in case the
// newest is the one that was wrong.
var DefaultPolicy = Policy{KeepUpdate: 2, KeepScheduled: 2, KeepRestore: 2, Schedule: ScheduleOff}

func policyPath(dataDir string) string { return filepath.Join(Dir(dataDir), "policy.json") }

// LoadPolicy reads the policy, or the default when none was saved.
func LoadPolicy(dataDir string) Policy {
	p := DefaultPolicy
	if b, err := os.ReadFile(policyPath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	return p.normal()
}

func (p Policy) normal() Policy {
	clamp := func(n int) int {
		if n < 1 {
			return 1
		}
		if n > 100 {
			return 100
		}
		return n
	}
	p.KeepUpdate, p.KeepScheduled, p.KeepRestore = clamp(p.KeepUpdate), clamp(p.KeepScheduled), clamp(p.KeepRestore)
	switch p.Schedule {
	case ScheduleDaily, ScheduleWeekly:
	default:
		p.Schedule = ScheduleOff
	}
	return p
}

// SavePolicy writes the policy. Keeping fewer than one of a kind is refused:
// the newest backup of a kind is the one you would restore.
func SavePolicy(dataDir string, p Policy) error {
	if p.KeepUpdate < 1 || p.KeepScheduled < 1 || p.KeepRestore < 1 {
		return fmt.Errorf("keep at least one of each kind")
	}
	switch p.Schedule {
	case ScheduleOff, ScheduleDaily, ScheduleWeekly:
	default:
		return fmt.Errorf("schedule is off, daily or weekly, not %q", p.Schedule)
	}
	if err := os.MkdirAll(Dir(dataDir), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(p.normal(), "", "  ")
	return os.WriteFile(policyPath(dataDir), b, 0o600)
}

// keep is how many of a kind the policy keeps; 0 means every one.
func (p Policy) keep(reason string) int {
	switch reason {
	case ReasonUpdate:
		return p.KeepUpdate
	case ReasonScheduled:
		return p.KeepScheduled
	case ReasonRestore:
		return p.KeepRestore
	}
	return 0
}

// Interval is how long the schedule waits between backups; 0 when off.
func (p Policy) Interval() time.Duration {
	switch p.Schedule {
	case ScheduleDaily:
		return 24 * time.Hour
	case ScheduleWeekly:
		return 7 * 24 * time.Hour
	}
	return 0
}

// Due reports whether the schedule wants a backup now: when the newest backup
// of any kind is older than the interval. A backup you made yesterday is as
// good as a scheduled one, so it counts.
func Due(dataDir string, now time.Time) bool {
	iv := LoadPolicy(dataDir).Interval()
	if iv == 0 {
		return false
	}
	all, err := List(dataDir)
	if err != nil {
		return false
	}
	if len(all) == 0 {
		return true
	}
	return now.Sub(all[0].At) >= iv
}

// excess is what the policy would remove from a list (newest first), keeping
// keepLess fewer of each kind than it says — 1 to make room for a new one of
// that kind before it exists. The newest backup overall is never in it, nor
// is any of yours.
func excess(all []Backup, p Policy, reason string, keepLess int) []Backup {
	seen := map[string]int{}
	var out []Backup
	for i, b := range all {
		n := p.keep(b.Reason)
		if n == 0 {
			continue
		}
		if b.Reason == reason {
			n -= keepLess
		}
		seen[b.Reason]++
		if seen[b.Reason] > n && i > 0 {
			out = append(out, b)
		}
	}
	return out
}

// Prune removes what the policy does not keep, and returns it. dryRun says
// what would go and removes nothing.
func Prune(dataDir string, dryRun bool) ([]Backup, error) {
	all, err := List(dataDir)
	if err != nil {
		return nil, err
	}
	gone := excess(all, LoadPolicy(dataDir), "", 0)
	if dryRun {
		return gone, nil
	}
	for _, b := range gone {
		if err := Delete(b); err != nil {
			return nil, err
		}
	}
	return gone, nil
}
