package importer

import (
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

// SplitEMLX separates an Apple .emlx container into its message and metadata.
//
// The format is a decimal byte count on the first line, that many bytes of
// RFC822, then an Apple property list:
//
//	1847
//	Return-Path: <...>
//	...
//	<?xml version="1.0" ...><plist ...>
//
// Reports false when the input is not an .emlx, so a caller handed a plain .eml
// can pass it straight through.
func SplitEMLX(data []byte) (rfc822, plist []byte, ok bool) {
	newline := bytes.IndexByte(data, '\n')
	if newline <= 0 || newline > 24 {
		// The first line of an .emlx is a short number. Anything longer is a
		// header line, which means this is a plain message.
		return nil, nil, false
	}

	count, err := strconv.Atoi(strings.TrimSpace(string(data[:newline])))
	if err != nil || count <= 0 {
		return nil, nil, false
	}

	start := newline + 1
	end := start + count
	if end > len(data) {
		// Truncated — the file is being written, or was copied mid-flight.
		// Take what is there rather than failing outright.
		end = len(data)
	}
	return data[start:end], data[end:], true
}

// emlxPlistFlags digs the `flags` integer out of an .emlx trailing plist.
//
// The plist is a flat <dict> of alternating <key>/value elements, so rather
// than modelling Apple's format this walks the token stream and takes the
// integer that follows the flags key. Absent or malformed metadata yields
// (0, false), which callers treat as "no flags known" rather than "no flags".
func emlxPlistFlags(plist []byte) (int64, bool) {
	decoder := xml.NewDecoder(bytes.NewReader(plist))

	var lastKey string
	var inKey, wantValue bool

	for {
		token, err := decoder.Token()
		if err != nil {
			return 0, false
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "key":
				inKey = true
				lastKey = ""
			case "integer":
				if wantValue {
					var text string
					if err := decoder.DecodeElement(&text, &t); err != nil {
						return 0, false
					}
					n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
					return n, err == nil
				}
			}
		case xml.CharData:
			if inKey {
				lastKey += string(t)
			}
		case xml.EndElement:
			if t.Name.Local == "key" {
				inKey = false
				wantValue = strings.EqualFold(strings.TrimSpace(lastKey), "flags")
			}
		}
	}
}

// Apple's .emlx flag bits.
//
// Reverse-engineered rather than documented, so only the two bits that are
// well attested and that InboxQL has any use for are decoded. Everything else is
// left alone rather than guessed at.
const (
	emlxFlagRead    = 1 << 0
	emlxFlagFlagged = 1 << 4
)

// EMLXFlags converts an .emlx trailing plist into IMAP-style flag strings.
func EMLXFlags(plist []byte) []string {
	bits, ok := emlxPlistFlags(plist)
	if !ok {
		return nil
	}
	var flags []string
	if bits&emlxFlagRead != 0 {
		flags = append(flags, `\Seen`)
	}
	if bits&emlxFlagFlagged != 0 {
		flags = append(flags, `\Flagged`)
	}
	return flags
}

// AccumulateStats folds one parsed message into a deep scan's running totals.
//
// Shared by every source so the numbers on screen mean the same thing whichever
// client they came from.
func AccumulateStats(stats *Stats, msg *message.Message, raw []byte, contacts map[string]struct{}) {
	if msg == nil {
		stats.Unreadable++
		return
	}

	if !msg.Date.IsZero() {
		if stats.Oldest.IsZero() || msg.Date.Before(stats.Oldest) {
			stats.Oldest = msg.Date
		}
		if stats.Newest.IsZero() || msg.Date.After(stats.Newest) {
			stats.Newest = msg.Date
		}
	}

	seen := false
	for _, f := range msg.Flags {
		if strings.EqualFold(f, `\Seen`) {
			seen = true
		}
	}
	if !seen {
		stats.Unread++
	}

	for _, addr := range append([]string{msg.From}, append(msg.To, msg.Cc...)...) {
		if addr = strings.ToLower(strings.TrimSpace(addr)); addr != "" {
			contacts[addr] = struct{}{}
		}
	}

	count, bytes := CountAttachments(raw)
	stats.Attachments += count
	stats.AttachmentBytes += bytes
}

// DuplicatePreview counts how many of the given Message-IDs an account already
// holds.
//
// Matching on Message-ID rather than content hash is deliberate: it needs only
// the headers, which is an order of magnitude cheaper than parsing every body,
// and it is accurate enough for a number whose job is to inform a decision
// rather than drive one.
func DuplicatePreview(accountID string, messageIDs []string) (int, error) {
	found := 0
	for _, id := range messageIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		exists, err := store.MessageExistsByMessageIDForAccount(accountID, id)
		if err != nil {
			return found, err
		}
		if exists {
			found++
		}
	}
	return found, nil
}
