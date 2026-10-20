package message

import (
	"strings"
	"testing"
)

// A receipt as Home Depot sends it: a stylesheet first, entities in the item
// lines, and the items in table rows.
func TestHTMLTextReadsLikeTheReceipt(t *testing.T) {
	src := `<html><head><title>Receipt</title><style>@charset "UTF-8"; html, body, div { margin: 0 }</style></head>
<body><!-- tracking --><table><tr><td>885911470902 FLEXVOLT 20-</td><td>&lt;A&gt;</td><td>159.00</td></tr>
<tr><td>Visa&nbsp;Credit</td></tr></table><p>Thanks &amp; see you soon</p><script>var x=1;</script></body></html>`
	got := HTMLText(src)
	for _, absent := range []string{"@charset", "margin", "var x", "tracking", "&lt;", "&nbsp;", "Receipt"} {
		if strings.Contains(got, absent) {
			t.Errorf("%q survived:\n%s", absent, got)
		}
	}
	for _, line := range []string{"885911470902 FLEXVOLT 20- <A> 159.00", "Visa Credit", "Thanks & see you soon"} {
		if !strings.Contains(got, line) {
			t.Errorf("missing line %q in:\n%s", line, got)
		}
	}
}

func TestReadablePrefersThePlainPart(t *testing.T) {
	m := &Message{Body: "  plain  ", HTMLBody: "<p>html</p>"}
	if got := Readable(m); got != "plain" {
		t.Errorf("Readable = %q", got)
	}
	if got := Readable(&Message{HTMLBody: "<p>one</p><p>two</p>"}); got != "one\ntwo" {
		t.Errorf("Readable = %q", got)
	}
}
