// Package maintenance runs the long operations that prepare a mailbox.
//
// # Why these are jobs and not request handlers
//
// Recovering attachments, reading what is inside them, running OCR over the
// scans and embedding the result are all measured in minutes: the OCR pass over
// five scanned files took about ten on a local model. An HTTP handler that does
// that work inline is not a slow endpoint, it is a broken one — the browser
// gives up, the user retries, and now two passes are writing to the same rows.
//
// So they are jobs, with the same shape as an import: start, watch, cancel. The
// pattern is deliberately the importer's rather than a new one, because a
// second way of running background work is a second place for progress
// reporting and cancellation to be subtly wrong.
//
// # Why they are never run automatically
//
// Every one of them is expensive and none is required for the app to work. A
// mailbox with unextracted attachments is a mailbox that does not show
// attachments — which is a gap, not a fault, and one the owner should close
// deliberately. OCR additionally sends pages to a model, which is a decision
// nobody should discover having already been made for them.
package maintenance

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/user/inboxql/internal/annotate"
	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/health"
	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/store"
)

// Job is one run of a maintenance operation.
type Job struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Status is queued, running, done, failed or cancelled.
	Status string `json:"status"`
	// Current is what it is working on now, for a progress line.
	Current string `json:"current,omitempty"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	// Summary is the human-readable outcome, written when the job finishes.
	Summary   string     `json:"summary,omitempty"`
	LastError string     `json:"lastError,omitempty"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

// Running reports whether the job is still going.
func (j *Job) Running() bool { return j.Status == StatusQueued || j.Status == StatusRunning }

// Job statuses.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Manager owns the running jobs.
//
// History is deliberately in memory. A maintenance run is something you watch
// and then forget; persisting it would mean a schema, a retention policy and a
// migration, for a list nobody reads twice. What matters — what was extracted,
// what was read — is already in the database the job wrote to, and `doctor`
// reports it.
type Manager struct {
	dataDir string

	mu      sync.Mutex
	jobs    map[string]*jobRun
	order   []string
	running map[string]string // kind -> job id

	// override replaces a worker, for tests.
	//
	// The lifecycle here — progress, cancellation, one-at-a-time, recovering
	// from a panic — is the part worth testing, and it is the part that has
	// nothing to do with what the jobs actually do. Testing it through the real
	// workers would mean a database, a model and ten minutes per case.
	override map[string]worker
}

type jobRun struct {
	cancel context.CancelFunc

	mu          sync.Mutex
	job         *Job
	subscribers map[int]chan *Job
	nextSub     int
}

// NewManager returns a manager working under dataDir.
func NewManager(dataDir string) *Manager {
	return &Manager{
		dataDir: dataDir,
		jobs:    map[string]*jobRun{},
		running: map[string]string{},
	}
}

// ErrAlreadyRunning says this kind of job is already in flight.
//
// One at a time per kind, because two OCR passes over the same files would
// both read the same rows, send the same pages to the model twice and race to
// write the answer.
type ErrAlreadyRunning struct {
	Kind  string
	JobID string
}

func (e *ErrAlreadyRunning) Error() string {
	return fmt.Sprintf("%s is already running", e.Kind)
}

// Options are the knobs a caller may set for a run.
type Options struct {
	// AllowRemote consents to sending mail content to a model that is not on
	// this machine. Required for OCR and embedding against a hosted endpoint;
	// meaningless for the rest.
	AllowRemote bool
	// Limit caps how many items a run will process, for trying one first.
	Limit int
	// Redo re-does work already done once, for when an extractor has improved.
	Redo bool
	// Profile names the model profile to embed with. Empty means the default,
	// which is usually a chat model and so usually wrong — see embedProfile.
	Profile string
}

