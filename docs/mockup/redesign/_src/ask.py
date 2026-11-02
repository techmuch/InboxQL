from chrome import *

Q = 'How much have we spent on the kitchen so far, and what is still to pay?'

main = f'''
  <p class="crumbs"><a href="today.html">Today</a> › Asked in words</p>
  <div class="page-head"><div class="grow">
    <p class="small muted" style="margin:0">You asked · 9:41 AM</p>
    <h1 id="page-title">{Q}</h1>
    <p class="small muted" style="margin:.3rem 0 0">{C(1)} Answered by <strong>Claude Code</strong> on this computer · 14 s · read 4 messages and 1 file</p>
  </div></div>

  <section class="section card" style="padding:1rem 1.2rem" aria-labelledby="h-ans">
    <h2 id="h-ans">Answer {C(2)}</h2>
    <p style="font-size:1.05rem;margin:.4rem 0">You have spent <strong>$5,412.20</strong> of the <strong>$18,450</strong> quote, so <strong>$13,037.80</strong> is left.
      The quote asks for <strong>40% when the cabinets are delivered</strong> — <strong>$7,380</strong>, which would be due Monday, Nov 2, if you approve the delivery.</p>
    <p class="small muted" style="margin:.2rem 0 0">Totals come from receipts the agent extracted; the payment schedule from the quote PDF, page 3. <a href="#ev-ask">See how it was answered</a>.</p>
  </section>

  <section class="section" aria-labelledby="h-res">
    <header><h2 id="h-res" class="grow">What it is based on {C(3)}</h2><a class="small" href="query.html">Open as a query</a></header>
    <div style="overflow-x:auto" tabindex="0" role="region" aria-label="Payments table">
    <table class="grid"><caption class="sr-only">Payments for the kitchen remodel</caption>
      <thead><tr><th scope="col">Date</th><th scope="col">Paid to</th><th scope="col">From</th><th scope="col" class="num">Amount</th></tr></thead>
      <tbody>
        <tr><td>Oct 14</td><td>Ortiz Renovations — deposit</td><td><a href="conversation.html">Deposit received</a></td><td class="num">$5,000.00</td></tr>
        <tr><td>Oct 19</td><td>Hardware Depot #87140</td><td><a href="#">Your order</a></td><td class="num">$199.80</td></tr>
        <tr><td>Oct 30</td><td>Hardware Depot #88231</td><td><a href="#">Order confirmed</a></td><td class="num">$212.40</td></tr>
        <tr><th scope="row" colspan="3">Spent</th><td class="num"><strong>$5,412.20</strong></td></tr>
      </tbody></table></div>
    <blockquote class="quote" style="margin-top:.75rem">Payment schedule: 25% deposit on signing · 40% on cabinet delivery · 35% on completion.<cite>Quote-v2.pdf, page 3 · Rafael Ortiz, Oct 27 · <a href="files.html">open</a></cite></blockquote>
  </section>

  <section class="section" aria-labelledby="h-next">
    <header><h2 id="h-next" class="grow">Keep it {C(4)}</h2></header>
    <div style="display:flex;flex-wrap:wrap;gap:.5rem">
      <button class="btn primary">Add “Kitchen money” to Kitchen remodel</button>
      <button class="btn">Save the query as a subject</button>
      <button class="btn">Remind me before the $7,380 payment</button>
    </div>
  </section>

  <form class="section card" style="padding:.75rem 1rem" aria-labelledby="h-follow" onsubmit="return false">
    <h2 id="h-follow" style="font-size:1rem">Ask a follow-up {C(5)}</h2>
    <p class="small muted" style="margin:.2rem 0 .5rem">Continues the same conversation with Claude Code, so “it” and “that payment” mean what you think.</p>
    <div style="display:flex;gap:.5rem;flex-wrap:wrap"><label for="fu" class="sr-only">Follow-up question</label>
      <input id="fu" class="btn" style="flex:1;min-width:12rem" value="Is the island included in that quote?"><button class="btn primary">Ask <kbd>↵</kbd></button></div>
  </form>

  <section class="section" aria-labelledby="h-states">
    <h2 id="h-states">Other moments</h2>
    <div style="display:grid;gap:.75rem;grid-template-columns:repeat(auto-fit,minmax(16rem,1fr))">
      <figure class="card" style="margin:0;padding:.75rem 1rem"><figcaption class="small muted">Starting — typing in the query bar</figcaption>
        <div class="querybar" style="margin-top:.4rem"><span aria-hidden="true" class="muted">⌕</span><span class="small" style="flex:1">how much have we spent on the kitchen</span></div>
        <ul class="small" style="list-style:none;padding:0;margin:.4rem 0 0;display:grid;gap:.25rem">
          <li class="chip accent" style="justify-self:start">Ask Claude Code <kbd>⌘↵</kbd></li>
          <li class="muted">Search for these words <kbd>↵</kbd></li></ul>
        <p class="small" style="margin:.4rem 0 0">Words that are not a query offer both. A query (<code>from:…</code>) is just run.</p></figure>
      <figure class="card" style="margin:0;padding:.75rem 1rem"><figcaption class="small muted">While it works {C(6)}</figcaption>
        <ol class="small" style="padding-left:1.1rem;margin:.4rem 0 0"><li>Finding the kitchen topic ✓</li><li>Adding up receipts in it ✓</li><li>Reading Quote-v2.pdf…</li></ol>
        <div style="display:flex;gap:.4rem;margin-top:.5rem"><button class="btn">Stop</button><span class="small muted" style="align-self:center">9 s</span></div></figure>
      <figure class="card" style="margin:0;padding:.75rem 1rem"><figcaption class="small muted">When it cannot answer</figcaption>
        <p class="small" style="margin:.4rem 0">Claude Code is installed but not signed in. Open a terminal and run <code>claude</code> once, then ask again.</p>
        <p class="small muted" style="margin:0">Or <a href="query.html">write the query yourself</a> — nothing about asking in words is needed for that.</p></figure>
    </div>
  </section>
'''
main += notes('n-ask',
    'A question in plain words, answered by the agent CLI you already use (Claude Code, agy), running on this computer with the iql skill. The answer is never only prose: it comes with the results it rests on and the exact commands it ran, so it can be checked, kept or turned into a query.',
    ['It always says <strong>who answered, how long it took, and how much it read</strong>. What was sent to the agent’s provider is in the evidence panel.',
     'The answer comes first, in sentences, with every figure in bold so the eye finds the numbers.',
     '<strong>Evidence, not trust.</strong> The rows and the quoted line the answer rests on are shown, each linked to its message or file.',
     'An answer can become something lasting: a section on a subject, a saved query, or a reminder. The agent only proposes; you press the button.',
     'A follow-up continues the same agent session.',
     'While it works, its steps are listed in words from the commands it runs, in a polite live region announced step by step, with Stop.'],
    ['New. Asking in words replaces nothing: the query bar still runs queries; words that are not a query get two choices.',
     'Answers go through the agent CLI rather than InboxQL’s own AI provider settings, so there is one place your mail goes and one account to pay for.'],
    [('<kbd>⌘↵</kbd> in the query bar', 'Ask in words'), ('<kbd>Esc</kbd>', 'Stop while it works'), ('<kbd>f</kbd>', 'Focus the follow-up')],
    'The page heading is the question. The answer is a section headed “Answer”; progress steps are announced as they finish. Bold figures are also in the table below, so nothing depends on emphasis alone.',
    '<a href="workflows.html#w11">W11 Ask in words</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-ask"><h2 id="ev-ask">How it was answered</h2>
  <p class="small muted">Commands Claude Code ran, from InboxQL’s own log. <a href="#">Open in the log</a></p>
  <ol class="small" style="padding-left:1.1rem;display:grid;gap:.5rem">
    <li><code>iql --json query "in:tickets OR topic:* kitchen | count by topic"</code><br><span class="muted">0.03 s · found topic <em>kitchen-remodel</em></span></li>
    <li><code>iql --json query "topic:kitchen-remodel extract:receipts | sum receipts.amount"</code><br><span class="muted">0.05 s · $5,412.20 in 3 records</span></li>
    <li><code>iql --json query "in:attachments topic:kitchen-remodel content:payment"</code><br><span class="muted">0.04 s · Quote-v2.pdf</span></li>
    <li><code>iql --json read m-2f81c --attachments</code><br><span class="muted">0.02 s · page 3, payment schedule</span></li>
  </ol>
  <h3>Sent to Anthropic</h3><p class="small">Your question, 4 messages and 1 file (12 KB of text), by Claude Code under your own account. Nothing else from the mailbox.</p>
  <h3>Changed</h3><p class="small">Nothing. It may only read; anything it suggests is a button for you.</p>
  <p class="small"><a href="settings-agents.html#setting-ask-cli">Change which agent answers</a></p>
</aside>'''
page('ask.html', 'Ask in words', 'A plain-language question answered by the agent CLI, with the evidence and the commands it ran.', Q, '', main, aside)
