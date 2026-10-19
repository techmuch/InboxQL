import { useEffect, useMemo, useState } from 'react';
import { CommandPalette, commandRegistry } from 'nexus-shell';
import { Loader2, Send, X } from 'lucide-react';
import { sendNew, undoSend, useSendingStore } from '../lib/sending';
import { useAppUi } from '../lib/appUi';
import type { MailtoDraft } from '../lib/emailLinks';
import { BINDINGS } from '../lib/keySequences';
import { openQuery } from '../lib/tabs';
import { matchesSetting, matchesWords, openSetting, SECTION_LABEL, SETTINGS, type SettingEntry } from '../lib/settingsIndex';

/**
 * The dialogs any part of the application opens: New message, New task,
 * Find anything, and the keyboard page. One place, so a menu, a key and a
 * button all open the same thing.
 */
export const AppDialogs = () => {
  const ui = useAppUi();
  return (
    <>
      {ui.compose && <ComposeDialog initial={ui.composeDraft} onClose={() => ui.set('compose', false)} />}
      {ui.task && <NewTaskDialog onClose={() => ui.set('task', false)} />}
      {ui.keys && <KeyboardHelp onClose={() => ui.set('keys', false)} />}
      <FindAnything open={ui.palette} onClose={() => ui.set('palette', false)} />
      <SendingToast />
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
 * Sending… with Undo, then whether it went. Bottom left, as Gmail has it,
 * out of the way of the message list.
 */
const SendingToast = () => {
  const cur = useSendingStore(s => s.current);
  const clear = () => useSendingStore.getState().set(null);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!cur) return;
    const t = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(t);
  }, [cur]);
  useEffect(() => {
    if (!cur || cur.state === 'waiting' || cur.state === 'failed') return;
    const t = setTimeout(clear, 4000);
    return () => clearTimeout(t);
  }, [cur]);
  if (!cur) return null;
  const left = Math.max(0, Math.ceil((cur.until - now) / 1000));
  const subject = cur.subject || '(no subject)';
  return (
    <div role="status" aria-live="polite"
      className="fixed bottom-4 left-4 z-50 flex max-w-md items-center gap-3 border border-border bg-foreground px-4 py-2.5 text-sm text-background shadow-lg">
      {cur.state === 'waiting' && (
        <>
          <span className="truncate">{left > 0 ? `Sending “${subject}” in ${left}s` : `Sending “${subject}”…`}</span>
          {left > 0 && <button onClick={() => void undoSend()} className="font-semibold underline">Undo</button>}
        </>
      )}
      {cur.state === 'sent' && <span className="truncate">Sent “{subject}”.</span>}
      {cur.state === 'undone' && <span className="truncate">Not sent. “{subject}” is back in Drafts.</span>}
      {cur.state === 'failed' && (
        <>
          <span className="min-w-0">Not sent: {cur.error || 'the server refused it'}. It is in Drafts.</span>
          <button onClick={clear} aria-label="Dismiss" className="shrink-0"><X className="h-4 w-4" /></button>
        </>
      )}
    </div>
  );
};

/**
 * New message. Send holds it for a few seconds, with Undo, then sends it;
 * Save draft keeps it in Drafts.
 */
