from chrome import *

GUIDES = [
    ('getting-started', 'Getting started', 'The first hour: accounts, the first sync, Today.'),
    ('reading', 'Reading and replying', 'Conversations, the evidence panel, drafts and sending.'),
    ('query-language', 'The query language', 'Everything you can type in the query bar.'),
    ('labels', 'Labels', 'Gmail-style labels, and labels the agent gives.'),
    ('contacts', 'People', 'Contacts, open loops and who is waiting on whom.'),
    ('files', 'Files', 'Files from mail, versions, and reading inside them.'),
    ('calendar', 'Calendar', 'Events, invitations and proposals.'),
    ('tickets', 'Board', 'Cards, lists, rules, and cards raised from mail.'),
    ('accounts-and-import', 'Accounts and import', 'Adding accounts; bringing in Apple Mail and .eml.'),
    ('privacy', 'Privacy', 'What leaves this computer, and when.'),
    ('settings', 'Settings', 'Every setting, by section.'),
    ('installing', 'Installing and updating', 'The service, updates, the phone.'),
    ('command-line', 'The command line', 'iql for people; AGENTS.md for agents.'),
    ('troubleshooting', 'Troubleshooting', 'When something is wrong, and how to tell what.'),
]


def search_box(val=''):
    return (f'<form role="search" onsubmit="return false" style="display:flex;gap:.5rem;margin:.5rem 0 1rem">'
            f'<label for="hq" class="sr-only">Search help</label><input id="hq" class="btn" style="flex:1;font-size:1rem;padding:.5rem .7rem" placeholder="Search the guides, or ask how to do something" value="{val}">'
            f'<button class="btn primary">Search</button></form>')


# ================================================================== Help home
glist = ''.join(f'<li class="row" style="grid-template-columns:minmax(0,1fr)"><div><a class="who" href="help-guide.html">{t}</a><div class="line">{d}</div></div></li>'
                for _, t, d in GUIDES)
main = f'''
  <div class="page-head"><div class="grow"><h1 id="page-title">Help</h1>
    <p class="muted" style="margin:.3rem 0 0">{C(1)} Every guide is built into InboxQL and matches the version you are running, 2026.10.31.2. Nothing here is fetched from the web.</p></div></div>
  {search_box()}
  <section class="section" aria-labelledby="h-here">
    <header><h2 id="h-here" class="grow">Help where you are {C(2)}</h2></header>
    <p class="card" style="padding:.7rem 1rem;margin:0">Press <kbd>?</kbd> on any screen for what it is for, its sections, and its keys — <a href="help-here.html">see it on Today</a>. Each setting and each agent conclusion links to the paragraph that explains it.</p>
  </section>
  <section class="section" aria-labelledby="h-new">
    <header><h2 id="h-new" class="grow">What’s new in 2026.10.31.2 {C(3)}</h2><a class="small" href="#">All release notes</a></header>
    <ul class="card small" style="padding:.7rem 1rem .7rem 2rem;margin:0">
      <li><strong>Selected rows</strong> use a colour measured against every theme, so the selection is visible in Dark.</li>
      <li>An <strong>indigo Light theme</strong>, a clearer <strong>unread mark</strong>, and no text smaller than 11 px.</li>
      <li>One toolbar row fewer on the Desk.</li></ul>
  </section>
  <section class="section" aria-labelledby="h-guides">
    <header><h2 id="h-guides" class="grow">Guides</h2><span class="small muted">{len(GUIDES)} guides · start with Getting started</span></header>
    <ul class="list card" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(17rem,1fr))">{glist}</ul>
  </section>
  <section class="section" aria-labelledby="h-ref">
    <header><h2 id="h-ref" class="grow">Reference</h2></header>
    <ul class="small" style="padding-left:1.1rem"><li><a href="help-here.html">Keyboard shortcuts</a> — every key, on one page, and the ones you have changed</li>
      <li><a href="#">For agents (AGENTS.md)</a> — the contract an agent driving <code>iql</code> follows; the same text as <code>iql help agents</code></li>
      <li><a href="#">Workflows</a> — step by step, with what a screen reader hears</li></ul>
  </section>
'''
main += notes('n-help',
    'One place for every guide, the release notes and the keyboard reference, all built into the binary — so help is always about the version you have, works offline, and is the same text <code>iql help</code> prints.',
    ['Says the version the guides describe, and that nothing is fetched.',
     '<strong>Help where you are</strong> is the main way in: <kbd>?</kbd> on any screen opens that screen’s own help (the same content these mockups show under each screen), and Settings rows, agent conclusions and error messages link to their paragraph.',
     'What’s new shows the release you are running. After an update, Today shows one line — “Updated to 2026.11.1.1 · What’s new” — once, and dismissing it does not hide the notes here.'],
    ['Help → Documentation today is a list of Markdown files in a viewer tab; it becomes a page in the three-part layout.',
     'Search covers guides, release notes, keyboard keys and settings together, and offers Ask in words for “how do I…” questions.'],
    [('<kbd>?</kbd>', 'Help for this screen'), ('<kbd>g h</kbd>', 'Go to Help'), ('<kbd>/</kbd> in Help', 'Search help')],
    'Guides are a list of links with one-line descriptions. The search is a search landmark labelled “Search help”.',
    '')
