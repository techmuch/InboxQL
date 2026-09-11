package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/blobstore"
	"github.com/user/inboxql/internal/filetext"
)

// # Reading pictures of pages
//
// A scanned PDF holds no text: [ExtractAttachmentText] records it as `empty`,
// and that recorded fact is what makes this pass possible — the set of files
// worth looking at is already named.
//
// What reads them is a vision model, because that is what is available. It is
// worth being plain about what that means, because it is not the same thing as
// classical OCR and pretending otherwise would be the dishonest choice:
//
//   - A vision model transcribes and also invents. Run over a supermarket
//     receipt in the mailbox this was built against, it read the shop, the
//     street, the totals and the card savings correctly, misread "POWERS" as
//     "FOULERS", and added two lines of plausible loyalty-scheme text that
//     were not on the paper at all.
//   - For a search index that trade is usually worth taking: a document that
//     was completely unfindable becomes findable by most of the words really
//     on it. The cost is occasional matches on words that were never there.
//   - So OCR text is stored with its extractor recorded as "ocr", never merged
//     into the `pdf` reading, and every surface that shows it says where it
//     came from. A reader who sees a phrase attributed to a scan can then
//     check the page — which is exactly what the preview is for.
//
// The alternative was to leave these files unsearchable, which is the state
// this whole feature exists to end.

// OCRPrompt is what the model is asked to do.
//
// Deliberately narrow. Asking for a transcription rather than a description or
// a summary is what keeps the output close to the page; a model asked to
// "describe this document" writes about it instead of copying it, and what
// lands in the index is then commentary rather than content.
const OCRPrompt = "Transcribe every word of text visible in this image, exactly as it appears. " +
	"Preserve line breaks. Do not summarise, explain, translate or add anything. " +
	"If the image contains no legible text, reply with nothing at all."

// OCRSystem frames the task for models that take a system message.
const OCRSystem = "You are a careful transcription tool. You output only the text you can actually read."

// OCRResult reports what a pass did.
type OCRResult struct {
	Considered int `json:"considered"`
	Read       int `json:"read"`
	Illegible  int `json:"illegible"`
	NoImages   int `json:"noImages"`
	Failed     int `json:"failed"`
}

// OCRReader is the part of the vision provider this needs.
//
// An interface here rather than an import of llm so the store keeps out of the
// provider's business — the same reason the query compiler is handed its
// folder predicate rather than importing the package that defines it.
type OCRReader interface {
	Name() string
	ReadImage(ctx context.Context, mime string, data []byte) (string, error)
}

// OCRAttachments reads the files that hold no text layer.
//
// Only files already recorded as `empty` are considered: the pass that decided
// that is what makes this one cheap, and re-deciding here would mean reading
// every PDF in the mailbox again.
//
// maxEdge downscales page images before they are sent. A scanned page is
// commonly 6800x8800, which is several megabytes of base64 and far more pixels
// than any model attends to.
func OCRAttachments(
	ctx context.Context,
	blobs *blobstore.Store,
	reader OCRReader,
	maxEdge int,
	limit int,
	progress func(done, total int, filename string),
) (*OCRResult, error) {
	if blobs == nil {
		return nil, fmt.Errorf("reading scans needs the blob store")
	}
	if reader == nil {
		return nil, fmt.Errorf("reading scans needs a model that can see images")
	}
	if maxEdge <= 0 {
		maxEdge = 1600
	}

	hashes, err := ScansAwaitingOCR()
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(hashes) > limit {
		hashes = hashes[:limit]
	}

	out := &OCRResult{Considered: len(hashes)}
	for i, hash := range hashes {
		if err := ctx.Err(); err != nil {
			return out, err
		}

		name := attachmentName(hash)
		if progress != nil {
			progress(i+1, len(hashes), name)
		}

		data, err := blobs.Read(hash)
		if err != nil {
			out.Failed++
			continue
		}

		images, err := filetext.PageImages(data, 10000)
		if err != nil || len(images) == 0 {
			// A scan whose pages are in a codec this cannot lift out — CCITT
			// fax, JBIG2, JPEG 2000.
			//
			// The status stays `empty` rather than becoming `unsupported`,
			// because the status describes the file and the file is still a
			// scan with no text layer: `is:scanned` must keep naming it, or
			// asking "which of my files are pictures of pages" stops returning
			// the ones furthest out of reach. What stops this being retried
			// forever is the recorded extractor, not the status.
			detail := "a scan whose page images are in a format this cannot read"
			if err != nil {
				detail = err.Error()
			}
			if saveErr := SaveAttachmentText(hash, filetext.Result{
				Status: filetext.StatusEmpty, Extractor: "ocr", Detail: detail,
			}); saveErr != nil {
				return out, saveErr
			}
			out.NoImages++
			continue
		}

		pages := make([]filetext.Page, 0, len(images))
		for _, img := range images {
			small, err := filetext.Downscale(img.Data, maxEdge)
			if err != nil {
				small = img.Data
			}
			text, err := reader.ReadImage(ctx, img.MIME, small)
			if err != nil {
				if ctx.Err() != nil {
					return out, ctx.Err()
				}
				continue
			}
			if text = CleanOCRText(text); text == "" {
				continue
			}
			pages = append(pages, filetext.Page{Number: img.Page, Text: text})
		}

		result := filetext.Result{Extractor: "ocr", Pages: pages}
		switch {
		case len(pages) > 0:
			result.Status = filetext.StatusOK
			result.Detail = "read by " + reader.Name()
			out.Read++
		default:
			// The model looked and found nothing legible. Still a recorded
			// outcome: a blank scanned page is a fact, not a retry.
			result.Status = filetext.StatusEmpty
			result.Detail = reader.Name() + " found no legible text"
			out.Illegible++
		}

		if err := SaveAttachmentText(hash, result); err != nil {
			return out, err
		}
	}
	return out, nil
}

