import { useEffect, useState } from 'react';
import { SlidersHorizontal } from 'lucide-react';
import { getSetting, putSetting } from './api';
import { Notice } from './Models';

/**
 * Analysis — the algorithms that are not an LLM.
 *
 * # Why this tab exists
 *
 * The page was called "AI & Analysis Configuration" and the entire analysis
 * half was one comma-separated word list, wedged under a gateway form.
 * Meanwhile the parameters that actually change what the system decides —
 * how much extraction output to trust, for one — were CLI-only.
 *
 * # Why half of it is read-only
 *
 * Threading, deduplication and search are algorithms with real behaviour and
 * no knobs. Leaving them undocumented means people hunt for switches that do
 * not exist and guess wrong about what the system did. Saying plainly "this is
 * how it works and it is not configurable" is more useful than a settings page
 * that pretends the question was never asked.
 */
export const Analysis = () => {
  const [ignoreWords, setIgnoreWords] = useState('');
  const [autoAccept, setAutoAccept] = useState('');
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([getSetting('ignore_words'), getSetting('ticket_auto_accept')])
      .then(([words, threshold]) => {
        setIgnoreWords(words);
        setAutoAccept(threshold);
      })
      .catch(() => {/* an unset setting is empty, not an error */})
      .finally(() => setLoading(false));
  }, []);

  const save = async (key: string, value: string, what: string) => {
    setNotice(null);
    try {
      await putSetting(key, value);
      setNotice({ ok: true, text: `Saved ${what}.` });
    } catch (e) {
      setNotice({ ok: false, text: e instanceof Error ? e.message : String(e) });
    }
  };

  if (loading) return <div className="p-8 text-sm text-muted-foreground">Loading…</div>;

  // Above 1 means never, which is the safe default: extraction proposes, and a
  // task list you cannot trust is worse than no task list at all.
  const threshold = parseFloat(autoAccept);
  const never = !(threshold >= 0 && threshold <= 1);

  return (
    <div className="max-w-4xl space-y-8">
      <header>
        <h2 className="flex items-center gap-2 text-2xl font-bold text-foreground">
          <SlidersHorizontal className="h-6 w-6 text-primary" />
          Analysis
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          The parameters behind the parts of InboxQL that decide things without asking a model.
        </p>
      </header>

      {notice && <Notice ok={notice.ok} text={notice.text} onDismiss={() => setNotice(null)} />}

      <section className="space-y-3 border border-border bg-card p-6">
        <h3 className="font-bold">Ticket proposals</h3>
        <p className="text-xs leading-relaxed text-muted-foreground">
          Extraction proposes tickets; it does not create them. A proposal whose confidence
          reaches this threshold skips the queue and lands in the working set. Everything
          below it waits for you to accept it.
        </p>

        <div className="flex items-center gap-4">
          <input
            type="range"
            min="0"
            max="1.1"
            step="0.05"
            value={never ? 1.1 : threshold}
            onChange={e => setAutoAccept(e.target.value)}
            className="max-w-xs flex-1"
            aria-label="Auto-accept confidence threshold"
          />
          <span className="w-32 font-mono text-sm tabular-nums">
            {never ? 'never' : threshold.toFixed(2)}
          </span>
        </div>
        <p className="text-[11px] text-muted-foreground">
          {never
            ? 'Nothing is auto-accepted. Every proposed ticket waits in the queue.'
            : `Proposals at ${threshold.toFixed(2)} confidence or above go straight to the board.`}
          {' '}This is the default; <code className="font-mono">--auto-accept</code> still
          overrides it for a single run.
        </p>

        <button
          type="button"
          onClick={() => save('ticket_auto_accept', never ? '1.1' : String(threshold), 'the threshold')}
          className="bg-primary px-4 py-1.5 text-xs font-bold text-primary-foreground hover:opacity-90"
        >
          Save threshold
        </button>
      </section>

      <section className="space-y-3 border border-border bg-card p-6">
        <h3 className="font-bold">Topic extraction</h3>
        <p className="text-xs leading-relaxed text-muted-foreground">
          Topics are taken from the first meaningful word of a subject line. Words listed here
          are skipped, so reply prefixes and filler do not become topics. Comma-separated.
        </p>
        <textarea
          value={ignoreWords}
          onChange={e => setIgnoreWords(e.target.value)}
          placeholder="re:,fwd:,the,and"
          className="h-28 w-full border border-border bg-background px-4 py-3 font-mono text-sm outline-none focus:ring-1 focus:ring-primary"
        />
        <button
          type="button"
          onClick={() => save('ignore_words', ignoreWords, 'the ignore list')}
          className="bg-primary px-4 py-1.5 text-xs font-bold text-primary-foreground hover:opacity-90"
        >
          Save ignore words
        </button>
      </section>

      <section className="space-y-3 border border-border bg-card p-6">
        <h3 className="font-bold">Fixed behaviour</h3>
        <p className="text-xs leading-relaxed text-muted-foreground">
          These decide real things and have no settings. They are listed so you know what the
          system did, rather than looking for a switch that is not here.
        </p>
        <dl className="divide-y divide-border/60 text-xs">
          <Fact term="Threading">
            Conversations are built from the <span className="font-mono">References</span> header,
            never from subject lines. A reply that drops the header starts its own conversation —
            a miss, which is better than merging two unrelated threads that happen to share a subject.
          </Fact>
          <Fact term="Deduplication">
            Identical messages are collapsed by a hash of the normalised body, so the same mail
            imported twice, or delivered to two accounts, counts once.
          </Fact>
          <Fact term="Search">
            Bare words use SQLite&rsquo;s FTS5 index when this build has it, and substring matching
            otherwise. Substring matching is correct but scans, so it is slower on a large mailbox.
            Run <code className="font-mono">iql doctor</code> to see which is active.
          </Fact>
          <Fact term="Ranking">
            There is none. Results come back newest first, or in whatever order a query&rsquo;s
            <span className="font-mono"> | sort</span> asked for. There is no relevance model and
            no vector search.
          </Fact>
        </dl>
      </section>
    </div>
  );
};

const Fact = ({ term, children }: { term: string; children: React.ReactNode }) => (
  <div className="grid grid-cols-[7rem_1fr] gap-4 py-2">
    <dt className="font-bold uppercase tracking-wider text-muted-foreground text-[10px] pt-0.5">{term}</dt>
    <dd className="leading-relaxed text-muted-foreground">{children}</dd>
  </div>
);
