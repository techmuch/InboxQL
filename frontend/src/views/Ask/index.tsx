import { useEffect, useRef, useState } from 'react';
import { askStatus, setAskCLI, useAsk, type AskStatus } from '../../lib/ask';
import { openQuery } from '../../lib/tabs';
import { Evidence } from '../../shell/Evidence';

/**
 * Ask in words: a question answered by Claude Code on this computer.
 *
 * The answer is never only prose. Beside it, in the evidence panel, is every
 * command Claude Code ran — each a query you can open — and what was sent
 * where. A follow-up continues the same conversation.
 */
export const Ask = () => {
  const { turns, ask, reset } = useAsk();
  const [status, setStatus] = useState<AskStatus | null | undefined>(undefined);
  const [text, setText] = useState('');
  const [error, setError] = useState<string | null>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { void askStatus().then(setStatus); }, []);
  const pending = turns.some(t => (t as { pending?: boolean }).pending);
  const last = turns.at(-1);

  const submit = (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!text.trim() || pending) return;
    void ask(text);
    setText('');
  };

  const turnOn = async () => {
    try { setStatus(await setAskCLI('claude')); setError(null); } catch (err) { setError((err as Error).message); }
  };

  if (status === undefined) return <div className="p-6 text-sm text-muted-foreground">Loading…</div>;

  if (!status?.enabled) {
    const installed = Boolean(status?.available?.claude);
    return (
      <div className="mx-auto max-w-3xl px-6 py-5">
        <h1 className="text-2xl font-semibold">Ask in words</h1>
        <p className="mt-2">Ask a question about your mail in plain words — “how much have we spent on the kitchen?” — and Claude Code answers it on this computer, running InboxQL’s read-only commands. The answer comes with every command it ran.</p>
        <section className="mt-5 border border-amber-600 bg-card p-4 dark:border-amber-400" aria-labelledby="ask-sends">
          <h2 id="ask-sends" className="font-semibold">What leaves this computer</h2>
          <p className="mt-1 text-sm">{status?.sends ?? 'Your question, and what the commands return, to Anthropic, by Claude Code under your own account.'}</p>
          <p className="mt-1 text-sm">It can only read: it cannot send, archive, label or change anything. Each answer reports what Claude Code says it cost.</p>
        </section>
        {installed ? (
          <button type="button" className="iql-btn primary mt-4" onClick={turnOn}>Turn on asking with Claude Code</button>
        ) : (
          <p className="mt-4 text-sm">Claude Code is not installed on this computer. Install it and sign in once by running <code>claude</code> in a terminal, then come back.</p>
        )}
        {error && <p role="alert" className="mt-2 text-sm text-destructive">{error}</p>}
        <p className="mt-4 text-sm text-muted-foreground">You can turn it off again in Settings → AI and agents. Queries never need it: <button type="button" className="underline" onClick={() => openQuery('')}>write one yourself</button>.</p>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-3xl px-6 py-5">
      <div className="flex items-baseline gap-3">
        <h1 className="flex-1 text-2xl font-semibold">Ask in words</h1>
        {turns.length > 0 && <button type="button" className="text-sm underline" onClick={reset}>New question</button>}
      </div>
      <p className="mt-1 text-sm text-muted-foreground">Answered by Claude Code on this computer, reading your mail with iql. It cannot change anything.</p>

      <ol className="mt-5 space-y-6" aria-label="Questions and answers">
        {turns.map((t, i) => {
          const p = (t as { pending?: boolean }).pending;
          return (
            <li key={i} aria-busy={p || undefined}>
              <p className="text-sm text-muted-foreground">You asked</p>
              <h2 className="text-lg font-semibold">{t.question}</h2>
              {p ? (
                <p role="status" className="mt-2 text-sm text-muted-foreground">Claude Code is reading your mail… this usually takes 10 to 40 seconds.</p>
              ) : t.error ? (
                <p role="alert" className="mt-2 border border-destructive px-3 py-2 text-sm">Could not answer: {t.error}</p>
              ) : (
                <section aria-label="Answer" className="mt-2 border border-border bg-card px-4 py-3">
                  <div className="whitespace-pre-wrap text-[15px] leading-relaxed">{t.text}</div>
                  <p className="mt-2 text-xs text-muted-foreground">
                    {t.cli} · {t.seconds} s · ran {t.commands.length} {t.commands.length === 1 ? 'command' : 'commands'}
                    {t.costUsd ? ` · reported cost $${t.costUsd.toFixed(2)}` : ''}
                  </p>
                </section>
              )}
            </li>
          );
        })}
      </ol>

      <form className="mt-6" onSubmit={submit} aria-label={turns.length ? 'Ask a follow-up' : 'Ask a question'}>
        <label htmlFor="ask-q" className="mb-1 block text-sm font-medium">{turns.length && !last?.error ? 'Ask a follow-up — it continues the same conversation' : 'Your question'}</label>
        <textarea id="ask-q" ref={input} rows={2} value={text} onChange={e => setText(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) submit(); }}
          placeholder="How much have we spent on the kitchen so far?" className="w-full border border-border bg-background px-3 py-2 text-[15px]" />
        <div className="mt-2 flex items-center gap-3">
          <button type="submit" className="iql-btn primary" disabled={pending || !text.trim()}>Ask <kbd>⌘↵</kbd></button>
          {pending && <span className="text-sm text-muted-foreground">Waiting for the answer…</span>}
        </div>
      </form>

      <Evidence>
        <h2 className="mb-1 text-base font-semibold">How it was answered</h2>
        {!last || (last as { pending?: boolean }).pending ? (
          <p className="text-sm text-muted-foreground">The commands Claude Code runs appear here, each one a query you can open.</p>
        ) : (
          <>
            <p className="mb-2 text-sm text-muted-foreground">Commands Claude Code ran for the last answer:</p>
            {last.commands.length === 0 ? <p className="text-sm">None.</p> : (
              <ol className="space-y-2 pl-5 text-sm" style={{ listStyle: 'decimal' }}>
                {last.commands.map((c, i) => {
                  const q = /query\s+"([^"]+)"/.exec(c.command)?.[1];
                  return (
                    <li key={i}>
                      <code className="break-all text-xs">{c.command}</code>
                      {c.refused && <span className="ml-1 text-xs text-amber-700 dark:text-amber-300">— refused: not a read command</span>}
                      {q && <div><button type="button" className="text-xs underline" onClick={() => openQuery(q)}>Open this query</button></div>}
                    </li>
                  );
                })}
              </ol>
            )}
          </>
        )}
        <h3 className="mb-1 mt-5 text-sm font-semibold">Sent to Anthropic</h3>
        <p className="text-sm">{status.sends}</p>
        <h3 className="mb-1 mt-5 text-sm font-semibold">Changed</h3>
        <p className="text-sm">Nothing. Claude Code may only run iql’s read commands here.</p>
      </Evidence>
    </div>
  );
};
