import { useEffect, useState } from 'react';
import { Archive, Eye, Loader2, MailOpen, Tag } from 'lucide-react';
import { compose } from '../../lib/filters';
import { bulkApply, type BulkResult } from '../../lib/mail';
import { runQuery } from './api';

/** What the folder can be grouped by, and how a group is written as a term. */
export const GROUPINGS: { id: string; label: string }[] = [
  { id: 'from', label: 'Sender' },
  { id: 'domain', label: 'Domain' },
  { id: 'importance', label: 'Importance' },
  { id: 'sentby', label: 'Kind of sender' },
];

/**
 * The term that is one group: what Show adds to the query, and what a
 * group's actions act on. Null for a group no term can name.
 */
export const groupTerm = (by: string, label: string): string | null => {
  if (!label || label.startsWith('(')) return null;
  const plain = /^[^\s"()]+$/.test(label);
  switch (by) {
    case 'from': return plain ? `from:=${label}` : null;
    case 'domain': return plain ? `from:*@${label}` : null;
    case 'importance': return `importance:${label}`;
    case 'sentby': return `sentby:${label}`;
  }
  return null;
};

interface Group { label: string; value: number }

/**
 * The folder, in groups: each with how much it holds and what can be done to
 * all of it at once. This is the "deal with every newsletter" view — two
 * clicks to clear a sender, whatever their count.
 */
export const GroupView = ({
  query, by, labels, onShow, onDone,
}: {
  query: string;
  by: string;
  labels: string[];
  onShow: (term: string) => void;
  onDone: (said: string, result: BulkResult) => void;
}) => {
  const [base, setBase] = useState<string | null>(null);
  const [groups, setGroups] = useState<Group[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [labelling, setLabelling] = useState<string | null>(null);
  const [label, setLabel] = useState('');
  const [reload, setReload] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setGroups(null);
    (async () => {
      try {
        // A conversation view groups the same mail; the stage is dropped so
        // the count is of messages.
        const b = await compose(query, { dropStage: 'timeline' });
        const res: any = await runQuery(`${b} | count by ${by}`, 200, 0);
        if (cancelled) return;
        setBase(b);
        setGroups((res.groups ?? []).map((g: any) => ({ label: String(g.label), value: Number(g.value) })));
        setError(null);
      } catch (e: any) {
        if (!cancelled) setError(e.message ?? String(e));
      }
    })();
    return () => { cancelled = true; };
  }, [query, by, reload]);

  const act = async (g: Group, action: 'archive' | 'read' | 'label', said: string, value = '') => {
    const term = groupTerm(by, g.label);
    if (!term || base === null) return;
    setBusy(g.label);
    try {
      const out = await bulkApply(`${base} ${term}`, action, value);
      onDone(`${said} ${out.changed.toLocaleString()} from ${g.label}.`, out);
      setReload(r => r + 1);
    } catch (e: any) {
      setError(e.message ?? String(e));
    } finally {
      setBusy(null);
    }
  };

  if (error) return <p className="p-4 text-sm text-destructive">{error}</p>;
  if (!groups) {
    return <p className="p-4 text-sm text-muted-foreground flex items-center gap-2"><Loader2 size={14} className="animate-spin" /> Grouping…</p>;
  }
  if (groups.length === 0) return <p className="p-4 text-sm text-muted-foreground">Nothing here to group.</p>;

  const max = Math.max(...groups.map(g => g.value));
  return (
    <div className="flex-1 overflow-auto" role="list" aria-label="Groups">
      {groups.map(g => {
        const term = groupTerm(by, g.label);
        return (
          <div key={g.label} role="listitem" className="group flex items-center gap-3 px-4 py-2 border-b border-border/50 hover:bg-accent/20">
            <div className="w-64 min-w-0">
              <div className="truncate text-sm font-medium" title={g.label}>{g.label}</div>
              <div className="h-1 mt-1 bg-muted"><div className="h-full bg-primary/60" style={{ width: `${(g.value / max) * 100}%` }} /></div>
            </div>
            <span className="w-16 text-right text-sm tabular-nums">{g.value.toLocaleString()}</span>
            {term ? (
              <div className="flex flex-wrap items-center gap-1 text-xs">
                <button type="button" onClick={() => onShow(term)}
                  className="flex items-center gap-1 border border-border px-2 py-0.5 hover:bg-accent/40">
                  <Eye size={12} /> Show
                </button>
                <button type="button" disabled={busy !== null} onClick={() => act(g, 'archive', 'Archived')}
                  className="flex items-center gap-1 border border-border px-2 py-0.5 hover:bg-accent/40 disabled:opacity-50">
                  {busy === g.label ? <Loader2 size={12} className="animate-spin" /> : <Archive size={12} />} Archive all
                </button>
                <button type="button" disabled={busy !== null} onClick={() => act(g, 'read', 'Marked read')}
                  className="flex items-center gap-1 border border-border px-2 py-0.5 hover:bg-accent/40 disabled:opacity-50">
                  <MailOpen size={12} /> Mark read
                </button>
                {labelling === g.label ? (
                  <form className="flex items-center gap-1" onSubmit={e => {
                    e.preventDefault();
                    if (!label.trim()) return;
                    setLabelling(null);
                    void act(g, 'label', `Labelled “${label.trim()}”:`, label.trim());
                  }}>
                    <input autoFocus list="group-labels" aria-label={`Label for ${g.label}`} value={label}
                      onChange={e => setLabel(e.target.value)} onKeyDown={e => { if (e.key === 'Escape') setLabelling(null); }}
                      className="w-28 bg-background border border-border px-1.5 py-0.5" placeholder="Label" />
                    <datalist id="group-labels">{labels.map(l => <option key={l} value={l} />)}</datalist>
                    <button type="submit" className="border border-border px-2 py-0.5 hover:bg-accent/40">Add</button>
                  </form>
                ) : (
                  <button type="button" disabled={busy !== null} onClick={() => { setLabel(''); setLabelling(g.label); }}
                    className="flex items-center gap-1 border border-border px-2 py-0.5 hover:bg-accent/40 disabled:opacity-50">
                    <Tag size={12} /> Label…
                  </button>
                )}
              </div>
            ) : (
              <span className="text-xs text-muted-foreground">Not judged yet — the harness answers this.</span>
            )}
          </div>
        );
      })}
    </div>
  );
};
