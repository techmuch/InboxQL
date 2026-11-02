from chrome import *

SECTIONS = [
    ('settings.html', 'Overview', ''),
    ('settings-accounts.html', 'You and your accounts', ''),
    ('settings-agents.html', 'Agents and AI', ''),
    ('settings-reading.html', 'Reading and appearance', ''),
    ('settings-data.html', 'Your data', '1'),
    ('settings-computer.html', 'This computer', '1'),
]


def setnav(cur):
    items = ''.join(
        f'<li><a href="{h}"{" aria-current=\"page\"" if h == cur else ""}>{n}'
        + (f'<span class="chip warn" title="needs attention">{b}<span class="sr-only"> needs attention</span></span>' if b else '') + '</a></li>'
        for h, n, b in SECTIONS)
    return (f'<nav class="setnav" aria-label="Settings sections"><form role="search" onsubmit="return false">'
            f'<label for="setq" class="sr-only">Find a setting</label><input id="setq" placeholder="Find a setting…  ⌘F"></form><ul>{items}</ul></nav>')


def sw(label_id, on=True, disabled=False):
    return f'<button type="button" class="switch" role="switch" aria-checked="{"true" if on else "false"}" aria-labelledby="{label_id}"{" disabled" if disabled else ""}></button>'


def seg(name, label_id, opts, checked):
    return (f'<fieldset class="seg" aria-labelledby="{label_id}">' + ''.join(
        f'<label><input type="radio" name="{name}"{" checked" if o == checked else ""}> {o}</label>' for o in opts) + '</fieldset>')


def S(sid, label, desc, ctl, cls='', changed=False):
    ch = '<span class="changed">changed <a href="#" class="small">reset</a></span>' if changed else ''
    return (f'<div class="set {cls}" id="setting-{sid}"><div><div class="lbl" id="l-{sid}">{label}{ch}</div>'
            f'<p class="desc" id="d-{sid}">{desc}</p></div><div class="ctl">{ctl}</div></div>')


def group(gid, title, inner, intro=''):
    i = f'<p class="small muted" style="margin:-.25rem 0 .5rem">{intro}</p>' if intro else ''
    return f'<section class="setgroup" aria-labelledby="g-{gid}"><h2 id="g-{gid}">{title}</h2>{i}<div class="card">{inner}</div></section>'


def cli(cmds, apply_, doc):
    lines = ''.join(f'<li><code>{c}</code><br><span class="small muted">{d}</span></li>' for c, d in cmds)
    return f'''<aside class="evidence" aria-labelledby="ev-s"><h2 id="ev-s">From the command line</h2>
  <p class="small muted">Everything on this page can also be done with <code>iql</code>, so a script or an agent can read it — and the change shows here.</p>
  <ul class="small" style="list-style:none;padding:0;display:grid;gap:.55rem">{lines}</ul>
  <h3 style="margin-top:1rem">When changes apply</h3><p class="small">{apply_}</p>
  <h3 style="margin-top:1rem">Help</h3><p class="small"><a href="#">{doc}</a></p></aside>'''


def spage(fname, title, desc, query, content, aside, notes_html):
    main = f'''<div class="page-head"><div class="grow"><h1 id="page-title">{title}</h1></div></div>
<div class="settings">{setnav(fname)}<div>{content}{notes_html}</div></div>'''
    page(fname, title + ' · Settings' if fname != 'settings.html' else 'Settings', desc, query, 'Settings', main, aside)


# ======================================================================= Overview
cards = [
    ('settings-accounts.html', 'You and your accounts', 'Alex Kim · 2 mail accounts, both live · Google Calendar connected', ''),
    ('settings-agents.html', 'Agents and AI', 'Claude Code answers questions · 8 elements on · local models installed', ''),
    ('settings-reading.html', 'Reading and appearance', 'Light theme · text 100% · single-key shortcuts on · reminders on', ''),
    ('settings-data.html', 'Your data', '4.2 GB mailbox · <strong>last backup 9 days ago</strong>', 'warn'),
    ('settings-computer.html', 'This computer', 'Version 2026.10.31.2 · running at login · <strong>an update is ready</strong>', 'warn'),
]
cardhtml = ''.join(
    f'<li class="row" style="grid-template-columns:minmax(0,1fr) auto"><div><a class="who" href="{h}">{t}</a>'
    f'<div class="line" style="white-space:normal">{d}</div></div><span class="meta">{"<span class=\"chip warn\">needs attention</span>" if w else "<span class=\"chip good\">ok</span>"}</span></li>'
    for h, t, d, w in cards)
