from chrome import *

body = head('Today', 'The home screen: what needs a human today, before what arrived.')
body += banner('today.html')
body += appbar('needs:me')
body += '<div class="shell">\n' + nav('Today')
body += '''
<main id="main" class="main" aria-labelledby="page-title">
  <div class="page-head">
    <div class="grow">
      <p class="crumbs">Saturday, October 31</p>
      <h1 id="page-title">Today</h1>
      <p class="muted" style="margin-top:.3rem"><span class="callout" aria-hidden="true">1</span>
        You owe <strong>4 replies</strong>, there are <strong>9 things to review</strong> and
        <strong>1 message waiting to send</strong>. 64 conversations arrived since yesterday; the agent has read 62.</p>
    </div>
    <a class="btn" href="review.html">Start reviewing <kbd>g r</kbd></a>
  </div>

  <section class="section" id="replies" aria-labelledby="h-replies">
    <header><h2 id="h-replies" class="grow">Replies you owe <span class="muted">· 4</span></h2>
      <a class="small" href="replies.html">Show as a list</a></header>
    <ul class="list card" role="listbox" aria-label="Replies you owe, newest first" aria-describedby="replies-help">
      <li class="row" role="option" aria-selected="true" tabindex="0"
          aria-label="Rafael Ortiz, Kitchen remodel: cabinet hardware, asks you to choose a finish by Monday. 2 days. Agent: waiting on you.">
        <span class="avatar" aria-hidden="true">RO</span>
        <div>
          <span class="who">Rafael Ortiz</span> <span class="chip">Kitchen remodel</span>
          <div class="line">Cabinet hardware — “Can you pick brushed brass or matte black by Monday so I can order?”</div>
        </div>
        <span class="meta">2 days<br><span class="chip agent" title="The waiting element judged this conversation">agent: on you</span></span>
        <div class="acts" aria-hidden="true">
          <span class="btn primary">Reply <kbd>r</kbd></span>
          <span class="btn">Open <kbd>↵</kbd></span>
          <span class="btn">Everything with Rafael <kbd>f</kbd></span>
          <span class="btn quiet">Not waiting on me <kbd>n</kbd></span>
        </div>
      </li>
      <li class="row" role="option" aria-selected="false" tabindex="-1"
          aria-label="Priya Raman, Q4 report draft, asks for comments on section 3. 1 day. Agent: waiting on you.">
        <span class="avatar" aria-hidden="true">PR</span>
        <div><span class="who">Priya Raman</span> <span class="chip">Q4 report</span>
          <div class="line">Draft for review — “Could you look at section 3 before Tuesday’s call?”</div></div>
        <span class="meta">1 day<br><span class="chip agent">agent: on you</span></span>
      </li>
      <li class="row" role="option" aria-selected="false" tabindex="-1"
          aria-label="Dana Whitfield, Saturday carpool, asks whether you can drive. 5 hours. Agent: waiting on you.">
        <span class="avatar" aria-hidden="true">DW</span>
        <div><span class="who">Dana Whitfield</span>
          <div class="line">Saturday carpool — “Are you able to take the 8:15 run this week?”</div></div>
        <span class="meta">5 h<br><span class="chip agent">agent: on you</span></span>
      </li>
      <li class="row" role="option" aria-selected="false" tabindex="-1"
          aria-label="Northwind Bank, Verify your new payee, needs an action, not a reply. 3 hours. Agent: waiting on you.">
        <span class="avatar" aria-hidden="true">NB</span>
        <div><span class="who">Northwind Bank</span> <span class="chip">system</span>
          <div class="line">Confirm the new payee “Ortiz Renovations” before the first transfer</div></div>
        <span class="meta">3 h<br><span class="chip agent">agent: on you</span></span>
      </li>
    </ul>
    <p id="replies-help" class="small muted" style="margin-top:.35rem">↑↓ to move · <kbd>r</kbd> reply · <kbd>↵</kbd> open · <kbd>f</kbd> everything with this person · <kbd>n</kbd> not waiting on me (teaches the agent)</p>
  </section>

  <section class="section" aria-labelledby="h-review">
    <header><h2 id="h-review" class="grow">To review <span class="muted">· 9</span></h2>
      <a class="small" href="review.html">Review all</a></header>
    <div class="card" style="padding:.75rem 1rem; display:grid; gap:.5rem; grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr));">
      <a class="btn" href="review.html#send" style="justify-content:space-between"><span>Message to send</span><span class="chip warn">1</span></a>
      <a class="btn" href="review.html#events" style="justify-content:space-between"><span>Calendar proposals</span><span class="chip">3</span></a>
      <a class="btn" href="review.html#cards" style="justify-content:space-between"><span>Cards proposed</span><span class="chip">2</span></a>
      <a class="btn" href="review.html#checks" style="justify-content:space-between"><span>Agent answers to check</span><span class="chip">3</span></a>
    </div>
  </section>

  <section class="section" aria-labelledby="h-new">
    <header><h2 id="h-new" class="grow">Arrived since yesterday, by subject <span class="muted">· 64</span></h2>
      <a class="small" href="inbox.html">Open the inbox</a></header>
    <p class="small muted">Grouped by what the agent concluded, so a whole group can be dealt with at once. <span class="callout" aria-hidden="true">2</span></p>
    <ul class="list card">
      <li class="row"><span class="avatar" aria-hidden="true">🧾</span>
        <div><span class="who">Receipts</span> <span class="muted">· 5</span>
          <div class="line">Hardware Depot $212.40 · Lumen Air $164.00 · Bookshelf $18.99 · 2 more — <span class="chip agent">agent extracted $412.20</span></div></div>
        <span class="meta"><a class="btn" href="query.html">Open</a> <button class="btn">Archive 5</button></span></li>
      <li class="row"><span class="avatar" aria-hidden="true">KR</span>
        <div><span class="who">Kitchen remodel</span> <span class="muted">· 3</span>
          <div class="line">Rafael Ortiz (2), Hardware Depot order update — 1 reply owed, 1 card proposed</div></div>
        <span class="meta"><a class="btn" href="topic.html">Open subject</a></span></li>
      <li class="row"><span class="avatar" aria-hidden="true">✉</span>
        <div><span class="who">Newsletters and promotions</span> <span class="muted">· 41</span>
          <div class="line">The agent judged all 41 <em>low</em> or <em>ignorable</em>; none are from people</div></div>
        <span class="meta"><a class="btn" href="query.html#low">Look first</a> <button class="btn">Archive 41</button></span></li>
      <li class="row"><span class="avatar" aria-hidden="true">PR</span>
        <div><span class="who">Q4 report</span> <span class="muted">· 4</span>
          <div class="line">Priya Raman, Sam Okafor — 1 reply owed, a file revised (Q4-report-v3.docx)</div></div>
        <span class="meta"><a class="btn" href="q4.html">Open subject</a></span></li>
      <li class="row"><span class="avatar" aria-hidden="true">?</span>
        <div><span class="who">Not read by the agent yet</span> <span class="muted">· 2</span>
          <div class="line">Arrived in the last 10 minutes; the next agent pass will read them</div></div>
        <span class="meta"><a class="btn" href="query.html#unread-by-agent">Read them yourself</a></span></li>
    </ul>
    <figure style="margin:.5rem 0 0"><figcaption class="small muted">Example — After pressing Archive on a group:</figcaption><div class="toast demo">Archived 41 newsletters and promotions. <button type="button">Undo</button> <kbd>z</kbd></div></figure>
  </section>

  <section class="section" aria-labelledby="h-ahead">
    <header><h2 id="h-ahead" class="grow">Ahead <span class="callout" aria-hidden="true">3</span></h2>
      <a class="small" href="ahead.html">Two weeks <kbd>g a</kbd></a> <a class="small" href="ahead-months.html">Two months</a></header>
    <ul class="countdowns" aria-label="Important dates, soonest first" style="margin-bottom:.6rem">
      <li class="cd compact soon" aria-label="Q4 report to the board, Tue, Nov 10, in 10 days. Within its lead time."><span class="n" aria-hidden="true">10 <small>days</small></span><span class="what" aria-hidden="true">Q4 report to the board</span><span class="when" aria-hidden="true">Tue, Nov 10</span></li>
      <li class="cd compact" aria-label="Flight to Denver, Wed, Nov 25, in 25 days"><span class="n" aria-hidden="true">25 <small>days</small></span><span class="what" aria-hidden="true">Flight to Denver</span><span class="when" aria-hidden="true">Wed, Nov 25</span></li>
      <li class="cd compact" aria-label="Start renewing your passport, Thu, Dec 3, in 33 days"><span class="n" aria-hidden="true">33 <small>days</small></span><span class="what" aria-hidden="true">Start passport renewal</span><span class="when" aria-hidden="true">Thu, Dec 3 · expires Jan 14</span></li>
      <li class="cd compact" aria-label="Anniversary, Sat, Dec 12, in 42 days"><span class="n" aria-hidden="true">42 <small>days</small></span><span class="what" aria-hidden="true">Anniversary</span><span class="when" aria-hidden="true">Sat, Dec 12</span></li>
    </ul>
    <dl class="kv card" style="padding:.6rem .85rem;margin-bottom:.6rem">
      <dt>This week</dt><dd>2 days left · Trunk-or-treat tonight; clocks go back on Sunday</dd>
      <dt>Next week</dt><dd><strong>Busiest in a month</strong> — 14 h booked, 2 replies due, 1 card due, <span style="color:var(--warn)">an overlap on Thursday</span></dd>
      <dt>November</dt><dd>Busy first half; away Nov 25 – 29</dd>
      <dt>December</dt><dd>2 important dates; passport lead time starts Dec 3</dd>
    </dl>
    <h3 class="small muted" style="margin:.2rem 0 .3rem">Next 48 hours</h3>
    <ul class="list card">
      <li class="row"><span class="meta" style="text-align:left;min-width:5.5rem">Sat 6:00 PM</span><div><span class="who">Trunk-or-treat</span><div class="line">Church parking lot · accepted</div></div><span></span></li>
      <li class="row"><span class="meta" style="text-align:left;min-width:5.5rem">Sun 9:00</span><div><span class="who">Ward council</span><div class="line">Church building · accepted</div></div><span></span></li>
      <li class="row"><span class="meta" style="text-align:left;min-width:5.5rem">Mon 10:30</span><div><span class="who">Cabinet delivery window</span> <span class="chip agent">agent proposed</span><div class="line">From Rafael’s message of Oct 29 · not on your calendar until you approve</div></div><a class="btn" href="review.html#events">Review</a></li>
    </ul>
  </section>

  <section class="notes" aria-labelledby="notes-today">
    <h2 id="notes-today">About this screen</h2>
    <p><strong>Purpose.</strong> The home screen answers <em>what needs me?</em> before <em>what arrived?</em> (objective 2).
      It is built entirely from queries — <code>awaiting:me</code>, <code>is:proposed</code>, <code>in:drafts status:queued</code>,
      <code>in:events next:2d</code> — so every section can be opened as a list, saved, or handed to an agent.</p>
    <h3>Callouts</h3>
    <ol>
      <li><span class="callout">1</span> One sentence of state, in words, at the top. A screen reader hears it first after the heading. It also says how much the agent has <em>not</em> read, so an empty section is never mistaken for “nothing there”.</li>
      <li><span class="callout">2</span> New mail grouped by subject, not listed by time. A group can be archived whole — the toast offers Undo for 10 seconds and the action is in Agent activity afterwards.</li>
      <li><span class="callout">3</span> <strong>Ahead</strong>, last on the page because it is rarely the first thing to act on: the dates you marked important as countdowns, one line each for this week, next week, this month and next month, then the next 48 hours in detail. Each line opens <a href="ahead.html">Ahead</a> at that range.</li>
    </ol>
    <h3>What changed from today's app</h3>
    <ul>
      <li>The inbox is no longer home; it is one section, one click away.</li>
      <li>Owed replies come from the <code>waiting</code> element; “Not waiting on me” records your ruling, which outranks the agent.</li>
      <li>Approvals that used to live in the Calendar sidebar, the Board's Proposed list and Intelligence → Waiting for approval are counted in one place.</li>
    </ul>
    <h3>Keyboard</h3>
    <table class="doc"><tr><th>Key</th><th>Does</th></tr>
      <tr><td><kbd>g t</kbd></td><td>Go to Today (from anywhere)</td></tr>
      <tr><td><kbd>↑</kbd> <kbd>↓</kbd></td><td>Move within a section; <kbd>Tab</kbd> moves between sections</td></tr>
      <tr><td><kbd>r</kbd> <kbd>↵</kbd> <kbd>f</kbd> <kbd>n</kbd></td><td>Reply · open · everything with this person · not waiting on me</td></tr>
      <tr><td><kbd>g r</kbd></td><td>Start reviewing</td></tr>
    </table>
    <h3>Screen reader</h3>
    <p>Landmarks: banner (app bar with the query), navigation “Places”, main “Today”, and the notes. Each section is a heading level 2; each owed reply is one listbox option with a single spoken name: <em>“Rafael Ortiz, Kitchen remodel: cabinet hardware, asks you to choose a finish by Monday. 2 days. Agent: waiting on you.”</em> Row buttons are mouse affordances; each has a key, announced in the list's description.</p>
    <h3>Workflows that start here</h3>
    <p><a href="workflows.html#w1">W1 Morning pass</a> · <a href="workflows.html#w2">W2 Work one subject</a> · <a href="workflows.html#w4">W4 Clear a group</a></p>
  </section>
</main>
'''
body += '''
<aside class="evidence" aria-labelledby="ev-today">
  <h2 id="ev-today">How this was made</h2>
  <p class="small muted">Every section is a query. Open one to see, edit or save it.</p>
  <dl class="kv" style="margin:.5rem 0 1rem">
    <dt>Replies</dt><dd><code>awaiting:me</code></dd>
    <dt>Review</dt><dd><code>is:proposed OR needs:check</code></dd>
    <dt>To send</dt><dd><code>in:drafts status:queued</code></dd>
    <dt>New</dt><dd><code>after:1d | count by subject</code></dd>
    <dt>Ahead</dt><dd><code>next:2mo OR is:important</code></dd>
    <dt>48 hours</dt><dd><code>in:events next:2d</code></dd>
  </dl>
  <h3>The agent since yesterday</h3>
  <ul class="small" style="padding-left:1.1rem">
    <li>Read <strong>62</strong> of 64 conversations (2 pending)</li>
    <li>Judged 4 as waiting on you, 7 on others</li>
    <li>Extracted 5 receipts and 1 travel booking</li>
    <li>Proposed 3 events and 2 cards</li>
    <li>Wrote 1 reply draft — waiting for you to send</li>
  </ul>
  <a class="small" href="activity.html">Everything the agent did</a>
  <h3 style="margin-top:1rem">How often it is right</h3>
  <p class="small">On your rulings this month: <strong>waiting</strong> 41 of 44 right, <strong>importance</strong> 37 of 44. <a href="review.html#checks">3 checks due</a>.</p>
</aside>
</div>

'''
body += foot()
write('today.html', body)
