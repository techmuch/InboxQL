package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/user/inboxql/internal/backups"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/vault"
)

func init() {
	register(&Command{
		Name:    "backup",
		Aliases: []string{"backups"},
		Summary: "back up the mailbox, and manage its backups",
		Usage: `iql backup [<path>] [--include-key] [--include-attachments] [--no-check]
iql backup list                    every backup: when, why, size, checked
iql backup check <backup>          read one back and say whether it is whole
iql backup delete <backup>         remove one, with what is beside it
iql backup prune [--dry-run]       remove what the keep rules do not keep
iql backup policy [--keep-update n] [--keep-scheduled n] [--keep-restore n]
                  [--schedule off|daily|weekly]

Uses SQLite's online backup API, so it is safe while the server is up. With no
path, it writes into <data>/backups/, reads the copy back to check it, and then
removes what the keep rules do not keep. A backup is named by its file name,
or the start of it.

Room first: a backup needs about the database's size free. When the disk has
less, the backups the keep rules would remove anyway are removed first; when
even that is not enough, nothing is written and the numbers are reported.

Keep rules (the defaults): 2 backups taken before updates, 2 scheduled, 2
databases set aside by restores. Yours — made with iql backup — are kept until
you delete them. Scheduled backups are off unless --schedule says otherwise.

  --include-key           also copy vault.key beside the backup
  --include-attachments   also archive <data>/attachments/ beside the backup

Without the vault key a backup cannot decrypt any account password. Store the
key separately if the backup goes somewhere less trusted than this machine —
together they are equivalent to the plaintext passwords. Attachment bytes live
outside the database, so a plain backup does not hold them.`,
		Run: runBackup,
	})

	register(&Command{
		Name:    "restore",
		Summary: "replace the database from a backup",
		Usage: `iql restore <backup> [--yes]

<backup> is a name from iql backup list (or the start of one), or a path.
The current database is set aside as a backup of its own, not deleted.

The server must be stopped: it has the database open. To restore while it
runs, use Settings → Data Management, which restarts it to do this. A backup
made by a newer InboxQL is refused; an older one is upgraded when opened. If
the backup was taken with a different vault.key, restore that key too.`,
		Run: runRestore,
	})
}

func runBackup(ctx *Context, args []string) error {
	switch sub, rest := subcommand(args); sub {
	case "list", "ls":
		return backupList(ctx)
	case "check", "verify":
		return backupCheck(ctx, rest)
	case "delete", "rm", "remove":
		return backupDelete(ctx, rest)
	case "prune":
		return backupPrune(ctx, rest)
	case "policy", "keep", "schedule":
		return backupPolicy(ctx, rest)
	}

	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	includeKey := fs.Bool("include-key", false, "also copy vault.key beside the backup")
	includeAttachments := fs.Bool("include-attachments", false, "also archive the attachment files")
	noCheck := fs.Bool("no-check", false, "do not read the backup back")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	made, err := backups.Make(backups.Options{
		DataDir: ctx.DataDir, Reason: backups.ReasonManual, Version: Version, Dest: fs.Arg(0),
		Key: *includeKey, Attachments: *includeAttachments, NoCheck: *noCheck,
	})
	var room *backups.NoRoom
	if errors.As(err, &room) {
		return Fail(ExitError, "%v\n\nSee what is taking room: iql backup list", err)
	}
	if err != nil && made == nil {
		return Fail(ExitError, "%v", err)
	}

	if ctx.JSON {
		out := map[string]any{"backup": made.Backup, "madeRoom": made.MadeRoom, "pruned": made.Pruned,
			"database": made.Backup.Path}
		if made.AttachmentsLeftOut > 0 {
			out["attachmentsOmitted"] = made.AttachmentsLeftOut
		}
		if err != nil {
			out["error"] = err.Error()
			ctx.EmitJSON(out)
			return Fail(ExitError, "%v", err)
		}
		return ctx.EmitJSON(out)
	}
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	p := ctx.Printer()
	ctx.Printf("Backed up to %s (%s)", made.Backup.Path, backups.Human(made.Backup.Bytes))
	if made.Backup.Checked == "ok" {
		ctx.Printf(", checked")
	}
	ctx.Printf("\n")
	for _, b := range made.MadeRoom {
		ctx.Printf("  %s %s, to make room\n", p.Dim("removed"), b.Name)
	}
	for _, b := range made.Pruned {
		ctx.Printf("  %s %s (keep rules)\n", p.Dim("removed"), b.Name)
	}
	if made.AttachmentsLeftOut > 0 {
		ctx.Printf("\nWARNING: %s are NOT in this backup; they live outside the database.\n",
			count(made.AttachmentsLeftOut, "attachment file", "attachment files"))
		ctx.Printf("Re-run with --include-attachments, or back up %s separately.\n",
			filepath.Join(ctx.DataDir, "attachments"))
	}
	if made.Backup.Key {
		ctx.Printf("Vault key copied beside it — this pair is equivalent to the plaintext passwords.\n")
	} else {
		ctx.Printf("Vault key NOT included: account passwords in this backup cannot be\n")
		ctx.Printf("decrypted without %s. Re-run with --include-key, or store the key separately.\n", vault.KeyFileName)
	}
	return nil
}

