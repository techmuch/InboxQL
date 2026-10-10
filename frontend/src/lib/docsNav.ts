import { openTool } from './tabs';

/**
 * Opening the documentation at a guide, or a heading within one.
 *
 * The reader is a tab, so it may not be mounted when asked. The request is
 * kept until a reader takes it, and announced for one already open.
 */
export interface DocRequest {
  slug: string;
  anchor?: string;
  /** Distinguishes asking twice for the same place, so the second still scrolls. */
  at?: number;
}

export const DOC_EVENT = 'iql:open-doc';
export const DOCS_TAB = 'docs';

let pending: DocRequest | null = null;

export function openDoc(slug = 'getting-started', anchor?: string) {
  pending = { slug, anchor, at: Date.now() };
  window.dispatchEvent(new CustomEvent(DOC_EVENT, { detail: pending }));
  openTool(DOCS_TAB, 'Documentation');
}

export function takeDocRequest(): DocRequest | null {
  const r = pending;
  pending = null;
  return r;
}
