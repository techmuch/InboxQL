import { useCallback, useEffect, useState } from 'react';
import { Loader2, Monitor, RefreshCw, Send, Radio, RadioTower } from 'lucide-react';
import { listWindows, sendToWindow, useWindowsStore, type WindowInfo } from '../lib/windows';

/**
 * Every open window, and what each is showing.
 *
 * # Why a person wants this, not just an agent
 *
 * Two windows on two screens, or one on a laptop and one on a desktop, and no
 * way to tell what the other is doing without walking over to it. This is the
 * same answer `iql ui list` gives, in the place somebody is already standing.
 *
 * It is also how the names become meaningful. A window called "2" is a label
 * nobody can map to a screen until something shows them the mapping — so this
 * marks which row is the window you are reading it in.
 *
 * # What it is not
 *
 * Not a mirror. Pointing another window at a query sends it an instruction; it
 * does not make this window the owner of what that one shows. Reload that
 * window and it returns to its own state.
 */
export const Windows = () => {
  const myName = useWindowsStore(s => s.name);
  const connected = useWindowsStore(s => s.connected);

  const [windows, setWindows] = useState<WindowInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [sending, setSending] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setWindows(await listWindows());
      setError(null);
    } catch (e: any) {
      setError(`Could not read the open windows: ${e.message ?? e}`);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
    // Polled rather than streamed: this is a list of who is connected, which
    // changes when somebody opens or closes a window — not something that
    // needs to be live to the second, and a second stream per window to watch
    // the streams would be more machinery than the answer is worth.
    const t = window.setInterval(load, 5000);
    return () => window.clearInterval(t);
  }, [load]);

  const send = async (name: string) => {
    const q = (draft[name] ?? '').trim();
    if (!q) return;
    setSending(name);
    try {
      await sendToWindow(name, 'query', q, `sent from window ${myName ?? '?'}`);
      setDraft(d => ({ ...d, [name]: '' }));
      setError(null);
      await load();
    } catch (e: any) {
      setError(`Could not reach window ${name}: ${e.message ?? e}`);
    } finally {
      setSending(null);
    }
  };

  return (
    <div className="flex flex-col h-full bg-background text-foreground">
      <div className="h-12 border-b border-border flex items-center px-4 gap-3 shrink-0">
        <Monitor className="w-4 h-4 text-muted-foreground" />
        <h2 className="text-sm font-bold">Windows</h2>
        <span className="text-[11px] text-muted-foreground">
          {myName
            ? <>this one is <span className="font-mono font-bold text-foreground">{myName}</span>{connected ? '' : ' · not connected'}</>
            : 'this one is not registered'}
        </span>
        <div className="flex-1" />
        <span className="text-xs text-muted-foreground tabular-nums">
          {windows.length === 1 ? '1 open' : `${windows.length} open`}
        </span>
        <button onClick={load} disabled={loading}
          className="p-2 hover:bg-accent text-muted-foreground disabled:opacity-40" title="Refresh">
          {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
        </button>
      </div>

      {error && (
        <div className="m-4 border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="flex-1 overflow-auto p-4 space-y-3">
        {!loading && windows.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No windows registered. One appears here as soon as a browser connects.
          </p>
        )}

        {windows.map(w => {
          const isMe = w.name === myName;
          return (
            <div key={w.name}
              className={`border px-4 py-3 ${isMe ? 'border-primary bg-primary/5' : 'border-border'}`}>
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono font-bold text-sm">{w.name}</span>
                {/* Which row is the window you are reading this in. Without it
                    the names are labels nobody can map to a screen. */}
                {isMe && <span className="text-[10px] font-bold uppercase text-primary">this window</span>}
                {w.connected
                  ? <span className="flex items-center gap-1 text-[10px] text-emerald-600 dark:text-emerald-400">
                      <RadioTower className="w-3 h-3" /> live
                    </span>
                  : <span className="flex items-center gap-1 text-[10px] text-amber-600 dark:text-amber-400"
                      title="Open, but its command channel is not connected — it cannot be sent anything">
                      <Radio className="w-3 h-3" /> no channel
                    </span>}
                {w.title && <span className="text-[11px] text-muted-foreground truncate">{w.title}</span>}
                <span className="ml-auto text-[10px] text-muted-foreground tabular-nums">
                  seen {ago(w.lastSeen)}
                </span>
              </div>

              <div className="mt-2 text-xs">
                <span className="text-muted-foreground">showing </span>
                {w.showing?.query
                  ? <span className="font-mono">{w.showing.query}</span>
                  : <span className="text-muted-foreground">nothing in particular</span>}
              </div>
              {w.showing?.tabs && w.showing.tabs.length > 0 && (
                <div className="mt-1 flex flex-wrap gap-1">
                  {w.showing.tabs.map(t => (
                    <span key={t}
                      className={`px-1.5 py-0.5 text-[10px] font-mono ${
                        t === w.showing.active ? 'bg-primary/15 text-primary' : 'bg-muted text-muted-foreground'
                      }`}>
                      {t}
                    </span>
                  ))}
                </div>
              )}

              {/* Said, because a window showing something nobody in front of it
                  asked for is otherwise a mystery. */}
              {w.lastCommand && (
                <p className="mt-2 text-[11px] text-muted-foreground">
                  last told: <span className="font-mono">{w.lastCommand.verb} {w.lastCommand.arg}</span>
                  {w.lastCommand.note && <> — {w.lastCommand.note}</>}
                </p>
              )}

              {!isMe && w.connected && (
                <div className="mt-3 flex items-center gap-2">
                  <input
                    value={draft[w.name] ?? ''}
                    onChange={e => setDraft(d => ({ ...d, [w.name]: e.target.value }))}
                    onKeyDown={e => { if (e.key === 'Enter') void send(w.name); }}
                    placeholder={`point window ${w.name} at a query`}
                    className="flex-1 bg-background border border-border px-2 py-1 font-mono text-xs
                               outline-none focus:border-primary"
                  />
                  <button
                    onClick={() => send(w.name)}
                    disabled={sending === w.name || !(draft[w.name] ?? '').trim()}
                    className="flex items-center gap-1 border border-border px-2 py-1 text-xs
                               hover:bg-accent/40 disabled:opacity-40">
                    {sending === w.name
                      ? <Loader2 className="w-3 h-3 animate-spin" />
                      : <Send className="w-3 h-3" />}
                    Send
                  </button>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

function ago(iso: string): string {
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (s < 2) return 'now';
  if (s < 60) return `${s}s ago`;
  return `${Math.round(s / 60)}m ago`;
}
