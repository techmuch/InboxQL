from chrome import *

# =================================================================== Calendar
H = 2.6  # rem per hour; 8 AM–8 PM


def evb(title, start, end, sub='', cls='', spoken=''):
    top, ht = (start - 8) * H, max((end - start) * H, 1.3)
    fmt = lambda t: f"{int(t) % 12 or 12}:{int(round((t % 1) * 60)):02d}{' PM' if t >= 12 else ' AM'}"
    tag = {'proposed': ' Proposed by the agent, not on your calendar yet.', 'clash': ' Overlaps another event.'}
    extra = ''.join(v for k, v in tag.items() if k in cls)
    return (f'<li class="ev-b {cls}" style="top:{top:.2f}rem;height:{ht:.2f}rem"><a href="#" style="color:inherit;text-decoration:none">'
            + (f'<strong>{title} <span style="font-weight:400">{fmt(start)}</span></strong><span class="sr-only"> to {fmt(end)}</span>' if end - start <= 1
               else f'<strong>{title}</strong>{fmt(start)}–{fmt(end)}{(" · " + sub) if sub else ""}')
            + f'<span class="sr-only">.{extra}</span></a></li>')


DAYS = [
    ('Mon', 2, 'Monday, November 2', [], [evb('Cabinet delivery', 10.5, 12.5, 'Rafael', 'proposed sel'), evb('Team stand-up', 13, 13.5), evb('Design review', 15, 17)]),
    ('Tue', 3, 'Tuesday, November 3', ['Election Day'], [evb('Planning', 9, 11), evb('Q4 report call', 14, 15, 'Priya, Sam')]),
    ('Wed', 4, 'Wednesday, November 4', [], [evb('Youth activity', 19, 20, 'church')]),
    ('Thu', 5, 'Thursday, November 5', [], [evb('Dentist', 15, 16, 'Dr. Mehta', 'clash half'), evb('Conference', 15.25, 15.75, 'school', 'clash half2')]),
    ('Fri', 6, 'Friday, November 6', [], [evb('Budget workshop', 9, 11.5), evb('Lunch with Jordan', 12, 13)]),
    ('Sat', 7, 'Saturday, November 7', [], []),
    ('Sun', 8, 'Sunday, November 8', [], [evb('Ward council', 9, 10.5)]),
]
cols = ''.join(
    f'<section class="col" aria-labelledby="d{dn}"><h3 id="d{dn}"><span class="sr-only">{full}</span><span aria-hidden="true">{dw}<span class="dn">{dn}</span></span></h3>'
    f'<ul class="allday" aria-label="All day">{"".join(f"<li>{a}</li>" for a in ad)}</ul>'
    f'<ul class="slots" aria-label="Timed events">{"".join(evs)}</ul></section>'
    for dw, dn, full, ad, evs in DAYS)
hours = '<div class="hours" aria-hidden="true"><div style="height:2.6rem"></div><div style="height:1.6rem;border-bottom:1px solid var(--border)"></div><div style="position:relative;height:31.2rem">' + ''.join(
    f'<span style="top:{(h - 8) * H}rem">{h % 12 or 12} {"PM" if h >= 12 else "AM"}</span>' for h in range(9, 20)) + '</div></div>'

