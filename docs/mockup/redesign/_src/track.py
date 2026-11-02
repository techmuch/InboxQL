"""Set task statuses in plan_data.py and re-render plan.html.
usage: python3 track.py p1 all done   |   python3 track.py p2 3 done "note"
"""
import sys, subprocess, datetime
ns = {}
exec(open('plan_data.py').read(), ns)
pid, which, status = sys.argv[1], sys.argv[2], sys.argv[3]
note = sys.argv[4] if len(sys.argv) > 4 else None
for p in ns['PHASES']:
    if p['id'] != pid:
        continue
    for i, t in enumerate(p['tasks'], 1):
        if which == 'all' or i in [int(x) for x in which.split(',')]:
            p['tasks'][i - 1] = (t[0], status, t[2] if note is None else note)
lines = ['"""The redesign implementation plan, as data. plan.py renders it.\n\nstatus: todo | doing | done | later\n"""\n',
         f"UPDATED = {datetime.date.today().isoformat()!r}\n", 'PHASES = [']
for p in ns['PHASES']:
    lines.append('    dict(' + ', '.join(f'{k}={v!r}' for k, v in p.items() if k != 'tasks') + ',\n         tasks=[')
    for t in p['tasks']:
        lines.append(f'             {t!r},')
    lines.append('         ]),')
lines.append(']\n\nLATER = [')
for l in ns['LATER']:
    lines.append(f'    {l!r},')
lines.append(']\n')
open('plan_data.py', 'w').write('\n'.join(lines))
subprocess.run(['python3', 'plan.py'], check=True)
