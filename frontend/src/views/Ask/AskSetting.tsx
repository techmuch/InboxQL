import { useEffect, useState } from 'react';
import { askStatus, setAskCLI, type AskStatus } from '../../lib/ask';

/** Settings → AI and agents: whether questions in words are answered, and by what. */
export const AskSetting = () => {
  const [s, setS] = useState<AskStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => { void askStatus().then(setS); }, []);
  if (!s) return null;
  const choose = async (cli: '' | 'claude') => {
    try { setS(await setAskCLI(cli)); setError(null); } catch (e) { setError((e as Error).message); }
  };
  return (
    <section id="setting-ask-cli" className="mb-8 border border-border p-4">
      <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">Ask in words</h3>
      <p className="mt-1 text-sm">A question in plain words, answered by an agent on this computer that may only read your mail.</p>
      <fieldset className="mt-3">
        <legend className="sr-only">Which agent answers</legend>
        <label className="flex items-center gap-2 text-sm"><input type="radio" name="ask-cli" checked={s.cli === ''} onChange={() => choose('')} /> Off</label>
        <label className="mt-1 flex items-center gap-2 text-sm">
          <input type="radio" name="ask-cli" checked={s.cli === 'claude'} disabled={!s.available.claude} onChange={() => choose('claude')} />
          Claude Code {s.available.claude ? <span className="text-muted-foreground">· {s.available.claude}</span> : <span className="text-muted-foreground">· not installed</span>}
        </label>
      </fieldset>
      <p className="mt-2 text-xs text-muted-foreground">When on: {s.sends}</p>
      {error && <p role="alert" className="mt-2 text-sm text-destructive">{error}</p>}
    </section>
  );
};
