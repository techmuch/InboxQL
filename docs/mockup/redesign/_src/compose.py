from chrome import *

body = head('Reply', 'Writing and sending: an agent draft with a blank only you can fill, and a send you can undo.')
body += banner('compose.html')
body += appbar('in:drafts')
body += '<div class="shell">\n' + nav('Drafts and outbox')
body += '''
<main id="main" class="main" aria-labelledby="page-title">
  <p class="crumbs"><a href="conversation.html">← Cabinet hardware</a></p>
  <div class="page-head">
    <div class="grow">
      <h1 id="page-title">Reply to Rafael Ortiz</h1>
      <p class="muted" style="margin:0"><span class="chip agent">started by the agent</span> You are editing it; the agent will not change it again.</p>
    </div>
  </div>

  <form class="card" style="padding:1rem" aria-labelledby="page-title" onsubmit="return false">
    <div style="display:grid;grid-template-columns:max-content 1fr;gap:.5rem .75rem;align-items:center">
      <label for="from">From</label>
      <select id="from" class="btn" style="justify-self:start"><option>you@example.com (Personal)</option><option>you@work.example (Work)</option></select>
      <label for="to">To</label>
      <input id="to" class="btn" style="width:100%" value="Rafael Ortiz <rafael@ortizreno.example>">
      <label for="cc">Cc</label>
      <input id="cc" class="btn" style="width:100%" value="Jordan Lee <jordan@example.net>">
      <label for="subj">Subject</label>
      <input id="subj" class="btn" style="width:100%" value="Re: Cabinet hardware">
    </div>

    <fieldset style="border:1px dashed var(--warn);border-radius:var(--radius);margin:1rem 0 .75rem;padding:.6rem .8rem;background:var(--warn-tint)">
      <legend style="color:var(--warn);font-weight:600;padding:0 .3rem"><span class="callout" aria-hidden="true">1</span>Your decision: which finish?</legend>
      <p class="small" style="margin:0 0 .4rem">Rafael asked: “Can you pick brushed brass or matte black by Monday so I can order?”</p>
      <label style="margin-right:1rem"><input type="radio" name="finish"> Brushed brass</label>
      <label style="margin-right:1rem"><input type="radio" name="finish" checked> Matte black</label>
      <label><input type="radio" name="finish"> Something else — I'll write it</label>
    </fieldset>

    <label for="body" class="sr-only">Message</label>
    <textarea id="body" rows="9" style="width:100%;font:inherit;padding:.75rem;border:1px solid var(--border);border-radius:var(--radius);background:var(--bg);color:var(--text)">Thanks Rafael — let's go with matte black.

Monday delivery works; I'll be home from 10.

Could you also send the revised quote with the island when you have it?

— Alex</textarea>

    <div style="display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;margin-top:.75rem"><span class="callout" aria-hidden="true">2</span>
      <button class="btn primary">Send <kbd>⌘↵</kbd></button>
      <button class="btn">Send later…</button>
      <button class="btn">Save draft</button>
      <span class="spacer" style="flex:1"></span>
      <button class="btn quiet">Discard</button>
    </div>
  </form>
    <figure style="margin:.5rem 0 0"><figcaption class="small muted">Example — After pressing Send:</figcaption><div class="toast demo">Sending to Rafael Ortiz and Jordan Lee in 8 s <button type="button">Undo</button></div></figure>

  <section class="notes" aria-labelledby="notes-compose">
    <h2 id="notes-compose">About this screen</h2>
    <p><strong>Purpose.</strong> Writing happens in the page, beside its context, not in a modal over it. A reply opened from a conversation keeps the conversation in the right-hand panel.</p>
    <h3>Callouts</h3>
    <ol>
      <li><span class="callout">1</span> <strong>The decision is a form control, not a sentence to edit.</strong> When an agent's draft needs something only you know, the choice is a labelled radio group taken from the words of the question. Choosing fills the draft. Send is off until it is answered.</li>
      <li><span class="callout">2</span> Send waits 10 seconds with <strong>Undo</strong> in the status toast (and <kbd>z</kbd>), as now. “Send later” is <span class="planned">planned</span>.</li>
    </ol>
    <h3>Who wrote what</h3>
    <p>A draft records its origin (agent or you). Once you edit an agent draft it is yours; the outbox shows “started by the agent, edited by you” so an unedited agent draft is always recognisable.</p>
    <h3>Keyboard and screen reader</h3>
    <p>All fields are labelled; the decision is a <code>fieldset</code> with a <code>legend</code>, so a screen reader announces the question before the choices. <kbd>⌘↵</kbd> sends; <kbd>Esc</kbd> returns to the conversation and keeps the draft.</p>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w3">W3 Reply with the agent's draft</a></p>
  </section>
</main>
<aside class="evidence" aria-labelledby="ev-compose">
  <h2 id="ev-compose">Replying to</h2>
  <article class="card" style="padding:.7rem .8rem;margin:.5rem 0 1rem;font-size:.9rem">
    <p class="small muted" style="margin:0 0 .4rem"><strong style="color:var(--text)">Rafael Ortiz</strong> · Thu, Oct 29, 7:42 AM</p>
    <p>Hi — options attached. Both are in stock at the supplier this week.</p>
    <p>Can you pick brushed brass or matte black by Monday so I can order? If the order goes in Monday, we can deliver the cabinets Monday Nov 2 between 10:30 and 12:30.</p>
    <span class="chip">📎 Hardware-options.jpg</span>
  </article>
  <h2>Also open with Rafael</h2>
  <p class="small">You asked on Oct 26 for <strong>the revised quote with the island</strong>; no answer yet. <span class="muted">The agent added a line asking again.</span></p>
  <h2 style="margin-top:1rem">Outbox</h2>
  <ul class="small" style="padding-left:1.1rem">
    <li>This reply — editing</li>
    <li>Re: Saturday carpool — <span class="chip">draft</span></li>
  </ul>
</aside>
</div>

'''
body += foot()
write('compose.html', body)
