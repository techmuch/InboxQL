import { useCallback, useEffect, useState } from 'react';
import {
  AlertTriangle, Globe, Play, Plus, RefreshCw, Tag, Trash2,
} from 'lucide-react';
import {
  deleteAnnotator, listAnnotators, listProfiles, runAnnotator, saveAnnotator,
  type Annotator, type Profile, type RunOutcome,
} from '../ai/api';
import { Notice } from '../ai/Models';
import { useQueryStore } from '../../lib/filters';

/**
 * Annotators — the labelling and extraction surface.
 *
 * # Why this is a tab and not a settings pane
 *
 * An annotator is not configuration you set once. It is a thing you write,
 * run, read the results of, correct, and rewrite — the loop that turns an
 * unstructured message body into something `label:` and `extract:` can query.
 * Settings is a place people visit twice a year.
 *
 * Until now this whole surface was CLI-only, while a graph editor that cannot
 * execute anything had a tab of its own.
 *
 * # What it deliberately does not do
 *
 * Runs are bounded and synchronous — a batch at a time, with progress in
 * between. Firing a goroutine and drawing a progress bar over a job system
 * that does not exist would look finished and lose work.
 */
export const Annotators = () => {
  const [annotators, setAnnotators] = useState<Annotator[]>([]);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Partial<Annotator> | null>(null);
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const setQuery = useQueryStore(s => s.set);

  const load = useCallback(async () => {
    try {
      const [as, ps] = await Promise.all([listAnnotators(), listProfiles()]);
      setAnnotators(as ?? []);
      setProfiles(ps ?? []);
    } catch (e) {
      setNotice({ ok: false, text: e instanceof Error ? e.message : String(e) });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const run = async (a: Annotator, dryRun: boolean) => {
    setBusy(a.name);
    setNotice(null);
    try {
      const out = await runAnnotator(a.name, dryRun ? { dryRun: true } : { limit: 200 });
      setNotice({ ok: true, text: describeRun(a, out, dryRun) });
      if (!dryRun) await load();
    } catch (e) {
      setNotice({ ok: false, text: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(null);
    }
  };

  if (loading) {
    return (
      <div className="p-8 text-center text-sm text-muted-foreground">
        <RefreshCw className="mx-auto mb-2 h-5 w-5 animate-spin text-primary" />
        Loading annotators…
      </div>
    );
  }

  return (
    <div className="h-full overflow-auto p-6">
      <div className="mx-auto max-w-4xl space-y-6">
        <header className="flex items-start justify-between gap-4">
          <div>
            <h2 className="flex items-center gap-2 text-2xl font-bold text-foreground">
              <Tag className="h-6 w-6 text-primary" />
              Annotators
            </h2>
            <p className="mt-1 text-sm text-muted-foreground">
              A named, versioned instruction applied to messages. A label answers yes or no;
              an extractor pulls structured records out of a body. Both become query terms —{' '}
              <code className="font-mono">label:billing</code>,{' '}
              <code className="font-mono">extract:invoice.amount&gt;100</code>.
            </p>
          </div>
          <button
            type="button"
            onClick={() => setEditing({ kind: 'label', engine: 'rule' })}
            className="flex shrink-0 items-center gap-1 border border-border px-3 py-1.5 text-xs hover:bg-accent/40"
          >
            <Plus className="h-3 w-3" /> New annotator
          </button>
        </header>

        {notice && <Notice ok={notice.ok} text={notice.text} onDismiss={() => setNotice(null)} />}

        {annotators.length === 0 ? (
          <div className="border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
            <p>No annotators yet.</p>
            <p className="mx-auto mt-2 max-w-md text-xs">
              A rule annotator is just a query expression, so the language in the query bar is
              also the labelling language. Start with one — they cost nothing to run and can be
              rewritten freely.
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {annotators.map(a => (
              <AnnotatorCard
                key={a.id}
                annotator={a}
                busy={busy === a.name}
                onEdit={() => setEditing(a)}
                onRun={dry => run(a, dry)}
                onDelete={async () => {
                  if (!window.confirm(
                    `Delete "${a.name}" and every result it produced? Messages are untouched.`,
                  )) return;
                  try {
                    await deleteAnnotator(a.name);
                    await load();
                    setNotice({ ok: true, text: `Deleted ${a.name}.` });
                  } catch (e) {
                    setNotice({ ok: false, text: e instanceof Error ? e.message : String(e) });
                  }
                }}
                onShowResults={() => setQuery(
                  a.kind === 'extract' ? `| extract ${a.name}` : `label:${a.name}`,
                )}
              />
            ))}
          </div>
        )}

        {editing && (
          <AnnotatorEditor
            annotator={editing}
            profiles={profiles}
            onCancel={() => setEditing(null)}
            onSave={async a => {
              try {
                await saveAnnotator(a);
                await load();
                setEditing(null);
                setNotice({ ok: true, text: `Saved ${a.name}.` });
              } catch (e) {
                setNotice({ ok: false, text: e instanceof Error ? e.message : String(e) });
              }
            }}
          />
        )}
      </div>
    </div>
  );
};

const AnnotatorCard = ({
  annotator: a,
  busy,
  onEdit,
  onRun,
  onDelete,
  onShowResults,
}: {
  annotator: Annotator;
  busy: boolean;
  onEdit: () => void;
  onRun: (dryRun: boolean) => void;
  onDelete: () => void;
  onShowResults: () => void;
}) => {
  const total = a.progress?.total ?? 0;
  const done = a.progress?.evaluated ?? 0;
  const pending = Math.max(total - done, 0);

  return (
    <div className="border border-border bg-card">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3">
        <button type="button" onClick={onEdit} className="min-w-0 flex-1 text-left">
          <span className="font-medium">{a.name}</span>
          <span className="ml-2 font-mono text-[11px] text-muted-foreground">
            v{a.version} · {a.kind} · {a.engine}
            {a.engine === 'llm' && a.profile ? ` · ${a.profile}` : ''}
          </span>
        </button>

        {a.engine === 'llm' && a.scope === 'remote' && (
          <span
            title={a.endpoint}
            className="flex items-center gap-1 bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-mono text-amber-600 dark:text-amber-400"
          >
            <Globe className="h-3 w-3" /> remote
          </span>
        )}

        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => onRun(true)}
            disabled={busy}
            title="Report what a run would do, without doing it"
            className="border border-border px-2 py-0.5 text-xs hover:bg-accent/40 disabled:opacity-50"
          >
            Plan
          </button>
          <button
            type="button"
            onClick={() => onRun(false)}
            disabled={busy || pending === 0}
            title={pending === 0 ? 'Nothing left to evaluate at this version' : 'Evaluate up to 200 messages'}
            className="flex items-center gap-1 border border-border px-2 py-0.5 text-xs hover:bg-accent/40 disabled:opacity-50"
          >
            {busy ? <RefreshCw className="h-3 w-3 animate-spin" /> : <Play className="h-3 w-3" />}
            Run
          </button>
          <button
            type="button"
            onClick={onShowResults}
            title="Query what it produced"
            className="border border-border px-2 py-0.5 text-xs hover:bg-accent/40"
          >
            Results
          </button>
          <button
            type="button"
            onClick={onDelete}
            className="p-1 text-muted-foreground hover:text-destructive"
            aria-label={`Delete ${a.name}`}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      <p className="truncate px-4 pb-2 font-mono text-xs text-muted-foreground">{a.instructions}</p>

      {/* Coverage, because an annotator that has only seen a tenth of the
          mailbox gives answers that look complete and are not. */}
      <div className="flex items-center gap-3 border-t border-border/60 px-4 py-1.5">
        <div className="h-1 flex-1 bg-muted">
          <div
            className="h-1 bg-primary"
            style={{ width: `${total > 0 ? Math.round((done / total) * 100) : 0}%` }}
          />
        </div>
        <span className="shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">
          {done.toLocaleString()} / {total.toLocaleString()} evaluated
        </span>
      </div>

      {a.profileMissing && (
        <Banner tone="error">
          Its model profile no longer exists, so a run will fail. Edit it to pick another.
        </Banner>
      )}
      {a.consentMissing && !a.profileMissing && (
        <Banner tone="warn">
          Runs will refuse: this sends message bodies to <span className="font-mono">{a.endpoint}</span>,
          and consent has not been given. Edit it to allow that, or move it to a local profile.
        </Banner>
      )}
    </div>
  );
};

const Banner = ({ tone, children }: { tone: 'warn' | 'error'; children: React.ReactNode }) => (
  <div className={`flex items-start gap-2 border-t px-4 py-2 text-xs ${
    tone === 'error'
      ? 'border-destructive/20 bg-destructive/10 text-destructive'
      : 'border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-400'
  }`}>
    <AlertTriangle className="mt-px h-3.5 w-3.5 shrink-0" />
    <span>{children}</span>
  </div>
);

const AnnotatorEditor = ({
  annotator,
  profiles,
  onSave,
  onCancel,
}: {
  annotator: Partial<Annotator>;
  profiles: Profile[];
  onSave: (a: Partial<Annotator> & { name: string }) => void;
  onCancel: () => void;
}) => {
  const isNew = !annotator.id;
  const [name, setName] = useState(annotator.name ?? '');
  const [kind, setKind] = useState<'label' | 'extract'>(annotator.kind ?? 'label');
  const [engine, setEngine] = useState<'rule' | 'llm'>(annotator.engine ?? 'rule');
  const [instructions, setInstructions] = useState(annotator.instructions ?? '');
  const [profile, setProfile] = useState(annotator.profile ?? '');
  const [allowRemote, setAllowRemote] = useState(annotator.allowRemote ?? false);

  const chosen = profiles.find(p => p.name === profile) ?? profiles.find(p => p.isDefault);
  const remote = engine === 'llm' && chosen?.scope === 'remote';
  // Rewording the instruction bumps the version, which marks every earlier
  // result stale. Worth saying before the save, not after.
  const rewording =
    !isNew && (annotator.instructions ?? '').trim() !== instructions.trim();

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-auto bg-black/40 p-8">
      <div className="w-full max-w-2xl space-y-4 border border-border bg-card p-6 shadow-lg">
        <h3 className="text-lg font-bold">{isNew ? 'New annotator' : `Edit ${annotator.name}`}</h3>

        <div className="grid grid-cols-3 gap-3">
          <Labelled label="Name">
            <input
              value={name}
              onChange={e => setName(e.target.value)}
              disabled={!isNew}
              autoFocus={isNew}
              placeholder="billing"
              className="w-full border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:ring-1 focus:ring-primary disabled:opacity-60"
            />
          </Labelled>
          <Labelled label="Kind">
            <select
              value={kind}
              onChange={e => setKind(e.target.value as 'label' | 'extract')}
              className="w-full border border-border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="label">label — yes or no</option>
              <option value="extract">extract — structured records</option>
            </select>
          </Labelled>
          <Labelled label="Engine">
            <select
              value={engine}
              onChange={e => setEngine(e.target.value as 'rule' | 'llm')}
              className="w-full border border-border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="rule">rule — a query</option>
              <option value="llm">llm — a prompt</option>
            </select>
          </Labelled>
        </div>

        <Labelled
          label={engine === 'rule' ? 'Rule' : 'Prompt'}
          hint={engine === 'rule'
            ? 'A query expression. The same language the query bar takes, so a rule is testable there first.'
            : 'What to ask of each message body.'}
        >
          <textarea
            value={instructions}
            onChange={e => setInstructions(e.target.value)}
            rows={engine === 'rule' ? 3 : 6}
            placeholder={engine === 'rule'
              ? 'from:*@stripe.com OR subject:invoice'
              : 'Does this message ask me to do something? Answer with matched and confidence.'}
            className="w-full border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:ring-1 focus:ring-primary"
          />
        </Labelled>

        {engine === 'llm' && (
          <>
            <Labelled label="Model profile" hint="Which gateway this runs against.">
              <select
                value={profile}
                onChange={e => setProfile(e.target.value)}
                className="w-full border border-border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-primary"
              >
                <option value="">
                  {profiles.find(p => p.isDefault)
                    ? `default (${profiles.find(p => p.isDefault)!.name})`
                    : 'default'}
                </option>
                {profiles.map(p => (
                  <option key={p.id} value={p.name}>{p.name} — {p.provider}/{p.model} ({p.scope})</option>
                ))}
              </select>
            </Labelled>

            {remote && (
              <div className="space-y-2 border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-400">
                <div className="flex items-start gap-2">
                  <Globe className="mt-px h-3.5 w-3.5 shrink-0" />
                  <span>
                    <strong>This sends message bodies to {chosen?.endpoint}.</strong>{' '}
                    Consent is per annotator, so allowing it here does not allow it anywhere else.
                  </span>
                </div>
                <label className="flex items-center gap-2 font-medium">
                  <input
                    type="checkbox"
                    checked={allowRemote}
                    onChange={e => setAllowRemote(e.target.checked)}
                  />
                  Allow this annotator to send message bodies off this machine
                </label>
              </div>
            )}
          </>
        )}

        {rewording && (
          <Banner tone="warn">
            The instruction changed, so this becomes v{(annotator.version ?? 1) + 1}. Earlier results
            are kept for provenance but stop answering queries until you re-run it.
          </Banner>
        )}

        <div className="flex justify-end gap-2 pt-2">
          <button type="button" onClick={onCancel} className="border border-border px-4 py-1.5 text-xs hover:bg-accent/40">
            Cancel
          </button>
          <button
            type="button"
            disabled={!name.trim() || !instructions.trim()}
            onClick={() => onSave({
              id: annotator.id, name: name.trim(), kind, engine,
              instructions: instructions.trim(),
              profile: engine === 'llm' ? profile : '',
              allowRemote: engine === 'llm' && allowRemote,
            })}
            className="bg-primary px-4 py-1.5 text-xs font-bold text-primary-foreground hover:opacity-90 disabled:opacity-50"
          >
            Save
          </button>
        </div>
      </div>
    </div>
  );
};

const Labelled = ({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) => (
  <div className="space-y-1">
    {/* The control sits inside the label, so they are associated without
        having to mint and thread an id through every caller. The hint stays
        outside it, or it would be read out as part of the field's name. */}
    <label className="block space-y-1">
      <span className="block text-[10px] font-bold uppercase tracking-wider text-muted-foreground">
        {label}
      </span>
      {children}
    </label>
    {hint && <p className="text-[11px] text-muted-foreground">{hint}</p>}
  </div>
);

/** What a run or a plan actually did, in one sentence. */
function describeRun(a: Annotator, out: RunOutcome, dryRun: boolean): string {
  if (dryRun) {
    const pending = out.pending ?? 0;
    if (pending === 0) return `${a.name} has already evaluated everything in scope.`;
    const size = out.estimatedChars
      ? ` Roughly ${out.estimatedChars.toLocaleString()} characters would be sent.`
      : '';
    return `${a.name} would evaluate ${pending.toLocaleString()} message${pending === 1 ? '' : 's'}.${size}`;
  }
  const evaluated = out.evaluated ?? 0;
  const matched = out.matched ?? out.records ?? 0;
  return `${a.name} evaluated ${evaluated.toLocaleString()} message${evaluated === 1 ? '' : 's'}, ` +
    `matching ${matched.toLocaleString()}.`;
}
