from chrome import *

W = [
 ('w1', 'Morning pass', 'Get to “nothing needs me” in one short session.', 'You, on opening the app', 'Today → To review → Subject / Conversation',
  [('Open InboxQL', '—', 'Today: one sentence of state, then Replies you owe', '“Today, heading level 1. You owe 4 replies, there are 9 things to review and 1 message waiting to send.”'),
   ('Archive whole low-value groups', '<kbd>Tab</kbd> to “Arrived since yesterday”, Archive on Newsletters', 'Toast: Archived 41, Undo', '“Archived 41 newsletters and promotions. Undo, button.”'),
   ('Answer the owed replies, top down', '<kbd>↓</kbd> <kbd>r</kbd>', 'Reply page beside the conversation (W3)', '“Reply to Rafael Ortiz, heading level 1.”'),
   ('Work the review queue', '<kbd>g r</kbd>, then <kbd>a</kbd> / <kbd>⌫</kbd> per item', 'One item at a time on the right', 'Each option’s name: kind, subject, who proposed it'),
   ('Done', '—', 'Today shows “Nothing needs you” and when the agent next runs', '“Nothing needs you. The agent last read your mail at 8:09.”')],
  'Every archive, approval and send shows Undo for 10 seconds and is listed in History afterwards.', 'Objective 2 · Value 1'),
 ('w2', 'Work one subject', 'Deal with everything about one person or topic, then return to exactly where you were.', 'You, from any row or by name', 'Today / Palette → Subject → Conversation → back',
  [('Open the subject', '<kbd>f</kbd> on a row, or <kbd>⌘K</kbd> and type a name', 'Subject page: open loops first, then one stream', '“Rafael Ortiz, heading level 1. Open loops, heading level 2.”'),
   ('Settle each open loop', '<kbd>↵</kbd> to open, <kbd>r</kbd> reply, “Nudge”, or “Not waiting”', 'The loop moves out of the list as it is settled', 'Polite announcement: “1 open loop left.”'),
   ('Look across kinds', '<kbd>1</kbd>–<kbd>6</kbd> for All / Mail / Files / Cards / Events / Money', 'The stream narrows; the query in the app bar gains a term', '“Files, toggle button, pressed. 4 items.”'),
   ('Return', '<kbd>Esc</kbd> or <kbd>u</kbd>', 'Today, same scroll position, focus on the row you left from', '“Rafael Ortiz… Replies you owe, 3 items.”')],
  'Leaving a subject never loses state; nothing to close.', 'Objective 1 · Value 5'),
 ('w3', 'Reply with the agent’s draft', 'Send a reply the agent wrote, after deciding what only you can decide.', 'The agent (drafts), then you', 'Conversation → Reply → (To review)',
  [('See that a draft exists', '—', '“Reply drafted” in the conversation’s evidence panel, with “1 blank”', '“Reply drafted, heading level 3. Agent draft, not sent. 1 blank.”'),
   ('Open it', '<kbd>d</kbd>', 'Reply page; the decision as a radio group above the text', '“Your decision: which finish? group. Brushed brass, radio button, 1 of 3.”'),
   ('Decide', '<kbd>↓</kbd> to choose', 'The draft text updates; Send turns on', '“Matte black, selected. Send is now available.”'),
   ('Read and edit', '<kbd>Tab</kbd> into the message', 'The draft is now yours (“started by the agent, edited by you”)', '—'),
   ('Send', '<kbd>⌘↵</kbd>', 'Toast with a 10-second countdown and Undo', '“Sending to Rafael Ortiz and Jordan Lee in 10 seconds. Undo, button.”')],
  'Undo within 10 s; afterwards it has gone, as with any mail.', 'Value 1 · Objective 3'),
 ('w4', 'Clear a group', 'Archive or label many messages at once, safely.', 'You', 'Today or Ask',
  [('Open the group', '“Look first” on a Today group, or a query in Ask', 'Messages view of that query', '“Newsletters and promotions, 41 messages.”'),
   ('Check it', '<kbd>↓</kbd> through, or read the plain-language sentence', 'Sentence says what is left out (Trash, Spam) and agent coverage', '—'),
   ('Select', '<kbd>x</kbd> on rows, or “Select all 41”', 'Selection bar with the count', '“41 selected.”'),
   ('Act', '<kbd>e</kbd> archive, <kbd>l</kbd> label', 'Toast with Undo; History records the query and the ids', '“Archived 41. Undo, button.”')],
  'Undo restores exactly the messages changed, here and on the server. Changing the view clears the selection and says so.', 'Value 2'),
 ('w5', 'Correct the agent', 'Fix a wrong conclusion so it stays fixed and teaches the agent.', 'You, wherever a conclusion shows', 'Conversation (evidence panel), Today, Activity',
  [('See the conclusion and its evidence', '—', 'A card with the conclusion and the quoted words', '“Waiting on you, heading level 3. Quote: Can you pick…”'),
   ('Correct it', '“On Rafael” / “No one”, or <kbd>n</kbd> on a Today row', 'The chip changes; “your ruling” replaces “agent”', '“Ruled: waiting on Rafael.”'),
   ('Optionally undo a whole pass', 'Activity → “Undo the whole pass”', 'Every change in the pass is reversed', '“Undid 6 archives and 3 labels.”')],
  'A ruling outranks the agent, survives re-runs, and counts as an inflow correction (not a review measurement).', 'Objective 3 · Value 4'),
 ('w6', 'Check the agent', 'Measure how often the agent is right, honestly.', 'You, when checks are due', 'To review → Check the agent',
  [('Start a check', '<kbd>↓</kbd> to a “check” item', 'The conversation, with the agent’s answer hidden', '“Check: is Saturday carpool waiting on you? The agent’s answer is hidden until you answer.”'),
   ('Give your answer', '<kbd>1</kbd> / <kbd>2</kbd> / <kbd>3</kbd>', 'Then the agent’s answer is revealed, agreeing or not', '“You said on you. The agent said on you. Agreed.”'),
   ('Disagreements', '—', 'Flagged for a deeper re-read; listed in Activity', '—')],
  'A check can be changed until the next pass.', 'Value 4'),
 ('w7', 'Ask a question of the mail', 'Answer “how much / how many / who” without leaving the app.', 'You', 'Palette or query bar → Ask',
  [('Start', '<kbd>/</kbd> in the query bar, or <kbd>⌘K</kbd> and type', 'Suggestions as you type: fields, real addresses, labels', 'Combobox suggestions, count announced'),
   ('Shape it by clicking', '“+ Add a filter”, suggestion chips', 'Chips in words; the text updates with each', '“Added: not from me.”'),
   ('Choose how to see it', 'Show as: Messages / Conversations / Total by month / …', 'A chart with a sentence summary and a table view', '“Total spent by month, 2026. 15,907 dollars in all. Show as a table, collapsed.”'),
   ('Keep it', '<kbd>p</kbd> pin as a subject, or Hand to an agent', 'It appears under Subjects', '“Pinned Receipts 2026.”')],
  'Nothing to undo — asking changes nothing.', 'Objective 4'),
 ('w8', 'Find out what happened', 'Answer “why did this message disappear?”', 'You', 'Palette → Activity, or a message’s history',
  [('Search for it', '<kbd>⌘K</kbd>, type words from it', 'It is found even if archived; its location is shown', '“Hardware Depot promotion, archived by Claude at 8:09.”'),
   ('See who and why', 'Open it; the evidence panel says who archived it and the judgement behind it', '“Ignorable — promotion from a merchant” with the quote', '—'),
   ('Undo if wrong', 'Undo on the Activity entry, then correct (W5)', 'Back in the inbox; the ruling stops it recurring', '“Moved back to the inbox.”')],
  'Every change by anyone other than you is in Activity with its own Undo.', 'Value 2'),
 ('w9', 'Look ahead', 'Once a week, see the next two weeks and the next two months, and settle anything that will be a problem.', 'You, typically Sunday evening', 'Today → Ahead (two weeks) → Ahead (two months)',
  [('Open Ahead', '<kbd>g a</kbd>, or a line in Today’s “Ahead” section', 'One sentence comparing next week with your usual, then countdowns', '“Ahead, heading level 1. Next week is your busiest in a month: 14 hours booked, 2 replies due, 1 card due and one overlap on Thursday.”'),
   ('Scan the week', 'Read the at-a-glance strip; <kbd>H</kbd> (heading navigation) day by day', 'Busy and empty days by bar and number; amber marks', '“Thursday, November 5, heading level 3. Dentist, Dr. Mehta, 3 PM, overlaps.”'),
   ('Settle what conflicts', '<kbd>↵</kbd> on an item; “Move one”, approve a proposal, or reply', 'The evidence panel lists what to settle before Monday', '“2 things left to settle.”'),
   ('Look further', '<kbd>m</kbd>', 'November and December: a sentence, a grid, and “What matters”', '“November. 31 events, 38 hours booked, 4 things due, 1 trip, 1 important date.”'),
   ('Prepare for what has a lead time', '<kbd>↵</kbd> on “Start renewing your passport”', 'The date’s page with its source and a “Make a card” action', '“Passport expires, Thursday January 14 2027. Start renewing by Thursday December 3.”')],
  'Looking changes nothing. Each fix (moving an event, approving, replying) has its own Undo.', 'Objective 2 · Value 5'),
 ('w11', 'Ask in words', 'Get an answer to a question you cannot easily write as a query, with the evidence behind it.', 'You', 'Query bar → Ask in words → (subject)',
  [('Type the question', '<kbd>/</kbd>, then words; <kbd>⌘↵</kbd> to ask', 'Two choices: Ask Claude Code, or search for the words', '“Ask Claude Code, option 1 of 2.”'),
   ('Wait, or stop', '<kbd>Esc</kbd> stops', 'Its steps, in words, as the commands run', '“Adding up receipts in Kitchen remodel. Done.” announced step by step'),
   ('Read the answer and check it', '<kbd>Tab</kbd> to the evidence', 'The answer, the rows and quote it rests on, the commands it ran, and what was sent', '“Answer, heading level 2. You have spent 5,412 dollars 20…”'),
   ('Keep it', 'A button under “Keep it”', 'A section on the subject, a saved query, or a reminder', '“Added Kitchen money to Kitchen remodel. Undo, button.”'),
   ('Follow up', '<kbd>f</kbd>, type, <kbd>↵</kbd>', 'Same session; a new answer below', '—')],
  'Asking changes nothing. The agent may only read; what it suggests is a button, and each button has Undo.', 'Objective 4 · Value 4'),
 ('w12', 'Put InboxQL on your phone', 'Use the mailbox from your phone, and get reminders there.', 'You, once per phone', 'Settings → This computer → Phone; the phone’s Chrome',
  [('Open the address on the phone', 'Scan the QR code in Settings', 'The phone shows a 4-digit code and waits', '“Waiting for your Mac. Code 4 7 1 9.”'),
   ('Allow it on the Mac', '“Allow this phone” on the card at the top of Today', 'The card names the phone and the Tailscale account', '“A phone wants to use your mailbox, Pixel 8. Allow this phone, button.”'),
   ('Install', '“Install” in Chrome’s sheet', 'An InboxQL icon on the home screen', 'Android’s own install announcement'),
   ('Get a reminder', '—', 'An Android notification with Open and Remind me tomorrow', 'Android reads the reminder line'),
   ('Open it', 'Tap Open', 'The subject the reminder is about', '“Q4 report, heading level 1.”')],
  'Sign a phone out in Settings → This computer → Phone; it can no longer open the mailbox.', 'Objective 6 · Value 3'),
 ('w10', 'Mark a date as important', 'Make a date count down on Today and Ahead, with a lead time if it needs preparing for.', 'You; or the agent suggests and you accept', 'Any event, card, trip or message → Ahead',
  [('Mark it', '<kbd>i</kbd> on an event, card, trip or message; or “+ Mark a date as important”', 'A small form: which date, repeats yearly, lead time (none, 1 week, 6 weeks, custom)', '“Mark as important, dialog. Date: Thursday January 14 2027. Lead time, combo box, none.”'),
   ('Set a lead time', 'Choose “6 weeks”', 'The countdown shows both: the date and when to start', '“Start renewing by Thursday December 3, in 33 days.”'),
   ('Or accept a suggestion', '“Mark important” on an agent suggestion in Ahead or Review', 'The dashed violet card becomes a countdown', '“Car registration renewal marked important. Undo, button.”'),
   ('Decline a suggestion', '“Not important”', 'It goes; the agent stops suggesting that kind from that sender', '“Not important. The agent will not suggest registration reminders from this sender again.”'),
   ('Unmark later', '<kbd>i</kbd> again, or Remove on the countdown', 'It leaves the countdowns; the event or card is unchanged', '“No longer important.”')],
  'Marking and unmarking are instant and undoable; neither changes the event, card or message itself.', 'Value 1 · Objective 7'),
]

