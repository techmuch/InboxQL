from chrome import *
import calendar

# ---------------------------------------------------------------- countdowns
# (what, date words, n, unit, source chip, lead line, classes)
COUNTDOWNS = [
    ('Q4 report to the board', 'Tue, Nov 10', 10, 'days', '<span class="chip">card · you set the due date</span>', '', 'soon'),
    ('Flight to Denver', 'Wed, Nov 25 · Lumen Air LA 418, 7:05 AM', 25, 'days', '<span class="chip">from a booking email</span>', '', ''),
    ('Passport expires', 'Thu, Jan 14, 2027', 75, 'days', '<span class="chip">you added</span>',
     'Start renewing by <strong>Thu, Dec 3</strong> — in 33 days (you set 6 weeks ahead)', ''),
    ('Anniversary', 'Sat, Dec 12', 42, 'days', '<span class="chip">from contacts · every year</span>', '', ''),
]


def countdowns(compact=False, with_suggestion=True, callout=''):
    out = ['<ul class="countdowns" aria-label="Important dates, soonest first">']
    for what, when, n, unit, src, lead, cls in COUNTDOWNS:
        c = ('cd ' + cls + (' compact' if compact else '')).strip()
        spoken = f'{what}, {when.split(" ·")[0]}, in {n} {unit}' + ('. Within its lead time.' if cls == 'soon' else '')
        lead_html = f'<span class="lead">{lead}</span>' if lead and not compact else ''
        src_html = '' if compact else src
        soon = '<span class="chip warn">within 2 weeks</span>' if cls == 'soon' and not compact else ''
        out.append(f'<li class="{c}" aria-label="{spoken}"><span class="n" aria-hidden="true">{n} <small>{unit}</small></span>'
                   f'<span class="what" aria-hidden="true">{what}</span><span class="when" aria-hidden="true">{when}</span>{lead_html}{soon}{src_html}</li>')
    if with_suggestion and not compact:
        out.append('<li class="cd suggest"><span class="chip agent" style="align-self:flex-start">agent suggests</span>'
                   '<span class="what">Car registration renewal</span><span class="when">Due Mon, Nov 30 — from “Your registration expires soon”, Oct 28</span>'
                   '<span style="display:flex;gap:.4rem;margin-top:.35rem"><button class="btn">Mark important</button><button class="btn quiet">Not important</button></span></li>')
    if not compact:
        out.append('<li class="cd add"><button class="btn">+ Mark a date as important <kbd>i</kbd></button>'
                   '<span class="small muted">Or press <kbd>i</kbd> on any event, card, trip or message.</span></li>')
    out.append('</ul>')
    return ''.join(out)


# ---------------------------------------------------------------- day agenda
def item(t, k, text, extra='', imp=False):
    inner = f'<span class="imp">{text}</span>' if imp else text
    return f'<li><span class="t">{t}</span><span class="k">{k}</span><span >{inner}{extra}</span></li>'


def day(dow, dn, full, items, hours, cap=8, today=False, note=''):
    pct = min(100, round(hours / cap * 100))
    hrs = f'{hours:g} h booked' if hours else 'nothing booked'
    cls = 'day today' if today else 'day'
    t = ' <span class="chip accent">today</span>' if today else ''
    return (f'<li class="{cls}"><h3><span class="sr-only">{full}</span><span aria-hidden="true">{dow}{t}<span class="dn">{dn}</span></span></h3>'
            f'<ul class="items">{"".join(items) or "<li class=muted>Nothing scheduled</li>"}</ul>'
            f'<div class="load"><span class="bar" aria-hidden="true"><i style="width:{pct}%"></i></span>{hrs}{note}</div></li>')


AGENT = '<span class="chip agent">agent proposed</span>'
FOUND = '<span class="chip agent">agent found in mail</span>'
OVERLAP = '<span class="chip warn">overlaps</span>'

THIS_WEEK = [
    day('Sat', 31, 'Saturday, October 31', [
        item('6:00 PM', 'Event', 'Trunk-or-treat · church parking lot'),
    ], 2, today=True),
    day('Sun', 1, 'Sunday, November 1', [
        item('all day', 'Holiday', 'Daylight saving time ends — clocks go back an hour'),
        item('9:00 AM', 'Event', 'Ward council · accepted'),
    ], 1.5),
]

