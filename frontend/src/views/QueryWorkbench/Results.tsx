import { Inbox } from 'lucide-react';
import { openMessage } from '../../lib/tabs';
import { drillDownTerm, type QueryResult } from './api';

interface ResultsProps {
  result: QueryResult;
  /** Narrow the query to one group, by appending the term it stands for. */
  onDrillDown: (term: string) => void;
}

/**
 * The result pane.
 *
 * One endpoint returns three shapes, and `kind` says which — so this switches
 * rather than guessing from whether an array happens to be populated. Getting
 * that wrong would render an aggregate as an empty message list, which reads as
 * "nothing matched" rather than "you asked a different question".
 */
export const Results = ({ result, onDrillDown }: ResultsProps) => {
  switch (result.kind) {
    case 'count':
      return <CountResult total={result.total ?? 0} />;
    case 'groups':
      return <GroupResult result={result} onDrillDown={onDrillDown} />;
    default:
      return <MessageResult result={result} />;
  }
};

const CountResult = ({ total }: { total: number }) => (
  <div className="flex flex-col items-center justify-center h-full gap-1 py-16">
    <div className="font-mono text-5xl font-semibold tabular-nums">{total.toLocaleString()}</div>
    <div className="text-sm text-muted-foreground">
      {total === 1 ? 'message matches' : 'messages match'}
    </div>
  </div>
);

/**
 * Aggregate results, as a table with the magnitude drawn behind each row.
 *
 * The bar is a background on the label cell rather than a charting library:
 * one dependency less, it stays legible at any row count, and it lines up with
 * the number it describes instead of sitting in a separate panel.
 */
const GroupResult = ({ result, onDrillDown }: ResultsProps) => {
  const groups = result.groups ?? [];
  if (groups.length === 0) return <Empty label="No groups" />;

  const max = Math.max(...groups.map(g => Math.abs(g.value)), 1);
  const isNumeric = groups.every(g => Number.isInteger(g.value));

  return (
    <div className="overflow-auto h-full">
      <table className="w-full text-sm">
        <thead className="sticky top-0 bg-background border-b border-border">
          <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
            <th className="px-4 py-2 font-medium">{result.groupField ?? 'Group'}</th>
            <th className="px-4 py-2 font-medium text-right w-32">Value</th>
          </tr>
        </thead>
        <tbody>
          {groups.map(g => {
            const term = drillDownTerm(result, g.label);
            const width = `${Math.max((Math.abs(g.value) / max) * 100, 1)}%`;
            return (
              <tr
                key={g.label}
                onClick={() => term && onDrillDown(term)}
                title={term ? `Narrow to ${term}` : undefined}
                className={`border-b border-border/50 ${
                  term ? 'cursor-pointer hover:bg-accent/40' : ''
                }`}
              >
                <td className="px-4 py-1.5 relative">
                  <span
                    aria-hidden="true"
                    className="absolute inset-y-0.5 left-0 bg-primary/15"
                    style={{ width }}
                  />
                  <span className="relative font-mono">{g.label || '(none)'}</span>
                </td>
                <td className="px-4 py-1.5 text-right font-mono tabular-nums">
                  {isNumeric ? g.value.toLocaleString() : g.value.toFixed(2)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};

const MessageResult = ({ result }: { result: QueryResult }) => {
  const messages = result.messages ?? [];
  if (messages.length === 0) return <Empty label="No messages matched" />;

  return (
    <div className="overflow-auto h-full">
      <table className="w-full text-sm">
        <thead className="sticky top-0 bg-background border-b border-border">
          <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
            <th className="px-4 py-2 font-medium w-28">Date</th>
            <th className="px-4 py-2 font-medium w-64">From</th>
            <th className="px-4 py-2 font-medium">Subject</th>
          </tr>
        </thead>
        <tbody>
          {messages.map(m => {
            const unread = !(m.flags ?? []).includes('\\Seen');
            return (
              <tr
                key={m.id}
                onClick={() => openMessage(m)}
                className="border-b border-border/50 cursor-pointer hover:bg-accent/40"
              >
                <td className="px-4 py-1.5 font-mono text-xs text-muted-foreground whitespace-nowrap">
                  {new Date(m.date).toLocaleDateString()}
                </td>
                <td className="px-4 py-1.5 truncate max-w-0">{m.from}</td>
                <td className={`px-4 py-1.5 truncate max-w-0 ${unread ? 'font-medium' : ''}`}>
                  {m.subject || <span className="text-muted-foreground">(no subject)</span>}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};

const Empty = ({ label }: { label: string }) => (
  <div className="flex flex-col items-center justify-center h-full gap-2 py-16 text-muted-foreground">
    <Inbox size={28} className="opacity-40" />
    <p className="text-sm">{label}</p>
  </div>
);
