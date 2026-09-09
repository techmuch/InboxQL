import { useCallback, useEffect, useState } from 'react';
import {
  AlertCircle, Binary, Check, Cpu, Globe, MessageSquare, Play, Plus, RefreshCw,
  Square, Star, Trash2, Zap,
} from 'lucide-react';
import {
  deleteProfile, getStatus, refreshRuntimes, saveProfile, setDefaultProfile,
  startRuntime, stopRuntime, testProfile,
  type LLMStatus, type Profile, type Runtime,
} from './api';

/**
 * Models — the plural gateway list.
 *
 * # Why a list and not a form
 *
 * There used to be one form here, because there was one configuration: one
 * provider, one model, one key. That was already at odds with the rest of the
 * product — an annotator could name a model but not the gateway it lives on,
 * so pointing one at a cloud model while the machine was configured for a
 * local runtime asked the local runtime for a model it had never heard of.
 *
 * A profile is the whole address, so this is a list of addresses.
 *
 * # Why runtimes sit apart from profiles
 *
 * Starting a daemon and configuring a model are different verbs. Mixing them
 * into one card is what made the old page hard to read: "Configure Swama" held
 * a start/stop button, a launch mode, an autostart hook and a model picker,
 * which are two unrelated decisions wearing one heading.
 */
