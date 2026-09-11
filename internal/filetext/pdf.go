package filetext

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// # Reading text out of a PDF, without a PDF library
//
// This is a deliberate subset, not an attempt at a PDF implementation. What it
// does is find the content streams, decompress them, and read the text-showing
// operators — which is enough for the PDFs that mail actually carries: things
// produced by word processors, browsers printing to PDF, and web services
// generating receipts and statements.
//
// # Why not a dependency
//
// InboxQL ships as one self-contained binary. A PDF library is a large,
// historically vulnerability-prone surface to run over bytes that arrive from
// strangers, and the alternative — shelling out to pdftotext — trades a
// dependency for a runtime requirement that may not be installed. The subset
// below parses no fonts, executes nothing, and treats every input as hostile
// data with bounded work.
//
// # What it will not read, and says so
//
//   - Encrypted PDFs. Reported as failed with the reason.
//   - Scanned pages. There is no text to find, and the result is
//     [StatusEmpty], which is the fact a later OCR pass needs.
//   - Text in fonts with a custom encoding and no ToUnicode map. The bytes are
//     glyph indices with no way back to characters; such runs are dropped
//     rather than indexed as mojibake, which would poison search with
//     nonsense words that match nothing anyone types.
//
// Being narrow is the point: a wrong answer here is invisible, because nobody
// reads the index directly. The failure would show up as searches that quietly
// miss, which is the failure this whole feature exists to remove.
//
// # The one case that still gets through
//
// A file whose own ToUnicode map is wrong. One PDF in the mailbox this was
// built against declares a map that turns its glyphs into plausible-looking
// lowercase runs, and applying the file's own stated mapping faithfully is the
// only thing an extractor can do — detecting the lie would mean rendering the
// page and reading it, which is OCR. The cost is a few junk tokens in the
// index, which match nothing; it is not a wrong answer to any question.

// maxPDFWork bounds the parsing of one file.
//
// The input is attacker-controlled. A PDF can nest, self-reference, and
// declare enormous decompressed sizes; without a ceiling, one mailed file
// turns an extraction pass into an outage.
const (
	maxPDFObjects       = 50_000
	maxStreamBytes      = 64 << 20 // decompressed, per stream
	maxTotalTextBytes   = 16 << 20 // per document
	maxPagesToExtract   = 2_000
	maxOperandsPerArray = 20_000
)

// extractPDF pulls the text out of a PDF, a page at a time.
func extractPDF(data []byte) Result {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		// Some producers put junk before the header. Find it rather than
		// refusing: a leading byte-order mark should not cost the whole file.
		if i := bytes.Index(data, []byte("%PDF-")); i > 0 && i < 1024 {
			data = data[i:]
		} else {
			return Result{Status: StatusFailed, Extractor: "pdf", Detail: "not a PDF"}
		}
	}

	doc, err := parsePDF(data)
	if err != nil {
		return Result{Status: StatusFailed, Extractor: "pdf", Detail: err.Error()}
	}
	if doc.encrypted {
		return Result{
			Status: StatusFailed, Extractor: "pdf",
			Detail: "the document is encrypted",
		}
	}

	pages := doc.textByPage()
	total := 0
	out := make([]Page, 0, len(pages))
	for _, p := range pages {
		if p.Text == "" {
			continue
		}
		total += len(p.Text)
		if total > maxTotalTextBytes {
			break
		}
		out = append(out, p)
	}

	if len(out) == 0 {
		return Result{
			Status: StatusEmpty, Extractor: "pdf",
			// The wording matters: this is the set an OCR pass exists for, and
			// somebody reading `iql doctor` should be able to tell that these
			// files are not broken.
			Detail: "no text layer — the pages are images",
		}
	}
	return Result{Status: StatusOK, Extractor: "pdf", Pages: out}
}

// pdfObject is one indirect object's raw body, plus its stream if it has one.
type pdfObject struct {
	dict   string
	stream []byte
}

type pdfDoc struct {
	objects   map[int]*pdfObject
	encrypted bool
}

var objHeader = regexp.MustCompile(`(?s)(\d+)\s+(\d+)\s+obj\b`)

