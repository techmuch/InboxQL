package store

import (
	"context"
	"strings"

	"github.com/user/inboxql/internal/message"
)

// PreviewLength is how much of a message a list line shows.
const PreviewLength = 160

// ForList fills in what a list of messages shows that a message does not
// carry: the sender's name and a clean preview.
//
// The name, best source first: what you called the contact, the name this
// message's own header gave, then the name the address last arrived under.
// The second outranks the third because one address can speak for several
// people — a ward's noreply address signs as whoever sent that week.
//
// One query for the page's names, not one per message.
func ForList(ctx context.Context, msgs []*message.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	ids := make([]any, len(msgs))
	marks := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
		marks[i] = "?"
		m.Preview = message.Preview(m, PreviewLength)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT p.message_id,
		       COALESCE(NULLIF(TRIM(c.display_name), ''), NULLIF(TRIM(p.name), ''), NULLIF(TRIM(c.header_name), ''), '')
		FROM message_participants p
		LEFT JOIN contacts c ON c.address = p.address
		WHERE p.role = 'from' AND p.message_id IN (`+strings.Join(marks, ",")+`)`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		if name != "" {
			names[id] = name
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range msgs {
		m.FromName = names[m.ID]
	}
	return nil
}
