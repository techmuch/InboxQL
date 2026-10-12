import { useEffect, useState } from 'react';
import { CalendarDays, Copy, Loader2, Video } from 'lucide-react';
import { invitationFor, respond, type Occurrence, type StoredEvent } from './Calendar/api';
import { fmtRange, fmtTime } from './Calendar/dates';
import { openCalendar } from '../lib/calendarNav';

/**
 * An invitation, answered from the message it came in.
 *
 * Shown when the message carried a calendar part InboxQL read as an event:
 * when it is, what else is on then, and Yes / Maybe / No. An answer to an
 * invitation that only exists as mail goes back as mail, through the outbox,
 * and says so.
 */
export const InvitationCard = ({ messageId }: { messageId: string }) => {
  const [items, setItems] = useState<{ event: StoredEvent; clashes: Occurrence[] }[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    invitationFor(messageId).then(r => setItems(r.events)).catch(() => setItems([]));
  };
  useEffect(load, [messageId]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!items || items.length === 0) return null;

  return (
    <div className="mb-6 space-y-2">
      {items.map(({ event: ev, clashes: maybeClashes }) => {
        const clashes = maybeClashes ?? [];
        const start = new Date(ev.start);
        const end = new Date(ev.end);
        const cancelled = ev.status === 'cancelled';
        const answer = async (status: string) => {
          setBusy(true); setError(null); setNote(null);
          try {
            const r = await respond(ev.id, status);
            setNote(r.note ?? null);
            load();
          } catch (e: any) { setError(e.message); } finally { setBusy(false); }
        };
        return (
          <div key={ev.id} className="border border-border bg-card p-4" aria-label="Invitation">
            <div className="flex items-start gap-3">
              <div className="flex h-12 w-12 shrink-0 flex-col items-center justify-center border border-border bg-background">
                <span className="text-[10px] uppercase text-destructive">{start.toLocaleDateString(undefined, { month: 'short' })}</span>
                <span className="text-lg font-semibold leading-none">{start.getDate()}</span>
              </div>
              <div className="min-w-0 flex-1">
                <p className={`font-semibold ${cancelled ? 'line-through' : ''}`}>{ev.summary}</p>
                <p className="text-sm text-muted-foreground">{fmtRange(start, end, ev.allDay)}</p>
                {ev.repeats && <p className="text-xs text-muted-foreground">{ev.repeats}</p>}
                {ev.location && <p className="text-xs text-muted-foreground">{ev.location}</p>}
                {cancelled && <p className="mt-1 text-xs font-semibold text-destructive">This event has been cancelled.</p>}
                {!cancelled && clashes.length > 0 && (
                  <p className="mt-1 text-xs text-amber-700 dark:text-amber-400">
                    Clashes with {clashes.slice(0, 2).map(c => `${c.summary} (${fmtTime(new Date(c.start))})`).join(', ')}
                    {clashes.length > 2 ? ` and ${clashes.length - 2} more` : ''}
                  </p>
                )}
                {!cancelled && clashes.length === 0 && <p className="mt-1 text-xs text-muted-foreground">Nothing else is on then.</p>}
              </div>
              <div className="flex shrink-0 flex-col gap-1.5">
                {ev.conferenceUrl && (
                  <a href={ev.conferenceUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 border border-border px-2 py-1 text-xs hover:bg-accent">
                    <Video className="h-3 w-3" /> Join
                  </a>
                )}
                <button onClick={() => openCalendar({ date: ev.start, eventId: ev.id })} className="inline-flex items-center gap-1 border border-border px-2 py-1 text-xs hover:bg-accent">
                  <CalendarDays className="h-3 w-3" /> Calendar
                </button>
              </div>
            </div>
            {!cancelled && ev.myStatus !== 'organizer' && (
              <div className="mt-3 flex flex-wrap items-center gap-2">
                <span className="text-xs text-muted-foreground">Going?</span>
                {(['accepted', 'tentative', 'declined'] as const).map(s => (
                  <button key={s} disabled={busy} onClick={() => answer(s)}
                    className={`border px-3 py-1 text-xs ${ev.myStatus === s ? 'border-primary bg-primary/10 font-semibold' : 'border-border hover:bg-accent'}`}>
                    {s === 'accepted' ? 'Yes' : s === 'tentative' ? 'Maybe' : 'No'}
                  </button>
                ))}
                {busy && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />}
              </div>
            )}
            {note && (
              <p className="mt-2 flex items-start gap-1 text-xs text-muted-foreground">
                {note}
                {note.includes('iql outbox approve') && (
                  <button title="Copy the command" className="shrink-0 p-0.5" onClick={() => navigator.clipboard?.writeText(note.slice(note.indexOf('iql outbox approve')))}>
                    <Copy className="h-3 w-3" />
                  </button>
                )}
              </p>
            )}
            {error && <p className="mt-2 text-xs text-destructive">{error}</p>}
          </div>
        );
      })}
    </div>
  );
};