NEXT_WEEK = [
    day('Mon', 2, 'Monday, November 2', [
        item('any time', 'Reply by', 'Rafael Ortiz — pick a hardware finish', '<span class="chip warn">on you</span>'),
        item('10:30 AM', 'Event', 'Cabinet delivery window', AGENT),
        item('1:00 PM', 'Event', 'Team stand-up'),
    ], 2.5),
    day('Tue', 3, 'Tuesday, November 3', [
        item('all day', 'Holiday', 'Election Day'),
        item('by 2 PM', 'Reply by', 'Priya Raman — comments on section 3', '<span class="chip warn">on you</span>'),
        item('2:00 PM', 'Event', 'Q4 report review call'),
    ], 3),
    day('Wed', 4, 'Wednesday, November 4', [
        item('—', 'Delivery', 'Hardware Depot order #88231 arrives', FOUND),
        item('7:00 PM', 'Event', 'Youth activity'),
    ], 1.5),
    day('Thu', 5, 'Thursday, November 5', [
        item('3:00 PM', 'Event', 'Dentist — Dr. Mehta', OVERLAP),
        item('3:15 PM', 'Event', 'Parent–teacher conference', OVERLAP),
    ], 2, note='<br><span style="color:var(--warn)">1 overlap</span>'),
    day('Fri', 6, 'Friday, November 6', [
        item('Fri', 'Due', 'Send Q4 numbers to Sam Okafor', '<span class="chip">card</span>'),
        item('12:00 PM', 'Event', 'Lunch with Jordan'),
    ], 3.5),
    day('Sat', 7, 'Saturday, November 7', [
        item('8:15 AM', 'Asked', 'Carpool run — Dana asked; you have not answered', '<span class="chip warn">on you</span>'),
    ], 0),
    day('Sun', 8, 'Sunday, November 8', [
        item('9:00 AM', 'Event', 'Ward council · repeats weekly'),
    ], 1.5),
]

GLANCE = [('Mon', 2, 2.5, '1 reply by'), ('Tue', 3, 3, '1 reply by'), ('Wed', 4, 1.5, ''), ('Thu', 5, 2, 'overlap'),
          ('Fri', 6, 3.5, '1 due'), ('Sat', 7, 0, '1 asked'), ('Sun', 8, 1.5, '')]


def glance():
    lis = []
    for dw, dn, h, flag in GLANCE:
        f = f'<span class="warnmark">{flag}</span>' if flag == 'overlap' else (f'<span>{flag}</span>' if flag else '<span class="muted">—</span>')
        lis.append(f'<li><span class="dw">{dw} {dn}</span><span class="fill" aria-hidden="true"><i style="height:{round(h / 8 * 100)}%"></i></span>'
                   f'{h:g} h<br>{f}</li>')
    return '<ul class="glance" aria-label="Next week at a glance: hours booked and what needs you, Monday to Sunday">' + ''.join(lis) + '</ul>'


def tabs(cur):
    t = [('ahead.html', 'Two weeks', 'w'), ('ahead-months.html', 'Two months', 'm')]
    return ('<nav class="tabs" aria-label="How far ahead">' + ''.join(
        f'<a href="{h}"{" aria-current=\"page\"" if n == cur else ""} aria-keyshortcuts="{k}">{n} <kbd>{k}</kbd></a>' for h, n, k in t)
        + '<span style="flex:1"></span><span class="small muted" style="align-self:center">Weeks start Monday · <a href="#">change</a></span></nav>')


