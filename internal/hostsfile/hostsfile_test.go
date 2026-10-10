package hostsfile

import (
	"strings"
	"testing"
)

const sample = "127.0.0.1\tlocalhost\n255.255.255.255\tbroadcasthost\n::1\tlocalhost\n"

func TestApplyAddsReplacesAndRemovesOnlyItsBlock(t *testing.T) {
	once := Apply(sample, "inboxql.localhost")
	if !strings.HasPrefix(once, sample) {
		t.Fatalf("the existing lines were changed:\n%s", once)
	}
	if Current(once) != "inboxql.localhost" {
		t.Errorf("current = %q", Current(once))
	}

	twice := Apply(once, "mail.test")
	if strings.Count(twice, beginMarker) != 1 || Current(twice) != "mail.test" {
		t.Errorf("replacing left two blocks or the old name:\n%s", twice)
	}

	if gone := Apply(twice, ""); gone != sample {
		t.Errorf("removing did not restore the file exactly:\n%q\nwant\n%q", gone, sample)
	}
}

// A begin marker with no end is a hand edit; the file is left alone rather
// than truncated at a guess.
func TestAnUnclosedBlockIsLeftAlone(t *testing.T) {
	broken := sample + beginMarker + "\n127.0.0.1\tsomething\n10.0.0.1\tprinter\n"
	if Strip(broken) != broken {
		t.Error("an unclosed block was stripped, taking the lines after it")
	}
}

func TestCheck(t *testing.T) {
	for _, ok := range []string{"inboxql.localhost", "mail.test", "inbox.internal", "inboxql"} {
		if w, err := Check(ok); err != nil || w != "" {
			t.Errorf("%s: warning %q, err %v", ok, w, err)
		}
	}
	if _, err := Check("inboxql.local"); err == nil {
		t.Error(".local was accepted; macOS resolves it over mDNS first")
	}
	if _, err := Check("not a name"); err == nil {
		t.Error("a name with spaces was accepted")
	}
	if w, err := Check("inbox.com"); err != nil || w == "" {
		t.Errorf("a real domain should be allowed with a warning: %q, %v", w, err)
	}
}