aside = '''<aside class="evidence" aria-labelledby="ev-h"><h2 id="ev-h">Example: asking how</h2>
  <p class="small muted">Searching “hide spam from the side” finds two paragraphs, and offers Ask in words:</p>
  <ul class="small" style="padding-left:1.1rem"><li><a href="#">Settings › Subjects in the navigation</a></li><li><a href="#">Labels › Mailbox rows are hidden, never deleted</a></li></ul>
  <div class="card small" style="padding:.6rem .7rem;margin-top:.5rem"><p style="margin:0 0 .3rem"><span class="chip agent">Claude Code</span></p>
    <p style="margin:0">Open <strong>Settings › Reading and appearance › Subjects in the navigation</strong> and switch off Spam. It is hidden, not deleted; Reset brings it back.</p>
    <p class="muted" style="margin:.3rem 0 0">From: Settings guide §4, Labels guide §6 · <button class="btn" style="padding:.1rem .5rem">Do it for me</button></p></div>
  <p class="small muted" style="margin-top:.5rem">“Do it for me” opens the setting with focus on it; it does not change it.</p>
</aside>'''
page('help.html', 'Help', 'Built-in guides, release notes and keyboard reference, with help on every screen.', 'help:', 'Help', main, aside)

# ================================================================== A guide, with live examples
def ex(q, n, note=''):
    count = f'<span class="chip good">{n} in your mail</span>' if n else '<span class="chip">nothing in your mail</span>'
    return (f'<figure class="card" style="margin:.6rem 0;padding:.5rem .75rem;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:.3rem .75rem;align-items:center">'
            f'<code style="font-size:.9rem;overflow-wrap:anywhere">{q}</code><span style="display:flex;gap:.4rem;align-items:center">{count}<button class="btn" aria-label="Run {q} on your mail">Run</button><button class="btn quiet" aria-label="Copy {q}">Copy</button></span>'
            + (f'<figcaption class="small muted" style="grid-column:1/3">{note}</figcaption>' if note else '') + '</figure>')


