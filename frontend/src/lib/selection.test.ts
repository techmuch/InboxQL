import { describe, it, expect, beforeEach } from 'vitest';
import {
  describeSelection, narrowable, refKey, refOf, selectedRefs, useSelectionStore,
  type SelectionRef,
} from './selection';

const msg = (id: string): SelectionRef => ({ kind: 'message', id, field: 'id' });
const thread = (id: string): SelectionRef => ({ kind: 'thread', id, field: 'thread' });

describe('selection', () => {
  beforeEach(() => useSelectionStore.getState().clear());

  // Selection used to be a Set of message ids, which only worked because the
  // only navigable list was messages.
  it('keys by kind, so ids may collide across kinds', () => {
    const { toggle } = useSelectionStore.getState();
    toggle(msg('x'));
    toggle(thread('x'));

    const refs = selectedRefs(useSelectionStore.getState().refs);
    expect(refs).toHaveLength(2);
    expect(refKey(msg('x'))).not.toBe(refKey(thread('x')));
  });

  it('toggles, replaces and extends', () => {
    const store = () => useSelectionStore.getState();

    store().only(msg('a'));
    expect(selectedRefs(store().refs)).toHaveLength(1);

    store().add(msg('b'));
    expect(selectedRefs(store().refs)).toHaveLength(2);

    store().toggle(msg('b'));
    expect(selectedRefs(store().refs)).toHaveLength(1);

    // `only` replaces rather than adds, which is what an unmodified click means.
    store().add(msg('c'));
    store().only(msg('d'));
    expect(selectedRefs(store().refs).map(r => r.id)).toEqual(['d']);
  });

  it('counts each kind rather than flattening them', () => {
    // A timeline holds conversations and the messages inside them, so a mixed
    // selection is normal and one number would describe nothing.
    expect(describeSelection([msg('a'), msg('b'), thread('t')]))
      .toBe('2 messages, 1 conversation');
    expect(describeSelection([msg('a')])).toBe('1 message');
  });

  it('turns a same-kind selection into one field and its values', () => {
    expect(narrowable([msg('a'), msg('b')])).toEqual({ field: 'id', values: ['a', 'b'] });
    expect(narrowable([thread('t1'), thread('t2')]))
      .toEqual({ field: 'thread', values: ['t1', 't2'] });
  });

  it('refuses to narrow a mixed selection', () => {
    // Conversations and messages are named by different fields, so this is two
    // queries. Producing one would match neither.
    expect(narrowable([msg('a'), thread('t')])).toBeNull();
    expect(narrowable([])).toBeNull();
    // A row that declares no field cannot be narrowed to at all.
    expect(narrowable([{ kind: 'group', id: 'acme.com' }])).toBeNull();
  });

  it('reads a row identity off the DOM', () => {
    document.body.innerHTML = `
      <div id="row" data-sel-kind="ticket" data-sel-id="t1"
           data-sel-field="id" data-sel-label="Pay the invoice">
        <span id="inner">child</span>
      </div>
      <div id="plain">not a row</div>`;

    expect(refOf(document.getElementById('row'))).toEqual({
      kind: 'ticket', id: 't1', field: 'id', label: 'Pay the invoice',
    });
    // From a child, because focus often lands on something inside the row.
    expect(refOf(document.getElementById('inner'))?.id).toBe('t1');
    expect(refOf(document.getElementById('plain'))).toBeNull();
    expect(refOf(null)).toBeNull();
  });
});
