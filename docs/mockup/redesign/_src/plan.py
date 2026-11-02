"""Render the implementation tracker: docs/mockup/redesign/plan.html."""
import html
import importlib
import plan_data
importlib.reload(plan_data)
from plan_data import PHASES, LATER, UPDATED
from chrome import head, banner, foot, write

LABEL = {'todo': 'To do', 'doing': 'In progress', 'done': 'Done', 'later': 'Later'}
CHIP = {'todo': 'chip', 'doing': 'chip warn', 'done': 'chip good', 'later': 'chip'}


def pct(tasks):
    return round(100 * sum(1 for t in tasks if t[1] == 'done') / len(tasks)) if tasks else 0


all_tasks = [t for p in PHASES for t in p['tasks']]
overall = pct(all_tasks)
done_n = sum(1 for t in all_tasks if t[1] == 'done')


def phase_state(p):
    s = [t[1] for t in p['tasks']]
    if all(x == 'done' for x in s):
        return 'done'
    if any(x in ('done', 'doing') for x in s):
        return 'doing'
    return 'todo'


summary_rows = ''.join(
    f'<tr><td><a href="#{p["id"]}">{i}. {p["title"]}</a></td><td><span class="{CHIP[phase_state(p)]}">{LABEL[phase_state(p)]}</span></td>'
    f'<td class="num">{sum(1 for t in p["tasks"] if t[1] == "done")} of {len(p["tasks"])}</td>'
    f'<td style="min-width:8rem"><div class="progress" role="img" aria-label="{pct(p["tasks"])}% done"><i style="width:{pct(p["tasks"])}%"></i></div></td></tr>'
    for i, p in enumerate(PHASES))

sections = []
for i, p in enumerate(PHASES):
    st = phase_state(p)
    mock = ''
    if p.get('mockups'):
        mock = '<p class="small" style="margin:.2rem 0 .6rem">Mockups: ' + ' · '.join(f'<a href="{m}">{m}</a>' for m in p['mockups']) + '</p>'
    rows = ''.join(
        f'<tr><td class="num">{i}.{j}</td><td>{html.escape(t[0])}</td><td><span class="{CHIP[t[1]]}">{LABEL[t[1]]}</span></td><td class="small">{t[2]}</td></tr>'
        for j, t in enumerate(p['tasks'], 1))
    sections.append(f'''
<section class="section card" style="padding:1rem 1.2rem" id="{p["id"]}" aria-labelledby="{p["id"]}-h">
  <header style="display:flex;flex-wrap:wrap;gap:.5rem 1rem;align-items:baseline"><h2 id="{p["id"]}-h" class="grow" style="flex:1">Phase {i} · {p["title"]}</h2>
    <span class="{CHIP[st]}">{LABEL[st]}</span><span class="small muted">{sum(1 for t in p["tasks"] if t[1] == "done")} of {len(p["tasks"])} done</span></header>
  <p style="margin:.3rem 0">{p["goal"]}</p>{mock}
  <div style="overflow-x:auto" tabindex="0" role="region" aria-label="Tasks in phase {i}">
  <table class="grid"><caption class="sr-only">Tasks in phase {i}, {p["title"]}</caption>
    <thead><tr><th scope="col" class="num">#</th><th scope="col">Task</th><th scope="col">Status</th><th scope="col">Notes</th></tr></thead><tbody>{rows}</tbody></table></div>
  <div style="margin-top:.75rem"><label for="fb-{p["id"]}" class="small"><strong>Your feedback on phase {i}</strong> <span class="muted">— kept in this browser; “Copy all feedback” collects it</span></label>
    <textarea id="fb-{p["id"]}" data-feedback="{p["id"]}" data-title="Phase {i} · {html.escape(p["title"])}" rows="2" style="width:100%;font:inherit;margin-top:.3rem;padding:.5rem;border:1px solid var(--border);border-radius:var(--radius);background:var(--bg);color:var(--text)"></textarea></div>
</section>''')

