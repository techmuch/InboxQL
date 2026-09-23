import { useCallback, useRef, useState } from 'react';

/**
 * Running an annotator as a job, and watching it.
 *
 * # Why a job and not a request
 *
 * The panel used to POST and wait. For a rule annotator that is milliseconds.
 * For a span annotator it is up to 200 messages at about twenty seconds each —
 * over an hour in one HTTP request. The browser gives up, nothing moves, and
 * the work that did happen is invisible until the page is reloaded.
 *
 * The bar that looked broken was never a run bar at all: it draws
 * evaluated/total, which is coverage, and coverage only changes when the
 * request returns. So a slow engine now goes through the maintenance job
 * system — the one triggers already use — which reports progress over SSE and
 * can be told to stop.
 *
 * A rule run stays a plain request. Routing something instant through a job
 * would add a spinner to work that is already done.
 */

export interface JobState {
  id: string;
  status: string;
  done: number;
  total: number;
  current: string;
  summary?: string;
  lastError?: string;
}

const running = (s?: string) => s === 'queued' || s === 'running';

export const useAnnotatorJob = (onFinished: () => void) => {
  const [job, setJob] = useState<JobState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const source = useRef<EventSource | null>(null);

  const close = useCallback(() => {
    source.current?.close();
    source.current = null;
  }, []);

  const watch = useCallback(
    (id: string) => {
      close();
      const es = new EventSource(`/api/maintenance/jobs/${id}/events`);
      source.current = es;
      es.onmessage = e => {
        try {
          const j = JSON.parse(e.data) as JobState;
          setJob(j);
          if (!running(j.status)) {
            close();
            // The coverage bar is read from the annotator list, so it only
            // becomes true again once that is re-read.
            onFinished();
          }
        } catch {
          /* a malformed frame is not worth tearing the stream down for */
        }
      };
      es.onerror = () => {
        close();
        onFinished();
      };
    },
    [close, onFinished],
  );

  const start = useCallback(
    async (name: string, limit = 200) => {
      setError(null);
      try {
        const r = await fetch('/api/maintenance/jobs', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ kind: 'annotators', annotator: name, limit }),
        });
        const body = await r.json();
        if (r.status === 409) {
          // Something is already running. Watching it is what the caller
          // wanted anyway, and is better than a second job contending for the
          // same CPU.
          if (body.jobId) watch(body.jobId);
          return;
        }
        if (!r.ok) throw new Error(body.error || 'could not start the run');
        setJob(body);
        watch(body.id);
      } catch (e) {
        setError(e instanceof Error ? e.message : 'could not start the run');
      }
    },
    [watch],
  );

  const cancel = useCallback(async () => {
    if (!job) return;
    await fetch(`/api/maintenance/jobs/${job.id}/cancel`, { method: 'POST' }).catch(() => {});
  }, [job]);

  return {
    job,
    error,
    start,
    cancel,
    busy: running(job?.status),
    clear: () => {
      close();
      setJob(null);
      setError(null);
    },
  };
};