content = f'''
<p class="muted" style="margin:0 0 1rem">{C(1)} <strong>2 things need you:</strong> a backup is overdue, and an update is ready. Everything else is as you left it.</p>
<section class="setgroup" aria-labelledby="g-attn"><h2 id="g-attn">Needs attention</h2>
<ul class="list card">
  <li class="row" style="grid-template-columns:auto minmax(0,1fr) auto"><span class="chip warn">backup</span><div><span class="who">The weekly backup was missed</span><div class="line" style="white-space:normal">The computer was asleep on Sunday. Last backup Oct 22, 9 days ago. 41 GB free; a backup needs about 4.3 GB.</div></div><span class="meta"><button class="btn primary">Back up now</button></span></li>
  <li class="row" style="grid-template-columns:auto minmax(0,1fr) auto"><span class="chip warn">update</span><div><span class="who">Version 2026.11.1.1 is ready</span><div class="line" style="white-space:normal">Backs up the mailbox, restarts the server (about 10 seconds) and reopens this window. <a href="#">What’s new</a></div></div><span class="meta"><a class="btn" href="settings-computer.html#setting-updates">Review</a></span></li>
</ul></section>
<section class="setgroup" aria-labelledby="g-all"><h2 id="g-all">All settings {C(2)}</h2><ul class="list card">{cardhtml}</ul></section>
<section class="setgroup" aria-labelledby="g-search"><h2 id="g-search">Example: finding a setting {C(3)}</h2>
<div class="card" style="padding:.75rem 1rem"><p class="small muted" style="margin:0 0 .4rem">Typing “dark” in Find a setting:</p>
<ul class="small" style="list-style:none;padding:0;margin:0;display:grid;gap:.3rem">
<li><a href="settings-reading.html#setting-theme"><strong>Theme</strong></a> — Reading and appearance · currently Light</li>
<li><a href="settings-reading.html#setting-theme"><strong>Match the system’s light or dark</strong></a> — Reading and appearance · off</li></ul></div></section>
'''
nt = notes('n-set',
    'Settings is a place in the navigation like any other, with one overview. The overview leads with what needs you and says each section’s state in a line, so most visits end here.',
    ['A sentence of state. A healthy install says so; a problem names itself and the fix.',
     'Six sections instead of eleven, named for what you are doing (accounts, agents, reading, your data, this computer) rather than for the code behind them. Each line is that section’s current state, not a description of it.',
     '<strong>Find a setting</strong> searches labels and keywords, as now, and says where the result lives and its current value. Choosing one opens the section and moves focus to the setting.'],
    ['Profile, Accounts and Calendars → <em>You and your accounts</em>.',
     'AI Configuration, Annotators, Elements and the work-loop targets → <em>Agents and AI</em>.',
     'General (theme, inbox layout, previews, rail) → <em>Reading and appearance</em>, which gains text size, motion and shortcut settings.',
     'Import, Data management, Maintenance, Storage → <em>Your data</em>.',
     'System and Logging, and the Windows tab → <em>This computer</em>.'],
    [('<kbd>⌘,</kbd>', 'Open Settings'), ('<kbd>⌘F</kbd> in Settings', 'Find a setting'), ('<kbd>Esc</kbd>', 'Back to where you were')],
    'Settings has its own navigation landmark (“Settings sections”) after the main Places navigation. A section with something wrong says “needs attention” in its link name, not only with a badge.',
    '')
spage('settings.html', 'Settings', 'The settings overview: what needs attention, then every section with its current state in one line.', 'settings', content,
      cli([('iql --json doctor', 'the same health checks as “Needs attention”'), ('iql --json where', 'which mailbox, and why'), ('iql --json update --check', 'is there a newer release')],
          'Each setting says when it applies. Most apply at once; a few need a restart and say so.', 'Settings guide'), nt)

# ======================================================================= Accounts
acct = lambda name, kind, line, st: (f'<div class="set"><div><div class="lbl">{name} <span class="chip">{kind}</span></div><p class="desc">{line}</p></div>'
                                     f'<div class="ctl">{st}<button class="btn">Edit</button><button class="btn">Test</button></div></div>')
