import { useId, useMemo, useState } from 'react';
import { AlertTriangle, CheckCircle2, CircleDashed, HelpCircle } from 'lucide-react';
import type { CalBin, Estimate, RouteStatus, WorkDay } from './api';
import { dayLabel, pct, STATUS_TEXT } from './format';

/**
 * Conversations stored per day: one series, one colour. Refusals and checks
 * are in each bar's tooltip and in the table, rather than a second series
 * competing for the same axis.
 *
 * Each bar is focusable and labelled, so the tooltip is reachable by
 * keyboard and read by a screen reader; "Show as table" gives every number.
 */
export const DailyBars = ({ days }: { days: WorkDay[] }) => {
  const [table, setTable] = useState(false);
  const [hover, setHover] = useState<number | null>(null);
  const tableId = useId();
  const max = Math.max(1, ...days.map(d => d.stored));
  const describe = (d: WorkDay) =>
    `${dayLabel(d.day, { weekday: 'short', month: 'short', day: 'numeric' })}: ${d.stored} stored, ${d.refused} refused, ${d.checked} checked`;

  if (days.every(d => d.stored === 0 && d.refused === 0 && d.checked === 0)) {
    return <p className="text-xs text-muted-foreground">Nothing was read in this period.</p>;
  }
  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">Conversations stored per day</p>
        <button type="button" onClick={() => setTable(t => !t)} aria-expanded={table} aria-controls={tableId}
          className="border border-border px-2 py-0.5 text-xs hover:bg-accent focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary">
          {table ? 'Show as chart' : 'Show as table'}
        </button>
      </div>
      {table ? (
        <div id={tableId} className="max-h-72 overflow-auto">
          <table className="w-full text-xs tabular-nums">
            <caption className="sr-only">Conversations stored, refused and checked per day</caption>
            <thead className="sticky top-0 bg-card text-left text-muted-foreground">
              <tr><th scope="col" className="py-1 font-medium">Day</th><th scope="col" className="text-right font-medium">Stored</th>
                <th scope="col" className="text-right font-medium">Refused</th><th scope="col" className="text-right font-medium">Checked</th></tr>
            </thead>
            <tbody className="divide-y divide-border">
              {[...days].reverse().filter(d => d.stored || d.refused || d.checked).map(d => (
                <tr key={d.day}><th scope="row" className="py-1 text-left font-normal">{dayLabel(d.day, { weekday: 'short', month: 'short', day: 'numeric' })}</th>
                  <td className="text-right">{d.stored}</td><td className="text-right">{d.refused}</td><td className="text-right">{d.checked}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div id={tableId} className="relative">
          <div className="flex items-stretch gap-2">
            <div className="flex w-8 flex-col justify-between text-right text-[10px] tabular-nums text-muted-foreground" aria-hidden>
              <span>{max}</span><span>0</span>
            </div>
            <ol className="relative flex h-32 flex-1 items-end gap-[2px] border-b border-border" aria-label="Conversations stored per day">
              {days.map((d, i) => (
                <li key={d.day} className="relative flex h-full flex-1 items-end">
                  <span
                    tabIndex={0}
                    role="img"
                    aria-label={describe(d)}
                    onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(h => (h === i ? null : h))}
                    onFocus={() => setHover(i)} onBlur={() => setHover(h => (h === i ? null : h))}
                    className="group flex h-full w-full items-end focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary"
                  >
                    <span className={`block w-full rounded-t-[4px] ${hover === i ? 'bg-primary' : 'bg-primary/70'}`}
                      style={{ height: d.stored ? `${Math.max(2, (d.stored / max) * 100)}%` : 0 }} />
                  </span>
                  {hover === i && (
                    <span role="tooltip" className="pointer-events-none absolute bottom-full left-1/2 z-10 mb-1 -translate-x-1/2 whitespace-nowrap border border-border bg-popover px-2 py-1 text-[11px] text-popover-foreground shadow-sm">
                      <span className="block font-medium">{dayLabel(d.day, { weekday: 'short', month: 'short', day: 'numeric' })}</span>
                      <span className="block tabular-nums">{d.stored} stored · {d.refused} refused · {d.checked} checked</span>
                    </span>
                  )}
                </li>
              ))}
            </ol>
          </div>
          <div className="ml-10 mt-1 flex justify-between text-[10px] text-muted-foreground" aria-hidden>
            <span>{dayLabel(days[0].day)}</span><span>{dayLabel(days[days.length - 1].day)}</span>
          </div>
        </div>
      )}
    </div>
  );
};

/**
 * An accuracy, the range it is probably in, and the target, on one track
 * from 50% to 100% — below half right, the detail does not matter. Drawn for
 * the eye only: the same numbers are written beside it.
 */
export const IntervalBar = ({ estimate, target }: { estimate?: Estimate; target: number }) => {
  const x = (v: number) => `${Math.min(100, Math.max(0, ((v - 0.5) / 0.5) * 100))}%`;
  return (
    <span className="relative block h-3 w-full min-w-24 bg-muted" aria-hidden>
      {estimate && (
        <>
          <span className="absolute inset-y-[3px] bg-primary/30" style={{ left: x(estimate.low), width: `calc(${x(estimate.high)} - ${x(estimate.low)})` }} />
          <span className="absolute inset-y-0 w-[3px] -translate-x-1/2 rounded-sm bg-primary" style={{ left: x(estimate.accuracy) }} />
        </>
      )}
      <span className="absolute -inset-y-[2px] w-[2px] -translate-x-1/2 bg-foreground" style={{ left: x(target) }} />
    </span>
  );
};

export const IntervalLegend = () => (
  <p className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
    <span className="flex items-center gap-1.5"><span className="inline-block h-3 w-[3px] rounded-sm bg-primary" aria-hidden /> measured accuracy</span>
    <span className="flex items-center gap-1.5"><span className="inline-block h-2 w-5 bg-primary/30" aria-hidden /> where it probably is (95%)</span>
    <span className="flex items-center gap-1.5"><span className="inline-block h-3.5 w-[2px] bg-foreground" aria-hidden /> target</span>
    <span>Track runs from 50% to 100%.</span>
  </p>
);

const STATUS: Record<RouteStatus, { icon: typeof CheckCircle2; className: string }> = {
  meeting: { icon: CheckCircle2, className: 'text-emerald-700 dark:text-emerald-400' },
  uncertain: { icon: HelpCircle, className: 'text-amber-700 dark:text-amber-400' },
  missing: { icon: AlertTriangle, className: 'text-destructive' },
  unmeasured: { icon: CircleDashed, className: 'text-muted-foreground' },
};

export const StatusBadge = ({ status }: { status: RouteStatus }) => {
  const s = STATUS[status];
  const Icon = s.icon;
  return (
    <span className={`inline-flex items-center gap-1 whitespace-nowrap text-xs ${s.className}`} title={STATUS_TEXT[status].describe}>
      <Icon className="h-3.5 w-3.5" aria-hidden />{STATUS_TEXT[status].label}
    </span>
  );
};


/**
 * Whether confidence means what it says: in each band of stated confidence,
 * how often the answers were right. A table, because the reader compares two
 * numbers per row; the inline bar shows the gap.
 */
export const CalibrationTable = ({ bins, unrated }: { bins: CalBin[]; unrated: number }) => {
  const used = useMemo(() => bins.filter(b => b.n > 0), [bins]);
  if (used.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        No random check has an answer that said how sure it was{unrated ? ` (${unrated} checked answers gave no confidence)` : ''}.
        Ask the harness to include <code className="font-mono">confidence</code> in its answers.
      </p>
    );
  }
  return (
    <div className="space-y-2">
      <p className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
        <span className="flex items-center gap-1.5"><span className="inline-block h-2 w-5 bg-primary/70" aria-hidden /> how often right</span>
        <span className="flex items-center gap-1.5"><span className="inline-block h-3.5 w-[2px] bg-foreground" aria-hidden /> how sure the answers said they were</span>
      </p>
      <div className="overflow-x-auto">
        <table className="w-full text-xs tabular-nums">
          <caption className="sr-only">Stated confidence against how often answers were right, on random checks</caption>
          <thead className="text-left text-muted-foreground">
            <tr>
              <th scope="col" className="py-1 font-medium">Said it was</th>
              <th scope="col" className="text-right font-medium">Answers</th>
              <th scope="col" className="text-right font-medium">Right</th>
              <th scope="col" className="w-1/2 pl-4 font-medium">Gap</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {used.map(b => {
              const right = b.right / b.n;
              const gap = right - b.mean;
              return (
                <tr key={b.lo}>
                  <th scope="row" className="py-1.5 text-left font-normal">{pct(b.lo)}–{pct(b.hi)} sure</th>
                  <td className="text-right">{b.n}</td>
                  <td className="text-right">{pct(right)}</td>
                  <td className="pl-4">
                    <span className="flex items-center gap-2">
                      <span className="relative block h-2.5 flex-1 bg-muted" aria-hidden>
                        <span className="absolute inset-y-0 left-0 rounded-r-[4px] bg-primary/70" style={{ width: pct(right) }} />
                        <span className="absolute -inset-y-[2px] w-[2px] -translate-x-1/2 bg-foreground" style={{ left: pct(b.mean) }} />
                      </span>
                      <span className="w-36 whitespace-nowrap text-right text-muted-foreground">
                        {b.n < 10 ? 'too few to tell' : Math.abs(gap) < 0.05 ? 'about right' : gap < 0 ? `over-sure by ${pct(-gap)}` : `under-sure by ${pct(gap)}`}
                      </span>
                    </span>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {unrated > 0 && <p className="text-[11px] text-muted-foreground">{unrated} checked answers gave no confidence and are not shown.</p>}
    </div>
  );
};
