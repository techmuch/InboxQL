import { create } from 'zustand';

/**
 * Which people have a stored picture.
 *
 * Asked once, as a list, so a contact list of a thousand rows makes one
 * request rather than a thousand that mostly answer "none". A picture is only
 * ever loaded from this server — never from Gravatar — and only for an
 * address on this list.
 */
interface AvatarState {
  /** address → when its picture was fetched, which also busts the cache. */
  stored: Record<string, string>;
  loaded: boolean;
  load: () => Promise<void>;
}

export const useAvatarStore = create<AvatarState>((set) => ({
  stored: {},
  loaded: false,
  load: async () => {
    try {
      const r = await fetch('/api/avatars/stored', { cache: 'no-store' });
      if (!r.ok) return;
      const d = await r.json();
      set({ stored: d.stored ?? {}, loaded: true });
    } catch {
      /* no pictures, then: initials everywhere */
    }
  },
}));

let requested = false;

/** The URL of an address's stored picture, or null when there is none. */
export function useAvatarUrl(address?: string | null): string | null {
  const stored = useAvatarStore(s => s.stored);
  const loaded = useAvatarStore(s => s.loaded);
  if (!requested && !loaded) {
    requested = true;
    void useAvatarStore.getState().load();
  }
  const a = address?.trim().toLowerCase();
  if (!a || !stored[a]) return null;
  return `/api/avatars/image?address=${encodeURIComponent(a)}&v=${encodeURIComponent(stored[a])}`;
}

export interface AvatarMeta {
  address: string;
  status: 'stored' | 'none' | 'absent';
  source?: string;
  fetchedAt?: string;
}

async function call(method: string, url: string, body?: unknown): Promise<AvatarMeta> {
  const r = await fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await r.text();
  if (!r.ok) throw new Error(text.trim() || `${r.status}`);
  await useAvatarStore.getState().load();
  return JSON.parse(text);
}

export const avatarMeta = async (address: string): Promise<AvatarMeta> => {
  const r = await fetch(`/api/avatars?address=${encodeURIComponent(address)}`, { cache: 'no-store' });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
};
export const fetchGravatar = (address: string) => call('POST', '/api/avatars/gravatar', { address });
export const removeAvatar = (address: string) => call('DELETE', `/api/avatars?address=${encodeURIComponent(address)}`);

/** Up to two initials from a name, or the first letter of an address. */
export function initials(name?: string | null, address?: string | null): string {
  // A From header gives `"Ada Lovelace" <ada@x.com>`; keep the name part.
  const n = (name ?? '').replace(/<[^>]*>/g, '').replace(/["']/g, '').trim();
  if (n.includes('@')) return n[0]?.toUpperCase() ?? '?';
  const words = n.split(/\s+/).filter(w => /^\p{L}/u.test(w));
  if (words.length >= 2) return (words[0][0] + words[words.length - 1][0]).toUpperCase();
  if (words.length === 1) return words[0][0].toUpperCase();
  return (address ?? '?').trim()[0]?.toUpperCase() ?? '?';
}