// parsePDF collects every indirect object by scanning for them.
//
// # Why not follow the cross-reference table
//
// Because a scan works on files whose xref is wrong, and real mail is full of
// them — PDFs assembled by concatenation, truncated by a mail gateway, or
// written by a producer that miscounted. The xref is an index for random
// access, which is what a viewer needs and a text extractor does not: reading
// everything once is both simpler and more tolerant.
func parsePDF(data []byte) (*pdfDoc, error) {
	doc := &pdfDoc{objects: make(map[int]*pdfObject)}

	if i := bytes.Index(data, []byte("/Encrypt")); i >= 0 {
		// /Encrypt in the trailer means the streams are encrypted. Checking
		// for the token anywhere is coarse, but the false positive is a file
		// that reports as encrypted rather than one that returns nonsense.
		doc.encrypted = true
		return doc, nil
	}

	locations := objHeader.FindAllSubmatchIndex(data, maxPDFObjects)
	for i, loc := range locations {
		number, err := strconv.Atoi(string(data[loc[2]:loc[3]]))
		if err != nil {
			continue
		}

		body := data[loc[1]:]
		if end := bytes.Index(body, []byte("endobj")); end >= 0 {
			body = body[:end]
		} else if i+1 < len(locations) {
			// No endobj — truncated or malformed. Stop at the next object
			// header rather than reading the rest of the file as one object.
			body = data[loc[1]:locations[i+1][0]]
		}

		obj := &pdfObject{}
		if start := bytes.Index(body, []byte("stream")); start >= 0 {
			obj.dict = string(body[:start])
			obj.stream = rawStream(body[start:])
		} else {
			obj.dict = string(body)
		}
		doc.objects[number] = obj
	}

	if len(doc.objects) == 0 {
		return nil, fmt.Errorf("no objects found")
	}
	doc.expandObjectStreams()
	return doc, nil
}

// expandObjectStreams unpacks objects that were compressed into a stream.
//
// # Why this is not optional
//
// Since PDF 1.5 a producer may pack most non-stream objects — page
// dictionaries, resource dictionaries, font dictionaries — into an /ObjStm and
// compress the lot. A scanner that only looks for "N G obj" in the file sees
// none of them.
//
// The symptom is specific and misleading: page objects are still found, their
// /Resources still resolves to a number, and that number names an object that
// does not exist. So fonts silently resolve to nothing, every string is
// treated as directly readable, and the extractor confidently returns the
// glyph indices of a subset font as if they were text. Two files in the
// mailbox this was built against did exactly that, which is how this was
// found.
func (d *pdfDoc) expandObjectStreams() {
	for _, obj := range d.objects {
		if !strings.Contains(obj.dict, "/ObjStm") {
			continue
		}
		data := obj.decoded()
		if len(data) == 0 {
			continue
		}

		first := dictInt(obj.dict, "/First")
		count := dictInt(obj.dict, "/N")
		if first <= 0 || first > len(data) || count <= 0 || count > maxPDFObjects {
			continue
		}

		// The header is `count` pairs of (object number, offset from /First).
		header := strings.Fields(string(data[:first]))
		if len(header) < count*2 {
			count = len(header) / 2
		}

		for i := 0; i < count; i++ {
			num, err1 := strconv.Atoi(header[i*2])
			offset, err2 := strconv.Atoi(header[i*2+1])
			if err1 != nil || err2 != nil || offset < 0 || first+offset > len(data) {
				continue
			}
			// An object runs to the start of the next one, or to the end.
			end := len(data)
			if i+1 < count {
				if next, err := strconv.Atoi(header[i*2+3]); err == nil && first+next <= len(data) {
					end = first + next
				}
			}
			// A top-level object of the same number wins: it is a later
			// revision of the same thing, and an incremental update is how a
			// PDF says "this replaces that".
			if _, exists := d.objects[num]; exists {
				continue
			}
			d.objects[num] = &pdfObject{dict: string(data[first+offset : end])}
		}
	}
}

var dictIntPattern = regexp.MustCompile(`/(\w+)\s+(\d+)`)

// dictInt reads an integer value from a dictionary by key.
func dictInt(dict, key string) int {
	i := strings.Index(dict, key)
	if i < 0 {
		return 0
	}
	m := dictIntPattern.FindStringSubmatch(dict[i:])
	if m == nil || "/"+m[1] != key {
		return 0
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return 0
	}
	return n
}

