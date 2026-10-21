import { useState } from 'react';
import {
  AlertOctagon, AlertTriangle, Bookmark, Bot, Check, ChevronDown, ChevronUp,
  Archive, Clock, File, Folder, HelpCircle, Inbox, Layout, Mails, Paperclip, Pencil, Plus,
  Search, Send, Settings2, Sparkles, Star, Tag, Ticket, Trash2, Users, X,
} from 'lucide-react';
import type { FacetCount, Facets, LabelCount } from '../../lib/mail';
import type { SavedQuery } from './api';
import { openCompose } from '../../lib/appUi';
import type { Annotator } from '../ai/api';
import { Help } from '../Help';

/**
 * The rail.
 *
 * # Three kinds of row, on purpose
 *
 * It looks like one list and is not, and pretending otherwise is how "edit the
 * rail" quietly comes to mean three different things depending on where
 * somebody clicks:
 *
 *   - **Folders** are the mailbox itself. They carry live unread counts and are
 *     wired to the folder facet rather than to a query anybody wrote. They can
 *     be hidden and they cannot be deleted — losing Inbox with no obvious way
 *     back is a bad afternoon, and the badges are half the point of these rows.
 *   - **Saved** are user content: editable, removable, reorderable. This is
 *     where the whole feature lives.
 *   - **Labels** are your labels and Gmail's, and below them the annotators'
 *     — one section, because `label:` answers for both. Derived, already
 *     filtered to what found something, so not arrangeable: a manual order
 *     would be stale the next time an annotator runs.
 *
 * # The numbers preview the click
 *
 * Given facets, every count is what clicking that row would leave you with.
 * A label adds to what is on screen, so its number is the frame with it —
 * *banking 12* on Starred is twelve starred messages. A folder swaps the
 * frame and keeps what was added, so with something added its number is that
 * inside the folder. A zero dims the row rather than hiding it, so the rail
 * does not reshuffle as the query changes, and a line over each section says
 * what the numbers are inside of — otherwise "banking 12" reads as a total.
 *
 * # Why the Other block is gone
 *
 * It was six saved queries written in TSX instead of saved — Tickets, Proposed,
 * Files, People, Systems, Unclassified. They are in the defaults pack now, and
 * they install like anything else.
 */

/** The icons a saved query may carry, by the names the store validates. */
const ICONS: Record<string, React.ComponentType<{ className?: string }>> = {
  bookmark: Bookmark, inbox: Inbox, star: Star, send: Send, file: File,
  tag: Tag, folder: Folder, clock: Clock, users: Users, paperclip: Paperclip,
  ticket: Ticket, bot: Bot, alert: AlertTriangle, search: Search,
};

const FOLDER_ICONS: Record<string, React.ComponentType<{ className?: string }>> = {
  inbox: Inbox, starred: Star, sent: Send, archive: Archive, drafts: File,
  allmail: Mails, spam: AlertOctagon, trash: Trash2,
};

export interface FolderRow {
  id: string;
  label: string;
  badge?: number;
}

interface RailProps {
  folders: FolderRow[];
  hiddenFolders: string[];
  saved: SavedQuery[];
  annotated: Annotator[];
  /** The mail labels: yours and Gmail's. */
  labels?: LabelCount[];
  labelQuery?: (name: string) => string;
  queryText: string;
  /**
   * The folder the query is inside: one is always selected. Absent, a folder
   * row is active when the query is exactly its term, as it used to be.
   */
  activeFolder?: string;
  /** Whether a label, annotation or saved row's query is part of the query now. */
  isActive?: (q: string) => boolean;
  /**
   * The counts inside what is on screen, and what that is called. Absent for
   * a query not about mail: the rail then shows its plain totals.
   */
  facets?: (Facets & { within: string; rest: string }) | null;
  /** A new query's counts are on their way; the old ones show dimmed. */
  facetsStale?: boolean;
  /** All Items: one search across every kind, above the folders. */
  onAllItems?: () => void;
  /** A label or an annotator's row: added to the folder, or taken off it. */
  onLabel?: (q: string) => void;
  onFolder: (id: string) => void;
  onQuery: (q: string) => void;
  annotationQuery: (a: Annotator) => string;
  /** Arranging. Absent while the rail is read-only. */
  onMove?: (name: string, to: number) => void;
  onRemove?: (name: string) => void;
  onEdit?: (q: SavedQuery) => void;
  onToggleFolder?: (id: string, hidden: boolean) => void;
  onResetFolders?: () => void;
  onAddDefaults?: () => void;
}

