package cli

import (
	"os"
	"strings"

	"github.com/user/inboxql/internal/skills"
)

func init() {
	register(&Command{
		Name:    "skills",
		Summary: "teach AI coding tools how to use iql",
		Usage: `iql skills <list|install|remove> [<where>] [--json]

Installs a short guide to iql where AI coding tools look for skills, so an
agent you run knows the rules before it starts: that ` + "`send`" + ` only queues,
that ` + "`-label:x`" + ` leaves out what was never looked at, that ` + "`me()`" + ` exists, and
that ` + "`iql help agents`" + ` has the rest.

  list              which tools are on this machine, and what is installed
  install <where>   write the skill
  remove <where>    delete it

<where> is a tool or a location:

  claude-code, claude   ~/.claude/skills/iql          Claude Code
  codex, gemini, agents ~/.agents/skills/iql          Codex and Gemini CLI

Codex and Gemini CLI read the same folder, so one install serves both.

Only a skill this command wrote is ever replaced or removed; a folder named
iql that something else put there is left alone. ` + "`iql update`" + ` and each start of
the server refresh installed skills to describe the version installed.`,
		Run: runSkills,
	})
}

func skillsLocation(home, where string) (skills.Location, error) {
	switch strings.ToLower(where) {
	case "claude-code", "claude":
		where = "claude"
	case "codex", "gemini", "gemini-cli", "agents":
		where = "agents"
	}
	return skills.Find(home, where)
}

func runSkills(ctx *Context, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		tools, states := skills.Detect(home), skills.Status(home, Version)
		if ctx.JSON {
			return ctx.EmitJSON(map[string]any{"tools": tools, "locations": states, "version": Version})
		}
		p := ctx.Printer()
		for _, st := range states {
			var names []string
			for _, t := range tools {
				if t.Location == st.ID && t.Installed {
					names = append(names, t.Name)
				}
			}
			state := p.Dim("not installed")
			switch {
			case st.Foreign:
				state = p.Yellow("a skill named iql is there that iql did not write; left alone")
			case st.Installed && st.Current:
				state = p.Green("installed, current")
			case st.Installed:
				state = p.Yellow("installed (" + st.Version + "); `iql skills install " + st.ID + "` brings it up to date")
			}
			used := "no tool that reads it is installed"
			if len(names) > 0 {
				used = "read by " + strings.Join(names, ", ")
			}
			ctx.Printf("%s\n  %-9s %s\n  %-9s %s\n", p.Bold(st.Path), p.Dim("state"), state, p.Dim("tools"), used)
		}
		return nil
	case "install", "remove":
		if len(args) < 2 {
			return Fail(ExitUsage, "say where: claude-code, codex or gemini (`iql skills list` shows them)")
		}
		l, err := skillsLocation(home, args[1])
		if err != nil {
			return Fail(ExitUsage, "%v", err)
		}
		var st skills.State
		if args[0] == "install" {
			st, err = skills.Install(l, Version)
		} else {
			st, err = skills.Remove(l, Version)
		}
		if err == skills.ErrForeign {
			return Fail(ExitError, "%s: %v", l.Path, err)
		}
		if err != nil {
			return Fail(ExitError, "%v", err)
		}
		if ctx.JSON {
			return ctx.EmitJSON(st)
		}
		if args[0] == "install" {
			ctx.Printf("Installed %s\n", l.Path)
		} else {
			ctx.Printf("Removed %s\n", l.Path)
		}
		return nil
	default:
		return Fail(ExitUsage, "unknown subcommand %q (list, install, remove)", args[0])
	}
}
