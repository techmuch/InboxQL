import { Approvals } from '../Approvals';
import { Disagreements } from '../WorkLoop/Disagreements';
import { Evidence } from '../../shell/Evidence';

/**
 * To review: one queue for everything an agent or a rule proposed and a person
 * decides — mail waiting to send, calendar changes, cards, and the agent's
 * answers that a second reading disagreed with.
 *
 * It replaces four places: Intelligence → Waiting for approval, the Calendar
 * sidebar's proposals, the Board's Proposed list and the Work loop's
 * disagreements. Each still shows its own proposals; this is where they meet.
 */
export const Review = () => (
  <Approvals title="To review">
    <section className="border border-border bg-card p-4" aria-label="Agent answers to settle">
      <Disagreements onSettled={() => {}} />
    </section>
    <Evidence>
      <h2 className="mb-1 text-base font-semibold">How this queue is made</h2>
      <ul className="list-disc space-y-1 pl-5 text-sm">
        <li><strong>Cards</strong> an extractor or agent proposed: <code className="text-xs">in:tickets state:proposed</code>.</li>
        <li><strong>Calendar changes</strong> an agent proposed. Nothing is on your calendar until you approve it.</li>
        <li><strong>Mail</strong> queued to send. An agent never sends; it waits here for your Send.</li>
        <li><strong>Agent answers</strong> a blind second reading disagreed with. Your choice is recorded as the right answer.</li>
      </ul>
      <p className="mt-3 text-sm text-muted-foreground">Rejecting is kept, not deleted: it is the evidence that the proposal was wrong.</p>
    </Evidence>
  </Approvals>
);
