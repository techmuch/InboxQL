package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/user/inboxql/internal/store"
)

// Dynamic completions.
//
// Cobra gives command and subcommand names for free. The values worth
// completing beyond that are the ones nobody can remember and everybody has to
// go and look up: account ids, mailbox folder names, and the ids of import
// sources. Each resolves against the real database, so the suggestions are the
// user's own data rather than a hardcoded list.
//
// A completion function must never fail loudly or write to stdout — the shell
// is reading that. Every one of these degrades to "no suggestions" on error.

// completeAccountIDs suggests configured account ids.
func completeAccountIDs(ctx *Context) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		ctx.resolve()
		if err := ctx.OpenStore(); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		defer store.CloseDB()

		accounts, err := store.ListAccounts()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, a := range accounts {
			if strings.HasPrefix(a.ID, toComplete) {
				// The description after \t is shown by zsh and fish.
				out = append(out, a.ID+"\t"+a.Email)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeFolders suggests the mailbox views search understands.
func completeFolders(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var out []string
	for _, f := range append([]string{store.FolderAll}, store.Folders...) {
		if strings.HasPrefix(f, toComplete) {
			out = append(out, f)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// registerCompletions attaches value completion to the commands where it earns
// its keep.
//
// These commands parse their own flags, so cobra cannot complete flag values
// directly. What it can do is complete positional arguments and offer the
// subcommand names, which covers the common shapes:
//
//	iql account sync <TAB>     -> the account ids
//	iql import scan <TAB>      -> the source ids
func registerCompletions(ctx *Context, root *cobra.Command) {
	byName := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		byName[c.Name()] = c
	}

	if c := byName["account"]; c != nil {
		c.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// First word is the subcommand; after `sync`, `verify` or
			// `remove` the next word is an account id.
			done := positional(ctx, args)
			if len(done) == 1 {
				switch done[0] {
				case "sync", "verify", "remove", "rm":
					return completeAccountIDs(ctx)(cmd, args, toComplete)
				}
			}
			if len(done) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return matching(subcommands["account"], toComplete)
		}
	}

	if c := byName["search"]; c != nil {
		c.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		_ = c.RegisterFlagCompletionFunc("folder", completeFolders)
	}

	for name, subs := range subcommands {
		c := byName[name]
		if c == nil || c.ValidArgsFunction != nil {
			continue
		}
		subs := subs
		c.ValidArgsFunction = func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(positional(ctx, args)) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return matching(subs, toComplete)
		}
	}
}

// positional strips global flags out of the arguments cobra hands a
// completion function, leaving the positional words.
//
// Necessary because these commands set DisableFlagParsing, and cobra only
// removes flags from the argument list for commands where it does the parsing
// itself. Without this, completing `iql --data ./data account sync <TAB>` saw
// three arguments already typed and offered nothing. Applying the globals to
// ctx on the way past is deliberate: it is what lets account-id completion
// read the database the user actually named.
func positional(ctx *Context, args []string) []string {
	rest := splitGlobals(ctx, args)
	out := rest[:0]
	for _, a := range rest {
		if a == "--" || strings.HasPrefix(a, "-") {
			continue
		}
		out = append(out, a)
	}
	return out
}

func matching(candidates []string, prefix string) ([]string, cobra.ShellCompDirective) {
	var out []string
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
