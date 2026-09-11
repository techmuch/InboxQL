package filetext

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"testing"
)

func TestExtractPlainText(t *testing.T) {
	got := Extract("text/plain", "notes.txt", []byte("Invoice total: $42.00\nDue on Friday.\n"))
	if got.Status != StatusOK {
		t.Fatalf("status %q, want ok", got.Status)
	}
	if len(got.Pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(got.Pages))
	}
	// A text file has no pagination, and inventing one would make "page 3"
	// mean nothing a reader could check.
	if got.Pages[0].Number != 0 {
		t.Errorf("page number %d, want 0 for an unpaginated file", got.Pages[0].Number)
	}
	if !strings.Contains(got.Pages[0].Text, "Invoice total: $42.00") {
		t.Errorf("text %q lost the content", got.Pages[0].Text)
	}
}

// The declared type is a hint, not evidence. Mail is full of scanners and
// gateways that label a PDF application/octet-stream.
func TestExtractPrefersContentOverLabel(t *testing.T) {
	pdf := buildPDF(t, "Hello from a mislabelled file")

	got := Extract("application/octet-stream", "scan.bin", pdf)
	if got.Status != StatusOK {
		t.Fatalf("status %q (%s), want ok — the magic number says PDF", got.Status, got.Detail)
	}
	if !strings.Contains(pageText(got), "mislabelled") {
		t.Errorf("extracted %q", pageText(got))
	}
}

func TestExtractUnsupported(t *testing.T) {
	// A PNG header, which nothing here reads.
	got := Extract("image/png", "photo.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	if got.Status != StatusUnsupported {
		t.Errorf("status %q, want unsupported", got.Status)
	}
}

// Binary that is not text must not be indexed as text: noise in the index is
// worse than the absence it replaces.
func TestLooksLikeTextRejectsBinary(t *testing.T) {
	if looksLikeText([]byte{0x00, 0x01, 0x02, 0xFF, 0xFE}) {
		t.Error("accepted bytes with NULs")
	}
	if looksLikeText(nil) {
		t.Error("accepted nothing")
	}
	if !looksLikeText([]byte("a perfectly ordinary line of text\n")) {
		t.Error("rejected plain text")
	}
	if !looksLikeText([]byte("cafés, naïve, £20 — punctuation\n")) {
		t.Error("rejected non-ASCII text")
	}
}

func TestExtractPDFText(t *testing.T) {
	got := Extract("application/pdf", "invoice.pdf", buildPDF(t, "Invoice 4815 total 92.50"))
	if got.Status != StatusOK {
		t.Fatalf("status %q (%s), want ok", got.Status, got.Detail)
	}
	if got.Pages[0].Number != 1 {
		t.Errorf("page number %d, want 1 — pages are 1-based", got.Pages[0].Number)
	}
	text := pageText(got)
	for _, want := range []string{"Invoice", "4815", "92.50"} {
		if !strings.Contains(text, want) {
			t.Errorf("extracted %q, missing %q", text, want)
		}
	}
}

// A scanned page is the case OCR exists for, and it has to be distinguishable
// from a failure or every later pass re-attempts every broken file forever.
func TestExtractPDFWithNoTextLayerIsEmptyNotFailed(t *testing.T) {
	got := Extract("application/pdf", "scan.pdf", buildPDF(t, ""))
	if got.Status != StatusEmpty {
		t.Fatalf("status %q (%s), want empty", got.Status, got.Detail)
	}
	if got.Detail == "" {
		t.Error("an empty result says nothing about why")
	}
}

func TestExtractPDFEncrypted(t *testing.T) {
	pdf := buildPDF(t, "secret")
	pdf = bytes.Replace(pdf, []byte("%PDF-1.4\n"), []byte("%PDF-1.4\n% /Encrypt 9 0 R\n"), 1)

	got := Extract("application/pdf", "locked.pdf", pdf)
	if got.Status != StatusFailed {
		t.Errorf("status %q, want failed for an encrypted document", got.Status)
	}
	if !strings.Contains(got.Detail, "encrypted") {
		t.Errorf("detail %q does not say why", got.Detail)
	}
}

// Garbage in must not panic, hang, or come back claiming success.
func TestExtractPDFTolerance(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("%PDF-1.4"),
		[]byte("%PDF-1.4\n1 0 obj\n<< /Type /Page >>"), // no endobj
		[]byte("%PDF-1.4\n1 0 obj\nstream\ntruncated"), // no endstream
		[]byte("%PDF-1.4\n" + strings.Repeat("0 0 obj\n", 1000)),
		append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte{0xFF}, 4096)...),
	} {
		got := Extract("application/pdf", "broken.pdf", data)
		if got.Status == StatusOK && pageText(got) == "" {
			t.Errorf("reported ok with no text for %d bytes of garbage", len(data))
		}
	}
}

