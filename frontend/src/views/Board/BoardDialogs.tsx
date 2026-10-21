import { useCallback, useEffect, useState } from 'react';
import { Play, Plus, Trash2, X } from 'lucide-react';
import {
  archiveCard, createRule, deleteRule, editList, editRule, fetchLists, fetchRules, runRules,
  type List, type Rule, type RuleAction, type Ticket,
} from './api';

const Dialog = ({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) => {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  return (
    <div className="absolute inset-0 z-20 bg-black/40 flex items-start justify-center overflow-auto py-10 px-4"
      onMouseDown={e => { if (e.target === e.currentTarget) onClose(); }}>
      <section role="dialog" aria-label={title} className="relative w-full max-w-2xl bg-background border border-border shadow-xl p-5 flex flex-col gap-4">
        <header className="flex items-center gap-2">
          <h2 className="flex-1 text-lg font-semibold">{title}</h2>
          <button type="button" aria-label="Close" onClick={onClose} className="p-1 text-muted-foreground hover:text-foreground">
            <X size={16} />
          </button>
        </header>
        {children}
      </section>
    </div>
  );
};

const ACTION_LABELS: Record<string, string> = {
  'check-all': 'tick every checklist item',
  'add-label': 'add the label',
  'remove-label': 'remove the label',
  'set-due': 'set the due date to',
  'clear-due': 'remove the due date',
  'set-priority': 'set the priority to',
  'archive': 'archive the card',
};
const TAKES_VALUE = new Set(['add-label', 'remove-label', 'set-due', 'set-priority']);

/**
 * Automation: a trigger and some actions. Small on purpose — a card enters a
 * list, or mail arrives — and nothing here moves a card between lists, so a
 * rule can never set off another, or itself.
 */
export const RulesDialog = ({ lists, onClose }: { lists: List[]; onClose: () => void }) => {
  const [rules, setRules] = useState<Rule[]>([]);
  const [actions, setActions] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);

  const load = useCallback(() => {
    fetchRules().then(r => { setRules(r.rules); setActions(r.actions); }).catch(e => setError(e.message));
  }, []);
  useEffect(load, [load]);

  const act = (op: () => Promise<unknown>) => op().then(load).catch(e => setError(e.message ?? String(e)));
  const title = (id: string) => lists.find(l => l.id === id)?.title ?? id;

  return (
    <Dialog title="Automation" onClose={onClose}>
      <p className="text-sm text-muted-foreground">
        A rule does something when a card enters a list, or makes a card when mail arrives — one card
        per conversation. Mail rules run after every sync.
      </p>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {note && <p className="text-sm text-muted-foreground">{note}</p>}

      <ul className="flex flex-col divide-y divide-border border border-border">
        {rules.length === 0 && <li className="p-3 text-sm text-muted-foreground">No rules yet.</li>}
        {rules.map(r => (
          <li key={r.id} className="p-3 flex items-start gap-3">
            <input type="checkbox" aria-label={`${r.title} is on`} checked={r.enabled}
              onChange={e => act(() => editRule({ ...r, enabled: e.target.checked }))} className="mt-1" />
            <div className="flex-1 min-w-0">
              <p className={`text-sm ${r.enabled ? '' : 'text-muted-foreground line-through'}`}>
                {r.trigger === 'mail'
                  ? <>When mail matching <code className="font-mono text-xs">{r.query}</code> arrives, make a card in <b>{title(r.list)}</b></>
                  : <>When a card enters <b>{title(r.list)}</b></>}
                {r.actions.length > 0 && <>, {r.actions.map(a => `${ACTION_LABELS[a.do] ?? a.do}${a.value ? ` ${a.value}` : ''}`).join(', ')}</>}
              </p>
              {r.lastRunAt && <p className="text-xs text-muted-foreground">Last ran {new Date(r.lastRunAt).toLocaleString()}</p>}
            </div>
            {r.trigger === 'mail' && (
              <button type="button" title="Run now, over older mail too" aria-label={`Run ${r.title}`}
                onClick={() => runRules(r.id, true)
                  .then(o => { setNote(`${o.matched} message(s) matched: ${o.created} card(s) made, ${o.attached} added to cards already made.`); load(); })
                  .catch(e => setError(e.message))}
                className="p-1 text-muted-foreground hover:text-foreground">
                <Play size={14} />
              </button>
            )}
            <button type="button" aria-label={`Delete ${r.title}`} onClick={() => act(() => deleteRule(r.id))}
              className="p-1 text-muted-foreground hover:text-destructive">
              <Trash2 size={14} />
            </button>
          </li>
        ))}
      </ul>

      {adding ? (
        <RuleForm lists={lists} actions={actions}
          onSave={async r => { await createRule(r); setAdding(false); load(); }}
          onCancel={() => setAdding(false)} />
      ) : (
        <button type="button" onClick={() => setAdding(true)}
          className="self-start flex items-center gap-1.5 px-2.5 py-1 text-sm border border-border hover:bg-accent/40">
          <Plus size={14} /> New rule
        </button>
      )}
    </Dialog>
  );
};

