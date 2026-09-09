package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/store"
)

// runLLMProfile manages the named model profiles.
//
// A profile is the whole address of a model — provider, endpoint, credential —
// because a bare model name is not one. `--model gpt-4o` against a machine
// configured for a local runtime used to mean "ask the local runtime for
// gpt-4o", which fails; naming a profile moves the endpoint and key with it.
func runLLMProfile(ctx *Context, args []string) error {
	sub, rest := subcommand(args)

	switch sub {
	case "list", "":
		profiles, err := store.ListLLMProfiles()
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			out := make([]map[string]any, 0, len(profiles))
			for _, p := range profiles {
				out = append(out, p.Redacted())
			}
			return ctx.EmitJSON(out)
		}
		if len(profiles) == 0 {
			ctx.Printf("No model profiles configured.\n\n")
			ctx.Printf("  iql llm profile add local --provider ollama --model llama3\n")
			return nil
		}

		p := ctx.Printer()
		t := p.NewTable("", "NAME", "PROVIDER", "MODEL", "FOR", "SCOPE", "KEY")
		for _, prof := range profiles {
			marker := ""
			if prof.IsDefault {
				marker = "*"
			}
			// Scope is the column that matters most here: it is the one that
			// says whether using this profile sends mail off the machine.
			scope := prof.Scope()
			if prof.IsRemote() {
				scope = p.Yellow(scope)
			}
			purpose := prof.Purpose
			if prof.Purpose == store.PurposeEmbedding && prof.Dimensions > 0 {
				purpose = fmt.Sprintf("embed/%d", prof.Dimensions)
			}
			t.Row(marker, prof.Name, prof.Provider,
				ui.Truncate(prof.Model, 32), purpose, scope, yesNo(prof.HasAPIKey()))
		}
		if err := t.Flush(); err != nil {
			return err
		}
		ctx.Printf("\n%s\n", p.Dim("* is the default, used by anything that does not name a profile."))
		return nil

	case "add", "set":
		name, flags := subcommand(rest)
		if name == "" {
			return Fail(ExitUsage, "give the profile a name, e.g. `iql llm profile add local ...`")
		}
		fs := flag.NewFlagSet("llm profile add", flag.ContinueOnError)
		fs.SetOutput(ctx.Stderr)
		provider := fs.String("provider", "", "ollama, openai or swama")
		model := fs.String("model", "", "model name")
		endpoint := fs.String("endpoint", "", "base URL; defaults per provider")
		withKey := fs.Bool("api-key", false, "prompt for an API key")
		makeDefault := fs.Bool("default", false, "use this profile when nothing names one")
		purpose := fs.String("purpose", "", "chat or embedding")
		if err := parseArgs(fs, flags); err != nil {
			return Fail(ExitUsage, "invalid flags")
		}

		existing, err := store.GetLLMProfile(name)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		prof := existing
		if prof == nil {
			prof = &store.LLMProfile{Name: name}
			if *provider == "" || *model == "" {
				return Fail(ExitUsage, "a new profile needs --provider and --model")
			}
		}
		// Editing sets only what was given, so `--default` alone does not
		// blank the provider.
		if *provider != "" {
			if !supportedProvider(*provider) {
				return Fail(ExitUsage, "unknown provider %q (supported: %s)",
					*provider, strings.Join(llm.Supported, ", "))
			}
			prof.Provider = *provider
		}
		if *model != "" {
			prof.Model = *model
		}
		if *endpoint != "" {
			prof.Endpoint = *endpoint
		}
		if *makeDefault {
			prof.IsDefault = true
		}
		if *purpose != "" {
			if *purpose != store.PurposeChat && *purpose != store.PurposeEmbedding {
				return Fail(ExitUsage, "--purpose must be chat or embedding")
			}
			prof.Purpose = *purpose
		}
		// An embedding profile's width is probed rather than declared: the
		// model is the only thing that knows it, and a stored vector that does
		// not match it cannot be compared against anything.
		if prof.Purpose == store.PurposeEmbedding {
			ctxProbe, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			dims, err := llm.ProbeEmbedding(ctxProbe, prof.Endpoint, prof.APIKey, prof.Provider, prof.Model)
			cancel()
			if err != nil {
				return Fail(ExitUsage,
					"%s does not embed at %s: %v\n\nRun `iql llm profile list` after pulling an embedding model.",
					prof.Model, orDefault(prof.Endpoint, llm.DefaultEndpoints[prof.Provider]), err)
			}
			prof.Dimensions = dims
		}
		if *withKey || os.Getenv("INBOXQL_LLM_API_KEY") != "" {
			key, err := ctx.ReadSecret("INBOXQL_LLM_API_KEY", "API key")
			if err != nil {
				return err
			}
			prof.APIKey = key
		}

		if err := store.SaveLLMProfile(prof); err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(prof.Redacted())
		}
		verb := "Added"
		if existing != nil {
			verb = "Updated"
		}
		ctx.Printf("%s profile %s (%s, %s", verb,
			ctx.Printer().Bold(prof.Name), prof.Provider, prof.Model)
		if prof.Purpose == store.PurposeEmbedding {
			ctx.Printf(", embedding, %d dimensions", prof.Dimensions)
		}
		ctx.Printf(").\n")
		if prof.IsRemote() {
			// Said plainly, because this is the sentence that decides whether
			// message bodies leave the machine.
			ctx.Printf("%s\n", ctx.Printer().Yellow(
				"This profile is remote: annotators using it send message bodies to "+prof.Endpoint+"."))
			ctx.Printf("They will refuse to run unless created with --allow-remote.\n")
		}
		ctx.Printf("Check it reaches the model with: iql llm test --profile %s\n", prof.Name)
		return nil

	case "default":
		name, _ := subcommand(rest)
		if name == "" {
			return Fail(ExitUsage, "name the profile to make default")
		}
		if err := store.SetDefaultLLMProfile(name); err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]string{"default": name})
		}
		ctx.Printf("%s is now the default profile.\n", ctx.Printer().Bold(name))
		return nil

	case "remove", "delete", "rm":
		name, _ := subcommand(rest)
		if name == "" {
			return Fail(ExitUsage, "name the profile to remove")
		}
		// Annotators pointing at it would start failing, so say so first
		// rather than after.
		users, err := annotatorsUsingProfile(name)
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if len(users) > 0 {
			return Fail(ExitError,
				"%s is used by %s: %s\nPoint them elsewhere first with `iql annotate create <name> --profile <other>`.",
				name, count(len(users), "annotator", "annotators"), strings.Join(users, ", "))
		}
		if err := store.DeleteLLMProfile(name); err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(map[string]string{"removed": name})
		}
		ctx.Printf("Removed profile %s.\n", name)
		return nil

	default:
		return Fail(ExitUsage, "unknown subcommand %q (want list, add, default or remove)", sub)
	}
}

func supportedProvider(name string) bool {
	for _, p := range llm.Supported {
		if name == p {
			return true
		}
	}
	return false
}

// annotatorsUsingProfile names the annotators that would break if a profile
// went away.
func annotatorsUsingProfile(name string) ([]string, error) {
	annotators, err := store.ListAnnotators()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range annotators {
		if a.Profile == name {
			out = append(out, a.Name)
		}
	}
	return out, nil
}
