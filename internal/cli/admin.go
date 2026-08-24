package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/user/uea/internal/auth"
	"github.com/user/uea/internal/blobstore"
	"github.com/user/uea/internal/llm"
	"github.com/user/uea/internal/store"
	"github.com/user/uea/internal/vault"

	"context"
)

func init() {
	register(&Command{
		Name:    "user",
		Summary: "manage dashboard logins",
		Usage: `uea user <list|add|passwd> [flags]

  list              show accounts that can sign in
  add <username>    create a login
  passwd <username> set a new password

Passwords are read from UEA_NEW_PASSWORD, from stdin when piped, or prompted
for without echo.

` + "`uea user passwd`" + ` is the recovery path when the administrator password is
lost — without it the only remedy would be deleting the database.`,
		Run: runUser,
	})

	register(&Command{
		Name:    "vault",
		Summary: "inspect and rotate the credential encryption key",
		Usage: `uea vault <status|rotate>

  status   report on the key and whether every account decrypts
  rotate   generate a new key and re-encrypt every stored password

Rotate when the key may have been exposed — in a backup that went somewhere it
should not have, for instance. The old key is kept as vault.key.<timestamp>.bak
so a failed rotation is recoverable; delete it once the new key is verified.`,
		Run: runVault,
	})

	register(&Command{
		Name:    "llm",
		Summary: "configure the optional completion provider",
		Usage: `uea llm <status|configure|test|disable>

  status      show the current provider
  configure   set the provider, model, endpoint and API key
  test        send a trivial prompt and report the round trip
  disable     forget the provider; analyze and draft revert to emitting context

configure flags:
  --provider <ollama|openai>   openai covers any /v1/chat/completions endpoint
  --model <name>               e.g. llama3, gpt-4o-mini
  --endpoint <url>             defaults per provider
  --api-key                    read from UEA_LLM_API_KEY, stdin, or prompted

Configuring a remote provider means email content leaves this machine when
analyze or draft runs. Ollama against localhost keeps everything local.`,
		Run: runLLM,
	})

	register(&Command{
		Name:    "maintenance",
		Summary: "vacuum, analyze and check the database",
		Usage: `uea maintenance <vacuum|analyze|integrity|checkpoint>

  vacuum      rebuild the database, reclaiming freed space
  analyze     refresh query planner statistics
  integrity   run SQLite's integrity_check
  checkpoint  fold the write-ahead log back into the main file

Stop the server before vacuum: it needs exclusive access.`,
		Run: runMaintenance,
	})

	register(&Command{
		Name:    "backup",
		Summary: "write a consistent copy of the database",
		Usage: `uea backup [<path>] [--include-key]

Uses SQLite's online backup API, so it is safe to run while the server is up.
With no path, writes into <data>/backups/ with a timestamped name.

  --include-key           also copy vault.key beside the backup
  --include-attachments   also archive <data>/attachments/ beside the backup

Without the vault key a backup cannot decrypt any account password. Store the
key separately if the backup goes somewhere less trusted than this machine —
together they are equivalent to the plaintext passwords.

Attachment bytes live on disk, not in the database, so a plain backup does not
contain them. When attachments exist and are being left out, this says so
rather than letting you discover it at restore time.`,
		Run: runBackup,
	})

	register(&Command{
		Name:    "restore",
		Summary: "replace the database from a backup",
		Usage: `uea restore <path> [--yes]

Replaces <data>/uea.db with the given backup. The current database is renamed
to uea.db.<timestamp>.bak rather than deleted.

Stop the server first. If the backup was taken with a different vault.key, the
restored account passwords will not decrypt — restore that key too.`,
		Run: runRestore,
	})
}

// --- user -------------------------------------------------------------------