export const Rail = ({
  folders, hiddenFolders, saved, annotated, labels = [], labelQuery = n => `label:${n}`, queryText, facets, facetsStale,
  activeFolder, isActive: isActiveProp, onAllItems, onLabel, onFolder, onQuery, annotationQuery,
  onMove, onRemove, onEdit, onToggleFolder, onResetFolders, onAddDefaults,
}: RailProps) => {
  // Arranging is a mode rather than always-on controls. A rail with a delete
  // button beside every row is a rail somebody deletes something from by
  // accident, and the common act here is clicking a row, not editing one.
  const [arranging, setArranging] = useState(false);
  const hidden = new Set(hiddenFolders);
  const shown = arranging ? folders : folders.filter(f => !hidden.has(f.id));

  const isActive = isActiveProp ?? ((q: string) => queryText.trim() === q.trim());
  const folderActive = (id: string) =>
    activeFolder !== undefined ? activeFolder === id : queryText.trim() === `folder:${id}`;

  // What the numbers are inside of, said once per section.
  const within = facets?.within ?? '';
  const find = (rows: FacetCount[] | undefined, name: string) => rows?.find(r => r.name === name);
  const scopedFolders = facets?.folders && facets.folders.length > 0 ? facets.folders : null;
  const countClass = `text-[10px] font-bold tabular-nums transition-opacity ${facetsStale ? 'opacity-40' : ''}`;

  const rowClass = (active: boolean, extra = '') =>
    `w-full flex items-center gap-4 px-4 py-2 text-sm transition-colors ${
      active ? 'bg-primary/10 text-primary font-bold' : 'hover:bg-accent text-foreground/70'
    } ${extra}`;

  return (
    <div className="w-64 flex flex-col pt-4 border-r border-border/50 bg-card/20">
      <div className="px-4 mb-6">
        <button onClick={openCompose} className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 shadow-sm hover:shadow-md transition-all font-semibold text-[13px]">
          <Plus className="w-4 h-4" /> Compose
        </button>
      </div>

      <div className="flex-1 overflow-auto px-2 space-y-0.5">
        {onAllItems && (
          // Not a folder of mail: one search across mail, files, cards, people
          // and events. Always shown, because it is how you look for anything.
          <button onClick={onAllItems} title="Search everything: mail, files, cards, people and events"
            className={rowClass(activeFolder === 'allitems')}>
            <Search className={`w-4 h-4 ${activeFolder === 'allitems' ? 'text-primary' : 'text-muted-foreground'}`} />
            <span className="flex-1 text-left">All Items</span>
          </button>
        )}
        {scopedFolders && (
          <Scoped text={`With ${facets!.rest}`} title={`Each folder counts ${facets!.rest} inside it: what clicking it would show.`} />
        )}
        {shown.map(item => {
          // Every rail entry is a query. The built-ins are folder:inbox and
          // friends; a saved query below is the same kind of thing.
          const entryQuery = `folder:${item.id}`;
          const isHidden = hidden.has(item.id);
          const Icon = FOLDER_ICONS[item.id] ?? Folder;
          // With something added, the folder's number is that inside it.
          // Drafts are not counted that way and keep their own.
          const scoped = find(scopedFolders ?? undefined, item.id);
          const badge = scoped ? scoped.total : item.badge;
          const zero = scoped !== undefined && scoped.total === 0;
          return (
            <div key={item.id} className="flex items-center">
              <button
                onClick={() => onFolder(item.id)}
                title={scoped ? `${scoped.total.toLocaleString()} with ${facets!.rest} in ${item.label}` : entryQuery}
                className={rowClass(folderActive(item.id), isHidden || zero ? 'opacity-40' : '')}
              >
                <Icon className={`w-4 h-4 ${folderActive(item.id) ? 'text-primary' : 'text-muted-foreground'}`} />
                <span className="flex-1 text-left">{item.label}</span>
                {badge ? <span className={scoped ? countClass : 'text-[10px] font-bold tabular-nums'}>{badge.toLocaleString()}</span> : null}
              </button>
              {arranging && onToggleFolder && (
                // Hidden, never deleted. These are the mailbox; the question is
                // whether you want to look at it, not whether it exists.
                <button
                  onClick={() => onToggleFolder(item.id, !isHidden)}
                  title={isHidden ? `Show ${item.label}` : `Hide ${item.label}`}
                  aria-label={isHidden ? `Show ${item.label}` : `Hide ${item.label}`}
                  className="p-1 text-muted-foreground hover:text-foreground shrink-0"
                >
                  {isHidden ? <Check className="w-3 h-3" /> : <X className="w-3 h-3" />}
                </button>
              )}
            </div>
          );
        })}

        <Heading
          label="Saved"
          help="rail"
          action={
            <button
              onClick={() => setArranging(a => !a)}
              aria-pressed={arranging}
              title={arranging ? 'Done arranging' : 'Arrange the rail'}
              className={`p-1 ${arranging ? 'text-primary' : 'text-muted-foreground hover:text-foreground'}`}
            >
              <Settings2 className="w-3 h-3" />
            </button>
          }
        />

        {saved.length === 0 && (
          <p className="px-4 py-2 text-[11px] text-muted-foreground">
            Nothing saved yet.
            {onAddDefaults && (
              <> <button onClick={onAddDefaults} className="underline hover:text-foreground">Add some</button>.</>
            )}
          </p>
        )}

        {saved.map((q, i) => {
          const Icon = ICONS[q.icon ?? ''] ?? Bookmark;
          return (
            <div key={q.name ?? q.title} className="flex items-center">
              <button
                // A saved query runs inside the folder, unless it names its own
                // folder or kind — the Desk decides which.
                onClick={() => onQuery(q.query)}
                title={q.query}
                className={rowClass(isActive(q.query))}
              >
                <Icon className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
                <span className="flex-1 text-left truncate">{q.title}</span>
              </button>
              {arranging && (
                <div className="flex items-center shrink-0">
                  {/* Explicit move rather than drag: it works from a keyboard,
                      it is what the CLI does, and drag is a dependency. */}
                  <button onClick={() => q.name && onMove?.(q.name, i - 1)} disabled={i === 0}
                    aria-label={`Move ${q.title} up`} title="Move up"
                    className="p-0.5 text-muted-foreground hover:text-foreground disabled:opacity-20">
                    <ChevronUp className="w-3 h-3" />
                  </button>
                  <button onClick={() => q.name && onMove?.(q.name, i + 1)} disabled={i === saved.length - 1}
                    aria-label={`Move ${q.title} down`} title="Move down"
                    className="p-0.5 text-muted-foreground hover:text-foreground disabled:opacity-20">
                    <ChevronDown className="w-3 h-3" />
                  </button>
                  <button onClick={() => onEdit?.(q)}
                    aria-label={`Edit ${q.title}`} title="Edit"
                    className="p-0.5 text-muted-foreground hover:text-foreground">
                    <Pencil className="w-3 h-3" />
                  </button>
                  <button onClick={() => q.name && onRemove?.(q.name)}
                    aria-label={`Remove ${q.title}`} title="Remove"
                    className="p-0.5 text-muted-foreground hover:text-destructive">
                    <Trash2 className="w-3 h-3" />
                  </button>
                </div>
              )}
            </div>
          );
        })}

        {arranging && (
          <div className="px-4 pt-2 pb-1 flex flex-col gap-1 items-start">
            {onAddDefaults && (
              <button onClick={onAddDefaults}
                className="text-[11px] text-muted-foreground hover:text-foreground underline">
                add from defaults
              </button>
            )}
            {onResetFolders && hidden.size > 0 && (
              // The way back. Every hiding feature needs one that does not
              // require remembering what was hidden.
              <button onClick={onResetFolders}
                className="text-[11px] text-muted-foreground hover:text-foreground underline">
                show all folders
              </button>
            )}
          </div>
        )}

        {/* What the annotators found.
            A label answers yes or no, so its mail is `label:x`; an extractor
            pulls records out, so its mail is `extract:x` — "this one produced
            something here". Two different questions, which is why the entry is
            not one spelling for both.

            Not arrangeable: this section earns its place by being derived, and
            a manual order would be stale the next time an annotator runs. */}
        {(annotated.length > 0 || labels.length > 0) && (
          <>
            <Heading label="Labels" />
            {facets && <Scoped text={`In ${within}`} title={`Each label counts the ${facets.total.toLocaleString()} in ${within} that carry it: what clicking it would show.`} />}
            {labels.map(l => {
              const q = labelQuery(l.name);
              const c = facets ? find(facets.labels, l.name) ?? { name: l.name, total: 0, unread: 0 } : null;
              const unread = c ? c.unread : l.unread;
              return (
                <button
                  key={`mail:${l.name}`}
                  onClick={() => (onLabel ?? onQuery)(q)}
                  title={c ? `${c.total.toLocaleString()} of ${facets!.total.toLocaleString()} in ${within}` : q}
                  aria-pressed={isActive(q)}
                  className={rowClass(isActive(q), c && c.total === 0 ? 'opacity-40' : '')}
                >
                  <Tag className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
                  <span className={`flex-1 text-left truncate ${unread > 0 ? 'font-semibold' : ''}`}>
                    {l.name === 'important' ? 'Important' : l.name}
                  </span>
                  {c ? (
                    c.total > 0 && <span className={countClass}>{c.total.toLocaleString()}</span>
                  ) : l.unread > 0 && (
                    <span className="text-[10px] font-bold tabular-nums">{l.unread.toLocaleString()}</span>
                  )}
                </button>
              );
            })}
            {annotated.map(a => {
              const q = annotationQuery(a);
              const off = a.enabled === false;
              const c = facets ? find(facets.annotators, a.name) ?? { name: a.name, total: 0, unread: 0 } : null;
              return (
                <button
                  key={a.id}
                  onClick={() => (onLabel ?? onQuery)(q)}
                  title={[
                    c ? `${c.total.toLocaleString()} of ${facets!.total.toLocaleString()} in ${within}` : q,
                    off ? `${a.name} is switched off; these are what it already found` : '',
                  ].filter(Boolean).join(' — ')}
                  aria-pressed={isActive(q)}
                  className={rowClass(isActive(q), off || (c && c.total === 0) ? 'opacity-40' : '')}
                >
                  {/* Sparkles marks a label a model or rule put on, beside the
                      ones a person or the server did. */}
                  <Sparkles className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
                  <span className="flex-1 text-left truncate">{a.name}</span>
                  {/* Switched off still appears, because its results are still
                      there and still queryable. Hiding it would contradict what
                      the switch means. */}
                  {off && <span className="text-[9px] font-mono text-muted-foreground">off</span>}
                  <span className={c ? `${countClass} text-muted-foreground` : 'text-[10px] font-bold tabular-nums text-muted-foreground'}>
                    {(c ? c.total : a.progress!.matched).toLocaleString()}
                  </span>
                </button>
              );
            })}
          </>
        )}
      </div>
    </div>
  );
};

const Heading = ({ label, action, help }: { label: string; action?: React.ReactNode; help?: string }) => (
  <div className="flex items-center gap-1 px-4 pt-4 pb-1">
    <span className="flex-1 text-[10px] font-bold uppercase tracking-wider text-muted-foreground">
      {label}
    </span>
    {action}
    {help && <Help topic={help} />}
  </div>
);

/** The line that says what a section's numbers are counted inside. */
const Scoped = ({ text, title }: { text: string; title: string }) => (
  <p className="px-4 pb-1 text-[10px] text-muted-foreground truncate" title={title}>{text}</p>
);

// Kept so the unused-import check does not hide a row that still wants one.
export const RAIL_SPARE_ICONS = { Layout, HelpCircle };
