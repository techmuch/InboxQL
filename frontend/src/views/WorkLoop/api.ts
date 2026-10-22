/** The Work loop page's view of /api/work. Shapes follow internal/work. */

export interface Tally { n: number; right: number; wrong: number; open: number }
export interface Estimate { accuracy: number; low: number; high: number; n: number; open?: number }

export type RouteStatus = 'unmeasured' | 'meeting' | 'uncertain' | 'missing';
export type Cost = 'high' | 'medium' | 'low';

export interface ElementRoute {
  element: string;
  target: number;
  cost: Cost;
  custom: boolean;
  random: Tally;
  guessed: Tally;
  targeted: Tally;
  estimate?: Estimate;
  status: RouteStatus;
  guess: boolean;
  why: string;
  checkRate: number;
}

export interface ElementMetrics {
  element: string;
  about: string;
  unit: string;
  answered: number;
  prefilled: number;
  found: number;
  withConfidence: number;
  reviewed: number;
  reviewedRight: number;
  route: ElementRoute;
}

export interface WorkDay { day: string; stored: number; refused: number; checked: number }
export interface Outcome { total: number; kept: number; undone: number; open: number }
export interface CalBin { lo: number; hi: number; n: number; right: number; mean: number }

export interface Metrics {
  since: string;
  model?: string;
  models: string[];
  reading: {
    stored: number;
    refused: number;
    medianSeconds: number;
    days: WorkDay[];
    problems: { problem: string; count: number }[];
    batches: number;
    leased: number;
    expired: number;
    refusalRate: number;
    perDay: number;
    guessedShare: number;
    usage?: { batches: number; conversations: number; tokensPerConversation: number; costPerConversation: number; secondsPerConversation: number };
  };
  checks: { random: Tally; targeted: Tally; open: number; due: number; rate: number };
  accuracy?: Estimate;
  elements: ElementMetrics[];
  actions: Record<string, Outcome>;
  calibration: { bins: CalBin[]; unrated: number };
  flagged: number;
}

export interface Answer {
  level?: string;
  records?: Record<string, unknown>[];
  tags?: string[];
  none?: string;
  skip?: string;
  confidence?: number;
  why?: string;
  quote?: string;
}

export interface Disagreement {
  id: string;
  thread: string;
  messageId: string;
  target: string;
  element: string;
  about: string;
  question: string;
  shape: string;
  unit: string;
  subject: string;
  from: string;
  date: string;
  snippet: string;
  originalModel?: string;
  originalPrefilled?: boolean;
  checkerModel?: string;
  originalSays: string;
  checkerSays: string;
  originalAnswer: Answer;
  checkerAnswer: Answer;
  stratum: string;
  resolution?: string;
}

export type Choice = 'original' | 'checker' | 'neither';
export type Period = '7d' | '30d' | '90d' | 'all';

async function json<T>(r: Response): Promise<T> {
  const text = await r.text();
  const body = text ? JSON.parse(text) : {};
  if (!r.ok) throw new Error(body?.error ?? `Request failed (${r.status})`);
  return body as T;
}

export const getMetrics = (period: Period, model: string) => {
  const p = new URLSearchParams({ since: period });
  if (model) p.set('model', model);
  return fetch(`/api/work/metrics?${p}`).then(r => json<Metrics>(r));
};

export const getBacklog = () =>
  fetch('/api/work/backlog').then(r => json<{ elements: { element: string; pending: number; total: number }[] }>(r));

export const getDisagreements = () =>
  fetch('/api/work/disagreements').then(r => json<{ disagreements: Disagreement[]; open: number }>(r));

export const resolveDisagreement = (id: string, choice: Choice) =>
  fetch('/api/work/disagreements/resolve', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id, choice }),
  }).then(r => json<Disagreement>(r));

export const setTarget = (element: string, target: number, cost: Cost) =>
  fetch('/api/work/targets', {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ element, target, cost }),
  }).then(r => json<{ elements: Record<string, ElementRoute> }>(r));
