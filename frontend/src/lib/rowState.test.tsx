import { describe, it, expect, beforeEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { ariaCurrent, rowStateClasses, useIsCurrent } from './rowState';
import { useViewerStore, type ViewerKind } from './tabs';

/**
 * The regression these exist for.
 *
 * "Which row am I looking at" was answered by the browser's focus ring, which is
 * lost the moment focus goes anywhere else — so arrowing onto a message and then
 * clicking into the viewer to read it left the list showing nothing at all.
 */
describe('rowStateClasses', () => {
  it('fills a selected row and outlines the current one', () => {
    const selected = rowStateClasses({ selected: true, current: false });
    const current = rowStateClasses({ selected: false, current: true });

    expect(selected).toContain('bg-primary/10');
    // Current has no fill, so a selection of exactly one row cannot be misread
    // as a preview.
    expect(current).not.toContain('bg-primary/10');
    expect(current).toContain('ring-2');
  });

  /**
   * One ring, not two.
   *
   * `ring-1 ring-2` in a single class attribute does not resolve by the order
   * the caller wrote them — Tailwind emits both rules and the later one in the
   * stylesheet wins. Appending the two states' classes would therefore draw an
   * unpredictable ring, which is why this is computed in one place.
   */
  it('draws exactly one ring width when a row is both', () => {
    const both = rowStateClasses({ selected: true, current: true });
    expect(both).toContain('ring-2');
    expect(both).not.toContain('ring-1');
    // And it still fills, because it really is selected.
    expect(both).toContain('bg-primary/15');
  });

  it('leaves a plain row to its hover', () => {
    expect(rowStateClasses({ selected: false, current: false })).toBe('hover:bg-accent/40');
  });
});

describe('ariaCurrent', () => {
  // aria-current="false" is a value a screen reader announces, so fifty rows
  // saying they are not the current one is worse than silence.
  it('is absent rather than false', () => {
    expect(ariaCurrent(true)).toBe('true');
    expect(ariaCurrent(false)).toBeUndefined();
  });
});

describe('useIsCurrent', () => {
  beforeEach(() => useViewerStore.getState().clear());

  const Row = ({ kind, id }: { kind: ViewerKind; id: string | null }) => {
    const current = useIsCurrent(kind, id);
    return <div data-testid={`${kind}-${id}`}>{current ? 'current' : 'not'}</div>;
  };

  it('tracks the viewer slot for its kind', () => {
    render(<Row kind="message" id="m1" />);
    expect(screen.getByTestId('message-m1').textContent).toBe('not');

    act(() => { useViewerStore.getState().setMessage({ id: 'm1' }); });
    expect(screen.getByTestId('message-m1').textContent).toBe('current');
  });

  it('identifies a file by its content address', () => {
    const key = 'a'.repeat(64);
    render(<Row kind="file" id={key} />);
    act(() => { useViewerStore.getState().setFile({ key, filename: 'invoice.pdf' }); });
    expect(screen.getByTestId(`file-${key}`).textContent).toBe('current');
  });

  it('answers false for a row with no id rather than needing a branch', () => {
    render(<Row kind="message" id={null} />);
    act(() => { useViewerStore.getState().setMessage({ id: 'm1' }); });
    expect(screen.getByTestId('message-null').textContent).toBe('not');
  });

  /**
   * The cascade, in one assertion.
   *
   * A file selected in the Desk list and a message selected inside that file are
   * different viewer slots, so both stay lit. One global "current row" would
   * un-highlight the file the moment a message inside it was previewed — while
   * the file is still on screen in its own tab.
   */
  it('keeps a file current while a message inside it is shown', () => {
    const key = 'b'.repeat(64);
    act(() => { useViewerStore.getState().setMode('split'); });
    render(
      <>
        <Row kind="file" id={key} />
        <Row kind="message" id="m2" />
      </>,
    );

    act(() => { useViewerStore.getState().setFile({ key, filename: 'contract.pdf' }); });
    act(() => { useViewerStore.getState().setMessage({ id: 'm2' }); });

    expect(screen.getByTestId(`file-${key}`).textContent).toBe('current');
    expect(screen.getByTestId('message-m2').textContent).toBe('current');
  });

  // Reuse mode holds one subject at a time, so the second one replaces the
  // first. That is the mode working, not the highlight failing.
  it('and in reuse mode the file gives way, because the tab does', () => {
    const key = 'c'.repeat(64);
    act(() => { useViewerStore.getState().setMode('reuse'); });
    render(
      <>
        <Row kind="file" id={key} />
        <Row kind="message" id="m3" />
      </>,
    );

    act(() => { useViewerStore.getState().setFile({ key, filename: 'contract.pdf' }); });
    act(() => { useViewerStore.getState().setMessage({ id: 'm3' }); });

    expect(screen.getByTestId(`file-${key}`).textContent).toBe('not');
    expect(screen.getByTestId('message-m3').textContent).toBe('current');
  });
});
