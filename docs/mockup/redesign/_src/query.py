from chrome import *

Q = 'extract:receipts after:2026-01-01 -from:me()'
body = head('Ask', 'The query as the interface: every view is a query you can see, build by clicking, save as a subject, or hand to an agent.')
body += banner('query.html')
body += appbar(Q)
body += '<div class="shell">\n' + nav('Receipts 2026')

bars = [('Jan', 612), ('Feb', 488), ('Mar', 903), ('Apr', 540), ('May', 1210), ('Jun', 760), ('Jul', 2130), ('Aug', 1480), ('Sep', 5820), ('Oct', 1964)]
mx = max(v for _, v in bars)
chart = ''.join(
    f'<div style="display:flex;flex-direction:column;align-items:center;gap:.2rem;flex:1;min-width:1.6rem">'
    f'<div title="{m}: ${v:,}" style="width:100%;max-width:2.2rem;height:{max(4, round(v / mx * 120))}px;background:var(--accent);border-radius:3px 3px 0 0"></div>'
    f'<span class="small muted">{m}</span></div>' for m, v in bars)
table = ''.join(f'<tr><th scope="row">{m}</th><td>${v:,}</td></tr>' for m, v in bars)

body += f'''
<main id="main" class="main" aria-labelledby="page-title">
  <div class="page-head">
    <div class="grow">
      <h1 id="page-title">Receipts 2026</h1>
      <p class="muted" style="margin:0"><span class="callout" aria-hidden="true">1</span>Receipts the agent extracted this year, not from you — <strong>142 messages</strong>.</p>
    </div>
    <button class="btn" aria-pressed="true">Pinned as a subject</button>
    <button class="btn">Hand to an agent…</button>
  </div>

  <div role="group" aria-label="The query, as terms" style="display:flex;flex-wrap:wrap;gap:.4rem;align-items:center;margin-bottom:.4rem"><span class="callout" aria-hidden="true">2</span>
    <span class="chip accent">has a receipt <button class="btn quiet" style="padding:0 .2rem" aria-label="Remove: has a receipt">×</button></span>
    <span class="chip accent">since Jan 1, 2026 <button class="btn quiet" style="padding:0 .2rem" aria-label="Remove: since January 1, 2026">×</button></span>
    <span class="chip accent">not from me <button class="btn quiet" style="padding:0 .2rem" aria-label="Remove: not from me">×</button></span>
    <button class="chip">+ Add a filter</button>
    <span class="small muted" style="margin-left:.5rem">or try: <button class="chip">from people only</button> <button class="chip">over $100</button> <button class="chip">with a PDF</button></span>
  </div>

  <div class="tabs" role="tablist" aria-label="Show as"><span class="callout" aria-hidden="true">3</span>
    <a href="#" role="tab" aria-selected="false">Messages</a>
    <a href="#" role="tab" aria-selected="false">Conversations</a>
    <a href="#" role="tab" aria-selected="true">Total by month</a>
    <a href="#" role="tab" aria-selected="false">By merchant</a>
    <a href="#" role="tab" aria-selected="false">Count</a>
  </div>

  <figure class="card" style="padding:1rem;margin:0" aria-labelledby="chart-cap">
    <figcaption id="chart-cap"><strong>Total spent by month, 2026</strong> <span class="muted small">— sum of <code>receipts.total</code>, USD · $15,907 in all</span></figcaption>
    <div aria-hidden="true" style="display:flex;align-items:flex-end;gap:.4rem;height:150px;margin-top:.75rem">{chart}</div>
    <details style="margin-top:.75rem"><summary class="small">Show as a table</summary>
      <table class="doc" style="max-width:20rem"><thead><tr><th scope="col">Month</th><th scope="col">Total</th></tr></thead><tbody>{table}</tbody></table>
    </details>
    <p class="small muted" style="margin:.4rem 0 0">September is high: <strong>Ortiz Renovations deposit $5,000</strong>. Click a bar to open its receipts.</p>
  </figure>

  <section class="section" aria-labelledby="h-sel">
    <header><h2 id="h-sel" class="grow">When the view is messages</h2></header>
    <div class="card" style="display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;padding:.5rem .75rem;background:var(--accent-tint)">
      <strong>12 selected</strong> <span class="muted small">of 142</span>
      <button class="btn">Select all 142</button>
      <button class="btn">Archive</button><button class="btn">Label…</button><button class="btn">Mark read</button>
      <button class="btn">Narrow to these senders</button>
      <span style="flex:1"></span><button class="btn quiet">Clear selection <kbd>Esc</kbd></button>
    </div>
    <p class="small muted" style="margin-top:.35rem">A selection belongs to this view: changing the query clears it, and says so.</p>
  </section>

  <section class="notes" aria-labelledby="notes-q">
    <h2 id="notes-q">About this screen</h2>
    <p><strong>Purpose.</strong> Objective 4, <em>the query is the interface</em>. This is today's Desk and Dashboard combined: any view in the app opens here as its query, shown three ways at once — the text in the app bar, removable terms in plain words, and the results.</p>
    <h3>Callouts</h3>
    <ol>
      <li><span class="callout">1</span> A plain-language sentence of what the query means and how many it found. When Trash or Spam are left out, it says how many matched there.</li>
      <li><span class="callout">2</span> Terms as chips, named in words (“not from me”, not <code>-from:me()</code>). Removing one edits the text; typing in the text updates the chips. Suggestions are the terms most likely to narrow <em>this</em> result.</li>
      <li><span class="callout">3</span> “Show as” replaces the pipeline stage: Messages, Conversations (<code>| timeline</code>), a total, a grouping, a count. Every chart has a table view and a sentence summary; colour is never the only encoding.</li>
    </ol>
    <h3>Hand to an agent</h3>
    <p>Copies a ready instruction — for example <code>iql work next --query "extract:receipts after:2026-01-01 -from:me()"</code> — or, with a connected agent, starts it. The agent's results come back as proposals in To review.</p>
    <h3>Ask in words <span class="planned">planned</span></h3>
    <p>Typing a question in the query bar (“what did I spend on the kitchen?”) asks the configured model to write the query, and shows the query for you to run. It never runs a query you have not seen, and with no model configured the bar only accepts the language.</p>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w4">W4 Clear a group</a> · <a href="workflows.html#w7">W7 Ask a question of the mail</a></p>
  </section>
</main>
<aside class="evidence" aria-labelledby="ev-q">
  <h2 id="ev-q">This query</h2>
  <pre class="mono card" style="padding:.5rem .6rem;white-space:pre-wrap;margin:.5rem 0">{Q}
| sum receipts.total by month</pre>
  <dl class="kv small">
    <dt>Kind</dt><dd>mail</dd>
    <dt>Left out</dt><dd>Trash and Spam — 3 receipts there</dd>
    <dt>Coverage</dt><dd>The receipts element has read 98% of this year's mail; 214 messages wait</dd>
    <dt>Saved as</dt><dd>“Receipts 2026”, pinned</dd>
  </dl>
  <h3 style="margin-top:1rem">Biggest merchants</h3>
  <ol class="small" style="padding-left:1.2rem">
    <li>Ortiz Renovations · $5,000</li><li>Hardware Depot · $2,941</li><li>Lumen Air · $1,312</li><li>Northwind Grocers · $1,105</li>
  </ol>
  <p class="small muted">Values are extracted spans of the mail itself; each total links to its receipt.</p>
</aside>
</div>
'''
body += foot()
write('query.html', body)
