package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/user/inboxql/internal/health"
)

// waitFor polls until the condition holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// withWorker swaps in a worker for one job kind, so the lifecycle can be tested
// without a database or a model behind it.
func withWorker(t *testing.T, m *Manager, kind string, w worker) {
	t.Helper()
	m.override = map[string]worker{kind: w}
}

func TestJobRunsAndReportsItsSummary(t *testing.T) {
	m := NewManager(t.TempDir())
	withWorker(t, m, health.JobAttachments, func(ctx context.Context, dir string, o Options, run *jobRun) (string, error) {
		run.progress(1, 2, "one")
		run.progress(2, 2, "two")
		return "did the thing", nil
	})

	job, err := m.Start(health.JobAttachments, Options{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitFor(t, "the job to finish", func() bool { return !m.Get(job.ID).Running() })

	done := m.Get(job.ID)
	if done.Status != StatusDone {
		t.Errorf("status %q, want done (%s)", done.Status, done.LastError)
	}
	if done.Summary != "did the thing" {
		t.Errorf("summary %q", done.Summary)
	}
	if done.Done != 2 || done.Total != 2 {
		t.Errorf("progress %d/%d, want 2/2", done.Done, done.Total)
	}
}

func TestFailingJobKeepsTheReason(t *testing.T) {
	m := NewManager(t.TempDir())
	withWorker(t, m, health.JobText, func(ctx context.Context, dir string, o Options, run *jobRun) (string, error) {
		return "", errors.New("the disk is on fire")
	})

	job, _ := m.Start(health.JobText, Options{})
	waitFor(t, "failure", func() bool { return !m.Get(job.ID).Running() })

	got := m.Get(job.ID)
	if got.Status != StatusFailed {
		t.Errorf("status %q, want failed", got.Status)
	}
	if got.LastError != "the disk is on fire" {
		t.Errorf("lastError %q, want the worker's reason", got.LastError)
	}
}

// A panic in a worker runs on its own goroutine, so nothing above would catch
// it and the whole server would go down with it.
func TestPanickingJobDoesNotTakeTheProcessDown(t *testing.T) {
	m := NewManager(t.TempDir())
	withWorker(t, m, health.JobText, func(ctx context.Context, dir string, o Options, run *jobRun) (string, error) {
		panic("something impossible")
	})

	job, _ := m.Start(health.JobText, Options{})
	waitFor(t, "the panic to be recorded", func() bool { return !m.Get(job.ID).Running() })

	if got := m.Get(job.ID); got.Status != StatusFailed {
		t.Errorf("status %q, want failed", got.Status)
	}
}

// Two passes of the same kind would read the same rows, send the same pages to
// a model twice and race to write the answer.
func TestOneOfEachKindAtATime(t *testing.T) {
	m := NewManager(t.TempDir())
	release := make(chan struct{})
	withWorker(t, m, health.JobOCR, func(ctx context.Context, dir string, o Options, run *jobRun) (string, error) {
		<-release
		return "", nil
	})

	first, err := m.Start(health.JobOCR, Options{})
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	waitFor(t, "the first job to start", func() bool { return m.Get(first.ID).Status == StatusRunning })

	_, err = m.Start(health.JobOCR, Options{})
	var busy *ErrAlreadyRunning
	if !errors.As(err, &busy) {
		t.Fatalf("second Start returned %v, want ErrAlreadyRunning", err)
	}
	// The id is part of the error so a caller can watch the run instead.
	if busy.JobID != first.ID {
		t.Errorf("error names job %q, want %q", busy.JobID, first.ID)
	}

	close(release)
	waitFor(t, "the first job to end", func() bool { return !m.Get(first.ID).Running() })

	// And the kind is free again once it has.
	if _, err := m.Start(health.JobOCR, Options{}); err != nil {
		t.Errorf("third Start after completion: %v", err)
	}
}

func TestCancel(t *testing.T) {
	m := NewManager(t.TempDir())
	withWorker(t, m, health.JobOCR, func(ctx context.Context, dir string, o Options, run *jobRun) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	job, _ := m.Start(health.JobOCR, Options{})
	waitFor(t, "start", func() bool { return m.Get(job.ID).Status == StatusRunning })

	if !m.Cancel(job.ID) {
		t.Fatal("Cancel reported nothing to cancel")
	}
	waitFor(t, "cancellation", func() bool { return !m.Get(job.ID).Running() })

	if got := m.Get(job.ID); got.Status != StatusCancelled {
		t.Errorf("status %q, want cancelled", got.Status)
	}
	// Cancelling a finished job is not an error, it is a no-op.
	if m.Cancel(job.ID) {
		t.Error("Cancel on a finished job reported success")
	}
}

func TestUnknownKindIsRejected(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.Start("polish-the-database", Options{}); err == nil {
		t.Error("Start accepted a job kind that does not exist")
	}
}

// A subscriber that arrives mid-job renders something immediately rather than
// waiting for the next tick, and the channel closes when the job ends.
func TestSubscribeDeliversStateAndCloses(t *testing.T) {
	m := NewManager(t.TempDir())
	release := make(chan struct{})
	withWorker(t, m, health.JobText, func(ctx context.Context, dir string, o Options, run *jobRun) (string, error) {
		run.progress(1, 3, "working")
		<-release
		return "finished", nil
	})

	job, _ := m.Start(health.JobText, Options{})
	waitFor(t, "start", func() bool { return m.Get(job.ID).Status == StatusRunning })

	updates, stop := m.Subscribe(job.ID)
	defer stop()

	first, open := <-updates
	if !open || first == nil {
		t.Fatal("subscribing delivered nothing")
	}

	close(release)
	// Drain until closed; the last state seen must be terminal.
	var last = first
	for u := range updates {
		last = u
	}
	if last.Running() {
		t.Errorf("stream closed while the job was still %q", last.Status)
	}
}

// Subscribing to a job that has already finished must answer rather than hang.
func TestSubscribeToFinishedJob(t *testing.T) {
	m := NewManager(t.TempDir())
	withWorker(t, m, health.JobReindex, func(ctx context.Context, dir string, o Options, run *jobRun) (string, error) {
		return "done", nil
	})

	job, _ := m.Start(health.JobReindex, Options{})
	waitFor(t, "completion", func() bool { return !m.Get(job.ID).Running() })

	updates, stop := m.Subscribe(job.ID)
	defer stop()

	select {
	case got, open := <-updates:
		if !open || got.Status != StatusDone {
			t.Errorf("got %+v, open=%v", got, open)
		}
	case <-time.After(time.Second):
		t.Fatal("subscribing to a finished job hung")
	}
}

func TestSubscribeToUnknownJob(t *testing.T) {
	m := NewManager(t.TempDir())
	updates, stop := m.Subscribe("no-such-job")
	defer stop()

	select {
	case _, open := <-updates:
		if open {
			t.Error("an unknown job delivered a snapshot")
		}
	case <-time.After(time.Second):
		t.Fatal("subscribing to an unknown job hung")
	}
}

func TestIsRemote(t *testing.T) {
	local := []string{
		"http://localhost:11434", "http://127.0.0.1:28100/v1",
		"http://[::1]:8080", "HTTP://LOCALHOST:1234",
	}
	for _, e := range local {
		if IsRemote(e, "ollama") {
			t.Errorf("IsRemote(%q) = true, want false", e)
		}
	}

	remote := []string{
		"https://api.openai.com/v1", "http://192.168.1.50:11434",
		"https://models.example.com",
	}
	for _, e := range remote {
		if !IsRemote(e, "openai") {
			t.Errorf("IsRemote(%q) = false, want true", e)
		}
	}

	// An endpoint nobody set falls back to the provider's default, and an
	// unrecognised one counts as remote — wrong in the direction that asks for
	// consent it did not need rather than skipping consent it did.
	if IsRemote("", "ollama") {
		t.Error("the ollama default should be local")
	}
	if !IsRemote("", "openai") {
		t.Error("the openai default should be remote")
	}
}
