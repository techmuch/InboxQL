package maintenance

import (
	"context"
	"fmt"

	"github.com/user/inboxql/internal/backups"
)

// runBackup makes a backup, reporting the copy's progress.
func runBackup(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error) {
	o := opts.Backup
	if o.Reason == "" {
		o.Reason = backups.ReasonManual
	}
	made, err := backups.Make(backups.Options{
		DataDir: dataDir, Reason: o.Reason, Version: o.Version, Key: o.Key, Attachments: o.Attachments,
		// In kilobytes: in megabytes a small mailbox reads "0 of 0".
		Progress: func(done, total int64, stage string) {
			run.progress(int(done>>10), int(total>>10), stage)
		},
	})
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("Backed up to %s (%s), checked", made.Backup.Name, backups.Human(made.Backup.Bytes))
	if n := len(made.MadeRoom) + len(made.Pruned); n > 0 {
		msg += fmt.Sprintf("; removed %d older backup(s) the keep rules allow", n)
	}
	if made.AttachmentsLeftOut > 0 {
		msg += fmt.Sprintf("; %d attachment file(s) not included", made.AttachmentsLeftOut)
	}
	return msg, nil
}

// runBackupCheck reads one backup back.
func runBackupCheck(ctx context.Context, dataDir string, opts Options, run *jobRun) (string, error) {
	b, err := backups.Find(dataDir, opts.Backup.Name)
	if err != nil {
		return "", err
	}
	run.progress(0, 1, b.Name)
	schema, err := backups.Check(b.Path)
	if err != nil {
		return "", fmt.Errorf("%s failed its check: %w", b.Name, err)
	}
	return fmt.Sprintf("%s is whole (schema v%d)", b.Name, schema), nil
}