// rawStream returns the bytes between `stream` and `endstream`.
func rawStream(body []byte) []byte {
	body = body[len("stream"):]
	// The keyword is followed by CRLF or LF, and nothing else is legal.
	switch {
	case bytes.HasPrefix(body, []byte("\r\n")):
		body = body[2:]
	case bytes.HasPrefix(body, []byte("\n")):
		body = body[1:]
	case bytes.HasPrefix(body, []byte("\r")):
		body = body[1:]
	}
	if end := bytes.LastIndex(body, []byte("endstream")); end >= 0 {
		body = body[:end]
	}
	return bytes.TrimRight(body, "\r\n")
}

// decoded returns a stream's bytes, inflating them when they are compressed.
func (o *pdfObject) decoded() []byte {
	if len(o.stream) == 0 {
		return nil
	}
	if !strings.Contains(o.dict, "FlateDecode") {
		// Uncompressed, or a filter this does not implement. Returning the
		// raw bytes is right for the first and harmless for the second: the
		// operator scan below finds nothing in bytes it cannot read.
		return o.stream
	}

	r, err := zlib.NewReader(bytes.NewReader(o.stream))
	if err != nil {
		return nil
	}
	defer r.Close()

	out, err := io.ReadAll(io.LimitReader(r, maxStreamBytes))
	if err != nil && len(out) == 0 {
		return nil
	}
	return out
}

var (
	contentsRef  = regexp.MustCompile(`/Contents\s+(?:(\d+)\s+\d+\s+R|\[([^\]]*)\])`)
	indirectRef  = regexp.MustCompile(`(\d+)\s+\d+\s+R`)
	toUnicodeRef = regexp.MustCompile(`/ToUnicode\s+(\d+)\s+\d+\s+R`)
	subsetFont   = regexp.MustCompile(`/BaseFont\s*/[A-Z]{6}\+`)
	fontRef      = regexp.MustCompile(`/([A-Za-z0-9]+)\s+(\d+)\s+\d+\s+R`)
)

// textByPage walks the pages and reads each one's content streams.
//
// Falls back to reading every stream when no page objects can be found, which
// happens with producers this scanner does not fully understand. A file whose
// text is attributed to page 0 is worse than one attributed correctly and much
// better than one that reports as empty while containing text.
func (d *pdfDoc) textByPage() []Page {
	var pages []Page

	numbers := d.pageObjects()
	for i, objNum := range numbers {
		if i >= maxPagesToExtract {
			break
		}
		page := d.objects[objNum]
		if page == nil {
			continue
		}

		fonts := d.pageFonts(page.dict)
		var text strings.Builder
		for _, contentNum := range contentStreams(page.dict) {
			content := d.objects[contentNum]
			if content == nil {
				continue
			}
			if s := extractStreamText(content.decoded(), fonts); s != "" {
				if text.Len() > 0 {
					text.WriteByte('\n')
				}
				text.WriteString(s)
			}
		}
		pages = append(pages, Page{Number: i + 1, Text: clean(text.String())})
	}

	if len(pages) > 0 {
		return pages
	}

	// No page tree this scanner could follow. Read everything that looks like
	// a content stream and attribute it to the document rather than to a page.
	var all strings.Builder
	for _, obj := range d.objects {
		if obj.stream == nil {
			continue
		}
		if s := extractStreamText(obj.decoded(), nil); s != "" {
			if all.Len() > 0 {
				all.WriteByte('\n')
			}
			all.WriteString(s)
		}
	}
	if text := clean(all.String()); text != "" {
		return []Page{{Number: 0, Text: text}}
	}
	return nil
}

// pageObjects lists the object numbers of /Type /Page objects, in file order.
//
// File order rather than tree order: following /Kids properly means resolving
// a tree that can nest and can lie, and producers write pages in order in
// practice. Getting page 7 labelled 8 is a smaller failure than not extracting
// it, and the text is what the search index needs.
func (d *pdfDoc) pageObjects() []int {
	var numbers []int
	for num, obj := range d.objects {
		if strings.Contains(obj.dict, "/Type") && strings.Contains(obj.dict, "/Page") &&
			!strings.Contains(obj.dict, "/Pages") {
			numbers = append(numbers, num)
		}
	}
	// Object numbers ascend with position for every producer worth supporting,
	// and a stable order beats map iteration either way.
	for i := 1; i < len(numbers); i++ {
		for j := i; j > 0 && numbers[j] < numbers[j-1]; j-- {
			numbers[j], numbers[j-1] = numbers[j-1], numbers[j]
		}
	}
	return numbers
}

