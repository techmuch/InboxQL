from chrome import *

body = head('Phone', 'The phone layout: the same three parts stacked, with a bottom bar of four places.')
body += banner('mobile.html')

PHONE = 'width:min(23.5rem,100%);border:10px solid var(--text);border-radius:2rem;overflow:hidden;background:var(--bg);display:flex;flex-direction:column;height:46rem'
TABBAR = lambda cur, t='': (f'<nav aria-label="Places ({t})" style="margin-top:auto;display:grid;grid-template-columns:repeat(4,1fr);border-top:1px solid var(--border);background:var(--surface)">'
    + ''.join(f'<a href="#" {"aria-current=\"page\"" if n == cur else ""} style="text-align:center;padding:.55rem 0 .7rem;font-size:.8rem;text-decoration:none;color:{"var(--accent-text);font-weight:600" if n == cur else "var(--muted)"}"><span aria-hidden="true" style="display:block;font-size:1.1rem">{i}</span>{n}{b}</a>'
              for n, i, b in [('Today', '◎', ''), ('Review', '✓', ' <span class="chip warn" style="padding:0 .3rem">9</span>'), ('Ask', '⌕', ''), ('Subjects', '☰', '')])
    + '</nav>')


def phone(title, label, inner, cur):
    return (f'<figure style="margin:0;display:flex;flex-direction:column;align-items:center;gap:.6rem" aria-labelledby="{label}">'
            f'<div style="{PHONE}" role="group" aria-label="Phone: {title}"><div style="flex:1;overflow:hidden;display:flex;flex-direction:column">{inner}</div>{TABBAR(cur, title)}</div>'
            f'<figcaption id="{label}" class="small"><strong>{title}</strong></figcaption></figure>')


today = '''
<div style="padding:.8rem 1rem .4rem"><p class="small muted" style="margin:0">Sat, Oct 31</p><h2 style="font-size:1.3rem">Today</h2>
<p class="small" style="margin:.3rem 0 0">4 replies owed · 9 to review · 1 to send</p></div>
<h3 class="small muted" style="padding:.5rem 1rem .2rem">REPLIES YOU OWE</h3>
<ul class="list">
<li class="row" style="grid-template-columns:auto 1fr"><span class="avatar">RO</span><div><span class="who">Rafael Ortiz</span><div class="line">Pick brushed brass or matte black by Monday?</div></div></li>
<li class="row" style="grid-template-columns:auto 1fr"><span class="avatar">PR</span><div><span class="who">Priya Raman</span><div class="line">Could you look at section 3 before Tuesday?</div></div></li>
<li class="row" style="grid-template-columns:auto 1fr"><span class="avatar">DW</span><div><span class="who">Dana Whitfield</span><div class="line">Can you take the 8:15 run this week?</div></div></li>
</ul>
<h3 class="small muted" style="padding:.7rem 1rem .2rem">ARRIVED, BY SUBJECT</h3>
<ul class="list">
<li class="row" style="grid-template-columns:1fr auto"><div><span class="who">Receipts · 5</span><div class="line">$412.20 extracted</div></div><button class="btn">Archive</button></li>
<li class="row" style="grid-template-columns:1fr auto"><div><span class="who">Newsletters · 41</span><div class="line">all low or ignorable</div></div><button class="btn">Archive</button></li>
</ul>'''