content = group('you', 'You',
    S('display-name', 'Your name', 'Shown on mail you send and on your own messages here.', '<input class="btn" aria-labelledby="l-display-name" value="Alex Kim">')
    + S('profile-email', 'Your addresses', '<code>me()</code> in a query means these. Taken from your accounts.', '<span class="small">you@example.com, you@work.example</span>')
    + S('profile-image', 'Picture', 'Shown beside your messages. Never fetched from anywhere unless you ask.', '<span class="avatar" aria-hidden="true">AK</span><button class="btn">Choose…</button>'))
content += group('mail', f'Mail accounts {C(1)}',
    acct('Personal', 'Gmail', 'you@example.com · live: changes reach Gmail in seconds · 82% of older mail here (38,400 of 46,900)', '<span class="chip good">live</span>')
    + acct('Work', 'IMAP', 'you@work.example · mail.work.example:993 · all mail here', '<span class="chip good">live</span>')
    + '<div class="set wide"><div class="ctl"><button class="btn">+ Add an account</button><span class="small muted">Gmail, iCloud, Fastmail, or any IMAP server</span></div></div>',
    'Passwords are kept encrypted on this computer and are never shown again.')
content += group('cal', 'Calendars',
    S('google-calendar', 'Google Calendar', 'you@example.com · 3 calendars · synced 2 min ago', '<span class="chip good">connected</span><button class="btn">Choose calendars</button>')
    + S('exchange-server', 'Exchange Server', 'Only Exchange Server on your organisation’s own servers. Microsoft 365 cannot be connected.', '<button class="btn">Connect…</button>')
    + S('calendar-invitations', 'Invitations in mail', 'Read invitations that arrive by mail into an Invitations calendar.', sw('l-calendar-invitations', True)))
content += group('send', 'Sending',
    S('undo-send', 'Time to undo a send', 'How long Send waits, with Undo, before mail goes.', seg('undo', 'l-undo-send', ['5 s', '10 s', '20 s', '30 s'], '10 s'))
    + S('signature', 'Signature', 'Added below what you write; per account.', '<button class="btn">Edit signatures</button>')
    + S('agent-mail', f'Mail written by agents {C(2)}', 'Always waits in Drafts and outbox for your Send. Shown here so you know; it is how InboxQL works, not a setting.', '<span class="chip">always waits for you</span>'))
nt = notes('n-set-acc', 'Who you are, which accounts InboxQL reads, and how sending behaves.',
    ['Each account says its state in words — live, how much older mail is here, and where changes go. Test classifies a failure (password refused, host not found, network, certificate) instead of one error.',
     'Some rules are facts, not settings. Agent mail waiting for your Send is shown so it is never a surprise, without a switch that suggests it could quietly be off.'],
    ['Profile, Accounts and Calendars were three sections; they are one page.', 'Undo-send time is new as a setting (it is fixed at 10 seconds today).'],
    [('<kbd>Tab</kbd>', 'Moves through settings in order; every control has a visible label')],
    'Each setting is a label, a description (linked with <code>aria-describedby</code>) and a control. The segmented choices are radio groups in a fieldset named by the setting.')
spage('settings-accounts.html', 'You and your accounts', 'Profile, mail and calendar accounts, and sending.', 'settings:accounts', content,
      cli([('iql --json account list', 'accounts, never with passwords'), ('iql account verify personal', 'test one; says why it failed'), ('iql --json calendar accounts', 'connected calendars')],
          'At once. Changing an account’s server asks for its password again — a password for one server is never sent to another.', 'Accounts and sync guide'), nt)

# ======================================================================= Agents and AI
el = lambda name, what, acc, tgt, on=True: (f'<tr><th scope="row">{name}</th><td>{what}</td><td class="num">{acc}</td><td class="num">{tgt}</td>'
                                            f'<td>{sw("e-" + name, on)}<span class="sr-only" id="e-{name}">{name} on</span></td></tr>')
content = f'<p class="muted" style="margin:0 0 1rem">{C(1)} <strong>Mail leaves this computer only when an agent reads it.</strong> When you ask in words, or run Claude Code yourself, it sends what it reads to Anthropic under your own account. InboxQL itself sends nothing; its two local models run here.</p>'
content += group('ask', 'Ask in words',
    S('ask-cli', 'Which agent answers', 'An agent CLI on this computer, run with the iql skill. Detected: Claude Code (signed in) · agy (not found).', seg('cli', 'l-ask-cli', ['Claude Code', 'agy', 'Off'], 'Claude Code'))
    + S('ask-may', 'What it may do', 'Read only: <code>iql query</code>, <code>read</code>, <code>analyze</code>, and pointing your window with <code>iql ui</code>. Anything else it suggests becomes a button for you.', '<span class="chip">read only</span>')
    + S('ask-history', 'Keep questions and answers', 'In the mailbox, so a past answer can be reopened and its commands seen. 30 days.', sw('l-ask-history', True)))
