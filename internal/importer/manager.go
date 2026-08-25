package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/store"
)

// Manager runs imports in the background and reports on them.
//
// Jobs are persisted to SQLite as they run, so a page reload can reattach and a
// server restart leaves a record rather than a mystery. Progress in memory is
// throttled before it reaches the database — writing a row per message would
// make the import IO-bound on its own bookkeeping.
type Manager struct {
	dataDir string

	mu      sync.Mutex
	running map[string]*jobRun
}

type jobRun struct {
	cancel context.CancelFunc

	mu          sync.Mutex
	job         *store.ImportJob
	subscribers map[int]chan *store.ImportJob
	nextSub     int
}

// NewManager returns a manager writing blobs under dataDir.
func NewManager(dataDir string) *Manager {
	return &Manager{dataDir: dataDir, running: map[string]*jobRun{}}
}

// Request describes an import to start.
type Request struct {
	SourceID    string   `json:"sourceId"`
	MailboxIDs  []string `json:"mailboxIds"`
	AccountID   string   `json:"accountId"`
	Limit       int      `json:"limit,omitempty"`
	Since       string   `json:"since,omitempty"`
	Until       string   `json:"until,omitempty"`
	DryRun      bool     `json:"dryRun,omitempty"`
	Attachments bool     `json:"attachments,omitempty"`
	MaxAttachMB int64    `json:"maxAttachmentMb,omitempty"`
}

func (r Request) selection() (Selection, error) {
	sel := Selection{Limit: r.Limit}
	parse := func(s string) (time.Time, error) {
		if s == "" {
			return time.Time{}, nil
		}
		return time.Parse("2006-01-02", s)
	}
	var err error
	if sel.Since, err = parse(r.Since); err != nil {
		return sel, fmt.Errorf("since: expected YYYY-MM-DD, got %q", r.Since)
	}
	if sel.Until, err = parse(r.Until); err != nil {
		return sel, fmt.Errorf("until: expected YYYY-MM-DD, got %q", r.Until)
	}
	if !sel.Until.IsZero() {
		sel.Until = sel.Until.Add(24*time.Hour - time.Nanosecond)
	}
	return sel, nil
}

// Start validates a request, records the job and runs it in the background.
//
// Returns as soon as the job exists, which is what makes this usable from an
// HTTP handler: an import of forty thousand messages is not a request/response.
func (m *Manager) Start(src Source, req Request) (*store.ImportJob, error) {
	sel, err := req.selection()
	if err != nil {
		return nil, err
	}
	if req.AccountID == "" {
		return nil, fmt.Errorf("an account is required")
	}
	if len(req.MailboxIDs) == 0 {
		return nil, fmt.Errorf("at least one mailbox is required")
	}

	// Validate mailbox ids up front. An unknown id would otherwise walk nothing
	// and report a successful import of zero messages.
	known, err := src.Mailboxes(context.Background())
	if err != nil {
		return nil, err
	}
	valid := map[string]bool{}
	total := 0
	for _, b := range known {
		valid[b.ID] = true
	}
	for _, id := range req.MailboxIDs {
		if !valid[id] {
			return nil, fmt.Errorf("no mailbox %q in %s", id, src.Name())
		}
		for _, b := range known {
			if b.ID == id {
				total += b.Messages
			}
		}
	}
	if req.Limit > 0 && req.Limit < total {
		total = req.Limit
	}

	options, _ := json.Marshal(req)
	job := &store.ImportJob{
		ID:        uuid.New().String(),
		Source:    src.ID(),
		AccountID: req.AccountID,
		Mailboxes: req.MailboxIDs,
		Options:   options,
		Status:    store.JobPending,
		DryRun:    req.DryRun,
		Total:     &total,
		CreatedAt: time.Now(),
	}
	if err := store.SaveImportJob(job); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	run := &jobRun{cancel: cancel, job: job, subscribers: map[int]chan *store.ImportJob{}}

	m.mu.Lock()
	m.running[job.ID] = run
	m.mu.Unlock()

	go m.execute(ctx, src, sel, req, run)
	return job, nil
}

