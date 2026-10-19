// Command devmail runs InboxQL against a fake Gmail, for working on the
// mirror, labels and sending without a real account.
//
//	go run ./internal/imaptest/devmail -data /tmp/devmail -addr 127.0.0.1:8477
//
// It starts a Gmail-like IMAP server and an SMTP server on loopback with a
// certificate this process trusts, adds an account for them to the mailbox
// (initialising it if needed), fills the fake with mail, and then runs
// `iql start` in this process. A second port takes the part of another
// device:
//
//	POST /deliver  {"subject":…, "from":…, "labels":["\\Inbox","Receipts"]}
//	POST /change   {"messageId":…, "add":[…], "remove":[…], "flags":[…], "unflags":[…]}
//	POST /delete   {"messageId":…}
//	GET  /state    every message's labels and flags, as the fake has them
//	GET  /sent     what the SMTP server was given
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/cli"
	"github.com/user/inboxql/internal/imaptest"
	"github.com/user/inboxql/internal/store"
)

func main() {
	data := flag.String("data", "", "the mailbox folder (made if missing)")
	addr := flag.String("addr", "127.0.0.1:8477", "where InboxQL listens")
	control := flag.String("control", "127.0.0.1:8478", "where the fake's control port listens")
	history := flag.Int("history", 1200, "older messages to put in the fake")
	flag.Parse()
	if *data == "" {
		log.Fatal("-data is required")
	}

	srv, err := imaptest.Start(imaptest.Options{Gmail: true})
	if err != nil {
		log.Fatal(err)
	}
	smtp, err := imaptest.StartSMTP(srv)
	if err != nil {
		log.Fatal(err)
	}
	account.TestRoots = srv.RootCAs

	// Older mail first, so UIDs grow with time as they do on a real account.
	for i := 0; i < *history; i++ {
		labels := []string{}
		if i%5 == 0 {
			labels = append(labels, "Receipts")
		}
		if i%11 == 0 {
			labels = append(labels, "Work/Clients")
		}
		if i%13 == 0 {
			labels = append(labels, imaptest.Sent)
		}
		m := srv.Deliver(imaptest.Message(fmt.Sprintf("old%05d@devmail", i), fmt.Sprintf("Older message %d", i),
			"archive@example.com"), labels...)
		srv.Change(m, func(m *imaptest.Msg) { m.Flags[`\Seen`] = true })
	}
	for i, subj := range []string{"Lunch on Friday?", "Your invoice from Acme", "Quarterly numbers", "Flight confirmation", "Re: the proposal"} {
		labels := []string{imaptest.Inbox}
		if i == 1 {
			labels = append(labels, "Receipts")
		}
		if i == 3 {
			labels = append(labels, "Travel", imaptest.Important)
		}
		srv.Deliver(imaptest.Message(fmt.Sprintf("new%d@devmail", i), subj, fmt.Sprintf("person%d@example.com", i)), labels...)
	}

	// The account, written before the server starts so its worker finds it.
	if code := cli.Execute([]string{"--data", *data, "init"}, strings.NewReader("\n\n\n"), os.Stdout, os.Stderr); code != 0 {
		log.Printf("init exited %d (fine if the mailbox exists)", code)
	}
	if _, err := store.InitDB(*data); err != nil {
		log.Fatal(err)
	}
	acc := &account.Account{ID: "gmail", Name: "Fake Gmail", Email: "me@gmail.example", Host: "127.0.0.1",
		Port: srv.Port, SSL: true, User: srv.User, Password: srv.Pass,
		SMTPHost: "127.0.0.1", SMTPPort: smtp.Port}
	if err := store.SaveAccount(acc); err != nil {
		log.Fatal(err)
	}
	store.CloseDB()

	go controlPort(*control, srv, smtp)
	log.Printf("fake Gmail on %s, SMTP on %s, control on http://%s", srv.Addr, smtp.Addr, *control)
	os.Exit(cli.Execute([]string{"--data", *data, "start", "--addr", *addr}, os.Stdin, os.Stdout, os.Stderr))
}

func controlPort(addr string, srv *imaptest.Server, smtp *imaptest.SMTP) {
	mux := http.NewServeMux()
	read := func(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }
	mux.HandleFunc("POST /deliver", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Subject, From string
			Labels        []string
			// Raw is a whole RFC 822 message, sent as it is, for an HTML
			// message or anything else Message does not make.
			Raw string
		}
		if err := read(r, &req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if req.From == "" {
			req.From = "someone@example.com"
		}
		id := fmt.Sprintf("%d@devmail", time.Now().UnixNano())
		msg := imaptest.Message(id, req.Subject, req.From)
		if req.Raw != "" {
			msg = req.Raw
		}
		srv.Deliver(msg, req.Labels...)
		json.NewEncoder(w).Encode(map[string]string{"messageId": id})
	})
	mux.HandleFunc("POST /change", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MessageID                   string
			Add, Remove, Flags, Unflags []string
		}
		if err := read(r, &req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		m := srv.Find(req.MessageID)
		if m == nil {
			http.Error(w, "no such message", 404)
			return
		}
		srv.Change(m, func(m *imaptest.Msg) {
			for _, l := range req.Add {
				m.Labels[l] = true
			}
			for _, l := range req.Remove {
				delete(m.Labels, l)
			}
			for _, f := range req.Flags {
				m.Flags[f] = true
			}
			for _, f := range req.Unflags {
				delete(m.Flags, f)
			}
		})
		w.Write([]byte("{}"))
	})
	mux.HandleFunc("POST /delete", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ MessageID string }
		read(r, &req)
		if m := srv.Find(req.MessageID); m != nil {
			srv.Delete(m)
		}
		w.Write([]byte("{}"))
	})
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("id")
		out := []map[string]any{}
		for _, id := range strings.Split(q, ",") {
			if m := srv.Find(id); m != nil {
				out = append(out, map[string]any{"messageId": id, "labels": srv.LabelsOf(m), "flags": srv.FlagsOf(m)})
			}
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /sent", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(smtp.Received())
	})
	log.Fatal(http.ListenAndServe(addr, mux))
}
