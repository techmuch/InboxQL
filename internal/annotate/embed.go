package annotate

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/inboxql/internal/llm"
	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// EmbedOutcome reports what an embedding run did.
type EmbedOutcome struct {
	Profile  string `json:"profile"`
	Model    string `json:"model"`
	Embedded int64  `json:"embedded"`
	Skipped  int64  `json:"skipped"`
	Pending  int64  `json:"pending"`
	Chars    int64  `json:"chars"`
	DryRun   bool   `json:"dryRun"`
}

// EmbedMessages embeds whatever the named profile has not embedded yet.
//
// # Why the profile has to be an embedding profile
//
// Pointing this at a chat model produces a provider error per message with
// nothing useful in it. The purpose is recorded on the profile precisely so
// this can be a configuration error stated once.
//
// # Why it embeds the cleaned body
//
// Two thirds of a real archive is quoted and forwarded material, and it is
// longer than the new text. Embedding it clusters messages by the mail they
// are replying to — and for a forward, attributes the original's content to
// whoever forwarded it.
func EmbedMessages(ctx context.Context, profileName, scope string, limit int, dryRun bool) (*EmbedOutcome, error) {
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

	ids, err := store.PendingEmbeddings(profile.Model, scope, limit)
	if err != nil {
		return nil, err
	}
	out.Pending = int64(len(ids))
	if dryRun || len(ids) == 0 {
		// The dry run still reads the bodies, because the size of what would
		// be sent is the number a person needs before agreeing to send it.
		for _, id := range ids {
			if text, err := embeddableText(id); err == nil {
				out.Chars += int64(len(text))
			}
		}
		return out, nil
	}

	// Consent is the profile's, checked once rather than per message: this is
	// one decision about one body of mail.
	if profile.IsRemote() {
		return nil, fmt.Errorf(
			"profile %q sends message bodies to %s.\n"+
				"Embedding the mailbox is a bulk send; point it at a local profile, "+
				"or say so explicitly with --allow-remote",
			profile.Name, profile.Endpoint)
	}

	return embedBatch(ctx, profile, ids, out)
}

// EmbedMessagesRemote is EmbedMessages with consent given.
func EmbedMessagesRemote(ctx context.Context, profileName, scope string, limit int) (*EmbedOutcome, error) {
	profile, err := store.ResolveLLMProfile(profileName)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.Purpose != store.PurposeEmbedding {
		return nil, fmt.Errorf("profile %q is not an embedding profile", profileName)
	}
	ids, err := store.PendingEmbeddings(profile.Model, scope, limit)
	if err != nil {
		return nil, err
	}
	out := &EmbedOutcome{Profile: profile.Name, Model: profile.Model, Pending: int64(len(ids))}
	return embedBatch(ctx, profile, ids, out)
}

func embedBatch(ctx context.Context, profile *store.LLMProfile, ids []string, out *EmbedOutcome) (*EmbedOutcome, error) {
	const batch = 16
	for start := 0; start < len(ids); start += batch {
		end := start + batch
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		texts := make([]string, 0, len(chunk))
		kept := make([]string, 0, len(chunk))
		for _, id := range chunk {
			text, err := embeddableText(id)
			if err != nil || strings.TrimSpace(text) == "" {
				out.Skipped++
				continue
			}
			texts = append(texts, text)
			kept = append(kept, id)
			out.Chars += int64(len(text))
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

		for i, id := range kept {
			if err := store.SaveEmbedding(&store.Embedding{
				MessageID: id, Profile: profile.Name, Model: profile.Model,
				Vector: vectors[i],
			}); err != nil {
				return out, err
			}
			out.Embedded++
		}
	}
	return out, nil
}

// embeddableText is what a message is about, in as few characters as possible.
//
// Subject first: it is the one line a human wrote to summarise the rest, and
// putting it in front means a truncated body still carries it.
func embeddableText(messageID string) (string, error) {
	m, err := store.GetMessageByID(messageID)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", fmt.Errorf("no message %s", messageID)
	}

	body := message.CleanBody(m.Body)
	// A ceiling rather than chunking. Chunking multiplies storage and needs a
	// pooling decision this does not have evidence for yet; a truncated first
	// 8000 characters of a cleaned body is nearly always the whole thing.
	const maxChars = 8000
	if len(body) > maxChars {
		body = body[:maxChars]
	}
	return strings.TrimSpace(m.Subject + "\n\n" + body), nil
}
