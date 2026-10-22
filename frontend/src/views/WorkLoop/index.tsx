import { useCallback, useEffect, useId, useState } from 'react';
import { Loader2, RefreshCw } from 'lucide-react';
import { Help } from '../Help';
import { Disagreements } from './Disagreements';
import { CalibrationTable, DailyBars, IntervalBar, IntervalLegend, StatusBadge } from './Charts';
import { fillDays, pct, statusDescription } from './format';
import { getBacklog, getMetrics, setTarget, type Cost, type ElementMetrics, type Metrics, type Period } from './api';

const PERIODS: { id: Period; label: string }[] = [
  { id: '7d', label: '7 days' },
  { id: '30d', label: '30 days' },
  { id: '90d', label: '90 days' },
  { id: 'all', label: 'All time' },
];
const TARGETS = [0.8, 0.85, 0.9, 0.95, 0.97, 0.99];
const COSTS: { id: Cost; label: string }[] = [
  { id: 'high', label: 'High' },
  { id: 'medium', label: 'Medium' },
  { id: 'low', label: 'Low' },
];
const ACTIONS: { kind: string; label: string; undone: string }[] = [
  { kind: 'ticket', label: 'Proposed cards', undone: 'rejected' },
  { kind: 'event', label: 'Proposed events', undone: 'rejected' },
  { kind: 'label', label: 'Labels added', undone: 'taken off' },
  { kind: 'archive', label: 'Conversations archived', undone: 'moved back' },
];

const num = (n: number) => n.toLocaleString();
const duration = (s: number) => (s < 90 ? `${Math.round(s)} s` : s < 5400 ? `${Math.round(s / 60)} min` : `${(s / 3600).toFixed(1)} h`);

const Section = ({ id, title, note, children, actions }: {
  id: string; title: string; note?: React.ReactNode; children: React.ReactNode; actions?: React.ReactNode;
}) => (
  <section aria-labelledby={id} className="border border-border bg-card p-4">
    <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
      <h2 id={id} className="text-sm font-semibold">{title}</h2>
      {actions}
    </div>
    {note && <p className="-mt-2 mb-3 text-xs text-muted-foreground">{note}</p>}
    {children}
  </section>
);

const Tile = ({ label, value, detail, tone }: { label: string; value: string; detail?: string; tone?: 'warn' }) => (
  <div className="border border-border bg-card p-3">
    <dt className="text-xs text-muted-foreground">{label}</dt>
    <dd className={`mt-1 text-2xl font-semibold tabular-nums ${tone === 'warn' ? 'text-amber-700 dark:text-amber-400' : ''}`}>{value}</dd>
    {detail && <dd className="mt-0.5 text-[11px] text-muted-foreground">{detail}</dd>}
  </div>
);

/**
 * The Work loop: how your AI harness reads mail through `iql work` — how much
 * effort it takes, how often it is right, and where it should read carefully.
 *
 * Ordered by what asks something of you: disagreements to settle come first,
 * then accuracy by element, the targets that steer it, then effort.
 */
