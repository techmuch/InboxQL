import { useEffect, useState } from 'react';

/**
 * Extracted values, shown where they were found.
 *
 * # Why the server sends runs rather than offsets
 *
 * The stored offsets are byte offsets into UTF-8; a JavaScript string is
 * UTF-16. Slicing here with the former would land a byte or two short on
 * exactly the messages that contain a narrow no-break space — the one a mail
 * client writes in "9:50 PM" — and the result looks like a broken decoder
 * rather than an encoding mismatch. So this component never computes an
 * offset: the API hands it text already cut into runs, and it concatenates
 * and marks.
 */

export interface Segment {
  text: string;
  label?: string;
  score?: number;
  annotator?: string;
  annotationId?: string;
  source?: string;
}

export interface MarkedField {
  field: string;
  segments: Segment[];
  marks: number;
}

export interface PlainAnnotation {
  annotator: string;
  kind?: string;
  engine?: string;
  status: string;
  source: string;
  score?: number;
  data?: unknown;
  model?: string;
}

export interface SpanResponse {
  messageId: string;
  fields: MarkedField[];
  other: PlainAnnotation[];
  labels: string[];
}

/**
 * Colour carries the label, not the score.
 *
 * Encoding confidence in colour as well would be a second thing to learn, and
 * it would fight the confidence slider, which is the control that actually
 * decides what counts. Score lives in the hover and in the opacity of spans
 * below the floor.
 */
const PALETTE = [
  'bg-sky-500/20 border-sky-500/40',
  'bg-emerald-500/20 border-emerald-500/40',
  'bg-amber-500/20 border-amber-500/40',
  'bg-violet-500/20 border-violet-500/40',
  'bg-rose-500/20 border-rose-500/40',
  'bg-teal-500/20 border-teal-500/40',
  'bg-orange-500/20 border-orange-500/40',
  'bg-indigo-500/20 border-indigo-500/40',
];

/** Stable per label, so a colour means the same thing in every message. */
export function colourFor(label: string, labels: string[]): string {
  const i = labels.indexOf(label);
  return PALETTE[(i < 0 ? 0 : i) % PALETTE.length];
}

export const useMessageSpans = (messageId: string | undefined) => {
  const [spans, setSpans] = useState<SpanResponse | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!messageId) {
      setSpans(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    fetch(`/api/messages/${encodeURIComponent(messageId)}/annotations`)
      .then(r => (r.ok ? r.json() : null))
      .then(d => {
        if (!cancelled) setSpans(d);
      })
      // A message with no annotations is the normal case, not an error. The
      // viewer shows the body either way.
      .catch(() => {
        if (!cancelled) setSpans(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [messageId]);

  return { spans, setSpans, loading };
};

/**
 * One field's text with its extracted values marked.
 *
 * Spans scoring below `floor` are not hidden but dimmed to plain text with a
 * dotted underline. Hiding them would make the slider feel like it was finding
 * more rather than trusting less, and the point of the control is to show what
 * is being left out.
 */
export const MarkedText = ({
  field,
  labels,
  floor = 0,
  className = '',
  onPick,
  picked,
}: {
  field: MarkedField;
  labels: string[];
  floor?: number;
  className?: string;
  /** Set to make the marks clickable, for correcting one. */
  onPick?: (segmentIndex: number) => void;
  picked?: number | null;
}) => (
  <div className={className}>
    {field.segments.map((s, i) => {
      if (!s.label) return <span key={i}>{s.text}</span>;

      const below = (s.score ?? 0) < floor;
      const human = s.source === 'human';
      const isPicked = picked === i;
      const title =
        `${s.label}` +
        (s.score !== undefined ? ` · ${(s.score * 100).toFixed(0)}%` : '') +
        (s.annotator ? ` · ${s.annotator}` : '') +
        (human ? ' · your correction' : '') +
        (onPick ? ' — click to correct' : '');

      return (
        <mark
          key={i}
          title={title}
          onClick={onPick ? () => onPick(i) : undefined}
          className={
            (below
              ? 'bg-transparent text-inherit underline decoration-dotted decoration-muted-foreground/60 underline-offset-2'
              : `rounded-[3px] border-b-2 px-[1px] text-inherit ${colourFor(s.label, labels)} ${
                  human ? 'ring-1 ring-foreground/30' : ''
                }`) +
            (onPick ? ' cursor-pointer' : '') +
            (isPicked ? ' outline outline-2 outline-primary' : '')
          }
        >
          {s.text}
        </mark>
      );
    })}
  </div>
);

/**
 * Every mark in the response, as the correction payload expects them.
 *
 * A correction replaces the whole set, so rejecting one value means sending
 * the others back unchanged. Rebuilding the list from what is displayed keeps
 * that honest: what you see is what gets asserted.
 *
 * Offsets are recomputed here from the segment lengths, in BYTES, because that
 * is what the store holds. `new TextEncoder().encode(s).length` rather than
 * `s.length`, or a message containing the narrow no-break space in "9:50 PM"
 * writes back offsets a byte or two short of where it read them.
 */
export interface Correction {
  label: string;
  field: string;
  start: number;
  end: number;
  text: string;
}

export function correctionsFrom(fields: MarkedField[], drop?: { field: string; index: number }): Correction[] {
  const bytes = (s: string) => new TextEncoder().encode(s).length;
  const out: Correction[] = [];

  for (const f of fields) {
    let offset = 0;
    f.segments.forEach((s, i) => {
      const len = bytes(s.text);
      if (s.label && !(drop && drop.field === f.field && drop.index === i)) {
        out.push({ label: s.label, field: f.field, start: offset, end: offset + len, text: s.text });
      }
      offset += len;
    });
  }
  return out;
}

/** The same list with one mark's label changed. */
export function relabel(
  fields: MarkedField[],
  at: { field: string; index: number },
  label: string,
): Correction[] {
  const bytes = (s: string) => new TextEncoder().encode(s).length;
  const out: Correction[] = [];

  for (const f of fields) {
    let offset = 0;
    f.segments.forEach((s, i) => {
      const len = bytes(s.text);
      if (s.label) {
        const isTarget = at.field === f.field && at.index === i;
        out.push({
          label: isTarget ? label : s.label,
          field: f.field,
          start: offset,
          end: offset + len,
          text: s.text,
        });
      }
      offset += len;
    });
  }
  return out;
}

/** A legend, so a colour can be read without hovering every mark. */
export const SpanLegend = ({
  labels,
  counts,
}: {
  labels: string[];
  counts: Record<string, number>;
}) => (
  <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
    {labels.map(l => (
      <span key={l} className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <span className={`inline-block h-2.5 w-2.5 rounded-sm border ${colourFor(l, labels)}`} />
        {l}
        {counts[l] ? <span className="opacity-60">{counts[l]}</span> : null}
      </span>
    ))}
  </div>
);

/** How many marks each label has, at or above the floor. */
export function countByLabel(fields: MarkedField[], floor: number): Record<string, number> {
  const out: Record<string, number> = {};
  for (const f of fields) {
    for (const s of f.segments) {
      if (!s.label) continue;
      if ((s.score ?? 0) < floor) continue;
      out[s.label] = (out[s.label] ?? 0) + 1;
    }
  }
  return out;
}

/** Total marks, and how many survive the floor. Drives the slider's caption. */
export function markTotals(fields: MarkedField[], floor: number): { total: number; kept: number } {
  let total = 0;
  let kept = 0;
  for (const f of fields) {
    for (const s of f.segments) {
      if (!s.label) continue;
      total++;
      if ((s.score ?? 0) >= floor) kept++;
    }
  }
  return { total, kept };
}
