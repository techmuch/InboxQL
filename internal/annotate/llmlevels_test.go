package annotate

import (
	"strings"
	"testing"
	"time"

	"github.com/user/inboxql/internal/message"
	"github.com/user/inboxql/internal/store"
)

var importance = &store.Annotator{Name: "importance", Kind: store.KindLabel, Engine: store.EngineLLM,
	Instructions: "how much this needs my attention",
	SchemaJSON:   `{"unit":"thread","levels":[{"label":"important","describe":"a person needs something"},{"label":"low"}]}`}

func TestTheLLMIsOfferedTheLevels(t *testing.T) {
	p := buildSystemPrompt(importance)
	if !strings.Contains(p, "- important: a person needs something") || !strings.Contains(p, `{"level":`) {
		t.Errorf("prompt does not offer the levels:\n%s", p)
	}
}

func TestALevelAnswerIsStoredAsTheLevel(t *testing.T) {
	got, err := parseResponse(importance, `{"level":"low","confidence":0.7}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Matched || got[0].Data["level"] != "low" {
		t.Errorf("got %+v", got)
	}
}

// A generative model can name a level that does not exist. Stored, it would
// match no query and silently drop out of every count.
func TestAnInventedLevelIsRefused(t *testing.T) {
	if _, err := parseResponse(importance, `{"level":"urgent","confidence":0.9}`); err == nil {
		t.Error("a level the label does not have was accepted")
	}
}

// The end of a conversation decides whether anyone is still waiting, so when
// it does not all fit, the oldest messages are the ones left out.
func TestALongConversationKeepsItsNewestMessages(t *testing.T) {
	var msgs []*message.Message
	for i := 0; i < 12; i++ {
		msgs = append(msgs, &message.Message{
			From: "a@x.com", Subject: "s", Date: time.Unix(int64(i), 0),
			Body: strings.Repeat("x", 3000) + string(rune('A'+i)),
		})
	}
	out := renderConversation(importance, msgs)
	if !strings.Contains(out, "xL") {
		t.Error("the newest message was cut")
	}
	if strings.Contains(out, "xA") {
		t.Error("the oldest message survived while later ones were cut")
	}
	if !strings.Contains(out, "earlier message(s) omitted") {
		t.Error("the cut was not said")
	}
}
