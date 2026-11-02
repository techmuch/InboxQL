from chrome import *

ADDR = 'inboxql.tail1a2b3c.ts.net'
PHONE = 'width:100%;border:10px solid var(--text);border-radius:2rem;overflow:hidden;background:var(--bg);display:flex;flex-direction:column;height:42rem'


def phone(n, title, inner, chrome_bar=True, dark_shade=False):
    bar = (f'<div style="display:flex;gap:.4rem;align-items:center;padding:.45rem .7rem;background:var(--surface-2);font-size:.8rem">'
           f'<span aria-hidden="true">🔒</span><span style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{ADDR}</span><span aria-hidden="true">⋮</span></div>') if chrome_bar else ''
    return (f'<figure style="margin:0;flex:0 1 19rem;min-width:0;display:flex;flex-direction:column;align-items:center;gap:.6rem" aria-labelledby="pc{n}">'
            f'<div style="{PHONE}" role="group" aria-label="Phone, step {n}: {title}">{bar}<div style="flex:1;display:flex;flex-direction:column;overflow:hidden;position:relative">{inner}</div></div>'
            f'<figcaption id="pc{n}" class="small" style="text-align:center"><span class="callout" aria-hidden="true">{n}</span> <strong>{title}</strong></figcaption></figure>')


s1 = f'''<div style="padding:1.2rem 1rem;display:grid;gap:.8rem">
  <p style="font-weight:700;font-size:1.2rem;margin:0">InboxQL</p>
  <p style="margin:0">Tailscale says this is <strong>alex@example.com</strong> on <strong>Pixel 8</strong>.</p>
  <p class="small" style="margin:0">To use your mailbox on this phone, allow it on your Mac. Check the code matches:</p>
  <p style="font-size:2rem;font-weight:700;letter-spacing:.3em;text-align:center;margin:.2rem 0;font-variant-numeric:tabular-nums" aria-label="Code 4 7 1 9">4719</p>
  <p class="small muted" style="margin:0">Waiting for your Mac… <span aria-hidden="true">◌</span></p>
  <p class="small muted" style="margin:0">Only devices on your Tailscale network can open this address.</p></div>'''

s2 = f'''<div style="padding:1rem;opacity:.45" aria-hidden="true"><div style="height:1.2rem;width:5rem;border-radius:4px;background:var(--text-2)"></div><div style="height:.7rem;width:11rem;border-radius:4px;background:var(--muted);margin:.5rem 0 .8rem"></div>
  <div class="card" style="height:5rem"></div><div class="card" style="height:5rem;margin-top:.5rem"></div></div>
  <div style="position:absolute;left:0;right:0;bottom:0;background:var(--bg);border-top:1px solid var(--border);border-radius:1rem 1rem 0 0;padding:1rem;box-shadow:var(--shadow);display:grid;gap:.6rem">
    <div style="display:flex;gap:.7rem;align-items:center"><span class="avatar" style="background:var(--accent);color:var(--on-accent)" aria-hidden="true">iQ</span>
      <div><strong>Install app</strong><div class="small muted">InboxQL · {ADDR}</div></div></div>
    <p class="small" style="margin:0">Opens in its own window, with its own icon, and can send you reminders.</p>
    <div style="display:flex;gap:.5rem;justify-content:flex-end"><button class="btn quiet" style="min-height:2.75rem">Not now</button><button class="btn primary" style="min-height:2.75rem">Install</button></div></div>'''

