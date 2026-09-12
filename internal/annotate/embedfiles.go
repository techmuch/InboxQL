package annotate

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/store"
)

// EmbedAttachments embeds the files a model has not embedded yet.
//
// # Why this is separate from EmbedMessages rather than a scope on it
//
// They embed different things into different tables and answer different
// questions. A message's vector is over its subject and body; a file's is over
// the text extraction or OCR recovered from inside it, with the filename
// leading. Folding them together would mean one table where a `similar:` over
// mail could return a file, which is not what either question asks.
//
// What they do share is every rule about how a model is chosen and consented
// to, which is why the checks below are the same checks, in the same order.
func EmbedAttachments(ctx context.Context, profileName string, limit int, dryRun bool) (*EmbedOutcome, error) {
	profile, err := store.ResolveLLMProfile(profileName)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("no model profile configured; see `iql llm profile list`")
	}
	if profile.Purpose != store.PurposeEmbedding {
		return nil, fmt.Errorf(
			"profile %q is for %s, not embedding.\n"+
				"Add one with: iql llm profile add <name> --purpose embedding --model <embedding model>",
			profile.Name, profile.Purpose)
	}

	out := &EmbedOutcome{Profile: profile.Name, Model: profile.Model, DryRun: dryRun}

	pending, err := store.PendingAttachmentEmbeddings(profile.Model, limit)
	if err != nil {
		return nil, err
	}
	out.Pending = int64(len(pending))
	if dryRun || len(pending) == 0 {
		for _, p := range pending {
			out.Chars += int64(len(p.Text))
		}
		return out, nil
	}

	// The same single decision about one body of material as the message side
	// makes, for the same reason — except that what would be sent here is the
	// contents of documents, which if anything raises the stakes.
	if profile.IsRemote() {
		return nil, fmt.Errorf(
			"profile %q sends text to %s.\n"+
				"Embedding every file's contents is a bulk send; point it at a local "+
				"profile, or say so explicitly with --allow-remote",
			profile.Name, profile.Endpoint)
	}

	return embedFileBatch(ctx, profile, pending, out)
}

// EmbedAttachmentsRemote is the same with consent already given.
func EmbedAttachmentsRemote(ctx context.Context, profileName string, limit int) (*EmbedOutcome, error) {
	profile, err := store.ResolveLLMProfile(profileName)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("no model profile configured; see `iql llm profile list`")
	}
	if profile.Purpose != store.PurposeEmbedding {
		return nil, fmt.Errorf("profile %q is for %s, not embedding", profile.Name, profile.Purpose)
	}

	out := &EmbedOutcome{Profile: profile.Name, Model: profile.Model}
	pending, err := store.PendingAttachmentEmbeddings(profile.Model, limit)
	if err != nil {
		return nil, err
	}
	out.Pending = int64(len(pending))
	if len(pending) == 0 {
		return out, nil
	}
	return embedFileBatch(ctx, profile, pending, out)
}

func embedFileBatch(
	ctx context.Context,
	profile *store.LLMProfile,
	pending []store.AttachmentEmbeddingInput,
	out *EmbedOutcome,
) (*EmbedOutcome, error) {
	const batch = 8
	for start := 0; start < len(pending); start += batch {
		end := start + batch
		if end > len(pending) {
			end = len(pending)
		}
		chunk := pending[start:end]

		texts := make([]string, 0, len(chunk))
		kept := make([]store.AttachmentEmbeddingInput, 0, len(chunk))
		for _, p := range chunk {
			if strings.TrimSpace(p.Text) == "" {
				out.Skipped++
				continue
			}
			texts = append(texts, p.Text)
			kept = append(kept, p)
			out.Chars += int64(len(p.Text))
		}
		if len(texts) == 0 {
			continue
		}

		vectors, err := llm.Embed(ctx, profile.Endpoint, profile.APIKey, profile.Provider,
			profile.Model, texts)
		if err != nil {
			return out, fmt.Errorf("embedding stopped after %d: %w", out.Embedded, err)
		}
		if len(vectors) != len(kept) {
			return out, fmt.Errorf(
				"asked for %d vectors and got %d; refusing to guess which is which",
				len(kept), len(vectors))
		}

		for i, p := range kept {
			if err := store.SaveAttachmentEmbedding(&store.AttachmentEmbedding{
				ContentHash: p.ContentHash, Profile: profile.Name, Model: profile.Model,
				Vector: vectors[i], Characters: len(p.Text),
			}); err != nil {
				return out, err
			}
			out.Embedded++
		}
	}
	return out, nil
}
