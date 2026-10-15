package imaptest

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"net"
	"strings"
	"sync"
)

// SMTP is a fake outgoing server beside a fake IMAP one: STARTTLS with the
// same certificate, AUTH PLAIN and LOGIN with the same login, and every
// message it accepts kept. Beside a Gmail-mode server it files what it is
// given in Sent, as Gmail does with mail sent through it.
type SMTP struct {
	Addr string
	Port int

	imap *Server
	ln   net.Listener
	mu   sync.Mutex
	got  []string
}

// StartSMTP runs one, filing into imap when that is Gmail-like.
func StartSMTP(imap *Server) (*SMTP, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &SMTP{Addr: ln.Addr().String(), Port: ln.Addr().(*net.TCPAddr).Port, imap: imap, ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s, nil
}

// Close stops it.
func (s *SMTP) Close() { s.ln.Close() }

// Received is every message accepted, as sent.
func (s *SMTP) Received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

func (s *SMTP) serve(c net.Conn) {
	defer c.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c))
	say := func(l string) { rw.WriteString(l + "\r\n"); rw.Flush() }
	say("220 fake ESMTP")
	secure, authed := false, false
	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			if secure {
				say("250-fake\r\n250 AUTH PLAIN LOGIN")
			} else {
				say("250-fake\r\n250 STARTTLS")
			}
		case cmd == "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{s.imap.cert}})
			if tc.Handshake() != nil {
				return
			}
			c, secure = tc, true
			rw = bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c))
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimSpace(line)[len("AUTH PLAIN"):]))
			if string(raw) == "\x00"+s.imap.User+"\x00"+s.imap.Pass {
				authed = true
				say("235 ok")
			} else {
				say("535 5.7.8 Authentication credentials invalid")
			}
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			if !authed {
				say("530 authenticate first")
				continue
			}
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"), cmd == "RSET", cmd == "NOOP":
			say("250 ok")
		case cmd == "DATA":
			say("354 go ahead")
			var msg strings.Builder
			for {
				l, err := rw.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				msg.WriteString(strings.TrimPrefix(l, "."))
			}
			s.mu.Lock()
			s.got = append(s.got, msg.String())
			s.mu.Unlock()
			if s.imap.opt.Gmail {
				s.imap.Deliver(msg.String(), Sent)
			}
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("502 no")
		}
	}
}
