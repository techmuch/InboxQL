package annotate

import (
	"strings"
	"testing"

	"github.com/user/inboxql/internal/store"
)

func TestAPlainLabelAsksYesOrNo(t *testing.T) {
	q := layaQuestion(&store.Annotator{Kind: store.KindLabel, Instructions: "expects a reply"})
	if q.Kind != "noul" || len(q.Options) != 0 {
		t.Errorf("got %+v, want a noul with no options", q)
	}
}

// Levels are stored best first and the model numbers them from the bottom, so
// "important" must be its highest level, not its lowest.
func TestALevelledLabelAsksForAScoreFromTheBottomUp(t *testing.T) {
	a := &store.Annotator{Kind: store.KindLabel, Instructions: "needs my attention",
		SchemaJSON: `{"levels":[{"label":"important"},{"label":"normal"},{"label":"ignorable"}]}`}
	q := layaQuestion(a)
	if q.Kind != "score" {
		t.Fatalf("kind %q, want score", q.Kind)
	}
	got := []string{}
	for _, o := range q.Options {
		got = append(got, o.Label)
	}
	if strings.Join(got, ",") != "ignorable,normal,important" {
		t.Errorf("levels asked as %v, want lowest first", got)
	}
}

func TestTheDigestSaysWhichWayAConversationPoints(t *testing.T) {
	theirs := digestLine(&store.ThreadDigest{Messages: 3, Participants: 2, LastFromMe: false, YouReplied: true})
	mine := digestLine(&store.ThreadDigest{Messages: 1, Participants: 2, LastFromMe: true})
	if !strings.Contains(theirs, "newest from them") || !strings.Contains(theirs, "you have replied") {
		t.Errorf("their turn read as %q", theirs)
	}
	if !strings.Contains(mine, "newest from you") {
		t.Errorf("your turn read as %q", mine)
	}
}

func TestAContactIsShownByItsAddressAndRecentSubjects(t *testing.T) {
	got := kindText("shop@x.com", []string{"Your receipt", ""})
	for _, want := range []string{"From: shop@x.com", "- Your receipt", "- (no subject)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