// contentStreams lists the object numbers a page's /Contents points at.
func contentStreams(dict string) []int {
	m := contentsRef.FindStringSubmatch(dict)
	if m == nil {
		return nil
	}
	if m[1] != "" {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil
		}
		return []int{n}
	}

	var out []int
	for _, ref := range indirectRef.FindAllStringSubmatch(m[2], 64) {
		if n, err := strconv.Atoi(ref[1]); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// font is what is known about one font on a page.
type font struct {
	// toUnicode maps glyph codes to characters. Present whenever the producer
	// supplied a ToUnicode CMap, which is most of the time.
	toUnicode map[rune]rune
	// codeWidth is how many bytes one glyph code occupies, as the CMap itself
	// declares. One or two.
	codeWidth int
	// readable records whether this font's bytes mean anything without a map.
	//
	// A standard-encoded font's codes are essentially Latin-1 and can be read
	// directly. An embedded subset font's are glyph indices into that subset:
	// "Tech" comes out as "7HFK". That text looks plausible enough to index
	// and matches nothing anyone will ever type, so it is dropped instead —
	// see the note at the top of this file.
	readable bool
	// twoByte marks a composite font, whose codes are always two bytes.
	//
	// Knowing this is what stops a near-miss from becoming nonsense. A subset's
	// codes are small numbers, so the low byte of a two-byte code is often
	// itself a valid key in the same table: reading `<0003 0024>` one byte at a
	// time finds entries for 0x03 and 0x24 and produces a plausible-looking
	// word from two characters that were never there. A composite font is
	// therefore read two bytes at a time or not at all.
	twoByte bool
}

// pageFonts maps a page's font names to what is known about them.
//
// # Why the resource chain is followed properly
//
// The first version looked for /Font in the page dictionary. Almost no PDF
// puts it there: it is inside /Resources, which is usually an indirect object,
// and /Font inside that is often another. Missing one link meant no font was
// ever resolved, no ToUnicode table was ever applied, and every subset-font
// document extracted to convincing-looking gibberish. That failure is silent
// in exactly the way this feature is meant to prevent, which is why the chain
// is walked link by link rather than pattern-matched in one go.
func (d *pdfDoc) pageFonts(dict string) map[string]font {
	resources := d.resolveDict(dict, "/Resources")
	if resources == "" {
		return nil
	}
	fontDict := d.resolveDict(resources, "/Font")
	if fontDict == "" {
		return nil
	}

	fonts := map[string]font{}
	for _, m := range fontRef.FindAllStringSubmatch(fontDict, 128) {
		name, numText := m[1], m[2]
		num, err := strconv.Atoi(numText)
		if err != nil || d.objects[num] == nil {
			continue
		}
		fonts["/"+name] = d.readFont(d.objects[num].dict)
	}
	return fonts
}

// resolveDict finds a named sub-dictionary, following one indirect reference.
//
// Returns the dictionary's text, whether it was written inline or lives in its
// own object.
func (d *pdfDoc) resolveDict(dict, key string) string {
	i := strings.Index(dict, key)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(dict[i+len(key):])

	// An indirect reference is "N G R" and nothing else may precede it.
	if m := indirectRef.FindStringSubmatchIndex(rest); m != nil && m[0] == 0 {
		n, err := strconv.Atoi(rest[m[2]:m[3]])
		if err != nil || d.objects[n] == nil {
			return ""
		}
		return d.objects[n].dict
	}

	// Inline: take the balanced << >> that follows.
	if !strings.HasPrefix(rest, "<<") {
		return ""
	}
	depth := 0
	for j := 0; j+1 < len(rest); j++ {
		switch {
		case rest[j] == '<' && rest[j+1] == '<':
			depth++
			j++
		case rest[j] == '>' && rest[j+1] == '>':
			depth--
			j++
			if depth == 0 {
				return rest[:j+1]
			}
		}
	}
	return rest
}

// readFont works out how to turn one font's bytes into characters.
func (d *pdfDoc) readFont(dict string) font {
	f := font{}

	if ref := toUnicodeRef.FindStringSubmatch(dict); ref != nil {
		if n, err := strconv.Atoi(ref[1]); err == nil && d.objects[n] != nil {
			f.toUnicode, f.codeWidth = parseCMap(d.objects[n].decoded())
		}
	}

	// A named standard encoding settles it: the codes are character codes, and
	// they stay character codes whether or not the glyph program is embedded.
	//
	// This has to outrank the subset check below, which is the other way round
	// from how it first read. Subsetting describes which glyphs travel with the
	// file, not how they are addressed — so a subset font declaring
	// MacRomanEncoding is perfectly readable, and treating the subset tag as
	// decisive threw away a document that had extracted correctly a moment
	// earlier.
	declared := strings.Contains(dict, "/WinAnsiEncoding") ||
		strings.Contains(dict, "/MacRomanEncoding") ||
		strings.Contains(dict, "/StandardEncoding")

	switch {
	case declared:
		f.readable = true
	case !d.hasEmbeddedFont(dict):
		// A font the viewer has to supply is one of the standard fourteen,
		// addressed by standard codes.
		f.readable = true
	case subsetFont.MatchString(dict):
		// An embedded subset with no declared encoding: the codes are indices
		// into that subset and mean nothing outside it. The spec makes these
		// identifiable — a subset's BaseFont is tagged with six uppercase
		// letters and a plus, as in /ABCDEF+ArialMT.
		f.readable = false
	}

	// A composite font's codes are two bytes wide and never Latin-1, whatever
	// else the dictionary says.
	if strings.Contains(dict, "/Type0") || strings.Contains(dict, "/Identity-H") {
		f.readable = false
		f.twoByte = true
	}
	return f
}

// hasEmbeddedFont reports whether a font carries its own glyph program.
//
// The font file is referenced from the /FontDescriptor, not from the font
// dictionary, so looking for /FontFile in the latter always fails — which is
// how a subset font came to be treated as standard-encoded.
func (d *pdfDoc) hasEmbeddedFont(dict string) bool {
	if strings.Contains(dict, "/FontFile") {
		return true
	}
	descriptor := d.resolveDict(dict, "/FontDescriptor")
	if descriptor == "" {
		// A composite font keeps its descriptor one level further down, in the
		// descendant. Not resolving that is a false negative, which the subset
		// tag and the Type0 check below both catch.
		return false
	}
	return strings.Contains(descriptor, "/FontFile")
}

var (
	codespaceBlock = regexp.MustCompile(`(?s)begincodespacerange(.*?)endcodespacerange`)
	bfcharBlock    = regexp.MustCompile(`(?s)beginbfchar(.*?)endbfchar`)
	bfrangeBlock   = regexp.MustCompile(`(?s)beginbfrange(.*?)endbfrange`)
	hexPair        = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
	rangeEntry     = regexp.MustCompile(`<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>`)
)

// parseCMap reads a ToUnicode CMap into a glyph-code to rune table.
//
// Returns the table and how many bytes a code occupies. The width is not a
// guess: a CMap declares its own codespace, and reading it is what removes the
// need to try one width and then the other — a retry that produced real words
// out of codes that were never there, because a subset's two-byte codes have
// low bytes that are themselves valid keys in the same table.
func parseCMap(data []byte) (map[rune]rune, int) {
	if len(data) == 0 {
		return nil, 0
	}
	table := map[rune]rune{}
	text := string(data)

	// `begincodespacerange <0000> <FFFF> endcodespacerange` — the number of
	// hex digits in a bound is twice the code width.
	width := 0
	if m := codespaceBlock.FindStringSubmatch(text); m != nil {
		if bounds := hexPair.FindAllStringSubmatch(m[1], 2); len(bounds) > 0 {
			width = len(bounds[0][1]) / 2
		}
	}

	for _, block := range bfcharBlock.FindAllStringSubmatch(text, 64) {
		pairs := hexPair.FindAllStringSubmatch(block[1], maxOperandsPerArray)
		for i := 0; i+1 < len(pairs); i += 2 {
			from, ok1 := hexRune(pairs[i][1])
			to, ok2 := hexRune(pairs[i+1][1])
			if ok1 && ok2 {
				table[from] = to
				if width == 0 {
					// No declared codespace: the source codes are all written
					// to the same width, so the first one settles it.
					width = len(pairs[i][1]) / 2
				}
			}
		}
	}

	for _, block := range bfrangeBlock.FindAllStringSubmatch(text, 64) {
		for _, entry := range rangeEntry.FindAllStringSubmatch(block[1], maxOperandsPerArray) {
			lo, ok1 := hexRune(entry[1])
			hi, ok2 := hexRune(entry[2])
			dst, ok3 := hexRune(entry[3])
			if !ok1 || !ok2 || !ok3 || hi < lo || hi-lo > 0xFFFF {
				continue
			}
			for c := lo; c <= hi; c++ {
				table[c] = dst + (c - lo)
			}
			if width == 0 {
				width = len(entry[1]) / 2
			}
		}
	}

	if width < 1 || width > 2 {
		width = 1
	}
	return table, width
}

func hexRune(s string) (rune, bool) {
	if len(s) == 0 || len(s) > 8 {
		return 0, false
	}
	// A ToUnicode value can be a surrogate pair or a ligature expansion; the
	// first code point is the useful approximation for search.
	if len(s) > 4 {
		s = s[:4]
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return rune(n), true
}

// extractStreamText reads the text-showing operators out of a content stream.
//
// A content stream is a sequence of operands followed by an operator. This
// tracks only what is needed to recover words: the current font (so the right
// ToUnicode table applies), the strings passed to Tj/TJ/'/", and the
// positioning operators that imply a line break.
func extractStreamText(content []byte, fonts map[string]font) string {
	if len(content) == 0 {
		return ""
	}

	var out strings.Builder
	var current font
	// A font is assumed readable until one is selected, so a stream that shows
	// text without a Tf still produces something.
	current.readable = true
	haveFonts := len(fonts) > 0

	p := &streamScanner{data: content, perGlyph: positionsEveryGlyph(content)}
	for {
		token, kind, ok := p.next()
		if !ok {
			break
		}

		switch kind {
		case tokenString:
			p.pending = append(p.pending, token)

		case tokenName:
			p.lastName = token

		case tokenNumber:
			p.numbers = append(p.numbers, token)

		case tokenOperator:
			switch token {
			case "Tf":
				// `/F1 12 Tf` — the name selects the font and the number is
				// its size, which is what makes a horizontal gap measurable
				// below.
				if len(p.numbers) > 0 {
					if size, err := strconv.ParseFloat(p.numbers[len(p.numbers)-1], 64); err == nil && size > 0 {
						p.fontSize = size
					}
				}
				// The font operand precedes the operator, so the most recent
				// name is the font being selected.
				if haveFonts {
					f, known := fonts[p.lastName]
					if known {
						current = f
					} else {
						// A font the resource walk did not find. Assuming it is
						// readable is what produced gibberish before; assuming
						// it is not costs text only where the alternative was
						// text nobody could search for anyway.
						current = font{}
					}
				}
			case "Tj", "TJ", "'", "\"":
				for _, s := range p.pending {
					out.WriteString(decodeString(s, current))
				}
				if token == "'" || token == "\"" {
					out.WriteByte('\n')
				}
			case "Td", "TD":
				// # Why the operands decide, rather than the operator
				//
				// Td moves the text position by (tx, ty). Treating every one
				// as a line break turned documents that position each word
				// into one word per line; treating every horizontal move as a
				// space turned documents that position each *glyph* into
				// "A 3 0 - Y e a r", which tokenizes as single letters and so
				// matches no search anyone would type.
				//
				// So a vertical move is a line, and a horizontal move is a
				// space only when it is wide enough to be one.
				switch p.moveKind() {
				case moveLine:
					out.WriteByte('\n')
				case moveSpace:
					out.WriteByte(' ')
				}
			case "T*":
				out.WriteByte('\n')
			case "ET":
				out.WriteByte('\n')
			}
			p.pending = p.pending[:0]
			p.numbers = p.numbers[:0]

		case tokenOther:
			// Array delimiters. In a TJ array the numbers are kerning
			// adjustments; a large negative one is a word space, but the
			// tokenizer that reads the index collapses runs of spaces anyway.
		}

		if out.Len() > maxTotalTextBytes {
			break
		}
	}
	return out.String()
}

type tokenKind int

const (
	tokenOther tokenKind = iota
	tokenString
	tokenName
	tokenNumber
	tokenOperator
)

// streamScanner walks a content stream's tokens.
type streamScanner struct {
	data     []byte
	pos      int
	pending  []string
	numbers  []string
	lastName string
	// fontSize is the size from the most recent Tf, which is the yardstick a
	// horizontal move is measured against.
	fontSize float64
	// perGlyph records that this stream places each glyph individually, so
	// horizontal moves are advances rather than spaces. See
	// positionsEveryGlyph.
	perGlyph bool
}

// positionsEveryGlyph reports whether a stream draws one glyph at a time.
//
// # Why this has to be detected
//
// Some producers — Chrome's print-to-PDF among them — place every glyph
// absolutely: each Tj shows a single character and the Td before it carries
// that character's advance width. The gaps are therefore always about a
// glyph wide, which is wider than any threshold that would still catch real
// word spaces, so measuring them cannot separate the two.
//
// What saves it is that such a document emits its own space characters as
// glyphs. So the right reading is to ignore positioning entirely here and let
// the text speak: the alternative, and the first version's behaviour, was
// "A 3 0 - Y e a r T r a p", which indexes as single letters and matches
// nothing anyone would ever search for.
//
// Decided per stream rather than per document because a producer is
// consistent within one, and a scan of the operators is cheap next to
// inflating the stream in the first place.
func positionsEveryGlyph(content []byte) bool {
	s := &streamScanner{data: content}
	var shows, singles int

	for shows < 400 {
		token, kind, ok := s.next()
		if !ok {
			break
		}
		switch kind {
		case tokenString:
			s.pending = append(s.pending, token)
		case tokenOperator:
			if token == "Tj" || token == "TJ" || token == "'" || token == "\"" {
				shows++
				total := 0
				for _, str := range s.pending {
					total += len(str)
				}
				// One glyph: a single byte, or a two-byte code in a composite
				// font. Anything longer is a run of text.
				if total > 0 && total <= 2 {
					singles++
				}
			}
			s.pending = s.pending[:0]
		}
	}

	// A handful of single-glyph shows is normal in any document — a dropped
	// capital, a bullet. The pattern only means what it means when nearly
	// everything is drawn that way.
	return shows >= 20 && singles*10 >= shows*8
}

type moveKind int

const (
	moveNone moveKind = iota
	moveSpace
	moveLine
)

// wordGapRatio is the fraction of the font size that counts as a word space.
//
// A space glyph is around a quarter of the point size in most text faces, and
// inter-letter positioning is a small fraction of that. Anything in between is
// ambiguous either way; erring low keeps words apart, which is the direction
// that preserves search — two words run together match neither.
const wordGapRatio = 0.18

// moveKind classifies a pending Td/TD by its operands.
func (s *streamScanner) moveKind() moveKind {
	if len(s.numbers) < 2 {
		// Without operands there is nothing to judge, and a break is the safer
		// guess: joining two lines reads worse than splitting one.
		return moveLine
	}
	tx, errX := strconv.ParseFloat(s.numbers[len(s.numbers)-2], 64)
	ty, errY := strconv.ParseFloat(s.numbers[len(s.numbers)-1], 64)
	if errX != nil || errY != nil {
		return moveLine
	}
	if ty != 0 {
		return moveLine
	}
	if s.perGlyph {
		// Every gap here is a glyph advance, and the document supplies its own
		// spaces. Adding more would put one between every letter.
		return moveNone
	}

	gap := tx
	if gap < 0 {
		gap = -gap
	}
	size := s.fontSize
	if size <= 0 {
		size = 12 // a reasonable default when no Tf was seen
	}
	if gap >= size*wordGapRatio {
		return moveSpace
	}
	return moveNone
}

func (s *streamScanner) next() (string, tokenKind, bool) {
	for s.pos < len(s.data) && isPDFSpace(s.data[s.pos]) {
		s.pos++
	}
	if s.pos >= len(s.data) {
		return "", tokenOther, false
	}

	switch c := s.data[s.pos]; {
	case c == '(':
		return s.literalString(), tokenString, true
	case c == '<':
		if s.pos+1 < len(s.data) && s.data[s.pos+1] == '<' {
			s.pos += 2
			return "<<", tokenOther, true
		}
		return s.hexString(), tokenString, true
	case c == '/':
		return s.name(), tokenName, true
	case c == '%':
		for s.pos < len(s.data) && s.data[s.pos] != '\n' {
			s.pos++
		}
		return "", tokenOther, true
	case c == '[' || c == ']' || c == '>' || c == '{' || c == '}':
		s.pos++
		return string(c), tokenOther, true
	case isOperatorByte(c):
		return s.operator(), tokenOperator, true
	default:
		// A number, or anything else. Consume to the next delimiter.
		start := s.pos
		for s.pos < len(s.data) && !isPDFSpace(s.data[s.pos]) && !isDelimiter(s.data[s.pos]) {
			s.pos++
		}
		if s.pos == start {
			s.pos++
		}
		text := string(s.data[start:s.pos])
		if isNumeric(text) {
			return text, tokenNumber, true
		}
		return text, tokenOther, true
	}
}

// literalString reads a (...) string, honouring escapes and nesting.
func (s *streamScanner) literalString() string {
	s.pos++ // past '('
	var b strings.Builder
	depth := 1

	for s.pos < len(s.data) {
		c := s.data[s.pos]
		s.pos++

		switch c {
		case '\\':
			if s.pos >= len(s.data) {
				return b.String()
			}
			e := s.data[s.pos]
			s.pos++
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'b', 'f':
				b.WriteByte(' ')
			case '\n':
				// A backslash before a newline is a line continuation.
			case '\r':
				if s.pos < len(s.data) && s.data[s.pos] == '\n' {
					s.pos++
				}
			default:
				if e >= '0' && e <= '7' {
					// Octal, up to three digits including the one consumed.
					value := int(e - '0')
					for i := 0; i < 2 && s.pos < len(s.data); i++ {
						d := s.data[s.pos]
						if d < '0' || d > '7' {
							break
						}
						value = value*8 + int(d-'0')
						s.pos++
					}
					b.WriteByte(byte(value))
				} else {
					b.WriteByte(e)
				}
			}
		case '(':
			depth++
			b.WriteByte(c)
		case ')':
			depth--
			if depth == 0 {
				return b.String()
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// hexString reads a <...> string.
func (s *streamScanner) hexString() string {
	s.pos++ // past '<'
	start := s.pos
	for s.pos < len(s.data) && s.data[s.pos] != '>' {
		s.pos++
	}
	hex := s.data[start:s.pos]
	if s.pos < len(s.data) {
		s.pos++
	}

	var b strings.Builder
	var high byte
	var haveHigh bool
	for _, c := range hex {
		v, ok := hexDigit(c)
		if !ok {
			continue
		}
		if haveHigh {
			b.WriteByte(high<<4 | v)
			haveHigh = false
		} else {
			high, haveHigh = v, true
		}
	}
	if haveHigh {
		// An odd number of digits: the spec says pad with zero.
		b.WriteByte(high << 4)
	}
	return b.String()
}

func (s *streamScanner) name() string {
	start := s.pos
	s.pos++ // past '/'
	for s.pos < len(s.data) && !isPDFSpace(s.data[s.pos]) && !isDelimiter(s.data[s.pos]) {
		s.pos++
	}
	return string(s.data[start:s.pos])
}

func (s *streamScanner) operator() string {
	start := s.pos
	for s.pos < len(s.data) && isOperatorByte(s.data[s.pos]) {
		s.pos++
	}
	return string(s.data[start:s.pos])
}

// decodeString turns a PDF string's bytes into text.
//
// The order is the priority order. A ToUnicode table is authoritative when it
// covers the run. UTF-16BE announces itself with a byte-order mark. A font
// with a standard encoding is read as Latin-1, which those encodings are close
// enough to for search. Anything else is dropped rather than guessed at.
func decodeString(s string, f font) string {
	if s == "" {
		return ""
	}

	if len(f.toUnicode) > 0 {
		// The CMap declared how wide its codes are, so there is no guessing
		// and no retry at the other width. A code the map does not cover is
		// dropped: it is a glyph with no character, and inventing one is how
		// the index fills with words nobody can search for.
		width := f.codeWidth
		if width != 2 {
			width = 1
		}

		var b strings.Builder
		for i := 0; i+width <= len(s); i += width {
			code := rune(s[i])
			if width == 2 {
				code = code<<8 | rune(s[i+1])
			}
			if mapped, found := f.toUnicode[code]; found {
				b.WriteRune(mapped)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
		if !f.readable {
			return ""
		}
	}

	if len(s) >= 2 && s[0] == 0xFE && s[1] == 0xFF {
		var b strings.Builder
		for i := 2; i+1 < len(s); i += 2 {
			b.WriteRune(rune(s[i])<<8 | rune(s[i+1]))
		}
		return b.String()
	}

	if !f.readable {
		// An embedded subset font with no usable map. Its bytes are glyph
		// indices: emitting them would put words like "7HFK" in the index,
		// which is text that matches nothing and reports the file as read.
		return ""
	}

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteRune(rune(s[i]))
	}
	return b.String()
}

// isNumeric reports whether a token is a PDF number.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	digits := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits = true
		case c == '.' || c == '-' || c == '+':
		default:
			return false
		}
	}
	return digits
}

func isPDFSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

func isDelimiter(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func isOperatorByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '*' || c == '\'' || c == '"'
}

func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
