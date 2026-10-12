import { useEffect, useState } from 'react';
import { Check, Copy, ExternalLink, Loader2, Mail, MapPin, Pencil, Repeat, Trash2, Users, Video, X } from 'lucide-react';
import { approveChange, getEvent, rejectChange, respond, type EventDetail, type Occurrence } from './api';
import { fmtRange } from './dates';
import { openContact, openMessageByID } from '../../lib/tabs';
import { Avatar } from '../Avatar';

/**
 * An event's card: when and where, who, the mail it came from, how you have
 * answered, and anything waiting on it.
 */

const STATUS_WORD: Record<string, string> = {
  accepted: 'Going', tentative: 'Maybe', declined: 'Not going', 'needs-action': 'Not answered', organizer: 'Organizer',
};

export const EventPanel = ({ occurrence, onClose, onEdit, onDelete, onChanged }: {
  occurrence: Occurrence;
  onClose: () => void;
  onEdit: (d: EventDetail) => void;
  onDelete: (d: EventDetail, scope: string) => void;
  onChanged: () => void;
}) => {
  const [detail, setDetail] = useState<EventDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const load = () => {
    getEvent(occurrence.eventId).then(setDetail).catch(e => setError(e.message));
  };
  useEffect(load, [occurrence.eventId]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!detail) {
    return (
      <aside className="w-80 shrink-0 border-l border-border bg-card p-4 text-sm">
        {error ? <p className="text-destructive">{error}</p> : <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
      </aside>
    );
  }
  const ev = detail.event;
  const start = new Date(occurrence.start);
  const end = new Date(occurrence.end);
  const recurring = (ev.recurrence?.length ?? 0) > 0 || !!ev.recurrenceId;
  const invitation = !!ev.sourceMessageId || (ev.attendees.length > 0 && ev.myStatus && ev.myStatus !== 'organizer');
  const proposals = (detail.changes ?? []).filter(c => c.state === 'proposed');
  const problems = (detail.changes ?? []).filter(c => c.state === 'conflict' || c.state === 'failed');

  const answer = async (status: string) => {
    setBusy(true); setError(null); setNote(null);
    try {
      const r = await respond(ev.id, status);
      setNote(r.note ?? (r.state === 'queued' ? 'Sent to the calendar on the next sync.' : null));
      load(); onChanged();
    } catch (e: any) { setError(e.message); } finally { setBusy(false); }
  };

  const decide = async (id: string, ok: boolean) => {
    setBusy(true); setError(null);
    try {
      if (ok) await approveChange(id); else await rejectChange(id);
      onChanged();
      if (!ok && ev.proposed) { onClose(); return; }
      load();
    } catch (e: any) { setError(e.message); } finally { setBusy(false); }
  };

  return (
    <aside className="w-80 shrink-0 overflow-auto border-l border-border bg-card p-4 text-sm" aria-label="Event">
      <div className="mb-2 flex items-start gap-2">
        <span className="mt-1.5 h-3 w-3 shrink-0" style={{ background: occurrence.color || detail.calendar?.color || '#2563eb' }} />
        <h2 className={`flex-1 text-lg font-semibold leading-snug ${ev.myStatus === 'declined' ? 'line-through opacity-60' : ''}`}>{ev.summary || '(no title)'}</h2>
        <button onClick={onClose} aria-label="Close" className="p-1 text-muted-foreground hover:text-foreground"><X className="h-4 w-4" /></button>
      </div>

      {ev.proposed && (
        <p className="mb-2 border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-xs">
          Proposed by an agent. Nothing has happened yet.
        </p>
      )}
      {ev.status === 'cancelled' && <p className="mb-2 text-xs font-semibold text-destructive">Cancelled</p>}

      <p className="text-muted-foreground">{fmtRange(start, end, occurrence.allDay)}</p>
      {ev.repeats && <p className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground"><Repeat className="h-3 w-3" />{ev.repeats}</p>}
      {ev.tz && ev.tz !== Intl.DateTimeFormat().resolvedOptions().timeZone && !occurrence.allDay && (
        <p className="mt-1 text-xs text-muted-foreground">Set in {ev.tz}</p>
      )}
      <p className="mt-1 text-xs text-muted-foreground">{detail.calendar?.name}</p>

      {ev.conferenceUrl && (
        <a href={ev.conferenceUrl} target="_blank" rel="noreferrer"
          className="mt-3 inline-flex items-center gap-1.5 bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground hover:opacity-90">
          <Video className="h-3.5 w-3.5" /> Join
        </a>
      )}
      {ev.location && <p className="mt-3 flex items-start gap-1.5"><MapPin className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />{ev.location}</p>}

      {invitation && ev.status !== 'cancelled' && !ev.proposed && (
        <div className="mt-4">
          <p className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            Your answer: {STATUS_WORD[ev.myStatus || 'needs-action'] ?? ev.myStatus}
          </p>
          <div className="flex gap-1.5">
            {(['accepted', 'tentative', 'declined'] as const).map(s => (
              <button key={s} disabled={busy} onClick={() => answer(s)}
                className={`flex-1 border px-2 py-1 text-xs ${ev.myStatus === s ? 'border-primary bg-primary/10 font-semibold' : 'border-border hover:bg-accent'}`}>
                {s === 'accepted' ? 'Yes' : s === 'tentative' ? 'Maybe' : 'No'}
              </button>
            ))}
          </div>
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

      {ev.attendees.length > 0 && (
        <div className="mt-4">
          <p className="mb-1 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            <Users className="h-3 w-3" /> {ev.attendees.length} {ev.attendees.length === 1 ? 'person' : 'people'}
          </p>
          <ul className="space-y-1">
            {ev.organizer && (
              <li className="flex items-center gap-2">
                <Avatar address={ev.organizer} size={20} />
                <button onClick={() => openContact(ev.organizer!)} className="flex-1 truncate text-left hover:underline">{ev.organizer}</button>
                <span className="text-[10px] text-muted-foreground">organizer</span>
              </li>
            )}
            {ev.attendees.filter(a => a.address !== ev.organizer).map(a => (
              <li key={a.address} className="flex items-center gap-2">
                <Avatar address={a.address} name={a.name} size={20} />
                <button onClick={() => openContact(a.address)} className="flex-1 truncate text-left hover:underline" title={a.address}>{a.name || a.address}</button>
                <span className={`text-[10px] ${a.status === 'accepted' ? 'text-emerald-600' : a.status === 'declined' ? 'text-destructive' : 'text-muted-foreground'}`}>
                  {a.status === 'accepted' ? <Check className="inline h-3 w-3" /> : STATUS_WORD[a.status] ?? a.status}{a.role === 'optional' ? ' · optional' : ''}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {ev.description && <p className="mt-4 whitespace-pre-wrap break-words text-xs">{ev.description}</p>}

      {detail.message && (
        <button onClick={() => openMessageByID(detail.message!.id)}
          className="mt-4 flex w-full items-center gap-2 border border-border px-2 py-1.5 text-left text-xs hover:bg-accent">
          <Mail className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="flex-1 truncate">{detail.message.subject}</span>
          <ExternalLink className="h-3 w-3 text-muted-foreground" />
        </button>
      )}

      {proposals.length > 0 && (
        <div className="mt-4 space-y-2">
          {proposals.map(c => (
            <div key={c.id} className="border border-amber-500/40 bg-amber-500/5 p-2 text-xs">
              <p className="mb-1.5">{c.payload.summary ?? c.op} <span className="text-muted-foreground">— proposed by {c.origin}</span></p>
              <div className="flex gap-1.5">
                <button disabled={busy} onClick={() => decide(c.id, true)} className="bg-primary px-2 py-0.5 font-semibold text-primary-foreground">Approve</button>
                <button disabled={busy} onClick={() => decide(c.id, false)} className="border border-border px-2 py-0.5 hover:bg-accent">Reject</button>
              </div>
            </div>
          ))}
        </div>
      )}
      {problems.map(c => (
        <p key={c.id} className="mt-2 text-xs text-destructive">
          {c.state === 'conflict' ? 'Changed elsewhere before your change arrived; the calendar shows their version. ' : 'Could not be sent: '}
          {c.error}
        </p>
      ))}

      {error && <p className="mt-3 text-xs text-destructive">{error}</p>}

      {detail.writable && !ev.proposed && (
        <div className="mt-5 flex gap-2 border-t border-border pt-3">
          <button onClick={() => onEdit(detail)} className="inline-flex items-center gap-1.5 border border-border px-3 py-1 text-xs hover:bg-accent">
            <Pencil className="h-3 w-3" /> Edit
          </button>
          {!confirmDelete ? (
            <button onClick={() => setConfirmDelete(true)} className="inline-flex items-center gap-1.5 border border-border px-3 py-1 text-xs hover:bg-accent">
              <Trash2 className="h-3 w-3" /> Delete
            </button>
          ) : (
            <div className="flex flex-wrap gap-1.5 text-xs">
              {recurring && <button onClick={() => onDelete(detail, 'this')} className="border border-destructive/50 px-2 py-1 text-destructive hover:bg-destructive/10">Only this one</button>}
              <button onClick={() => onDelete(detail, 'all')} className="border border-destructive/50 px-2 py-1 text-destructive hover:bg-destructive/10">{recurring ? 'All of them' : 'Delete it'}</button>
              <button onClick={() => setConfirmDelete(false)} className="px-2 py-1 text-muted-foreground">Keep</button>
            </div>
          )}
        </div>
      )}
      {!detail.writable && detail.calendar?.provider === 'mail' && (
        <p className="mt-5 border-t border-border pt-3 text-[11px] text-muted-foreground">
          This came by mail. Connect the calendar it lives in to edit it here; answering it works now.
        </p>
      )}
    </aside>
  );
};