def evidence(extra, r='14d'):
    return f'''
<aside class="evidence" aria-labelledby="ev-ahead">
  <h2 id="ev-ahead">How this was made</h2>
  <p class="small muted">One page over several kinds of thing. Each line is a query you can open.</p>
  <dl class="kv" style="margin:.5rem 0 1rem">
    <dt>Events</dt><dd><code>in:events next:{r}</code></dd>
    <dt>Due</dt><dd><code>in:tickets due:{r}</code></dd>
    <dt>Reply by</dt><dd><code>awaiting:me has:deadline</code> <span class="planned">planned</span></dd>
    <dt>Trips</dt><dd><code>extract:travel.start&lt;{r}</code></dd>
    <dt>Deliveries</dt><dd><code>extract:receipts.arrives&lt;{r}</code> <span class="planned">planned</span></dd>
    <dt>Important</dt><dd><code>is:important</code> <span class="planned">planned</span></dd>
  </dl>
  {extra}
  <h3 style="margin-top:1rem">Not shown</h3>
  <ul class="small" style="padding-left:1.1rem">
    <li>The <strong>Family</strong> calendar is hidden here — <a href="#">show it</a></li>
    <li>3 conversations mention a date the agent has not read yet; they will appear after its next pass</li>
    <li>Agent-proposed events show marked, and are not counted in hours booked until you approve them</li>
  </ul>
</aside>'''


FREE = '''<h3>Open time next week</h3>
  <p class="small">Between 9 and 5 on weekdays: <strong>23 hours</strong> free. Longest: Wednesday 9:00–6:30.</p>
  <h3 style="margin-top:1rem">To settle before Monday</h3>
  <ul class="small" style="padding-left:1.1rem">
    <li>Thursday 3:00 — two events overlap. <a href="#">Move one</a></li>
    <li>Cabinet delivery — <a href="review.html#events">approve or reject</a></li>
    <li>Carpool on Saturday — <a href="compose.html">answer Dana</a></li>
  </ul>'''

