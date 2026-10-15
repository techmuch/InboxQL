import { useEffect, useMemo, useState } from 'react';
import { CommandPalette, commandRegistry } from 'nexus-shell';
import { Copy, Loader2, X } from 'lucide-react';
import { useAppUi } from '../lib/appUi';
import { BINDINGS } from '../lib/keySequences';
import { openQuery } from '../lib/tabs';

/**
 * The dialogs any part of the application opens: New message, New task,
 * Find anything, and the keyboard page. One place, so a menu, a key and a
 * button all open the same thing.
 */
export const AppDialogs = () => {
  const ui = useAppUi();
  return (
    <>
      {ui.compose && <ComposeDialog onClose={() => ui.set('compose', false)} />}
      {ui.task && <NewTaskDialog onClose={() => ui.set('task', false)} />}
      {ui.keys && <KeyboardHelp onClose={() => ui.set('keys', false)} />}
      <FindAnything open={ui.palette} onClose={() => ui.set('palette', false)} />
    </>
  );
};

const Shell = ({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) => {
  useEffect(() => {
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', esc);
    return () => window.removeEventListener('keydown', esc);
  }, [onClose]);
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/30 pt-[10vh]" onMouseDown={onClose}>
      <div role="dialog" aria-label={title} onMouseDown={e => e.stopPropagation()}
        className="w-[34rem] max-w-[95vw] max-h-[80vh] overflow-auto border border-border bg-card p-5 shadow-xl">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button onClick={onClose} aria-label="Close" className="p-1 text-muted-foreground hover:text-foreground"><X className="h-4 w-4" /></button>
        </div>
        {children}
      </div>
    </div>
  );
};

const field = 'w-full bg-background border border-border px-2 py-1.5 text-sm';
const label = 'block text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-1';

/**
 * New message. Saved as a draft or queued in the outbox; mail leaves from
 * the outbox, approved at a terminal, until sending from here is built.
 */
