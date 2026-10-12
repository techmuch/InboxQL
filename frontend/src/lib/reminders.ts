import { create } from 'zustand';

export interface Reminder {
  eventId: string;
  summary: string;
  start: string;
  allDay: boolean;
  location?: string;
  conferenceUrl?: string;
  minutes: number;
}

/**
 * Reminders the server sent to this window, shown until dismissed.
 *
 * The server decides when an alarm is due — it is the one process that is
 * running whether or not a tab is open — and sends it to every open window.
 */
interface ReminderState {
  items: Reminder[];
  push: (r: Reminder) => void;
  dismiss: (eventId: string) => void;
}

export const useReminders = create<ReminderState>((set) => ({
  items: [],
  push: (r) => set(s => ({ items: [...s.items.filter(x => x.eventId !== r.eventId), r] })),
  dismiss: (eventId) => set(s => ({ items: s.items.filter(x => x.eventId !== eventId) })),
}));

export function reminderText(r: Reminder): string {
  if (r.allDay) return 'Today';
  if (r.minutes <= 0) return 'Now';
  const at = new Date(r.start).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
  return r.minutes >= 60 * 24 ? at : `In ${r.minutes} min · ${at}`;
}

/** Show a reminder, and a system notification if the person has allowed them. */
export function deliverReminder(raw: string) {
  let r: Reminder;
  try { r = JSON.parse(raw); } catch { return; }
  useReminders.getState().push(r);
  try {
    if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
      new Notification(r.summary, { body: `${reminderText(r)}${r.location ? ` · ${r.location}` : ''}`, tag: r.eventId });
    }
  } catch { /* notifications unavailable here */ }
}
