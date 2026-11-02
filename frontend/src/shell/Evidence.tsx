import { useEffect, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useEvidenceCount } from './evidenceSlot';

/**
 * The evidence panel's contents, from whichever screen is showing.
 *
 * A screen says what belongs on the right — why the agent concluded
 * something, how a list was made, the message under the cursor — by
 * rendering `<Evidence>`. It lands in the panel through a portal, so the
 * screen keeps owning its state and the frame stays ignorant of every screen.
 */
export const Evidence = ({ children }: { children: ReactNode }) => {
  const slot = useEvidenceCount(s => s.slot);
  useEffect(() => {
    const { add } = useEvidenceCount.getState();
    add(1);
    return () => add(-1);
  }, []);
  return slot ? createPortal(children, slot) : null;
};
