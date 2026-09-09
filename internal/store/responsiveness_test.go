package store

import (
	"testing"
	"time"

	"github.com/user/inboxql/internal/account"
	"github.com/user/inboxql/internal/message"
)

func TestContactResponsiveness(t *testing.T) {
	if _, err := InitDB(t.TempDir()); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(CloseDB)

	// User account
	if err := SaveAccount(&account.Account{
		ID: "acct", Name: "Me", Email: "me@example.com", User: "me@example.com",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	baseTime := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)

	// Thread 1:
	// m1: Alice -> Me at 10:00 (baseTime)
	// m2: Me -> Alice at 10:30 (+1800s, my reply time = 1800s)
	// m3: Alice -> Me at 11:30 (+3600s, her reply time = 3600s)
	// Thread 1 ends with Alice having sent m3 -> Awaiting My Reply!
	m1 := &message.Message{
		ID: "m1", AccountID: "acct", MessageID: "<t1-1@acme.com>",
		From: "Alice <alice@acme.com>", To: []string{"me@example.com"},
		Subject: "Project launch", Body: "Are we ready?",
		Date: baseTime, ContentHash: "h1",
		Header: []byte("Message-ID: <t1-1@acme.com>\r\n"),
	}
	m2 := &message.Message{
		ID: "m2", AccountID: "acct", MessageID: "<t1-2@acme.com>",
		From: "Me <me@example.com>", To: []string{"alice@acme.com"},
		Subject: "Re: Project launch", Body: "Yes, deploying now.",
		Date: baseTime.Add(30 * time.Minute), ContentHash: "h2",
		Header: []byte("Message-ID: <t1-2@acme.com>\r\nIn-Reply-To: <t1-1@acme.com>\r\nReferences: <t1-1@acme.com>\r\n"),
	}
	m3 := &message.Message{
		ID: "m3", AccountID: "acct", MessageID: "<t1-3@acme.com>",
		From: "Alice <alice@acme.com>", To: []string{"me@example.com"},
		Subject: "Re: Project launch", Body: "Great, let me know when done.",
		Date: baseTime.Add(90 * time.Minute), ContentHash: "h3",
		Header: []byte("Message-ID: <t1-3@acme.com>\r\nIn-Reply-To: <t1-2@acme.com>\r\nReferences: <t1-1@acme.com> <t1-2@acme.com>\r\n"),
	}

	// Thread 2:
	// m4: Me -> Alice at 14:00 (Alice in To)
	// Thread 2 ends with Me having sent m4 -> Awaiting Her Reply!
	m4 := &message.Message{
		ID: "m4", AccountID: "acct", MessageID: "<t2-1@acme.com>",
		From: "Me <me@example.com>", To: []string{"alice@acme.com"}, Cc: []string{"bob@acme.com"},
		Subject: "Question on design", Body: "Can you review the wireframe?",
		Date: baseTime.Add(4 * time.Hour), ContentHash: "h4",
		Header: []byte("Message-ID: <t2-1@acme.com>\r\n"),
	}

	// Thread 3:
	// m5: Bob -> Me with Alice in CC at 16:00
	m5 := &message.Message{
		ID: "m5", AccountID: "acct", MessageID: "<t3-1@acme.com>",
		From: "Bob <bob@acme.com>", To: []string{"me@example.com"}, Cc: []string{"alice@acme.com"},
		Subject: "FYI Note", Body: "Just keeping you in the loop.",
		Date: baseTime.Add(6 * time.Hour), ContentHash: "h5",
		Header: []byte("Message-ID: <t3-1@acme.com>\r\n"),
	}

	for _, m := range []*message.Message{m1, m2, m3, m4, m5} {
		if err := SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage(%s): %v", m.ID, err)
		}
	}

	resp, err := GetContactResponsiveness("alice@acme.com")
	if err != nil {
		t.Fatalf("GetContactResponsiveness: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil responsiveness result")
	}

	// Check reply times
	if resp.MyMedianReplySecs == nil || *resp.MyMedianReplySecs != 1800 {
		t.Errorf("MyMedianReplySecs = %v, want 1800", resp.MyMedianReplySecs)
	}
	if resp.TheirMedianReplySecs == nil || *resp.TheirMedianReplySecs != 3600 {
		t.Errorf("TheirMedianReplySecs = %v, want 3600", resp.TheirMedianReplySecs)
	}

	// Check open loops
	if resp.AwaitingMyReplyCount != 1 {
		t.Errorf("AwaitingMyReplyCount = %d, want 1", resp.AwaitingMyReplyCount)
	}
	if len(resp.AwaitingMyReplyThreads) != 1 || resp.AwaitingMyReplyThreads[0].Subject != "Re: Project launch" {
		t.Errorf("AwaitingMyReplyThreads = %+v", resp.AwaitingMyReplyThreads)
	}

	if resp.AwaitingTheirReplyCount != 1 {
		t.Errorf("AwaitingTheirReplyCount = %d, want 1", resp.AwaitingTheirReplyCount)
	}
	if len(resp.AwaitingTheirReplyThreads) != 1 || resp.AwaitingTheirReplyThreads[0].Subject != "Question on design" {
		t.Errorf("AwaitingTheirReplyThreads = %+v", resp.AwaitingTheirReplyThreads)
	}

	// Check roles:
	// m2 (To alice), m4 (To alice), m5 (Cc alice) -> 2 To, 1 Cc
	if resp.ToCount != 2 {
		t.Errorf("ToCount = %d, want 2", resp.ToCount)
	}
	if resp.CcCount != 1 {
		t.Errorf("CcCount = %d, want 1", resp.CcCount)
	}
	expectedToRatio := 2.0 / 3.0
	if resp.ToRatio < expectedToRatio-0.01 || resp.ToRatio > expectedToRatio+0.01 {
		t.Errorf("ToRatio = %f, want %f", resp.ToRatio, expectedToRatio)
	}

	// Check hourly distribution:
	// m1 at 10:00 UTC (h=10), m3 at 11:30 UTC (h=11)
	if resp.HourlyDistribution[10] != 1 || resp.HourlyDistribution[11] != 1 {
		t.Errorf("HourlyDistribution = %v, want 1 at h=10 and 1 at h=11", resp.HourlyDistribution)
	}
}
