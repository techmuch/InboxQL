package importer

import (
	"fmt"
	"strings"
	"testing"
)

// emlx builds an Apple .emlx container around a message body.
func emlx(rfc822, plist string) []byte {
	return []byte(fmt.Sprintf("%d\n%s%s", len(rfc822), rfc822, plist))
}

const sampleMessage = "From: alice@example.com\r\n" +
	"To: me@example.com\r\n" +
	"Subject: Lunch\r\n" +
	"Message-Id: <abc@example.com>\r\n" +
	"\r\n" +
	"Thursday works.\r\n"

func TestSplitEMLX(t *testing.T) {
	data := emlx(sampleMessage, "<?xml version=\"1.0\"?><plist></plist>")

	raw, plist, ok := SplitEMLX(data)
	if !ok {
		t.Fatal("a well-formed .emlx was not recognised")
	}
	if string(raw) != sampleMessage {
		t.Errorf("payload = %q, want %q", raw, sampleMessage)
	}
	if !strings.HasPrefix(string(plist), "<?xml") {
		t.Errorf("trailing plist = %q", plist)
	}
}

// A plain .eml must pass through untouched — the eml source hands both kinds to
// the same code path.
func TestSplitEMLXRejectsPlainMessage(t *testing.T) {
	if _, _, ok := SplitEMLX([]byte(sampleMessage)); ok {
		t.Error("a plain RFC822 message was misread as an .emlx container")
	}
}

func TestSplitEMLXRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "\n", "not-a-number\nbody", "0\nbody", "-5\nbody"} {
		if _, _, ok := SplitEMLX([]byte(in)); ok {
			t.Errorf("SplitEMLX(%q) accepted invalid input", in)
		}
	}
}

// Mail.app may be rewriting a file as it is read, so a truncated container must
// yield what is there rather than an error or a panic.
func TestSplitEMLXTruncated(t *testing.T) {
	data := []byte("9999\n" + sampleMessage)

	raw, _, ok := SplitEMLX(data)
	if !ok {
		t.Fatal("a truncated .emlx should still be readable")
	}
	if string(raw) != sampleMessage {
		t.Errorf("payload = %q, want the available bytes", raw)
	}
}

const plistWithFlags = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>subject</key><string>Lunch</string>
	<key>flags</key><integer>%d</integer>
	<key>date-sent</key><integer>1755900000</integer>
</dict>
</plist>`

func TestEMLXFlags(t *testing.T) {
	cases := []struct {
		name string
		bits int
		want []string
	}{
		{"unread", 0, nil},
		{"read", 1, []string{`\Seen`}},
		{"flagged", 16, []string{`\Flagged`}},
		{"read and flagged", 17, []string{`\Seen`, `\Flagged`}},
		// Real messages carry many bits InboxQL does not decode; the ones it does
		// must still be found among them.
		{"read amid unknown bits", 8623489 | 1, []string{`\Seen`}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EMLXFlags([]byte(fmt.Sprintf(plistWithFlags, tc.bits)))
			if len(got) != len(tc.want) {
				t.Fatalf("flags = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("flags = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// Missing or unparseable metadata means "flags unknown", which must not be
// reported as "no flags" — that would mark every message unread.
func TestEMLXFlagsAbsent(t *testing.T) {
	for _, in := range []string{"", "not xml", "<plist><dict></dict></plist>",
		`<plist><dict><key>subject</key><string>x</string></dict></plist>`} {
		if got := EMLXFlags([]byte(in)); got != nil {
			t.Errorf("EMLXFlags(%q) = %v, want nil", in, got)
		}
	}
}

func TestSelectionWants(t *testing.T) {
	day := func(s string) (t2 timeT) { return parseTestDay(s) }
	sel := Selection{Since: day("2026-01-01"), Until: day("2026-12-31")}

	if sel.Wants(day("2025-12-31")) {
		t.Error("a message before Since was accepted")
	}
	if !sel.Wants(day("2026-06-15")) {
		t.Error("a message inside the range was rejected")
	}
	if sel.Wants(day("2027-01-01")) {
		t.Error("a message after Until was accepted")
	}

	// An unbounded selection takes everything, including a zero date.
	if !(Selection{}).Wants(day("1999-01-01")) {
		t.Error("an unbounded selection rejected a message")
	}
}
