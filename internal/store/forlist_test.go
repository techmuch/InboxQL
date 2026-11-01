package store

import (
	"context"
	"testing"

	"github.com/user/inboxql/internal/message"
)

// A list says who sent each message the way a person would, and shows its
// first words: the name you gave the contact, else the one this message's
// header gave, else the one the address last arrived under.
func TestForListNamesTheSenderAndCleansThePreview(t *testing.T) {
	openQueryFixture(t)
	for _, q := range []string{
		`UPDATE message_participants SET name = 'Alice Ames' WHERE message_id = 'm1' AND role = 'from'`,
		`UPDATE message_participants SET name = NULL WHERE message_id = 'm3' AND role = 'from'`,
		`UPDATE contacts SET header_name = 'Stripe' WHERE address = 'billing@stripe.com'`,
		`UPDATE contacts SET display_name = 'Friend from work' WHERE address = 'notalice@acme.com'`,
		`UPDATE message_participants SET name = 'Not Alice' WHERE message_id = 'm2' AND role = 'from'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	msgs := []*message.Message{
		{ID: "m1", Body: "&zwnj; &zwnj; Please find the invoice https://track.example/?id=1"},
		{ID: "m2"},
		{ID: "m3"},
		{ID: "nope"},
	}
	if err := ForList(context.Background(), msgs); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, want string }{
		{"m1", "Alice Ames"},       // the header's name
		{"m2", "Friend from work"}, // yours outranks the header's
		{"m3", "Stripe"},           // the address's, when the message gave none
		{"nope", ""},               // nothing known: the list shows the address
	} {
		for _, m := range msgs {
			if m.ID == c.id && m.FromName != c.want {
				t.Errorf("%s: fromName %q, want %q", c.id, m.FromName, c.want)
			}
		}
	}
	if msgs[0].Preview != "Please find the invoice" {
		t.Errorf("preview %q", msgs[0].Preview)
	}
}
