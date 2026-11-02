import { openTool } from '../../lib/tabs';
import { Countdowns } from '../Ahead/Countdowns';
import { Section } from './Section';

/**
 * Today's look ahead: the dates you marked important, counted down, and the
 * way into Ahead for the weeks and months to come. Last on Today, because it
 * is rarely the first thing to act on.
 */
export const AheadStrip = () => (
  <Section id="t-ahead" title="Ahead" more={{ label: 'Two weeks and two months', go: () => openTool('ahead', 'Ahead') }}>
    <Countdowns compact limit={4} onMark={() => openTool('ahead', 'Ahead')} />
  </Section>
);
