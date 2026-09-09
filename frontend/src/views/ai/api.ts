/**
 * The AI settings surface's view of the server.
 *
 * Every shape here mirrors one the server defines. Nothing in this file decides
 * anything the server already decides — scope, defaulting, and whether a
 * profile can be deleted are all answered there, because they are the same
 * questions the CLI asks and two implementations would drift.
 */

/** A named, complete address for a model: where it runs and how to reach it. */
export interface Profile {
  id: string;
  name: string;
  provider: string;
  model: string;
  endpoint: string;
  /** "local" or "remote" — derived from the endpoint, never stored. */
  scope: 'local' | 'remote';
  isDefault: boolean;
  autoStart: boolean;
  launchMode: string;
  /** The key itself is never sent to the client. */
  hasApiKey: boolean;
  /** What the profile is for. A model serves one or the other. */
  purpose: 'chat' | 'embedding';
  /** Vector width, probed when an embedding profile is saved. */
  dimensions?: number;
}

/** A local model runner this machine may or may not have. */
export interface Runtime {
  provider: string;
  name: string;
  installed: boolean;
  binaryPath?: string;
  running: boolean;
  endpoint: string;
  launchModes: string[];
  models: string[];
  /** The same models with what each is for, and how wide its vectors are. */
  catalog?: { name: string; kind: 'chat' | 'embedding' | 'unknown'; dimensions?: number }[];
  /** Whether this runtime can embed at all. */
  embeddings?: boolean;
  /** Why this runtime yielded no models. Present only when something is wrong. */
  problem?: string;
}

export interface LLMStatus {
  config: {
    profile?: string;
    provider: string;
    model: string;
    endpoint: string;
    scope?: string;
    configured: boolean;
    hasApiKey: boolean;
  };
  profiles: Profile[];
  runtimes: Runtime[];
  activeStatus: { running: boolean; endpoint: string; provider: string; model: string };
}

/** An error the server explained, carrying its own message. */
export class ApiError extends Error {}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  const text = await res.text();
  let body: any = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    // A non-JSON body from an error page is still worth reporting verbatim.
  }
  if (!res.ok) {
    throw new ApiError(body?.error ?? text ?? `request failed (${res.status})`);
  }
  return body as T;
}

const asJson = (body: unknown): RequestInit => ({
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

export const getStatus = () => request<LLMStatus>('/api/llm/status');

export const listProfiles = () =>
  request<{ profiles: Profile[] }>('/api/llm/profiles').then(r => r.profiles ?? []);

/**
 * Save a profile.
 *
 * `apiKey` is omitted rather than sent empty when the user did not touch it:
 * the server reads an absent key as "leave it alone" and an empty one as
 * "remove it". A form that cannot display the stored key must not be able to
 * erase it just by being submitted.
 */
export const saveProfile = (p: Partial<Profile> & { name: string; apiKey?: string }) =>
  request<Profile>('/api/llm/profiles', asJson(p));

export const deleteProfile = (name: string) =>
  request<unknown>(`/api/llm/profiles?name=${encodeURIComponent(name)}`, { method: 'DELETE' });

export const setDefaultProfile = (name: string) =>
  request<unknown>('/api/llm/profiles/default', asJson({ name }));

/**
 * Re-scan local runtimes and report what happened.
 *
 * The old refresh swallowed its errors, so a runtime that was up but whose
 * model endpoint was broken rendered as "no new models" — a fault that reads
 * as a normal empty state. This returns each runtime's `problem`, so the UI
 * can say which it is.
 */
export const refreshRuntimes = (provider?: string) =>
  request<{ runtimes: Runtime[]; models: number }>('/api/llm/refresh', asJson({ provider }));

export const testProfile = (p: { provider: string; model: string; endpoint: string; apiKey?: string }) =>
  request<{ ok: boolean; elapsedMs?: number; reply?: string }>('/api/llm/test', asJson(p));

export const startRuntime = (provider: string, launchMode: string) =>
  request<{ pid: number }>('/api/llm/start', asJson({ provider, launchMode }));

export const stopRuntime = (provider: string) =>
  request<unknown>('/api/llm/stop', asJson({ provider }));

export const getSetting = (key: string) =>
  request<{ value: string }>(`/api/settings?key=${encodeURIComponent(key)}`).then(r => r.value ?? '');

export const putSetting = (key: string, value: string) =>
  fetch('/api/settings', asJson({ key, value })).then(res => {
    if (!res.ok) throw new ApiError('could not save that setting');
  });

// --- annotators -------------------------------------------------------------

export interface Annotator {
  id: string;
  name: string;
  kind: 'label' | 'extract';
  engine: 'rule' | 'llm';
  version: number;
  instructions: string;
  schemaJson?: string;
  profile?: string;
  model?: string;
  allowRemote: boolean;
  progress?: { total: number; evaluated: number; ok: number; empty: number };
  /** What running it would do, answered from its own profile. */
  scope?: 'local' | 'remote';
  endpoint?: string;
  consentMissing?: boolean;
  profileMissing?: boolean;
}

export const listAnnotators = () => request<Annotator[]>('/api/annotators');

export const saveAnnotator = (a: Partial<Annotator> & { name: string }) =>
  request<Annotator>('/api/annotators', asJson(a));

export const deleteAnnotator = (name: string) =>
  request<unknown>(`/api/annotators?name=${encodeURIComponent(name)}`, { method: 'DELETE' });

export interface RunOutcome {
  annotator: string;
  matched?: number;
  empty?: number;
  evaluated?: number;
  records?: number;
  duration?: string;
  dryRun?: boolean;
  pending?: number;
  remote?: boolean;
  consentMissing?: boolean;
  estimatedChars?: number;
}

export const runAnnotator = (name: string, opts: { dryRun?: boolean; limit?: number; scope?: string } = {}) =>
  request<RunOutcome>('/api/annotators/run', asJson({ name, ...opts }));