# ============================================================== two weeks page
body = head('Ahead', 'Ahead: important dates as countdowns, then this week and next week, day by day.')
body += banner('ahead.html')
body += appbar('next:14d OR is:important')
body += '<div class="shell">\n' + nav('Ahead')
body += f'''
<main id="main" class="main" aria-labelledby="page-title">
  <div class="page-head">
    <div class="grow">
      <p class="crumbs"><a href="today.html">Today</a> · Saturday, October 31</p>
      <h1 id="page-title">Ahead</h1>
      <p class="muted" style="margin-top:.3rem"><span class="callout" aria-hidden="true">1</span>
        Next week is your busiest in a month: <strong>14 hours booked</strong>, <strong>2 replies due</strong>, 1 card due and
        <strong>one overlap on Thursday</strong>. Your next important date is the Q4 report, in 10 days.</p>
    </div>
  </div>
  {tabs('Two weeks')}

  <section class="section" aria-labelledby="h-imp">
    <header><h2 id="h-imp" class="grow">Important dates <span class="callout" aria-hidden="true">2</span></h2>
      <a class="small" href="#">All important dates</a></header>
    {countdowns()}
  </section>

  <section class="section" aria-labelledby="h-this">
    <header><h2 id="h-this" class="grow">This week <span class="muted">· 2 days left</span></h2></header>
    <details class="card small" style="padding:.45rem .85rem;margin-bottom:.5rem"><summary>Mon 26 – Fri 30, already past: 11 events, 4 cards done, 23 replies sent</summary>
      <p style="margin:.4rem 0 0">Past days fold away so the page is about what is left. Opening this lists them, as History does.</p></details>
    <ol class="days card" aria-label="Rest of this week">{''.join(THIS_WEEK)}</ol>
  </section>

  <section class="section" aria-labelledby="h-next">
    <header><h2 id="h-next" class="grow">Next week <span class="muted">· Nov 2 – 8</span> <span class="callout" aria-hidden="true">3</span></h2>
      <a class="small" href="#">Open in Calendar</a></header>
    {glance()}
    <ol class="days card" aria-label="Next week, day by day">{''.join(NEXT_WEEK)}</ol>
    <p class="small muted" style="margin-top:.35rem"><span class="callout" aria-hidden="true">4</span> Kinds are words, not colours: Event, Due, Reply by, Asked, Trip, Delivery, Holiday. Amber only where you need to decide something.</p>
  </section>

  <section class="notes" aria-labelledby="notes-ahead">
    <h2 id="notes-ahead">About this screen</h2>
    <p><strong>Purpose.</strong> Today answers <em>what needs me now</em>. Ahead answers <em>what is coming, and is any of it a problem</em>.
      It puts the calendar, card due dates, replies with a deadline, trips and deliveries found in mail, and the dates you marked important
      on one page, in date order. It is reached from Today's “Ahead” section, the navigation, or <kbd>g a</kbd>.</p>
    <h3>Callouts</h3>
    <ol>
      <li><span class="callout">1</span> One sentence first, comparing the range with your usual: “busiest in a month”. It names the problems (an overlap) and the next important date, so a screen reader user has the whole picture before any list.</li>
      <li><span class="callout">2</span> <strong>Countdowns are for dates you marked important</strong>, not for everything with a date. Five at most are shown here; the rest are under “All important dates”. The agent can suggest one from mail (dashed, violet); it is not counted down until you accept. A date can carry a <strong>lead time</strong> — “start renewing 6 weeks before” — and the card then counts down to both. A countdown turns amber only inside its lead time or the last two weeks. <strong>Important dates notify you</strong> as they approach — when a lead time starts, a week before, the day before and that morning, changeable per date. Birthdays and anniversaries come from contacts <span class="planned">planned</span>.</li>
      <li><span class="callout">3</span> <strong>Next week at a glance</strong>: hours booked and what needs you, per day, so the busy and the empty days stand out before you read the detail. The bar is height only; the number is always printed beside it.</li>
      <li><span class="callout">4</span> Every item says its kind in a word. Things the agent found or proposed are marked violet and are not counted as booked until you approve them.</li>
    </ol>
    <h3>“This week” on a Saturday</h3>
    <p>The week is calendar weeks (starting Monday, changeable). Days already past fold into one line, so late in the week “this week” is short and “next week” does the work. The same rule makes <a href="ahead-months.html">Two months</a> show the rest of October in one line and then November and December in full.</p>
    <h3>What changed from today's app</h3>
    <ul>
      <li>Nothing like this exists today. The Calendar shows events only; due dates are on the Board; trips and deliveries are extracted but only visible by query.</li>
      <li>“Important” is a new mark you put on an event, card, trip, message or a plain date <span class="planned">planned</span>. It is one flag, not a new kind of thing, so <code>is:important</code> works in any query.</li>
      <li>Replies with a deadline (“by Monday”) need the <code>waiting</code> element to record the deadline it read <span class="planned">planned</span>.</li>
    </ul>
    <h3>Keyboard</h3>
    <table class="doc"><tr><th>Key</th><th>Does</th></tr>
      <tr><td><kbd>g a</kbd></td><td>Go to Ahead</td></tr>
      <tr><td><kbd>w</kbd> <kbd>m</kbd></td><td>Two weeks · two months</td></tr>
      <tr><td><kbd>[</kbd> <kbd>]</kbd></td><td>Earlier · later by one range</td></tr>
      <tr><td><kbd>↑</kbd> <kbd>↓</kbd></td><td>Move between items; <kbd>↵</kbd> opens one</td></tr>
      <tr><td><kbd>i</kbd></td><td>Mark the selected item important (or unmark it)</td></tr>
    </table>
    <h3>Screen reader</h3>
    <p>Each countdown is one list item with one spoken name: <em>“Q4 report to the board, Tue, Nov 10, in 10 days. Within its lead time.”</em> Each day is a heading level 3 with the full date (“Monday, November 2”), so heading navigation moves a day at a time; the items under it are a list. The at-a-glance strip is a list of seven items with the hours as text; the bars are hidden from assistive technology.</p>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w9">W9 Look ahead</a> · <a href="workflows.html#w10">W10 Mark a date as important</a></p>
  </section>
</main>
{evidence(FREE)}
</div>
'''
body += foot()
write('ahead.html', body)