main = f'''
  <p class="crumbs"><a href="help.html">Help</a> › Guides</p>
  <article aria-labelledby="page-title">
  <h1 id="page-title">The query language</h1>
  <p class="small muted">For 2026.10.31.2 · about 8 minutes · <a href="#">open as plain text</a></p>
  <p>Everything InboxQL shows you is the answer to a query, and every query is something you can type. This page is the whole language. It is short, because most of its power comes from a few pieces that combine.</p>
  <p>A query is a <strong>filter</strong>, optionally followed by <strong>stages</strong> after a <code>|</code>:</p>
  {ex('from:*@stripe.com after:2026-01-01 has:attachment', 42)}
  {ex('is:unread -from:*@acme.com | count by week', 0, f'{C(1)} Nothing here matches <code>*@acme.com</code>, so the example is shown with your most frequent sender instead: <a href="#">is:unread -from:*@hardwaredepot.example | count by week</a>.')}

  <h2 id="match" style="margin-top:1.5rem">Three ways to match</h2>
  {ex('from:ortiz', 38, 'contains “ortiz” — also matches notortiz@x.com')}
  {ex('from:=rafael@ortizreno.example', 36, 'exactly this address')}
  {ex('from:*@ortizreno.example', 38, f'{C(2)} a pattern; * stands for any run of characters. Examples use your own correspondents where the guide’s would match nothing.')}
  <p>They are different questions, so the language keeps them apart instead of guessing which you meant.</p>

  <h2 id="leave" style="margin-top:1.5rem">Leaving things out</h2>
  <p><code>-</code> or <code>NOT</code> in front of any term or group leaves out what it matches. On recipients, <code>-to:bob</code> means <strong>no recipient is Bob</strong> — not “some recipient is not Bob”, which would match nearly every message with more than one recipient.</p>
  {ex('from:*@ortizreno.example -to:jordan@example.net', 12)}
  <aside class="callout-box card" style="padding:.6rem .85rem;border-left:3px solid var(--accent);margin:.8rem 0" aria-label="Note">
    <p class="small" style="margin:0">{C(3)} <strong>Try it here:</strong> <kbd>Run</kbd> opens the results in Ask beside this guide; the guide stays open in the right-hand panel, at this paragraph, until you close it.</p></aside>
  </article>
'''
main += notes('n-guide',
    'Reading a guide. The text is the same Markdown as <code>docs/query-language.md</code>, but examples are live: each runs on your own mail.',
    ['Each example says how many messages it finds in <em>your</em> mail. An example that finds nothing says so and offers a version that does, built from your own senders — a guide that only ever shows “0” teaches that the feature does not work.',
     'Example counts are computed when the guide opens, locally; nothing is sent anywhere.',
     'Run keeps the guide open beside the results, in the evidence panel, at the paragraph you were reading — so you can read, try, and read on.'],
    ['Today the guides render as static Markdown in a tab; examples are text to copy.'],
    [('<kbd>r</kbd> on an example', 'Run it'), ('<kbd>c</kbd>', 'Copy it'), ('<kbd>[</kbd> <kbd>]</kbd>', 'Previous · next section')],
    'The guide is an <code>article</code> with one level-1 heading and level-2 sections. Each Run and Copy button is named with its query (“Run from:ortiz on your mail”), and the count is read as text after the query.',
    '<a href="workflows.html#w7">W7 Ask a question of the mail</a>')
aside = '''<aside class="evidence" aria-labelledby="ev-g"><h2 id="ev-g">On this page</h2>
  <nav aria-label="On this page"><ol class="small" style="padding-left:1.1rem;display:grid;gap:.2rem">
    <li><a href="#page-title">Introduction</a></li><li><a href="#">Filters</a></li><li><a href="#match" aria-current="location"><strong>Three ways to match</strong></a></li>
    <li><a href="#leave">Leaving things out</a></li><li><a href="#">What a query is about</a></li><li><a href="#">Calendar events</a></li><li><a href="#">Stages</a></li>
    <li><a href="#">Examples worth stealing</a></li><li><a href="#">When a query is wrong</a></li></ol></nav>
  <h3 style="margin-top:1rem">Related</h3><ul class="small" style="padding-left:1.1rem"><li><a href="#">Labels</a></li><li><a href="query.html">Ask — the query bar</a></li><li><a href="ask.html">Ask in words</a></li></ul>
  <h3 style="margin-top:1rem">Was this clear?</h3><p class="small" style="margin:0 0 .4rem">Saved on this computer, with the paragraph you were reading, for whoever improves the guide.</p>
  <div style="display:flex;gap:.4rem"><button class="btn">Yes</button><button class="btn">Not quite…</button></div>
</aside>'''
page('help-guide.html', 'The query language · Help', 'A built-in guide with live examples that run on your own mail.', 'help:query-language', 'Help', main, aside)