func runUser(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	switch sub {
	case "list", "":
		users, err := store.ListUsers()
		if err != nil {
			return Fail(ExitError, "failed to list users: %v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(users)
		}
		if len(users) == 0 {
			ctx.Printf("No logins exist. Create one with `uea user add <username>`.\n")
			return nil
		}
		ctx.Printf("%-32s %s\n", "USERNAME", "DISPLAY NAME")
		for _, u := range users {
			ctx.Printf("%-32s %s\n", u.Username, u.DisplayName)
		}
		return nil

	case "add", "passwd":
		if len(rest) != 1 {
			return Fail(ExitUsage, "usage: uea user %s <username>", sub)
		}
		username := rest[0]

		existing, err := store.GetUserByUsername(username)
		if err != nil {
			return Fail(ExitError, "failed to look up user: %v", err)
		}
		if sub == "add" && existing != nil {
			return Fail(ExitUsage, "user %s already exists; use `uea user passwd %s`", username, username)
		}
		if sub == "passwd" && existing == nil {
			return Fail(ExitNotFound, "no user named %s", username)
		}

		password, err := ctx.ReadSecret("UEA_NEW_PASSWORD", "new password")
		if err != nil {
			return err
		}
		if len(password) < 8 {
			return Fail(ExitUsage, "password must be at least 8 characters")
		}

		if err := auth.SetPassword(username, password); err != nil {
			return Fail(ExitError, "failed to set password: %v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]string{"username": username, "status": sub + "ed"})
		}
		if sub == "add" {
			ctx.Printf("Created login %s\n", username)
		} else {
			ctx.Printf("Password updated for %s\n", username)
		}
		return nil

	default:
		return Fail(ExitUsage, "unknown subcommand %q (want list, add or passwd)", sub)
	}
}

// --- vault ------------------------------------------------------------------

func runVault(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	keyPath := filepath.Join(ctx.DataDir, vault.KeyFileName)

	switch sub {
	case "status", "":
		accounts, err := store.ListAccounts()
		if err != nil {
			return Fail(ExitError, "failed to list accounts: %v", err)
		}
		readable, unreadable := 0, 0
		for _, a := range accounts {
			if a.Host != "" && a.Password == "" {
				unreadable++
			} else {
				readable++
			}
		}

		perm := "missing"
		if info, err := os.Stat(keyPath); err == nil {
			perm = fmt.Sprintf("%#o", info.Mode().Perm())
		}

		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{
				"keyPath":       keyPath,
				"keyMode":       perm,
				"accounts":      len(accounts),
				"decryptable":   readable,
				"undecryptable": unreadable,
			})
		}
		ctx.Printf("key          %s (mode %s)\n", keyPath, perm)
		ctx.Printf("accounts     %d\n", len(accounts))
		ctx.Printf("decryptable  %d\n", readable)
		if unreadable > 0 {
			ctx.Printf("UNREADABLE   %d — this key does not match those rows\n", unreadable)
			return &Error{Code: ExitError, Err: errString("some account passwords cannot be decrypted")}
		}
		return nil

	case "rotate":
		fs := flag.NewFlagSet("vault rotate", flag.ContinueOnError)
		fs.SetOutput(ctx.Stderr)
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		if err := parseArgs(fs, rest); err != nil {
			return Fail(ExitUsage, "invalid flags")
		}

		accounts, err := store.ListAccounts()
		if err != nil {
			return Fail(ExitError, "failed to list accounts: %v", err)
		}
		// Refuse to rotate while anything is unreadable: re-encrypting with a
		// new key would turn a recoverable problem into a permanent one.
		for _, a := range accounts {
			if a.Host != "" && a.Password == "" {
				return Fail(ExitError,
					"account %s cannot be decrypted with the current key; "+
						"rotating now would discard it permanently", a.ID)
			}
		}

		if !*yes && !ctx.Confirm(sprintf("Re-encrypt %d account password(s) under a new key?", len(accounts))) {
			return Fail(ExitError, "cancelled")
		}

		backupPath := keyPath + "." + time.Now().UTC().Format("20060102T150405Z") + ".bak"
		if err := os.Rename(keyPath, backupPath); err != nil {
			return Fail(ExitError, "cannot set the old key aside: %v", err)
		}

		// Passwords are already decrypted in memory; re-initialising the vault
		// against the now-absent key file mints a fresh one.
		if err := vault.Reload(ctx.DataDir); err != nil {
			os.Rename(backupPath, keyPath) // put things back
			return Fail(ExitError, "cannot create a new key: %v", err)
		}

		for _, a := range accounts {
			if err := store.SaveAccount(a); err != nil {
				return Fail(ExitError,
					"re-encryption failed at account %s: %v\n\nThe old key is at %s; restore it to recover.",
					a.ID, err, backupPath)
			}
		}

		// The LLM API key rides in the same vault and would otherwise be orphaned.
		if cfg, err := store.GetLLMConfig(); err == nil && cfg.HasAPIKey() {
			if err := store.SaveLLMConfig(cfg); err != nil {
				return Fail(ExitError, "re-encryption of the LLM API key failed: %v", err)
			}
		}

		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{
				"rotated": len(accounts), "previousKey": backupPath,
			})
		}
		ctx.Printf("Rotated the vault key and re-encrypted %d account(s).\n", len(accounts))
		ctx.Printf("Previous key kept at %s — delete it once you have confirmed things work.\n", backupPath)
		return nil

	default:
		return Fail(ExitUsage, "unknown subcommand %q (want status or rotate)", sub)
	}
}

// --- llm --------------------------------------------------------------------

