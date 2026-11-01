package message

import (
	"strings"
	"testing"
)

// Real first lines from a real inbox, as the list showed them.
func TestPreviewKeepsWhatAPersonWouldRead(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		not              []string
	}{
		{"marketing padding", "&zwnj; &zwnj; &zwnj; &zwnj; &zwnj; &zwnj;\nSee top rated San Antonio stays.", "See top rated San Antonio stays.", []string{"&zwnj;", "\u200c"}},
		{"decoded padding", "\u200c \u200c \u034f \u00ad Your weekly digest", "Your weekly digest", nil},
		{"tracking link", "Navy Federal Credit Union https://click.email.navyfederal.org/?qs=ABp7ImQiOjUwNDUsInQiOjEwM Deposit", "Navy Federal Credit Union Deposit", []string{"http", "qs="}},
		{"bracketed link", "<https://voice.google.com> Hi David We just got home", "Hi David We just got home", []string{"voice.google"}},
		{"leaked stylesheet", "Join Odoo in Atlanta, GA .o_layout .o_mail_wrapper .o_mail_wrapper_td a.btn.btn-secondary.btn-sm { color: red } Register", "Join Odoo in Atlanta, GA Register", []string{"o_layout", "btn", "color"}},
		{"a domain in a sentence stays", "Your order from amazon.com has shipped", "Your order from amazon.com has shipped", nil},
		{"a file name stays", "Attached is report.pdf for review", "Attached is report.pdf for review", nil},
	} {
		got := Preview(&Message{Body: c.body}, 200)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		for _, bad := range c.not {
			if strings.Contains(got, bad) {
				t.Errorf("%s: %q still contains %q", c.name, got, bad)
			}
		}
	}
}

func TestPreviewIsCutToLength(t *testing.T) {
	got := Preview(&Message{Body: strings.Repeat("word ", 100)}, 30)
	if n := len([]rune(got)); n > 30 {
		t.Errorf("%d characters, want at most 30", n)
	}
}

func TestPreviewReadsAnHTMLOnlyMessage(t *testing.T) {
	got := Preview(&Message{HTMLBody: "<html><head><style>.x{color:red}</style></head><body><p>Hello &amp; welcome</p></body></html>"}, 100)
	if got != "Hello & welcome" {
		t.Errorf("got %q", got)
	}
}