// Start begins a job, unless one of that kind is already running.
func (m *Manager) Start(kind string, opts Options) (*Job, error) {
	work, err := m.workerFor(kind)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if id, busy := m.running[kind]; busy {
		m.mu.Unlock()
		return nil, &ErrAlreadyRunning{Kind: kind, JobID: id}
	}

	id := fmt.Sprintf("%s-%d", kind, time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	run := &jobRun{
		cancel:      cancel,
		job:         &Job{ID: id, Kind: kind, Status: StatusQueued, StartedAt: time.Now()},
		subscribers: map[int]chan *Job{},
	}
	m.jobs[id] = run
	m.order = append(m.order, id)
	m.running[kind] = id
	m.mu.Unlock()

	go func() {
		defer func() {
			// A panic in a worker must not take the server with it: this runs
			// in its own goroutine, so nothing above would catch it.
			if r := recover(); r != nil {
				run.finish(StatusFailed, fmt.Sprintf("internal error: %v", r))
			}
			m.mu.Lock()
			delete(m.running, kind)
			m.mu.Unlock()
			run.closeSubscribers()
		}()

		run.update(func(j *Job) { j.Status = StatusRunning })
		summary, err := work(ctx, m.dataDir, opts, run)

		switch {
		case ctx.Err() != nil:
			run.finish(StatusCancelled, "")
		case err != nil:
			run.finish(StatusFailed, err.Error())
		default:
			run.update(func(j *Job) { j.Summary = summary })
			run.finish(StatusDone, "")
		}
	}()

	return run.snapshot(), nil
}

// Cancel stops a running job. Reports whether there was one to stop.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	run, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok || !run.snapshot().Running() {
		return false
	}
	run.cancel()
	return true
}

// Get returns one job.
func (m *Manager) Get(id string) *Job {
	m.mu.Lock()
	run, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return run.snapshot()
}

// List returns every job this process has run, newest first.
func (m *Manager) List() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]*Job, 0, len(m.order))
	for i := len(m.order) - 1; i >= 0; i-- {
		if run, ok := m.jobs[m.order[i]]; ok {
			out = append(out, run.snapshot())
		}
	}
	return out
}

// Subscribe streams snapshots of a job until it finishes.
func (m *Manager) Subscribe(id string) (<-chan *Job, func()) {
	m.mu.Lock()
	run, ok := m.jobs[id]
	m.mu.Unlock()

	if !ok {
		// Unknown id. Deliver nothing and close, so a subscriber always gets an
		// answer rather than hanging.
		ch := make(chan *Job)
		close(ch)
		return ch, func() {}
	}
	if !run.snapshot().Running() {
		ch := make(chan *Job, 1)
		ch <- run.snapshot()
		close(ch)
		return ch, func() {}
	}
	return run.subscribe()
}

// --- jobRun -----------------------------------------------------------------

func (r *jobRun) snapshot() *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *r.job
	return &clone
}

func (r *jobRun) update(mutate func(*Job)) {
	r.mu.Lock()
	mutate(r.job)
	clone := *r.job
	subs := make([]chan *Job, 0, len(r.subscribers))
	for _, ch := range r.subscribers {
		subs = append(subs, ch)
	}
	r.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- &clone:
		default:
			// A slow subscriber gets the next update rather than blocking the
			// work. Progress is a stream of snapshots; missing one is fine.
		}
	}
}

// progress is what a worker calls as it goes.
func (r *jobRun) progress(done, total int, current string) {
	r.update(func(j *Job) {
		j.Done, j.Total, j.Current = done, total, current
	})
}

func (r *jobRun) finish(status, message string) {
	ended := time.Now()
	r.update(func(j *Job) {
		j.Status = status
		j.LastError = message
		j.EndedAt = &ended
		j.Current = ""
	})
}

func (r *jobRun) subscribe() (<-chan *Job, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := r.nextSub
	r.nextSub++
	ch := make(chan *Job, 8)
	r.subscribers[id] = ch
	// The current state first, so a subscriber that arrives mid-job renders
	// something immediately rather than waiting for the next tick.
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

// --- workers ----------------------------------------------------------------

type worker func(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error)

func (m *Manager) workerFor(kind string) (worker, error) {
	if w, ok := m.override[kind]; ok {
		return w, nil
	}
	switch kind {
	case health.JobAttachments:
		return runAttachments, nil
	case health.JobText:
		return runText, nil
	case health.JobOCR:
		return runOCR, nil
	case health.JobEmbed:
		return runEmbed, nil
	case health.JobGLiNER:
		return runGLiNERInstall, nil
	case health.JobReindex:
		return runReindex, nil
	}
	return nil, fmt.Errorf("unknown maintenance job %q", kind)
}

func runAttachments(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error) {
	out, err := store.RecoverAttachments(blobstore.New(dataDir), 0, false)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Recovered %d attachment(s) from %d message(s), %.1f MB.",
		out.Recovered, out.Messages, float64(out.Bytes)/(1024*1024)), nil
}

func runText(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error) {
	out, err := store.ExtractAttachmentText(blobstore.New(dataDir), opts.Redo,
		func(done, total int64) {
			run.progress(int(done), int(total), "")
		})
	if err != nil {
		return "", err
	}
	summary := fmt.Sprintf("%d file(s) readable, %d with no text layer", out.WithText, out.NoText)
	if out.NoReader > 0 {
		summary += fmt.Sprintf(", %d with no reader", out.NoReader)
	}
	return summary + ".", nil
}