// The whole point of dropping unmapped glyphs: a subset font's codes are not
// characters, and indexing them produces words nobody can search for.
func TestSubsetFontWithoutMappingIsDropped(t *testing.T) {
	f := font{readable: false}
	if got := decodeString("\x07\x11\x0f", f); got != "" {
		t.Errorf("decoded unmappable subset bytes as %q, want them dropped", got)
	}

	// With a standard encoding the same bytes are characters and are kept.
	if got := decodeString("Tech", font{readable: true}); got != "Tech" {
		t.Errorf("decoded standard-encoded text as %q", got)
	}
}

// A composite font's codes are two bytes. Reading them one at a time finds
// entries by accident — a subset's codes are small numbers, so the low byte of
// a two-byte code is often itself a key — and produces words that were never
// in the document.
func TestCompositeFontIsReadTwoBytesAtATime(t *testing.T) {
	// The codes are 0x0141 and 0x0120. Their low bytes, 0x41 and 0x20, are
	// themselves keys in the same table mapping to something else entirely —
	// which is the accident that made a byte-at-a-time reading produce
	// plausible words that were never in the document.
	f := font{
		twoByte:   true,
		codeWidth: 2,
		toUnicode: map[rune]rune{
			0x0141: 'H', 0x0120: 'i',
			0x41: 'X', 0x20: 'Y',
		},
	}

	if got := decodeString("\x01\x41\x01\x20", f); got != "Hi" {
		t.Errorf("decoded %q, want %q — the low bytes must not be consulted", got, "Hi")
	}

	// The same bytes read one at a time, which is what the table's own
	// declared width prevents.
	narrow := f
	narrow.codeWidth = 1
	if got := decodeString("\x01\x41\x01\x20", narrow); got != "XY" {
		t.Fatalf("the fixture does not distinguish the two readings: got %q", got)
	}
}

func TestParseCMapReadsItsOwnCodeWidth(t *testing.T) {
	twoByte := []byte(`
		/CIDInit /ProcSet findresource begin
		1 begincodespacerange
		<0000> <FFFF>
		endcodespacerange
		2 beginbfchar
		<0024> <0041>
		<0003> <0020>
		endbfchar
	`)
	table, width := parseCMap(twoByte)
	if width != 2 {
		t.Errorf("width %d, want 2 from the declared codespace", width)
	}
	if table[0x0024] != 'A' {
		t.Errorf("table[0x24] = %q, want A", table[0x0024])
	}

	oneByte := []byte(`
		1 begincodespacerange
		<00> <FF>
		endcodespacerange
		1 beginbfchar
		<41> <0041>
		endbfchar
	`)
	if _, width := parseCMap(oneByte); width != 1 {
		t.Errorf("width %d, want 1", width)
	}
}

func TestParseCMapRanges(t *testing.T) {
	table, _ := parseCMap([]byte(`
		1 beginbfrange
		<0003> <0005> <0041>
		endbfrange
	`))
	for code, want := range map[rune]rune{0x03: 'A', 0x04: 'B', 0x05: 'C'} {
		if table[code] != want {
			t.Errorf("table[%#x] = %q, want %q", code, table[code], want)
		}
	}
}

func TestCleanCollapsesPositionalWhitespace(t *testing.T) {
	// PDF text operators emit position-driven spacing, so a table arrives with
	// runs of dozens of spaces and a tokenizer would index the gaps.
	got := clean("Invoice      4815\n\n\n\nTotal\t\t92.50  ")
	if got != "Invoice 4815\nTotal 92.50" {
		t.Errorf("clean produced %q", got)
	}
}

// --- fixtures ---------------------------------------------------------------

// buildPDF writes a minimal one-page PDF showing some text, with the content
// stream compressed the way a real producer would.
func buildPDF(t *testing.T, text string) []byte {
	t.Helper()

	content := "BT /F1 12 Tf 72 720 Td"
	if text != "" {
		content += fmt.Sprintf(" (%s) Tj", text)
	}
	content += " ET"

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	w.Close()

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	b.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	b.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /Contents 4 0 R " +
		"/Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	fmt.Fprintf(&b, "4 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", compressed.Len())
	b.Write(compressed.Bytes())
	b.WriteString("\nendstream\nendobj\n")
	b.WriteString("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica " +
		"/Encoding /WinAnsiEncoding >>\nendobj\n")
	b.WriteString("trailer\n<< /Root 1 0 R >>\n%%EOF\n")
	return b.Bytes()
}

func pageText(r Result) string {
	var b strings.Builder
	for _, p := range r.Pages {
		b.WriteString(p.Text)
		b.WriteByte('\n')
	}
	return b.String()
}
