import type { QueryTerm } from '../../lib/filters';
import { hasTerm, type Scope } from '../../lib/scope';

/**
 * One-click filters, by what the folder holds.
 *
 * Each is a term anybody could type — `importance:low`, `sentby:system` — so
 * clicking one teaches the language the way the rail does, and the box shows
 * exactly what was added. Filters in a group are either/or: Important and Low
 * are not both on.
 */
interface Filter {
  label: string;
  term: string;
  /** Terms it excludes: the others in its group. */
  group?: string[];
  title?: string;
}

const IMPORTANCE = ['importance:important', 'importance:low'];
const SENT_BY = ['sentby:person', 'sentby:system'];

export const FILTERS: Record<string, Filter[]> = {
  mail: [
    { label: 'Unread', term: 'is:unread' },
    { label: 'Starred', term: 'is:starred' },
    { label: 'Attachments', term: 'has:attachment' },
    { label: 'Important', term: 'importance:important', group: IMPORTANCE, title: 'Conversations the importance element judged important' },
    { label: 'Low', term: 'importance:low', group: IMPORTANCE, title: 'Conversations judged low importance' },
    { label: 'From people', term: 'sentby:person', group: SENT_BY },
    { label: 'From systems', term: 'sentby:system', group: SENT_BY, title: 'Newsletters, notifications and other machines' },
    { label: 'Waiting on me', term: 'awaiting:me', title: 'Conversations judged to be waiting on your reply' },
  ],
  cards: [
    { label: 'Open', term: 'state:open' },
    { label: 'Due this week', term: 'due:7d' },
    { label: 'Proposed', term: 'state:proposed' },
    { label: 'Finished', term: 'state:closed' },
  ],
  files: [
    { label: 'PDFs', term: 'filetype:pdf', group: ['filetype:pdf', 'filetype:image'] },
    { label: 'Images', term: 'filetype:image', group: ['filetype:pdf', 'filetype:image'] },
    { label: 'Over 5 MB', term: 'larger:5mb' },
    { label: 'Sent around', term: 'is:shared', title: 'Files that arrived on more than one message' },
  ],
  people: [
    { label: 'People', term: 'kind:person', group: ['kind:person', 'kind:system'] },
    { label: 'Systems', term: 'kind:system', group: ['kind:person', 'kind:system'] },
    { label: 'Waiting on me', term: 'awaiting:me' },
    { label: 'With notes', term: 'has:notes' },
  ],
  events: [
    { label: 'Next 7 days', term: 'next:7d' },
    { label: 'Needs an answer', term: 'status:needs-action' },
  ],
};

/** The filters for a folder: mail's for every mail folder, a kind's own otherwise. */
export const filtersFor = (scope: Scope): Filter[] =>
  scope.mail ? FILTERS.mail : FILTERS[scope.id] ?? [];

export const QuickFilters = ({
  scope, terms, onToggle,
}: {
  scope: Scope;
  terms: QueryTerm[] | null;
  /** Add `term` (taking off `remove` first), or take it off when it is on. */
  onToggle: (term: string, on: boolean, remove: string[]) => void;
}) => {
  const filters = filtersFor(scope);
  if (filters.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-1" role="group" aria-label="Quick filters">
      {filters.map(f => {
        const on = terms ? hasTerm(terms, f.term) : false;
        return (
          <button
            key={f.term}
            type="button"
            aria-pressed={on}
            title={f.title ? `${f.title} — ${f.term}` : f.term}
            onClick={() => onToggle(f.term, !on, (f.group ?? []).filter(g => g !== f.term && terms && hasTerm(terms, g)))}
            className={`px-2 py-0.5 text-xs border ${
              on ? 'border-primary bg-primary/10 text-primary font-medium' : 'border-border text-muted-foreground hover:text-foreground hover:bg-accent/40'
            }`}
          >
            {f.label}
          </button>
        );
      })}
    </div>
  );
};
