package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// PendingThread represents a conversation thread awaiting a reply.
type PendingThread struct {
	ThreadKey     string    `json:"threadKey"`
	Subject       string    `json:"subject"`
	LastMessageAt time.Time `json:"lastMessageAt"`
	Snippet       string    `json:"snippet,omitempty"`
}

// ContactResponsiveness holds communication dynamics and latency metrics for a contact.
type ContactResponsiveness struct {
	Address                   string          `json:"address"`
	MyMedianReplySecs         *int64          `json:"myMedianReplySecs"`
	TheirMedianReplySecs      *int64          `json:"theirMedianReplySecs"`
	AwaitingMyReplyCount      int             `json:"awaitingMyReplyCount"`
	AwaitingTheirReplyCount   int             `json:"awaitingTheirReplyCount"`
	AwaitingMyReplyThreads    []PendingThread `json:"awaitingMyReplyThreads"`
	AwaitingTheirReplyThreads []PendingThread `json:"awaitingTheirReplyThreads"`
	ToCount                   int64           `json:"toCount"`
	CcCount                   int64           `json:"ccCount"`
	ToRatio                   float64         `json:"toRatio"`
	HourlyDistribution        [24]int         `json:"hourlyDistribution"`
}

type threadMsg struct {
	threadKey string
	id        string
	sender    string
	subject   string
	date      int64
	snippet   string
}

