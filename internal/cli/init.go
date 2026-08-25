package cli

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"io"
	"os"
	"path/filepath"

	"github.com/user/inboxql/internal/auth"
	"github.com/user/inboxql/internal/store"
	"github.com/user/inboxql/internal/vault"
)

func init() {
	register(&Command{
		Name:    "init",
		Summary: "prepare a directory for InboxQL (database, vault key, admin user)",
		Usage: `iql init [--data <dir>] [--admin-user <email>] [--force]

Prepares a directory to hold everything InboxQL owns:

  <dir>/inboxql.db      SQLite database, migrated to the current schema
  <dir>/vault.key   AES-256 key for account passwords (mode 0600)
  <dir>/backups/    default destination for ` + "`iql backup`" + `

Also creates the administrator account. The password comes from
INBOXQL_ADMIN_PASSWORD if set, otherwise one is generated and printed once —
it is not recoverable afterwards, though ` + "`iql user passwd`" + ` can set a new one.

Safe to re-run: existing files are kept and only what is missing is created.
--force is required only to reinitialise a directory that already has a
database, and even then the database itself is never deleted.

Flags:
  --admin-user <email>   administrator username (default admin@inboxql.local)
  --force                proceed even if the directory is already initialised`,
		Run: runInit,
	})
}

func runInit(ctx *Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	adminUser := fs.String("admin-user", envOr("INBOXQL_ADMIN_USER", "admin@inboxql.local"), "administrator username")
	force := fs.Bool("force", false, "proceed even if already initialised")
	if err := parseArgs(fs, args); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	type result struct {
		DataDir      string `json:"dataDir"`
		Database     string `json:"database"`
		VaultKey     string `json:"vaultKey"`
		AdminUser    string `json:"adminUser"`
		AdminPass    string `json:"adminPassword,omitempty"`
		SchemaVer    int    `json:"schemaVersion"`
		AlreadySetUp bool   `json:"alreadyInitialised"`
	}
	out := result{DataDir: ctx.DataDir, AdminUser: *adminUser}

	dbExisted := false
	if _, err := os.Stat(ctx.dbPath()); err == nil {
		dbExisted = true
		if !*force {
			return Fail(ExitUsage,
				"%s already contains an InboxQL database\n\nRe-run with --force to fill in anything missing (the database is never deleted).",
				ctx.DataDir)
		}
	}
	out.AlreadySetUp = dbExisted

	for _, dir := range []string{ctx.DataDir, filepath.Join(ctx.DataDir, "backups")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Fail(ExitError, "cannot create %s: %v", dir, err)
		}
	}

	// InitDB runs migrations and initialises the vault, creating the key if it
	// is absent — so this single call covers both files.
	if _, err := store.InitDB(ctx.DataDir); err != nil {
		return Fail(ExitError, "failed to initialise database: %v", err)
	}
	out.Database = ctx.dbPath()
	out.VaultKey = filepath.Join(ctx.DataDir, vault.KeyFileName)
	out.SchemaVer = store.SchemaVersion

	// Only mint a password when the account does not already exist, so
	// re-running init never silently rotates a working credential.
	existing, err := store.GetUserByUsername(*adminUser)
	if err != nil {
		return Fail(ExitError, "failed to check for existing user: %v", err)
	}
	if existing == nil {
		password := os.Getenv("INBOXQL_ADMIN_PASSWORD")
		generated := password == ""
		if generated {
			password, err = generatePassword()
			if err != nil {
				return Fail(ExitError, "failed to generate a password: %v", err)
			}
		}
		if err := auth.CreateInitialUser(*adminUser, password); err != nil {
			return Fail(ExitError, "failed to create administrator: %v", err)
		}
		if generated {
			out.AdminPass = password
		}
	}

	if ctx.JSON {
		return ctx.EmitJSON(out)
	}

	ctx.Printf("Initialised InboxQL in %s\n\n", out.DataDir)
	ctx.Printf("  database    %s (schema v%d)\n", out.Database, out.SchemaVer)
	ctx.Printf("  vault key   %s\n", out.VaultKey)
	ctx.Printf("  backups     %s\n\n", filepath.Join(out.DataDir, "backups"))

	if out.AdminPass != "" {
		ctx.Printf("Administrator account created:\n\n")
		ctx.Printf("  username    %s\n", out.AdminUser)
		ctx.Printf("  password    %s\n\n", out.AdminPass)
		ctx.Printf("This password is shown once and is not stored in recoverable form.\n")
		ctx.Printf("Save it now, or set a new one later with `iql user passwd %s`.\n\n", out.AdminUser)
	} else if existing != nil {
		ctx.Printf("Administrator %s already exists; left unchanged.\n\n", out.AdminUser)
	} else {
		ctx.Printf("Administrator %s created with the password from INBOXQL_ADMIN_PASSWORD.\n\n", out.AdminUser)
	}

	ctx.Printf("Back up %s together with the database — without it,\n", vault.KeyFileName)
	ctx.Printf("stored account passwords cannot be decrypted.\n\n")
	ctx.Printf("Next:\n")
	ctx.Printf("  iql account add --data %s    connect a mailbox\n", out.DataDir)
	ctx.Printf("  iql doctor      --data %s    check everything is healthy\n", out.DataDir)
	ctx.Printf("  iql serve       --data %s    start the web interface\n", out.DataDir)
	return nil
}

// generatePassword returns a URL-safe random password with ~120 bits of
// entropy — long enough that it never needs a strength policy, short enough to
// copy by hand once.
func generatePassword() (string, error) {
	b := make([]byte, 15)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// readAll is io.ReadAll, kept here so prompt.go does not need the import.
func readAll(r io.Reader) ([]byte, error) { return io.ReadAll(r) }
