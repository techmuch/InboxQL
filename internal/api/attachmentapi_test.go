package api

import (
	"mime"
	"strings"
	"testing"
)

// The allowlist is the whole defence for "what will the browser do with this".
// A type that is not on it must not merely be discouraged from rendering — it
// must not be described to the browser as something renderable at all.
func TestSafeContentTypeAllowlist(t *testing.T) {
	cases := []struct {
		stored   string
		wantType string
		inline   bool
		why      string
	}{
		{"application/pdf", "application/pdf", true, "previewing a mailed PDF is the point of previewing"},
		{"image/png", "image/png", true, "a real image"},
		{"image/jpeg", "image/jpeg", true, "a real image"},
		{"text/plain", "text/plain; charset=utf-8", true, "a charset, so nothing is guessed"},

		{"text/html", "application/octet-stream", false, "HTML from a stranger never renders on this origin"},
		{"image/svg+xml", "application/octet-stream", false, "SVG is a document that can carry script"},
		{"application/xhtml+xml", "application/octet-stream", false, "the same problem, different type"},
		{"text/xml", "application/octet-stream", false, "and again"},
		{"application/javascript", "application/octet-stream", false, "obviously"},
		{"application/octet-stream", "application/octet-stream", false, "already opaque"},
		{"application/vnd.ms-excel", "application/octet-stream", false, "a real document, but not one to render"},
		{"", "application/octet-stream", false, "nothing recorded means nothing vouched for"},
		{"nonsense", "application/octet-stream", false, "unparseable is not a licence"},
	}

	for _, tc := range cases {
		gotType, gotInline := safeContentType(tc.stored)
		if gotType != tc.wantType || gotInline != tc.inline {
			t.Errorf("safeContentType(%q) = (%q, %v), want (%q, %v) — %s",
				tc.stored, gotType, gotInline, tc.wantType, tc.inline, tc.why)
		}
	}
}

// A stored type carrying parameters must not smuggle them into the response.
// A charset chosen by the sender is a charset chosen by an attacker.
func TestSafeContentTypeDropsParameters(t *testing.T) {
	got, inline := safeContentType("text/plain; charset=utf-7")
	if !inline {
		t.Fatalf("text/plain with a parameter stopped being inlineable: %q", got)
	}
	if strings.Contains(got, "utf-7") {
		t.Errorf("served %q; a sender's charset must not survive into the response", got)
	}
	if got != "text/plain; charset=utf-8" {
		t.Errorf("served %q, want text/plain; charset=utf-8", got)
	}

	// A type that is only allowlisted after its parameters are stripped must
	// still be recognised — otherwise every real mail client's "image/png;
	// name=x.png" would be forced to download.
	if got, inline := safeContentType("image/png; name=logo.png"); !inline || got != "image/png" {
		t.Errorf("safeContentType with a name parameter = (%q, %v), want (image/png, true)", got, inline)
	}

	// Case is not a way past the list either.
	if got, inline := safeContentType("IMAGE/PNG"); !inline || got != "image/png" {
		t.Errorf("safeContentType(IMAGE/PNG) = (%q, %v), want (image/png, true)", got, inline)
	}
	if got, _ := safeContentType("TEXT/HTML"); got != "application/octet-stream" {
		t.Errorf("safeContentType(TEXT/HTML) = %q; case must not open the door", got)
	}
}

// The filename comes from a mail header, which is to say from a stranger. A
// newline in it is response splitting; a path in it is a write outside the
// downloads folder.
func TestContentDispositionNeutralisesHostileNames(t *testing.T) {
	hostile := []struct {
		name string
		why  string
	}{
		{"report\r\nSet-Cookie: session=stolen", "CRLF is response splitting"},
		{"report\nX-Injected: yes", "a bare LF is too"},
		{"../../../etc/passwd", "a path is not a filename"},
		{`..\..\windows\system32\evil.exe`, "nor is a Windows path"},
		{"quote\"; filename=\"evil.exe", "a quote would end the parameter early"},
		{".bashrc", "a leading dot hides the download"},
		{"", "no name at all"},
	}

	for _, tc := range hostile {
		got := contentDisposition("attachment", tc.name)

		if strings.ContainsAny(got, "\r\n") {
			t.Errorf("contentDisposition(%q) = %q, which contains a newline — %s", tc.name, got, tc.why)
		}

		// The real assertion is structural, not textual: whatever the name
		// becomes, the header must parse back as one disposition with one
		// filename. Substring checks would flag the backslash of a correctly
		// escaped quote and miss an actually-broken value.
		kind, params, err := mime.ParseMediaType(got)
		if err != nil {
			t.Errorf("contentDisposition(%q) = %q, which does not parse: %v — %s", tc.name, got, err, tc.why)
			continue
		}
		if kind != "attachment" {
			t.Errorf("contentDisposition(%q) parsed as %q, want attachment — %s", tc.name, kind, tc.why)
		}
		filename := params["filename"]
		if strings.ContainsAny(filename, `/\";`) {
			t.Errorf("contentDisposition(%q) yielded filename %q, still carrying a separator or delimiter — %s",
				tc.name, filename, tc.why)
		}
		if strings.HasPrefix(filename, ".") {
			t.Errorf("contentDisposition(%q) yielded a hidden filename %q — %s", tc.name, filename, tc.why)
		}
	}
}

// An ordinary name must survive intact — a sanitiser that mangles
// "Invoice #123 (final).pdf" has made downloads worse to fix nothing.
func TestContentDispositionKeepsOrdinaryNames(t *testing.T) {
	for _, name := range []string{
		"invoice.pdf",
		"Invoice #123 (final).pdf",
		"Adobe Scan Nov 28, 2023 (1).pdf",
		"gtri_fmla_leave_request_form_for_self 2023.pdf",
	} {
		got := contentDisposition("attachment", name)
		if !strings.Contains(got, name) {
			t.Errorf("contentDisposition(%q) = %q, which lost the name", name, got)
		}
	}
}

// A non-ASCII name gets the RFC 5987 form alongside the quoted one, so it
// survives rather than being mangled or dropped.
func TestContentDispositionEncodesNonASCII(t *testing.T) {
	got := contentDisposition("attachment", "rapport-café.pdf")
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Errorf("contentDisposition of a non-ASCII name = %q, want an RFC 5987 form too", got)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("contentDisposition produced a newline: %q", got)
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"invoice.pdf":         "invoice.pdf",
		"../../etc/passwd":    "_.._etc_passwd",
		"with\x00null.pdf":    "withnull.pdf",
		"trailing space.pdf ": "trailing space.pdf",
		"...":                 "",
		"/":                   "_",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}

	// A name long enough to be a denial-of-service on someone's filesystem is
	// truncated rather than rejected: the download still works, with a shorter
	// name.
	long := strings.Repeat("a", 5000)
	if got := sanitizeFilename(long); len(got) > 200 {
		t.Errorf("sanitizeFilename left a %d-character name", len(got))
	}
}