export const Models = () => {
  const [status, setStatus] = useState<LLMStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Partial<Profile> | null>(null);
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null);

  const load = useCallback(async () => {
    try {
      setStatus(await getStatus());
    } catch (e) {
      setNotice({ ok: false, text: e instanceof Error ? e.message : String(e) });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  // A runtime started in another terminal should not need a page reload to
  // appear. The old page fetched once on mount and then told you it was
  // stopped indefinitely.
  useEffect(() => {
    const tick = () => { if (document.visibilityState === 'visible') load(); };
    const id = window.setInterval(tick, 15000);
    document.addEventListener('visibilitychange', tick);
    return () => {
      window.clearInterval(id);
      document.removeEventListener('visibilitychange', tick);
    };
  }, [load]);

  const act = async (fn: () => Promise<unknown>, success: string) => {
    setNotice(null);
    try {
      await fn();
      await load();
      setNotice({ ok: true, text: success });
    } catch (e) {
      setNotice({ ok: false, text: e instanceof Error ? e.message : String(e) });
    }
  };

  if (loading) {
    return (
      <div className="p-8 text-center text-sm text-muted-foreground">
        <RefreshCw className="mx-auto mb-2 h-5 w-5 animate-spin text-primary" />
        Scanning local AI runtimes…
      </div>
    );
  }

  const profiles = status?.profiles ?? [];
  const runtimes = status?.runtimes ?? [];

  return (
    <div className="max-w-4xl space-y-8">
      <header>
        <h2 className="flex items-center gap-2 text-2xl font-bold text-foreground">
          <Cpu className="h-6 w-6 text-primary" />
          Models
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          A profile is the whole address of a model: which provider, at which endpoint,
          reached with which credential. Annotators name a profile, which is what makes
          "does this leave my machine?" answerable one annotator at a time.
        </p>
      </header>

      {notice && <Notice ok={notice.ok} text={notice.text} onDismiss={() => setNotice(null)} />}

      <RuntimeStrip
        runtimes={runtimes}
        onStart={(p, mode) => act(() => startRuntime(p, mode), `Started ${p}.`)}
        onStop={p => act(() => stopRuntime(p), `Stopped ${p}.`)}
        onRefresh={async provider => {
          setNotice(null);
          try {
            const res = await refreshRuntimes(provider);
            await load();
            // The whole point of the new endpoint: say why nothing was found,
            // rather than rendering an unexplained empty list.
            const problems = res.runtimes.filter(r => r.problem).map(r => r.problem!);
            setNotice(
              res.models > 0
                ? { ok: true, text: `Found ${res.models} model${res.models === 1 ? '' : 's'}.` }
                : { ok: false, text: problems[0] ?? 'No models found, and no runtime explained why.' },
            );
          } catch (e) {
            setNotice({ ok: false, text: e instanceof Error ? e.message : String(e) });
          }
        }}
      />

      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
            Model profiles
          </h3>
          <button
            type="button"
            onClick={() => setEditing({ provider: 'swama', launchMode: 'headless' })}
            className="flex items-center gap-1 border border-border px-2 py-1 text-xs hover:bg-accent/40"
          >
            <Plus className="h-3 w-3" /> Add model
          </button>
        </div>

        {profiles.length === 0 ? (
          <EmptyProfiles runtimes={runtimes} onAdd={p => setEditing(p)} />
        ) : (
          <div className="border border-border">
            {profiles.map(p => (
              <ProfileRow
                key={p.id}
                profile={p}
                runtimes={runtimes}
                onEdit={() => setEditing(p)}
                onMakeDefault={() => act(() => setDefaultProfile(p.name), `${p.name} is now the default.`)}
                onDelete={() => act(() => deleteProfile(p.name), `Removed ${p.name}.`)}
                onTest={() => act(async () => {
                  const r = await testProfile({ provider: p.provider, model: p.model, endpoint: p.endpoint });
                  if (!r.ok) throw new Error('the model did not answer');
                }, `${p.name} answered.`)}
              />
            ))}
          </div>
        )}
      </section>

      {editing && (
        <ProfileEditor
          profile={editing}
          runtimes={runtimes}
          existingNames={profiles.map(p => p.name)}
          onCancel={() => setEditing(null)}
          onSave={async p => {
            await act(() => saveProfile(p), `Saved ${p.name}.`);
            setEditing(null);
          }}
        />
      )}
    </div>
  );
};

/**
 * The local runners on this machine, and what they are doing.
 *
 * Deliberately compact: this is machine status, not configuration. It says
 * what is installed, whether it is up, how many models it has, and gives you
 * the two buttons that change those facts.
 */
const RuntimeStrip = ({
  runtimes,
  onStart,
  onStop,
  onRefresh,
}: {
  runtimes: Runtime[];
  onStart: (provider: string, launchMode: string) => void;
  onStop: (provider: string) => void;
  onRefresh: (provider?: string) => void;
}) => (
  <section className="space-y-2">
    <div className="flex items-center justify-between">
      <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
        Local runtimes
      </h3>
      <button
        type="button"
        onClick={() => onRefresh()}
        className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground"
      >
        <RefreshCw className="h-3 w-3" /> Re-scan all
      </button>
    </div>

    <div className="border border-border divide-y divide-border">
      {runtimes.map(rt => (
        <div key={rt.provider} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2 text-sm">
          <span
            aria-hidden="true"
            className={`h-2 w-2 shrink-0 rounded-full ${
              rt.running ? 'bg-green-500' : rt.installed ? 'bg-amber-500' : 'bg-muted-foreground/30'
            }`}
          />
          <span className="font-medium">{rt.name}</span>

          <span className="font-mono text-xs text-muted-foreground">
            {!rt.installed
              ? 'not installed'
              : rt.running
                ? `running · ${rt.models.length} model${rt.models.length === 1 ? '' : 's'}`
                : 'stopped'}
          </span>

          {rt.installed && (
            <div className="ml-auto flex items-center gap-1">
              {/* Next to the runtime whose models it refreshes, which is where
                  you look when the model you just pulled is not listed. */}
              <button
                type="button"
                onClick={() => onRefresh(rt.provider)}
                title={`Refresh ${rt.name}'s models`}
                className="flex items-center gap-1 border border-border px-2 py-0.5 text-xs hover:bg-accent/40"
              >
                <RefreshCw className="h-3 w-3" /> Refresh models
              </button>
              {rt.running ? (
                <button
                  type="button"
                  onClick={() => onStop(rt.provider)}
                  className="flex items-center gap-1 border border-border px-2 py-0.5 text-xs hover:bg-destructive/10 hover:text-destructive"
                >
                  <Square className="h-3 w-3" /> Stop
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => onStart(rt.provider, rt.launchModes[0] ?? 'headless')}
                  className="flex items-center gap-1 border border-border px-2 py-0.5 text-xs hover:bg-accent/40"
                >
                  <Play className="h-3 w-3" /> Start
                </button>
              )}
            </div>
          )}
        </div>
      ))}
    </div>
  </section>
);

const ProfileRow = ({
  profile,
  runtimes,
  onEdit,
  onMakeDefault,
  onDelete,
  onTest,
}: {
  profile: Profile;
  runtimes: Runtime[];
  onEdit: () => void;
  onMakeDefault: () => void;
  onDelete: () => void;
  onTest: () => void;
}) => {
  const runtime = runtimes.find(r => r.provider === profile.provider);
  const reachable = profile.scope === 'remote' || runtime?.running;

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border/60 px-3 py-2 text-sm last:border-b-0">
      <button
        type="button"
        onClick={onMakeDefault}
        disabled={profile.isDefault}
        title={profile.isDefault ? 'This is the default' : 'Use this when nothing names a profile'}
        className={profile.isDefault ? 'text-primary' : 'text-muted-foreground/30 hover:text-muted-foreground'}
      >
        <Star className="h-3.5 w-3.5" fill={profile.isDefault ? 'currentColor' : 'none'} />
      </button>

      <button type="button" onClick={onEdit} className="min-w-0 flex-1 text-left">
        <span className="font-medium">{profile.name}</span>
        <span className="ml-2 font-mono text-xs text-muted-foreground">
          {profile.provider} · {profile.model}
        </span>
      </button>

      {/* What the profile is for. Pointing an annotator at an embedding model
          used to fail at the moment of use with a provider error; the purpose
          is on the profile so it is a configuration fact instead. */}
      <span
        title={profile.purpose === 'embedding'
          ? `Embeddings, ${profile.dimensions ?? '?'} dimensions`
          : 'Completions'}
        className="flex items-center gap-1 px-1.5 py-0.5 text-[11px] font-mono bg-muted text-muted-foreground"
      >
        {profile.purpose === 'embedding'
          ? <><Binary className="h-3 w-3" />{profile.dimensions ?? '?'}</>
          : <><MessageSquare className="h-3 w-3" />chat</>}
      </span>

      {/* Scope is the column that matters most: it says whether using this
          profile sends mail off the machine. */}
      <span
        title={profile.endpoint}
        className={`px-1.5 py-0.5 text-[11px] font-mono ${
          profile.scope === 'remote'
            ? 'bg-amber-500/10 text-amber-600 dark:text-amber-400'
            : 'bg-green-500/10 text-green-600 dark:text-green-400'
        }`}
      >
        {profile.scope === 'remote' ? <Globe className="mr-1 inline h-3 w-3" /> : null}
        {profile.scope}
      </span>

      {profile.hasApiKey && (
        <span className="text-[11px] font-mono text-muted-foreground">key stored</span>
      )}
      {!reachable && (
        <span className="text-[11px] font-mono text-amber-600 dark:text-amber-400">runtime stopped</span>
      )}

      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={onTest}
          title="Send a trivial prompt"
          className="p-1 text-muted-foreground hover:text-primary"
        >
          <Zap className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onClick={onDelete}
          title="Remove this profile"
          className="p-1 text-muted-foreground hover:text-destructive"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  );
};