content += group('provider', 'AI provider',
    S('ai-provider', 'Where summaries and drafts are written', 'None: nothing is summarised or drafted for you, and nothing leaves this computer. On this computer: a local model does it. Remote: the text of a conversation is sent to the provider each time — you are shown where, and asked once, before the first.',
      seg('prov', 'l-ai-provider', ['None', 'On this computer (Ollama)', 'Remote…'], 'None'), 'wide')
    + S('local-models', 'Local models', 'GLiNER extracts values (800 MB) · Laya judges yes/no (1.2 GB). Both run here and send nothing.', '<span class="chip good">installed</span><button class="btn">Manage</button>'))
content += group('assist', 'Assistants',
    S('ai-assistants', 'Assistants that can use iql', 'A short guide installed for each tool, so it follows the rules on sending and changing mail.',
      '<span class="small">Claude Code <span class="chip good">installed</span> · Codex <span class="chip">not installed</span></span><button class="btn">Manage</button>')
    + S('agent-archive', 'Agents may archive and label', 'Every change is in Agent activity with Undo. Trash and spam only when you ask in words.', sw('l-agent-archive', True))
    + S('agent-proposals', 'Agents propose events and cards', 'Proposals wait in To review; nothing is added to your calendar or board until you approve.', sw('l-agent-proposals', True)))
content += f'''<section class="setgroup" aria-labelledby="g-el"><h2 id="g-el">What the agent reads for {C(2)}</h2>
<p class="small muted" style="margin:-.25rem 0 .5rem">Elements are the things worth knowing about each conversation. Accuracy is measured on your checks; below its target an element is no longer guessed.</p>
<div style="overflow-x:auto" tabindex="0" role="region" aria-label="Elements table"><table class="grid"><caption class="sr-only">Elements, with accuracy on your checks and their targets</caption>
<thead><tr><th scope="col">Element</th><th scope="col">Records</th><th scope="col" class="num">Right on your checks</th><th scope="col" class="num">Target</th><th scope="col">On</th></tr></thead><tbody>
{el('waiting', 'who owes a reply', '41 / 44 · 93%', '95%')}
{el('importance', 'important to ignorable', '37 / 44 · 84%', '80%')}
{el('receipts', 'merchant, items, total', '28 / 29 · 97%', '95%')}
{el('travel', 'flights, hotels, cars', '9 / 9', '95%')}
{el('events', 'dates to propose', '12 / 14 · 86%', '90%')}
{el('tasks', 'things to do', '6 / 9 · 67%', '80%')}
{el('topics', 'what it is about', '—', '—')}
{el('sender', 'person or system', '—', '—', False)}
</tbody></table></div>
<p class="small" style="margin-top:.4rem"><a href="#">Edit an element’s instructions</a> · <a href="review.html#checks">3 checks due</a> · <a href="activity.html">Agent activity</a></p></section>'''
nt = notes('n-set-ag', 'What leaves this computer, which agents may act and how, and what they read mail for — with how often they are right.',
    ['The first line answers the privacy question before anything else, in words, and changes the moment a remote provider is chosen.',
     'Elements show accuracy on your own checks next to their target, so a setting is judged by evidence. Turning one off stops new answers; what it already found stays.'],
    ['AI Configuration (three tabs), Annotators, Elements and work-loop targets were four places.', 'Accuracy, now only in the Work loop’s metrics, sits beside the switch it should inform.'],
    [('<kbd>Space</kbd>', 'Toggle a switch')],
    'Switches are <code>role="switch"</code> buttons named by their setting. The elements table has a caption and headers; each switch is named for its element.')
spage('settings-agents.html', 'Agents and AI', 'Where text may go, which agents may act, and what they read for, with accuracy.', 'settings:agents', content,
      cli([('iql --json llm status', 'which provider, local or remote'), ('iql --json elements list', 'elements and what is answered'), ('iql --json work targets', 'accuracy targets and cost'), ('iql skills list', 'assistants with the iql skill')],
          'At once. A remote provider is used only after you confirm what will be sent and where.', 'Agents guide (AGENTS.md)'), nt)

