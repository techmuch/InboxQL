"""Shared chrome for the redesign mockups: head, banner, app bar, nav."""

OUT = __import__('os').path.dirname(__import__('os').path.abspath(__file__)) + '/..'

SCREENS = [
    ('index.html', 'Overview'),
    ('today.html', 'Today'),
    ('ahead.html', 'Ahead'),
    ('subject.html', 'Subject: a person'),
    ('conversation.html', 'Conversation'),
    ('review.html', 'Review'),
    ('compose.html', 'Reply and send'),
    ('query.html', 'Ask (query)'),
    ('palette.html', 'Command palette'),
    ('activity.html', 'Agent activity'),
    ('mobile.html', 'Phone'),
    ('workflows.html', 'Workflows'),
]


def head(title, desc):
    return f'''<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title} · InboxQL redesign</title>
<meta name="description" content="{desc}">
<link rel="stylesheet" href="mock.css">
<script src="mock.js"></script>
</head>
<body>
<a class="skip" href="#main">Skip to content</a>
'''


BANNER = [('index.html', 'Overview'), ('plan.html', 'Plan'), ('index.html#h-screens', 'All screens'), ('workflows.html', 'Workflows'),
          ('palette.html', 'Command palette'), ('mobile.html', 'Phone'), ('phone-app.html', 'Phone app'), ('ask.html', 'Ask in words')]


def banner(current):
    links = ' · '.join(
        f'<a href="{f}"{" aria-current=\"page\"" if f == current else ""}>{n}</a>' for f, n in BANNER)
    return f'''<aside class="mock-banner" aria-label="About this mockup">
  <strong>Mockup</strong><span>Static, made-up data. Every item in the left-hand pane opens its own mockup; notes are below each screen.</span>
  <nav aria-label="Mockup screens" class="small">{links}</nav>
  <span class="spacer"></span>
  <button type="button" class="theme-toggle" data-theme-toggle>Show dark theme</button>
</aside>
'''


def appbar(query, status='Synced 1 min ago · agent: 3 answers in the last hour'):
    return f'''<header class="app-bar">
  <a class="brand" href="today.html">InboxQL</a>
  <form class="querybar" role="search" aria-label="Ask" onsubmit="return false">
    <label for="q" class="sr-only">Query — what this screen shows. Edit it to change the view.</label>
    <span aria-hidden="true" class="muted">⌕</span>
    <input id="q" class="q" value="{query}" spellcheck="false" autocomplete="off">
    <kbd title="Focus the query">/</kbd>
  </form>
  <a class="btn" href="palette.html" aria-keyshortcuts="Meta+K">Go to… <span class="k"><kbd>⌘K</kbd></span></a>
  <a class="btn primary" href="compose.html" aria-keyshortcuts="c">Write <span class="k"><kbd>c</kbd></span></a>
  <span class="status" role="status" aria-live="off"><span class="dot" aria-hidden="true" style="background:var(--good)"></span>{status}</span>
</header>
'''


NAV = [
    ('Needs me', [
        ('today.html', 'Today', '14'),
        ('ahead.html', 'Ahead', ''),
        ('replies.html', 'Replies I owe', '4'),
        ('review.html', 'To review', '9'),
        ('outbox.html', 'Drafts and outbox', '4'),
    ]),
    ('Mail', [
        ('inbox.html', 'Inbox', '117'),
        ('everything.html', 'Everything', ''),
    ]),
    ('Subjects', [
        ('subject.html', 'Rafael Ortiz', ''),
        ('topic.html', 'Kitchen remodel', ''),
        ('query.html', 'Receipts 2026', ''),
        ('q4.html', 'Q4 report', ''),
    ]),
    ('Places', [
        ('calendar.html', 'Calendar', ''),
        ('board.html', 'Board', ''),
        ('files.html', 'Files', ''),
        ('people.html', 'People', ''),
        ('activity.html', 'Agent activity', ''),
        ('settings.html', 'Settings', ''),
        ('help.html', 'Help', ''),
    ]),
]


def nav(current):
    parts = ['<nav class="nav" aria-label="Places">']
    for heading, items in NAV:
        hid = 'nav-' + heading.lower().replace(' ', '-')
        parts.append(f'<h2 id="{hid}">{heading}</h2><ul aria-labelledby="{hid}">')
        for href, label, count in items:
            cur = ' aria-current="page"' if label == current else ''
            c = f'<span class="count">{count}<span class="sr-only"> items</span></span>' if count else ''
            parts.append(f'<li><a href="{href}"{cur}>{label}{c}</a></li>')
        parts.append('</ul>')
    parts.append('<p class="small muted" style="padding:.75rem .6rem 0">Subjects are saved queries. Pin any person, topic or query with <kbd>p</kbd>.</p>')
    parts.append('</nav>')
    return '\n'.join(parts)


def foot():
    return '</body>\n</html>\n'


def write(name, html):
    with open(f'{OUT}/{name}', 'w') as f:
        f.write(html)


def notes(nid, purpose, callouts=(), changed=(), keys=(), sr='', wf=''):
    h = [f'<section class="notes" aria-labelledby="{nid}"><h2 id="{nid}">About this screen</h2><p><strong>Purpose.</strong> {purpose}</p>']
    if callouts:
        h.append('<h3>Callouts</h3><ol>' + ''.join(f'<li><span class="callout">{i}</span> {c}</li>' for i, c in enumerate(callouts, 1)) + '</ol>')
    if changed:
        h.append("<h3>What changed from today's app</h3><ul>" + ''.join(f'<li>{c}</li>' for c in changed) + '</ul>')
    if keys:
        h.append('<h3>Keyboard</h3><table class="doc"><tr><th>Key</th><th>Does</th></tr>' + ''.join(f'<tr><td>{k}</td><td>{d}</td></tr>' for k, d in keys) + '</table>')
    if sr:
        h.append(f'<h3>Screen reader</h3><p>{sr}</p>')
    if wf:
        h.append(f'<h3>Workflows</h3><p>{wf}</p>')
    h.append('</section>')
    return ''.join(h)


def C(n):
    return f'<span class="callout" aria-hidden="true">{n}</span>'


def page(fname, title, desc, query, navname, main, aside):
    body = head(title, desc) + banner(fname) + appbar(query) + '<div class="shell">\n' + nav(navname)
    body += f'<main id="main" class="main" aria-labelledby="page-title">{main}</main>\n{aside}\n</div>\n' + foot()
    write(fname, body)
