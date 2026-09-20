import { useState } from 'react';
import { Tag } from 'lucide-react';

import {
  MarkedText,
  SpanLegend,
  countByLabel,
  markTotals,
  type SpanResponse,
} from './MarkedText';

/**
 * What an annotator found in this message, and where.
 *
 * # The slider is the point, not the decoration
 *
 * A span extractor cannot invent a value, but it can be confidently wrong
 * about what a real value *is* — on this mailbox it offered `Table 31` as an
 * invoice number at 0.53 and `TC# 9121537647002640792` as an order number at
 * 0.52, next to a real amount at 0.90. The line between the two was found by
 * writing SQL, which is not a way for anybody to learn it.
 *
 * So the floor is a control with the counts beside it. Moving it dims the
 * marks that fall below, on real mail, which teaches the threshold in a way a
 * number in a settings page never would. Dimming rather than hiding, because
 * hiding would make the slider feel like it was finding more instead of
 * trusting less.
 */

/** Where the slider starts. See the comment above: 0.6 is the line on this
 *  mailbox between the real extractions and three card-shaped strings. */
const DEFAULT_FLOOR = 0.6;

export const SpanPanel = ({ spans }: { spans: SpanResponse | null }) => {
  const [floor, setFloor] = useState(DEFAULT_FLOOR);

  if (!spans) return null;
  const { total, kept } = markTotals(spans.fields, floor);
  if (total === 0 && spans.other.length === 0) return null;

  const counts = countByLabel(spans.fields, floor);
  const subject = spans.fields.find(f => f.field === 'subject');
  const body = spans.fields.find(f => f.field === 'body');

  return (
    <div className="mt-2 space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2 rounded border border-sky-500/25 bg-sky-500/10 px-3 py-1.5 text-xs text-sky-700 dark:text-sky-300">
        <span className="flex items-center gap-1.5 font-medium">
          <Tag className="h-3.5 w-3.5" />
          Extracted values
        </span>
        <span className="font-mono text-[10px] opacity-75">
          {/* Said plainly: these are pieces of this message, not a model's
              summary of it. That is the whole claim of the engine. */}
          every value below is a piece of this message
        </span>
      </div>

      {total > 0 && (
        <div className="flex flex-wrap items-center gap-3 rounded border border-border bg-muted/20 px-3 py-2">
          <label className="flex items-center gap-2 text-[11px] text-muted-foreground">
            <span className="font-bold uppercase tracking-wider">Confidence</span>
            <input
              type="range"
              min={0}
              max={0.95}
              step={0.05}
              value={floor}
              onChange={e => setFloor(Number(e.target.value))}
              className="h-1 w-40 cursor-pointer accent-sky-500"
            />
            <span className="w-8 font-mono tabular-nums text-foreground">{floor.toFixed(2)}</span>
          </label>
          <span className="text-[11px] text-muted-foreground">
            <span className="font-mono tabular-nums text-foreground">{kept}</span> of{' '}
            <span className="font-mono tabular-nums">{total}</span> shown
            {kept < total && <span className="opacity-70"> · the rest are underlined</span>}
          </span>
          <div className="ml-auto">
            <SpanLegend labels={spans.labels} counts={counts} />
          </div>
        </div>
      )}

      {subject && subject.marks > 0 && (
        <div className="rounded border border-border bg-muted/10 px-4 py-2">
          <div className="mb-1 text-[10px] font-bold uppercase tracking-wider text-muted-foreground">
            Subject
          </div>
          <MarkedText
            field={subject}
            labels={spans.labels}
            floor={floor}
            className="font-sans text-sm leading-relaxed"
          />
        </div>
      )}

      {body && (
        <div className="rounded border border-sky-500/20 bg-sky-500/[0.03] p-6 font-sans text-sm leading-relaxed text-foreground/90 dark:bg-sky-950/10">
          <MarkedText
            field={body}
            labels={spans.labels}
            floor={floor}
            className="whitespace-pre-wrap"
          />
        </div>
      )}

      {spans.other.length > 0 && (
        <div className="rounded border border-border bg-muted/10 px-4 py-2 text-[11px] text-muted-foreground">
          {/* A rule or LLM annotator has no offsets. Saying what else matched
              is better than silence; pretending to know where it matched is
              not. */}
          <div className="mb-1 font-bold uppercase tracking-wider">Also matched</div>
          {spans.other.map((o, i) => (
            <div key={i} className="font-mono">
              {o.annotator || 'unnamed'}
              <span className="opacity-60">
                {' '}
                · {o.status}
                {o.engine ? ` · ${o.engine}` : ''}
                {o.score !== undefined ? ` · ${(o.score * 100).toFixed(0)}%` : ''}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