main = f'''
  <div class="page-head"><div class="grow">
    <h1 id="page-title">Calendar</h1>
    <p class="muted" style="margin-top:.3rem">{C(1)} Next week: 14 hours booked across 10 events, <strong>1 overlap</strong> (Thursday 3:15) and <strong>1 proposal</strong> waiting for you.</p>
  </div>
  <div style="display:flex;gap:.3rem;flex-wrap:wrap"><button class="btn" aria-label="Previous week">←</button><button class="btn">This week</button><button class="btn" aria-label="Next week">→</button></div></div>
  <nav class="tabs" aria-label="View"><a href="#" >Day</a><a href="calendar.html" aria-current="page">Week</a><a href="ahead-months.html">Month</a><a href="ahead.html">Agenda</a>
    <span style="flex:1"></span><span class="small muted" style="align-self:center">Calendars: Personal, Work, Invitations · <a href="#">Family hidden</a></span></nav>
  <h2 class="sr-only">Week of November 2</h2>
  <div class="week">{hours}{cols}</div>
  <p class="small muted" style="margin-top:.4rem">{C(2)} Solid = on your calendar · <span style="color:var(--agent)">dashed violet = proposed by the agent</span> · <span style="color:var(--warn)">amber edge = overlaps</span>. Each is also said in words.</p>
'''
main += notes('n-cal',
    'The calendar stays a place, because time is a layout people read at a glance. It loses its own approval sidebar (proposals go to Review) and gains the evidence panel: every event says where it came from.',
    ['A sentence about the week first: hours, overlaps, proposals.',
     'Proposals sit on the grid where they would go, marked, so you can see whether they fit before approving. They are not counted in hours booked.'],
    ['Proposals move from the Calendar sidebar to Review, and remain visible here.',
     'Month and Agenda views are the two Ahead views; the calendar is the same data at day and week scale.'],
    [('<kbd>g c</kbd>', 'Go to Calendar'), ('<kbd>←</kbd> <kbd>→</kbd>', 'Previous · next week'), ('<kbd>d</kbd> <kbd>w</kbd> <kbd>m</kbd> <kbd>a</kbd>', 'Day · week · month · agenda'),
     ('<kbd>Tab</kbd>', 'Events in time order'), ('<kbd>a</kbd> / <kbd>⌫</kbd>', 'Approve · reject a selected proposal')],
    'Each day is a region with a heading (“Monday, November 2”) and its events as a list in time order, so the grid is read as an agenda. Each event’s name includes its time and, in words, whether it is proposed or overlaps.',
    '<a href="workflows.html#w9">W9 Look ahead</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-c"><h2 id="ev-c">Cabinet delivery</h2>
  <p class="small"><span class="chip agent">proposed by the agent</span> Mon, Nov 2, 10:30 AM – 12:30 PM</p>
  <h3>From</h3><blockquote class="quote">If the order goes in Monday, we can deliver the cabinets Monday Nov 2 between 10:30 and 12:30.<cite>Rafael Ortiz, Oct 29 · <a href="conversation.html">open</a></cite></blockquote>
  <h3>Fits?</h3><p class="small">Free 10:30–12:30. Team stand-up at 1:00.</p>
  <div style="display:flex;gap:.4rem;flex-wrap:wrap;margin-top:.5rem"><button class="btn primary">Approve <kbd>a</kbd></button><button class="btn">Edit</button><button class="btn quiet">Reject</button></div>
</aside>'''
page('calendar.html', 'Calendar', 'The week view, with agent proposals placed where they would go and every event tied to its source.', 'in:events during:2026-11-02..2026-11-08', 'Calendar', main, aside)

# =================================================================== Board
def kc(title, meta, labels='', cls=''):
    return f'<li class="kcard {cls}"><a href="#" class="t" style="color:inherit;text-decoration:none">{title}</a><div class="m">{labels}{meta}</div></li>'


L = lambda s: f'<span class="chip">{s}</span>'
main = f'''
  <div class="page-head"><div class="grow">
    <h1 id="page-title">Board</h1>
    <p class="muted" style="margin-top:.3rem">{C(1)} 9 open cards; 2 due this week. The agent proposed 2 from mail.</p>
  </div>
  <div role="group" aria-label="Board" style="display:flex;gap:.3rem"><button class="btn" aria-pressed="true">Personal</button><button class="btn" aria-pressed="false">Work</button><button class="btn quiet">+ Board</button></div></div>
  <div class="filters" role="group" aria-label="Filter cards"><button class="chip" aria-pressed="false">Due this week</button><button class="chip" aria-pressed="false">From mail</button><button class="chip" aria-pressed="false">Kitchen remodel</button><button class="chip" aria-pressed="false">Q4 report</button></div>
  <h2 class="sr-only">Lists</h2>
  <div class="board" tabindex="0" role="region" aria-label="Board lists">
    <section class="lane proposed" aria-labelledby="l-p"><h3 id="l-p">Proposed <span class="chip agent">agent</span> {C(2)}</h3><ol>
      {kc('Arrange old-cabinet pick-up', '<span>from “Pick-up of old cabinets”</span>', L('Kitchen remodel'))}
      {kc('Renew car registration', '<span>due Nov 30 · from DMV mail</span>')}
      <li class="small muted" style="padding:.2rem">Accept <kbd>a</kbd> moves a card to To do; Reject keeps it as evidence the agent was wrong.</li></ol></section>
    <section class="lane" aria-labelledby="l-t"><h3 id="l-t">To do <span class="muted">· 4</span></h3><ol>
      {kc('Order cabinet hardware', '<span>due Mon, Nov 2</span><span>✉ 2</span>', L('Kitchen remodel'), 'sel')}
      {kc('Send Q4 numbers to Sam', '<span>due Fri, Nov 6</span><span>✉ 3</span>', L('Q4 report'))}
      {kc('Answer carpool rota', '<span>✉ 1</span>')}
      {kc('Start passport renewal', '<span>start by Dec 3 ★</span>')}</ol></section>
    <section class="lane" aria-labelledby="l-d"><h3 id="l-d">Doing <span class="muted">· 2</span></h3><ol>
      {kc('Book the rough-in inspection', '<span>due Nov 17</span><span>☑ 1/3</span>', L('Kitchen remodel'))}
      {kc('Comments on section 3', '<span>by Tue</span><span>✉ 1</span>', L('Q4 report'))}</ol></section>
    <section class="lane" aria-labelledby="l-x"><h3 id="l-x">Done <span class="muted">· 14</span></h3><ol>
      {kc('Choose countertop', '<span>Oct 20</span>', L('Kitchen remodel'))}
      <li class="small muted" style="padding:.2rem">13 more · <a href="#">show</a></li></ol></section>
  </div>
'''
main += notes('n-board',
    'Your own to-dos and projects. The board stays a place for planning, but cards also show up in their subjects (Kitchen remodel lists its cards) and in Ahead by due date, so you rarely need to come here to find one.',
    ['The sentence counts what is open, due and proposed.',
     'Proposed cards are the first list, dashed and violet. Accepting one is a decision you make; nothing the agent proposes becomes a to-do on its own.'],
    ['Proposed cards are also counted in Review, so the queue is in one place.',
     'Card labels are the same words as mail topics, so filtering by “Kitchen remodel” here and opening the Kitchen remodel subject agree.'],
    [('<kbd>g b</kbd>', 'Go to Board'), ('<kbd>←</kbd> <kbd>→</kbd> <kbd>↑</kbd> <kbd>↓</kbd>', 'Move between cards'), ('<kbd>m</kbd>', 'Move the selected card: choose a list, then a position'),
     ('<kbd>a</kbd> / <kbd>⌫</kbd>', 'Accept · reject a proposed card'), ('<kbd>n</kbd>', 'New card in this list')],
    'Each list is a region with a heading and an ordered list of cards. Moving a card is never drag-only: <kbd>m</kbd> opens a “Move to” menu, and the result is announced (“Moved to Doing, position 1 of 3”).',
    '<a href="workflows.html#w2">W2 Work one subject</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-b"><h2 id="ev-b">Order cabinet hardware</h2>
  <dl class="kv" style="margin:.5rem 0 .8rem"><dt>List</dt><dd>To do</dd><dt>Due</dt><dd>Mon, Nov 2</dd><dt>Label</dt><dd>Kitchen remodel</dd><dt>Raised by</dt><dd><span class="chip agent">agent</span>, accepted by you Oct 26</dd></dl>
  <h3>From mail</h3><ul class="small" style="padding-left:1.1rem"><li><a href="conversation.html">Cabinet hardware</a> — Rafael, Oct 29</li><li>Hardware options — Rafael, Oct 26</li></ul>
  <h3>Checklist</h3><ul class="small" style="list-style:none;padding:0"><li>☐ Pick a finish</li><li>☐ Tell Rafael</li></ul>
</aside>'''
page('board.html', 'Board', 'Lists of cards, with agent proposals first and each card tied to the mail it came from.', 'in:tickets state:open OR state:proposed', 'Board', main, aside)

# =================================================================== Files
main = f'''
  <div class="page-head"><div class="grow">
    <h1 id="page-title">Files</h1>
    <p class="muted" style="margin-top:.3rem">{C(1)} 3,904 files from mail, each counted once however often it was sent. 211 have not been read for text yet, so a search inside files will not find them.</p>
  </div></div>
  <div class="filters" role="group" aria-label="Kind"><button class="chip" aria-pressed="true">All</button><button class="chip" aria-pressed="false">PDFs</button><button class="chip" aria-pressed="false">Documents</button><button class="chip" aria-pressed="false">Sheets</button><button class="chip" aria-pressed="false">Images</button><button class="chip" aria-pressed="false">Sent around (2+)</button><button class="chip" aria-pressed="false">Not read yet · 211</button></div>
  <div style="overflow-x:auto" tabindex="0" role="region" aria-label="Files table">
  <table class="grid"><caption class="sr-only">Files, newest first</caption>
    <thead><tr><th scope="col">Name</th><th scope="col">From</th><th scope="col" class="num">Sent</th><th scope="col">Inside {C(2)}</th><th scope="col" class="num">Size</th><th scope="col" class="num">Latest</th></tr></thead>
    <tbody>
      <tr class="sel"><td>📄 Q4-report-v3.docx</td><td>Priya Raman</td><td class="num">2×</td><td>read · 18 pages</td><td class="num">2.1 MB</td><td class="num">Oct 30</td></tr>
      <tr><td>📄 Quote-v2.pdf</td><td>Rafael Ortiz</td><td class="num">2×</td><td>read · <span class="chip agent">$18,450</span></td><td class="num">380 KB</td><td class="num">Oct 27</td></tr>
      <tr><td>🖼 Hardware-options.jpg</td><td>Rafael Ortiz</td><td class="num">1×</td><td>image · no text</td><td class="num">1.4 MB</td><td class="num">Oct 26</td></tr>
      <tr><td>📄 Boarding-pass-LA418.pdf</td><td>Lumen Air</td><td class="num">1×</td><td>read · <span class="chip agent">trip found</span></td><td class="num">96 KB</td><td class="num">Oct 22</td></tr>
      <tr><td>📄 Scan_0042.pdf</td><td>You</td><td class="num">1×</td><td><span class="chip warn">scan · needs OCR</span></td><td class="num">4.8 MB</td><td class="num">Oct 18</td></tr>
      <tr><td>📊 Budget-2027.xlsx</td><td>Sam Okafor</td><td class="num">4×</td><td><span class="chip">not read yet</span></td><td class="num">640 KB</td><td class="num">Oct 12</td></tr>
    </tbody></table></div>
  <p class="small muted" style="margin-top:.4rem">6 of 3,904 · <a href="query.html">Open in Ask</a> for <code>in:attachments</code> queries.</p>
'''
main += notes('n-files',
    'Every file that arrived or left by mail, as files rather than as attachments: one row per distinct file, with how many times it was sent and whether InboxQL could read inside it.',
    ['The sentence says how many files have not been read, because “no file says that” and “nobody looked” are different answers.',
     '<strong>Inside</strong> says what InboxQL knows about the contents: read, a scan that needs OCR, an image with no text, or not read yet — and what the agent extracted from it.'],
    ['Files also appear in each subject’s stream, filtered to that subject.',
     'Previews open in the right-hand panel, not a new tab.'],
    [('<kbd>g f</kbd>', 'Go to Files'), ('<kbd>space</kbd>', 'Preview the selected file'), ('<kbd>↵</kbd>', 'Show the messages that carried it')],
    'A table with a caption and headers; the Inside column states the reading state in words.',
    '<a href="workflows.html#w7">W7 Ask a question of the mail</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-f"><h2 id="ev-f">Q4-report-v3.docx</h2>
  <div class="card" style="height:9rem;display:grid;place-items:center;background:var(--surface)" role="img" aria-label="Preview of page 1: Q4 Report, draft 3"><span class="muted small">Page 1 preview</span></div>
  <h3 style="margin-top:.8rem">Sent in</h3><ul class="small" style="padding-left:1.1rem"><li>Priya → you, Sam · “v3 attached” · Oct 30</li><li>You → Priya · “one question on 3.2” · Oct 31 (same file, forwarded)</li></ul>
  <h3>Other versions</h3><p class="small">v2 (Oct 21), v1 (Oct 9) — <a href="q4.html">see them in Q4 report</a></p>
</aside>'''
page('files.html', 'Files', 'Files from mail, one row per distinct file, with how often it was sent and what is known about its contents.', 'in:attachments', 'Files', main, aside)

# =================================================================== People
main = f'''
  <div class="page-head"><div class="grow">
    <h1 id="page-title">People</h1>
    <p class="muted" style="margin-top:.3rem">{C(1)} 412 people you have written to or heard from. You owe 4 of them a reply; 7 owe you one. 3 senders could be a person or a system — <a href="#unsure">help sort them</a>.</p>
  </div><button class="btn">+ Add a note or label</button></div>
  <nav class="tabs" aria-label="Kind"><a href="people.html" aria-current="page">People · 412</a><a href="#">Organisations · 186</a><a href="#">Systems · 2,940</a><a href="#unsure">Not sure · 3</a></nav>
  <div class="filters" role="group" aria-label="Filter people"><button class="chip" aria-pressed="false">Owe them</button><button class="chip" aria-pressed="false">Owe me</button><button class="chip" aria-pressed="false">Labelled client</button><button class="chip" aria-pressed="false">Recent</button></div>
  <div style="overflow-x:auto" tabindex="0" role="region" aria-label="People table">
  <table class="grid"><caption class="sr-only">People, by most recent</caption>
    <thead><tr><th scope="col">Name</th><th scope="col">Open loops {C(2)}</th><th scope="col" class="num">Messages</th><th scope="col">Replies</th><th scope="col" class="num">Last</th></tr></thead>
    <tbody>
      <tr class="sel"><td><a href="subject.html">Rafael Ortiz</a><br><span class="small muted">Ortiz Renovations · contractor</span></td><td><span class="chip warn">you owe 1</span> <span class="chip">owes you 1</span></td><td class="num">38</td><td class="small">within a day</td><td class="num">Oct 29</td></tr>
      <tr><td>Dana Whitfield<br><span class="small muted">carpool</span></td><td><span class="chip warn">you owe 1</span></td><td class="num">112</td><td class="small">within hours</td><td class="num">Today</td></tr>
      <tr><td>Priya Raman<br><span class="small muted">work · client</span></td><td><span class="chip warn">you owe 1</span></td><td class="num">264</td><td class="small">same day</td><td class="num">Oct 30</td></tr>
      <tr><td>Sam Okafor<br><span class="small muted">work</span></td><td><span class="chip warn">you owe 1</span></td><td class="num">97</td><td class="small">2–3 days</td><td class="num">Oct 30</td></tr>
      <tr><td>Jordan Lee</td><td><span class="chip">owes you 1</span></td><td class="num">58</td><td class="small">within a day</td><td class="num">Oct 29</td></tr>
      <tr><td>Grandma Ruth</td><td><span class="muted">—</span></td><td class="num">31</td><td class="small">a week</td><td class="num">Oct 4</td></tr>
    </tbody></table></div>
'''
main += notes('n-people',
    'Everyone you correspond with, as a directory for finding a person and seeing at a glance who is owed what. Opening a person opens their subject page.',
    ['The sentence counts open loops in both directions and how many senders still need sorting into person or system.',
     'Open loops come from the agent’s <code>waiting</code> judgements; a person nobody has judged shows a dash, not “nothing owed”.'],
    ['The contact card becomes the person’s subject page.',
     'People, organisations and systems are separate tabs, so 2,940 mailers don’t bury 412 people.'],
    [('<kbd>g p</kbd>', 'Go to People'), ('<kbd>/</kbd>', 'Find by name or address'), ('<kbd>↵</kbd>', 'Open the person’s page')],
    'A table with a caption and headers; open loops are read as text (“you owe 1, owes you 1”).',
    '<a href="workflows.html#w2">W2 Work one subject</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-p"><h2 id="ev-p">Rafael Ortiz</h2>
  <dl class="kv" style="margin:.5rem 0 .8rem"><dt>Address</dt><dd>rafael@ortizreno.example</dd><dt>Kind</dt><dd>person <span class="small muted">(your ruling)</span></dd><dt>Labels</dt><dd>contractor</dd><dt>Topics</dt><dd>Kitchen remodel</dd><dt>Since</dt><dd>Aug 12</dd></dl>
  <h3>Your note</h3><p class="small">Prefers a call for anything over $500. Cell in his signature.</p>
  <a class="btn" href="subject.html">Open Rafael’s page</a>
</aside>'''
page('people.html', 'People', 'A directory of people with open loops in both directions, separate from organisations and systems.', 'in:contacts kind:person', 'People', main, aside)
