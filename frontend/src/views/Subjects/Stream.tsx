import { useEffect, useState } from 'react';
import { runQuery } from '../../lib/counts';
import { openMessage, openQuery } from '../../lib/tabs';
import { waited } from '../../lib/waited';

interface Msg { id: string; from: string; fromName?: string; subject?: string; preview?: string; date: string; labels?: string[] }

/** A subject's mail, newest first, with a way to the whole list. */
export const Stream = ({ query, title, limit = 15 }: { query: string; title: string; limit?: number }) => {
  const [rows, setRows] = useState<Msg[] | null>(null);
  const [total, setTotal] = useState<number | null>(null);
  useEffect(() => {
    const ctl = new AbortController();
    runQuery<{ messages?: Msg[]; count?: number }>(query, limit, ctl.signal).then(r => {
      if (ctl.signal.aborted) return;
      setRows(r?.messages ?? []);
      setTotal(r?.count ?? null);
    });
    return () => ctl.abort();
  }, [query, limit]);
  const id = `s-${title.replace(/\W+/g, '-').toLowerCase()}`;
  return (
    <section className="mt-7" aria-labelledby={id}>
      <header className="mb-2 flex items-baseline gap-3">
        <h2 id={id} className="flex-1 text-lg font-semibold">{title}</h2>
        <button type="button" className="text-sm underline underline-offset-2" onClick={() => openQuery(query)}>Open as a list</button>
      </header>
      {rows == null ? <p className="text-sm text-muted-foreground">Loading…</p> : rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">No mail.</p>
      ) : (
        <ol className="divide-y divide-border border border-border bg-card">
          {rows.map(m => (
            <li key={m.id}>
              <button type="button" onClick={() => openMessage(m)} className="grid w-full grid-cols-[5rem_minmax(0,1fr)] gap-3 px-4 py-2 text-left hover:bg-accent">
                <span className="text-xs tabular-nums text-muted-foreground">{waited(m.date)}</span>
                <span className="min-w-0">
                  <span className="font-medium">{m.fromName || m.from}</span>
                  <span className="text-sm text-muted-foreground"> · {m.subject}</span>
                  {m.preview && <span className="block truncate text-sm text-foreground/80">{m.preview}</span>}
                </span>
              </button>
            </li>
          ))}
        </ol>
      )}
      {total != null && rows != null && total > rows.length && <p className="mt-1 text-xs text-muted-foreground">Showing {rows.length}.</p>}
    </section>
  );
};
