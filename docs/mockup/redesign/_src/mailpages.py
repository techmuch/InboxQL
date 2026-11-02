from chrome import *


def row(lead, who, line, meta='', cls='', chips='', label=None):
    c = f' class="row {cls}"'.replace(' "', '"') if cls else ' class="row"'
    cur = ' aria-current="true"' if 'selected' in cls else ''
    return (f'<li{c}{cur}>{lead}<div><span class="who">{who}</span> {chips}'
            f'<div class="line">{line}</div></div><span class="meta">{meta}</span></li>')


AV = lambda s: f'<span class="avatar" aria-hidden="true">{s}</span>'

# =================================================================== Replies I owe
main = f'''
  <div class="page-head"><div class="grow">
    <p class="crumbs"><a href="today.html">Today</a></p>
    <h1 id="page-title">Replies I owe</h1>
    <p class="muted" style="margin-top:.3rem">{C(1)} You owe <strong>4 replies</strong>; 2 have a deadline in the next 3 days. You are waiting on others in <strong>7</strong> conversations; 2 of those are over a week old.</p>
  </div></div>
  <nav class="tabs" aria-label="Direction">
    <a href="replies.html" aria-current="page">I owe · 4</a><a href="#waiting">Waiting on others · 7</a><a href="#let-go">Let go recently · 12</a>
  </nav>

  <section class="section" aria-labelledby="h-deadline">
    <header><h2 id="h-deadline" class="grow">With a deadline {C(2)}</h2></header>
    <ul class="list card" aria-label="Replies with a deadline, soonest first">
      {row(AV('RO'), 'Rafael Ortiz', '“Can you pick brushed brass or matte black by Monday so I can order?”',
           '<strong>by Mon</strong> · 2 days<br><span class="chip agent">agent: on you</span>', 'selected',
           '<span class="chip">Kitchen remodel</span> <span class="chip agent">draft ready · 1 blank</span>')}
      {row(AV('PR'), 'Priya Raman', '“Could you look at section 3 before Tuesday’s call?”',
           '<strong>by Tue 2 PM</strong> · 1 day<br><span class="chip agent">agent: on you</span>', '', '<span class="chip">Q4 report</span>')}
    </ul>
  </section>

  <section class="section" aria-labelledby="h-nodeadline">
    <header><h2 id="h-nodeadline" class="grow">No deadline</h2></header>
    <ul class="list card" aria-label="Replies without a deadline, oldest first">
      {row(AV('NB'), 'Northwind Bank', 'Confirm the new payee “Ortiz Renovations” before the first transfer — an action, not a reply', '3 h<br><span class="chip agent">agent: on you</span>', '', '<span class="chip">system</span>')}
      {row(AV('DW'), 'Dana Whitfield', '“Are you able to take the 8:15 run this week?”', '5 h<br><span class="chip agent">agent: on you</span>')}
    </ul>
  </section>

  <section class="section" aria-labelledby="h-old">
    <header><h2 id="h-old" class="grow">Older than two weeks {C(3)}</h2></header>
    <p class="card small" style="padding:.6rem .85rem">None. When a reply has waited two weeks it moves here with one question: <em>still owed?</em> — “Reply”, “Not owed any more” or “Remind me in a week”.</p>
  </section>

  <div class="selbar" role="region" aria-label="Selection" style="margin-top:1rem">{C(4)} <strong>Example:</strong> 2 selected · <button class="btn">Not waiting on me</button><button class="btn">Remind me Monday</button><button class="btn">Hand to the agent to draft</button></div>
'''
main += notes('n-replies',
    'Every conversation the <code>waiting</code> element judged to be on you, in one list — the full version of the first section of Today. A tab turns it round to what you are waiting for from others.',
    ['One sentence of state for both directions, so the page never only shows half the picture.',
     '<strong>Deadline first.</strong> A reply with a date in it (“by Monday”) sorts by that date; the rest by age. The deadline is read by the agent and shown with its quote in the evidence panel <span class="planned">planned</span>.',
     'Old owed replies are asked about, not left to rot: a reply owed for two weeks is either still owed or it is not.',
     'Bulk actions on a selection. “Not waiting on me” records your ruling on each conversation; nothing is sent.'],
    ['Today this list exists only as a query (<code>awaiting:me</code>) and as counts on a contact card.',
     '“Waiting on others” replaces looking through Sent for unanswered mail.'],
    [('<kbd>g o</kbd>', 'Go to Replies I owe'), ('<kbd>r</kbd> <kbd>↵</kbd>', 'Reply · open'), ('<kbd>d</kbd>', 'Open the agent’s draft, if there is one'),
     ('<kbd>n</kbd>', 'Not waiting on me'), ('<kbd>x</kbd>', 'Select'), ('<kbd>]</kbd>', 'Waiting on others tab')],
    'Each row is one list item with the person, the quote, the deadline and the agent’s judgement in reading order. The selected row is <code>aria-current</code>. The tabs are links with <code>aria-current="page"</code>.',
    '<a href="workflows.html#w1">W1 Morning pass</a> · <a href="workflows.html#w3">W3 Reply with the agent’s draft</a> · <a href="workflows.html#w5">W5 Correct the agent</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-r"><h2 id="ev-r">Rafael Ortiz · Cabinet hardware</h2>
  <p class="small muted">Selected. Why the agent thinks you owe this:</p>
  <h3>Waiting on you</h3><blockquote class="quote">Can you pick brushed brass or matte black by Monday so I can order?<cite>Rafael, Oct 29 · last message in the conversation</cite></blockquote>
  <h3>Deadline</h3><blockquote class="quote">by Monday<cite>read as Mon, Nov 2 · <a href="#">wrong date?</a></cite></blockquote>
  <h3>Draft</h3><p class="small">The agent wrote a reply with <strong>1 blank</strong> for you to fill: which finish. <a href="compose.html">Open the draft</a></p>
  <div style="display:flex;gap:.4rem;flex-wrap:wrap;margin-top:.6rem"><button class="btn">On Rafael</button><button class="btn">No one</button></div>
</aside>'''
page('replies.html', 'Replies I owe', 'Every conversation waiting on you, deadlines first, with the other direction one tab away.', 'awaiting:me', 'Replies I owe', main, aside)

# =================================================================== Drafts and outbox
main = f'''
  <div class="page-head"><div class="grow">
    <h1 id="page-title">Drafts and outbox</h1>
    <p class="muted" style="margin-top:.3rem">{C(1)} <strong>1 waiting for you to send</strong>, 1 failed, 2 drafts. Nothing is sending right now.</p>
  </div><a class="btn primary" href="compose.html">Write <kbd>c</kbd></a></div>

  <section class="section" aria-labelledby="h-approve">
    <header><h2 id="h-approve" class="grow">Waiting for you to send {C(2)}</h2></header>
    <ul class="list card">
      {row(AV('DW'), 'To Dana Whitfield · Re: Saturday carpool', '“Yes — I can take the 8:15 run on Saturday. I’ll pick up Maya and Leo at…”',
           '<button class="btn primary">Send…</button> <a class="btn" href="compose.html">Edit</a> <button class="btn quiet">Return to drafts</button>',
           'selected', '<span class="chip agent">written by the agent</span> <span class="chip">not edited</span>')}
    </ul>
  </section>

  <section class="section" aria-labelledby="h-failed">
    <header><h2 id="h-failed" class="grow">Failed</h2></header>
    <ul class="list card">
      {row('<span class="chip bad" style="align-self:start">failed</span>', 'To Sam Okafor · Q4 numbers', 'The server refused it: “552 message size exceeds limit (25 MB)”. The attachment Q4-model.xlsx is 31 MB.',
           '<a class="btn" href="compose.html">Fix and resend</a>', '', '<span class="chip">yours</span>')}
    </ul>
  </section>

  <section class="section" aria-labelledby="h-drafts">
    <header><h2 id="h-drafts" class="grow">Drafts</h2></header>
    <ul class="list card">
      {row(AV('RO'), 'To Rafael Ortiz · Re: Cabinet hardware', 'Thanks Rafael — let’s go with ____. Monday delivery works…', '<a class="btn" href="compose.html">Open</a>', '', '<span class="chip agent">started by the agent</span> <span class="chip warn">1 blank</span>')}
      {row(AV('—'), 'No recipient · Thank-you note', 'Notes for the ward newsletter…', '<a class="btn" href="compose.html">Open</a>', 'read', '<span class="chip">yours · Oct 20</span>')}
    </ul>
  </section>

  <section class="section" aria-labelledby="h-sent">
    <header><h2 id="h-sent" class="grow">Sent in the last 7 days <span class="muted">· 23</span></h2><a class="small" href="everything.html">Open Sent</a></header>
    <p class="small muted">Folded by default; the outbox is about what has not gone yet.</p>
  </section>
  <figure style="margin:.5rem 0 0"><figcaption class="small muted">Example — Send pressed:</figcaption><div class="toast demo">Sending to Dana Whitfield in 8 s <button type="button">Undo</button></div></figure>
'''
main += notes('n-outbox',
    'Everything outgoing that has not gone yet: what waits for your Send, what failed and why, and drafts. It is the same queue the CLI’s <code>iql send</code> fills, so an agent’s mail always stops here.',
    ['The sentence names what needs you; “nothing is sending” is said, not implied by an empty list.',
     '<strong>Agent mail waits for you.</strong> “Written by the agent · not edited” is shown until you change a word. Send asks once, showing the exact message, and then waits 10 seconds with Undo.'],
    ['Replaces Intelligence → Waiting for approval and the Drafts folder in Desk.',
     'A failure says the server’s reason and what to do, instead of only a status.'],
    [('<kbd>g d</kbd>', 'Go to Drafts and outbox'), ('<kbd>↵</kbd>', 'Open'), ('<kbd>⌘↵</kbd>', 'Send the selected message (asks first)'), ('<kbd>z</kbd>', 'Undo the last send while the countdown runs')],
    'Each section is a heading; each message one list item: recipient and subject, who wrote it, the first line, then its buttons. The countdown toast is a polite live region announced once, not every second.',
    '<a href="workflows.html#w3">W3 Reply with the agent’s draft</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-o"><h2 id="ev-o">What will be sent</h2>
  <dl class="kv" style="margin:.5rem 0 .8rem"><dt>From</dt><dd>you@example.com</dd><dt>To</dt><dd>Dana Whitfield &lt;dana@example.org&gt;</dd><dt>Subject</dt><dd>Re: Saturday carpool</dd><dt>In reply to</dt><dd>Dana, Oct 31, 9:12 AM</dd><dt>Written by</dt><dd><span class="chip agent">Claude</span>, from your note “say yes”</dd></dl>
  <article class="card small" style="padding:.6rem .7rem"><p>Yes — I can take the 8:15 run on Saturday. I’ll pick up Maya and Leo at your place and drop them at the field by 8:40.</p><p>— Alex</p></article>
  <p class="small"><a href="#">Show the exact message (headers and all)</a></p>
</aside>'''
page('outbox.html', 'Drafts and outbox', 'Outgoing mail that has not gone: waiting for your Send, failed with a reason, and drafts.', 'in:drafts', 'Drafts and outbox', main, aside)

# =================================================================== Inbox
def mrow(av, who, subj, snip, when, chips='', cls='', imp=''):
    return row(f'<span style="display:flex;gap:.45rem;align-items:start"><input type="checkbox" aria-label="Select {who}: {subj}" style="margin-top:.55rem"{" checked" if "selected" in cls or who == "Grandma Ruth" else ""}>{AV(av)}</span>', who,
               f'<strong style="font-weight:inherit">{subj}</strong> — {snip}', f'{when}{imp}', cls, chips)


main = f'''
  <div class="page-head"><div class="grow">
    <h1 id="page-title">Inbox <span class="muted" style="font-weight:400">· 117</span></h1>
    <p class="muted" style="margin-top:.3rem">{C(1)} 9 unread from people · 41 low or ignorable · 2 not yet read by the agent.</p>
  </div>
  <div role="group" aria-label="Arrange" style="display:flex;gap:.3rem"><button class="btn" aria-pressed="true">By time</button><button class="btn" aria-pressed="false">By subject</button></div></div>
  <div class="filters" role="group" aria-label="Filter">{C(2)}
    <button class="chip" aria-pressed="false">Unread</button><button class="chip" aria-pressed="true">From people</button><button class="chip" aria-pressed="false">Important</button>
    <button class="chip" aria-pressed="false">Has files</button><button class="chip" aria-pressed="false">Owed a reply</button>
    <span class="small muted">· adds <code>sentby:person</code> to the query</span></div>
  <div class="selbar" role="region" aria-label="Selection">{C(3)} 2 selected · <button class="btn">Archive <kbd>e</kbd></button><button class="btn">Label… <kbd>l</kbd></button><button class="btn">Mark read</button><button class="btn quiet">Clear</button></div>
  <h2 class="sr-only">Messages</h2>
  <ul class="list card" aria-label="Inbox, from people, newest first">
    {mrow('DW', 'Dana Whitfield', 'Saturday carpool', 'Are you able to take the 8:15 run this week?', '9:12 AM', '<span class="chip warn">owed</span>', 'selected')}
    {mrow('GR', 'Grandma Ruth', 'Photos from the weekend', 'The kids look so tall! Here are the ones from the pumpkin patch', '8:40 AM', '<span class="chip">📎 6</span>')}
    {mrow('SO', 'Sam Okafor', 'Re: Q4 numbers', 'The model is too big to email — can you share a link?', 'Fri', '<span class="chip">Q4 report</span>')}
    {mrow('PR', 'Priya Raman', 'Draft for review', 'Could you look at section 3 before Tuesday’s call?', 'Fri', '<span class="chip warn">owed</span> <span class="chip">Q4 report</span>')}
    {mrow('RO', 'Rafael Ortiz', 'Cabinet hardware', 'Can you pick brushed brass or matte black by Monday…', 'Thu', '<span class="chip warn">owed</span> <span class="chip">Kitchen remodel</span> <span class="chip">📎 2</span>')}
    {mrow('JL', 'Jordan Lee', 'Lunch Friday?', 'Noon at the usual place works for me', 'Thu', '', 'read')}
    {mrow('MK', 'Maya’s school', 'Conference sign-up', 'Your slot: Thursday, Nov 5, 3:15 PM', 'Wed', '<span class="chip agent">event found</span>', 'read')}
  </ul>
  <p class="small muted" style="margin-top:.4rem">7 of 23 from people. <a href="#">Show all 117</a> · the other 94 are from organisations and systems.</p>
'''
main += notes('n-inbox',
    'The mailbox as a list, for when you want to go through mail yourself rather than by subject. It is a query — <code>folder:inbox</code> — with filters that add terms to it, so anything you can do here you can save as a subject.',
    ['The sentence says what is in the inbox and what the agent has not read, before any rows.',
     'Filters are toggle buttons; each one shows the term it adds, so the query bar teaches the language.',
     'Selection works as now (<kbd>x</kbd>, shift-click, select all matching). Changing a filter clears the selection and says so.'],
    ['The Desk’s rail, query and results columns become this page plus Subjects in the navigation.',
     '“By subject” groups the inbox the way Today does, for an inbox-zero pass.',
     'Opening a message goes to the Conversation page in the middle column; the right column shows the selected one’s summary.'],
    [('<kbd>g i</kbd>', 'Go to Inbox'), ('<kbd>j</kbd> <kbd>k</kbd> / <kbd>↑</kbd> <kbd>↓</kbd>', 'Move'), ('<kbd>x</kbd>', 'Select'), ('<kbd>e</kbd> <kbd>l</kbd> <kbd>#</kbd>', 'Archive · label · trash'), ('<kbd>↵</kbd>', 'Open the conversation')],
    'Each message is one list item: sender, subject, first line, time and chips, in that order. Every checkbox is labelled with sender and subject. Unread is said in words (“unread”), not only shown by weight.',
    '<a href="workflows.html#w4">W4 Clear a group</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-i"><h2 id="ev-i">Saturday carpool</h2>
  <p class="small muted">Dana Whitfield · 9:12 AM · 3 messages</p>
  <h3>Summary</h3><p class="small">Dana asks if you can drive the 8:15 run on Saturday Nov 7. Jordan already said he can’t.</p>
  <h3>Agent concluded</h3><ul class="small" style="padding-left:1.1rem"><li><span class="chip agent">waiting: on you</span></li><li><span class="chip agent">importance: normal</span></li><li>No event proposed — not yet agreed</li></ul>
  <div style="display:flex;gap:.4rem;flex-wrap:wrap;margin-top:.6rem"><a class="btn primary" href="compose.html">Reply</a><a class="btn" href="conversation.html">Open</a></div>
</aside>'''
page('inbox.html', 'Inbox', 'The inbox as a list with filters that are query terms, selection and bulk actions.', 'folder:inbox sentby:person', 'Inbox', main, aside)

# =================================================================== Everything
main = f'''
  <div class="page-head"><div class="grow">
    <h1 id="page-title">Everything</h1>
    <p class="muted" style="margin-top:.3rem">{C(1)} All mail in 2 accounts: <strong>48,210 messages</strong>, 2009 to today. Trash and Spam are left out — <strong>1,312</strong> more there. <a href="#">Include them</a></p>
  </div></div>
  <div class="filters" role="group" aria-label="Where">{C(2)}
    <button class="chip" aria-pressed="true">All mail</button><button class="chip" aria-pressed="false">Inbox</button><button class="chip" aria-pressed="false">Archived</button>
    <button class="chip" aria-pressed="false">Sent</button><button class="chip" aria-pressed="false">Starred</button><button class="chip" aria-pressed="false">Trash</button><button class="chip" aria-pressed="false">Spam</button></div>
  <div class="filters" role="group" aria-label="Account"><span class="small muted">Account:</span>
    <button class="chip" aria-pressed="true">Both</button><button class="chip" aria-pressed="false">Personal (Gmail)</button><button class="chip" aria-pressed="false">Work (IMAP)</button></div>
  <div style="overflow-x:auto" tabindex="0" role="region" aria-label="Messages table">
  <table class="grid"><caption class="sr-only">All mail, newest first</caption>
    <thead><tr><th scope="col">From</th><th scope="col">Subject</th><th scope="col">Where {C(3)}</th><th scope="col" class="num">Date</th></tr></thead>
    <tbody>
      <tr class="sel"><td>Dana Whitfield</td><td>Saturday carpool</td><td><span class="chip">Inbox</span></td><td class="num">9:12 AM</td></tr>
      <tr><td>Hardware Depot</td><td>20% off fasteners this weekend</td><td><span class="chip">Archived</span> <span class="chip agent">by agent, 8:09</span></td><td class="num">8:02 AM</td></tr>
      <tr><td>You</td><td>Re: Lunch Friday?</td><td><span class="chip">Sent</span></td><td class="num">Thu</td></tr>
      <tr><td>Lumen Air</td><td>Your trip to Denver: LA 418</td><td><span class="chip">Archived</span> <span class="chip">Travel</span></td><td class="num">Oct 22</td></tr>
      <tr><td>Ortiz Renovations</td><td>Deposit received</td><td><span class="chip">Archived</span> <span class="chip">Receipts</span></td><td class="num">Oct 14</td></tr>
      <tr><td>Priya Raman</td><td>Q4 kickoff notes</td><td><span class="chip">Archived</span> <span class="chip">Work/Q4</span></td><td class="num">Sep 30</td></tr>
    </tbody></table></div>
  <p class="small muted" style="margin-top:.4rem">6 of 48,210 · <a href="query.html">Open in Ask</a> to count, chart or add up.</p>
'''
main += notes('n-every',
    'All your mail, wherever it is: the place to look for something you archived, or to see where a message went. It is <code>folder:allmail</code> with the place and account as buttons.',
    ['The sentence says what is left out and how much, with a way to include it — an empty search is never a mystery.',
     'Where and account are toggle groups, not a folder tree. Mail has labels; a message can be in Sent and under Receipts at once.',
     '<strong>Where</strong> is a column: every row says where the message is and, if something other than you moved it, who did.'],
    ['The folder rows in the Desk rail become these buttons; labels stay under Subjects.',
     'A table rather than a list, because looking things up is comparing columns.'],
    [('<kbd>g e</kbd>', 'Go to Everything'), ('<kbd>/</kbd>', 'Type a query'), ('<kbd>↵</kbd>', 'Open')],
    'A real table with a caption and column headers in a focusable region; the “by agent” chip is read as text in the Where cell.',
    '<a href="workflows.html#w8">W8 Find out what happened</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-e"><h2 id="ev-e">In all mail</h2>
  <table class="grid small"><caption class="sr-only">Messages by place</caption><tbody>
  <tr><th scope="row">Inbox</th><td class="num">117</td></tr><tr><th scope="row">Archived</th><td class="num">44,830</td></tr><tr><th scope="row">Sent</th><td class="num">3,263</td></tr>
  <tr><th scope="row">Trash</th><td class="num">1,204</td></tr><tr><th scope="row">Spam</th><td class="num">108</td></tr></tbody></table>
  <h3 style="margin-top:1rem">By year</h3>
  <ul class="small" style="list-style:none;padding:0;display:grid;gap:.2rem">
    <li><span class="bar-h" style="width:6rem" aria-hidden="true"></span> 2026 · 9,880</li><li><span class="bar-h" style="width:5.4rem" aria-hidden="true"></span> 2025 · 8,920</li>
    <li><span class="bar-h" style="width:4.6rem" aria-hidden="true"></span> 2024 · 7,640</li><li><span class="bar-h" style="width:2rem" aria-hidden="true"></span> 2009–2023 · 21,770</li></ul>
  <p class="small muted">A first sync is newest first: older Gmail mail is still arriving (82% here).</p>
</aside>'''
page('everything.html', 'Everything', 'All mail in every account, with where each message is and who moved it.', 'folder:allmail', 'Everything', main, aside)
