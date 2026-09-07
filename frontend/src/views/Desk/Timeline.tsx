import { useState } from 'react';
import { CheckSquare, ChevronDown, ChevronRight, Mail, PenLine, Tag } from 'lucide-react';
import { openMessage, previewMessage } from '../../lib/tabs';
import { navRow } from '../../lib/rovingFocus';
import type { Thread, ThreadEntry } from './api';

interface ThreadResultProps {
  threads: Thread[];
  /** Narrow the query to one conversation. */
  onDrillDown: (term: string, stage?: string) => void;
}

/**
 * Conversations, each with everything it caused.
 *
 * # Why this is one list and not four panes
 *
 * A ticket shown next to the mail that raised it is a decision with
 * provenance; the same ticket in a separate panel is a task in a worse task
 * manager. Interleaving by time is what makes the connection readable — you
 * see the invoice arrive, the ticket go up, the reply go out.
 *
 * # Why the merge is not done here
 *
 * The entries arrive already ordered and already typed. Deciding what belongs
 * on a timeline and in what order is a question about meaning, and every time
 * this project has answered one of those in TypeScript it has answered it
 * differently in three places. This file renders a list.
 */
export const ThreadResult = ({ threads, onDrillDown }: ThreadResultProps) => {
  if (threads.length === 0) return null;

  return (
    <div className="overflow-auto h-full divide-y divide-border">
      {threads.map(thread => (
        <ThreadRow
          key={thread.key}
          thread={thread}
          /* One conversation on screen is already the thing you asked to
             read, so it opens expanded. A page of them is a list, and a list
             of fully expanded timelines is unscannable. */
          defaultOpen={threads.length === 1}
          onDrillDown={onDrillDown}
        />
      ))}
    </div>
  );
};

const ThreadRow = ({
  thread,
  defaultOpen,
  onDrillDown,
}: {
  thread: Thread;
  defaultOpen: boolean;
  onDrillDown: (term: string, stage?: string) => void;
}) => {
  const [open, setOpen] = useState(defaultOpen);
  const Chevron = open ? ChevronDown : ChevronRight;

  return (
    <div>
      <button
        type="button"
        {...navRow}
        onClick={() => setOpen(o => !o)}
        aria-expanded={open}
        className="w-full flex items-start gap-2 px-3 py-2 text-left hover:bg-accent/40
                   focus:outline-none focus:bg-primary/10"
      >
        <Chevron size={14} className="mt-0.5 shrink-0 text-muted-foreground" />

        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">
            {thread.subject || <span className="text-muted-foreground">(no subject)</span>}
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {summarise(thread)}
          </div>
        </div>

        {/* Badges say what a collapsed conversation holds, so the thing worth
            opening is visible without opening it. */}
        <div className="flex shrink-0 items-center gap-1.5 pt-0.5">
          {thread.ticketCount > 0 && (
            <Badge icon={<CheckSquare size={11} />} n={thread.ticketCount} tone="primary" />
          )}
          {thread.draftCount > 0 && (
            <Badge icon={<PenLine size={11} />} n={thread.draftCount} tone="muted" />
          )}
          <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
            {new Date(thread.end).toLocaleDateString()}
          </span>
        </div>
      </button>

      {open && (
        // The rail and the indent live on this wrapper rather than on the
        // <ol>. The app ships an unlayered `ol, ul, menu { margin: 0 }` reset,
        // and unlayered CSS beats anything inside an @layer — so every Tailwind
        // spacing utility on a list element in this codebase silently does
        // nothing. Worth remembering: the class is in the HTML and in the
        // stylesheet, and still has no effect.
        <div className="relative ml-[26px] border-l border-border pb-2 pl-4 pr-3">
          <ol>
            {thread.entries.map((entry, i) => (
              <Entry key={`${entry.kind}-${entry.at}-${i}`} entry={entry} />
            ))}
          </ol>

          <button
            type="button"
            {...navRow}
            onClick={() => onDrillDown(`thread:${thread.key}`)}
            className="pt-1 text-xs text-muted-foreground underline-offset-2 hover:text-foreground
                       hover:underline focus:outline-none focus:text-foreground focus:underline"
          >
            Open this conversation
          </button>
        </div>
      )}
    </div>
  );
};