func backupList(ctx *Context) error {
	if ctx.DataDir == "" {
		return Fail(ExitNotConfigured, "no mailbox")
	}
	all, err := backups.List(ctx.DataDir)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	free, _ := backups.FreeBytes(ctx.DataDir)
	policy := backups.LoadPolicy(ctx.DataDir)
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"backups": all, "totalBytes": backups.Total(all),
			"freeBytes": free, "databaseBytes": backups.DatabaseBytes(ctx.DataDir),
			"needBytes": backups.Need(ctx.DataDir, false), "policy": policy,
			"staged": backups.Staged(ctx.DataDir)})
	}
	p := ctx.Printer()
	if len(all) == 0 {
		ctx.Printf("No backups yet. Make one: iql backup\n")
	} else {
		t := p.NewTable("BACKUP", "WHEN", "WHY", "SIZE", "WITH", "CHECKED")
		for _, b := range all {
			with := []string{}
			if b.Key {
				with = append(with, "key")
			}
			if b.Attachments {
				with = append(with, "attachments")
			}
			checked := b.Checked
			if checked == "" {
				checked = "not yet"
			}
			why := b.Reason
			if b.Version != "" && b.Reason == backups.ReasonUpdate {
				why += " to " + b.Version
			}
			t.Row(b.Name, b.At.Format("2006-01-02 15:04"), why, backups.Human(b.Bytes+b.Extra),
				strings.Join(with, ","), checked)
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}
	ctx.Printf("\n%s in backups · %s free · a backup needs about %s\n",
		backups.Human(backups.Total(all)), backups.Human(free), backups.Human(backups.Need(ctx.DataDir, false)))
	ctx.Printf("%s\n", p.Dim(fmt.Sprintf("Keeping %d update, %d scheduled, %d set aside by restores, and all of yours. Schedule: %s.",
		policy.KeepUpdate, policy.KeepScheduled, policy.KeepRestore, policy.Schedule)))
	return nil
}

func backupCheck(ctx *Context, args []string) error {
	name, _ := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which backup? iql backup list shows them")
	}
	b, err := backups.Find(ctx.DataDir, name)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	schema, err := backups.Check(b.Path)
	if ctx.JSON {
		out := map[string]any{"backup": b.Name, "ok": err == nil, "schema": schema}
		if err != nil {
			out["error"] = err.Error()
		}
		ctx.EmitJSON(out)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		return nil
	}
	if err != nil {
		return Fail(ExitError, "%s failed its check: %v", b.Name, err)
	}
	ctx.Printf("%s is whole (schema v%d).\n", b.Name, schema)
	return nil
}

func backupDelete(ctx *Context, args []string) error {
	name, _ := subcommand(args)
	if name == "" {
		return Fail(ExitUsage, "which backup? iql backup list shows them")
	}
	b, err := backups.Find(ctx.DataDir, name)
	if err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if err := backups.Delete(*b); err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"deleted": b.Name, "freed": b.Bytes + b.Extra})
	}
	ctx.Printf("Deleted %s, %s freed.\n", b.Name, backups.Human(b.Bytes+b.Extra))
	return nil
}

