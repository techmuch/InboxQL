from chrome import *

body = head('Agent activity', 'Everything agents and rules did, by pass, with who, why and undo.')
body += banner('activity.html')
body += appbar('in:activity after:1d')
body += '<div class="shell">\n' + nav('Agent activity')


def entry(time, what, detail, chip, undo=True):
    u = '<button class="btn">Undo</button>' if undo else '<span class="small muted">—</span>'
    return (f'<li class="row"><span class="meta" style="text-align:left">{time}</span>'
            f'<div><span class="who">{what}</span> {chip}<div class="line">{detail}</div></div>{u}</li>')


body += '''
<main id="main" class="main" aria-labelledby="page-title">
  <div class="page-head">
    <div class="grow">
      <p class="crumbs"><a href="today.html">← Today</a></p>
      <h1 id="page-title">Agent activity</h1>
      <p class="muted" style="margin:0">What was done in your mailbox by someone other than you, newest first. Your own actions are under <a href="#">History</a>.</p>
    </div>
  </div>
  <div role="group" aria-label="Show activity by" style="display:flex;flex-wrap:wrap;gap:.35rem;margin-bottom:.75rem">
    <button class="chip accent" aria-pressed="true">Everyone</button>
    <button class="chip" aria-pressed="false">Claude</button>
    <button class="chip" aria-pressed="false">Codex</button>
    <button class="chip" aria-pressed="false">Rules</button>
    <button class="chip" aria-pressed="false">Changed mail</button>
    <button class="chip" aria-pressed="false">Proposed only</button>
  </div>

  <section class="section" aria-labelledby="p412">
    <header><h2 id="p412" class="grow">Claude · work loop pass, 8:02–8:09 AM</h2>
      <span class="small muted">10 conversations · 41k tokens · $0.19</span>
      <button class="btn">Undo the whole pass</button></header>
    <ol class="list card">
'''
body += entry('8:09', 'Archived 6 conversations', 'All judged <em>ignorable</em>: promotions from Hardware Depot, Lumen Air, Bookshelf', '<span class="chip">changed mail</span>')
body += entry('8:07', 'Labelled 3 “Receipts”', 'Hardware Depot $212.40 · Lumen Air $164.00 · Bookshelf $18.99', '<span class="chip">changed mail</span>')
body += entry('8:06', 'Extracted 5 receipts', 'Values with quotes; 1 below its confidence target, sent to checks', '<span class="chip agent">answer</span>')
body += entry('8:05', 'Drafted a reply to Rafael Ortiz', 'Cabinet hardware · left 1 blank for you · queued in To review', '<span class="chip agent">proposed</span>')
body += entry('8:05', 'Proposed an event', 'Cabinet delivery, Mon Nov 2 10:30 · waiting in To review', '<span class="chip agent">proposed</span>')
body += entry('8:04', 'Judged 4 conversations waiting on you', 'Rafael Ortiz, Priya Raman, Dana Whitfield, Northwind Bank', '<span class="chip agent">answer</span>')
body += '''
    </ol>
  </section>

  <section class="section" aria-labelledby="rule1">
    <header><h2 id="rule1" class="grow">Rule “Bank alerts become cards” · 7:12 AM</h2></header>
    <ol class="list card">
'''
body += entry('7:12', 'Proposed a card', 'Confirm new payee at Northwind · from Northwind Bank', '<span class="chip">rule</span>')
body += '''
    </ol>
  </section>

  <section class="section" aria-labelledby="sync">
    <header><h2 id="sync" class="grow">Your other devices · yesterday</h2></header>
    <ol class="list card">
'''
body += entry('Fri 9:40 PM', 'Read 12 messages on your phone (Gmail)', 'Applied here by sync', '<span class="chip">sync</span>', undo=False)
body += '''
    </ol>
  </section>

  <section class="notes" aria-labelledby="notes-act">
    <h2 id="notes-act">About this screen</h2>
    <p><strong>Purpose.</strong> Value 2, <em>nothing happens silently</em>. Agents act through the CLI, out of sight. This is where everything they did is visible afterwards, grouped by the pass that did it, with its cost, and undoable — one change or the whole pass.</p>
    <h3>What “Undo” means</h3>
    <ul>
      <li><strong>Changed mail</strong> (archive, label): reversed here and on the server, as <code>iql mail undo</code> does now.</li>
      <li><strong>Answers</strong> (a judgement, an extraction): undo removes the answer and makes the conversation pending again. Better is usually to correct it — open the conversation.</li>
      <li><strong>Proposed</strong> items have not happened; undo withdraws the proposal.</li>
      <li>Sync from your other devices is shown for completeness and cannot be undone here.</li>
    </ul>
    <h3>Not a notification feed</h3>
    <p>Nothing here demands attention; it is a record. Things that need you are in Today and To review. The status line in the app bar gives a one-line summary of the last pass.</p>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w5">W5 Correct the agent</a> · <a href="workflows.html#w8">W8 Find out what happened</a></p>
  </section>
</main>
<aside class="evidence" aria-labelledby="ev-act">
  <h2 id="ev-act">Claude this week</h2>
  <dl class="kv small" style="margin:.5rem 0 1rem">
    <dt>Passes</dt><dd>14</dd>
    <dt>Conversations</dt><dd>312 read</dd>
    <dt>Changed mail</dt><dd>188 archived, 64 labelled</dd>
    <dt>Undone by you</dt><dd>3 (1%)</dd>
    <dt>Cost</dt><dd>$2.41</dd>
  </dl>
  <h3>How often it is right</h3>
  <table class="doc small"><thead><tr><th scope="col">Element</th><th scope="col">Right</th><th scope="col">Target</th></tr></thead>
    <tbody>
      <tr><td>waiting</td><td>41 / 44</td><td>90% ✓</td></tr>
      <tr><td>importance</td><td>37 / 44</td><td>90% <span class="chip warn">below</span></td></tr>
      <tr><td>receipts</td><td>58 / 60</td><td>95% ✓</td></tr>
    </tbody></table>
  <p class="small muted">From your rulings on random checks only. Below target, the agent's answers are no longer pre-filled and more checks are drawn.</p>
</aside>
</div>
'''
body += foot()
write('activity.html', body)