const ComposeDialog = ({ onClose }: { onClose: () => void }) => {
  const [accounts, setAccounts] = useState<{ id: string; name: string; email?: string }[]>([]);
  const [accountId, setAccountId] = useState('');
  const [to, setTo] = useState('');
  const [cc, setCc] = useState('');
  const [subject, setSubject] = useState('');
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  useEffect(() => {
    fetch('/api/accounts').then(r => r.json()).then((a: any[]) => {
      setAccounts(a ?? []);
      if (a?.length) setAccountId(a[0].id);
    }).catch(() => {});
  }, []);

  const save = async (queue: boolean) => {
    setBusy(true); setError(null);
    try {
      const r = await fetch('/api/drafts', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ accountId, to: [to], cc: [cc], subject, body, queue }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(d.error ?? 'could not save');
      setDone(d.note);
    } catch (e: any) { setError(e.message); } finally { setBusy(false); }
  };

  if (done) {
    const cmd = done.includes('iql outbox approve') ? done.slice(done.indexOf('iql outbox approve')) : null;
    return (
      <Shell title="New message" onClose={onClose}>
        <p className="text-sm">{done}</p>
        {cmd && (
          <button onClick={() => navigator.clipboard?.writeText(cmd)} className="mt-3 inline-flex items-center gap-1.5 border border-border px-3 py-1 text-xs hover:bg-accent">
            <Copy className="h-3 w-3" /> Copy the command
          </button>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button onClick={() => openQuery('folder:drafts')} className="border border-border px-3 py-1.5 text-sm hover:bg-accent">Open drafts</button>
          <button onClick={onClose} className="bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground">Done</button>
        </div>
      </Shell>
    );
  }

  return (
    <Shell title="New message" onClose={onClose}>
      {accounts.length === 0 ? (
        <p className="text-sm text-muted-foreground">Add a mail account in Settings → Mail Accounts first.</p>
      ) : (
        <div className="space-y-3">
          {accounts.length > 1 && (
            <div>
              <label className={label} htmlFor="c-from">From</label>
              <select id="c-from" className={field} value={accountId} onChange={e => setAccountId(e.target.value)}>
                {accounts.map(a => <option key={a.id} value={a.id}>{a.email || a.name}</option>)}
              </select>
            </div>
          )}
          <div>
            <label className={label} htmlFor="c-to">To</label>
            <input id="c-to" autoFocus className={field} value={to} onChange={e => setTo(e.target.value)} placeholder="ada@example.com, bob@example.com" />
          </div>
          <div>
            <label className={label} htmlFor="c-cc">Cc</label>
            <input id="c-cc" className={field} value={cc} onChange={e => setCc(e.target.value)} />
          </div>
          <div>
            <label className={label} htmlFor="c-subj">Subject</label>
            <input id="c-subj" className={field} value={subject} onChange={e => setSubject(e.target.value)} />
          </div>
          <textarea aria-label="Message" className={`${field} min-h-[10rem]`} value={body} onChange={e => setBody(e.target.value)} />
          <p className="text-[11px] text-muted-foreground">
            Mail leaves InboxQL from the outbox: queue it here, then approve it with <span className="font-mono">iql outbox approve</span>.
          </p>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div className="flex justify-end gap-2">
            <button disabled={busy} onClick={() => save(false)} className="border border-border px-3 py-1.5 text-sm hover:bg-accent disabled:opacity-50">Save draft</button>
            <button disabled={busy || !to.trim() || !subject.trim()} onClick={() => save(true)} className="bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground disabled:opacity-50">
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Queue to send'}
            </button>
          </div>
        </div>
      )}
    </Shell>
  );
};

/** New task: a card on the board, in To do. */
const NewTaskDialog = ({ onClose }: { onClose: () => void }) => {
  const [title, setTitle] = useState('');
  const [due, setDue] = useState('');
  const [priority, setPriority] = useState('');
  const [body, setBody] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const save = async () => {
    setBusy(true); setError(null);
    try {
      const r = await fetch('/api/tickets', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: title.trim(), body, status: 'todo', priority: priority || undefined, origin: 'human',
          dueAt: due ? new Date(`${due}T17:00`).toISOString() : undefined }),
      });
      if (!r.ok) throw new Error((await r.text()) || 'could not save');
      commandRegistry.executeCommand('iql.open-board');
      onClose();
    } catch (e: any) { setError(e.message); } finally { setBusy(false); }
  };
  return (
    <Shell title="New task" onClose={onClose}>
      <div className="space-y-3">
        <input autoFocus aria-label="Title" className={`${field} text-base font-medium`} placeholder="What needs doing" value={title} onChange={e => setTitle(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter' && title.trim()) void save(); }} />
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className={label} htmlFor="t-due">Due</label>
            <input id="t-due" type="date" className={field} value={due} onChange={e => setDue(e.target.value)} />
          </div>
          <div>
            <label className={label} htmlFor="t-pri">Priority</label>
            <select id="t-pri" className={field} value={priority} onChange={e => setPriority(e.target.value)}>
              <option value="">None</option><option value="high">High</option><option value="medium">Medium</option><option value="low">Low</option>
            </select>
          </div>
        </div>
        <textarea aria-label="Notes" className={`${field} min-h-[5rem]`} placeholder="Notes" value={body} onChange={e => setBody(e.target.value)} />
        {error && <p className="text-sm text-destructive">{error}</p>}
        <div className="flex justify-end gap-2">
          <button onClick={onClose} className="border border-border px-3 py-1.5 text-sm hover:bg-accent">Cancel</button>
          <button disabled={busy || !title.trim()} onClick={save} className="bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground disabled:opacity-50">Add to board</button>
        </div>
      </div>
    </Shell>
  );
};

/** Help → Keyboard shortcuts, and "?". */
export const KeyboardHelp = ({ onClose }: { onClose: () => void }) => {
  const groups = ['Go', 'Create', 'Help'] as const;
  const extra = [
    { keys: '⌘K / Ctrl+K', label: 'Find anything — commands, places, saved queries' },
    { keys: '↑ ↓, Enter', label: 'Move through a list and open a row' },
    { keys: 'F1', label: 'Documentation' },
    { keys: 'Ctrl+,', label: 'Settings' },
    { keys: 't · ← → · d w m a · n', label: 'In the calendar: today, previous/next, views, new event' },
  ];
  const Key = ({ k }: { k: string }) => (
    <span className="inline-flex gap-1">{k.split(' ').map((p, i) => <kbd key={i} className="border border-border bg-muted px-1.5 py-0.5 font-mono text-[11px]">{p}</kbd>)}</span>
  );
  return (
    <Shell title="Keyboard shortcuts" onClose={onClose}>
      <div className="space-y-4 text-sm">
        {groups.map(g => (
          <section key={g}>
            <h3 className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">{g}</h3>
            <ul className="space-y-1">
              {BINDINGS.filter(b => b.group === g).map(b => (
                <li key={b.keys} className="flex items-center justify-between"><span>{b.label}</span><Key k={b.keys} /></li>
              ))}
            </ul>
          </section>
        ))}
        <section>
          <h3 className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">Everywhere</h3>
          <ul className="space-y-1">
            {extra.map(x => <li key={x.keys} className="flex items-center justify-between gap-4"><span>{x.label}</span><span className="font-mono text-[11px] text-muted-foreground">{x.keys}</span></li>)}
          </ul>
        </section>
        <p className="text-[11px] text-muted-foreground">Sequences: press g, let go, then the letter. Keys typed into a field are never taken.</p>
      </div>
    </Shell>
  );
};

interface PaletteItem { id: string; label: string; keybinding?: string; run: () => void }

/**
 * Find anything: every command and every saved query, matched as you type.
 */
const FindAnything = ({ open, onClose }: { open: boolean; onClose: () => void }) => {
  const [saved, setSaved] = useState<{ title: string; query: string }[]>([]);
  useEffect(() => {
    if (!open) return;
    fetch('/api/queries').then(r => r.json()).then(d => setSaved(Array.isArray(d) ? d : [])).catch(() => setSaved([]));
  }, [open]);
  const items = useMemo<PaletteItem[]>(() => {
    if (!open) return [];
    const cmds: PaletteItem[] = commandRegistry.getCommands()
      .filter(c => c.label && c.id !== 'iql.find-anything')
      .map(c => ({ id: c.id, label: c.label, keybinding: BINDINGS.find(b => b.command === c.id)?.keys ?? c.keybinding,
        run: () => commandRegistry.executeCommand(c.id) }));
    const queries: PaletteItem[] = saved.map(s => ({ id: `saved:${s.title}`, label: `Saved query: ${s.title}`, run: () => openQuery(s.query) }));
    return [...cmds, ...queries];
  }, [open, saved]);
  return (
    <CommandPalette<PaletteItem>
      open={open}
      commands={items}
      onClose={onClose}
      placeholder="Find anything — commands, places, saved queries"
      onSelect={c => { onClose(); c.run(); }}
    />
  );
};