func backupPrune(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("backup prune", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	dry := fs.Bool("dry-run", false, "say what would go, and remove nothing")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	gone, err := backups.Prune(ctx.DataDir, *dry)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"removed": gone, "freed": backups.Total(gone), "dryRun": *dry})
	}
	if len(gone) == 0 {
		ctx.Printf("Nothing to remove: every backup is one the keep rules keep.\n")
		return nil
	}
	verb := "Removed"
	if *dry {
		verb = "Would remove"
	}
	for _, b := range gone {
		ctx.Printf("%s %s (%s, %s)\n", verb, b.Name, b.Reason, backups.Human(b.Bytes+b.Extra))
	}
	ctx.Printf("%s in all.\n", backups.Human(backups.Total(gone)))
	return nil
}

func backupPolicy(ctx *Context, args []string) error {
	p := backups.LoadPolicy(ctx.DataDir)
	fs := flag.NewFlagSet("backup policy", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	ku := fs.String("keep-update", "", "backups taken before updates to keep")
	ks := fs.String("keep-scheduled", "", "scheduled backups to keep")
	kr := fs.String("keep-restore", "", "databases set aside by restores to keep")
	schedule := fs.String("schedule", "", "off, daily or weekly")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	changed := false
	for _, f := range []struct {
		v   string
		dst *int
	}{{*ku, &p.KeepUpdate}, {*ks, &p.KeepScheduled}, {*kr, &p.KeepRestore}} {
		if f.v == "" {
			continue
		}
		n, err := strconv.Atoi(f.v)
		if err != nil {
			return Fail(ExitUsage, "%q is not a number", f.v)
		}
		*f.dst, changed = n, true
	}
	if *schedule != "" {
		p.Schedule, changed = *schedule, true
	}
	if changed {
		if err := backups.SavePolicy(ctx.DataDir, p); err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		p = backups.LoadPolicy(ctx.DataDir)
	}
	if ctx.JSON {
		return ctx.EmitJSON(p)
	}
	ctx.Printf("Keeping %d update, %d scheduled and %d set-aside backups, and all of yours.\nSchedule: %s.\n",
		p.KeepUpdate, p.KeepScheduled, p.KeepRestore, p.Schedule)
	if p.Schedule != backups.ScheduleOff {
		ctx.Printf("Scheduled backups are made by the running server, and wait while on battery.\n")
	}
	return nil
}

func runRestore(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}
	if fs.NArg() != 1 {
		return Fail(ExitUsage, "usage: iql restore <backup> [--yes]")
	}
	source := fs.Arg(0)
	if b, err := backups.Find(ctx.DataDir, source); err == nil {
		source = b.Path
	} else if _, statErr := os.Stat(source); statErr != nil {
		return Fail(ExitNotFound, "%v", err)
	}

	// The server has the database open; replacing it underneath would leave
	// it writing to a file that is no longer the mailbox.
	if info, held := readServerLock(ctx.DataDir); held {
		where := ""
		if info != nil && info.URL != "" {
			where = " at " + info.URL
		}
		return Fail(ExitError, "a server is running this mailbox%s.\n\nRestore from Settings → Data Management, which restarts it to do this,\nor stop it first: iql service stop", where)
	}

	if _, err := backups.CanRestore(source); err != nil {
		return Fail(ExitError, "%v", err)
	}
	if !*yes && !ctx.Confirm(sprintf("Replace the mailbox in %s with %s?", ctx.DataDir, filepath.Base(source))) {
		return Fail(ExitError, "cancelled")
	}
	out, err := backups.RestoreNow(ctx.DataDir, source, Version)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if ctx.JSON {
		return ctx.EmitJSON(out)
	}
	ctx.Printf("Restored from %s.\n", filepath.Base(source))
	if out.SetAside != "" {
		ctx.Printf("The database it replaced is kept as %s.\n", filepath.Base(out.SetAside))
	}
	if out.Upgrade {
		ctx.Printf("It is from an older InboxQL, and is upgraded when next opened.\n")
	}
	ctx.Printf("\nRun `iql doctor` to confirm the vault key still matches.\n")
	return nil
}