const ComposeDialog = ({ initial, onClose }: { initial: MailtoDraft | null; onClose: () => void }) => {
  const [accounts, setAccounts] = useState<{ id: string; name: string; email?: string }[]>([]);
  const [accountId, setAccountId] = useState('');
  const [to, setTo] = useState(initial?.to ?? '');
  const [cc, setCc] = useState(initial?.cc ?? '');
  const [subject, setSubject] = useState(initial?.subject ?? '');
  const [body, setBody] = useState(initial?.body ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const canSend = to.trim() !== '' && subject.trim() !== '';

  useEffect(() => {
    fetch('/api/accounts').then(r => r.json()).then((a: any[]) => {
      setAccounts(a ?? []);
      if (a?.length) setAccountId(a[0].id);
    }).catch(() => {});
  }, []);

  const save = async () => {
    setBusy(true); setError(null);
    try {
      const r = await fetch('/api/drafts', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ accountId, to: [to], cc: [cc], subject, body }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(d.error ?? 'could not save');
      setDone(d.note);
    } catch (e: any) { setError(e.message); } finally { setBusy(false); }
  };
  const send = async () => {
    setBusy(true); setError(null);
    try {
      await sendNew({ accountId, to: [to], cc: [cc], subject, body });
      onClose();
    } catch (e: any) { setError(e.message); } finally { setBusy(false); }
  };

  if (done) {
    return (
      <Shell title="New message" onClose={onClose}>
        <p className="text-sm">{done}</p>
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
          <textarea aria-label="Message" className={`${field} min-h-[10rem]`} value={body} onChange={e => setBody(e.target.value)}
            onKeyDown={e => { if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && canSend && !busy) { e.preventDefault(); void send(); } }} />
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div className="flex items-center justify-end gap-2">
            <span className="mr-auto text-[11px] text-muted-foreground">⌘/Ctrl+Enter sends; you can undo for a few seconds.</span>
            <button disabled={busy} onClick={() => void save()} className="border border-border px-3 py-1.5 text-sm hover:bg-accent disabled:opacity-50">Save draft</button>
            <button disabled={busy || !canSend} onClick={() => void send()} className="inline-flex items-center gap-1.5 bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground disabled:opacity-50">
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <><Send className="h-3.5 w-3.5" /> Send</>}
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
    { keys: '⌘K / Ctrl+K', label: 'Find anything — commands, places, settings, saved queries' },
    { keys: '↑ ↓, Enter', label: 'Move through a list and open a row' },
    { keys: 'F1', label: 'Documentation' },
    { keys: 'Ctrl+,', label: 'Settings' },
    { keys: 't · ← → · d w m a · n', label: 'In the calendar: today, previous/next, views, new event' },
  ];
  // The Desk's keys act on the selection, or else the row with focus.
  const mail = [
    { keys: 'e', label: 'Archive' },
    { keys: '#', label: 'Trash' },
    { keys: '!', label: 'Spam' },
    { keys: 'l', label: 'Label' },
    { keys: 's', label: 'Star or unstar' },
    { keys: 'I', label: 'Mark read (Shift+i)' },
    { keys: 'U', label: 'Mark unread (Shift+u)' },
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
          <h3 className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">Mail, in the Desk</h3>
          <ul className="space-y-1">
            {mail.map(m => <li key={m.keys} className="flex items-center justify-between"><span>{m.label}</span><Key k={m.keys} /></li>)}
          </ul>
        </section>
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

interface PaletteItem { id: string; label: string; keybinding?: string; run: () => void; setting?: SettingEntry }

/**
 * Find anything: every command, every setting and every saved query, matched
 * as you type. Every word typed has to begin a word somewhere, in any order,
 * so "dark theme" and "theme dark" both find the theme.
 */
const matches = (c: PaletteItem, query: string) => {
  // Settings only once something is typed: forty of them would bury the
  // commands in the list you see on opening.
  if (c.setting) return query.trim() !== '' && matchesSetting(c.setting, query);
  return matchesWords(`${c.label} ${c.id.replace(/[.:-]/g, ' ')}`, query);
};

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
    const settings: PaletteItem[] = SETTINGS.map(st => ({
      id: `setting:${st.id}`,
      label: `Settings › ${SECTION_LABEL[st.section] ?? st.section} › ${st.label}`,
      setting: st,
      run: () => openSetting(st.id),
    }));
    return [...cmds, ...settings, ...queries];
  }, [open, saved]);
  return (
    <CommandPalette<PaletteItem>
      open={open}
      commands={items}
      onClose={onClose}
      placeholder="Find anything — commands, places, settings, saved queries"
      filter={matches}
      onSelect={c => { onClose(); c.run(); }}
    />
  );
};