func runLLM(ctx *Context, args []string) error {
	sub, rest := subcommand(args)
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	switch sub {
	case "status", "":
		cfg, err := store.GetLLMConfig()
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(cfg.Redacted())
		}
		if cfg.Provider == "" {
			ctx.Printf("No LLM provider configured.\n\n")
			ctx.Printf("analyze and draft still work: they emit structured JSON context\n")
			ctx.Printf("for an external agent instead of generating prose.\n\n")
			ctx.Printf("To enable generation:  uea llm configure --provider ollama --model llama3\n")
			return nil
		}
		ctx.Printf("provider   %s\n", cfg.Provider)
		ctx.Printf("model      %s\n", cfg.Model)
		ctx.Printf("endpoint   %s\n", orDefault(cfg.Endpoint, llm.DefaultEndpoints[cfg.Provider]))
		ctx.Printf("api key    %s\n", yesNo(cfg.HasAPIKey()))
		return nil

	case "configure":
		fs := flag.NewFlagSet("llm configure", flag.ContinueOnError)
		fs.SetOutput(ctx.Stderr)
		provider := fs.String("provider", "", "ollama or openai")
		model := fs.String("model", "", "model name")
		endpoint := fs.String("endpoint", "", "base URL")
		withKey := fs.Bool("api-key", false, "prompt for an API key")
		if err := parseArgs(fs, rest); err != nil {
			return Fail(ExitUsage, "invalid flags")
		}
		if *provider == "" || *model == "" {
			return Fail(ExitUsage, "--provider and --model are required")
		}
		valid := false
		for _, p := range llm.Supported {
			if *provider == p {
				valid = true
			}
		}
		if !valid {
			return Fail(ExitUsage, "unknown provider %q (supported: ollama, openai)", *provider)
		}

		cfg := store.LLMConfig{Provider: *provider, Model: *model, Endpoint: *endpoint}
		if *withKey || os.Getenv("UEA_LLM_API_KEY") != "" {
			key, err := ctx.ReadSecret("UEA_LLM_API_KEY", "API key")
			if err != nil {
				return err
			}
			cfg.APIKey = key
		}

		if err := store.SaveLLMConfig(cfg); err != nil {
			return Fail(ExitError, "failed to save configuration: %v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(cfg.Redacted())
		}
		ctx.Printf("Configured %s (%s).\n", cfg.Provider, cfg.Model)
		ctx.Printf("Check it reaches the model with: uea llm test\n")
		return nil

	case "test":
		cfg, err := store.GetLLMConfig()
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		provider, err := llm.New(cfg)
		if err != nil {
			return Fail(ExitNotConfigured, "%v", err)
		}

		timeout, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		start := time.Now()
		reply, err := provider.Complete(timeout,
			"You are a health check. Reply with exactly the word: ok",
			"Reply with exactly the word: ok")
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}

		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{
				"provider": provider.Name(), "elapsedMs": elapsed.Milliseconds(), "reply": reply,
			})
		}
		ctx.Printf("%s responded in %s: %s\n", provider.Name(), elapsed, truncate(reply, 80))
		return nil

	case "disable":
		if err := store.SaveLLMConfig(store.LLMConfig{}); err != nil {
			return Fail(ExitError, "failed to clear configuration: %v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]string{"status": "disabled"})
		}
		ctx.Printf("LLM provider disabled. analyze and draft will emit context instead of prose.\n")
		return nil

	default:
		return Fail(ExitUsage, "unknown subcommand %q (want status, configure, test or disable)", sub)
	}
}

// --- maintenance ------------------------------------------------------------

func runMaintenance(ctx *Context, args []string) error {
	sub, _ := subcommand(args)
	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	var action func() error
	switch sub {
	case "vacuum":
		action = store.Vacuum
	case "analyze":
		action = store.Analyze
	case "integrity":
		action = store.IntegrityCheck
	case "checkpoint":
		action = store.Checkpoint
	case "":
		return Fail(ExitUsage, "usage: uea maintenance <vacuum|analyze|integrity|checkpoint>")
	default:
		return Fail(ExitUsage, "unknown subcommand %q", sub)
	}

	start := time.Now()
	if err := action(); err != nil {
		return Fail(ExitError, "%s failed: %v", sub, err)
	}
	elapsed := time.Since(start).Round(time.Millisecond)

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"operation": sub, "elapsedMs": elapsed.Milliseconds()})
	}
	ctx.Printf("%s completed in %s\n", sub, elapsed)
	return nil
}

// --- backup / restore -------------------------------------------------------

