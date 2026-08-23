package mailer

import (
	"strings"
	"testing"
	"time"
)

func base() *Message {
	return &Message{
		From:    "me@example.com",
		To:      []string{"you@example.com"},
		Subject: "Hello",
		Body:    "Hi there.",
		Date:    time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC),
	}
}

// Bcc leaking into the headers would disclose every hidden recipient to
// everyone else on the message, which is the one thing Bcc must never do.
func TestBccIsNotInHeadersButIsInEnvelope(t *testing.T) {
	m := base()
	m.Cc = []string{"cc@example.com"}
	m.Bcc = []string{"secret@example.com"}

	rendered := string(m.Render())

	if strings.Contains(rendered, "secret@example.com") {
		t.Errorf("Bcc recipient appears in the rendered message:\n%s", rendered)
	}
	if strings.Contains(rendered, "Bcc:") {
		t.Error("a Bcc header was written into the message")
	}
	if !strings.Contains(rendered, "Cc: cc@example.com") {
		t.Error("Cc should appear in the headers")
	}

	// ...but the SMTP envelope must still deliver to them.
	got := m.Recipients()
	want := map[string]bool{"you@example.com": false, "cc@example.com": false, "secret@example.com": false}
	for _, r := range got {
		if _, ok := want[r]; !ok {
			t.Errorf("unexpected recipient %q", r)
		}
		want[r] = true
	}
	for addr, seen := range want {
		if !seen {
			t.Errorf("recipient %q missing from the envelope", addr)
		}
	}
}

// A bare "." on its own line terminates SMTP DATA. Without dot-stuffing, a
// message body containing one would be truncated and the remainder
// interpreted as SMTP commands.
func TestBodyDotStuffing(t *testing.T) {
	m := base()
	m.Body = "first line\n.\nlast line"

	rendered := string(m.Render())

	if !strings.Contains(rendered, "\r\n..\r\n") {
		t.Errorf("a lone dot line was not stuffed:\n%q", rendered)
	}
	if strings.Contains(rendered, "\r\n.\r\nlast line") {
		t.Error("body still contains an unescaped dot line, which would truncate the message")
	}
}

func TestHeadersUseCRLF(t *testing.T) {
	rendered := string(base().Render())

	// A bare LF in headers is a protocol violation some servers reject outright.
	for _, line := range strings.Split(rendered, "\r\n") {
		if strings.Contains(line, "\n") {
			t.Errorf("found a bare LF in %q", line)
		}
	}
	if !strings.Contains(rendered, "\r\n\r\n") {
		t.Error("no blank line separating headers from body")
	}
}

func TestReplyHeaders(t *testing.T) {
	m := base()
	m.InReplyTo = "<original@example.com>"

	rendered := string(m.Render())

	// Clients display against In-Reply-To but thread on References.
	if !strings.Contains(rendered, "In-Reply-To: <original@example.com>") {
		t.Error("missing In-Reply-To")
	}
	if !strings.Contains(rendered, "References: <original@example.com>") {
		t.Error("missing References, so the reply will not thread")
	}
}

func TestNoReplyHeadersWhenNotAReply(t *testing.T) {
	rendered := string(base().Render())
	if strings.Contains(rendered, "In-Reply-To") || strings.Contains(rendered, "References") {
		t.Error("threading headers written for a message that is not a reply")
	}
}

func TestNonASCIISubjectIsEncoded(t *testing.T) {
	m := base()
	m.Subject = "Café meeting"

	rendered := string(m.Render())

	if strings.Contains(rendered, "Subject: Café meeting") {
		t.Error("non-ASCII subject was emitted raw instead of MIME-encoded")
	}
	if !strings.Contains(rendered, "=?utf-8?") {
		t.Errorf("expected an encoded-word subject, got:\n%s", rendered)
	}
}

func TestASCIISubjectStaysReadable(t *testing.T) {
	// Encoding a plain subject would be legal but makes the wire format
	// needlessly unreadable when debugging.
	if !strings.Contains(string(base().Render()), "Subject: Hello") {
		t.Error("a plain ASCII subject should not be encoded")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Message)
		wantErr string
	}{
		{"valid", func(*Message) {}, ""},
		{"bad from", func(m *Message) { m.From = "not-an-address" }, "invalid From"},
		{"bad recipient", func(m *Message) { m.To = []string{"nope"} }, "invalid recipient"},
		{"no recipients", func(m *Message) { m.To = nil }, "no recipients"},
		{"empty subject", func(m *Message) { m.Subject = "   " }, "subject is empty"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			tc.mutate(m)
			err := m.Validate()

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("got %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
