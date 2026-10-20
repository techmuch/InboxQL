import { ResponsiveCalendar, ResponsiveTimeRange } from '@nivo/calendar';
import { useThemeStore } from 'nexus-shell';

export interface DayCount {
  /** YYYY-MM-DD, as `| count by day` labels its groups. */
  day: string;
  value: number;
}

export function dayAfter(day: string): string {
  const [y, m, d] = day.split('-').map(Number);
  const next = new Date(y, m - 1, d + 1);
  return `${next.getFullYear()}-${String(next.getMonth() + 1).padStart(2, '0')}-${String(next.getDate()).padStart(2, '0')}`;
}

/**
 * Mail per day as a heatmap: the dashboard's Communication Intensity and a
 * contact card's How often they write. One component so the two cannot drift
 * apart in colour or in how they look in the dark theme.
 *
 * A calendar year is drawn as nivo's Calendar. Any other range — the card's
 * last twelve months — is one strip of weeks, because Calendar splits a range
 * at New Year into two rows that are each mostly empty.
 */
export const ActivityCalendar = ({ data, from, to, onDay }: {
  data: DayCount[];
  /** First and last day shown, YYYY-MM-DD, both included. */
  from: string;
  to: string;
  onDay?: (day: string) => void;
}) => {
  const { theme } = useThemeStore();
  const dark = theme === 'dark';
  const common = {
    data,
    from,
    to,
    emptyColor: dark ? '#27272a' : '#f4f4f5',
    colors: ['#61cdbb', '#97e3d5', '#e8c1a0', '#f47560'],
    dayBorderWidth: 2,
    dayBorderColor: dark ? '#18181b' : '#ffffff',
    theme: {
      text: { fill: dark ? '#a1a1aa' : '#71717a', fontSize: 10, fontWeight: 600 },
      tooltip: { container: { background: dark ? '#18181b' : '#ffffff', color: dark ? '#fafafa' : '#18181b' } },
    },
    onClick: onDay ? (datum: { day: string }) => onDay(datum.day) : undefined,
  };

  if (from.slice(0, 4) !== to.slice(0, 4)) {
    return (
      <ResponsiveTimeRange
        {...common}
        // TimeRange stops the day before `to`; without this, today — the day
        // most likely to have mail on it — was not drawn.
        to={dayAfter(to)}
        margin={{ top: 24, right: 12, bottom: 0, left: 0 }}
        // Room for "Mon", not nivo's 75px for "Wednesday". The week starts on
        // Sunday because nivo counts its columns from Sunday, and starting on
        // Monday pushed the last week past the edge.
        weekdays={['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']}
        weekdayTicks={[1, 3, 5]}
        weekdayLegendOffset={32}
      />
    );
  }
  return (
    <ResponsiveCalendar
      {...common}
      margin={{ top: 20, right: 0, bottom: 0, left: 20 }}
      yearSpacing={40}
      monthBorderColor={dark ? '#18181b' : '#ffffff'}
    />
  );
};