func runBackup(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	includeKey := fs.Bool("include-key", false, "also copy vault.key beside the backup")
	includeAttachments := fs.Bool("include-attachments", false, "also archive the attachment blobs")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	if err := ctx.OpenStore(); err != nil {
		return err
	}
	defer store.CloseDB()

	dest := fs.Arg(0)
	if dest == "" {
		// The timestamp is only second-granular, so two backups a moment apart
		// would collide and the second would be refused. An auto-generated name
		// steps aside; an explicit path still refuses, because overwriting a
		// backup someone named is never what they meant.
		base := filepath.Join(ctx.DataDir, "backups",
			"uea-"+time.Now().UTC().Format("20060102T150405Z"))
		dest = base + ".db"
		for i := 2; ; i++ {
			if _, err := os.Stat(dest); os.IsNotExist(err) {
				break
			}
			dest = fmt.Sprintf("%s-%d.db", base, i)
		}
	}

	if err := store.BackupTo(dest); err != nil {
		return Fail(ExitError, "%v", err)
	}

	result := map[string]any{"database": dest}

	if *includeKey {
		keyDest := dest + ".vault.key"
		data, err := os.ReadFile(filepath.Join(ctx.DataDir, vault.KeyFileName))
		if err != nil {
			return Fail(ExitError, "database was backed up but the vault key could not be read: %v", err)
		}
		if err := os.WriteFile(keyDest, data, 0o600); err != nil {
			return Fail(ExitError, "database was backed up but the vault key could not be written: %v", err)
		}
		result["vaultKey"] = keyDest
	}

	// Attachment bytes are not in the database. Either archive them, or say
	// clearly that this backup is not self-sufficient.
	blobs := blobstore.New(ctx.DataDir)
	blobCount, blobBytes, _ := blobs.Usage()
	switch {
	case blobCount == 0:
		// Nothing to say.
	case *includeAttachments:
		archive := dest + ".attachments.tar"
		if err := tarDirectory(blobs.Root(), archive); err != nil {
			return Fail(ExitError, "database was backed up but the attachments could not be archived: %v", err)
		}
		result["attachments"] = archive
		result["attachmentCount"] = blobCount
	default:
		result["attachmentsOmitted"] = blobCount
	}

	if ctx.JSON {
		return ctx.EmitJSON(result)
	}
	ctx.Printf("Backed up to %s\n", dest)
	switch {
	case blobCount == 0:
	case *includeAttachments:
		ctx.Printf("Archived %d attachment blob(s), %s, to %s\n",
			blobCount, humanBytes(blobBytes), result["attachments"])
	default:
		ctx.Printf("\nWARNING: %d attachment blob(s), %s, are NOT in this backup.\n",
			blobCount, humanBytes(blobBytes))
		ctx.Printf("Attachment bytes live in %s, outside the database.\n", blobs.Root())
		ctx.Printf("Re-run with --include-attachments, or back that directory up separately.\n\n")
	}

	if *includeKey {
		ctx.Printf("Vault key copied to %s — this pair is equivalent to the plaintext passwords.\n", result["vaultKey"])
	} else {
		ctx.Printf("Vault key NOT included: account passwords in this backup cannot be\n")
		ctx.Printf("decrypted without %s. Re-run with --include-key, or store the key separately.\n", vault.KeyFileName)
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
		return Fail(ExitUsage, "usage: uea restore <path> [--yes]")
	}
	source := fs.Arg(0)

	if _, err := os.Stat(source); err != nil {
		return Fail(ExitNotFound, "cannot read %s: %v", source, err)
	}

	target := ctx.dbPath()
	if !*yes && !ctx.Confirm(sprintf("Replace %s with %s?", target, source)) {
		return Fail(ExitError, "cancelled")
	}

	// Move the live database aside rather than overwriting it: if the backup
	// turns out to be the wrong one, the only copy of current state should not
	// already be gone.
	var movedTo string
	if _, err := os.Stat(target); err == nil {
		movedTo = target + "." + time.Now().UTC().Format("20060102T150405Z") + ".bak"
		if err := os.Rename(target, movedTo); err != nil {
			return Fail(ExitError, "cannot set the current database aside: %v", err)
		}
		// WAL sidecars belong to the old database and would corrupt the
		// restored one if left in place.
		os.Remove(target + "-wal")
		os.Remove(target + "-shm")
	}

	data, err := os.ReadFile(source)
	if err != nil {
		return Fail(ExitError, "cannot read %s: %v", source, err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return Fail(ExitError, "cannot write %s: %v", target, err)
	}

	if ctx.JSON {
		return ctx.EmitJSON(map[string]any{"restored": target, "previous": movedTo})
	}
	ctx.Printf("Restored %s from %s\n", target, source)
	if movedTo != "" {
		ctx.Printf("Previous database kept at %s\n", movedTo)
	}
	ctx.Printf("\nRun `uea doctor` to confirm the vault key still matches.\n")
	return nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func yesNo(b bool) string {
	if b {
		return "stored"
	}
	return "none"
}
