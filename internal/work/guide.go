package work

// Guide is what `iql work guide` prints: everything a harness needs to run
// the loop, so Claude Code or Antigravity can start from one command.
const Guide = `# Reading mail for InboxQL

You read conversations once and record what is worth knowing about them
later: purchases, travel, calendar events, tasks, who is waiting on whom, how
much a conversation matters, what kind of sender someone is, and its topics.
InboxQL keeps your answers, checks them, and makes them searchable.

## The loop

Fast path (two tool calls per batch):

    iql work next --limit 10 --template /tmp/answers.json --model <id> [--accept-tickets]
    iql --json work submit /tmp/answers.json [--accept-tickets]

--template <file> prints a compact digest of all leased conversations to
stdout (with element schemas, message IDs, sender addresses, and clean text)
and writes a pre-scaffolded JSON array to <file> with all thread keys,
leases, message IDs, sender guesses, and default "none" answers already
filled in. Edit <file> to fill in quotes, tags, labels, and any extracted
records, then pass it to work submit.

For raw JSON:

    iql --json work next --limit 10        conversations to read, newest first
    iql --json work submit < answers.json  your answers, one per conversation

Repeat until work next returns no items. Narrow with --only receipts,travel or
--query "from:homedepot"; iql elements shows each element in full.

work next returns:

  elements   the definition of every element asked in this batch: its
             instructions, unit, shape, levels or fields. Read them; they are
             the question.
  topics     topics already in use; prefer them to new near-synonyms
  items      one per conversation:
     thread, lease   echo both back in your answer
     messages        oldest first: id, date, from, to, subject, mine (sent by
                     the user), text, attachments with their text
     earlier         how many older messages were left out
     people          who is in it, with what is known: kind, me
     ask             what to answer: element, and the messages (or senders)
                     it is about

Each conversation is leased to you for 30 minutes.

## The answer

One JSON object per conversation; send one, a JSON array of them, or one per
line.

    {
      "thread": "<thread>", "lease": "<lease>", "model": "<your model id>",
      "labels": ["receipts"], "archive": false, "acceptTickets": true,
      "answers": {
        "waiting":    {"level": "on-me", "confidence": 0.85,
                       "why": "Alice asked for the contract by Friday",
                       "quote": "Can you send it back by Friday?"},
        "importance": {"level": "important", "quote": "by Friday"},
        "tasks":      {"records": [{"what": "Send Alice the signed contract",
                       "owner": "me", "due": "2026-10-24",
                       "quote": "Can you send it back by Friday?"}]},
        "topics":     {"tags": ["contract", "acme"]},
        "receipts":   {"<message id>": {"none": "a contract, not a purchase"}},
        "travel":     {"<message id>": {"none": "no travel"}},
        "events":     {"<message id>": {"none": "no event with a time"}},
        "sender":     {"alice@acme.com": {"level": "person"}}
      }
    }

  - Optional labels adds mail labels (with origin=agent), archive: true
    archives the conversation, and acceptTickets: true (or --accept-tickets)
    places created task tickets directly on the board (status: todo) in the
    same step — no separate iql mail label or iql ticket accept calls needed.
  - A conversation element (unit thread) takes one answer. A message element
    takes one answer per message id it was asked about; sender takes one per
    address.
  - An answer is exactly one of: level, records, tags, none (does not apply,
    with why) or skip (cannot tell, with why).
  - Every level (except sender's) and every record needs a quote: words copied
    exactly from the conversation that show it. "…" may join pieces. A quote
    that is not there refuses the whole answer.
  - Records use only the element's fields, with their types: numbers as
    numbers, dates as 2026-10-24, times as 2026-10-24T14:30 (local, as
    written). Lists are lists of objects.
  - confidence is 0 to 1 and should mean it: it is checked against the user's
    corrections.
  - Answer every element asked, for every target. A problem stores nothing for
    that conversation and comes back listed; fix it and send again.

## What happens to your answers

  - receipts, travel and the rest become searchable:
        iql query "extract:receipts.items.brand=DeWalt"
        iql query "extract:travel.kind=flight after:2026-01-01"
  - tasks become proposed tickets, events become proposed calendar events: the
    user approves them.
  - waiting drives "awaiting:me"; sender sets a contact's kind; topics are
    topic: terms.
  - The user's corrections outrank you. iql annotate score <element> --model
    <id> measures you against them. iql work clear --model <id> undoes a run.

## Reading well

  - Brands: receipts abbreviate. "885911470902 FLEXVOLT 20-" is DeWalt
    (UPC prefix 885911); name the brand when you can tell, leave it out when
    you cannot.
  - Waiting is a judgement, not who wrote last: "Thanks!" closes;
    "I'll get back to you Monday" from the user means it waits on the user.
  - The user is whoever sent the messages marked mine.
`
