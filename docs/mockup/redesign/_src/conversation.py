from chrome import *

body = head('Cabinet hardware', 'Reading a conversation, with what the agent concluded shown against the words that support it.')
body += banner('conversation.html')
body += appbar('thread:&lt;cabinet-hardware@ortizreno.example&gt;')
body += '<div class="shell">\n' + nav('Rafael Ortiz')
body += '''
<main id="main" class="main" aria-labelledby="page-title">
  <p class="crumbs"><a href="today.html">← Today</a> <span aria-hidden="true">›</span> <a href="subject.html">Rafael Ortiz</a></p>
  <div class="page-head">
    <div class="grow">
      <h1 id="page-title">Cabinet hardware</h1>
      <p class="muted" style="margin:0">3 messages · Rafael Ortiz, you · <span class="chip">Kitchen remodel</span> <span class="chip warn">waiting on you</span></p>
    </div>
  </div>
  <div role="toolbar" aria-label="Conversation actions" style="display:flex;flex-wrap:wrap;gap:.4rem;margin-bottom:1rem"><span class="callout" aria-hidden="true">1</span>
    <a class="btn primary" href="compose.html">Reply <kbd>r</kbd></a>
    <a class="btn" href="compose.html#draft">Use the agent's draft <kbd>d</kbd></a>
    <button class="btn">Forward</button>
    <button class="btn">Archive <kbd>e</kbd></button>
    <button class="btn">Label <kbd>l</kbd></button>
    <button class="btn">Not waiting on me <kbd>n</kbd></button>
    <button class="btn quiet">More…</button>
  </div>

  <details class="card" style="padding:.6rem .9rem;margin-bottom:.5rem">
    <summary><strong>Rafael Ortiz</strong> <span class="muted">· Oct 20 · Cabinet layout attached; hardware next</span></summary>
    <p style="margin-top:.5rem">Layout attached. Once you sign off we'll talk hardware.</p>
  </details>
  <details class="card" style="padding:.6rem .9rem;margin-bottom:.5rem">
    <summary><strong>You</strong> <span class="muted">· Oct 21 · Signed off — looks great</span></summary>
    <p style="margin-top:.5rem">Signed off — looks great. Send hardware options when you have them.</p>
  </details>
  <article class="card" style="padding:1rem 1.1rem" aria-labelledby="m3-from">
    <header style="display:flex;gap:.75rem;align-items:center;margin-bottom:.75rem">
      <span class="avatar" aria-hidden="true">RO</span>
      <div class="grow" style="flex:1"><strong id="m3-from">Rafael Ortiz</strong> <span class="muted small">to you · cc Jordan Lee</span></div>
      <time class="small muted" datetime="2026-10-29T07:42">Thu, Oct 29, 7:42 AM</time>
    </header>
    <p>Hi — options attached. Both are in stock at the supplier this week.</p>
    <p><mark style="background:var(--agent-tint);color:inherit;padding:0 .15rem;box-shadow:inset 0 -2px 0 var(--agent)">Can you pick brushed brass or matte black by Monday so I can order?</mark><sup><a href="#c-waiting" aria-label="Evidence for: waiting on you">1</a></sup>
      If the order goes in Monday, <mark style="background:var(--agent-tint);color:inherit;padding:0 .15rem;box-shadow:inset 0 -2px 0 var(--agent)">we can deliver the cabinets Monday Nov 2 between 10:30 and 12:30</mark><sup><a href="#c-event" aria-label="Evidence for: proposed event">2</a></sup>.</p>
    <p>Thanks,<br>Rafael</p>
    <div style="display:flex;gap:.5rem;flex-wrap:wrap;margin-top:.75rem">
      <span class="chip">📎 Hardware-options.jpg · 412 KB</span><span class="chip">📎 Supplier-sheet.pdf · 88 KB</span>
    </div>
  </article>
  <p class="small muted" style="margin-top:.5rem"><span class="callout" aria-hidden="true">2</span> Highlighted words are what the agent read as the reason for its conclusions. Each links to the conclusion on the right.</p>

  <section class="notes" aria-labelledby="notes-conv">
    <h2 id="notes-conv">About this screen</h2>
    <p><strong>Purpose.</strong> Read a conversation and act on it. What makes this different from a mail client is the right-hand panel: every conclusion an agent drew from this conversation, with the words it drew it from (value 4, <em>show the evidence</em>), and one action to accept, correct or undo it (objective 3).</p>
    <h3>Callouts</h3>
    <ol>
      <li><span class="callout">1</span> One toolbar with real buttons and keys. “Use the agent's draft” appears only when a draft exists; it opens the draft for editing — it never sends.</li>
      <li><span class="callout">2</span> Evidence highlights use a tint <em>and</em> an underline, so they are not colour alone, and each has a numbered link a screen reader can follow to the conclusion.</li>
    </ol>
    <h3>Reading order</h3>
    <p>Older messages are collapsed <code>&lt;details&gt;</code> elements: a screen reader hears sender, date and first line, and opens one with Enter. The newest message is an <code>&lt;article&gt;</code> named by its sender. The panel is a complementary landmark after the main content.</p>
    <h3>Opening marks read</h3>
    <p>Opening a conversation marks it read, as it does now — but Today and the subject page let you see what's in it (the first line, the agent's summary) without opening it, so triage doesn't change read state.</p>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w3">W3 Reply with the agent's draft</a> · <a href="workflows.html#w5">W5 Correct the agent</a></p>
  </section>
</main>
<aside class="evidence" aria-labelledby="ev-conv">
  <h2 id="ev-conv">What the agent concluded</h2>
  <p class="small muted">Claude, Oct 29 at 8:05 AM · work loop pass 412</p>

  <section id="c-waiting" class="card" style="padding:.6rem .75rem;margin:.6rem 0" aria-labelledby="c1">
    <h3 id="c1"><span class="muted">1</span> Waiting on you</h3>
    <blockquote class="quote">“Can you pick brushed brass or matte black by Monday so I can order?”<cite>Rafael, Oct 29</cite></blockquote>
    <div style="display:flex;gap:.35rem;flex-wrap:wrap"><button class="btn">Right</button><button class="btn">On Rafael</button><button class="btn">No one</button></div>
  </section>

  <section id="c-event" class="card" style="padding:.6rem .75rem;margin:.6rem 0" aria-labelledby="c2">
    <h3 id="c2"><span class="muted">2</span> Event proposed</h3>
    <p class="small" style="margin:.2rem 0">Cabinet delivery · Mon Nov 2, 10:30–12:30 · your calendar</p>
    <blockquote class="quote">“we can deliver the cabinets Monday Nov 2 between 10:30 and 12:30”<cite>Rafael, Oct 29</cite></blockquote>
    <div style="display:flex;gap:.35rem;flex-wrap:wrap"><button class="btn primary">Add to calendar</button><button class="btn">Edit</button><button class="btn quiet">Reject</button></div>
  </section>

  <section class="card" style="padding:.6rem .75rem;margin:.6rem 0" aria-labelledby="c3">
    <h3 id="c3">Card</h3>
    <p class="small" style="margin:.2rem 0">“Order cabinet hardware”, due Nov 2 · <span class="chip">To do</span> accepted by you Oct 26</p>
  </section>

  <section class="card" style="padding:.6rem .75rem;margin:.6rem 0" aria-labelledby="c4">
    <h3 id="c4">Importance · Topic</h3>
    <p class="small" style="margin:.2rem 0"><span class="chip">important</span> <span class="chip">Kitchen remodel</span></p>
    <button class="btn quiet small">Change</button>
  </section>

  <section class="card" style="padding:.6rem .75rem;margin:.6rem 0" aria-labelledby="c5">
    <h3 id="c5">Reply drafted</h3>
    <p class="small" style="margin:.2rem 0">“Thanks Rafael — let's go with [your choice]. Monday delivery works…” <span class="chip agent">agent draft · not sent</span> <span class="chip warn">1 blank</span></p>
    <a class="btn" href="compose.html#draft">Open the draft</a>
  </section>
  <p class="small muted">Corrections are recorded as your rulings: they outrank the agent here and count toward how often it is right.</p>
</aside>
</div>
'''
body += foot()
write('conversation.html', body)
