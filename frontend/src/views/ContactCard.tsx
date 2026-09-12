import { useCallback, useEffect, useState } from 'react';
import {
  AlertCircle,
  ArrowLeft,
  Bot,
  Building2,
  Check,
  Clock,
  FileText,
  HelpCircle,
  Mail,
  MessageSquare,
  Plus,
  Tag,
  User,
  Users,
  X,
} from 'lucide-react';
import { openContact, openMessage, openQuery, useViewerStore } from '../lib/tabs';
import {
  contactName,
  getContactResponsiveness,
  modifyContactTag,
  runQuery,
  setContactNotes,
  type Contact,
  type ContactResponsiveness,
} from './Desk/api';
import { AttachmentSection } from './AttachmentPreview';

interface ContactTopic {
  topic: string;
  messages: number;
  share: number;
  lift: number;
}

function formatDurationSecs(secs?: number | null): string {
  if (secs === undefined || secs === null) return '—';
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.round(secs / 60)}m`;
  if (secs < 86400) {
    const hours = Math.floor(secs / 3600);
    const mins = Math.round((secs % 3600) / 60);
    return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
  }
  const days = Math.floor(secs / 86400);
  const hours = Math.round((secs % 86400) / 3600);
  return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
}

/**
 * One contact, and everything the mailbox knows about them.
 *
 * # Why it reads through the query language
 *
 * The contact, their topics and their correspondents are three queries —
 * `in:contacts email:=x`, `| topics`, `anyone:=x | count by anyone` — rather
 * than three endpoints. Contacts are an entity in the language, and a second
 * read path would be a second set of filtering and ordering semantics to keep
 * in step with the first.
 *
 * The one thing that is not a query is the correction, because the language
 * reads and does not write.
 */
export const ContactCard = ({ address }: { address: string }) => {
  const previousMessage = useViewerStore(s => s.previousMessage);
  const [contact, setContact] = useState<Contact | null>(null);
  const [topics, setTopics] = useState<ContactTopic[]>([]);
  const [network, setNetwork] = useState<{ label: string; value: number }[]>([]);
  const [responsiveness, setResponsiveness] = useState<ContactResponsiveness | null>(null);
  const [filter, setFilter] = useState('');
  const [loading, setLoading] = useState(true);
  const [saved, setSaved] = useState(false);

  // Tag editing state
  const [newTag, setNewTag] = useState('');
  const [isAddingTag, setIsAddingTag] = useState(false);

  // Notes editing state
  const [notesDraft, setNotesDraft] = useState('');
  const [notesSaving, setNotesSaving] = useState(false);
  const [notesSaved, setNotesSaved] = useState(false);

  const quoted = `=${address}`;

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [who, theirTopics, theirNetwork, resp] = await Promise.all([
        runQuery(`in:contacts email:${quoted}`, 1, 0),
        runQuery(`in:contacts email:${quoted} | topics 8`, 8, 0),
        runQuery(`anyone:${quoted} | count by anyone`, 1000, 0),
        getContactResponsiveness(address).catch(() => null),
      ]);
      const c = who.contacts?.[0] ?? null;
      setContact(c);
      setNotesDraft(c?.notes ?? '');
      setResponsiveness(resp);
      setTopics((theirTopics as any).topics ?? []);
      // Every participant who appeared on a message alongside this contact,
      // ranked by how many messages they shared, excluding the contact themselves.
      const selfAddr = address.toLowerCase();
      const peers = (theirNetwork.groups ?? [])
        .map(g => ({
          label: other(g.label, address),
          value: g.value,
        }))
        .filter(g => {
          const target = g.label.toLowerCase();
          return target !== selfAddr && target.length > 0;
        });
      setNetwork(peers);
    } catch {
      setContact(null);
    } finally {
      setLoading(false);
    }
  }, [address, quoted]);

  useEffect(() => {
    setFilter('');
  }, [address]);

  useEffect(() => { load(); }, [load]);

  const correct = async (patch: Partial<Contact>) => {
    await fetch('/api/contacts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ address, ...patch }),
    });
    setSaved(true);
    window.setTimeout(() => setSaved(false), 2000);
    await load();
  };

  const handleAddTag = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    const tag = newTag.trim().toLowerCase();
    if (!tag) return;
    try {
      const res = await modifyContactTag(address, tag, 'add');
      setContact(prev => prev ? { ...prev, tags: res.tags } : null);
      setNewTag('');
      setIsAddingTag(false);
    } catch (err) {
      console.error(err);
    }
  };

  const handleRemoveTag = async (tag: string) => {
    try {
      const res = await modifyContactTag(address, tag, 'remove');
      setContact(prev => prev ? { ...prev, tags: res.tags } : null);
    } catch (err) {
      console.error(err);
    }
  };

  const handleSaveNotes = async () => {
    setNotesSaving(true);
    try {
      await setContactNotes(address, notesDraft);
      setNotesSaved(true);
      setContact(prev => prev ? { ...prev, notes: notesDraft } : null);
      window.setTimeout(() => setNotesSaved(false), 2000);
    } catch (err) {
      console.error(err);
    } finally {
      setNotesSaving(false);
    }
  };

  if (loading) return <div className="p-8 text-sm text-muted-foreground">Loading…</div>;
  if (!contact) {
    return (
      <div className="p-8 text-sm text-muted-foreground space-y-4">
        {previousMessage && (
          <div>
            <button
              type="button"
              onClick={() => openMessage(previousMessage)}
              className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
            >
              <ArrowLeft className="h-3.5 w-3.5" /> Back to message
            </button>
          </div>
        )}
        <div>Nothing is known about <span className="font-mono">{address}</span> yet.</div>
      </div>
    );
  }

  return (
    <div className="h-full overflow-auto p-6">
      <div className="mx-auto max-w-3xl space-y-6">
        {previousMessage && (
          <div>
            <button
              type="button"
              onClick={() => openMessage(previousMessage)}
              className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
              title={previousMessage.subject ? `Back to "${previousMessage.subject}"` : 'Back to message'}
            >
              <ArrowLeft className="h-3.5 w-3.5" /> Back to message
            </button>
          </div>
        )}
        <header className="flex items-start gap-4">
          <KindIcon kind={contact.kind} />
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-2xl font-bold">{contactName(contact)}</h2>
            <p className="truncate font-mono text-sm text-muted-foreground">{contact.address}</p>

            {/* Custom tags pills */}
            <div className="mt-2 flex flex-wrap items-center gap-1.5">
              {(contact.tags ?? []).map(t => (
                <span
                  key={t}
                  className="inline-flex items-center gap-1 border border-border bg-accent/30 px-2 py-0.5 text-xs text-foreground group rounded-sm"
                >
                  <button
                    type="button"
                    onClick={() => openQuery(`in:contacts tag:${t}`)}
                    className="hover:underline flex items-center gap-1 cursor-pointer"
                    title={`Filter contacts by tag:${t}`}
                  >
                    <Tag className="h-2.5 w-2.5 text-muted-foreground" />
                    <span>{t}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => handleRemoveTag(t)}
                    className="text-muted-foreground hover:text-destructive cursor-pointer ml-0.5"
                    title={`Remove tag ${t}`}
                  >
                    <X className="h-3 w-3" />
                  </button>
                </span>
              ))}
              {isAddingTag ? (
                <form onSubmit={handleAddTag} className="inline-flex items-center gap-1">
                  <input
                    type="text"
                    value={newTag}
                    onChange={e => setNewTag(e.target.value)}
                    placeholder="tag name..."
                    autoFocus
                    className="h-6 w-24 bg-background border border-primary px-1.5 text-xs outline-none rounded-xs"
                  />
                  <button
                    type="submit"
                    disabled={!newTag.trim()}
                    className="h-6 px-2 bg-primary text-primary-foreground text-xs font-medium cursor-pointer disabled:opacity-50 rounded-xs"
                  >
                    Add
                  </button>
                  <button
                    type="button"
                    onClick={() => { setIsAddingTag(false); setNewTag(''); }}
                    className="h-6 px-1.5 text-xs text-muted-foreground hover:text-foreground cursor-pointer"
                  >
                    Cancel
                  </button>
                </form>
              ) : (
                <button
                  type="button"
                  onClick={() => setIsAddingTag(true)}
                  className="inline-flex items-center gap-1 border border-dashed border-border px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground hover:border-foreground/50 transition-colors cursor-pointer rounded-sm"
                >
                  <Plus className="h-3 w-3" /> Tag
                </button>
              )}
            </div>
          </div>
          <button
            type="button"
            onClick={() => openQuery(`anyone:${contact.address}`)}
            className="flex shrink-0 items-center gap-1 border border-border px-3 py-1.5 text-xs hover:bg-accent/40 cursor-pointer"
          >
            <Mail className="h-3 w-3" /> Their mail
          </button>
        </header>

        {/* Counted from the participant edges on every read, never cached —
            which is why they cannot disagree with the mailbox. */}
        <section className="grid grid-cols-4 gap-px border border-border bg-border">
          <Stat label="Messages" value={contact.messages.toLocaleString()} />
          <Stat label="Sent" value={contact.sent.toLocaleString()} />
          <Stat label="Received" value={contact.received.toLocaleString()} />
          <Stat
            label="Last seen"
            value={contact.lastSeen ? new Date(contact.lastSeen).toLocaleDateString() : '—'}
          />
        </section>

        {/* Communication Dynamics & Open Loops */}
        {responsiveness && (
          <section className="space-y-4 border border-border bg-card p-4">
            <div className="flex items-center justify-between">
              <h3 className="flex items-center gap-1.5 text-xs font-bold uppercase tracking-wider text-muted-foreground">
                <Clock className="h-3.5 w-3.5" /> Communication Dynamics
              </h3>
            </div>

            {/* Latency & Role Grid */}
            <div className="grid grid-cols-3 gap-px border border-border bg-border">
              <div className="bg-card p-3">
                <div className="text-[10px] font-bold uppercase tracking-wider text-muted-foreground">Your Turnaround</div>
                <div className="font-mono text-lg font-semibold tabular-nums mt-0.5">
                  {formatDurationSecs(responsiveness.myMedianReplySecs)}
                </div>
                <div className="text-[11px] text-muted-foreground mt-0.5">Median time to reply to them</div>
              </div>
              <div className="bg-card p-3">
                <div className="text-[10px] font-bold uppercase tracking-wider text-muted-foreground">Their Turnaround</div>
                <div className="font-mono text-lg font-semibold tabular-nums mt-0.5">
                  {formatDurationSecs(responsiveness.theirMedianReplySecs)}
                </div>
                <div className="text-[11px] text-muted-foreground mt-0.5">Median time to reply to you</div>
              </div>
              <div className="bg-card p-3">
                <div className="text-[10px] font-bold uppercase tracking-wider text-muted-foreground">Addressing Role</div>
                <div className="font-mono text-lg font-semibold tabular-nums mt-0.5">
                  {responsiveness.toCount + responsiveness.ccCount > 0
                    ? `${Math.round(responsiveness.toRatio * 100)}% Direct`
                    : '—'}
                </div>
                <div className="text-[11px] text-muted-foreground mt-0.5">
                  {responsiveness.toCount} in To · {responsiveness.ccCount} in Cc
                </div>
              </div>
            </div>

            {/* Open Loops */}
            <div className="space-y-3 pt-1">
              <div className="text-xs font-medium text-foreground">Open Loops (Pending Replies)</div>

              {responsiveness.awaitingMyReplyCount === 0 && responsiveness.awaitingTheirReplyCount === 0 && (
                <div className="border border-border/70 bg-accent/20 px-3 py-2 text-xs text-muted-foreground flex items-center gap-2">
                  <Check className="h-4 w-4 text-green-500 shrink-0" />
                  <span>All loops closed — no pending replies in either direction.</span>
                </div>
              )}

              {responsiveness.awaitingMyReplyCount > 0 && (
                <div className="border border-amber-500/30 bg-amber-500/5 p-3 space-y-2">
                  <div className="flex items-center justify-between text-xs font-semibold text-amber-600 dark:text-amber-400">
                    <span className="flex items-center gap-1.5">
                      <AlertCircle className="h-3.5 w-3.5" /> Awaiting your reply ({responsiveness.awaitingMyReplyCount})
                    </span>
                    <button
                      type="button"
                      onClick={() => openQuery(`anyone:${contact.address} is:unread`)}
                      className="text-[11px] hover:underline font-normal cursor-pointer"
                    >
                      View mail
                    </button>
                  </div>
                  <ul className="divide-y divide-border/40 text-xs space-y-1.5 pt-1">
                    {(responsiveness.awaitingMyReplyThreads ?? []).map(t => (
                      <li key={t.threadKey} className="pt-1.5 first:pt-0">
                        <div className="flex items-baseline justify-between gap-2">
                          <button
                            type="button"
                            onClick={() => openQuery(`thread:${t.threadKey}`)}
                            className="font-medium hover:text-primary text-left truncate cursor-pointer"
                          >
                            {t.subject || '(no subject)'}
                          </button>
                          <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">
                            {new Date(t.lastMessageAt).toLocaleDateString()}
                          </span>
                        </div>
                        {t.snippet && (
                          <p className="text-[11px] text-muted-foreground truncate mt-0.5">{t.snippet}</p>
                        )}
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {responsiveness.awaitingTheirReplyCount > 0 && (
                <div className="border border-blue-500/30 bg-blue-500/5 p-3 space-y-2">
                  <div className="flex items-center justify-between text-xs font-semibold text-blue-600 dark:text-blue-400">
                    <span className="flex items-center gap-1.5">
                      <MessageSquare className="h-3.5 w-3.5" /> Awaiting their reply ({responsiveness.awaitingTheirReplyCount})
                    </span>
                  </div>
                  <ul className="divide-y divide-border/40 text-xs space-y-1.5 pt-1">
                    {(responsiveness.awaitingTheirReplyThreads ?? []).map(t => (
                      <li key={t.threadKey} className="pt-1.5 first:pt-0">
                        <div className="flex items-baseline justify-between gap-2">
                          <button
                            type="button"
                            onClick={() => openQuery(`thread:${t.threadKey}`)}
                            className="font-medium hover:text-primary text-left truncate cursor-pointer"
                          >
                            {t.subject || '(no subject)'}
                          </button>
                          <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">
                            {new Date(t.lastMessageAt).toLocaleDateString()}
                          </span>
                        </div>
                        {t.snippet && (
                          <p className="text-[11px] text-muted-foreground truncate mt-0.5">{t.snippet}</p>
                        )}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>

            {/* 24h Activity Cadence */}
            {Array.isArray(responsiveness.hourlyDistribution) && responsiveness.hourlyDistribution.length === 24 && (
              <div className="space-y-1.5 pt-2 border-t border-border">
                <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                  <span>Active hours (incoming messages by UTC hour)</span>
                  <span className="font-mono text-[10px]">00:00 — 23:00</span>
                </div>
                <div className="flex items-end gap-1 h-12 pt-2 bg-muted/20 px-2 rounded-xs border border-border/50">
                  {(() => {
                    const max = Math.max(1, ...responsiveness.hourlyDistribution);
                    return responsiveness.hourlyDistribution.map((cnt, hour) => {
                      const heightPct = Math.round((cnt / max) * 100);
                      return (
                        <div
                          key={hour}
                          className="flex-1 flex flex-col items-center justify-end h-full group relative"
                        >
                          <div
                            style={{ height: `${cnt > 0 ? Math.max(15, heightPct) : 0}%` }}
                            className={`w-full transition-all rounded-t-xs ${
                              cnt > 0 ? 'bg-primary/70 hover:bg-primary' : 'bg-transparent'
                            }`}
                          />
                          <div className="absolute bottom-full mb-1 hidden group-hover:block z-10 bg-popover text-popover-foreground border border-border px-1.5 py-0.5 text-[10px] rounded-xs shadow-sm whitespace-nowrap pointer-events-none">
                            {String(hour).padStart(2, '0')}:00 — {cnt} {cnt === 1 ? 'msg' : 'msgs'}
                          </div>
                        </div>
                      );
                    });
                  })()}
                </div>
              </div>
            )}
          </section>
        )}

        {/* Private Notes */}
        <section className="space-y-2 border border-border bg-card p-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5">
              <FileText className="h-3.5 w-3.5 text-muted-foreground" />
              <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                Private Notes
              </h3>
            </div>
            <div className="flex items-center gap-2">
              {notesSaved && (
                <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
                  <Check className="h-3 w-3" /> Saved
                </span>
              )}
              <button
                type="button"
                onClick={handleSaveNotes}
                disabled={notesSaving || notesDraft === (contact.notes ?? '')}
                className="border border-border px-2.5 py-0.5 text-xs hover:bg-accent/40 disabled:opacity-40 cursor-pointer disabled:cursor-not-allowed"
              >
                {notesSaving ? 'Saving…' : 'Save note'}
              </button>
            </div>
          </div>
          <textarea
            value={notesDraft}
            onChange={e => setNotesDraft(e.target.value)}
            onBlur={() => {
              if (notesDraft !== (contact.notes ?? '')) {
                handleSaveNotes();
              }
            }}
            placeholder="Private notes (markdown supported)... Visible only to you. Searchable via in:contacts notes:keyword or has:notes."
            rows={3}
            className="w-full bg-background border border-border p-2.5 text-xs focus:ring-1 focus:ring-primary outline-none resize-y rounded-xs"
          />
          <p className="text-[11px] text-muted-foreground">
            Private notes are never synced or transmitted. Search with <code className="font-mono">in:contacts has:notes</code> or <code className="font-mono">in:contacts notes:keyword</code>.
          </p>
        </section>

        <section className="space-y-2 border border-border bg-card p-4">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
              What they are
            </h3>
            {saved && (
              <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
                <Check className="h-3 w-3" /> Saved
              </span>
            )}
          </div>

          <div className="flex flex-wrap gap-1">
            {(['person', 'organization', 'system', 'unknown'] as const).map(k => (
              <button
                key={k}
                type="button"
                onClick={() => correct({ kind: k })}
                className={`border px-2 py-1 text-xs cursor-pointer ${
                  contact.kind === k
                    ? 'border-primary bg-primary/10 text-primary font-semibold'
                    : 'border-border hover:bg-accent/40'
                }`}
              >
                {k}
              </button>
            ))}
          </div>
          <p className="text-[11px] text-muted-foreground">
            {contact.kindSource
              ? <>Currently set by <span className="font-mono">{contact.kindSource}</span>. Choosing here records it as yours, and no rule or model run will change it back.</>
              : <>Not classified. Choosing here records it as yours.</>}
          </p>
        </section>

        {(contact.firstName || contact.lastName || contact.phone || contact.org || contact.title) && (
          <section className="space-y-1 border border-border bg-card p-4">
            <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
              Details
            </h3>
            <dl className="divide-y divide-border/60 text-sm">
              <Detail term="First name" value={contact.firstName} />
              <Detail term="Last name" value={contact.lastName} />
              <Detail term="Phone" value={contact.phone} />
              <Detail term="Organisation" value={contact.org} />
              <Detail term="Title" value={contact.title} />
            </dl>
            {contact.enrichedBy && (
              <p className="pt-1 text-[11px] text-muted-foreground">
                Extracted by <span className="font-mono">{contact.enrichedBy}</span>.
              </p>
            )}
          </section>
        )}

        <section className="space-y-3 border border-border bg-card p-4">
          <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
            Files
          </h3>
          {/* Sent first, because "files from Alice" means files Alice sent —
              not every file on a thread she happened to be copied on. The
              wider reading is one click away rather than the default. */}
          <AttachmentSection
            scopes={[
              {
                label: 'They sent',
                query: `in:attachments from:${quoted}`,
                hint: 'Files that arrived on a message they sent',
              },
              {
                label: 'Any message with them',
                query: `in:attachments anyone:${quoted}`,
                hint: 'Files on any message they appear on, however they appear',
              },
            ]}
            emptyLabel="No files."
          />
        </section>

        <section className="space-y-2 border border-border bg-card p-4">
          <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
            Topics
          </h3>
          {topics.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              None recorded. Topics come from an extractor — see the Annotators tab.
            </p>
          ) : (
            <>
              <ul className="divide-y divide-border/60">
                {topics.map(t => (
                  <li key={t.topic} className="flex items-baseline gap-3 py-1.5 text-sm">
                    <button
                      type="button"
                      onClick={() => openQuery(`topic:${t.topic} anyone:${contact.address}`)}
                      className="min-w-0 flex-1 truncate text-left hover:text-primary cursor-pointer"
                    >
                      {t.topic}
                    </button>
                    <span className="w-16 text-right font-mono text-xs tabular-nums text-muted-foreground">
                      {t.messages}
                    </span>
                    {/* Lift, not volume: whoever you exchange the most mail
                        with would otherwise top every topic. */}
                    <span className={`w-14 text-right font-mono text-xs tabular-nums ${
                      t.lift >= 1.5 ? 'text-primary font-semibold' : 'text-muted-foreground'
                    }`}>
                      {t.lift.toFixed(1)}×
                    </span>
                  </li>
                ))}
              </ul>
              <p className="text-[11px] text-muted-foreground">
                × is how much more they discuss it than the mailbox does. Ranked by that rather
                than by count, so the answer is not always whoever writes most.
              </p>
            </>
          )}
        </section>

        {(() => {
          const displayedNetwork = filter.trim()
            ? network.filter(edge => other(edge.label, contact.address).toLowerCase().includes(filter.toLowerCase().trim()))
            : network;
          return (
            <section className="space-y-3 border border-border bg-card p-4">
              <div className="flex items-center justify-between">
                <h3 className="flex items-center gap-1.5 text-xs font-bold uppercase tracking-wider text-muted-foreground">
                  <Users className="h-3 w-3" /> Appears alongside
                  {network.length > 0 && (
                    <span className="font-mono text-[11px] font-normal text-muted-foreground">
                      ({network.length})
                    </span>
                  )}
                </h3>
              </div>
              {network.length === 0 ? (
                <p className="text-xs text-muted-foreground">Nobody — every message is one-to-one.</p>
              ) : (
                <>
                  {network.length > 10 && (
                    <input
                      type="text"
                      placeholder="Filter correspondents…"
                      value={filter}
                      onChange={e => setFilter(e.target.value)}
                      className="w-full bg-background border border-border px-2.5 py-1 text-xs focus:ring-1 focus:ring-primary outline-none rounded-xs"
                    />
                  )}
                  <ul className="divide-y divide-border/60">
                    {displayedNetwork.map(edge => {
                      const otherAddr = other(edge.label, contact.address);
                      return (
                        <li key={edge.label} className="flex items-baseline gap-3 py-1.5 text-sm group">
                          <button
                            type="button"
                            onClick={() => openContact(otherAddr)}
                            className="min-w-0 flex-1 truncate text-left font-mono text-xs hover:text-primary cursor-pointer"
                            title={`Open contact card for ${otherAddr}`}
                          >
                            {otherAddr}
                          </button>
                          <button
                            type="button"
                            onClick={() => openQuery(`anyone:${otherAddr}`)}
                            className="opacity-0 group-hover:opacity-100 transition-opacity text-muted-foreground hover:text-primary cursor-pointer"
                            title={`Show mail with ${otherAddr}`}
                          >
                            <Mail className="h-3 w-3" />
                          </button>
                          <span className="font-mono text-xs tabular-nums text-muted-foreground">
                            {edge.value}
                          </span>
                        </li>
                      );
                    })}
                  </ul>
                  {displayedNetwork.length === 0 && filter.trim() && (
                    <p className="text-xs text-muted-foreground italic py-1">
                      No correspondents match "{filter}".
                    </p>
                  )}
                </>
              )}
            </section>
          );
        })()}
      </div>
    </div>
  );
};

/** The other end of an edge, given one of its ends. */
function other(label: string, self: string): string {
  const ends = label.split(' — ');
  return ends.find(a => a !== self) ?? label;
}

const KindIcon = ({ kind }: { kind: Contact['kind'] }) => {
  const Icon = kind === 'system' ? Bot : kind === 'organization' ? Building2 : kind === 'unknown' ? HelpCircle : User;
  return (
    <span className="flex h-10 w-10 shrink-0 items-center justify-center border border-border bg-muted">
      <Icon className="h-5 w-5 text-muted-foreground" />
    </span>
  );
};

const Stat = ({ label, value }: { label: string; value: string }) => (
  <div className="bg-card p-3">
    <div className="text-[10px] font-bold uppercase tracking-wider text-muted-foreground">{label}</div>
    <div className="font-mono text-lg tabular-nums">{value}</div>
  </div>
);

const Detail = ({ term, value }: { term: string; value?: string }) =>
  value ? (
    <div className="grid grid-cols-[8rem_1fr] gap-3 py-1.5">
      <dt className="text-xs uppercase tracking-wider text-muted-foreground">{term}</dt>
      <dd>{value}</dd>
    </div>
  ) : null;
