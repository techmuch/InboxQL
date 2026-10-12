import { useEffect, useMemo, useRef, useState } from 'react';
import type { Occurrence } from './api';
import { DAY_MS, addDays, fmtTime, layoutColumns, minutesIntoDay, sameDay, snap15, startOfDay } from './dates';

/**
 * The day and week views: a column per day, an hour per row.
 *
 * Click an empty slot to start an event there. Drag an event to move it —
 * across days too, in the week view — and drag its bottom edge to change its
 * length. Everything snaps to a quarter of an hour. A change is handed up
 * rather than saved here, because a recurring event first needs the "this
 * one, or all of them?" question asked.
 */

export const HOUR_PX = 48;
const PX_PER_MIN = HOUR_PX / 60;

interface Props {
  days: Date[];
  occurrences: Occurrence[];
  calendars: Map<string, { color?: string; writable: boolean }>;
  selected: string | null;
  onSelect: (o: Occurrence) => void;
  onCreate: (start: Date, end: Date, allDay: boolean) => void;
  onMove: (o: Occurrence, start: Date, end: Date) => void;
}

interface Drag {
  occ: Occurrence;
  mode: 'move' | 'resize';
  startX: number;
  startY: number;
  dayWidth: number;
  dMinutes: number;
  dDays: number;
}

