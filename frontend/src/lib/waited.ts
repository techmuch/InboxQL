/** "3 h", "2 days" — how long it has waited, in words a row can carry. */
export function waited(date: string, now = Date.now()): string {
  const ms = now - new Date(date).getTime();
  const h = Math.round(ms / 3_600_000);
  if (h < 1) return 'just now';
  if (h < 24) return `${h} h`;
  const d = Math.round(h / 24);
  return d === 1 ? '1 day' : `${d} days`;
}
