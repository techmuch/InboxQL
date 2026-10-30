import { fullDate, shortDate } from '../lib/dates';

/**
 * A date in a list, said briefly — see shortDate — with the whole date a
 * hover away, and as a <time> element so the value itself is machine-readable.
 */
export const When = ({ value, dateOnly, className }: {
  value: string | number | Date | undefined | null;
  dateOnly?: boolean;
  className?: string;
}) => {
  const text = shortDate(value, { dateOnly });
  if (!text) return null;
  const d = value instanceof Date ? value : new Date(value as string | number);
  return (
    <time dateTime={d.toISOString()} title={fullDate(value, { dateOnly })} className={className}>
      {text}
    </time>
  );
};