/**
 * One moment.
 *
 * `summary` is the server's rendering and is what the row says. The payload is
 * used only for what a list cannot express — opening the message a row stands
 * for, or showing an extraction's fields.
 */
const Entry = ({ entry }: { entry: ThreadEntry }) => {
  const clickable = entry.kind === 'message' && entry.message;

  return (
    <li className="relative py-1">
      {/* The marker sits on the rail, so the eye reads one column of events
          rather than four kinds of row. */}
      <span
        aria-hidden="true"
        className="absolute -left-[21px] top-1.5 flex h-3.5 w-3.5 items-center justify-center
                   rounded-full bg-background text-muted-foreground"
      >
        {markerFor(entry.kind)}
      </span>

      {/* The row is this div rather than the <li>, because the row has to be
          the element carrying the click: the hook synthesises Enter by
          clicking the focused element, and an event dispatched on the <li>
          would never reach a handler on its child. */}
      <div
        {...navRow}
        data-message-id={clickable ? entry.message!.id : undefined}
        onFocus={clickable ? () => previewMessage(entry.message!) : undefined}
        onClick={clickable ? () => openMessage(entry.message!) : undefined}
        className={`flex items-baseline gap-2 text-sm focus:outline-none focus:bg-primary/10 ${
          clickable ? 'cursor-pointer hover:text-primary' : ''
        }`}
      >
        <time
          dateTime={entry.at}
          className="w-16 shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground"
        >
          {new Date(entry.at).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}
        </time>

        <span className="min-w-0 flex-1 truncate">
          {entry.kind === 'message' && entry.actor && (
            <span className="mr-2 text-muted-foreground">{entry.actor}</span>
          )}
          {entry.summary || <span className="text-muted-foreground">(no subject)</span>}
          {entry.kind === 'annotation' && entry.annotation && (
            <span className="ml-2 font-mono text-[11px] text-muted-foreground">
              {compactJson(entry.annotation.dataJson)}
            </span>
          )}
        </span>
      </div>
    </li>
  );
};

function markerFor(kind: ThreadEntry['kind']) {
  switch (kind) {
    case 'message':
      return <Mail size={11} />;
    case 'draft':
      return <PenLine size={11} />;
    case 'annotation':
      return <Tag size={11} />;
    default:
      return <CheckSquare size={11} />;
  }
}

const Badge = ({ icon, n, tone }: { icon: React.ReactNode; n: number; tone: 'primary' | 'muted' }) => (
  <span
    className={`flex items-center gap-0.5 px-1 py-0.5 text-[11px] tabular-nums ${
      tone === 'primary' ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'
    }`}
  >
    {icon}
    {n}
  </span>
);

/** The line under a conversation's subject: what it holds, and who is in it. */
function summarise(thread: Thread): string {
  const parts = [plural(thread.messageCount, 'message')];
  if (thread.ticketCount > 0) parts.push(plural(thread.ticketCount, 'ticket'));
  if (thread.draftCount > 0) parts.push(plural(thread.draftCount, 'draft'));
  if (thread.participants?.length) parts.push(thread.participants.join(', '));
  return parts.join(' · ');
}

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? '' : 's'}`;
}

/**
 * An extraction's fields on one line.
 *
 * Values only: the keys are the annotator's schema, which is the same on every
 * row, and repeating them turns a timeline into a wall of JSON.
 */
function compactJson(raw: string): string {
  try {
    const data = JSON.parse(raw);
    if (!data || typeof data !== 'object') return '';
    return Object.values(data)
      .filter(v => v !== null && v !== '' && typeof v !== 'object')
      .join(' · ');
  } catch {
    return '';
  }
}
