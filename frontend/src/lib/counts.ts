/**
 * How many things a query finds, for the counts beside places.
 *
 * A count is a query like any other — `awaiting:me | count` — so the number
 * beside "Replies I owe" is the same answer the CLI gives, never a second
 * definition kept in step by hand.
 */
export async function queryCount(q: string, signal?: AbortSignal): Promise<number | null> {
  try {
    const params = new URLSearchParams({ q: `${q} | count`, limit: '1' });
    const res = await fetch(`/api/query?${params}`, { signal });
    if (!res.ok) return null;
    const body = await res.json();
    // A count answers in `total`, and a total of zero is left out of the
    // JSON — `count` is then the number of rows (1), not the answer.
    if (body.kind === 'count') return typeof body.total === 'number' ? body.total : 0;
    return typeof body.total === 'number' ? body.total : null;
  } catch {
    return null;
  }
}

/** Run a query and return its JSON, or null. */
export async function runQuery<T = unknown>(q: string, limit = 50, signal?: AbortSignal): Promise<T | null> {
  try {
    const params = new URLSearchParams({ q, limit: String(limit) });
    const res = await fetch(`/api/query?${params}`, { signal });
    if (!res.ok) return null;
    return await res.json();
  } catch {
    return null;
  }
}
