package store

import (
	"strings"
	"testing"
)

func mustCard(t *testing.T, title, list string) *Ticket {
	t.Helper()
	tk := &Ticket{Title: title, Status: list}
	if err := SaveTicket(tk); err != nil {
		t.Fatalf("SaveTicket(%q): %v", title, err)
	}
	return tk
}

func boardTitles(t *testing.T, filter, list string) []string {
	t.Helper()
	cols, err := BoardColumns(filter, 100)
	if err != nil {
		t.Fatalf("BoardColumns(%q): %v", filter, err)
	}
	for _, c := range cols {
		if c.Status == list {
			out := []string{}
			for _, tk := range c.Tickets {
				out = append(out, tk.Title)
			}
			return out
		}
	}
	t.Fatalf("no column %q", list)
	return nil
}

func same(a, b []string) bool { return strings.Join(a, "|") == strings.Join(b, "|") }

// You start with the four lists the board always had, and their ids are the
// statuses, so every query and saved query written before keeps working.
func TestTheFourListsYouStartWith(t *testing.T) {
	openQueryFixture(t)
	lists, err := TicketLists(false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range lists {
		got = append(got, l.ID+":"+l.Kind)
	}
	want := []string{"proposed:proposed", "todo:open", "doing:open", "done:closed"}
	if !same(got, want) {
		t.Errorf("lists = %v, want %v", got, want)
	}
	cols, _ := BoardColumns("", 10)
	if len(cols) != 4 || cols[1].Title != "To do" {
		t.Errorf("board columns = %+v", cols)
	}
}

func TestListsAreYours(t *testing.T) {
	openQueryFixture(t)

	l, err := CreateTicketList("Waiting on vendor", ListOpen, TicketTodo)
	if err != nil {
		t.Fatalf("CreateTicketList: %v", err)
	}
	if l.ID != "waiting-on-vendor" {
		t.Errorf("id = %q", l.ID)
	}
	lists, _ := TicketLists(false)
	if lists[2].ID != l.ID {
		t.Errorf("the new list is not after To do: %v", lists[2].ID)
	}

	card := mustCard(t, "Chase the quote", l.ID)
	if got := ticketIDs(t, "status:waiting-on-vendor"); !same(got, []string{"Chase the quote"}) {
		t.Errorf("status:waiting-on-vendor = %v", got)
	}
	if got := ticketIDs(t, "list:waiting-on-vendor"); len(got) != 1 {
		t.Errorf("list: is status:, got %v", got)
	}
	if got := ticketIDs(t, "state:open"); !same(got, []string{"Chase the quote"}) {
		t.Errorf("state:open = %v", got)
	}

	// Renaming keeps the id, so the card and every query naming it stay put.
	if err := RenameTicketList(l.ID, "Waiting"); err != nil {
		t.Fatal(err)
	}
	if got := ticketIDs(t, "status:waiting-on-vendor"); len(got) != 1 {
		t.Errorf("a rename moved the card: %v", got)
	}

	// An unknown list is an error that names the real ones.
	if _, err := RunQuery("status:nowhere", 10, 0); err == nil || !strings.Contains(err.Error(), "waiting-on-vendor") {
		t.Errorf("status:nowhere: %v", err)
	}

	// A card is never deleted as a side effect.
	if err := DeleteTicketList(l.ID, ""); err == nil {
		t.Error("deleting a list with cards and nowhere to put them succeeded")
	}
	if err := DeleteTicketList(l.ID, TicketDoing); err != nil {
		t.Fatalf("DeleteTicketList: %v", err)
	}
	got, _ := GetTicket(card.ID)
	if got.Status != TicketDoing {
		t.Errorf("the card went to %q, want doing", got.Status)
	}

	// The board always has somewhere open, somewhere closed, and the queue.
	if err := DeleteTicketList(TicketDone, TicketDoing); err == nil {
		t.Error("deleted the last closed list")
	}
	if err := DeleteTicketList(TicketProposed, TicketDoing); err == nil {
		t.Error("deleted the proposed list")
	}
	if err := ArchiveTicketList(TicketDone, true); err == nil {
		t.Error("archived the last closed list")
	}
}

// A closed list of your own finishes cards the way Done does.
func TestAClosedListFinishesCards(t *testing.T) {
	openQueryFixture(t)
	shipped, err := CreateTicketList("Shipped", ListClosed, "")
	if err != nil {
		t.Fatal(err)
	}
	card := mustCard(t, "Send the parcel", TicketTodo)
	if err := MoveTicket(card.ID, shipped.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := GetTicket(card.ID)
	if got.ClosedAt == nil {
		t.Error("entering a closed list did not stamp the card finished")
	}
	if titles := ticketIDs(t, "state:closed"); !same(titles, []string{"Send the parcel"}) {
		t.Errorf("state:closed = %v", titles)
	}

	// Making the list open reopens what is in it.
	if err := SetTicketListKind(shipped.ID, ListOpen); err != nil {
		t.Fatal(err)
	}
	got, _ = GetTicket(card.ID)
	if got.ClosedAt != nil {
		t.Error("opening the list left the card finished")
	}
}

func TestCardsKeepTheOrderYouGiveThem(t *testing.T) {
	openQueryFixture(t)
	a := mustCard(t, "A", TicketTodo)
	b := mustCard(t, "B", TicketTodo)
	c := mustCard(t, "C", TicketTodo)

	// New cards go to the bottom, as in Trello.
	if got := boardTitles(t, "", TicketTodo); !same(got, []string{"A", "B", "C"}) {
		t.Fatalf("order = %v", got)
	}
	if err := MoveTicket(c.ID, TicketTodo, "^"); err != nil {
		t.Fatal(err)
	}
	if err := MoveTicket(a.ID, TicketTodo, b.ID); err != nil {
		t.Fatal(err)
	}
	if got := boardTitles(t, "", TicketTodo); !same(got, []string{"C", "B", "A"}) {
		t.Errorf("after moves = %v, want C B A", got)
	}

	// Into another list, after a card there.
	d := mustCard(t, "D", TicketDoing)
	if err := MoveTicket(b.ID, TicketDoing, "^"); err != nil {
		t.Fatal(err)
	}
	if err := MoveTicket(c.ID, TicketDoing, b.ID); err != nil {
		t.Fatal(err)
	}
	if got := boardTitles(t, "", TicketDoing); !same(got, []string{"B", "C", "D"}) {
		t.Errorf("doing = %v, want B C D", got)
	}
	_ = d

	// Halving the same gap over and over runs out of precision; the list is
	// renumbered rather than the order lost.
	for i := 0; i < 80; i++ {
		mover := c.ID
		if i%2 == 1 {
			mover = d.ID
		}
		if err := MoveTicket(mover, TicketDoing, b.ID); err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
	}
	if got := boardTitles(t, "", TicketDoing); got[0] != "B" || len(got) != 3 {
		t.Errorf("after 80 moves = %v", got)
	}

	// Saving a card someone had open does not put it back where it was.
	if err := MoveTicket(a.ID, TicketTodo, "^"); err != nil {
		t.Fatal(err)
	}
	stale := *a
	stale.Title = "A, renamed"
	if err := SaveTicket(&stale); err != nil {
		t.Fatal(err)
	}
	if got := boardTitles(t, "", TicketTodo); got[0] != "A, renamed" {
		t.Errorf("a save moved the card: %v", got)
	}
}

// Archived is off the board, not gone: queries leave it out unless they say
// archived:, the way Trash is left out of mail.
func TestArchivedCardsAreLeftOutUnlessAskedFor(t *testing.T) {
	openQueryFixture(t)
	keep := mustCard(t, "Keep", TicketTodo)
	gone := mustCard(t, "Old", TicketTodo)
	if err := ArchiveTicket(gone.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := ticketIDs(t, "status:todo"); !same(got, []string{"Keep"}) {
		t.Errorf("status:todo = %v", got)
	}
	if got := boardTitles(t, "", TicketTodo); !same(got, []string{"Keep"}) {
		t.Errorf("board = %v", got)
	}
	if got := ticketIDs(t, "status:todo archived:yes"); !same(got, []string{"Old"}) {
		t.Errorf("archived:yes = %v", got)
	}
	if got := ticketIDs(t, "status:todo archived:*"); len(got) != 2 {
		t.Errorf("archived:* = %v", got)
	}
	if err := ArchiveTicket(gone.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := ticketIDs(t, "status:todo"); len(got) != 2 {
		t.Errorf("restored card missing: %v", got)
	}
	events, _ := TicketEvents(gone.ID)
	kinds := []string{}
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if !strings.Contains(strings.Join(kinds, ","), "archived,restored") {
		t.Errorf("history = %v", kinds)
	}
	_ = keep
}

// Labels and topics are one vocabulary. A card raised from mail starts with
// its conversation's topics; after that they are the card's own.
func TestLabelsAreTopics(t *testing.T) {
	openQueryFixture(t)
	if err := SetMessageTopics("m1", []string{"invoices", "acme"}, SourceAgent, nil); err != nil {
		t.Fatal(err)
	}
	key, err := MessageThreadKey("m1")
	if err != nil {
		t.Fatal(err)
	}
	card := &Ticket{Title: "Pay Acme", ThreadKey: key}
	if err := SaveTicket(card); err != nil {
		t.Fatal(err)
	}
	got, _ := GetTicket(card.ID)
	if !same(got.Labels, []string{"acme", "invoices"}) {
		t.Fatalf("seeded labels = %v", got.Labels)
	}

	if err := RemoveTicketLabels(card.ID, []string{"acme"}); err != nil {
		t.Fatal(err)
	}
	if err := AddTicketLabels(card.ID, []string{"Money"}); err != nil {
		t.Fatal(err)
	}
	if got := ticketIDs(t, "in:tickets label:money"); !same(got, []string{"Pay Acme"}) {
		t.Errorf("label:money = %v", got)
	}
	if got := ticketIDs(t, "in:tickets topic:invoices"); len(got) != 1 {
		t.Errorf("topic: in a ticket query is a card label: %v", got)
	}
	// A label is a whole word.
	if got := ticketIDs(t, "in:tickets label:mon"); len(got) != 0 {
		t.Errorf("label:mon matched a fragment: %v", got)
	}
	if got := ticketIDs(t, "in:tickets -label:acme"); len(got) != 1 {
		t.Errorf("-label:acme = %v", got)
	}
	// topic: alone is still about mail.
	if res, err := RunQuery("topic:invoices", 10, 0); err != nil || res.Kind != "messages" {
		t.Errorf("topic:invoices alone: kind %v err %v", res, err)
	}

	labels, err := BoardLabels()
	if err != nil {
		t.Fatal(err)
	}
	var money *LabelInfo
	for i := range labels {
		if labels[i].Name == "money" {
			money = &labels[i]
		}
	}
	if money == nil || money.Cards != 1 || money.Color == "" {
		t.Errorf("money = %+v", money)
	}
	if err := SetLabelColor("money", "teal"); err == nil {
		t.Error("teal is not a Trello colour, and was accepted")
	}
	if err := SetLabelColor("money", "green"); err != nil {
		t.Fatal(err)
	}
	labels, _ = BoardLabels()
	for _, l := range labels {
		if l.Name == "money" && (l.Color != "green" || !l.Chosen) {
			t.Errorf("money = %+v", l)
		}
	}
}

func TestChecklistsAndComments(t *testing.T) {
	openQueryFixture(t)
	card := mustCard(t, "Paint the fence", TicketTodo)
	first, err := AddChecklistItem(card.ID, "", "Buy primer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddChecklistItem(card.ID, "", "Sand"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddChecklistItem(card.ID, "Clean up", "Wash the brushes"); err != nil {
		t.Fatal(err)
	}
	if err := SetChecklistItemDone(first.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := AddTicketComment(card.ID, "Ask about the **colour**", ""); err != nil {
		t.Fatal(err)
	}

	res, _ := RunQuery("status:todo", 10, 0)
	tk := res.Tickets[0]
	if tk.ChecklistDone != 1 || tk.ChecklistTotal != 3 || tk.Comments != 1 {
		t.Errorf("front of card: %d/%d, %d comments", tk.ChecklistDone, tk.ChecklistTotal, tk.Comments)
	}

	d, err := GetTicketDetail(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Checklist[0].Checklist != DefaultChecklist || d.Checklist[2].Checklist != "Clean up" {
		t.Errorf("checklists = %+v", d.Checklist)
	}
	if !strings.Contains(d.Notes[0].BodyHTML, "<strong>colour</strong>") {
		t.Errorf("comment html = %q", d.Notes[0].BodyHTML)
	}
	var checked bool
	for _, e := range d.History {
		checked = checked || (e.Kind == EventChecked && e.To == "Buy primer")
	}
	if !checked {
		t.Error("ticking an item is not in the history")
	}

	if got := ticketIDs(t, "in:tickets comment:colour"); len(got) != 1 {
		t.Errorf("comment:colour = %v", got)
	}
	if got := ticketIDs(t, "in:tickets checklist:primer"); len(got) != 1 {
		t.Errorf("checklist:primer = %v", got)
	}
	// A bare word finds a card by its own words too.
	if got := ticketIDs(t, "status:todo brushes"); len(got) != 1 {
		t.Errorf("bare word over the checklist = %v", got)
	}

	if err := RenameChecklist(card.ID, "Clean up", "After"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteChecklist(card.ID, "After"); err != nil {
		t.Fatal(err)
	}
	items, _ := ChecklistItems(card.ID)
	if len(items) != 2 {
		t.Errorf("items after deleting a checklist = %d", len(items))
	}
}

// Descriptions render as Markdown, and raw HTML in them stays text.
func TestDescriptionsRenderSafely(t *testing.T) {
	html := RenderMarkdown("- [ ] one\n- two\n\n<script>alert(1)</script>")
	if strings.Contains(html, "<script>") {
		t.Errorf("raw html passed through: %s", html)
	}
	if !strings.Contains(html, "<li>") {
		t.Errorf("no list rendered: %s", html)
	}
}

func TestRulesWhenACardEntersAList(t *testing.T) {
	openQueryFixture(t)
	if err := CreateTicketRule(&TicketRule{Trigger: RuleEnterList, List: "Done", Actions: []RuleAction{
		{Do: ActionCheckAll}, {Do: ActionAddLabel, Value: "Finished"}, {Do: ActionClearDue},
	}}); err != nil {
		t.Fatalf("CreateTicketRule: %v", err)
	}
	if err := CreateTicketRule(&TicketRule{Trigger: RuleEnterList, List: "done",
		Actions: []RuleAction{{Do: "explode"}}}); err == nil {
		t.Error("an unknown action was accepted")
	}

	card := mustCard(t, "Book the flight", TicketTodo)
	card.DueAt, _ = ParseTicketDue("3d")
	if err := SaveTicket(card); err != nil {
		t.Fatal(err)
	}
	if _, err := AddChecklistItem(card.ID, "", "Pick seats"); err != nil {
		t.Fatal(err)
	}
	if err := MoveTicket(card.ID, TicketDone, ""); err != nil {
		t.Fatal(err)
	}

	d, _ := GetTicketDetail(card.ID)
	if !d.Checklist[0].Done {
		t.Error("the checklist was not ticked")
	}
	if !same(d.Labels, []string{"finished"}) {
		t.Errorf("labels = %v", d.Labels)
	}
	if d.DueAt != nil {
		t.Error("the due date was not cleared")
	}
	var byRule bool
	for _, e := range d.History {
		byRule = byRule || (e.Kind == EventLabel && e.Actor == ActorRule)
	}
	if !byRule {
		t.Error("the rule's change is not credited to the rule")
	}
}

func TestMailRulesMakeOneCardPerConversation(t *testing.T) {
	openQueryFixture(t)
	r := &TicketRule{Trigger: RuleMail, Query: "from:*@acme.com subject:invoice",
		Actions: []RuleAction{{Do: ActionAddLabel, Value: "bills"}}}
	if err := CreateTicketRule(r); err != nil {
		t.Fatalf("CreateTicketRule: %v", err)
	}
	if r.List != TicketProposed {
		t.Errorf("a mail rule's cards go to %q by default, want proposed", r.List)
	}
	if err := CreateTicketRule(&TicketRule{Trigger: RuleMail, Query: "status:todo"}); err == nil {
		t.Error("a mail rule over tickets was accepted")
	}

	// The fixture's mail is older than the rule: nothing, unless backfilled.
	out, err := RunMailRules("", false)
	if err != nil || out.Created != 0 {
		t.Fatalf("run before backfill: %+v %v", out, err)
	}
	out, err = RunMailRules(r.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	// m1 and its reply m5 are one conversation: one card, both messages.
	if out.Created != 1 || out.Attached != 1 {
		t.Errorf("backfill = %+v, want 1 created and 1 attached", out)
	}
	res, _ := RunQuery("status:proposed raised:rule", 10, 0)
	if len(res.Tickets) != 1 || res.Tickets[0].Title != "Quarterly invoice attached" ||
		len(res.Tickets[0].Sources) != 2 || !same(res.Tickets[0].Labels, []string{"bills"}) {
		t.Fatalf("card = %+v", res.Tickets)
	}

	// Running again changes nothing.
	out, _ = RunMailRules(r.ID, true)
	if out.Created != 0 || out.Attached != 0 {
		t.Errorf("a re-run did work: %+v", out)
	}
}

func TestBoardsAreNamedFilters(t *testing.T) {
	openQueryFixture(t)
	home, err := CreateTicketBoard("Home", "label:home due:30d")
	if err != nil {
		t.Fatal(err)
	}
	if home.ID != "home" || !same(home.Labels, []string{"home"}) {
		t.Errorf("board = %+v", home)
	}
	if _, err := CreateTicketBoard("Bad", "status:todo | count"); err == nil {
		t.Error("a filter with a stage was accepted")
	}
	if _, err := CreateTicketBoard("Bad", "frm:x"); err == nil {
		t.Error("a filter that does not compile was accepted")
	}
	if got := FilterLabels("label:home -label:work (topic:a OR topic:b) topic:garden"); !same(got, []string{"home", "garden"}) {
		t.Errorf("FilterLabels = %v", got)
	}

	boards, _ := TicketBoards()
	if len(boards) != 2 || boards[0].ID != "all" {
		t.Errorf("boards = %+v", boards)
	}
	if err := MoveTicketBoard("home", "^"); err != nil {
		t.Fatal(err)
	}
	boards, _ = TicketBoards()
	if boards[0].ID != "home" {
		t.Errorf("after move: %v", boards[0].ID)
	}
	if err := DeleteTicketBoard("home"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteTicketBoard("all"); err == nil {
		t.Error("deleted the only board")
	}
}