# ============================================================== two months page
def month(year, mon, cells, today=None):
    cal = calendar.Calendar(firstweekday=0)
    rows = []
    for week in cal.monthdatescalendar(year, mon):
        tds = []
        for d in week:
            out = d.month != mon
            cls = []
            evs = '' if out else cells.get(d.day, '')
            if out:
                cls.append('out')
            if not out and today == d.day:
                cls.append('today')
            if not out and 'class="ev imp"' in evs:
                cls.append('imp')
            c = f' class="{" ".join(cls)}"' if cls else ''
            label = d.strftime('%a %b ') + str(d.day)
            tds.append(f'<td{c}><span class="dn"><span aria-hidden="true">{d.day}</span><span class="sr-only">{label}</span></span>{evs}</td>')
        rows.append('<tr>' + ''.join(tds) + '</tr>')
    name = calendar.month_name[mon]
    head_ = ''.join(f'<th scope="col"><abbr title="{n}">{n[:3]}</abbr></th>' for n in calendar.day_name)
    return (f'<div style="overflow-x:auto" tabindex="0" role="region" aria-label="{name} {year} grid">'
            f'<table class="month"><caption class="sr-only">{name} {year}</caption><thead><tr>{head_}</tr></thead><tbody>{"".join(rows)}</tbody></table></div>')


def ev(text, kind=''):
    cls = 'ev ' + kind if kind else 'ev'
    return f'<span class="{cls}" title="{text}">{text}</span>'


NOV = {
    1: ev('Clocks back') + ev('Ward council'),
    2: ev('Cabinets', 'agent') + ev('Reply Rafael') + '<span class="more">+1</span>',
    3: ev('Election Day') + ev('Q4 call') + '<span class="more">+1</span>',
    4: ev('Delivery', 'agent') + ev('Youth'),
    5: ev('Dentist') + ev('Conference'),
    6: ev('Q4 numbers due') + ev('Lunch'),
    7: ev('Carpool?'),
    8: ev('Ward council'),
    10: ev('Q4 report', 'imp'),
    11: ev('Veterans Day'),
    13: ev('Book club'),
    15: ev('Ward council'),
    17: ev('Inspection due'),
    19: ev('Ruth’s birthday', 'imp') + ev('Dentist cleaning'),
    22: ev('Ward council'),
    25: ev('Fly to Denver', 'imp'),
    26: ev('Thanksgiving'),
    29: ev('Fly home'),
    30: ev('Registration', 'agent'),
}
DEC = {
    3: ev('Start passport', 'imp'),
    6: ev('Ward council'),
    9: ev('Choir'),
    12: ev('Anniversary', 'imp'),
    13: ev('Ward council'),
    15: ev('Q4 close'),
    18: ev('School ends'),
    20: ev('Ward council'),
    24: ev('Christmas Eve'),
    25: ev('Christmas'),
    27: ev('Ward council'),
    31: ev('New Year’s Eve'),
}


def notable(items):
    return '<ul class="items card" style="padding:.6rem .85rem;margin-top:.6rem">' + ''.join(items) + '</ul>'