// runOCR reads the scans, having checked that sending them is wanted.
//
// The consent check is here rather than in the handler because it is a
// property of the work, not of the transport: whatever calls this is about to
// upload the contents of somebody's scanned correspondence, and finding that
// out afterwards is not an option.
func runOCR(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error) {
	cfg, err := store.GetLLMConfig()
	if err != nil {
		return "", err
	}
	provider, err := llm.New(cfg)
	if err != nil {
		return "", fmt.Errorf("no model is configured to read images: %w", err)
	}
	if !llm.CanSee(provider) {
		return "", fmt.Errorf("%s cannot be shown images; configure a vision-capable model", provider.Name())
	}
	if IsRemote(cfg.Endpoint, cfg.Provider) && !opts.AllowRemote {
		return "", fmt.Errorf(
			"%s is not on this machine, so every scanned page would be uploaded to it — "+
				"say so explicitly to go ahead", cfg.Endpoint)
	}

	out, err := store.OCRAttachments(ctx, blobstore.New(dataDir), &visionReader{provider}, 1600, opts.Limit,
		func(done, total int, filename string) {
			run.progress(done, total, filename)
		})
	if err != nil {
		return "", err
	}

	summary := fmt.Sprintf("Read %d file(s)", out.Read)
	if out.Illegible > 0 {
		summary += fmt.Sprintf(", %d with nothing legible", out.Illegible)
	}
	if out.NoImages > 0 {
		summary += fmt.Sprintf(", %d in a page format this cannot extract", out.NoImages)
	}
	return summary + ". OCR text is marked as such: a model transcribing a page can also invent.", nil
}

func runEmbed(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error) {
	profile := opts.Profile
	if profile == "" {
		found, err := embedProfile()
		if err != nil {
			return "", err
		}
		profile = found
	}

	var out *annotate.EmbedOutcome
	var err error
	if opts.AllowRemote {
		out, err = annotate.EmbedAttachmentsRemote(ctx, profile, opts.Limit)
	} else {
		out, err = annotate.EmbedAttachments(ctx, profile, opts.Limit, false)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Embedded %d file(s) with %s.", out.Embedded, out.Model), nil
}

// embedProfile finds the profile to embed with when none was named.
//
// # Why this is not just "the default profile"
//
// The default is whatever `iql llm configure` set, which is a chat model — and
// a chat model cannot produce an embedding. Passing it through produced
// `profile "default" is for chat, not embedding`, which is a correct error and
// a useless one to meet by clicking a button: the person clicking it did not
// choose a profile and has no obvious way to.
//
// So the only embedding profile is used when there is exactly one. Two is
// ambiguous and none is unconfigured, and both say so rather than guessing.
func embedProfile() (string, error) {
	profiles, err := store.ListLLMProfiles()
	if err != nil {
		return "", err
	}

	var candidates []string
	for _, p := range profiles {
		if p.Purpose == store.PurposeEmbedding {
			candidates = append(candidates, p.Name)
		}
	}

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf(
			"no embedding profile is configured; add one with " +
				"`iql llm profile add <name> --purpose embedding --model <embedding model>`")
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf(
			"several embedding profiles are configured (%s); say which to use",
			strings.Join(candidates, ", "))
	}
}

func runReindex(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error) {
	if err := store.RebuildFullTextIndex(); err != nil {
		return "", err
	}
	if err := store.ReindexGraph(); err != nil {
		return "", err
	}
	return "Rebuilt the full-text index and the participant and reference tables.", nil
}

// visionReader adapts a provider to what the OCR pass needs.
type visionReader struct{ provider llm.Provider }

func (v *visionReader) Name() string { return v.provider.Name() }

func (v *visionReader) ReadImage(ctx context.Context, mime string, data []byte) (string, error) {
	return llm.Describe(ctx, v.provider, store.OCRSystem, store.OCRPrompt,
		[]llm.Image{{MIME: mime, Data: data}})
}

// IsRemote reports whether a model endpoint is off this machine.
//
// Wrong in the safe direction: an endpoint this does not recognise counts as
// remote, so consent is asked for when it is not needed rather than skipped
// when it is.
func IsRemote(endpoint, provider string) bool {
	if endpoint == "" {
		endpoint = llm.DefaultEndpoints[provider]
	}
	e := strings.ToLower(endpoint)
	for _, local := range []string{"localhost", "127.0.0.1", "[::1]", "0.0.0.0"} {
		if strings.Contains(e, local) {
			return false
		}
	}
	return true
}
