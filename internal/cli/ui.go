package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/user/inboxql/internal/cli/ui"
	"github.com/user/inboxql/internal/uisession"
)

func init() {
	register(&Command{
		Name:    "ui",
		Summary: "see the open windows and point them at things",
		Usage: `iql ui <list|query|open|notice|close> [--addr host:port]

  list              who is open, and what each is showing
  query <id> <q>    point one window at a query
  open <id> <what>  open something: desk, log, settings, annotators, or a message id
  notice <id> <text>  say something, without changing anything
  close <id>        forget a window that is gone

flags:
  --addr <host:port>  the running server (default: $INBOXQL_ADDR, the machine settings, then 127.0.0.1:8420)
  --note <text>       why, shown beside whatever changes

## This one talks to the server, not the database

Every other command reads the data directory directly. This cannot: the list of
open windows lives in the memory of the process serving them, because a
connection is not a fact that should outlive its process. A registry restored
from disk would be a list of windows that are not there, and every command sent
to one would appear to succeed.

So the server has to be running, and --addr has to point at it.

## What a command is

An instruction to a live window, not a new source of truth about it. Point a
window at a query, reload the window, and it comes back showing whatever it had
persisted for itself. This points; it does not drive.

## Say why

  iql ui query 2 "from:stripe after:7d" --note "the invoices you asked about"

A screen that reorganises itself with no explanation is alarming rather than
helpful. The note is shown beside whatever changed, and costs one flag.`,
		Run: runUI,
	})
}

func runUI(ctx *Context, args []string) error {
	sub, rest := subcommand(args)

	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(ctx.Stderr)
	addr := fs.String("addr", ctx.defaultAddr(), "the running server")
	note := fs.String("note", "", "why, shown beside whatever changes")

	// The positionals come before the flags are parsed, because an id and a
	// query are both ordinary words and flag.Parse stops at the first one.
	var positional []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
	if err := parseArgs(fs, rest); err != nil {
		return Fail(ExitUsage, "invalid flags")
	}

	c := &uiClient{addr: *addr, ctx: ctx}

	switch sub {
	case "list", "":
		return c.list()
	case "query":
		if len(positional) < 2 {
			return Fail(ExitUsage, `which window, and what query? iql ui query 2 "from:stripe"`)
		}
		return c.send(positional[0], uisession.Command{
			Verb: "query", Arg: strings.Join(positional[1:], " "), Note: *note})
	case "open":
		if len(positional) < 2 {
			return Fail(ExitUsage, "which window, and what to open? iql ui open 2 log")
		}
		return c.send(positional[0], uisession.Command{
			Verb: "open", Arg: positional[1], Note: *note})
	case "notice":
		if len(positional) < 2 {
			return Fail(ExitUsage, `which window, and what to say? iql ui notice 2 "have a look"`)
		}
		return c.send(positional[0], uisession.Command{
			Verb: "notice", Arg: strings.Join(positional[1:], " ")})
	case "close":
		if len(positional) < 1 {
			return Fail(ExitUsage, "which window?")
		}
		return c.forget(positional[0])
	default:
		return Fail(ExitUsage, "unknown subcommand %q (want list, query, open, notice or close)", sub)
	}
}

type uiClient struct {
	addr string
	ctx  *Context
}

func (c *uiClient) url(path string) string {
	addr := c.addr
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	return strings.TrimRight(addr, "/") + path
}

// do makes one request, translating a refused connection into the advice that
// actually helps.
func (c *uiClient) do(method, path string, body any) ([]byte, error) {
	var buf *bytes.Buffer
	if body != nil {
		blob, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(blob)
	} else {
		buf = bytes.NewBuffer(nil)
	}

	req, err := http.NewRequest(method, c.url(path), buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) || strings.Contains(err.Error(), "refused") {
			return nil, fmt.Errorf(
				"no server at %s.\nThis command talks to a running server, because the open "+
					"windows live in its memory.\nStart one with `iql start`, or pass --addr", c.addr)
		}
		return nil, err
	}
	defer resp.Body.Close()

	out := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(chunk)
		out = append(out, chunk[:n]...)
		if rerr != nil {
			break
		}
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(out, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("%s", e.Error)
		}
		return nil, fmt.Errorf("%s returned %s", path, resp.Status)
	}
	return out, nil
}

func (c *uiClient) list() error {
	blob, err := c.do(http.MethodGet, "/api/ui", nil)
	if err != nil {
		return Fail(ExitError, "%v", err)
	}
	var body struct {
		Windows []*uisession.Session `json:"windows"`
	}
	if err := json.Unmarshal(blob, &body); err != nil {
		return Fail(ExitError, "%v", err)
	}

	if c.ctx.JSON {
		return c.ctx.EmitJSON(body)
	}
	if len(body.Windows) == 0 {
		c.ctx.Printf("No windows open.\n")
		c.ctx.Printf("%s\n", c.ctx.Printer().Dim(
			"A browser pointed at the server registers itself and appears here."))
		return nil
	}

	p := c.ctx.Printer()
	t := p.NewTable("ID", "", "SHOWING", "TABS", "SEEN")
	for _, s := range body.Windows {
		status, label := ui.OK, "live"
		if !s.Connected {
			// Open but uncommandable: worth distinguishing, because a command
			// to it will fail and the listing is where somebody finds out why.
			status, label = ui.Warn, "no channel"
		}
		showing := s.Showing.Query
		if showing == "" {
			showing = p.Dim("—")
		}
		t.Row(s.Name, t.Cell(status, label), showing,
			strings.Join(s.Showing.Tabs, " "), ago(s.LastSeen))
	}
	if err := t.Flush(); err != nil {
		return Fail(ExitError, "%v", err)
	}

	for _, s := range body.Windows {
		if s.LastCommand != nil {
			// Said, because a window showing something nobody in front of it
			// asked for is otherwise a mystery.
			c.ctx.Printf("%s\n", p.Dim(fmt.Sprintf(
				"  %s was last told: %s %s", s.Name, s.LastCommand.Verb, s.LastCommand.Arg)))
		}
	}
	return nil
}

func (c *uiClient) send(name string, cmd uisession.Command) error {
	if _, err := c.do(http.MethodPost, "/api/ui/"+name+"/command", cmd); err != nil {
		return Fail(ExitNotFound, "%v", err)
	}
	if c.ctx.JSON {
		return c.ctx.EmitJSON(map[string]any{"window": name, "sent": cmd})
	}
	c.ctx.Printf("Sent to window %s.\n", name)
	return nil
}

func (c *uiClient) forget(name string) error {
	if _, err := c.do(http.MethodDelete, "/api/ui/"+name, nil); err != nil {
		return Fail(ExitError, "%v", err)
	}
	c.ctx.Printf("Forgot window %s.\n", name)
	return nil
}

// ago renders how long since a window was heard from.
func ago(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < time.Second {
		return "now"
	}
	return d.String() + " ago"
}