# ======================================================================= Reading and appearance
def swatch(name, side, main_, sel=False):
    return (f'<label class="swatch"><span class="pv" aria-hidden="true"><span style="background:{side}"></span><span style="background:{main_}"></span></span>'
            f'<span><input type="radio" name="theme"{" checked" if sel else ""}> {name}</span></label>')


content = group('look', 'Look',
    '<div class="set wide" id="setting-theme"><div><div class="lbl" id="l-theme">Theme</div><p class="desc">Every theme meets WCAG AA contrast for text and controls.</p></div>'
    f'<fieldset class="swatches" aria-labelledby="l-theme">{swatch("Light", "#f7f7fa", "#ffffff", True)}{swatch("Dark", "#1d1f26", "#16171c")}{swatch("Georgia Tech", "#003057", "#f9f6e5")}{swatch("Texas A&amp;M", "#500000", "#ffffff")}</fieldset></div>'
    + S('match-system', 'Match the system’s light or dark', 'Follow macOS: Light by day, Dark at night.', sw('l-match-system', False))
    + S('text-size', f'Text size {C(1)}', 'Scales all text and controls together; layouts reflow rather than cut off.', seg('ts', 'l-text-size', ['100%', '112%', '125%', '150%'], '100%'))
    + S('density', 'Spacing', 'Compact fits more rows; comfortable is easier to read and to hit.', seg('dn', 'l-density', ['Comfortable', 'Compact'], 'Comfortable'))
    + S('motion', 'Motion', 'Follows your system’s Reduce motion unless you choose here.', seg('mo', 'l-motion', ['Like the system', 'Reduce', 'Full'], 'Like the system')))
content += group('layout', 'Layout',
    S('evidence-panel', f'Show the evidence panel {C(2)}', 'The right-hand column. <kbd>.</kbd> shows or hides it anywhere.', sw('l-evidence-panel', True))
    + S('evidence-width', 'Evidence panel width', 'Drag the panel’s left edge, or focus it and use <kbd>←</kbd> <kbd>→</kbd>. The same width on every screen. Now 352 px, the default.', '<button class="btn">Reset width</button>')
    + S('subjects', 'Subjects in the navigation', 'Pinned people, topics and queries, in your order.', '<button class="btn">Arrange…</button>'))
content += group('read', 'Reading',
    S('conversations-open', 'Conversations open', 'Show the whole conversation, or the newest message with the rest folded.', seg('co', 'l-conversations-open', ['Newest only', 'Whole conversation'], 'Newest only'))
    + S('attachment-previews', 'Attachment previews', 'Open a file in the evidence panel, or in a new browser tab.', seg('ap', 'l-attachment-previews', ['In the panel', 'New tab'], 'In the panel'))
    + S('week-start', 'Weeks start on', 'Used by Ahead and the Calendar.', seg('ws', 'l-week-start', ['Sunday', 'Monday'], 'Monday'), changed=True)
    + S('time-format', 'Times', '', seg('tf', 'l-time-format', ['3:00 PM', '15:00'], '3:00 PM')))
content += group('notify', 'Reminders',
    S('notify-dates', 'Important dates', 'Notify me when a lead time starts, a week before, the day before, and that morning. Each date can change its own.', '<button class="btn">Choose when…</button>')
    + S('notify-where', 'Where reminders appear', 'In this browser and on your phone, once it is installed there.', '<span class="small">This browser · Phone <span class="chip">not installed</span></span>')
    + S('notify-quiet', 'Quiet hours', 'Reminders that fall at night wait until morning.', seg('qh', 'l-notify-quiet', ['Off', '10 PM – 7 AM'], '10 PM – 7 AM'))
    + S('birthdays', 'Birthdays and anniversaries', 'Taken from your contacts and counted down like any important date.', sw('l-birthdays', True)))
content += group('keys', f'Keyboard {C(3)}',
    S('single-keys', 'Single-key shortcuts', 'Keys like <kbd>r</kbd>, <kbd>e</kbd> and <kbd>x</kbd> act without a modifier. Turn off if you use speech input or a screen reader’s quick keys, or change any key.', sw('l-single-keys', True) + '<button class="btn">Change keys…</button>')
    + S('focus-ring', 'Always show the focus outline', 'Show where focus is even after a mouse click, not only when using the keyboard.', sw('l-focus-ring', False)))
