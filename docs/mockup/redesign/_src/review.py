from chrome import *

body = head('Review', 'One queue for everything that waits on a person: mail to send, proposals, and agent answers to check.')
body += banner('review.html')
body += appbar('is:proposed OR needs:check OR (in:drafts status:queued)')
body += '<div class="shell">\n' + nav('To review')
body += '''
<main id="main" class="main" aria-labelledby="page-title">
  <div class="page-head">
    <div class="grow">
      <p class="crumbs"><a href="today.html">← Today</a></p>
      <h1 id="page-title">To review <span class="muted">· 9</span></h1>
      <p class="muted" style="margin:0">Everything an agent or a rule proposed that waits for you. Nothing here has happened yet.</p>
    </div>
    <button class="btn">Select all calendar proposals</button>
  </div>

  <div role="listbox" aria-label="Waiting for review" aria-multiselectable="true" aria-describedby="review-keys" class="card">
    <h2 id="send" class="small muted" style="padding:.6rem .85rem 0" role="presentation">TO SEND · 1</h2>
    <div class="row selected" role="option" aria-selected="true" tabindex="0"
         aria-label="Message to send: reply to Rafael Ortiz, Cabinet hardware. Written by the agent. Waiting for you to send.">
      <span class="chip warn">send</span>
      <div><span class="who">Re: Cabinet hardware</span> <span class="chip agent">written by agent</span>
        <div class="line">To Rafael Ortiz · “Thanks Rafael — let's go with [your choice]. Monday delivery works…” · <strong>1 blank to fill</strong></div></div>
      <span class="meta">queued 7 min ago</span>
    </div>

    <h2 id="events" class="small muted" style="padding:.6rem .85rem 0" role="presentation">CALENDAR · 3</h2>
    <div class="row" role="option" aria-selected="false" tabindex="-1" aria-label="Calendar proposal: Cabinet delivery, Monday November 2, 10:30 to 12:30. From Rafael Ortiz's message.">
      <span class="chip">event</span>
      <div><span class="who">Cabinet delivery</span><div class="line">Mon Nov 2, 10:30–12:30 · from Rafael's message of Oct 29</div></div>
      <span class="meta">agent</span></div>
    <div class="row" role="option" aria-selected="false" tabindex="-1" aria-label="Calendar proposal: Flight LA 482 to Austin, Thursday December 17.">
      <span class="chip">event</span>
      <div><span class="who">Flight LA 482 → Austin</span><div class="line">Thu Dec 17, 6:10–9:05 PM · Lumen Air booking ZK4R2P</div></div>
      <span class="meta">agent</span></div>
    <div class="row" role="option" aria-selected="false" tabindex="-1" aria-label="Calendar proposal: Parent teacher conference, Thursday November 12, 4 PM. Conflicts with Q4 review.">
      <span class="chip">event</span>
      <div><span class="who">Parent–teacher conference</span> <span class="chip bad">conflicts with Q4 review</span><div class="line">Thu Nov 12, 4:00–4:20 PM · from Lincoln Elementary</div></div>
      <span class="meta">agent</span></div>

    <h2 id="cards" class="small muted" style="padding:.6rem .85rem 0" role="presentation">CARDS · 2</h2>
    <div class="row" role="option" aria-selected="false" tabindex="-1" aria-label="Card proposal: Comment on section 3 of the Q4 report, due Tuesday.">
      <span class="chip">card</span>
      <div><span class="who">Comment on Q4 report §3</span><div class="line">Due Tue Nov 3 · from Priya Raman's request</div></div>
      <span class="meta">agent</span></div>
    <div class="row" role="option" aria-selected="false" tabindex="-1" aria-label="Card proposal: Confirm new payee at Northwind Bank.">
      <span class="chip">card</span>
      <div><span class="who">Confirm new payee at Northwind</span><div class="line">No due date · from Northwind Bank</div></div>
      <span class="meta">rule: bank alerts</span></div>

    <h2 id="checks" class="small muted" style="padding:.6rem .85rem 0" role="presentation">CHECK THE AGENT · 3</h2>
    <div class="row" role="option" aria-selected="false" tabindex="-1" aria-label="Check: is this conversation waiting on you? Saturday carpool, Dana Whitfield. The agent's answer is hidden until you answer.">
      <span class="chip accent">check</span>
      <div><span class="who">Is “Saturday carpool” waiting on you?</span><div class="line">Blind check — the agent's answer is shown after yours</div></div>
      <span class="meta">random draw</span></div>
    <div class="row" role="option" aria-selected="false" tabindex="-1" aria-label="Check: is this a receipt? Bookshelf order, 18 dollars 99.">
      <span class="chip accent">check</span>
      <div><span class="who">Receipt: Bookshelf $18.99?</span><div class="line">Low-confidence extraction</div></div>
      <span class="meta">likely wrong</span></div>
    <div class="row" role="option" aria-selected="false" tabindex="-1" aria-label="Check: importance of Ward newsletter.">
      <span class="chip accent">check</span>
      <div><span class="who">Importance of “Ward newsletter”</span><div class="line">Blind check</div></div>
      <span class="meta">random draw</span></div>
  </div>
  <p id="review-keys" class="small muted" style="margin-top:.4rem"><kbd>↑</kbd><kbd>↓</kbd> move · <kbd>x</kbd> select · <kbd>a</kbd> approve / send · <kbd>e</kbd> edit · <kbd>⌫</kbd> reject · <kbd>s</kbd> skip · <kbd>z</kbd> undo the last</p>

  <section class="notes" aria-labelledby="notes-review">
    <h2 id="notes-review">About this screen</h2>
    <p><strong>Purpose.</strong> Value 1, <em>the person decides</em>, made fast. Today these approvals live in four places — Intelligence → Waiting for approval, the Calendar sidebar, the Board's Proposed list, and Work loop checks. Here they are one queue, worked with the same keys.</p>
    <h3>Rules</h3>
    <ul>
      <li><strong>Send needs its own confirmation.</strong> <kbd>a</kbd> on a message shows the exact recipients and gives the usual 10 seconds to undo; selecting many items and pressing <kbd>a</kbd> never sends mail — messages are excluded from bulk approval.</li>
      <li><strong>Checks are blind.</strong> The agent's answer is hidden until you give yours, so your ruling measures the agent rather than agreeing with it.</li>
      <li><strong>Agents leave decisions blank.</strong> A draft that needs something only you know — a choice, a date, a yes — carries a marked blank, and Send stays off until it is filled. The agent writes; it does not decide for you.</li>
      <li><strong>Who proposed it</strong> is always shown: an agent (by name), a rule (by name), or an extractor.</li>
      <li><strong>Rejecting is kept</strong>, as now: it is evidence the agent was wrong.</li>
    </ul>
    <h3>Screen reader</h3>
    <p>A multi-select listbox. Each option's name says the kind, the subject, and who proposed it; the detail panel on the right is a complementary landmark whose heading changes with the selection and is announced politely.</p>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w1">W1 Morning pass</a> · <a href="workflows.html#w3">W3 Reply with the agent's draft</a> · <a href="workflows.html#w6">W6 Check the agent</a></p>
  </section>
</main>
<aside class="evidence" aria-labelledby="ev-review" aria-live="polite">
  <h2 id="ev-review">Re: Cabinet hardware</h2>
  <p class="small muted">Written by Claude, Oct 29 at 8:05 AM, from the conversation “Cabinet hardware”. Queued in your outbox; nothing has been sent.</p>
  <dl class="kv" style="margin:.6rem 0">
    <dt>From</dt><dd>you@example.com</dd>
    <dt>To</dt><dd>Rafael Ortiz &lt;rafael@ortizreno.example&gt;</dd>
    <dt>Cc</dt><dd>Jordan Lee</dd>
  </dl>
  <div class="card" style="padding:.7rem .8rem;font-size:.92rem">
    <p>Thanks Rafael — let's go with <mark style="background:var(--warn-tint);color:var(--warn);padding:0 .2rem;border:1px dashed currentColor">brushed brass or matte black?</mark>.</p>
    <p>Monday delivery works; I'll be home from 10.</p>
    <p>Could you also send the revised quote with the island when you have it?</p>
  </div>
  <p class="small" style="margin:.5rem 0 .25rem"><strong>Why the agent wrote this</strong></p>
  <blockquote class="quote">“Can you pick brushed brass or matte black by Monday so I can order?”<cite>Rafael, Oct 29 · the agent cannot choose a finish for you, so it left the choice blank</cite></blockquote>
  <div style="display:flex;gap:.4rem;flex-wrap:wrap;margin-top:.6rem">
    <button class="btn primary" aria-disabled="true" aria-describedby="blank-why" style="opacity:.55">Send <kbd>a</kbd></button>
    <a class="btn" href="compose.html#draft">Edit <kbd>e</kbd></a>
    <button class="btn quiet">Return to drafts <kbd>⌫</kbd></button>
  </div>
  <p id="blank-why" class="small" style="margin-top:.4rem;color:var(--warn)">Send is off until the blank is filled. Press <kbd>e</kbd> to choose.</p>
</aside>
</div>
'''
body += foot()
write('review.html', body)
