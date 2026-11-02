import { create } from 'zustand';
import { goTo } from './places';

/**
 * Subjects: a person or a topic, as a page.
 *
 * Which one is open is held here, beside the place, so the page survives a
 * move to a message and Back.
 */
interface SubjectState {
  topic: string | null;
  setTopic: (t: string) => void;
}

export const useSubjectStore = create<SubjectState>(set => ({
  topic: null,
  setTopic: topic => set({ topic }),
}));

/** Open a topic's page. */
export const openTopic = (topic: string) => {
  useSubjectStore.getState().setTopic(topic);
  goTo('topic');
};

export interface TopicCount { topic: string; count: number }

export async function listTopics(limit = 100, signal?: AbortSignal): Promise<TopicCount[]> {
  try {
    const r = await fetch(`/api/topics?limit=${limit}`, { signal });
    if (!r.ok) return [];
    return (await r.json()).topics ?? [];
  } catch {
    return [];
  }
}
