package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// # The rail, as something other than a component
//
// The rail was three lists with three natures, all written inline: the folders,
// whatever had been saved, and the annotators that had found something. Only
// the middle one was ever editable, and only by creating and deleting.
//
// These are the parts that are not saved queries.

// RailFolders are the mailbox's own rows, in the order they are shown.
//
// Not user content: they carry live unread counts and are wired to the folder
// facet rather than to a query somebody wrote. So they can be hidden and they
// cannot be deleted — a rail with no Inbox and no obvious way back is a bad
// afternoon, and the badges are half the point of these rows.
var RailFolders = []string{"inbox", "starred", "sent", "drafts", "spam", "trash"}

const hiddenFoldersSetting = "rail.hiddenFolders"

// HiddenFolders are the mailbox rows a person has turned off.
func HiddenFolders() []string {
	raw, err := GetSetting(hiddenFoldersSetting)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

// SetFolderHidden shows or hides one mailbox row.
func SetFolderHidden(folder string, hidden bool) error {
	folder = strings.ToLower(strings.TrimSpace(folder))
	if !validFolder(folder) {
		return fmt.Errorf("%q is not a mailbox folder (%s)",
			folder, strings.Join(RailFolders, ", "))
	}

	keep := make([]string, 0, len(RailFolders))
	for _, f := range HiddenFolders() {
		if f != folder {
			keep = append(keep, f)
		}
	}
	if hidden {
		keep = append(keep, folder)
	}

	blob, err := json.Marshal(keep)
	if err != nil {
		return err
	}
	return UpdateSetting(hiddenFoldersSetting, string(blob))
}

// ShowAllFolders is the way back.
//
// Every hiding feature needs one. Somebody who has hidden the row they now want
// should not have to remember which it was, or find the setting that holds the
// list.
func ShowAllFolders() error {
	return UpdateSetting(hiddenFoldersSetting, "[]")
}

func validFolder(f string) bool {
	for _, known := range RailFolders {
		if known == f {
			return true
		}
	}
	return false
}