/**
 * The empty state, which offers the shortest real next step.
 *
 * If a runtime is already running with a model pulled, one click configures it
 * — nobody should have to type a model name they can see on the screen above.
 */
const EmptyProfiles = ({
  runtimes,
  onAdd,
}: {
  runtimes: Runtime[];
  onAdd: (p: Partial<Profile>) => void;
}) => {
  const ready = runtimes.find(r => r.running && r.models.length > 0);
  return (
    <div className="border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
      <p>No models configured.</p>
      <p className="mt-1 text-xs">
        Without one, <code className="font-mono">analyze</code> and{' '}
        <code className="font-mono">draft</code> emit structured context for an external
        agent instead of prose. That is a supported way to run InboxQL, not an error.
      </p>
      {ready && (
        <button
          type="button"
          onClick={() => onAdd({
            name: ready.provider, provider: ready.provider,
            model: ready.models[0], isDefault: true,
          })}
          className="mt-3 border border-border px-3 py-1 text-xs hover:bg-accent/40"
        >
          Use {ready.name} with {ready.models[0]}
        </button>
      )}
    </div>
  );
};

/** Creating or editing one profile. */
const ProfileEditor = ({
  profile,
  runtimes,
  existingNames,
  onSave,
  onCancel,
}: {
  profile: Partial<Profile>;
  runtimes: Runtime[];
  existingNames: string[];
  onSave: (p: Partial<Profile> & { name: string; apiKey?: string }) => void;
  onCancel: () => void;
}) => {
  const isNew = !profile.id;
  const [name, setName] = useState(profile.name ?? '');
  const [provider, setProvider] = useState(profile.provider ?? 'swama');
  const [model, setModel] = useState(profile.model ?? '');
  const [endpoint, setEndpoint] = useState(profile.endpoint ?? '');
  const [apiKey, setApiKey] = useState('');
  const [isDefault, setIsDefault] = useState(profile.isDefault ?? false);
  const [purpose, setPurpose] = useState<'chat' | 'embedding'>(profile.purpose ?? 'chat');

  const runtime = runtimes.find(r => r.provider === provider);
  const models = runtime?.models ?? [];
  const duplicate = isNew && existingNames.includes(name.trim());

  // Scope is computed the same way the server computes it, and shown live, so
  // the consequence of an endpoint is visible while it is being typed rather
  // than discovered when an annotator refuses to run.
  const effective = endpoint || DEFAULT_ENDPOINTS[provider] || '';
  const remote = effective !== '' && !/:\/\/(localhost|127\.0\.0\.1|\[::1\]|0\.0\.0\.0)/i.test(effective);

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-auto bg-black/40 p-8">
      <div className="w-full max-w-lg space-y-4 border border-border bg-card p-6 shadow-lg">
        <h3 className="text-lg font-bold">{isNew ? 'Add a model' : `Edit ${profile.name}`}</h3>

        <Field label="Name" hint="What annotators will call it, e.g. local-fast or cloud.">
          <input
            value={name}
            onChange={e => setName(e.target.value)}
            disabled={!isNew}
            autoFocus={isNew}
            className="w-full border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:ring-1 focus:ring-primary disabled:opacity-60"
          />
          {duplicate && <p className="text-xs text-destructive">A profile called {name} already exists.</p>}
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label="Provider">
            <select
              value={provider}
              onChange={e => {
                setProvider(e.target.value);
                setEndpoint('');
                const rt = runtimes.find(r => r.provider === e.target.value);
                setModel(rt?.models[0] ?? '');
              }}
              className="w-full border border-border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="swama">Swama (Apple Silicon MLX)</option>
              <option value="ollama">Ollama</option>
              <option value="openai">OpenAI-compatible</option>
            </select>
          </Field>

          <Field label="Model">
            {models.length > 0 ? (
              <select
                value={model}
                onChange={e => setModel(e.target.value)}
                className="w-full border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:ring-1 focus:ring-primary"
              >
                {!models.includes(model) && model && <option value={model}>{model}</option>}
                {models.map(m => <option key={m} value={m}>{m}</option>)}
              </select>
            ) : (
              <input
                value={model}
                onChange={e => setModel(e.target.value)}
                placeholder={provider === 'openai' ? 'gpt-4o-mini' : 'llama3'}
                className="w-full border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:ring-1 focus:ring-primary"
              />
            )}
          </Field>
        </div>

        <Field label="Endpoint" hint={`Leave blank for ${DEFAULT_ENDPOINTS[provider] ?? 'the provider default'}.`}>
          <input
            value={endpoint}
            onChange={e => setEndpoint(e.target.value)}
            placeholder={DEFAULT_ENDPOINTS[provider] ?? ''}
            className="w-full border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:ring-1 focus:ring-primary"
          />
        </Field>

        <Field
          label="API key"
          hint={
            profile.hasApiKey
              ? 'A key is stored. Leave blank to keep it; typing replaces it.'
              : 'Sealed with the same vault that protects account passwords.'
          }
        >
          <input
            type="password"
            value={apiKey}
            onChange={e => setApiKey(e.target.value)}
            placeholder={profile.hasApiKey ? '••••••••' : ''}
            className="w-full border border-border bg-background px-3 py-2 font-mono text-sm outline-none focus:ring-1 focus:ring-primary"
          />
        </Field>

        <Field
          label="For"
          hint={purpose === 'embedding'
            ? 'Its width is probed when you save, because only the model knows it and vectors of different widths cannot be compared.'
            : 'Completions: annotators, analyze and draft.'}
        >
          <select
            value={purpose}
            onChange={e => setPurpose(e.target.value as 'chat' | 'embedding')}
            className="w-full border border-border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-primary"
          >
            <option value="chat">chat — completions</option>
            <option value="embedding">embedding — similarity and topics</option>
          </select>
        </Field>

        {/* Only the models that can do the chosen job. */}
        {purpose === 'embedding' && runtime?.catalog && (
          <p className="text-[11px] text-muted-foreground">
            {runtime.catalog.some(m => m.kind === 'embedding')
              ? <>Embedding models here: {runtime.catalog.filter(m => m.kind === 'embedding').map(m => m.name).join(', ')}</>
              : <>{runtime.name} reports no embedding model. Pull one, then re-scan.</>}
          </p>
        )}

        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={isDefault} onChange={e => setIsDefault(e.target.checked)} />
          Use this when nothing names a profile
        </label>

        {/* The sentence that decides whether message bodies leave the machine,
            shown while the decision is being made rather than after. */}
        <div className={`flex items-start gap-2 border p-3 text-xs ${
          remote
            ? 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400'
            : 'border-green-500/30 bg-green-500/10 text-green-700 dark:text-green-400'
        }`}>
          {remote ? <Globe className="mt-0.5 h-3.5 w-3.5 shrink-0" /> : <Check className="mt-0.5 h-3.5 w-3.5 shrink-0" />}
          <span>
            {remote ? (
              <>
                <strong>Remote.</strong> Annotators using this profile send message bodies to{' '}
                <span className="font-mono">{effective}</span>, and refuse to run until you
                give them consent explicitly.
              </>
            ) : (
              <><strong>Local.</strong> Nothing leaves this machine.</>
            )}
          </span>
        </div>

        <div className="flex justify-end gap-2 pt-2">
          <button type="button" onClick={onCancel} className="border border-border px-4 py-1.5 text-xs hover:bg-accent/40">
            Cancel
          </button>
          <button
            type="button"
            disabled={!name.trim() || !model.trim() || duplicate}
            onClick={() => onSave({
              id: profile.id, name: name.trim(), provider, model: model.trim(),
              endpoint: endpoint.trim(), isDefault, purpose,
              // Omitted when untouched, so saving a form that cannot show the
              // stored key does not erase it.
              ...(apiKey ? { apiKey } : {}),
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

const DEFAULT_ENDPOINTS: Record<string, string> = {
  ollama: 'http://localhost:11434',
  openai: 'https://api.openai.com/v1',
  swama: 'http://localhost:28100/v1',
};

const Field = ({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) => (
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

export const Notice = ({ ok, text, onDismiss }: { ok: boolean; text: string; onDismiss?: () => void }) => (
  <div
    role="status"
    onClick={onDismiss}
    className={`flex items-start gap-2 border p-3 text-xs ${
      ok
        ? 'border-green-500/20 bg-green-500/10 text-green-600 dark:text-green-400'
        : 'border-destructive/20 bg-destructive/10 text-destructive'
    } ${onDismiss ? 'cursor-pointer' : ''}`}
  >
    {ok ? <Check className="mt-px h-3.5 w-3.5 shrink-0" /> : <AlertCircle className="mt-px h-3.5 w-3.5 shrink-0" />}
    <span>{text}</span>
  </div>
);
