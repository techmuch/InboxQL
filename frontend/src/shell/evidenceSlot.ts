import { create } from 'zustand';

/** Where screens put their evidence: the panel's slot element. */
export const EVIDENCE_SLOT_ID = 'evidence-slot';

interface EvidenceSlotState {
  /** The panel's slot, set by the frame when it mounts. */
  slot: HTMLElement | null;
  /** How many screens are filling it right now, so its default steps aside. */
  n: number;
  setSlot: (el: HTMLElement | null) => void;
  add: (d: number) => void;
}

export const useEvidenceCount = create<EvidenceSlotState>(set => ({
  slot: null,
  n: 0,
  setSlot: slot => set({ slot }),
  add: d => set(s => ({ n: Math.max(0, s.n + d) })),
}));