func (m *Manager) execute(ctx context.Context, src Source, sel Selection, req Request, run *jobRun) {
	defer func() {
		// A panic in a parser must not take the server down, and must not leave
		// the job apparently running forever either.
		if r := recover(); r != nil {
			run.finish(store.JobFailed, fmt.Sprintf("internal error: %v", r))
		}
		m.mu.Lock()
		delete(m.running, run.snapshot().ID)
		m.mu.Unlock()
		run.closeSubscribers()
	}()

	started := time.Now()
	run.update(func(j *store.ImportJob) {
		j.Status = store.JobRunning
		j.StartedAt = &started
	})

	opts := Options{
		JobID:              run.snapshot().ID,
		AccountID:          req.AccountID,
		Selection:          sel,
		DryRun:             req.DryRun,
		Attachments:        req.Attachments,
		MaxAttachmentBytes: req.MaxAttachMB << 20,
	}
	if req.Attachments {
		opts.Blobs = blobstore.New(m.dataDir)
	}

	// Progress is throttled: the engine reports per message, and persisting
	// each one would make the import IO-bound on its own progress rows.
	var lastPersist time.Time
	progress := ProgressFunc(func(p Progress) {
		if time.Since(lastPersist) < 300*time.Millisecond {
			return
		}
		lastPersist = time.Now()
		run.update(func(j *store.ImportJob) {
			j.Scanned = p.Current
			j.Current = p.Mailbox
		})
	})

	result, err := Run(ctx, src, req.MailboxIDs, opts, progress)

	if result != nil {
		run.update(func(j *store.ImportJob) {
			j.Scanned = result.Scanned
			j.Imported = result.Imported
			j.Duplicates = result.Duplicates
			j.Skipped = result.Skipped
			j.Failed = result.Failed
			j.Attachments = result.AttachmentsStored
			j.Bytes = result.Bytes
			j.Current = ""
		})
	}

	switch {
	case ctx.Err() != nil:
		// Cancellation keeps whatever was already imported: the messages are
		// in the database and deleting them would be a worse surprise than a
		// partial archive.
		run.finish(store.JobCancelled, "cancelled; messages imported before cancellation were kept")
	case err != nil:
		run.finish(store.JobFailed, err.Error())
	default:
		run.finish(store.JobDone, "")
	}
}

// Cancel stops a running job. Unknown or finished jobs are a no-op.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	run, ok := m.running[id]
	m.mu.Unlock()
	if !ok {
		return false
	}
	run.cancel()
	return true
}

// Subscribe returns a channel of job snapshots, and a function to stop
// listening. The channel closes when the job finishes.
func (m *Manager) Subscribe(id string) (<-chan *store.ImportJob, func()) {
	m.mu.Lock()
	run, ok := m.running[id]
	m.mu.Unlock()

	if !ok {
		// Already finished, or never ours. Deliver the persisted state once and
		// close, so a subscriber always gets an answer rather than hanging.
		ch := make(chan *store.ImportJob, 1)
		if job, err := store.GetImportJob(id); err == nil && job != nil {
			ch <- job
		}
		close(ch)
		return ch, func() {}
	}
	return run.subscribe()
}

// --- jobRun -----------------------------------------------------------------

func (r *jobRun) snapshot() *store.ImportJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *r.job
	return &clone
}

// update mutates the job, persists it and fans the new state out.
func (r *jobRun) update(mutate func(*store.ImportJob)) {
	r.mu.Lock()
	mutate(r.job)
	clone := *r.job
	subs := make([]chan *store.ImportJob, 0, len(r.subscribers))
	for _, ch := range r.subscribers {
		subs = append(subs, ch)
	}
	r.mu.Unlock()

	// Best effort: losing a progress row must not abort an import that is
	// otherwise working.
	_ = store.SaveImportJob(&clone)

	for _, ch := range subs {
		select {
		case ch <- &clone:
		default:
			// A slow subscriber gets the next update rather than blocking the
			// import. Progress is a stream of snapshots; missing one is fine.
		}
	}
}

func (r *jobRun) finish(status, message string) {
	finished := time.Now()
	r.update(func(j *store.ImportJob) {
		j.Status = status
		j.LastError = message
		j.FinishedAt = &finished
		j.Current = ""
	})
}

func (r *jobRun) subscribe() (<-chan *store.ImportJob, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := r.nextSub
	r.nextSub++
	ch := make(chan *store.ImportJob, 8)
	r.subscribers[id] = ch

	// Deliver current state immediately so a late subscriber is not staring at
	// nothing until the next progress tick.
	clone := *r.job
	ch <- &clone

	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if existing, ok := r.subscribers[id]; ok {
			delete(r.subscribers, id)
			close(existing)
		}
	}
}

func (r *jobRun) closeSubscribers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, ch := range r.subscribers {
		delete(r.subscribers, id)
		close(ch)
	}
}
