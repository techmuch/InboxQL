package store

import "testing"

// Ranking by volume answers every question with whoever you email most. On a
// real 188-message archive one address appeared on 185 of them, so counting
// would have named the same person for every topic.
func TestTopicRankingPrefersDistinctivenessOverVolume(t *testing.T) {
	openQueryFixture(t)

	// alice is on two messages and both are about invoices.
	// me@example.com is on everything, and only one is about invoices.
	if err := SetMessageTopics("m1", []string{"invoices"}, TopicFromModel, nil); err != nil {
		t.Fatalf("SetMessageTopics: %v", err)
	}
	if err := SetMessageTopics("m5", []string{"invoices"}, TopicFromModel, nil); err != nil {
		t.Fatalf("SetMessageTopics: %v", err)
	}
	for _, id := range []string{"m2", "m3", "m4"} {
		if err := SetMessageTopics(id, []string{"chatter"}, TopicFromModel, nil); err != nil {
			t.Fatalf("SetMessageTopics: %v", err)
		}
	}

	contacts, err := ContactsForTopic("invoices", 5)
	if err != nil {
		t.Fatalf("ContactsForTopic: %v", err)
	}
	if len(contacts) == 0 {
		t.Fatal("nobody is associated with invoices")
	}
	// me@example.com is on more invoice messages in absolute terms than bob,
	// but invoices are a far larger share of alice's and bob's mail.
	if contacts[0].Address == "me@example.com" {
		t.Errorf("the busiest address won the topic: %s", contacts[0].Address)
	}

	topics, err := TopicsFor("alice@acme.com", 10)
	if err != nil {
		t.Fatalf("TopicsFor: %v", err)
	}
	if len(topics) == 0 || topics[0].Topic != "invoices" {
		t.Fatalf("alice's top topic is %v, want invoices", topics)
	}
	if topics[0].Lift <= 1 {
		t.Errorf("lift is %.2f, want above 1 for a topic they discuss disproportionately",
			topics[0].Lift)
	}
}

// A model is asked for topics and returns whatever shape it likes.
func TestTopicListAcceptsWhatModelsActuallyReturn(t *testing.T) {
	cases := map[string][]string{
		`{"topics":["Invoices","billing"]}`: {"invoices", "billing"},
		`{"topics":"invoices, billing"}`:    {"invoices", "billing"},
		`{"topic":"Invoices"}`:              {"invoices"},
		`{"tags":["invoices","invoices"]}`:  {"invoices"},
		`{"nothing":1}`:                     nil,
		`not json`:                          nil,
	}
	for data, want := range cases {
		got := topicList(data)
		if len(got) != len(want) {
			t.Errorf("topicList(%s) = %v, want %v", data, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("topicList(%s) = %v, want %v", data, got, want)
				break
			}
		}
	}
}

// A model re-run must not delete a topic a person added, and vice versa.
func TestTopicSourcesDoNotOverwriteEachOther(t *testing.T) {
	openQueryFixture(t)

	if err := SetMessageTopics("m1", []string{"mine"}, TopicFromHuman, nil); err != nil {
		t.Fatalf("SetMessageTopics: %v", err)
	}
	if err := SetMessageTopics("m1", []string{"theirs"}, TopicFromModel, nil); err != nil {
		t.Fatalf("SetMessageTopics: %v", err)
	}
	// A second model run replaces only what the model said.
	if err := SetMessageTopics("m1", []string{"revised"}, TopicFromModel, nil); err != nil {
		t.Fatalf("SetMessageTopics: %v", err)
	}

	res, err := RunQuery("topic:mine", 10, 0)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Error("a model re-run deleted a topic a person added")
	}
	if res, _ := RunQuery("topic:theirs", 10, 0); len(res.Messages) != 0 {
		t.Error("the model's previous answer survived its own re-run")
	}
	if res, _ := RunQuery("topic:revised", 10, 0); len(res.Messages) != 1 {
		t.Error("the model's new answer was not recorded")
	}
}
