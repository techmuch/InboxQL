from chrome import *

AV = lambda s: f'<span class="avatar" aria-hidden="true">{s}</span>'


def row(lead, who, line, meta='', cls='', chips=''):
    c = f'row {cls}'.strip()
    return f'<li class="{c}">{lead}<div><span class="who">{who}</span> {chips}<div class="line">{line}</div></div><span class="meta">{meta}</span></li>'


def when(s):
    return f'<span class="meta" style="text-align:left;min-width:3.5rem">{s}</span>'


# =================================================================== Kitchen remodel (topic)
main = f'''
  <p class="crumbs"><a href="today.html">Today</a> › Subjects</p>
  <div class="page-head"><div class="grow">
    <h1 id="page-title">Kitchen remodel</h1>
    <p class="muted" style="margin:0"><span class="chip">topic</span> 3 people · 52 messages · 6 files · 5 cards · since Aug 12</p>
  </div><button class="btn" aria-pressed="true">Pinned <kbd>p</kbd></button></div>
  <p>{C(1)} <strong>Next:</strong> choose a hardware finish by Monday so cabinets can be delivered Nov 2. Rafael still owes you the island quote. The rough-in inspection is due Nov 17.</p>

  <section class="section" aria-labelledby="h-k-loops">
    <header><h2 id="h-k-loops" class="grow">Open loops</h2></header>
    <ul class="list card">
      {row('<span class="chip warn">on you</span>', 'Cabinet hardware finish', 'Rafael: “Can you pick brushed brass or matte black by Monday…” · Oct 29', '<a class="btn primary" href="compose.html">Reply</a>', 'selected')}
      {row('<span class="chip">on Rafael</span>', 'Revised quote with the island', 'You asked Oct 26 · no answer in 5 days', '<button class="btn">Nudge</button>')}
      {row('<span class="chip">on Jordan</span>', 'Pick-up of old cabinets', 'You asked Jordan Lee on Oct 24 whether his truck is free on the 7th', '<button class="btn">Nudge</button>')}
    </ul>
  </section>

  <div style="display:grid;gap:1rem;grid-template-columns:repeat(auto-fit,minmax(16rem,1fr))">
  <section class="section card" style="padding:.8rem 1rem;margin:0" aria-labelledby="h-k-money">
    <h2 id="h-k-money">Money {C(2)}</h2>
    <p style="margin:.3rem 0">Spent <strong>$5,412.20</strong> of a <strong>$18,450</strong> quote.</p>
    <div class="progress" aria-hidden="true"><i style="width:29%"></i></div>
    <ul class="small" style="padding-left:1.1rem;margin:.5rem 0 0"><li>Deposit, Ortiz Renovations — $5,000.00, Oct 14</li><li>Hardware Depot (2 orders) — $412.20</li></ul>
    <p class="small muted" style="margin:.3rem 0 0">From receipts the agent extracted · <a href="query.html">Open as a query</a></p>
  </section>
  <section class="section card" style="padding:.8rem 1rem;margin:0" aria-labelledby="h-k-dates">
    <h2 id="h-k-dates">Dates</h2>
    <ul class="items tight" style="margin-top:.4rem">
      <li><span class="t">Mon 2</span><span class="k">Event</span><span>Cabinet delivery <span class="chip agent">proposed</span></span></li>
      <li><span class="t">Wed 4</span><span class="k">Delivery</span><span>Hardware Depot #88231</span></li>
      <li><span class="t">Tue 17</span><span class="k">Due</span><span>Rough-in inspection</span></li>
    </ul>
    <p class="small muted" style="margin:.4rem 0 0"><a href="ahead.html">See in Ahead</a></p>
  </section>
  <section class="section card" style="padding:.8rem 1rem;margin:0" aria-labelledby="h-k-cards">
    <h2 id="h-k-cards">Cards</h2>
    <ul class="small" style="padding-left:1.1rem;margin:.4rem 0 0"><li>Order cabinet hardware — <em>To do</em>, due Nov 2</li><li>Book the inspection — <em>Doing</em></li><li>Choose countertop — <em>Done</em></li><li><span class="chip agent">proposed</span> Arrange old-cabinet pick-up</li></ul>
    <p class="small muted" style="margin:.4rem 0 0"><a href="board.html">Open on the Board</a></p>
  </section>
  </div>

  <section class="section" aria-labelledby="h-k-people">
    <header><h2 id="h-k-people" class="grow">People</h2></header>
    <ul class="list card">
      {row(AV('RO'), '<a href="subject.html">Rafael Ortiz</a>', 'Contractor · 38 messages · usually replies within a day', '')}
      {row(AV('JL'), 'Jordan Lee', 'Copied on 9 · helping with the pick-up', '')}
      {row(AV('HD'), 'Hardware Depot', 'System · 5 order emails', '', 'read')}
    </ul>
  </section>

  <section class="section" aria-labelledby="h-k-stream">
    <header><h2 id="h-k-stream" class="grow">Everything in this topic</h2></header>
    <div role="group" aria-label="Show" class="filters"><button class="chip" aria-pressed="true">All</button><button class="chip" aria-pressed="false">Mail</button><button class="chip" aria-pressed="false">Files</button><button class="chip" aria-pressed="false">Cards</button><button class="chip" aria-pressed="false">Money</button></div>
    <ol class="list card" aria-label="Kitchen remodel, newest first">
      {row(when('Oct 31'), '✉ Hardware Depot: order #88231 shipped', 'Arrives Wednesday', '', 'read')}
      {row(when('Oct 29'), '✉ Rafael Ortiz: Cabinet hardware', '“Can you pick brushed brass or matte black by Monday…”', '<a class="btn" href="conversation.html">Open</a>', '', '<span class="chip warn">owed</span>')}
      {row(when('Oct 27'), '📎 Quote-v2.pdf', 'Revised quote · $18,450 extracted', '<a class="btn" href="files.html">Preview</a>')}
    </ol>
  </section>
'''
main += notes('n-topic',
    'A topic is a subject that is not a person: everything about one piece of your life or work, across people. It is the page for a project like a remodel, where the questions are <em>what is next, what is it costing, and who is holding things up</em>.',
    ['<strong>Next</strong> is one sentence about what moves the project forward, built from open loops and dates — not from a summary of the mail.',
     'Money is added up from receipts in the topic and compared with the quote the agent found. Each figure links to its message.'],
    ['Today a topic is a tag on threads (<code>topic:</code>) with no page of its own.',
     'Topics come from the agent’s <code>topics</code> element; you can add or remove any conversation, and your choice wins.'],
    [('<kbd>p</kbd>', 'Pin or unpin as a subject'), ('<kbd>1</kbd>–<kbd>5</kbd>', 'All / Mail / Files / Cards / Money'), ('<kbd>t</kbd>', 'Add the open conversation to a topic')],
    'Sections are headings in order: open loops, money, dates, cards, people, everything. The money progress bar is decorative; the sentence above it says the figures.',
    '<a href="workflows.html#w2">W2 Work one subject</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-k"><h2 id="ev-k">How this topic is made</h2>
  <p class="small"><code>topic:kitchen-remodel</code></p>
  <ul class="small" style="padding-left:1.1rem"><li>49 conversations tagged by the agent</li><li>3 you added by hand</li><li>1 you removed (“Hardware Depot newsletter”) — the agent won’t add it again</li></ul>
  <h3>Usually in this topic</h3><p class="small">Mail from <code>*@ortizreno.example</code>, Hardware Depot orders, and anything mentioning cabinets, countertop or inspection.</p>
  <button class="btn" style="margin-top:.5rem">Rename or merge topic</button>
</aside>'''
page('topic.html', 'Kitchen remodel', 'A topic subject: next step, open loops, money against the quote, dates, cards and people.', 'topic:kitchen-remodel', 'Kitchen remodel', main, aside)

# =================================================================== Q4 report (document-centred subject)
main = f'''
  <p class="crumbs"><a href="today.html">Today</a> › Subjects</p>
  <div class="page-head"><div class="grow">
    <h1 id="page-title">Q4 report</h1>
    <p class="muted" style="margin:0"><span class="chip">topic</span> <span class="chip accent">★ important · due Tue, Nov 10 · in 10 days</span> Priya Raman, Sam Okafor · 31 messages</p>
  </div><button class="btn" aria-pressed="true">Pinned <kbd>p</kbd></button></div>
  <p>{C(1)} <strong>Next:</strong> your comments on section 3 before Tuesday’s call. Sam is waiting for a link to the model, which was too large to email.</p>

  <section class="section" aria-labelledby="h-q-ver">
    <header><h2 id="h-q-ver" class="grow">The document {C(2)}</h2></header>
    <ol class="list card" aria-label="Versions of Q4-report.docx, newest first">
      {row(when('Oct 30'), 'Q4-report-v3.docx <span class="chip accent">latest</span>', 'From Priya Raman · “section 3 rewritten, new charts” · 2.1 MB', '<button class="btn">Open</button> <button class="btn quiet">Compare with v2</button>', 'selected')}
      {row(when('Oct 21'), 'Q4-report-v2.docx', 'From you · your edits on sections 1–2', '<button class="btn quiet">Open</button>', 'read')}
      {row(when('Oct 9'), 'Q4-report-v1.docx', 'From Priya Raman · first draft', '<button class="btn quiet">Open</button>', 'read')}
    </ol>
    <p class="small muted" style="margin-top:.35rem">Versions are the same file name sent at different times; each is one file however many people it went to.</p>
  </section>

  <section class="section" aria-labelledby="h-q-loops">
    <header><h2 id="h-q-loops" class="grow">Open loops</h2></header>
    <ul class="list card">
      {row('<span class="chip warn">on you</span>', 'Comments on section 3', 'Priya: “Could you look at section 3 before Tuesday’s call?” · by Tue 2 PM', '<a class="btn primary" href="compose.html">Reply</a>')}
      {row('<span class="chip warn">on you</span>', 'Share the model', 'Sam: “The model is too big to email — can you share a link?” · your send failed (31 MB)', '<a class="btn" href="outbox.html">See the failed send</a>')}
    </ul>
  </section>

  <section class="section" aria-labelledby="h-q-plan">
    <header><h2 id="h-q-plan" class="grow">Towards Nov 10</h2></header>
    <ul class="items card" style="padding:.6rem .85rem">
      <li><span class="t">Tue 3</span><span class="k">Event</span><span>Q4 report review call, 2:00 PM</span></li>
      <li><span class="t">Fri 6</span><span class="k">Due</span><span>Send Q4 numbers to Sam <span class="chip">card</span></span></li>
      <li><span class="t">Tue 10</span><span class="k">Due</span><span class="imp">Q4 report to the board</span></li>
    </ul>
  </section>
'''
main += notes('n-q4',
    'Some subjects are about a document more than a conversation. This page leads with the versions of the file, then what is owed and the dates up to the deadline. It is the same subject page as Kitchen remodel, with the document section shown because the topic has a file sent more than twice under one name.',
    ['The next step, from the open loops and the important date.',
     '<strong>Versions</strong>: one row per distinct file, newest first, with who sent it and what they said about it. “Compare” is <span class="planned">planned</span> and only for formats InboxQL can read.'],
    ['Versions of a file are spread across messages today; Files lists them but not as a sequence.'],
    [('<kbd>v</kbd>', 'Open the latest version'), ('<kbd>p</kbd>', 'Pin or unpin'), ('<kbd>i</kbd>', 'Mark the deadline important')],
    'The versions list is an ordered list labelled “Versions of Q4-report.docx, newest first”; “latest” is spoken as text.',
    '<a href="workflows.html#w2">W2 Work one subject</a> · <a href="workflows.html#w10">W10 Mark a date as important</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-q"><h2 id="ev-q">Q4-report-v3.docx</h2>
  <dl class="kv" style="margin:.5rem 0 .8rem"><dt>From</dt><dd>Priya Raman, Oct 30</dd><dt>Size</dt><dd>2.1 MB · 18 pages</dd><dt>Text</dt><dd>read · searchable</dd><dt>Also sent to</dt><dd>Sam Okafor</dd></dl>
  <h3>Changed since v2</h3><p class="small">Priya’s message: “Section 3 rewritten around the new regional numbers; charts on pages 9–11 are new.”</p>
  <h3>Mentions you</h3><blockquote class="quote">Alex — can you sanity-check the West region figures in 3.2?<cite>page 10, a comment in the document</cite></blockquote>
</aside>'''
page('q4.html', 'Q4 report', 'A document-centred subject: versions of the file, open loops, and the dates up to its deadline.', 'topic:q4-report', 'Q4 report', main, aside)