nt = notes('n-set-read', 'How InboxQL looks and reads. Accessibility settings are not a separate “accessibility” page — text size, motion, spacing and shortcuts sit with the rest of how things look, because everyone uses them.',
    ['Text size scales the whole interface. At 150% the three parts become two, then one, rather than truncating.',
     'Every screen has the same three parts. There is no tab workspace and no expert mode; the only layout choice is the evidence panel — shown or hidden, and how wide.',
     'Single-character shortcuts can be turned off or remapped (WCAG 2.1.4), which matters for speech input and screen-reader quick-navigation keys.'],
    ['General (theme, inbox layout, previews, rail entries) becomes this page.', 'Text size, spacing, motion, week start, time format, reminders and the shortcut settings are new.',
     'The rail arrangement becomes “Subjects in the navigation”.'],
    [('<kbd>⌘+</kbd> / <kbd>⌘−</kbd>', 'Text size, anywhere'), ('<kbd>.</kbd>', 'Show or hide the evidence panel'), ('<kbd>←</kbd> <kbd>→</kbd> on the divider', 'Widen or narrow the evidence panel; <kbd>Enter</kbd> hides it')],
    'Theme swatches are a radio group named “Theme”; each swatch’s name is its theme. A changed setting says “changed” with a Reset link next to its label.')
spage('settings-reading.html', 'Reading and appearance', 'Theme, text size, spacing, motion, layout, reading and keyboard settings.', 'settings:reading', content,
      cli([('iql saved move &lt;name&gt; top', 'order of subjects in the navigation'), ('iql saved folders hide spam', 'hide a mailbox row')],
          'At once, in this window and every other open one. Stored in this browser except the subjects order, which is in the mailbox.', 'Reading and keyboard guide'), nt)

# ======================================================================= Your data
content = group('where', 'The mailbox',
    S('mailbox-location', 'Where it is', '<code>~/.iql/mail</code> — from the machine settings. Messages 3.1 GB · files 0.9 GB · search index 0.2 GB.', '<button class="btn">Show in Finder</button>')
    + S('disk', 'Room left', '41 GB free on this disk.', '<span class="chip good">plenty</span>'))
content += group('backups', f'Backups {C(1)}',
    S('backups', 'Last backup', '<strong>Oct 22, 9 days ago.</strong> The weekly backup on Sunday was missed because the computer was asleep; the next will run when it wakes.', '<span class="chip warn">overdue</span><button class="btn primary">Back up now</button>')
    + S('backup-keep', 'Schedule and keeping', 'Weekly · keep 4 scheduled, 3 from updates, 2 from restores. Backups you make yourself are never removed.', seg('bs', 'l-backup-keep', ['Off', 'Daily', 'Weekly'], 'Weekly') + '<button class="btn">Keep rules…</button>')
    + S('backup-list', 'Backups', '7 backups, 27 GB. Each is checked after it is written.', '<button class="btn">Show all</button><button class="btn">Restore…</button>'))
content += group('import', 'Bring mail in',
    S('import-source', 'Apple Mail', '<span style="color:var(--warn)">Blocked:</span> needs Full Disk Access for <strong>Terminal</strong> — the program macOS checks, not iql. <a href="#">How to allow it</a>', '<button class="btn" disabled aria-describedby="d-import-source">Import…</button>')
    + S('import-eml', 'A folder of .eml files', 'No permission needed: drag messages out of Mail into a folder.', '<button class="btn">Choose folder…</button>')
    + S('export', 'Export', 'All mail as .mbox, or a query’s results.', '<button class="btn">Export…</button>'))
content += group('maint', 'Health',
    S('health-checks', 'Checks', 'All 14 passed this morning. 211 files not yet read for text.', '<span class="chip good">ok</span><button class="btn">Read the 211 files</button>'))
content += group('danger', 'Delete',
    S('erase-data', f'Delete all stored messages {C(2)}', 'Removes every message, file and agent answer from this computer, after making a backup. Mail on your servers is not touched.', '<button class="btn" style="color:var(--bad);border-color:var(--bad)">Delete everything…</button>', 'danger'))
nt = notes('n-set-data', 'Where your mail is kept, whether it is safe, and how it comes in and goes out.',
    ['Backups say when, what was missed and why, and what happens next — “overdue” is the state, “Back up now” the fix.',
     'Destructive actions are last, in their own group, and say what they will and will not affect before you confirm by typing the mailbox name.'],
    ['Import, Data management, Maintenance and Storage were four sections.', 'Full Disk Access names the program that needs it, as the CLI does.'],
    [],
    'The disabled Import button is described by the sentence saying why, so a screen reader hears the reason.')
