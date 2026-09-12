package store

import (
	"strings"
	"testing"
)

// A reasoning model answers in channels, and the raw completion can open with
// the model planning out loud. Stored as-is that text goes into the search
// index — so a scanned receipt becomes findable by "the user wants me to act
// as", which is what this was doing on the first real run.
func TestCleanOCRTextDropsTheModelsOwnVoice(t *testing.T) {
	raw := "<|channel|>thought<|message|>Here's a thinking process to arrive at the " +
		"desired output:\n1. Analyze the Request: the user wants me to transcribe.\n" +
		"<|channel|>final<|message|>KROGER\n1122 POWERS FERRY ROAD\nTOTAL 6.70"

	got := CleanOCRText(raw)
	if strings.Contains(got, "thinking process") || strings.Contains(got, "Analyze the Request") {
		t.Errorf("kept the model's reasoning: %q", got)
	}
	if !strings.Contains(got, "POWERS FERRY ROAD") {
		t.Errorf("lost the transcription: %q", got)
	}
	if strings.Contains(got, "<|") {
		t.Errorf("left control tokens in: %q", got)
	}
}

func TestCleanOCRTextStripsThinkTags(t *testing.T) {
	got := CleanOCRText("<think>I should read carefully.</think>\nInvoice 4815\nTotal 92.50")
	if strings.Contains(got, "should read") {
		t.Errorf("kept reasoning: %q", got)
	}
	if !strings.HasPrefix(got, "Invoice 4815") {
		t.Errorf("got %q, want the transcription", got)
	}
}

// Commentary with no channel markers is still commentary, and indexing it
// would make the mailbox searchable for phrases about the request.
func TestCleanOCRTextRejectsBareCommentary(t *testing.T) {
	for _, raw := range []string{
		"Here's the transcription of the image:\n\nKROGER",
		"I cannot read this image.",
		"The image contains no legible text.",
		"I'm sorry, but I can't help with that.",
		"",
		"   \n  ",
	} {
		if got := CleanOCRText(raw); got != "" {
			t.Errorf("CleanOCRText(%q) = %q, want it dropped", raw, got)
		}
	}
}

// A real page that happens to contain one of those phrases in its body must
// survive: discarding a genuine document is worse than the pollution avoided.
func TestCleanOCRTextKeepsRealPages(t *testing.T) {
	page := "TERMS OF SERVICE\n\nSection 4. Where the user wants to cancel, notice " +
		"must be given in writing within thirty days.\n\nSection 5. Refunds."

	got := CleanOCRText(page)
	if got != page {
		t.Errorf("CleanOCRText mangled a real page:\n got %q\nwant %q", got, page)
	}
}

func TestCleanOCRTextPlainTranscription(t *testing.T) {
	page := "MASSEY AUTOMOTIVE\nRepair Order # 0081759\n2050 LOWER ROSWELL ROAD"
	if got := CleanOCRText(page); got != page {
		t.Errorf("got %q, want it unchanged", got)
	}
}