review = '''
<div style="padding:.8rem 1rem .4rem"><p class="small muted" style="margin:0">1 of 9</p><h2 style="font-size:1.2rem">Re: Cabinet hardware</h2>
<p class="small" style="margin:.2rem 0 0"><span class="chip agent">written by agent</span> to Rafael Ortiz</p></div>
<div style="padding:.4rem 1rem">
<fieldset style="border:1px dashed var(--warn);border-radius:var(--radius);padding:.5rem .7rem;background:var(--warn-tint)"><legend style="color:var(--warn);font-weight:600">Which finish?</legend>
<label style="display:block;padding:.35rem 0"><input type="radio" name="m"> Brushed brass</label><label style="display:block;padding:.35rem 0"><input type="radio" name="m" checked> Matte black</label></fieldset>
<div class="card" style="padding:.6rem .7rem;margin-top:.6rem;font-size:.9rem"><p>Thanks Rafael — let's go with matte black.</p><p>Monday delivery works; I'll be home from 10.</p></div>
</div>
<div style="display:grid;grid-template-columns:1fr 1fr;gap:.5rem;padding:.6rem 1rem;margin-top:auto">
<button class="btn" style="justify-content:center;min-height:2.75rem">Edit</button><button class="btn primary" style="justify-content:center;min-height:2.75rem">Send</button>
<button class="btn quiet" style="justify-content:center;min-height:2.75rem">Return to drafts</button><button class="btn quiet" style="justify-content:center;min-height:2.75rem">Skip</button></div>'''

subject = '''
<div style="padding:.8rem 1rem .4rem;display:flex;gap:.6rem;align-items:center"><a href="#" aria-label="Back to Today" class="btn quiet">←</a><span class="avatar">RO</span><div><h2 style="font-size:1.15rem">Rafael Ortiz</h2><p class="small muted" style="margin:0">Ortiz Renovations</p></div></div>
<div style="display:flex;gap:.3rem;padding:.2rem 1rem .5rem;overflow-x:auto"><span class="chip accent">All</span><span class="chip">Mail</span><span class="chip">Files</span><span class="chip">Cards</span><span class="chip">Money</span></div>
<h3 class="small muted" style="padding:.3rem 1rem .2rem">OPEN LOOPS</h3>
<ul class="list"><li class="row" style="grid-template-columns:1fr"><div><span class="chip warn">on you</span> <span class="who">Cabinet hardware</span><div class="line">by Monday</div></div></li>
<li class="row" style="grid-template-columns:1fr"><div><span class="chip">on Rafael</span> <span class="who">Revised quote</span><div class="line">asked Oct 26</div></div></li></ul>
<h3 class="small muted" style="padding:.6rem 1rem .2rem">EVERYTHING</h3>
<ul class="list"><li class="row" style="grid-template-columns:1fr"><div><span class="who">📅 Cabinet delivery</span> <span class="chip agent">proposed</span><div class="line">Mon Nov 2, 10:30</div></div></li>
<li class="row" style="grid-template-columns:1fr"><div><span class="who">📎 Quote-v2.pdf</span><div class="line">$18,450 extracted</div></div></li></ul>'''

body += '''
<main id="main" class="main" style="max-width:80rem;margin:0 auto" aria-labelledby="page-title">
  <h1 id="page-title">On a phone</h1>
  <p class="muted">Below 720px the three parts stack and the navigation becomes a bottom bar of four places. These are drawings; every desktop page above also reflows to this width on its own.</p>
  <div style="display:flex;flex-wrap:wrap;gap:2rem;justify-content:center;margin:1.5rem 0">
''' + phone('Today', 'ph1', today, 'Today') + phone('Review, one at a time', 'ph2', review, 'Review') + phone('A subject', 'ph3', subject, 'Subjects') + '''
  </div>
  <section class="notes" aria-labelledby="notes-m">
    <h2 id="notes-m">About these screens</h2>
    <ul>
      <li><strong>Four places, not ten.</strong> Today, Review, Ask and Subjects. Everything else is reached from a subject or by searching.</li>
      <li><strong>Review is one item at a time</strong> on a phone, with large buttons (at least 44 px) and the decision first. Swiping is not required for anything; every swipe has a button.</li>
      <li><strong>Reading and triage, not configuration.</strong> Settings, annotators and the work-loop metrics are desktop screens; on a phone they show read-only.</li>
      <li><strong>Decided:</strong> the phone is a real target — InboxQL installed as an app from Chrome and reached over the person’s Tailscale network. How sign-in, the secure address and notifications work is still being designed.</li>
    </ul>
    <p><a href="workflows.html#w1">W1 Morning pass</a> works end to end on these three screens.</p>
  </section>
</main>
'''
body += foot()
write('mobile.html', body)