// GetContactResponsiveness calculates communication dynamics, reply latencies,
// pending reply threads (open loops), role ratios, and hourly distribution for a contact.
func GetContactResponsiveness(address string) (*ContactResponsiveness, error) {
	addr, _ := NormaliseAddress(address)
	if addr == "" {
		addr = strings.ToLower(strings.TrimSpace(address))
	}
	if addr == "" {
		return nil, fmt.Errorf("a contact address is required")
	}

	mine, err := selfAddresses()
	if err != nil {
		return nil, err
	}
	selfMap := make(map[string]bool, len(mine))
	for _, m := range mine {
		selfMap[strings.ToLower(strings.TrimSpace(m))] = true
	}

	resp := &ContactResponsiveness{
		Address:                   addr,
		AwaitingMyReplyThreads:    []PendingThread{},
		AwaitingTheirReplyThreads: []PendingThread{},
	}

	// 1. Role counts (To vs Cc/Bcc)
	roleRows, err := db.Query(`
		SELECT role, COUNT(*)
		FROM message_participants
		WHERE address = ?
		GROUP BY role`, addr)
	if err != nil {
		return nil, err
	}
	defer roleRows.Close()

	for roleRows.Next() {
		var role string
		var count int64
		if err := roleRows.Scan(&role, &count); err != nil {
			return nil, err
		}
		switch role {
		case "to":
			resp.ToCount += count
		case "cc", "bcc":
			resp.CcCount += count
		}
	}
	if err := roleRows.Err(); err != nil {
		return nil, err
	}

	if resp.ToCount+resp.CcCount > 0 {
		resp.ToRatio = float64(resp.ToCount) / float64(resp.ToCount+resp.CcCount)
	}

	// 2. Hourly distribution of incoming messages from this contact
	hourRows, err := db.Query(`
		SELECT m.date
		FROM messages m
		JOIN message_participants p ON p.message_id = m.id
		WHERE p.address = ? AND p.role = 'from'`, addr)
	if err != nil {
		return nil, err
	}
	defer hourRows.Close()

	for hourRows.Next() {
		var d int64
		if err := hourRows.Scan(&d); err != nil {
			return nil, err
		}
		t := millisToTime(d)
		h := t.UTC().Hour()
		if h >= 0 && h < 24 {
			resp.HourlyDistribution[h]++
		}
	}
	if err := hourRows.Err(); err != nil {
		return nil, err
	}

	// 3. Conversation threads: query all messages belonging to threads touching this contact
	rows, err := db.Query(`
		WITH contact_threads AS (
			SELECT DISTINCT COALESCE(m.thread_key, m.id) AS tkey
			FROM messages m
			JOIN message_participants p ON p.message_id = m.id
			WHERE p.address = ?
		)
		SELECT
			COALESCE(m.thread_key, m.id) AS tkey,
			m.id,
			COALESCE(p_from.address, m.from_addr) AS sender,
			m.subject,
			m.date,
			SUBSTR(COALESCE(NULLIF(m.normalized_body, ''), m.body), 1, 160) AS snippet
		FROM messages m
		JOIN contact_threads ct ON ct.tkey = COALESCE(m.thread_key, m.id)
		LEFT JOIN message_participants p_from ON p_from.message_id = m.id AND p_from.role = 'from'
		ORDER BY tkey, m.date ASC`, addr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	threads := make(map[string][]threadMsg)
	threadOrder := []string{}

	for rows.Next() {
		var msg threadMsg
		if err := rows.Scan(&msg.threadKey, &msg.id, &msg.sender, &msg.subject, &msg.date, &msg.snippet); err != nil {
			return nil, err
		}
		cleanSender, _ := NormaliseAddress(msg.sender)
		if cleanSender == "" {
			cleanSender = strings.ToLower(strings.TrimSpace(msg.sender))
		}
		msg.sender = cleanSender

		if _, exists := threads[msg.threadKey]; !exists {
			threadOrder = append(threadOrder, msg.threadKey)
		}
		threads[msg.threadKey] = append(threads[msg.threadKey], msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var myReplyDeltas []int64
	var theirReplyDeltas []int64

	for _, tkey := range threadOrder {
		msgs := threads[tkey]
		if len(msgs) == 0 {
			continue
		}

		var pendingThemTimestamp int64
		var pendingMeTimestamp int64

		for _, m := range msgs {
			isMe := selfMap[m.sender]
			isThem := (m.sender == addr)

			if isThem {
				// Them sent a message
				if pendingMeTimestamp > 0 && m.date >= pendingMeTimestamp {
					theirReplyDeltas = append(theirReplyDeltas, (m.date-pendingMeTimestamp)/1000)
					pendingMeTimestamp = 0
				}
				if pendingThemTimestamp == 0 {
					pendingThemTimestamp = m.date
				}
			} else if isMe {
				// Me sent a message
				if pendingThemTimestamp > 0 && m.date >= pendingThemTimestamp {
					myReplyDeltas = append(myReplyDeltas, (m.date-pendingThemTimestamp)/1000)
					pendingThemTimestamp = 0
				}
				if pendingMeTimestamp == 0 {
					pendingMeTimestamp = m.date
				}
			}
		}

		// Open loop check: look at the last message in the thread
		lastMsg := msgs[len(msgs)-1]
		if pendingThemTimestamp > 0 && !selfMap[lastMsg.sender] {
			// Thread ended with contact waiting for my reply
			resp.AwaitingMyReplyThreads = append(resp.AwaitingMyReplyThreads, PendingThread{
				ThreadKey:     tkey,
				Subject:       lastMsg.subject,
				LastMessageAt: millisToTime(lastMsg.date),
				Snippet:       lastMsg.snippet,
			})
		} else if pendingMeTimestamp > 0 && selfMap[lastMsg.sender] {
			// Thread ended with user waiting for contact's reply
			resp.AwaitingTheirReplyThreads = append(resp.AwaitingTheirReplyThreads, PendingThread{
				ThreadKey:     tkey,
				Subject:       lastMsg.subject,
				LastMessageAt: millisToTime(lastMsg.date),
				Snippet:       lastMsg.snippet,
			})
		}
	}

	// Sort pending threads descending by last message date
	sort.Slice(resp.AwaitingMyReplyThreads, func(i, j int) bool {
		return resp.AwaitingMyReplyThreads[i].LastMessageAt.After(resp.AwaitingMyReplyThreads[j].LastMessageAt)
	})
	sort.Slice(resp.AwaitingTheirReplyThreads, func(i, j int) bool {
		return resp.AwaitingTheirReplyThreads[i].LastMessageAt.After(resp.AwaitingTheirReplyThreads[j].LastMessageAt)
	})

	resp.AwaitingMyReplyCount = len(resp.AwaitingMyReplyThreads)
	resp.AwaitingTheirReplyCount = len(resp.AwaitingTheirReplyThreads)

	// Calculate medians
	calcMedian := func(deltas []int64) *int64 {
		if len(deltas) == 0 {
			return nil
		}
		sort.Slice(deltas, func(i, j int) bool { return deltas[i] < deltas[j] })
		n := len(deltas)
		var m int64
		if n%2 == 1 {
			m = deltas[n/2]
		} else {
			m = (deltas[n/2-1] + deltas[n/2]) / 2
		}
		return &m
	}

	resp.MyMedianReplySecs = calcMedian(myReplyDeltas)
	resp.TheirMedianReplySecs = calcMedian(theirReplyDeltas)

	return resp, nil
}
