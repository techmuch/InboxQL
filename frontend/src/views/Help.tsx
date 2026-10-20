import { CircleHelp } from 'lucide-react';
import { helpTopic, openHelp } from '../lib/helpTopics';

/**
 * A small "?" that opens the guide section explaining where it sits.
 *
 * One per panel or section, not one per control: a "?" beside every switch is
 * noise people learn to ignore. The topic is an id from lib/helpTopics.ts,
 * where the tests check it names a real heading.
 */
export const Help = ({ topic, className = '' }: { topic: string; className?: string }) => {
  const t = helpTopic(topic);
  if (!t) return null;
  return (
    <button
      type="button"
      onClick={e => { e.stopPropagation(); openHelp(topic); }}
      title={`${t.label} — open the guide`}
      aria-label={`Help: ${t.label}`}
      data-help={topic}
      className={`inline-flex shrink-0 items-center justify-center rounded-full text-muted-foreground/70 hover:text-primary focus-visible:text-primary focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary ${className}`}
    >
      <CircleHelp className="h-3.5 w-3.5" />
    </button>
  );
};
