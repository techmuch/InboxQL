import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Archive, ArchiveRestore, Calendar, CheckSquare, Clock, Mail, MessageSquare, Paperclip,
  Tag, Trash2, X,
} from 'lucide-react';
import { openQuery } from '../../lib/tabs';
import {
  addChecklistItem, addComment, archiveCard, deleteCard, deleteChecklist, deleteChecklistItem,
  deleteComment, editChecklistItem, editComment, fetchCard, fromDay, moveCard, patchCard,
  renameChecklist, setCardLabels, toDay,
  type CardDetail, type ChecklistItem, type HistoryEvent, type LabelInfo, type List,
} from './api';
import { LabelPicker } from './LabelPicker';
import { labelClass } from './labels';

/**
 * A card, opened: everything Trello's card back has that a personal board
 * needs, plus the mail behind it.
 *
 * Every part saves on its own, through its own small call, so editing the
 * description never undoes a move somebody (or a rule) made meanwhile.
 */
export const CardPanel = ({
  id,
  lists,
  labels,
  onClose,
  onChanged,
  onLabelsChanged,
}: {
  id: string;
  lists: List[];
  labels: LabelInfo[];
  onClose: () => void;
  /** The board should reload: something on the card's front changed. */
  onChanged: () => void;
  onLabelsChanged: () => void;
}) => {
  const [card, setCard] = useState<CardDetail | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    fetchCard(id).then(c => { setCard(c); setError(null); }).catch(e => setError(e.message));
  }, [id]);
  useEffect(load, [load]);

  // Every change goes through here: show the card as it now is, and tell the
  // board, whose front of the card may have changed too.
  const run = async (op: () => Promise<unknown>) => {
    try {
      const out = await op();
      if (out && typeof out === 'object' && 'checklist' in (out as object)) setCard(out as CardDetail);
      else load();
      setError(null);
      onChanged();
    } catch (e: any) {
      setError(e.message ?? String(e));
    }
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      // Escape in a field abandons the edit; the field handles it.
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT')) return;
      onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const colorOf = useMemo(() => new Map(labels.map(l => [l.name, l.color])), [labels]);
  const listTitle = useMemo(() => new Map(lists.map(l => [l.id, l.title])), [lists]);

  return (
    <div
      className="absolute inset-0 z-20 bg-black/40 flex items-start justify-center overflow-auto py-10 px-4"
      onMouseDown={e => { if (e.target === e.currentTarget) onClose(); }}
    >
      <article
        role="dialog"
        aria-label={card?.title ?? 'Card'}
        className="relative w-full max-w-3xl bg-background border border-border shadow-xl"
      >
        <button
          type="button"
          onClick={onClose}
          aria-label="Close card"
          className="absolute right-2 top-2 p-1.5 text-muted-foreground hover:text-foreground hover:bg-accent/40"
        >
          <X size={16} />
        </button>

        {!card ? (
          <p className="p-6 text-sm text-muted-foreground">{error ?? 'Loading…'}</p>
        ) : (
          <div className="p-5 flex flex-col gap-5">
            <header className="pr-8">
              <TitleEditor
                value={card.title}
                onSave={title => run(() => patchCard(card.id, { title }))}
              />
              <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span>in list</span>
                <select
                  aria-label="List"
                  value={card.status}
                  onChange={e => run(() => moveCard(card.id, e.target.value, ''))}
                  className="bg-background border border-border px-1.5 py-0.5 text-xs text-foreground"
                >
                  {lists.map(l => <option key={l.id} value={l.id}>{l.title}</option>)}
                  {!lists.some(l => l.id === card.status) && <option value={card.status}>{card.status}</option>}
                </select>
                {card.archivedAt && (
                  <span className="px-1.5 py-0.5 bg-amber-500/20 text-amber-700 dark:text-amber-300">
                    Archived
                  </span>
                )}
                {card.origin !== 'human' && (
                  <span title="Who raised this card">raised by {card.origin}
                    {card.confidence !== undefined && card.confidence !== null && ` · ${Math.round(card.confidence * 100)}%`}
                  </span>
                )}
              </div>
            </header>

            {error && <p className="text-sm text-destructive">{error}</p>}

            <section className="flex flex-wrap gap-x-6 gap-y-3" aria-label="Details">
              <Field label="Labels" icon={<Tag size={12} />}>
                <div className="flex flex-wrap items-center gap-1">
                  {(card.labels ?? []).map(l => (
                    <span key={l} className={`px-2 py-0.5 text-xs font-medium ${labelClass(colorOf.get(l))}`}>{l}</span>
                  ))}
                  <LabelPicker
                    labels={labels}
                    selected={card.labels ?? []}
                    onToggle={(name, on) => run(() => setCardLabels(card.id, on ? { add: [name] } : { remove: [name] }))}
                    onColors={onLabelsChanged}
                  />
                </div>
              </Field>
              <Field label="Start" icon={<Clock size={12} />}>
                <input
                  type="date"
                  aria-label="Start date"
                  value={toDay(card.startAt)}
                  onChange={e => run(() => patchCard(card.id, { startAt: fromDay(e.target.value) }))}
                  className="bg-background border border-border px-1.5 py-0.5 text-sm"
                />
              </Field>
              <Field label="Due" icon={<Calendar size={12} />}>
                <div className="flex items-center gap-2">
                  <input
                    type="date"
                    aria-label="Due date"
                    value={toDay(card.dueAt)}
                    onChange={e => run(() => patchCard(card.id, { dueAt: fromDay(e.target.value) }))}
                    className="bg-background border border-border px-1.5 py-0.5 text-sm"
                  />
                  <DueBadge card={card} lists={lists} />
                </div>
              </Field>
              <Field label="Priority">
                <select
                  aria-label="Priority"
                  value={card.priority ?? ''}
                  onChange={e => run(() => patchCard(card.id, { priority: e.target.value }))}
                  className="bg-background border border-border px-1.5 py-0.5 text-sm"
                >
                  <option value="">None</option>
                  <option value="high">High</option>
                  <option value="medium">Medium</option>
                  <option value="low">Low</option>
                  {card.priority && !['high', 'medium', 'low'].includes(card.priority) &&
                    <option value={card.priority}>{card.priority}</option>}
                </select>
              </Field>
            </section>

            <Description
              body={card.body ?? ''}
              html={card.bodyHtml ?? ''}
              onSave={body => run(() => patchCard(card.id, { body }))}
            />

            <Checklists card={card} run={run} />

            {(card.sources?.length ?? 0) > 0 && (
              <section aria-label="Mail">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground mb-2 flex items-center gap-1.5">
                  <Mail size={12} /> From this mail
                </h3>
                <ul className="flex flex-col gap-1">
                  {card.sources!.map(s => (
                    <li key={s.messageId}>
                      <button
                        type="button"
                        onClick={() => openQuery(`thread:${s.messageId}`)}
                        className="w-full text-left flex items-center gap-2 text-sm hover:text-primary"
                      >
                        <span className="text-xs text-muted-foreground font-mono shrink-0">
                          {s.date ? new Date(s.date).toLocaleDateString() : ''}
                        </span>
                        <span className="truncate">{s.subject || '(no subject)'}</span>
                        <span className="text-xs text-muted-foreground truncate">{s.from}</span>
                        {(s.attachments ?? 0) > 0 && (
                          <span className="text-xs text-muted-foreground flex items-center gap-0.5 shrink-0">
                            <Paperclip size={10} />{s.attachments}
                          </span>
                        )}
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
            )}

            <Activity card={card} listTitle={listTitle} run={run} />

            <footer className="flex flex-wrap items-center gap-2 border-t border-border pt-3">
              {card.archivedAt ? (
                <button type="button" onClick={() => run(() => archiveCard(card.id, false))} className={action}>
                  <ArchiveRestore size={14} /> Send to board
                </button>
              ) : (
                <button type="button" onClick={() => run(() => archiveCard(card.id, true))} className={action}>
                  <Archive size={14} /> Archive
                </button>
              )}
              <DeleteCard
                onDelete={async () => {
                  try {
                    await deleteCard(card.id);
                    onChanged();
                    onClose();
                  } catch (e: any) {
                    setError(e.message);
                  }
                }}
              />
              <span className="ml-auto text-xs text-muted-foreground font-mono" title={card.id}>
                {card.id.slice(0, 8)}
              </span>
            </footer>
          </div>
        )}
      </article>
    </div>
  );
};

const action = 'flex items-center gap-1.5 px-2.5 py-1 text-sm border border-border hover:bg-accent/40';

const Field = ({ label, icon, children }: { label: string; icon?: React.ReactNode; children: React.ReactNode }) => (
  <div className="flex flex-col gap-1">
    <span className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground flex items-center gap-1">
      {icon}{label}
    </span>
    {children}
  </div>
);

/** A due date's state: finished when the card is in a closed list. */
const DueBadge = ({ card, lists }: { card: CardDetail; lists: List[] }) => {
  if (!card.dueAt) return null;
  const closed = lists.find(l => l.id === card.status)?.kind === 'closed';
  if (closed) return <span className="text-xs px-1.5 py-0.5 bg-green-600 text-white">Complete</span>;
  const due = new Date(card.dueAt).getTime();
  const now = Date.now();
  if (due < now) return <span className="text-xs px-1.5 py-0.5 bg-red-600 text-white">Overdue</span>;
  if (due - now < 24 * 3600_000) return <span className="text-xs px-1.5 py-0.5 bg-yellow-400 text-black">Due soon</span>;
  return null;
};

const TitleEditor = ({ value, onSave }: { value: string; onSave: (v: string) => void }) => {
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  const commit = () => {
    const v = draft.trim();
    if (v && v !== value) onSave(v);
    else setDraft(value);
  };
  return (
    <textarea
      aria-label="Title"
      value={draft}
      rows={1}
      onChange={e => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={e => {
        if (e.key === 'Enter') { e.preventDefault(); (e.target as HTMLTextAreaElement).blur(); }
        if (e.key === 'Escape') { setDraft(value); (e.target as HTMLTextAreaElement).blur(); }
      }}
      className="w-full resize-none bg-transparent text-lg font-semibold leading-snug outline-none
                 focus:bg-background focus:ring-1 focus:ring-primary px-1 -mx-1"
    />
  );
};

/**
 * The description: Markdown, rendered by the server. Raw HTML in it is
 * escaped there, so what is shown here is never the description's own markup.
 */
const Description = ({ body, html, onSave }: { body: string; html: string; onSave: (v: string) => void }) => {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(body);
  useEffect(() => { if (!editing) setDraft(body); }, [body, editing]);

  return (
    <section aria-label="Description">
      <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground mb-2">Description</h3>
      {editing ? (
        <div className="flex flex-col gap-2">
          <textarea
            autoFocus
            aria-label="Description"
            value={draft}
            onChange={e => setDraft(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { onSave(draft); setEditing(false); }
              if (e.key === 'Escape') setEditing(false);
            }}
            placeholder="Markdown: **bold**, - lists, [links](https://…)"
            className="min-h-32 bg-background border border-border p-2 text-sm font-mono outline-none focus:border-primary"
          />
          <div className="flex gap-2">
            <button type="button" onClick={() => { onSave(draft); setEditing(false); }}
              className="px-3 py-1 text-sm bg-primary text-primary-foreground">Save</button>
            <button type="button" onClick={() => setEditing(false)} className="px-3 py-1 text-sm hover:bg-accent/40">Cancel</button>
          </div>
        </div>
      ) : html ? (
        <div
          className="iql-doc text-sm cursor-text"
          onClick={e => {
            const a = (e.target as HTMLElement).closest('a');
            if (a) {
              e.preventDefault();
              const href = a.getAttribute('href') ?? '';
              if (/^https?:\/\//i.test(href)) window.open(href, '_blank', 'noopener,noreferrer');
              return;
            }
            setEditing(true);
          }}
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="w-full text-left text-sm text-muted-foreground bg-muted/40 hover:bg-muted px-3 py-3"
        >
          Add a more detailed description…
        </button>
      )}
    </section>
  );
};

const Checklists = ({ card, run }: { card: CardDetail; run: (op: () => Promise<unknown>) => void }) => {
  const groups = useMemo(() => {
    const out: { name: string; items: ChecklistItem[] }[] = [];
    for (const it of card.checklist) {
      const g = out.find(x => x.name === it.checklist);
      if (g) g.items.push(it);
      else out.push({ name: it.checklist, items: [it] });
    }
    return out;
  }, [card.checklist]);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('Checklist');
  const [first, setFirst] = useState('');

  return (
    <section aria-label="Checklists" className="flex flex-col gap-4">
      {groups.map(g => (
        <ChecklistGroup key={g.name} card={card} name={g.name} items={g.items} run={run} />
      ))}
      {adding ? (
        <form
          className="flex flex-wrap gap-2"
          onSubmit={e => {
            e.preventDefault();
            // A checklist exists once it has an item, so naming one asks for
            // its first line in the same breath.
            if (!first.trim()) return;
            run(() => addChecklistItem(card.id, first.trim(), name.trim() || 'Checklist'));
            setFirst('');
            setAdding(false);
          }}
        >
          <input aria-label="Checklist name" value={name} onChange={e => setName(e.target.value)}
            className="w-40 bg-background border border-border px-2 py-1 text-sm" />
          <input autoFocus aria-label="First item" placeholder="First item" value={first}
            onChange={e => setFirst(e.target.value)}
            className="flex-1 min-w-40 bg-background border border-border px-2 py-1 text-sm" />
          <button type="submit" className="px-3 py-1 text-sm bg-primary text-primary-foreground">Add</button>
          <button type="button" onClick={() => setAdding(false)} className="px-2 py-1 text-sm hover:bg-accent/40">Cancel</button>
        </form>
      ) : (
        <button type="button" onClick={() => { setName(groups.length ? `Checklist ${groups.length + 1}` : 'Checklist'); setAdding(true); }}
          className={`${action} self-start`}>
          <CheckSquare size={14} /> Add a checklist
        </button>
      )}
    </section>
  );
};

const ChecklistGroup = ({
  card, name, items, run,
}: { card: CardDetail; name: string; items: ChecklistItem[]; run: (op: () => Promise<unknown>) => void }) => {
  const done = items.filter(i => i.done).length;
  const pct = items.length ? Math.round((done / items.length) * 100) : 0;
  const [text, setText] = useState('');
  const [renaming, setRenaming] = useState(false);
  const [title, setTitle] = useState(name);

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <CheckSquare size={14} className="text-muted-foreground" />
        {renaming ? (
          <input
            autoFocus
            aria-label="Checklist name"
            value={title}
            onChange={e => setTitle(e.target.value)}
            onBlur={() => { setRenaming(false); if (title.trim() && title !== name) run(() => renameChecklist(card.id, name, title.trim())); }}
            onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') { setTitle(name); setRenaming(false); } }}
            className="flex-1 bg-background border border-border px-1.5 py-0.5 text-sm font-semibold"
          />
        ) : (
          <h3 className="flex-1 text-sm font-semibold cursor-text" onClick={() => setRenaming(true)}>{name}</h3>
        )}
        <button
          type="button"
          onClick={() => run(() => deleteChecklist(card.id, name))}
          className="text-xs text-muted-foreground hover:text-destructive"
        >
          Delete
        </button>
      </div>
      <div className="flex items-center gap-2">
        <span className="text-[11px] text-muted-foreground font-mono w-8 text-right">{pct}%</span>
        <div className="flex-1 h-1.5 bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
          <div className={`h-full ${pct === 100 ? 'bg-green-600' : 'bg-primary'}`} style={{ width: `${pct}%` }} />
        </div>
      </div>
      <ul className="flex flex-col">
        {items.map(it => <ChecklistLine key={it.id} item={it} run={run} />)}
      </ul>
      <form
        className="flex gap-2 pl-10"
        onSubmit={e => {
          e.preventDefault();
          if (!text.trim()) return;
          run(() => addChecklistItem(card.id, text.trim(), name));
          setText('');
        }}
      >
        <input
          aria-label={`Add an item to ${name}`}
          placeholder="Add an item"
          value={text}
          onChange={e => setText(e.target.value)}
          className="flex-1 bg-background border border-border px-2 py-1 text-sm"
        />
      </form>
    </div>
  );
};

const ChecklistLine = ({ item, run }: { item: ChecklistItem; run: (op: () => Promise<unknown>) => void }) => {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(item.text);
  useEffect(() => setText(item.text), [item.text]);
  return (
    <li className="group flex items-center gap-2 pl-4 py-0.5 hover:bg-accent/20">
      <input
        type="checkbox"
        aria-label={item.text}
        checked={item.done}
        onChange={e => run(() => editChecklistItem(item.id, { done: e.target.checked }))}
        className="w-4 h-4 accent-[var(--color-primary,#2563eb)]"
      />
      {editing ? (
        <input
          autoFocus
          aria-label="Item text"
          value={text}
          onChange={e => setText(e.target.value)}
          onBlur={() => { setEditing(false); if (text.trim() && text !== item.text) run(() => editChecklistItem(item.id, { text: text.trim() })); }}
          onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') { setText(item.text); setEditing(false); } }}
          className="flex-1 bg-background border border-border px-1.5 py-0.5 text-sm"
        />
      ) : (
        <span
          className={`flex-1 text-sm cursor-text ${item.done ? 'line-through text-muted-foreground' : ''}`}
          onClick={() => setEditing(true)}
        >
          {item.text}
        </span>
      )}
      <button
        type="button"
        aria-label={`Remove ${item.text}`}
        onClick={() => run(() => deleteChecklistItem(item.id))}
        className="opacity-0 group-hover:opacity-100 focus:opacity-100 p-0.5 text-muted-foreground hover:text-destructive"
      >
        <X size={12} />
      </button>
    </li>
  );
};

/** Comments and history together, newest first, as Trello's activity is. */
const Activity = ({
  card, listTitle, run,
}: { card: CardDetail; listTitle: Map<string, string>; run: (op: () => Promise<unknown>) => void }) => {
  const [text, setText] = useState('');
  const [showHistory, setShowHistory] = useState(true);
  const entries = useMemo(() => {
    const out: ({ at: string; comment: CardDetail['commentList'][number] } | { at: string; event: HistoryEvent })[] = [];
    for (const c of card.commentList) out.push({ at: c.createdAt, comment: c });
    if (showHistory) for (const e of card.history) out.push({ at: e.at, event: e });
    return out.sort((a, b) => b.at.localeCompare(a.at));
  }, [card.commentList, card.history, showHistory]);

  return (
    <section aria-label="Activity" className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <h3 className="flex-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground flex items-center gap-1.5">
          <MessageSquare size={12} /> Activity
        </h3>
        <button type="button" onClick={() => setShowHistory(s => !s)} className="text-xs text-muted-foreground hover:text-foreground">
          {showHistory ? 'Hide details' : 'Show details'}
        </button>
      </div>
      <form
        className="flex flex-col gap-1.5"
        onSubmit={e => {
          e.preventDefault();
          if (!text.trim()) return;
          run(() => addComment(card.id, text.trim()));
          setText('');
        }}
      >
        <textarea
          aria-label="Write a comment"
          placeholder="Write a comment…"
          value={text}
          onChange={e => setText(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              (e.currentTarget.form as HTMLFormElement).requestSubmit();
            }
          }}
          className="min-h-14 bg-background border border-border p-2 text-sm outline-none focus:border-primary"
        />
        {text.trim() && (
          <button type="submit" className="self-start px-3 py-1 text-sm bg-primary text-primary-foreground">Save</button>
        )}
      </form>
      <ol className="flex flex-col gap-2">
        {entries.map(e => 'comment' in e
          ? <CommentLine key={e.comment.id} comment={e.comment} run={run} />
          : <li key={e.event.id} className="text-xs text-muted-foreground">
              <span className="text-foreground/80">{describe(e.event, listTitle)}</span>
              {e.event.actor !== 'human' && <span> · by {e.event.actor}</span>}
              <span> · {new Date(e.event.at).toLocaleString()}</span>
            </li>)}
      </ol>
    </section>
  );
};

const CommentLine = ({ comment, run }: { comment: CardDetail['commentList'][number]; run: (op: () => Promise<unknown>) => void }) => {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(comment.body);
  const [confirm, setConfirm] = useState(false);
  return (
    <li className="flex flex-col gap-1">
      <div className="text-xs text-muted-foreground">
        <span className="font-semibold text-foreground">{comment.actor === 'human' ? 'You' : comment.actor}</span>
        {' · '}{new Date(comment.createdAt).toLocaleString()}
        {comment.updatedAt !== comment.createdAt && ' · edited'}
      </div>
      {editing ? (
        <div className="flex flex-col gap-1">
          <textarea autoFocus aria-label="Edit comment" value={text} onChange={e => setText(e.target.value)}
            className="min-h-14 bg-background border border-border p-2 text-sm" />
          <div className="flex gap-2">
            <button type="button" className="px-3 py-1 text-sm bg-primary text-primary-foreground"
              onClick={() => { run(() => editComment(comment.id, text)); setEditing(false); }}>Save</button>
            <button type="button" className="px-2 py-1 text-sm hover:bg-accent/40" onClick={() => setEditing(false)}>Cancel</button>
          </div>
        </div>
      ) : (
        <div className="iql-doc text-sm bg-muted/40 px-3 py-2" dangerouslySetInnerHTML={{ __html: comment.bodyHtml ?? '' }} />
      )}
      {!editing && (
        <div className="flex gap-3 text-xs text-muted-foreground">
          <button type="button" className="hover:text-foreground underline" onClick={() => setEditing(true)}>Edit</button>
          {confirm ? (
            <>
              <button type="button" className="text-destructive underline" onClick={() => run(() => deleteComment(comment.id))}>Delete it</button>
              <button type="button" className="hover:text-foreground" onClick={() => setConfirm(false)}>Keep</button>
            </>
          ) : (
            <button type="button" className="hover:text-foreground underline" onClick={() => setConfirm(true)}>Delete</button>
          )}
        </div>
      )}
    </li>
  );
};

/** A history event, said the way a person would. */
export const describe = (e: HistoryEvent, listTitle: Map<string, string>): string => {
  const list = (id?: string) => (id ? listTitle.get(id) ?? id : '');
  switch (e.kind) {
    case 'created': return `Added to ${list(e.to)}`;
    case 'status': return `Moved from ${list(e.from)} to ${list(e.to)}`;
    case 'priority': return e.to ? `Priority set to ${e.to}` : `Priority cleared`;
    case 'due': return e.to ? `Due date set to ${e.to}` : `Due date removed`;
    case 'start': return e.to ? `Start date set to ${e.to}` : `Start date removed`;
    case 'evidence': return `More mail attached`;
    case 'merged': return `Merged “${e.from}” into this card`;
    case 'archived': return `Archived`;
    case 'restored': return `Sent back to the board`;
    case 'label': return e.to ? `Label “${e.to}” added` : `Label “${e.from}” removed`;
    case 'checked': return `Completed “${e.to}”`;
    default: return e.kind;
  }
};

const DeleteCard = ({ onDelete }: { onDelete: () => void }) => {
  const [confirm, setConfirm] = useState(false);
  const timer = useRef<number | null>(null);
  useEffect(() => () => { if (timer.current) window.clearTimeout(timer.current); }, []);
  if (!confirm) {
    return (
      <button type="button" className={action} onClick={() => {
        setConfirm(true);
        timer.current = window.setTimeout(() => setConfirm(false), 5000);
      }}>
        <Trash2 size={14} /> Delete
      </button>
    );
  }
  return (
    <span className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Delete for good? Its mail stays.</span>
      <button type="button" onClick={onDelete} className="px-2.5 py-1 bg-destructive text-destructive-foreground">Delete card</button>
      <button type="button" onClick={() => setConfirm(false)} className="px-2 py-1 hover:bg-accent/40">Keep</button>
    </span>
  );
};