spage('settings-data.html', 'Your data', 'Where the mailbox is, backups, import and export, health, and deleting.', 'settings:data', content,
      cli([('iql --json backup list', 'every backup, room taken and left'), ('iql --json backup', 'make one now'), ('iql --json import sources', 'what can be imported, and what blocks it'), ('iql --json doctor', 'health checks')],
          'Backups run in the background; you can keep working. Restoring restarts the server.', 'Backups and import guide'), nt)

# ======================================================================= This computer
content = group('ver', 'InboxQL',
    S('updates', 'Version', 'You have 2026.10.31.2. <strong>2026.11.1.1 is ready</strong>: Ahead, important dates, and a calmer Settings. <a href="#">Release notes</a>', '<button class="btn primary">Update and restart</button>')
    + S('auto-check', 'Check for updates', 'Checks once a day; never installs without you.', sw('l-auto-check', True)))
content += group('svc', 'Running',
    S('login-service', 'Start at login', 'Runs in the background from login to logout, so mail stays current with no window open.', sw('l-login-service', True))
    + S('server-address', 'Address', '<code>http://127.0.0.1:8420</code> — this computer only, so no password is asked. Opening it to your network requires one.', '<button class="btn">Change…</button>')
    + S('heavy-work-on-battery', 'Heavy work on battery', 'Agent passes and model work wait for mains power. Nothing is lost; it stays pending.', sw('l-heavy-work-on-battery', False))
    + S('restart', 'Restart the server', 'Closes and reopens open windows; about 10 seconds.', '<button class="btn">Restart</button>'))
content += group('phone', 'Phone',
    S('phone-address', 'Reach InboxQL from your phone', f'Over Tailscale at <code>https://inboxql.tail1a2b3c.ts.net</code> — only devices on your network. <a href="phone-app.html">How it works</a>', '<span class="chip good">on</span><button class="btn">Show QR code</button>')
    + S('phone-devices', 'Phones allowed', 'Pixel 8 · alex@example.com · allowed Nov 1 · last seen 2 min ago', '<button class="btn">Sign out</button>')
    + S('phone-new', 'New phones', 'A new phone must be allowed here, with a matching code, before it can open your mailbox.', '<span class="chip">always asks</span>'))
content += group('windows', 'Open windows',
    S('windows', 'Windows showing InboxQL', '2 open: this one, and one on “Kitchen remodel”. Agents can point a window at a query; each time, the window says so.', '<button class="btn">Show</button>'))
content += group('log', 'Logging',
    S('log-level', 'How much is recorded', 'Debug is loud; turn it back down after chasing a problem.', seg('ll', 'l-log-level', ['Errors', 'Warnings', 'Info', 'Debug'], 'Info'))
    + S('log-query-text', 'Record the text of queries', 'Queries can be as private as the mail they find. When off, timings are kept and the words are not.', sw('l-log-query-text', True))
    + S('log-slow-queries', 'Slow query threshold', 'A query slower than this is logged as a warning, with its SQL.', '<select class="btn" aria-labelledby="l-log-slow-queries"><option>250 ms</option><option selected>500 ms</option><option>1 s</option></select>')
    + S('log-view', 'The log', '50,000 lines kept · 3 warnings today.', '<button class="btn">Open the log</button>'))
nt = notes('n-set-comp', 'How InboxQL runs on this computer: version, background service, address, power, open windows and logging.',
    ['Update, restart and address changes end the server serving your windows, so each says so on the button and in its description.'],
    ['System, Logging and the Windows tab become this page.', 'The machine settings file is not shown as its own thing; its values are ordinary settings here, with the file path in Help.'],
    [],
    'Buttons that restart say so in their names (“Update and restart”).')
spage('settings-computer.html', 'This computer', 'Version and updates, the background service, address, power, open windows and logging.', 'settings:computer', content,
      cli([('iql --json service status', 'installed, running, address'), ('iql --json update --check', 'newer release?'), ('iql --json ui list', 'open windows'), ('iql --json log level', 'logging level and dropped lines')],
          'Start at login and logging apply at once. Address changes need a restart; the page says so and offers it.', 'Running InboxQL guide'), nt)