body = head('Workflows', 'Every workflow the redesign supports, step by step: keys, what you see, what a screen reader hears, and what can be undone.')
body += banner('workflows.html')
body += '<main id="main" class="main" style="max-width:68rem;margin:0 auto" aria-labelledby="page-title">\n'
body += '''<h1 id="page-title">Workflows</h1>
<p class="muted">Each workflow says what it is for, who starts it, the screens it passes through, and every step: what you do, the key, what you see, and what a screen reader hears. Spoken text is illustrative.</p>
<nav aria-label="Workflows on this page"><ol>''' + ''.join(f'<li><a href="#{w[0]}">{w[1]}</a> — {w[2]}</li>' for w in W) + '</ol></nav>'
for wid, name, goal, who, screens, steps, undo, serves in W:
    rows = ''.join(f'<tr><td>{i}</td><td>{a}</td><td>{k}</td><td>{s}</td><td>{sr}</td></tr>' for i, (a, k, s, sr) in enumerate(steps, 1))
    body += f'''
<section class="section card" style="padding:1rem 1.2rem" id="{wid}" aria-labelledby="{wid}-h">
  <h2 id="{wid}-h">{wid.upper()} · {name}</h2>
  <dl class="kv" style="margin:.5rem 0 .75rem"><dt>Goal</dt><dd>{goal}</dd><dt>Starts</dt><dd>{who}</dd><dt>Screens</dt><dd>{screens}</dd><dt>Serves</dt><dd>{serves}</dd></dl>
  <div style="overflow-x:auto" tabindex="0" role="region" aria-label="Steps of {name}"><table class="doc"><caption class="sr-only">Steps of {name}</caption>
    <thead><tr><th scope="col">#</th><th scope="col">You</th><th scope="col">Keys</th><th scope="col">You see</th><th scope="col">Screen reader hears</th></tr></thead>
    <tbody>{rows}</tbody></table></div>
  <p class="small"><strong>Undo.</strong> {undo}</p>
</section>'''
body += '\n</main>\n' + foot()
write('workflows.html', body)