export const WorkLoop = () => {
  const [period, setPeriod] = useState<Period>('30d');
  const [model, setModel] = useState('');
  const [m, setM] = useState<Metrics | null>(null);
  const [backlog, setBacklog] = useState<Record<string, number> | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState('');
  const periodName = useId();

  const load = useCallback(async () => {
    setLoading(true); setError(null);
    try {
      setM(await getMetrics(period, model));
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not load the measures');
    } finally {
      setLoading(false);
    }
  }, [period, model]);
  useEffect(() => { void load(); }, [load]);

  // The backlog reads the whole mailbox, so it arrives after the rest.
  useEffect(() => {
    let live = true;
    getBacklog()
      .then(r => { if (live) setBacklog(Object.fromEntries(r.elements.map(e => [e.element, e.pending]))); })
      .catch(() => { if (live) setBacklog({}); });
    return () => { live = false; };
  }, []);

  const changeTarget = async (e: ElementMetrics, target: number, cost: Cost) => {
    setSaved('');
    try {
      await setTarget(e.element, target, cost);
      setSaved(`Saved: ${e.element} must be ${pct(target)} right; a wrong answer costs ${cost}.`);
      void load();
    } catch (err) {
      setSaved(err instanceof Error ? err.message : 'Could not save that');
    }
  };

  const idle = m && m.reading.stored === 0 && m.reading.refused === 0 && m.checks.random.n + m.checks.targeted.n === 0
    && m.elements.every(e => e.answered === 0);
  const control = 'border border-border bg-background px-2 py-1 text-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary';

  return (
    <div className="h-full overflow-auto p-6">
      <div className="mx-auto max-w-5xl space-y-5">
        <header className="space-y-3">
          <div>
            <h1 className="flex items-center gap-2 text-xl font-semibold">Work loop <Help topic="work-loop" /></h1>
            <p className="text-sm text-muted-foreground">
              How your AI harness reads mail with <code className="font-mono text-xs">iql work</code>: how often it is right,
              how much effort it takes, and where it should read more carefully.
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <fieldset className="flex items-center gap-1">
              <legend className="sr-only">Period</legend>
              {PERIODS.map(p => (
                <label key={p.id} className={`cursor-pointer border px-2 py-1 text-xs focus-within:ring-1 focus-within:ring-primary ${period === p.id ? 'border-primary bg-primary/10 font-medium' : 'border-border hover:bg-accent'}`}>
                  <input type="radio" name={periodName} value={p.id} checked={period === p.id} onChange={() => setPeriod(p.id)} className="sr-only" />
                  {p.label}
                </label>
              ))}
            </fieldset>
            <label className="flex items-center gap-1.5 text-xs">
              <span className="text-muted-foreground">Model</span>
              <select value={model} onChange={e => setModel(e.target.value)} className={control}>
                <option value="">All models</option>
                {(m?.models ?? []).map(x => <option key={x} value={x}>{x}</option>)}
              </select>
            </label>
            <button type="button" onClick={() => void load()} className={`${control} inline-flex items-center gap-1 hover:bg-accent`}>
              {loading ? <Loader2 className="h-3 w-3 animate-spin" aria-hidden /> : <RefreshCw className="h-3 w-3" aria-hidden />} Refresh
            </button>
            <span role="status" aria-live="polite" className="sr-only">{loading ? 'Loading measures' : ''}</span>
          </div>
        </header>

        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        {!m && loading && <p className="text-sm text-muted-foreground">Loading…</p>}

        {m && (
          <>
            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6" aria-label="Summary">
              <Tile label="Conversations read" value={num(m.reading.stored)} detail={m.reading.perDay ? `${m.reading.perDay.toFixed(1)} a day when reading` : undefined} />
              <Tile label="Right, on random checks" value={m.accuracy ? pct(m.accuracy.accuracy) : '—'}
                detail={m.accuracy
                  ? `probably ${pct(m.accuracy.low)}–${pct(m.accuracy.high)}, ${m.accuracy.n} checks${m.accuracy.open ? `, ${m.accuracy.open} still to settle` : ''}`
                  : 'not measured yet'} />
              <Tile label="Waiting for you" value={num(m.checks.open)} detail="disagreements to settle" tone={m.checks.open > 0 ? 'warn' : undefined} />
              <Tile label="Guesses sent back untouched" value={pct(m.reading.guessedShare)} detail="of answers stored" />
              <Tile label="Refused" value={pct(m.reading.refusalRate)} detail={`${num(m.reading.refused)} submission${m.reading.refused === 1 ? '' : 's'} to redo`} />
              <Tile label="Checks due" value={num(m.checks.due)} detail={`${num(m.flagged)} flagged for a careful read`} />
            </dl>

            {idle && (
              <Section id="wl-start" title="Nothing measured yet">
                <p className="text-sm">Your harness reads mail in two steps, then checks a share of what it read:</p>
                <pre className="mt-2 overflow-x-auto bg-muted p-3 text-xs">{`iql work next --limit 10 --template /tmp/answers.json --model <id>
iql --json work submit /tmp/answers.json
iql work audit next --limit 4 --template /tmp/check.json --model <another id>
iql --json work audit submit /tmp/check.json`}</pre>
                <p className="mt-2 text-xs text-muted-foreground">Ask it to run <code className="font-mono">iql work guide</code> first. What it does shows up here.</p>
              </Section>
            )}

            <Disagreements onSettled={() => void load()} />

            <Section id="wl-elements" title="How often each kind of answer is right"
              note="Accuracy comes from random checks only: targeted checks look where mistakes are likely, so they would make it look worse than it is.">
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <caption className="sr-only">Answers, guesses, checks and measured accuracy for each element</caption>
                  <thead className="text-left text-muted-foreground">
                    <tr>
                      <th scope="col" className="py-1.5 pr-3 font-medium">Element</th>
                      <th scope="col" className="pr-3 text-right font-medium">Answered</th>
                      <th scope="col" className="pr-3 text-right font-medium"><abbr title="Answers that went back exactly as the template guessed them" className="no-underline">Untouched guesses</abbr></th>
                      <th scope="col" className="pr-3 text-right font-medium">Checked</th>
                      <th scope="col" className="w-72 pr-3 font-medium">Right</th>
                      <th scope="col" className="pr-3 text-right font-medium">Your reviews</th>
                      <th scope="col" className="pr-3 text-right font-medium">Still to read</th>
                      <th scope="col" className="font-medium">Status</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {m.elements.map(e => {
                      const est = e.route.estimate;
                      return (
                        <tr key={e.element}>
                          <th scope="row" className="py-2 pr-3 text-left font-medium">
                            {e.element}
                            {e.about && <span className="block font-normal text-[11px] text-muted-foreground">{e.about}</span>}
                          </th>
                          <td className="pr-3 text-right tabular-nums">{num(e.answered)}</td>
                          <td className="pr-3 text-right tabular-nums">{e.answered ? pct(e.prefilled / e.answered) : '—'}</td>
                          <td className="pr-3 text-right tabular-nums">
                            {e.route.random.n}
                            {e.route.targeted.n > 0 && <span className="block text-[11px] text-muted-foreground">+{e.route.targeted.n} targeted</span>}
                          </td>
                          <td className="pr-3">
                            <span className="flex items-center gap-2">
                              <IntervalBar estimate={est} target={e.route.target} />
                              <span className="w-32 shrink-0 tabular-nums">
                                {est ? <>{pct(est.accuracy)} <span className="text-[11px] text-muted-foreground">({pct(est.low)}–{pct(est.high)})</span></> : <span className="text-muted-foreground">—</span>}
                                <span className="block text-[11px] text-muted-foreground">target {pct(e.route.target)}</span>
                              </span>
                            </span>
                          </td>
                          <td className="pr-3 text-right tabular-nums">{e.reviewed ? `${e.reviewedRight} of ${e.reviewed}` : '—'}</td>
                          <td className="pr-3 text-right tabular-nums">{e.unit === 'sender' ? '—' : backlog === null ? <span aria-label="counting">…</span> : num(backlog[e.element] ?? 0)}</td>
                          <td><StatusBadge status={e.route.status} /></td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
              <div className="mt-3"><IntervalLegend /></div>
            </Section>

            <Section id="wl-targets" title="Where it reads carefully"
              note="Set how often each kind of answer must be right, and what a wrong one costs. Where checks show an element missing its target, the template stops guessing it and the harness has to read it. Costlier elements are checked more often.">
              <p role="status" aria-live="polite" className="mb-2 min-h-4 text-xs text-muted-foreground">{saved}</p>
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <caption className="sr-only">Target accuracy, cost of a wrong answer, and what follows for each element</caption>
                  <thead className="text-left text-muted-foreground">
                    <tr>
                      <th scope="col" className="py-1.5 pr-3 font-medium">Element</th>
                      <th scope="col" className="pr-3 font-medium">Must be right</th>
                      <th scope="col" className="pr-3 font-medium">A wrong answer costs</th>
                      <th scope="col" className="pr-3 font-medium">Template</th>
                      <th scope="col" className="pr-3 text-right font-medium">Checked</th>
                      <th scope="col" className="font-medium">Why</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {m.elements.map(e => (
                      <tr key={e.element}>
                        <th scope="row" className="py-2 pr-3 text-left font-medium">{e.element}</th>
                        <td className="pr-3">
                          <select aria-label={`How often ${e.element} must be right`} className={control} value={e.route.target}
                            onChange={ev => void changeTarget(e, Number(ev.target.value), e.route.cost)}>
                            {[...new Set([...TARGETS, e.route.target])].sort().map(t => <option key={t} value={t}>{pct(t)}</option>)}
                          </select>
                        </td>
                        <td className="pr-3">
                          <select aria-label={`What a wrong ${e.element} answer costs`} className={control} value={e.route.cost}
                            onChange={ev => void changeTarget(e, e.route.target, ev.target.value as Cost)}>
                            {COSTS.map(c => <option key={c.id} value={c.id}>{c.label}</option>)}
                          </select>
                        </td>
                        <td className="pr-3">{e.route.guess ? 'May guess' : <span className="font-medium">Must read</span>}</td>
                        <td className="pr-3 text-right tabular-nums">{pct(e.route.checkRate)} of reads</td>
                        <td className="text-muted-foreground">
                          {!e.route.guess ? e.route.why
                            : e.route.status === 'unmeasured' ? `${e.route.random.n} of 10 random checks settled: not enough to act on yet.`
                            : statusDescription(e.route.status)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="mt-2 text-[11px] text-muted-foreground">
                A sender whose answers passed ten checks in a row may be guessed even where its element is missing target —
                unless the guesses themselves were what was wrong. A guess is never made where the conversation looks like it
                holds the thing (a receipt-looking subject never gets “no receipt”), and labels are only ever suggested.
              </p>
            </Section>

            <Section id="wl-signals" title="What you did with its work"
              note="The corrections you make without being asked: cards and events you rejected, labels you took off, conversations you moved back to the inbox.">
              <div className="overflow-x-auto">
                <table className="w-full text-xs tabular-nums">
                  <caption className="sr-only">What became of what the loop proposed or did</caption>
                  <thead className="text-left text-muted-foreground">
                    <tr>
                      <th scope="col" className="py-1.5 pr-3 font-medium">What</th>
                      <th scope="col" className="pr-3 text-right font-medium">Kept</th>
                      <th scope="col" className="pr-3 text-right font-medium">Undone by you</th>
                      <th scope="col" className="pr-3 text-right font-medium">Still waiting</th>
                      <th scope="col" className="text-right font-medium">Kept, of those decided</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {ACTIONS.map(a => {
                      const o = m.actions[a.kind] ?? { total: 0, kept: 0, undone: 0, open: 0 };
                      const decided = o.kept + o.undone;
                      return (
                        <tr key={a.kind}>
                          <th scope="row" className="py-1.5 pr-3 text-left font-normal">{a.label}</th>
                          <td className="pr-3 text-right">{num(o.kept)}</td>
                          <td className="pr-3 text-right">{num(o.undone)} <span className="text-muted-foreground">{a.undone}</span></td>
                          <td className="pr-3 text-right">{num(o.open)}</td>
                          <td className="text-right">{decided ? pct(o.kept / decided) : '—'}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </Section>

            <Section id="wl-effort" title="Effort"
              note="How much reading it took. Refusals are rework: the reasons say what the guide should make clearer.">
              <div className="grid gap-5 lg:grid-cols-[2fr_1fr]">
                <DailyBars days={fillDays(m.reading.days, m.since)} />
                <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs lg:grid-cols-1">
                  <div><dt className="text-muted-foreground">Lease to submit, typically</dt><dd className="tabular-nums">{m.reading.medianSeconds ? duration(m.reading.medianSeconds) : '—'}</dd></div>
                  <div><dt className="text-muted-foreground">Batches</dt><dd className="tabular-nums">{num(m.reading.batches)} ({num(m.reading.leased)} conversations leased)</dd></div>
                  <div><dt className="text-muted-foreground">Leases that ran out unanswered</dt><dd className="tabular-nums">{num(m.reading.expired)}</dd></div>
                  <div>
                    <dt className="text-muted-foreground">Cost per conversation</dt>
                    <dd className="tabular-nums">
                      {m.reading.usage
                        ? `${Math.round(m.reading.usage.tokensPerConversation).toLocaleString()} tokens${m.reading.usage.costPerConversation ? ` · $${m.reading.usage.costPerConversation.toFixed(4)}` : ''}`
                        : <span className="text-muted-foreground">Not reported. A harness can add <code className="font-mono">--input-tokens</code>, <code className="font-mono">--output-tokens</code> and <code className="font-mono">--cost-usd</code> to <code className="font-mono">work submit</code>.</span>}
                    </dd>
                  </div>
                </dl>
              </div>
              <h3 className="mt-4 text-xs font-semibold">Why submissions were refused</h3>
              {m.reading.problems.length === 0 ? (
                <p className="mt-1 text-xs text-muted-foreground">None were refused.</p>
              ) : (
                <ul className="mt-1 divide-y divide-border text-xs">
                  {m.reading.problems.map(p => (
                    <li key={p.problem} className="flex justify-between gap-3 py-1">
                      <span>{p.problem}</span><span className="tabular-nums text-muted-foreground">{num(p.count)}×</span>
                    </li>
                  ))}
                </ul>
              )}
            </Section>

            <Section id="wl-calibration" title="Does its confidence mean what it says?"
              note="On random checks, answers that said they were 90% sure should be right about 90% of the time. A large gap means confidence cannot be used to decide what to check.">
              <CalibrationTable bins={m.calibration.bins} unrated={m.calibration.unrated} />
            </Section>
          </>
        )}
      </div>
    </div>
  );
};
