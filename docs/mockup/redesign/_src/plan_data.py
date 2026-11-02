"""The redesign implementation plan, as data. plan.py renders it.

status: todo | doing | done | later
"""

UPDATED = '2026-11-02'

PHASES = [
    dict(id='p0', title='Plan and tracker', goal='A phased plan everyone can follow, with a page that tracks it and collects feedback.',
         tasks=[
             ('Write the phased plan from the approved mockups and decisions', 'done', ''),
             ('Publish this tracker beside the mockups and link it from the overview', 'done', 'docs/mockup/redesign/plan.html'),
         ]),
    dict(id='p1', title='The three-part shell', goal='Replace the tab-and-pane workspace with places, a left-hand pane, the screen, and a resizable evidence panel. Every existing view keeps working.', mockups=['index.html'],
         tasks=[
             ('Places router: one place at a time, in the address (#/today), with Back and Forward', 'done', ''),
             ('openTool() and every “open this” call route to places instead of tabs', 'done', ''),
             ('App bar: brand, the query bar, Go to…, Write, sync status', 'done', ''),
             ('Left-hand pane: Needs me, Mail, Subjects, Places, with live counts', 'done', ''),
             ('Evidence panel: resizable by drag and keyboard (window-splitter pattern), hidden with “.”, width remembered', 'done', ''),
             ('Phone width: the pane becomes a bottom bar', 'done', ''),
             ('Remove tabs: no ShellLayout, menu bar, chat pane, viewer-tab modes or Reset layout; commands stay in Go to…', 'done', 'nexus-shell stays for themes and the command palette only'),
             ('Open windows report the place they show (iql ui list), and iql ui commands move them', 'done', ''),
             ('Tests updated for the shell', 'done', ''),
         ]),
    dict(id='p2', title='Today', goal='The home screen: what needs you before what arrived.', mockups=['today.html'],
         tasks=[
             ('One sentence of state at the top', 'done', ''),
             ('Replies you owe, with reply / open / not waiting on me', 'done', ''),
             ('To review: counts by kind, linking to the queue', 'done', ''),
             ('Arrived since yesterday, grouped by topic, archive a group with Undo', 'done', 'Grouped by sender for now: the topics element is not answered widely enough to group by'),
             ('Next 48 hours', 'done', ''),
             ('Today is the home screen', 'done', ''),
         ]),
    dict(id='p3', title='Important dates and Ahead', goal='Countdowns for dates you mark important, the next two weeks and two months, and reminders as they approach.', mockups=['ahead.html', 'ahead-months.html'],
         tasks=[
             ('Important dates stored in the mailbox (title, date, lead time, yearly, what it is about), with an API', 'done', 'Schema v49, /api/dates; agent suggestions are stored but not counted down'),
             ('Ahead: two weeks, day by day, from events, card due dates and important dates', 'done', ''),
             ('Ahead: two months as grids with “What matters”', 'done', ''),
             ('Countdowns on Today and Ahead; mark and unmark a date', 'done', ''),
             ('Reminders when a lead time starts, a week before, the day before and that morning', 'done', 'Sent by the server at 8 AM to every window, or as a system notification'),
         ]),
    dict(id='p4', title='Needs me', goal='Everything waiting on you, in one place each.', mockups=['replies.html', 'review.html', 'outbox.html', 'activity.html'],
         tasks=[
             ('Replies I owe, and Waiting on others', 'done', ''),
             ('To review: one queue for mail to send, calendar proposals, proposed cards and agent checks', 'done', ''),
             ('Drafts and outbox: waiting for your Send, failed with the reason, drafts', 'done', ''),
             ('Agent activity: what agents did, with Undo, and how often they are right', 'done', 'Counts of what agents wrote and proposed, with the work loop (accuracy, disagreements) beneath; per-change Undo stays on each item'),
         ]),
    dict(id='p5', title='Mail and subjects', goal='The inbox, all mail, and pages for people and topics.', mockups=['inbox.html', 'everything.html', 'subject.html', 'topic.html'],
         tasks=[
             ('Inbox and Everything in the new layout; the selected message reads in the evidence panel', 'done', 'The Desk keeps its folders and labels, now a rail you can put away'),
             ('Subjects in the left-hand pane are your saved queries, in your order', 'done', ''),
             ('Person page: open loops first, then everything with them', 'done', ''),
             ('Topic page: open loops, money, dates, cards and people for a topic', 'done', 'From the topics element; /api/topics lists them; Topics is under Subjects'),
         ]),
    dict(id='p6', title='Settings and Help', goal='Six settings sections led by what needs attention; help on every screen.', mockups=['settings.html', 'help.html', 'help-here.html'],
         tasks=[
             ('Settings overview: needs attention, then each section’s state in a line', 'done', ''),
             ('Settings regrouped into accounts, agents and AI, reading and appearance, your data, this computer', 'done', ''),
             ('Help home: search, guides, what’s new', 'done', ''),
             ('“?” on any screen: what it is for, its sections and its keys', 'done', ''),
         ]),
    dict(id='p7', title='Ask in words', goal='A question in plain words, answered by Claude Code on this computer, with the evidence and the commands it ran.', mockups=['ask.html'],
         tasks=[
             ('Server runs the chosen agent CLI read-only (iql commands only) and returns its answer', 'done', 'Off by default; read-only iql allow-list; tested end to end on an empty mailbox (14 s)'),
             ('Ask page: the answer, what it rests on, the commands it ran, what was sent', 'done', ''),
             ('The query bar offers “Ask” for words that are not a query', 'done', ''),
             ('Settings: which agent answers, or off', 'done', 'Settings → AI and agents → Ask in words: Off or Claude Code'),
         ]),
    dict(id='p8', title='Phone app', goal='InboxQL installable from Chrome, laid out for a phone.', mockups=['phone-app.html', 'mobile.html'],
         tasks=[
             ('Web app manifest, icons and a service worker, so Chrome offers “Install app”', 'done', 'Chrome reports no installability errors; the worker is network-first and never caches /api/'),
             ('Phone layout checked: bottom bar, one column, 44 px targets', 'done', ''),
             ('Guide: reaching InboxQL from your phone over Tailscale', 'done', 'docs/installing.md → On your phone (tailscale serve --bg 8420)'),
         ]),
    dict(id='p9', title='Release', goal='Documented, tested, released and installed.',
         tasks=[
             ('Guides updated for the new layout; release notes', 'done', ''),
             ('Typecheck, unit tests, Go tests; accessibility check in light and dark', 'done', ''),
             ('Commit, tag, push; the release workflow builds', 'done', 'Tagged v2026.11.2.1; release workflow green; CI failed on gofmt of one test file, fixed in 77ce657 (not yet re-checked: GitHub rate limit)'),
             ('Installed with iql update', 'done', 'Running 2026.11.2.1 at schema v49; the service binary (~/go/bin/iql) differs from the one on PATH (~/.local/bin/iql), so both were updated'),
         ]),
]

LATER = [
    ('agy as the agent that answers', 'Only Claude Code is supported: agy here is an alias that skips permissions, which a read-only answerer must not do.'),
    ('Allowing a new phone from the Mac with a matching code', 'Needs a device-pairing design reviewed against Tailscale identity.'),
    ('Reminders pushed to a phone whose app is closed', 'Needs Web Push (VAPID keys, a push subscription per phone).'),
    ('Birthdays and anniversaries from contacts', 'Contacts do not hold dates yet.'),
    ('A terminal panel with the agent CLI', 'Deferred until Ask in words has been used.'),
    ('Guide examples that run on your own mail, with counts', 'After the Help home settles.'),
    ('Reply deadlines read from mail (“by Monday”)', 'Needs the waiting element to record a date.'),
]