const RuleForm = ({
  lists, actions, onSave, onCancel,
}: {
  lists: List[];
  actions: string[];
  onSave: (r: Partial<Rule>) => Promise<void>;
  onCancel: () => void;
}) => {
  const [trigger, setTrigger] = useState<'enter-list' | 'mail'>('enter-list');
  const [list, setList] = useState(lists.find(l => l.kind === 'closed')?.id ?? lists[0]?.id ?? '');
  const [query, setQuery] = useState('');
  const [steps, setSteps] = useState<RuleAction[]>([{ do: 'check-all' }]);
  const [error, setError] = useState<string | null>(null);

  const field = 'bg-background border border-border px-2 py-1 text-sm';
  return (
    <form className="flex flex-col gap-3 border border-border p-3"
      onSubmit={async e => {
        e.preventDefault();
        try {
          await onSave({ trigger, list, query: trigger === 'mail' ? query : '', actions: steps.filter(s => s.do) });
        } catch (err: any) {
          setError(err.message ?? String(err));
        }
      }}>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span>When</span>
        <select aria-label="Trigger" value={trigger} onChange={e => {
          const t = e.target.value as 'enter-list' | 'mail';
          setTrigger(t);
          if (t === 'mail') { setList(lists.find(l => l.kind === 'proposed')?.id ?? list); setSteps([]); }
        }} className={field}>
          <option value="enter-list">a card enters</option>
          <option value="mail">mail arrives matching</option>
        </select>
        {trigger === 'mail' && (
          <input aria-label="Mail query" value={query} onChange={e => setQuery(e.target.value)} spellCheck={false}
            placeholder="from:landlord@example.com" className={`${field} flex-1 min-w-48 font-mono`} />
        )}
        {trigger === 'mail' && <span>make a card in</span>}
        <select aria-label="List" value={list} onChange={e => setList(e.target.value)} className={field}>
          {lists.map(l => <option key={l.id} value={l.id}>{l.title}</option>)}
        </select>
      </div>

      <div className="flex flex-col gap-2">
        <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Then</span>
        {steps.map((s, i) => (
          <div key={i} className="flex items-center gap-2">
            <select aria-label={`Action ${i + 1}`} value={s.do}
              onChange={e => setSteps(st => st.map((x, j) => (j === i ? { do: e.target.value } : x)))} className={field}>
              {actions.map(a => <option key={a} value={a}>{ACTION_LABELS[a] ?? a}</option>)}
            </select>
            {TAKES_VALUE.has(s.do) && (
              <input aria-label={`Value for action ${i + 1}`} value={s.value ?? ''}
                placeholder={s.do === 'set-due' ? '3d, 2w, 2026-11-01' : s.do === 'set-priority' ? 'high' : 'label'}
                onChange={e => setSteps(st => st.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))}
                className={`${field} flex-1`} />
            )}
            <button type="button" aria-label={`Remove action ${i + 1}`} onClick={() => setSteps(st => st.filter((_, j) => j !== i))}
              className="p-1 text-muted-foreground hover:text-destructive"><X size={14} /></button>
          </div>
        ))}
        <button type="button" onClick={() => setSteps(st => [...st, { do: 'add-label', value: '' }])}
          className="self-start text-sm text-muted-foreground hover:text-foreground flex items-center gap-1">
          <Plus size={12} /> Add an action
        </button>
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}
      <div className="flex gap-2">
        <button type="submit" className="px-3 py-1 text-sm bg-primary text-primary-foreground">Make rule</button>
        <button type="button" onClick={onCancel} className="px-2 py-1 text-sm hover:bg-accent/40">Cancel</button>
      </div>
    </form>
  );
};

/** What is off the board: archived cards and lists, to open or send back. */
export const ArchivedDialog = ({ onOpen, onClose }: { onOpen: (id: string) => void; onClose: () => void }) => {
  const [cards, setCards] = useState<Ticket[]>([]);
  const [lists, setLists] = useState<List[]>([]);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    fetch(`/api/tickets?${new URLSearchParams({ q: 'archived:yes | sort updated desc' })}`)
      .then(r => (r.ok ? r.json() : Promise.reject(new Error(`${r.status}`))))
      .then(setCards)
      .catch(e => setError(e.message));
    fetchLists(true).then(ls => setLists(ls.filter(l => l.archivedAt))).catch(() => {});
  }, []);
  useEffect(load, [load]);

  const act = (op: () => Promise<unknown>) => op().then(load).catch(e => setError(e.message ?? String(e)));

  return (
    <Dialog title="Archived" onClose={onClose}>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {lists.length > 0 && (
        <section className="flex flex-col gap-1">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Lists</h3>
          {lists.map(l => (
            <div key={l.id} className="flex items-center gap-2 text-sm">
              <span className="flex-1">{l.title} <span className="text-muted-foreground">· {l.cards} card{l.cards === 1 ? '' : 's'}</span></span>
              <button type="button" onClick={() => act(() => editList(l.id, { archived: false }))}
                className="px-2 py-0.5 text-sm border border-border hover:bg-accent/40">Send to board</button>
            </div>
          ))}
        </section>
      )}
      <section className="flex flex-col gap-1">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Cards</h3>
        {cards.length === 0 && <p className="text-sm text-muted-foreground">No archived cards.</p>}
        {cards.map(c => (
          <div key={c.id} className="flex items-center gap-2 text-sm">
            <button type="button" onClick={() => onOpen(c.id)} className="flex-1 text-left hover:text-primary truncate">{c.title}</button>
            <span className="text-xs text-muted-foreground">{c.status}</span>
            <button type="button" onClick={() => act(() => archiveCard(c.id, false))}
              className="px-2 py-0.5 text-sm border border-border hover:bg-accent/40">Send to board</button>
          </div>
        ))}
      </section>
    </Dialog>
  );
};
