import type { Occurrence } from './api';
import { addDays, fmtDay, fmtTime, monthGrid, sameDay, startOfDay } from './dates';

const coversDay = (o: Occurrence, d: Date) => {
  const s = new Date(o.start);
  const e = new Date(o.end);
  const day = startOfDay(d);
  return s < addDays(day, 1) && e > day;
};

/** The month view: six weeks, three events a day and a count of the rest. */
export const MonthGrid = ({ anchor, occurrences, colourOf, selected, onSelect, onCreate, onOpenDay }: {
  anchor: Date;
  occurrences: Occurrence[];
  colourOf: (o: Occurrence) => string;
  selected: string | null;
  onSelect: (o: Occurrence) => void;
  onCreate: (day: Date) => void;
  onOpenDay: (day: Date) => void;
}) => {
  const days = monthGrid(anchor);
  const today = new Date();
  return (
    <div className="grid h-full grid-cols-7 grid-rows-[auto_repeat(6,minmax(0,1fr))]">
      {days.slice(0, 7).map(d => (
        <div key={'h' + d.getDay()} className="border-b border-border px-2 py-1 text-[11px] text-muted-foreground">
          {d.toLocaleDateString(undefined, { weekday: 'short' })}
        </div>
      ))}
      {days.map(d => {
        const items = occurrences.filter(o => coversDay(o, d));
        const inMonth = d.getMonth() === anchor.getMonth();
        return (
          <div
            key={d.toISOString()}
            className={`min-h-0 overflow-hidden border-b border-l border-border/60 p-1 ${inMonth ? '' : 'bg-muted/30'}`}
            onDoubleClick={() => onCreate(d)}
          >
            <button
              onClick={() => onOpenDay(d)}
              className={`mb-0.5 px-1 text-[11px] ${sameDay(d, today) ? 'bg-primary text-primary-foreground font-semibold' : inMonth ? 'text-foreground' : 'text-muted-foreground'}`}
              title={`Open ${fmtDay(d)}`}
            >
              {d.getDate()}
            </button>
            {items.slice(0, 3).map(o => (
              <button
                key={o.eventId + o.start}
                onClick={() => onSelect(o)}
                className={`flex w-full items-center gap-1 truncate px-1 text-left text-[11px] hover:bg-accent ${selected === o.eventId ? 'bg-accent' : ''} ${o.myStatus === 'declined' ? 'line-through opacity-50' : ''}`}
                title={o.summary}
              >
                <span className="h-2 w-2 shrink-0" style={{ background: colourOf(o) }} />
                {!o.allDay && <span className="text-muted-foreground">{fmtTime(new Date(o.start))}</span>}
                <span className={`truncate ${o.proposed ? 'italic' : ''}`}>{o.summary}</span>
              </button>
            ))}
            {items.length > 3 && (
              <button onClick={() => onOpenDay(d)} className="px-1 text-[11px] text-muted-foreground hover:text-foreground">
                {items.length - 3} more
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
};

/** The agenda: what is on, day by day, as a list you can read and arrow through. */
export const Agenda = ({ occurrences, colourOf, selected, onSelect }: {
  occurrences: Occurrence[];
  colourOf: (o: Occurrence) => string;
  selected: string | null;
  onSelect: (o: Occurrence) => void;
}) => {
  if (occurrences.length === 0) {
    return <p className="p-8 text-sm text-muted-foreground">Nothing on in these weeks.</p>;
  }
  const groups: { day: Date; items: Occurrence[] }[] = [];
  for (const o of occurrences) {
    const d = startOfDay(new Date(o.start));
    const last = groups[groups.length - 1];
    if (last && sameDay(last.day, d)) last.items.push(o);
    else groups.push({ day: d, items: [o] });
  }
  return (
    <div className="h-full overflow-auto px-6 py-4">
      {groups.map(g => (
        <section key={g.day.toISOString()} className="mb-5">
          <h3 className={`mb-1 text-xs font-semibold uppercase tracking-wider ${sameDay(g.day, new Date()) ? 'text-primary' : 'text-muted-foreground'}`}>
            {fmtDay(g.day)}
          </h3>
          <ul>
            {g.items.map(o => (
              <li key={o.eventId + o.start}>
                <button
                  onClick={() => onSelect(o)}
                  className={`flex w-full items-center gap-3 border-b border-border/40 px-2 py-1.5 text-left text-sm hover:bg-accent ${selected === o.eventId ? 'bg-accent' : ''}`}
                >
                  <span className="h-3 w-1 shrink-0" style={{ background: colourOf(o) }} />
                  <span className="w-32 shrink-0 text-xs text-muted-foreground tabular-nums">
                    {o.allDay ? 'All day' : `${fmtTime(new Date(o.start))} – ${fmtTime(new Date(o.end))}`}
                  </span>
                  <span className={`flex-1 truncate ${o.myStatus === 'declined' ? 'line-through opacity-50' : ''}`}>{o.summary}</span>
                  {o.proposed && <span className="text-[10px] uppercase tracking-wider text-amber-600">proposed</span>}
                  {o.myStatus === 'needs-action' && <span className="text-[10px] uppercase tracking-wider text-amber-600">not answered</span>}
                  {o.location && <span className="max-w-[12rem] truncate text-xs text-muted-foreground">{o.location}</span>}
                </button>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
};