s3 = '''<div style="background:#1f2430;color:#eef0f6;flex:1;padding:.8rem .7rem;display:grid;align-content:start;gap:.55rem">
  <p style="margin:0;font-size:2rem;font-weight:300">7:30</p><p class="small" style="margin:-.4rem 0 .4rem;color:#c3c7d4">Thursday, November 5</p>
  <div style="background:#2c3242;border-radius:1rem;padding:.7rem .8rem"><div style="display:flex;gap:.4rem;align-items:center;font-size:.8rem;color:#c3c7d4"><span aria-hidden="true">iQ</span> InboxQL · now</div>
    <p style="margin:.3rem 0 0;font-weight:600">Q4 report to the board — in 5 days</p><p class="small" style="margin:.1rem 0 0;color:#dfe2ea">Due Tue, Nov 10. Your comments on section 3 are still owed.</p>
    <div style="display:flex;gap:1rem;margin-top:.5rem;font-size:.85rem;font-weight:600;color:#c7d2fe"><span>Open</span><span>Remind me tomorrow</span></div></div>
  <div style="background:#2c3242;border-radius:1rem;padding:.7rem .8rem"><div style="display:flex;gap:.4rem;align-items:center;font-size:.8rem;color:#c3c7d4"><span aria-hidden="true">iQ</span> InboxQL · 7:00</div>
    <p style="margin:.3rem 0 0;font-weight:600">Grandma Ruth’s birthday is in 2 weeks</p><p class="small" style="margin:.1rem 0 0;color:#dfe2ea">Thu, Nov 19 · she turns 84</p></div></div>'''

s4 = '''<div style="padding:.8rem 1rem .4rem;display:flex;gap:.6rem;align-items:center"><a href="#" aria-label="Back" class="btn quiet">←</a><div><h3 style="font-size:1.1rem">Q4 report</h3><p class="small muted" style="margin:0">★ due Tue, Nov 10 · in 5 days</p></div></div>
<div style="padding:.2rem 1rem"><p class="small" style="margin:.2rem 0 .6rem"><strong>Next:</strong> your comments on section 3, owed to Priya since Oct 30.</p></div>
<ul class="list" style="border-top:1px solid var(--border)">
<li class="row" style="grid-template-columns:1fr"><div><span class="chip warn">on you</span> <span class="who">Comments on section 3</span><div class="line">“Could you look at section 3…”</div></div></li>
<li class="row" style="grid-template-columns:1fr"><div><span class="who">📄 Q4-report-v3.docx</span><div class="line">Priya · Oct 30 · latest</div></div></li></ul>
<div style="display:grid;grid-template-columns:1fr 1fr;gap:.5rem;padding:.8rem 1rem;margin-top:auto"><button class="btn primary" style="justify-content:center;min-height:2.75rem">Reply to Priya</button><button class="btn" style="justify-content:center;min-height:2.75rem">Open v3</button></div>
<nav aria-label="Places (step 4)" style="display:grid;grid-template-columns:repeat(4,1fr);border-top:1px solid var(--border);background:var(--surface);font-size:.8rem;text-align:center">
<a href="#" style="padding:.6rem 0;color:var(--muted);text-decoration:none">Today</a><a href="#" style="padding:.6rem 0;color:var(--muted);text-decoration:none">Review</a><a href="#" style="padding:.6rem 0;color:var(--muted);text-decoration:none">Ask</a><a href="#" aria-current="page" style="padding:.6rem 0;color:var(--accent-text);font-weight:600;text-decoration:none">Subjects</a></nav>'''

mac = f'''<div class="card" style="padding:1rem 1.2rem;max-width:34rem" role="group" aria-labelledby="mac-h">
  <p class="small muted" style="margin:0">On your Mac — at the top of Today, and in Settings → This computer</p>
  <h3 id="mac-h" style="margin:.3rem 0">A phone wants to use your mailbox</h3>
  <p class="small" style="margin:0 0 .5rem"><strong>Pixel 8</strong>, signed in to Tailscale as <strong>alex@example.com</strong>. Allow it only if it shows the code <strong style="font-size:1.1rem;letter-spacing:.15em">4719</strong>.</p>
  <div style="display:flex;gap:.5rem;flex-wrap:wrap"><button class="btn primary">Allow this phone</button><button class="btn">Not mine — refuse</button></div></div>'''