export const TimeGrid = ({ days, occurrences, calendars, selected, onSelect, onCreate, onMove }: Props) => {
  const scroller = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(new Date());
  const [drag, setDrag] = useState<Drag | null>(null);

  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 60_000);
    return () => clearInterval(t);
  }, []);

  // Open at the working day, not at midnight.
  useEffect(() => {
    if (scroller.current) scroller.current.scrollTop = 7 * HOUR_PX;
  }, []);

  const { timed, allDay } = useMemo(() => {
    const timed: Occurrence[] = [];
    const allDay: Occurrence[] = [];
    for (const o of occurrences) {
      const s = new Date(o.start);
      const e = new Date(o.end);
      if (o.allDay || e.getTime() - s.getTime() >= DAY_MS) allDay.push(o);
      else timed.push(o);
    }
    return { timed, allDay };
  }, [occurrences]);

  const colourOf = (o: Occurrence) => o.color || calendars.get(o.calendarId)?.color || '#2563eb';
  const writable = (o: Occurrence) => !!calendars.get(o.calendarId)?.writable;

  useEffect(() => {
    if (!drag) return;
    const move = (e: PointerEvent) => {
      const dMinutes = snap15((e.clientY - drag.startY) / PX_PER_MIN);
      const dDays = drag.mode === 'move' && days.length > 1 ? Math.round((e.clientX - drag.startX) / drag.dayWidth) : 0;
      setDrag(d => (d ? { ...d, dMinutes, dDays } : d));
    };
    const up = () => {
      setDrag(d => {
        if (d && (d.dMinutes !== 0 || d.dDays !== 0)) {
          const s = new Date(d.occ.start);
          const e = new Date(d.occ.end);
          const shift = d.dMinutes * 60_000 + d.dDays * DAY_MS;
          if (d.mode === 'move') onMove(d.occ, new Date(s.getTime() + shift), new Date(e.getTime() + shift));
          else onMove(d.occ, s, new Date(Math.max(e.getTime() + d.dMinutes * 60_000, s.getTime() + 15 * 60_000)));
        }
        return null;
      });
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up, { once: true });
    return () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
  }, [drag, days.length, onMove]);

  const startDrag = (e: React.PointerEvent, occ: Occurrence, mode: Drag['mode']) => {
    if (!writable(occ) || e.button !== 0) return;
    e.stopPropagation();
    const col = (e.currentTarget as HTMLElement).closest('[data-day-col]') as HTMLElement | null;
    setDrag({ occ, mode, startX: e.clientX, startY: e.clientY, dayWidth: col?.offsetWidth ?? 100, dMinutes: 0, dDays: 0 });
  };

  const clickSlot = (day: Date, e: React.MouseEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const minutes = Math.max(0, Math.min(24 * 60 - 30, snap15((e.clientY - rect.top) / PX_PER_MIN - 15)));
    const start = new Date(startOfDay(day).getTime() + minutes * 60_000);
    onCreate(start, new Date(start.getTime() + 60 * 60_000), false);
  };

  return (
    <div className="flex h-full flex-col">
      {/* Day headings and the all-day row. */}
      <div className="flex border-b border-border shrink-0">
        <div className="w-14 shrink-0" />
        {days.map(d => {
          const today = sameDay(d, now);
          const items = allDay.filter(o => {
            const s = new Date(o.start);
            const e = new Date(o.end);
            return s < addDays(startOfDay(d), 1) && e > startOfDay(d);
          });
          return (
            <div key={d.toISOString()} className="flex-1 min-w-0 border-l border-border/60 px-1 pb-1">
              <button
                className="w-full pt-1 text-center text-xs"
                onClick={() => onCreate(startOfDay(d), addDays(startOfDay(d), 1), true)}
                title="Add an all-day event"
              >
                <span className="text-muted-foreground">{d.toLocaleDateString(undefined, { weekday: 'short' })} </span>
                <span className={today ? 'inline-flex h-6 w-6 items-center justify-center bg-primary text-primary-foreground font-semibold' : 'font-semibold'}>
                  {d.getDate()}
                </span>
              </button>
              <div className="space-y-0.5">
                {items.map(o => (
                  <button
                    key={o.eventId + o.start}
                    onClick={() => onSelect(o)}
                    className={`block w-full truncate px-1.5 py-0.5 text-left text-[11px] text-white ${selected === o.eventId ? 'ring-2 ring-offset-1 ring-primary' : ''} ${o.proposed ? 'opacity-70' : ''}`}
                    style={{ background: colourOf(o) }}
                    title={o.summary}
                  >
                    {o.summary}
                  </button>
                ))}
              </div>
            </div>
          );
        })}
      </div>

      <div ref={scroller} className="flex-1 overflow-auto">
        <div className="relative flex" style={{ height: 24 * HOUR_PX }}>
          <div className="w-14 shrink-0 relative">
            {Array.from({ length: 24 }, (_, h) => (
              <div key={h} className="absolute right-2 -translate-y-1/2 text-[10px] text-muted-foreground" style={{ top: h * HOUR_PX }}>
                {h === 0 ? '' : new Date(2000, 0, 1, h).toLocaleTimeString(undefined, { hour: 'numeric' })}
              </div>
            ))}
          </div>
          {days.map(d => {
            const dayStart = startOfDay(d);
            const dayEnd = addDays(dayStart, 1);
            const items = layoutColumns(
              timed
                .map(o => ({ o, start: new Date(o.start), end: new Date(o.end) }))
                .filter(x => x.start < dayEnd && x.end > dayStart),
            );
            return (
              <div
                key={d.toISOString()}
                data-day-col
                className="relative flex-1 min-w-0 border-l border-border/60 cursor-cell"
                onClick={e => clickSlot(d, e)}
              >
                {Array.from({ length: 24 }, (_, h) => (
                  <div key={h} className="absolute inset-x-0 border-t border-border/40" style={{ top: h * HOUR_PX }} />
                ))}
                {sameDay(d, now) && (
                  <div className="absolute inset-x-0 z-20 border-t-2 border-red-500 pointer-events-none" style={{ top: minutesIntoDay(now) * PX_PER_MIN }} />
                )}
                {items.map(({ o, start, end, col, cols }) => {
                  const dragging = drag?.occ.eventId === o.eventId && drag.occ.start === o.start ? drag : null;
                  const s = Math.max(0, (start.getTime() - dayStart.getTime()) / 60_000);
                  const e = Math.min(24 * 60, (end.getTime() - dayStart.getTime()) / 60_000);
                  let top = s * PX_PER_MIN;
                  let height = Math.max(18, (e - s) * PX_PER_MIN - 2);
                  let leftShift = 0;
                  if (dragging) {
                    if (dragging.mode === 'move') {
                      top += dragging.dMinutes * PX_PER_MIN;
                      leftShift = dragging.dDays * dragging.dayWidth;
                    } else {
                      height = Math.max(18, height + dragging.dMinutes * PX_PER_MIN);
                    }
                  }
                  const declined = o.myStatus === 'declined';
                  const unanswered = o.myStatus === 'needs-action';
                  return (
                    <div
                      key={o.eventId + o.start}
                      role="button"
                      tabIndex={0}
                      aria-label={`${o.summary}, ${fmtTime(start)} to ${fmtTime(end)}`}
                      onClick={ev => { ev.stopPropagation(); onSelect(o); }}
                      onKeyDown={ev => { if (ev.key === 'Enter') onSelect(o); }}
                      onPointerDown={ev => startDrag(ev, o, 'move')}
                      className={`absolute z-10 overflow-hidden px-1.5 py-0.5 text-[11px] leading-tight text-white shadow-sm select-none
                        ${writable(o) ? 'cursor-grab active:cursor-grabbing' : 'cursor-pointer'}
                        ${selected === o.eventId ? 'ring-2 ring-offset-1 ring-primary z-30' : ''}
                        ${dragging ? 'opacity-80 z-40' : ''}`}
                      style={{
                        top, height,
                        left: `calc(${(col / cols) * 100}% + 2px + ${leftShift}px)`,
                        width: `calc(${100 / cols}% - 4px)`,
                        background: o.proposed || unanswered
                          ? `repeating-linear-gradient(45deg, ${colourOf(o)}, ${colourOf(o)} 6px, ${colourOf(o)}cc 6px, ${colourOf(o)}cc 12px)`
                          : colourOf(o),
                        opacity: declined ? 0.45 : undefined,
                        textDecoration: declined ? 'line-through' : undefined,
                      }}
                      title={o.proposed ? 'Proposed — not yet approved' : unanswered ? 'You have not answered this invitation' : undefined}
                    >
                      <div className="font-semibold truncate">{o.summary || '(no title)'}</div>
                      {height > 30 && <div className="truncate opacity-90">{fmtTime(start)} – {fmtTime(end)}{o.location ? ` · ${o.location}` : ''}</div>}
                      {writable(o) && (
                        <div
                          className="absolute inset-x-0 bottom-0 h-1.5 cursor-ns-resize"
                          onPointerDown={ev => startDrag(ev, o, 'resize')}
                          aria-hidden="true"
                        />
                      )}
                    </div>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
};
