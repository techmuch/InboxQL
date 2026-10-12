import { Bell, Video, X } from 'lucide-react';
import { reminderText, useReminders } from '../lib/reminders';
import { openCalendar } from '../lib/calendarNav';

/** Reminders, bottom right, until dismissed. */
export const ReminderToasts = () => {
  const { items, dismiss } = useReminders();
  const canAsk = typeof Notification !== 'undefined' && Notification.permission === 'default';
  if (items.length === 0) return null;
  return (
    <div className="fixed bottom-10 right-4 z-[60] w-80 space-y-2" role="status" aria-live="polite">
      {items.map(r => (
        <div key={r.eventId} className="border border-border bg-card p-3 shadow-lg text-sm">
          <div className="flex items-start gap-2">
            <Bell className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
            <div className="min-w-0 flex-1">
              <p className="font-semibold truncate">{r.summary}</p>
              <p className="text-xs text-muted-foreground">{reminderText(r)}{r.location ? ` · ${r.location}` : ''}</p>
            </div>
            <button aria-label="Dismiss" onClick={() => dismiss(r.eventId)} className="p-0.5 text-muted-foreground hover:text-foreground"><X className="h-3.5 w-3.5" /></button>
          </div>
          <div className="mt-2 flex gap-2 text-xs">
            {r.conferenceUrl && (
              <a href={r.conferenceUrl} target="_blank" rel="noreferrer" onClick={() => dismiss(r.eventId)}
                className="inline-flex items-center gap-1 bg-primary px-2 py-1 font-semibold text-primary-foreground">
                <Video className="h-3 w-3" /> Join
              </a>
            )}
            <button onClick={() => { openCalendar({ date: r.start, eventId: r.eventId }); dismiss(r.eventId); }} className="border border-border px-2 py-1 hover:bg-accent">Open</button>
            {canAsk && (
              <button onClick={() => Notification.requestPermission()} className="ml-auto text-muted-foreground hover:text-foreground" title="Show reminders even when this tab is in the background">
                Allow notifications
              </button>
            )}
          </div>
        </div>
      ))}
    </div>
  );
};
