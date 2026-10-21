package store

import (
	"strings"
	"testing"
)

// importance: is the conversation's judgement, so every message in a
// conversation judged low is low — archiving "the low-importance mail" takes
// whole conversations, not their newest message alone.
func TestImportanceIsTheConversations(t *testing.T) {
	openQueryFixture(t)
	a := &Annotator{Name: "importance", Kind: KindLabel, Engine: EngineAgent,
		Instructions: "how much it matters", SchemaJSON: `{"element":true,"unit":"thread","shape":"levels",` +
			`"levels":[{"label":"important"},{"label":"normal"},{"label":"low"},{"label":"ignorable"}]}`}
	if err := SaveAnnotator(a); err != nil {
		t.Fatal(err)
	}
	judge := func(id, level string) {
		if err := SaveAnnotations(a.ID, a.Version, id, []*Annotation{{Status: StatusOK, Source: SourceAgent,
			DataJSON: `{"level":"` + level + `"}`}}); err != nil {
			t.Fatal(err)
		}
	}
	// m1 and its reply m5 are one conversation, judged on its newest message.
	judge("m5", "low")
	judge("m3", "important")

	if got := ids(t, "importance:low"); strings.Join(got, ",") != "m1,m5" {
		t.Errorf("importance:low = %v, want the whole conversation m1,m5", got)
	}
	if got := ids(t, "importance:important"); strings.Join(got, ",") != "m3" {
		t.Errorf("importance:important = %v", got)
	}
	// Nothing judged is no importance at all, not "normal".
	if got := ids(t, "importance:normal"); len(got) != 0 {
		t.Errorf("importance:normal = %v, want none", got)
	}
	if got := ids(t, "-importance:low"); strings.Join(got, ",") != "m2,m3,m4" {
		t.Errorf("-importance:low = %v", got)
	}
	// A person's ruling outranks the judgement.
	if err := SaveAnnotations(a.ID, a.Version, "m5", []*Annotation{{Status: StatusOK, Source: SourceHuman,
		DataJSON: `{"level":"important"}`}}); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, "importance:low"); len(got) != 0 {
		t.Errorf("after a ruling, importance:low = %v", got)
	}
	if _, err := RunQuery("importance:urgent", 10, 0); err == nil {
		t.Error("an unknown level was accepted")
	}
}

// sentby: is the sender's kind, as the contact records it.
func TestSentByIsTheSendersKind(t *testing.T) {
	openQueryFixture(t)
	if err := SetContactKind("billing@stripe.com", KindSystem, ContactFromAgent); err != nil {
		t.Fatal(err)
	}
	if err := SetContactKind("alice@acme.com", KindPerson, ContactFromAgent); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, "sentby:system"); strings.Join(got, ",") != "m3" {
		t.Errorf("sentby:system = %v", got)
	}
	if got := ids(t, "sentby:person"); strings.Join(got, ",") != "m1" {
		t.Errorf("sentby:person = %v", got)
	}
	// Unknown is anyone not classified as something.
	if got := ids(t, "sentby:unknown"); strings.Join(got, ",") != "m2,m4,m5" {
		t.Errorf("sentby:unknown = %v", got)
	}
}

// The Group view groups by the same judgements its filters read.
func TestGroupingByImportanceAndSender(t *testing.T) {
	openQueryFixture(t)
	if err := SetContactKind("billing@stripe.com", KindSystem, ContactFromAgent); err != nil {
		t.Fatal(err)
	}
	res, err := RunQuery("folder:inbox | count by sentby", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, g := range res.Groups {
		got[g.Label] = g.Value
	}
	if got["system"] != 1 || got["unknown"] != 4 {
		t.Errorf("count by sentby = %v", got)
	}
	res, err = RunQuery("folder:inbox | count by importance", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Label != "(not judged)" {
		t.Errorf("count by importance = %+v", res.Groups)
	}
}

// All Items runs the words across every kind and keeps them apart.
func TestSearchAllKeepsKindsApart(t *testing.T) {
	openQueryFixture(t)
	if err := SaveTicket(&Ticket{Title: "Pay the quarterly invoice"}); err != nil {
		t.Fatal(err)
	}
	byID := map[string]SearchGroup{}
	for _, g := range SearchAll("invoice", 5) {
		byID[g.ID] = g
	}
	if g := byID["mail"]; g.Total != 2 || g.Items == nil || len(g.Items.Messages) != 2 {
		t.Errorf("mail = %+v", g)
	}
	if g := byID["cards"]; g.Total != 1 || g.Query != "in:tickets invoice" {
		t.Errorf("cards = %+v", g)
	}
	// A term only mail has still answers for mail.
	for _, g := range SearchAll("is:unread", 5) {
		if g.ID == "mail" && (g.Error != "" || g.Total == 0) {
			t.Errorf("mail with is:unread: %+v", g)
		}
	}
	if len(SearchAll("", 5)) != len(SearchKinds) {
		t.Error("an empty search did not return the kinds")
	}
}

// A filtered topic chart: the ignored words are bound after the filter's
// arguments, where their placeholders stand. Bound in front, `from:x | top
// topic` matched nothing and `subject:x | top topic` failed outright.
func TestTopTopicUnderAFilter(t *testing.T) {
	openQueryFixture(t)
	if err := UpdateSetting("ignore_words", "re:,the"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"from:alice@acme.com | top topic 10", "subject:quarterly | top topic 10"} {
		res, err := RunQuery(q, 0, 0)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		found := false
		for _, g := range res.Groups {
			found = found || g.Label == "quarterly"
		}
		if !found {
			t.Errorf("%s = %+v, want quarterly among them", q, res.Groups)
		}
	}
}