body = head('Phone app', 'InboxQL on a phone: installed from Chrome, reached over Tailscale, allowed once from the Mac, and sending reminders for important dates.')
body += banner('phone-app.html')
body += f'''
<main id="main" class="main" style="max-width:84rem;margin:0 auto" aria-labelledby="page-title">
  <h1 id="page-title">InboxQL on your phone</h1>
  <p class="muted">The Mac keeps the mailbox; the phone is a window onto it. It is reached over your Tailscale network at a private HTTPS address, installed from Chrome as an app, and allowed once from the Mac. Nothing about your mail is stored on a server anywhere else.</p>

  <section class="section" aria-labelledby="h-steps">
    <h2 id="h-steps">Setting it up, and a reminder arriving</h2>
    <div style="display:flex;flex-wrap:wrap;gap:1.5rem;justify-content:center;margin:1rem 0">
      {phone(1, 'Open the address — allow it on your Mac', s1)}
      {phone(2, 'Chrome offers to install it', s2)}
      {phone(3, 'A reminder arrives', s3, chrome_bar=False)}
      {phone(4, 'Tapping it opens the subject', s4, chrome_bar=False)}
    </div>
    <h3 style="margin-top:1.5rem">Step 1, on the Mac</h3>
    {mac}
  </section>

  <section class="notes" aria-labelledby="n-phone">
    <h2 id="n-phone">About this screen</h2>
    <p><strong>Purpose.</strong> The phone is a real target (decided Nov 1). This page shows how it gets there and what the person sees, the way herdr is reached through ovis today: a web app on a private network, installed from Chrome so it looks and behaves like a native app.</p>
    <h3>Steps</h3>
    <ol>
      <li><span class="callout">1</span> <strong>The address</strong> is an HTTPS name on your Tailscale network (Settings → This computer → Phone shows it, with a QR code). Only devices on your network can reach it. Tailscale tells InboxQL who is asking, so there is no password to type — but a new device is still <strong>allowed once from the Mac</strong>, with a matching code, because the phone can do anything you can. Each allowed phone is listed in Settings and can be signed out.</li>
      <li><span class="callout">2</span> <strong>Install.</strong> InboxQL is an installable web app: Chrome offers “Install app”, and it then opens in its own window with its own icon. Nothing comes from an app store.</li>
      <li><span class="callout">3</span> <strong>Reminders</strong> for important dates arrive as ordinary Android notifications — when a lead time starts, a week before, the day before, that morning — with Open and Remind me tomorrow. To reach a phone whose app is closed, a notification goes through Google’s push service, encrypted so Google cannot read it. It carries only the reminder line, <strong>never mail</strong>; the app fetches anything more from your Mac when you open it.</li>
      <li><span class="callout">4</span> Tapping a reminder opens the subject it is about, already at the next step, with the four-place bottom bar.</li>
    </ol>
    <h3>When the Mac cannot be reached</h3>
    <p>Asleep, off, or off the network: the app shows Today as it was last seen, labelled “as of 7:12 AM — your Mac is not reachable”. Reading what was already loaded works; actions are turned off and say why, rather than queuing changes that might go out later without you seeing them.</p>
    <h3>Not decided yet</h3>
    <ul>
      <li>How InboxQL is published on the tailnet — copying how ovis serves herdr is the likely route.</li>
      <li>Whether Ask in words runs from the phone (it would run on the Mac; the phone only sees the answer).</li>
      <li>Reminders to a phone need the Mac awake at the time; a reminder missed while it slept arrives when it wakes, marked late.</li>
    </ul>
    <h3>Screen reader</h3>
    <p>Each drawing is a group named for its step; the code is read digit by digit. The installed app uses the same pages as the desktop at phone width, so everything on the <a href="mobile.html">Phone</a> page applies.</p>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w12">W12 Put InboxQL on your phone</a></p>
  </section>
</main>
'''
body += foot()
write('phone-app.html', body)
