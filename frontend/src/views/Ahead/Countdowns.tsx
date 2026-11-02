import { useEffect, useId, useRef, useState } from 'react';
import { countdowns, inDays, shortDay, useImportantDates, type ImportantDate } from '../../lib/importantDates';

/**
 * Countdowns: the dates the person marked important, soonest first.
 *
 * Each card is one list item with one spoken name — "Q4 report, Tue Nov 10,
 * in 10 days" — and the visible parts hidden from assistive technology, so a
 * screen reader hears a sentence rather than four fragments.
 */
export const Countdowns = ({ compact = false, limit, onMark }: { compact?: boolean; limit?: number; onMark?: () => void }) => {
  const { dates, load, remove } = useImportantDates();
  useEffect(() => { if (dates == null) void load(); }, [dates, load]);
  if (dates == null) return <p className="text-sm text-muted-foreground">Loading…</p>;
  const all = countdowns(dates);
  const shown = limit ? all.slice(0, limit) : all;
  if (shown.length === 0) {
    return (
      <p className="border border-dashed border-border px-4 py-3 text-sm text-muted-foreground">
        No important dates yet. Mark a deadline, a trip or an anniversary and it is counted down here, with reminders as it approaches.
        {onMark && <> <button type="button" className="underline" onClick={onMark}>Mark a date</button></>}
      </p>
    );
  }
  return (
    <ul className="grid gap-2" style={{ gridTemplateColumns: `repeat(auto-fill, minmax(${compact ? '10.5rem' : '12rem'}, 1fr))` }} aria-label="Important dates, soonest first">
      {shown.map(c => {
        const spoken = `${c.date.title}, ${shortDay(c.on)}, ${inDays(c.days)}${c.start && c.start.days >= 0 ? `. Start by ${shortDay(c.start.on)}, ${inDays(c.start.days)}` : ''}${c.soon ? '. Soon.' : ''}`;
        return (
          <li key={c.date.id} aria-label={spoken}
            className={`flex flex-col gap-0.5 border bg-card px-3 py-2 ${c.soon ? 'border-amber-600 shadow-[inset_3px_0_0_rgb(217_119_6)] dark:border-amber-400' : 'border-border'}`}>
            <span aria-hidden="true" className={`${compact ? 'text-xl' : 'text-3xl'} font-bold tabular-nums leading-tight`}>
              {c.days}<span className="ml-1 text-sm font-semibold text-muted-foreground">{c.days === 1 ? 'day' : 'days'}</span>
            </span>
            <span aria-hidden="true" className="font-semibold leading-snug">{c.date.title}</span>
            <span aria-hidden="true" className="text-sm text-muted-foreground">{shortDay(c.on)}{c.date.yearly ? ' · every year' : ''}</span>
            {!compact && c.start && c.start.days >= 0 && (
              <span aria-hidden="true" className="text-sm">Start by <strong>{shortDay(c.start.on)}</strong> — {inDays(c.start.days)}</span>
            )}
            {!compact && (
              <button type="button" className="mt-1 self-start text-xs text-muted-foreground underline"
                onClick={() => void remove(c.date.id)} aria-label={`No longer important: ${c.date.title}`}>No longer important</button>
            )}
          </li>
        );
      })}
    </ul>
  );
};

const LEADS: { days: number; label: string }[] = [
  { days: 0, label: 'None' }, { days: 7, label: '1 week' }, { days: 14, label: '2 weeks' }, { days: 42, label: '6 weeks' },
];

/**
 * Marking a date as important: what, when, whether it repeats, and how long
 * before it there is something to do.
 */
export const MarkDateDialog = ({ initial, onClose }: { initial?: Partial<ImportantDate>; onClose: () => void }) => {
  const save = useImportantDates(s => s.save);
  const [title, setTitle] = useState(initial?.title ?? '');
  const [date, setDate] = useState(initial?.date ?? '');
  const [lead, setLead] = useState(initial?.leadDays ?? 0);
  const [yearly, setYearly] = useState(initial?.yearly ?? false);
  const [error, setError] = useState<string | null>(null);
  const first = useRef<HTMLInputElement>(null);
  const id = useId();
  useEffect(() => {
    first.current?.focus();
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', esc);
    return () => window.removeEventListener('keydown', esc);
  }, [onClose]);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await save({ ...initial, title, date, leadDays: lead, yearly, origin: 'human' });
      onClose();
    } catch (err) {
      setError((err as Error).message);
    }
  };
  const field = 'w-full border border-border bg-background px-2 py-1.5 text-sm';
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/30 pt-[12vh]" onMouseDown={onClose}>
      <form role="dialog" aria-modal="true" aria-labelledby={`${id}-t`} onMouseDown={e => e.stopPropagation()} onSubmit={submit}
        className="w-[28rem] max-w-[95vw] border border-border bg-card p-5 shadow-xl">
        <h2 id={`${id}-t`} className="mb-3 text-lg font-semibold">Mark a date as important</h2>
        <label className="mb-1 block text-sm font-medium" htmlFor={`${id}-title`}>What</label>
        <input id={`${id}-title`} ref={first} className={field} value={title} onChange={e => setTitle(e.target.value)} required placeholder="Passport expires" />
        <label className="mb-1 mt-3 block text-sm font-medium" htmlFor={`${id}-date`}>When</label>
        <input id={`${id}-date`} type="date" className={field} value={date} onChange={e => setDate(e.target.value)} required />
        <fieldset className="mt-3">
          <legend className="mb-1 text-sm font-medium">Lead time — when to start getting ready</legend>
          <div className="flex flex-wrap gap-3">
            {LEADS.map(l => (
              <label key={l.days} className="flex items-center gap-1.5 text-sm">
                <input type="radio" name={`${id}-lead`} checked={lead === l.days} onChange={() => setLead(l.days)} /> {l.label}
              </label>
            ))}
          </div>
        </fieldset>
        <label className="mt-3 flex items-center gap-2 text-sm">
          <input type="checkbox" checked={yearly} onChange={e => setYearly(e.target.checked)} /> Every year (an anniversary or birthday)
        </label>
        <p className="mt-3 text-xs text-muted-foreground">Reminders arrive at 8 in the morning: when the lead time starts, a week before, the day before and that morning.</p>
        {error && <p role="alert" className="mt-2 text-sm text-destructive">{error}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="iql-btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="iql-btn primary">Mark important</button>
        </div>
      </form>
    </div>
  );
};
