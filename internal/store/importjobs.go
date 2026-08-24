package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Import job statuses.
const (
	JobPending   = "pending"
	JobRunning   = "running"
	JobDone      = "done"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
	// JobInterrupted marks a job whose process died mid-run. Distinct from
	// failed: nothing went wrong with the import itself, and the record exists
	// so a restart does not leave a job apparently running forever.
	JobInterrupted = "interrupted"
)

// ImportJob is one import, persisted so it survives a restart.
type ImportJob struct {
	ID        string   `json:"id"`
	Source    string   `json:"source"`
	AccountID string   `json:"accountId"`
	Mailboxes []string `json:"mailboxes"`
	// Options is the request that created the job, kept verbatim so the UI can
	// show what was asked for and a future resume knows the terms.
	Options json.RawMessage `json:"options,omitempty"`

	Status string `json:"status"`
	DryRun bool   `json:"dryRun"`

	Total       *int  `json:"total,omitempty"`
	Scanned     int   `json:"scanned"`
	Imported    int   `json:"imported"`
	Duplicates  int   `json:"duplicates"`
	Skipped     int   `json:"skipped"`
	Failed      int   `json:"failed"`
	Attachments int   `json:"attachments"`
	Bytes       int64 `json:"bytes"`

	// Current is the mailbox or message being worked on, for a progress line.
	Current   string `json:"current,omitempty"`
	LastError string `json:"lastError,omitempty"`

	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// Terminal reports whether the job has stopped for good.
func (j *ImportJob) Terminal() bool {
	switch j.Status {
	case JobDone, JobFailed, JobCancelled, JobInterrupted:
		return true
	}
	return false
}

// SaveImportJob inserts or updates a job.
func SaveImportJob(j *ImportJob) error {
	mailboxes, _ := json.Marshal(j.Mailboxes)
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now()
	}
	options := "{}"
	if len(j.Options) > 0 {
		options = string(j.Options)
	}

	_, err := db.Exec(`
		INSERT INTO import_jobs (id, source, account_id, mailboxes, options, status,
		                         dry_run, total, scanned, imported, duplicates, skipped,
		                         failed, attachments, bytes, current, last_error,
		                         created_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status      = EXCLUDED.status,
			total       = EXCLUDED.total,
			scanned     = EXCLUDED.scanned,
			imported    = EXCLUDED.imported,
			duplicates  = EXCLUDED.duplicates,
			skipped     = EXCLUDED.skipped,
			failed      = EXCLUDED.failed,
			attachments = EXCLUDED.attachments,
			bytes       = EXCLUDED.bytes,
			current     = EXCLUDED.current,
			last_error  = EXCLUDED.last_error,
			started_at  = EXCLUDED.started_at,
			finished_at = EXCLUDED.finished_at;
	`, j.ID, j.Source, j.AccountID, string(mailboxes), options, j.Status,
		j.DryRun, j.Total, j.Scanned, j.Imported, j.Duplicates, j.Skipped,
		j.Failed, j.Attachments, j.Bytes, nullIfEmpty(j.Current), nullIfEmpty(j.LastError),
		j.CreatedAt.UnixMilli(), millisOrNil(j.StartedAt), millisOrNil(j.FinishedAt))
	return err
}

const jobColumns = `id, source, account_id, mailboxes, options, status, dry_run,
	total, scanned, imported, duplicates, skipped, failed, attachments, bytes,
	current, last_error, created_at, started_at, finished_at`

func scanImportJob(scan func(...any) error) (*ImportJob, error) {
	j := &ImportJob{}
	var mailboxes, options string
	var total sql.NullInt64
	var current, lastError sql.NullString
	var created int64
	var started, finished sql.NullInt64

	if err := scan(&j.ID, &j.Source, &j.AccountID, &mailboxes, &options, &j.Status,
		&j.DryRun, &total, &j.Scanned, &j.Imported, &j.Duplicates, &j.Skipped,
		&j.Failed, &j.Attachments, &j.Bytes, &current, &lastError,
		&created, &started, &finished); err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(mailboxes), &j.Mailboxes)
	j.Options = json.RawMessage(options)
	if total.Valid {
		n := int(total.Int64)
		j.Total = &n
	}
	j.Current = current.String
	j.LastError = lastError.String
	j.CreatedAt = time.UnixMilli(created)
	if started.Valid {
		t := time.UnixMilli(started.Int64)
		j.StartedAt = &t
	}
	if finished.Valid {
		t := time.UnixMilli(finished.Int64)
		j.FinishedAt = &t
	}
	return j, nil
}

// GetImportJob returns a job, or (nil, nil) when it does not exist.
func GetImportJob(id string) (*ImportJob, error) {
	row := db.QueryRow("SELECT "+jobColumns+" FROM import_jobs WHERE id = ?", id)
	j, err := scanImportJob(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

// ListImportJobs returns jobs newest first.
func ListImportJobs(limit int) ([]*ImportJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query("SELECT "+jobColumns+" FROM import_jobs ORDER BY created_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ImportJob
	for rows.Next() {
		j, err := scanImportJob(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