body = head('Ahead · two months', 'Ahead over two months: important dates, then November and December as grids with a list of what matters.')
body += banner('ahead.html')
body += appbar('next:2mo OR is:important')
body += '<div class="shell">\n' + nav('Ahead')
body += f'''
<main id="main" class="main" aria-labelledby="page-title">
  <div class="page-head">
    <div class="grow">
      <p class="crumbs"><a href="today.html">Today</a> · Saturday, October 31</p>
      <h1 id="page-title">Ahead · two months</h1>
      <p class="muted" style="margin-top:.3rem"><span class="callout" aria-hidden="true">1</span>
        November is busier than usual in its first two weeks, then quiet: you travel <strong>Nov 25 – 29</strong>.
        December has <strong>2 important dates</strong> and a lead time starting on the 3rd.</p>
    </div>
  </div>
  {tabs('Two months')}

  <section class="section" aria-labelledby="h-imp">
    <header><h2 id="h-imp" class="grow">Important dates</h2><a class="small" href="ahead.html#h-imp">Manage</a></header>
    {countdowns(compact=True)}
  </section>

  <section class="section" aria-labelledby="h-oct">
    <header><h2 id="h-oct" class="grow">This month <span class="muted">· October, 1 day left</span></h2></header>
    <p class="card small" style="padding:.45rem .85rem">Today: Trunk-or-treat at 6:00 PM. Nothing else — <a href="ahead.html">see this week</a>.</p>
  </section>

  <section class="section" aria-labelledby="h-nov">
    <header><h2 id="h-nov" class="grow">November <span class="callout" aria-hidden="true">2</span></h2><a class="small" href="#">Open in Calendar</a></header>
    <p class="small" style="margin:0 0 .5rem">31 events (38 h booked), 4 things due, 1 trip, 1 important date. Busiest week: Nov 2–8 (14 h). Quietest: Nov 23–29, while you are away.</p>
    {month(2026, 11, NOV)}
    <h3 style="margin-top:1rem">What matters in November <span class="callout" aria-hidden="true">3</span></h3>
    {notable([
        item('Tue 10', 'Due', 'Q4 report to the board', imp=True),
        item('Tue 17', 'Due', 'Kitchen rough-in inspection', '<span class="chip">card</span>'),
        item('Thu 19', 'Birthday', 'Grandma Ruth turns 84', '<span class="chip">from contacts</span>', imp=True),
        item('Wed 25', 'Trip', 'Fly to Denver, Lumen Air LA 418, 7:05 AM', imp=True),
        item('Thu 26', 'Holiday', 'Thanksgiving'),
        item('Sun 29', 'Trip', 'Fly home, LA 421, 4:40 PM'),
        item('Mon 30', 'Due', 'Car registration renewal', FOUND),
    ])}
  </section>

  <section class="section" aria-labelledby="h-dec">
    <header><h2 id="h-dec" class="grow">December</h2><a class="small" href="#">Open in Calendar</a></header>
    <p class="small" style="margin:0 0 .5rem">18 events so far (21 h), 1 thing due, 2 important dates. Calendars usually fill in later: last December ended with 34 events.</p>
    {month(2026, 12, DEC)}
    <h3 style="margin-top:1rem">What matters in December</h3>
    {notable([
        item('Thu 3', 'Lead', 'Start renewing your passport (expires Jan 14)', imp=True),
        item('Sat 12', 'Date', 'Anniversary', imp=True),
        item('Tue 15', 'Due', 'Q4 close', '<span class="chip">card</span>'),
        item('Fri 18', 'Event', 'Last day of school before the break'),
        item('Thu 24 – Fri 25', 'Holiday', 'Christmas'),
    ])}
  </section>

  <section class="notes" aria-labelledby="notes-months">
    <h2 id="notes-months">About this screen</h2>
    <p><strong>Purpose.</strong> The long view: what the next two months hold, where the busy and empty stretches are, and what is coming that needs preparing for. It is for planning, not for working through items — that is the two-week view and Today.</p>
    <h3>Callouts</h3>
    <ol>
      <li><span class="callout">1</span> The summary sentence is about shape: busy or quiet, away or home, and what has a lead time. On the last day of a month, “this month” is one line and the next two months are shown in full.</li>
      <li><span class="callout">2</span> Each month is a grid for the eye, with a sentence above it that says the same thing in words. Important dates are tinted and starred; agent-found dates are violet. At phone width the grid shows only day numbers and stars, and the list below carries the words.</li>
      <li><span class="callout">3</span> <strong>What matters</strong> is the month without routine: important dates, due dates, trips, holidays and anything agent-found. Weekly repeats (Ward council) are left out here; they are in the grid and the calendar.</li>
    </ol>
    <h3>Screen reader</h3>
    <p>The month grid is a real table with a caption and day-of-week column headers, inside a focusable region. It is optional: the sentence and the “What matters” list say everything the grid shows except the routine.</p>
    <h3>Workflows</h3>
    <p><a href="workflows.html#w9">W9 Look ahead</a> · <a href="workflows.html#w10">W10 Mark a date as important</a></p>
  </section>
</main>
{evidence('<h3>Compared with your usual</h3><p class="small">“Busier than usual” compares hours booked with the same weeks over the last six months: November’s first fortnight is 31 h against a usual 22 h.</p>', '2mo')}
</div>
'''
body += foot()
write('ahead-months.html', body)
