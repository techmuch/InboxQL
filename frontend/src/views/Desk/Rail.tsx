import { useState } from 'react';
import {
  AlertOctagon, AlertTriangle, Bookmark, Bot, Check, ChevronDown, ChevronUp,
  Archive, Clock, File, Folder, HelpCircle, Inbox, Layout, Paperclip, Pencil, Plus,
  Search, Send, Settings2, Star, Tag, Ticket, Trash2, Users, X,
} from 'lucide-react';
import type { SavedQuery } from './api';
import type { Annotator } from '../ai/api';

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
 *   - **Annotations** are derived, already filtered to the annotators that
 *     found something. Hand-ordering them would fight that: run a new annotator
 *     tomorrow and the manual order is stale.
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
  spam: AlertOctagon, trash: Trash2,
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
  queryText: string;
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
  folders, hiddenFolders, saved, annotated, queryText,
  onFolder, onQuery, annotationQuery,
  onMove, onRemove, onEdit, onToggleFolder, onResetFolders, onAddDefaults,
}: RailProps) => {
  // Arranging is a mode rather than always-on controls. A rail with a delete
  // button beside every row is a rail somebody deletes something from by
  // accident, and the common act here is clicking a row, not editing one.
  const [arranging, setArranging] = useState(false);
  const hidden = new Set(hiddenFolders);
  const shown = arranging ? folders : folders.filter(f => !hidden.has(f.id));

  const rowClass = (active: boolean, extra = '') =>
    `w-full flex items-center gap-4 px-4 py-2 text-sm transition-colors ${
      active ? 'bg-primary/10 text-primary font-bold' : 'hover:bg-accent text-foreground/70'
    } ${extra}`;

  return (
    <div className="w-64 flex flex-col pt-4 border-r border-border/50 bg-card/20">
      <div className="px-4 mb-6">
        <button className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 shadow-sm hover:shadow-md transition-all font-semibold text-[13px]">
          <Plus className="w-4 h-4" /> Compose
        </button>
      </div>

      <div className="flex-1 overflow-auto px-2 space-y-0.5">
        {shown.map(item => {
          // Every rail entry is a query. The built-ins are folder:inbox and
          // friends; a saved query below is the same kind of thing.
          const entryQuery = `folder:${item.id}`;
          const isHidden = hidden.has(item.id);
          const Icon = FOLDER_ICONS[item.id] ?? Folder;
          return (
            <div key={item.id} className="flex items-center">
              <button
                onClick={() => onFolder(item.id)}
                title={entryQuery}
                className={rowClass(queryText.trim() === entryQuery, isHidden ? 'opacity-40' : '')}
              >
                <Icon className={`w-4 h-4 ${queryText.trim() === entryQuery ? 'text-primary' : 'text-muted-foreground'}`} />
                <span className="flex-1 text-left">{item.label}</span>
                {item.badge ? <span className="text-[10px] font-bold tabular-nums">{item.badge}</span> : null}
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
                // A saved query is a complete expression, not a facet, so it
                // replaces rather than merges.
                onClick={() => onQuery(q.query)}
                title={q.query}
                className={rowClass(queryText.trim() === q.query.trim())}
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
        {annotated.length > 0 && (
          <>
            <Heading label="Annotations" />
            {annotated.map(a => {
              const q = annotationQuery(a);
              const off = a.enabled === false;
              return (
                <button
                  key={a.id}
                  onClick={() => onQuery(q)}
                  title={off ? `${q} — ${a.name} is switched off; these are what it already found` : q}
                  className={rowClass(queryText.trim() === q, off ? 'opacity-50' : '')}
                >
                  <Tag className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
                  <span className="flex-1 text-left truncate">{a.name}</span>
                  {/* Switched off still appears, because its results are still
                      there and still queryable. Hiding it would contradict what
                      the switch means. */}
                  {off && <span className="text-[9px] font-mono text-muted-foreground">off</span>}
                  <span className="text-[10px] font-bold tabular-nums text-muted-foreground">
                    {a.progress!.matched.toLocaleString()}
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

const Heading = ({ label, action }: { label: string; action?: React.ReactNode }) => (
  <div className="flex items-center px-4 pt-4 pb-1">
    <span className="flex-1 text-[10px] font-bold uppercase tracking-wider text-muted-foreground">
      {label}
    </span>
    {action}
  </div>
);

// Kept so the unused-import check does not hide a row that still wants one.
export const RAIL_SPARE_ICONS = { Layout, HelpCircle };