// CleanOCRText strips a model's own voice out of a transcription.
//
// # Why this is necessary and not fussiness
//
// A reasoning model answers in channels, and the raw completion can begin
// "<|channel|>thought Here's a thinking process to arrive at the desired
// output: 1. Analyze the Request..." before it gets to the page. Stored as-is,
// that text goes into the search index — so the mailbox becomes searchable for
// phrases like "the user wants me to act as", attributed to a scanned receipt.
// It was doing exactly that on the first run against real files.
//
// Two rules, in order:
//
//   - Where a final channel exists, only it is the answer. Everything before
//     it is the model thinking aloud.
//   - Whatever survives, if it reads as commentary about the task rather than
//     as a transcription, is dropped entirely. A page that yields nothing is
//     an honest outcome; a page that yields the model's opinion of the request
//     is pollution that no later pass will know to remove.
func CleanOCRText(raw string) string {
	text := raw

	// Harmony-style channels: the final one is the answer.
	if i := strings.LastIndex(text, "<|channel|>final"); i >= 0 {
		text = text[i:]
		if j := strings.Index(text, "<|message|>"); j >= 0 {
			text = text[j+len("<|message|>"):]
		}
	} else if i := strings.LastIndex(text, "<|message|>"); i >= 0 {
		text = text[i+len("<|message|>"):]
	}

	// Reasoning wrapped in tags rather than channels.
	for _, open := range []string{"<think>", "<thinking>", "<reasoning>"} {
		close := strings.Replace(open, "<", "</", 1)
		for {
			start := strings.Index(text, open)
			if start < 0 {
				break
			}
			end := strings.Index(text[start:], close)
			if end < 0 {
				text = text[:start]
				break
			}
			text = text[:start] + text[start+end+len(close):]
		}
	}

	// Any remaining control tokens.
	for {
		start := strings.Index(text, "<|")
		if start < 0 {
			break
		}
		end := strings.Index(text[start:], "|>")
		if end < 0 {
			text = text[:start]
			break
		}
		text = text[:start] + text[start+end+len("|>"):]
	}

	text = strings.TrimSpace(text)
	if looksLikeCommentary(text) {
		return ""
	}
	return text
}

// commentaryMarkers are phrases a transcription does not contain and a model
// talking about its instructions does.
var commentaryMarkers = []string{
	"thinking process",
	"the user wants",
	"the user is asking",
	"i will transcribe",
	"i need to transcribe",
	"as a transcription tool",
	"here's the transcription",
	"here is the transcription",
	"analyze the request",
	"i cannot",
	"i'm sorry",
	"no legible text",
	"no text is visible",
	"the image contains no",
}

// looksLikeCommentary reports whether text is the model talking rather than
// the page speaking.
//
// # Why only the first line
//
// A model that is going to comment does it immediately — that is what makes
// the tell usable. Scanning further finds the same phrases in real documents:
// a page of terms and conditions reading "Where the user wants to cancel,
// notice must be given" is a genuine page, and an earlier version of this
// discarded it. Losing a real document is a worse failure than keeping a line
// of commentary, because nothing downstream can tell that the document is
// missing.
func looksLikeCommentary(text string) bool {
	if text == "" {
		return true
	}

	head, _, _ := strings.Cut(text, "\n")
	head = strings.ToLower(strings.TrimSpace(head))
	if len(head) > 200 {
		head = head[:200]
	}

	for _, marker := range commentaryMarkers {
		if strings.Contains(head, marker) {
			return true
		}
	}
	return false
}

// attachmentName returns a filename for a hash, for progress reporting.
func attachmentName(hash string) string {
	var name string
	db.QueryRow(
		"SELECT COALESCE(filename, '') FROM attachments WHERE content_hash = ? LIMIT 1",
		hash).Scan(&name)
	return name
}

// OCRCandidates counts the files a pass would consider.
func OCRCandidates() (int, error) {
	hashes, err := ScansAwaitingOCR()
	return len(hashes), err
}
