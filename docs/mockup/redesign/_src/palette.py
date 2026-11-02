from chrome import *

body = head('Go to…', 'The command palette: one box to reach any person, subject, place or action.')
body += banner('palette.html')
body += appbar('needs:me')
body += '<div style="position:relative;height:88vh;overflow:hidden;border-bottom:2px dashed var(--border)"><div class="shell two" aria-hidden="true" inert>\n' + nav('Today') + '''
<main class="main"><h1>Today</h1><p class="muted">(Today, behind the palette.)</p></main></div>
'''


def opt(i, kind, label, detail, sel=False, key=''):
    k = f'<kbd>{key}</kbd>' if key else ''
    return (f'<li id="opt{i}" role="option" aria-selected="{"true" if sel else "false"}" class="row{" selected" if sel else ""}" '
            f'style="grid-template-columns:5.5rem minmax(0,1fr) auto;padding:.45rem .9rem">'
            f'<span class="small muted">{kind}</span><span><strong>{label}</strong> <span class="muted small">{detail}</span></span><span>{k}</span></li>')


body += '''
<div class="scrim" style="position:absolute">
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="pal-title">
    <h1 id="pal-title" class="sr-only">Go to</h1>
    <div style="display:flex;align-items:center;gap:.5rem;padding:.6rem .9rem;border-bottom:1px solid var(--border)">
      <span aria-hidden="true" class="muted">⌕</span>
      <input role="combobox" aria-expanded="true" aria-controls="pal-list" aria-activedescendant="opt1" aria-autocomplete="list"
             aria-label="Go to a person, subject, place or action" value="rafa" autofocus
             style="flex:1;border:0;background:transparent;font:inherit;font-size:1.05rem;color:var(--text)">
      <kbd>Esc</kbd>
    </div>
    <ul id="pal-list" role="listbox" aria-label="Matches" class="list" style="max-height:60vh;overflow:auto">
      <li role="presentation" class="small muted" style="padding:.5rem .9rem 0">PEOPLE</li>
'''
body += opt(1, 'Person', 'Rafael Ortiz', 'rafael@ortizreno.example · 38 messages · 1 reply owed', True, '↵')
body += opt(2, 'Person', 'Rafaela Souza', 'rsouza@example.org · 3 messages')
body += '<li role="presentation" class="small muted" style="padding:.5rem .9rem 0">SUBJECTS</li>'
body += opt(3, 'Topic', 'Kitchen remodel', '12 conversations · Rafael Ortiz, Jordan Lee')
body += '<li role="presentation" class="small muted" style="padding:.5rem .9rem 0">ACTIONS</li>'
body += opt(4, 'Action', 'Reply to Rafael', 'Cabinet hardware · the agent drafted one', key='')
body += opt(5, 'Action', 'Write to Rafael', '')
body += opt(6, 'Action', 'Pin Rafael Ortiz', 'adds him to Subjects')
body += '<li role="presentation" class="small muted" style="padding:.5rem .9rem 0">SEARCH</li>'
body += opt(7, 'Query', 'Mail mentioning “rafa”', '<code>rafa</code> · 41 messages')
body += opt(8, 'Query', 'Mail from Rafael this month', '<code>from:rafael@ortizreno.example after:2026-10-01</code>')
body += '''
    </ul>
    <p class="small muted" style="padding:.5rem .9rem;border-top:1px solid var(--border);margin:0">↑↓ choose · ↵ go · type <kbd>&gt;</kbd> for actions only, <kbd>/</kbd> to write a query</p>
  </div>
</div>
</div>
<main id="main" class="main" style="max-width:52rem;margin:0 auto">
  <section class="notes" aria-labelledby="notes-pal">
    <h2 id="notes-pal">About this screen</h2>
    <p><strong>Purpose.</strong> Objective 5, <em>fewer places</em>. With subjects replacing most destinations, the way to anywhere is to type its name. <kbd>⌘K</kbd> from any screen. The navigation column stays, holding only what you pinned and the four Needs-me entries.</p>
    <h3>What it finds</h3>
    <ul>
      <li><strong>People and organizations</strong>, ranked by how recently and how much you correspond — not alphabetically.</li>
      <li><strong>Subjects</strong>: topics, labels, saved queries, cards.</li>
      <li><strong>Actions</strong> that apply to the top match (“Reply to Rafael”) and global ones (“Go to Calendar”, “Switch theme”).</li>
      <li><strong>Queries</strong>: the text as a full-text search, and a structured guess. Choosing one opens Ask with that query — shown, never run unseen.</li>
    </ul>
    <h3>Screen reader</h3>
    <p>The ARIA combobox pattern: a modal dialog, an input with <code>aria-activedescendant</code> pointing at the highlighted option, group labels as presentation rows. Each option's text says its kind first. The count of matches is announced politely after typing pauses.</p>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w2">W2 Work one subject</a> · <a href="workflows.html#w7">W7 Ask a question of the mail</a></p>
  </section>
</main>
'''
body += foot()
write('palette.html', body)
