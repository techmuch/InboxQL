from chrome import *

body = head('Rafael Ortiz', 'A subject page: everything about one person in one stream.')
body += banner('subject.html')
body += appbar('anyone:rafael@ortizreno.example')
body += '<div class="shell">\n' + nav('Rafael Ortiz')
body += '''
<main id="main" class="main" aria-labelledby="page-title">
  <p class="crumbs"><a href="today.html">← Today</a> <span aria-hidden="true">›</span> Replies you owe</p>
  <div class="page-head">
    <span class="avatar" aria-hidden="true" style="width:3rem;height:3rem;font-size:1rem">RO</span>
    <div class="grow">
      <h1 id="page-title">Rafael Ortiz</h1>
      <p class="muted" style="margin:0">Ortiz Renovations · rafael@ortizreno.example · <span class="chip">person</span> <span class="chip">contractor</span> <span class="chip">Kitchen remodel</span></p>
    </div>
    <a class="btn primary" href="compose.html">Write to Rafael</a>
    <button class="btn" aria-pressed="true">Pinned <kbd>p</kbd></button>
  </div>

  <section class="section" aria-labelledby="h-loops"><span class="callout" aria-hidden="true">1</span>
    <header><h2 id="h-loops" class="grow">Open loops</h2></header>
    <ul class="list card">
      <li class="row selected"><span class="chip warn">on you</span>
        <div><span class="who">Cabinet hardware finish</span><div class="line">“Can you pick brushed brass or matte black by Monday so I can order?” · Oct 29</div></div>
        <span class="meta"><a class="btn primary" href="compose.html">Reply</a> <a class="btn" href="conversation.html">Open</a></span></li>
      <li class="row"><span class="chip">on Rafael</span>
        <div><span class="who">Revised quote with the island</span><div class="line">You asked on Oct 26: “Could you price the island with the quartz top?” · no answer yet</div></div>
        <span class="meta"><button class="btn">Nudge</button></span></li>
    </ul>
  </section>

  <section class="section" aria-labelledby="h-stream">
    <header><h2 id="h-stream" class="grow">Everything with Rafael</h2>
      <span class="small muted">38 messages · 4 files · 3 cards · 2 events · since Aug 12</span></header>
    <div role="group" aria-label="Show" style="display:flex;flex-wrap:wrap;gap:.35rem;margin-bottom:.6rem"><span class="callout" aria-hidden="true">2</span>
      <button class="chip accent" aria-pressed="true">All</button>
      <button class="chip" aria-pressed="false">Mail</button>
      <button class="chip" aria-pressed="false">Files</button>
      <button class="chip" aria-pressed="false">Cards</button>
      <button class="chip" aria-pressed="false">Events</button>
      <button class="chip" aria-pressed="false">Money</button>
    </div>
    <ol class="list card" aria-label="Everything with Rafael, newest first">
      <li class="row"><span class="meta" style="text-align:left">Oct 29</span>
        <div><span class="who">✉ Cabinet hardware</span> <span class="chip warn">owed</span>
          <div class="line">Rafael: “Can you pick brushed brass or matte black by Monday…” · 2 attachments</div></div>
        <a class="btn" href="conversation.html">Open</a></li>
      <li class="row"><span class="meta" style="text-align:left">Oct 29</span>
        <div><span class="who">📅 Cabinet delivery window</span> <span class="chip agent">agent proposed</span>
          <div class="line">Mon Nov 2, 10:30–12:30 · from the message above · waiting for your approval</div></div>
        <span class="meta"><button class="btn">Approve</button> <button class="btn quiet">Reject</button></span></li>
      <li class="row"><span class="meta" style="text-align:left">Oct 27</span>
        <div><span class="who">📎 Quote-v2.pdf</span> <span class="chip agent">$18,450 extracted</span>
          <div class="line">Revised quote, 4 pages · also sent to Jordan Lee</div></div>
        <button class="btn">Preview</button></li>
      <li class="row"><span class="meta" style="text-align:left">Oct 26</span>
        <div><span class="who">▢ Order cabinet hardware</span> <span class="chip">card · To do</span>
          <div class="line">Due Nov 2 · raised by the agent from “Cabinet hardware” · you accepted it</div></div>
        <button class="btn">Open card</button></li>
      <li class="row read"><span class="meta" style="text-align:left">Oct 26</span>
        <div><span class="who">✉ You: Island pricing</span>
          <div class="line">“Could you price the island with the quartz top?”</div></div>
        <a class="btn" href="conversation.html">Open</a></li>
      <li class="row read"><span class="meta" style="text-align:left">Oct 14</span>
        <div><span class="who">🧾 Deposit received — Ortiz Renovations</span> <span class="chip agent">receipt · $5,000.00</span>
          <div class="line">Paid by transfer from Northwind ••4471</div></div>
        <a class="btn" href="conversation.html">Open</a></li>
    </ol>
    <p class="small muted" style="margin-top:.4rem">Showing 6 of 47. <a href="query.html">Open as a list</a> to sort, select and act on many.</p>
  </section>

  <section class="notes" aria-labelledby="notes-subject">
    <h2 id="notes-subject">About this screen</h2>
    <p><strong>Purpose.</strong> Objective 1, <em>one subject at a time</em>. A person, a topic, a label or any saved query opens as a page like this one. Mail, files, cards, events and money that today live in Desk, Files, Board, Calendar and the Dashboard appear here in one stream, filtered to this subject.</p>
    <h3>Callouts</h3>
    <ol>
      <li><span class="callout">1</span> <strong>Open loops</strong> first: what you owe and what you are waiting for, from the <code>waiting</code> element, each with the line that makes it a loop. The person's page is where you decide whether to reply, nudge or let it go.</li>
      <li><span class="callout">2</span> One stream, filterable by kind. Filters are toggle buttons (<code>aria-pressed</code>); each adds a term to the query in the app bar, so the page is always the query you can see.</li>
    </ol>
    <h3>Leaving and coming back</h3>
    <p>The crumb says how you got here. <kbd>Esc</kbd> or <kbd>u</kbd> goes back to the exact place you left — the same row in Today, with focus on it. A subject is never a tab you must close.</p>
    <h3>Topic and query subjects</h3>
    <p>A topic (“Kitchen remodel”) uses the same page: the header lists the people in it instead of an address, and open loops cover every conversation tagged with the topic. A saved query subject shows its query as the header.</p>
    <h3>Keyboard</h3>
    <table class="doc"><tr><th>Key</th><th>Does</th></tr>
      <tr><td><kbd>Esc</kbd> / <kbd>u</kbd></td><td>Back to where you came from</td></tr>
      <tr><td><kbd>1</kbd>–<kbd>6</kbd></td><td>Show All, Mail, Files, Cards, Events, Money</td></tr>
      <tr><td><kbd>p</kbd></td><td>Pin or unpin this subject in the navigation</td></tr>
      <tr><td><kbd>w</kbd></td><td>Write to this person</td></tr>
    </table>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w2">W2 Work one subject</a> · <a href="workflows.html#w3">W3 Reply with the agent's draft</a></p>
  </section>
</main>
<aside class="evidence" aria-labelledby="ev-subject">
  <h2 id="ev-subject">What we know about Rafael</h2>
  <dl class="kv" style="margin:.5rem 0 1rem">
    <dt>Kind</dt><dd>Person <span class="small muted">(you ruled, Sep 3)</span></dd>
    <dt>First</dt><dd>Aug 12, 2026</dd>
    <dt>He replies</dt><dd>in about 4 hours</dd>
    <dt>You reply</dt><dd>in about 1 day</dd>
    <dt>Writes</dt><dd>weekdays, 7–9 am</dd>
  </dl>
  <h3>Money <span class="small muted">· extracted by the agent</span></h3>
  <dl class="kv small" style="margin:.4rem 0 .4rem">
    <dt>Quoted</dt><dd>$18,450.00</dd>
    <dt>Paid</dt><dd>$5,000.00 deposit</dd>
    <dt>Balance</dt><dd>$13,450.00 <span class="muted">(computed)</span></dd>
  </dl>
  <blockquote class="quote">“Total for the scope above: $18,450.00 including installation.”<cite>Quote-v2.pdf, page 4 · confidence ranks high</cite></blockquote>
  <h3 style="margin-top:1rem">Files</h3>
  <ul class="small" style="padding-left:1.1rem">
    <li>Quote-v2.pdf · Oct 27</li><li>Cabinet-layout.pdf · Oct 20</li><li>Hardware-options.jpg · Oct 29</li><li>Quote-v1.pdf · Sep 30</li>
  </ul>
  <h3 style="margin-top:1rem">Your note</h3>
  <p class="small">Prefers phone for anything urgent: 555-0142. Ask about the warranty on the hinges.</p>
  <button class="btn quiet small">Edit note</button>
</aside>
</div>
'''
body += foot()
write('subject.html', body)