# ================================================================== ? on a screen
body = head('Help on Today', 'Pressing ? on a screen: what the screen is for, its sections, and its keys.')
body += banner('help-here.html')
body += appbar('needs:me')
body += ('<div style="position:relative;height:92vh;overflow:hidden;border-bottom:2px dashed var(--border)"><div class="shell two" aria-hidden="true" inert>\n'
         + nav('Today') + '<main class="main"><h1>Today</h1><p class="muted">(Today, behind the help.)</p></main></div>')
body += f'''
<div class="scrim" style="position:absolute;place-items:start end;padding:1rem">
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="hh-title" style="width:min(30rem,calc(100vw - 2rem));max-height:calc(92vh - 2rem);overflow:auto">
    <div style="display:flex;align-items:center;gap:.5rem;padding:.7rem 1rem;border-bottom:1px solid var(--border)">
      <h1 id="hh-title" style="font-size:1.1rem;flex:1">Help: Today</h1><button class="btn quiet" aria-label="Close help">Esc</button></div>
    <div style="padding:.8rem 1rem">
      <p style="margin-top:0">{C(1)} Today shows what needs you before what arrived. Every section is a query you can open, save or hand to an agent.</p>
      <h2 style="font-size:.95rem;margin-top:.8rem">The sections {C(2)}</h2>
      <dl class="kv" style="margin:.3rem 0">
        <dt><a href="today.html#replies">Replies you owe</a></dt><dd>Conversations the agent judged waiting on you. <kbd>n</kbd> if one is not.</dd>
        <dt>To review</dt><dd>Proposals and checks, counted; <kbd>g r</kbd> to work through them.</dd>
        <dt>Arrived</dt><dd>New mail grouped by subject; archive a group at once, with Undo.</dd>
        <dt>Ahead</dt><dd>Important dates counted down, and the weeks and months ahead.</dd></dl>
      <h2 style="font-size:.95rem;margin-top:.8rem">Keys on this screen {C(3)}</h2>
      <table class="doc small"><tr><th scope="col">Key</th><th scope="col">Does</th></tr>
        <tr><td><kbd>↑</kbd> <kbd>↓</kbd></td><td>Move in a section; <kbd>Tab</kbd> to the next section</td></tr>
        <tr><td><kbd>r</kbd> <kbd>↵</kbd> <kbd>f</kbd></td><td>Reply · open · everything with this person</td></tr>
        <tr><td><kbd>n</kbd></td><td>Not waiting on me (teaches the agent)</td></tr>
        <tr><td><kbd>g a</kbd> <kbd>g r</kbd></td><td>Go to Ahead · To review</td></tr>
        <tr><td><kbd>z</kbd></td><td>Undo the last change</td></tr></table>
      <p class="small muted" style="margin:.3rem 0 0">Shown as you have them; a key you changed shows your key. <a href="settings-reading.html">Change keys</a></p>
      <h2 style="font-size:.95rem;margin-top:.8rem">Go further</h2>
      <ul class="small" style="padding-left:1.1rem;margin:.3rem 0"><li><a href="help-guide.html">Getting started › Today</a></li><li><a href="workflows.html#w1">The morning pass, step by step</a></li><li><a href="help.html">All help</a> <kbd>g h</kbd></li></ul>
    </div>
  </div>
</div></div>
<main id="main" class="main" style="max-width:68rem;margin:0 auto" aria-labelledby="notes-hh-t">
  <h1 id="notes-hh-t" class="sr-only">About help on a screen</h1>
''' + notes('n-hh',
    '<kbd>?</kbd> on any screen opens its own help in a panel at the side, without leaving the screen: what it is for, what each section is, and the keys that work here — as you have them, including keys you changed.',
    ['One sentence: what the screen is for.',
     'Each section named as it appears on the screen, with a link that moves focus to it — so the help also works as a map.',
     'Only the keys for this screen, not all of them. Changed keys are shown as changed.'],
    ['Today the shortcuts are listed in one dialog for the whole app; there is no help per screen.'],
    [('<kbd>?</kbd>', 'Open or close help for this screen'), ('<kbd>Esc</kbd>', 'Close, and return focus where it was')],
    'A modal dialog labelled “Help: Today”; focus starts on its heading and returns to where it was when it closes. The section list is a description list so each name and its explanation are read together.',
    '') + '</main>\n' + foot()
write('help-here.html', body)
