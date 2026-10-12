export interface Occurrence {
  eventId: string;
  calendarId: string;
  uid: string;
  start: string;
  end: string;
  allDay: boolean;
  summary: string;
  location?: string;
  conferenceUrl?: string;
  status: string;
  showAs: string;
  myStatus?: string;
  recurring: boolean;
  proposed: boolean;
  color?: string;
  attendees: number;
  charge?: string;
}

export interface CalendarInfo {
  id: string;
  accountId?: string;
  provider: string;
  name: string;
  color?: string;
  access: string;
  hidden: boolean;
  primary: boolean;
  writable: boolean;
  events: number;
  syncedAt?: string;
}

export interface CalendarAccount {
  id: string;
  provider: string;
  name: string;
  email?: string;
  status: string;
  lastError?: string;
  syncedAt?: string;
  config: Record<string, string>;
}

export interface Attendee {
  address: string;
  name?: string;
  role: string;
  status: string;
}

export interface StoredEvent {
  id: string;
  calendarId: string;
  uid: string;
  recurrenceId?: string;
  summary: string;
  description?: string;
  location?: string;
  conferenceUrl?: string;
  start: string;
  end: string;
  allDay: boolean;
  tz?: string;
  recurrence?: string[];
  repeats?: string;
  status: string;
  showAs: string;
  private: boolean;
  organizer?: string;
  myStatus?: string;
  sequence: number;
  alarms: number[];
  attendees: Attendee[];
  sourceMessageId?: string;
  threadKey?: string;
  origin: string;
  proposed: boolean;
}

export interface Change {
  id: string;
  eventId?: string;
  calendarId: string;
  op: string;
  payload: { summary?: string; scope?: string; status?: string };
  state: string;
  origin: string;
  error?: string;
  createdAt: string;
}

export interface EventDetail {
  event: StoredEvent;
  calendar?: CalendarInfo;
  writable?: boolean;
  message?: { id: string; subject: string; from: string; date: string };
  changes?: Change[];
  me?: string[];
}

export interface EventInput {
  summary: string;
  description?: string;
  location?: string;
  conferenceUrl?: string;
  start: string;
  end: string;
  allDay: boolean;
  tz?: string;
  recurrence?: string[];
  showAs?: string;
  private?: boolean;
  attendees?: Attendee[];
  alarms?: number[];
}

export interface ChangeResult {
  eventId?: string;
  changeId?: string;
  state: 'applied' | 'queued' | 'proposed';
  draftId?: string;
  note?: string;
}

async function call<T>(method: string, url: string, body?: unknown): Promise<T> {
  const r = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: 'no-store',
  });
  const text = await r.text();
  if (!r.ok) {
    let msg = text.trim();
    try { msg = JSON.parse(text).error ?? msg; } catch { /* plain text */ }
    throw new Error(msg || `${r.status}`);
  }
  return text ? JSON.parse(text) : ({} as T);
}

export const listCalendars = () =>
  call<{ calendars: CalendarInfo[]; accounts: CalendarAccount[]; attention: number }>('GET', '/api/calendar/calendars');
export const patchCalendar = (id: string, body: { hidden?: boolean; color?: string }) =>
  call<CalendarInfo>('PATCH', `/api/calendar/calendars/${encodeURIComponent(id)}`, body);
export const listOccurrences = (from: Date, to: Date) =>
  call<{ occurrences: Occurrence[] }>('GET',
    `/api/calendar/occurrences?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`);
export const getEvent = (id: string) => call<EventDetail>('GET', `/api/calendar/events/${encodeURIComponent(id)}`);
export const createEvent = (calendarId: string, event: EventInput) =>
  call<ChangeResult>('POST', '/api/calendar/events', { calendarId, event });
export const updateEvent = (id: string, event: EventInput, scope: string, occurrence?: string) =>
  call<ChangeResult>('PUT', `/api/calendar/events/${encodeURIComponent(id)}`, { event, scope, occurrence: occurrence ?? '' });
export const deleteEvent = (id: string, scope: string, occurrence?: string) =>
  call<ChangeResult>('DELETE', `/api/calendar/events/${encodeURIComponent(id)}?scope=${scope}&occurrence=${encodeURIComponent(occurrence ?? '')}`);
export const respond = (id: string, status: string, comment = '') =>
  call<ChangeResult>('POST', `/api/calendar/events/${encodeURIComponent(id)}/respond`, { status, comment });
export const listChanges = () => call<{ changes: Change[] }>('GET', '/api/calendar/changes');
export const approveChange = (id: string) => call<ChangeResult>('POST', `/api/calendar/changes/${encodeURIComponent(id)}/approve`);
export const rejectChange = (id: string) => call<unknown>('POST', `/api/calendar/changes/${encodeURIComponent(id)}/reject`);
export const invitationFor = (messageId: string) =>
  call<{ events: { event: StoredEvent; clashes: Occurrence[] }[] }>('GET', `/api/calendar/invitation?message=${encodeURIComponent(messageId)}`);
export const syncAccount = (id: string) => call<CalendarAccount>('POST', `/api/calendar/accounts/${encodeURIComponent(id)}/sync`);