later = ''.join(f'<li><strong>{a}</strong> — {b}</li>' for a, b in LATER)

body = head('Implementation plan', 'The phased plan for building the InboxQL redesign, with progress and a place for feedback.')
body += banner('plan.html')
body += f'''
<main id="main" class="main" style="max-width:72rem;margin:0 auto" aria-labelledby="page-title">
  <h1 id="page-title">Building the redesign</h1>
  <p class="muted" style="margin-top:.3rem">The phased plan for turning the <a href="index.html">mockups</a> into InboxQL. Updated {UPDATED}. Each phase ships something usable; statuses change as the work is done.</p>
  <div class="card" style="padding:1rem 1.2rem;margin:1rem 0" role="group" aria-labelledby="ov-h">
    <h2 id="ov-h" style="font-size:1rem">Overall: {done_n} of {len(all_tasks)} tasks done ({overall}%)</h2>
    <div class="progress" style="margin:.5rem 0 1rem;height:.7rem" role="img" aria-label="{overall}% done"><i style="width:{overall}%"></i></div>
    <div style="overflow-x:auto" tabindex="0" role="region" aria-label="Phases summary"><table class="grid"><caption class="sr-only">Phases</caption>
      <thead><tr><th scope="col">Phase</th><th scope="col">Status</th><th scope="col" class="num">Tasks</th><th scope="col">Progress</th></tr></thead><tbody>{summary_rows}</tbody></table></div>
    <p style="margin:.8rem 0 0;display:flex;gap:.5rem;flex-wrap:wrap"><button type="button" class="btn" id="copy-fb">Copy all feedback</button><span id="copy-status" class="small muted" role="status"></span></p>
  </div>
  <section class="section" aria-labelledby="h-how"><h2 id="h-how">How it is built</h2>
    <ul>
      <li><strong>One change underneath.</strong> Everything in InboxQL opens through one function, <code>openTool()</code>. Phase 1 turns it into a router of places, so the Desk, Calendar, Board, Settings and the viewer keep working inside the new layout while new screens arrive.</li>
      <li><strong>Each phase is usable on its own</strong> and leaves the tests passing.</li>
      <li><strong>Nothing about sending changes.</strong> Agent mail still waits for your Send; agent calendar and card changes are still proposals.</li>
      <li><strong>Accessibility is checked per phase</strong>: keyboard, screen reader names, contrast in every theme, 11 px minimum text.</li>
    </ul></section>
  {''.join(sections)}
  <section class="section" aria-labelledby="h-later"><h2 id="h-later">Later — decided, not in this release</h2><ul>{later}</ul></section>
</main>
<script>
(function () {{
  var areas = document.querySelectorAll('[data-feedback]');
  areas.forEach(function (a) {{
    var k = 'iql-plan-fb-' + a.dataset.feedback;
    try {{ a.value = localStorage.getItem(k) || ''; }} catch (e) {{}}
    a.addEventListener('input', function () {{ try {{ localStorage.setItem(k, a.value); }} catch (e) {{}} }});
  }});
  document.getElementById('copy-fb').addEventListener('click', function () {{
    var out = [];
    areas.forEach(function (a) {{ if (a.value.trim()) out.push('## ' + a.dataset.title + '\\n' + a.value.trim()); }});
    var text = out.length ? out.join('\\n\\n') : '';
    var s = document.getElementById('copy-status');
    if (!text) {{ s.textContent = 'No feedback written yet.'; return; }}
    (navigator.clipboard ? navigator.clipboard.writeText(text) : Promise.reject()).then(
      function () {{ s.textContent = 'Copied feedback on ' + out.length + ' phase' + (out.length === 1 ? '' : 's') + '.'; }},
      function () {{ s.textContent = 'Could not copy; select the text in each box instead.'; }});
  }});
}})();
</script>
'''
body += foot()
write('plan.html', body)
