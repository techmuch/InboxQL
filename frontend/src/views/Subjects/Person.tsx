import { useViewerStore } from '../../lib/tabs';
import { ContactCard } from '../ContactCard';
import { Stream } from './Stream';

/**
 * A person as a page: who they are and what is open between you — the
 * contact card, which leads with open loops — then everything with them.
 */
export const Person = () => {
  const address = useViewerStore(s => s.contact);
  if (!address) {
    return <div className="mx-auto max-w-4xl px-6 py-5"><h1 className="text-2xl font-semibold">Person</h1><p className="mt-2 text-muted-foreground">Open a person from a message, People, or Go to.</p></div>;
  }
  return (
    <div className="mx-auto max-w-4xl px-2 py-3">
      <ContactCard address={address} />
      <div className="px-4 pb-6"><Stream query={`anyone:=${address}`} title="Everything with them" limit={20} /></div>
    </div>
  );
};
