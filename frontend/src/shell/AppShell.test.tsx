import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { AppShell } from './AppShell';
import { Evidence } from './Evidence';
import { usePlaceStore } from '../lib/places';
import { useQueryStore } from '../lib/filters';

const AVAILABLE = new Set(['desk', 'calendar', 'board', 'settings', 'help', 'review']);

function frame(children: React.ReactNode = <p>screen</p>) {
  return render(
    <AppShell available={AVAILABLE} labelOf={p => p} onQuery={() => {}} evidenceDefault={<p>nothing here yet</p>}>
      {children}
    </AppShell>,
  );
}

beforeEach(() => {
  localStorage.clear();
  usePlaceStore.setState({ id: 'calendar', recent: [] });
  useQueryStore.getState().clear();
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ kind: 'count', total: 3 }), { status: 200 })));
  // The panel starts open on a wide window.
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1440 });
});

describe('the frame', () => {
  it('has the landmarks: the query, places, the screen and the evidence', () => {
    frame();
    expect(screen.getByRole('search', { name: 'Query' })).toBeTruthy();
    expect(screen.getByRole('navigation', { name: 'Places' })).toBeTruthy();
    expect(screen.getByRole('main')).toBeTruthy();
    expect(screen.getByRole('complementary', { name: 'Evidence' })).toBeTruthy();
  });

  it('leaves out places that are not built', () => {
    frame();
    expect(screen.queryByRole('link', { name: /Ahead/ })).toBeNull();
    expect(screen.getByRole('link', { name: 'Calendar' })).toBeTruthy();
  });

  it('marks the place on screen as the current page', () => {
    frame();
    expect(screen.getByRole('link', { name: 'Calendar' }).getAttribute('aria-current')).toBe('page');
    expect(screen.getByRole('link', { name: 'Board' }).getAttribute('aria-current')).toBeNull();
  });

  it('an item that is a question sets the query and goes to the mail list', () => {
    frame();
    fireEvent.click(screen.getByRole('link', { name: /^Everything/ }));
    expect(usePlaceStore.getState().id).toBe('desk');
    expect(useQueryStore.getState().text).toBe('folder:allmail');
  });

  it('shows the default evidence until a screen supplies its own', () => {
    const { rerender } = frame();
    expect(screen.getByText('nothing here yet')).toBeTruthy();
    rerender(
      <AppShell available={AVAILABLE} labelOf={p => p} onQuery={() => {}} evidenceDefault={<p>nothing here yet</p>}>
        <Evidence><p>why the agent thinks so</p></Evidence>
      </AppShell>,
    );
    expect(screen.getByText('why the agent thinks so')).toBeTruthy();
    expect(screen.queryByText('nothing here yet')).toBeNull();
  });
});

describe('the evidence divider', () => {
  it('is a separator whose value is the panel width, moved by the arrow keys', () => {
    frame();
    const sep = screen.getByRole('separator', { name: 'Resize the evidence panel' });
    expect(sep.getAttribute('aria-valuenow')).toBe('352');
    fireEvent.keyDown(sep, { key: 'ArrowLeft' });
    expect(sep.getAttribute('aria-valuenow')).toBe('368');
    fireEvent.keyDown(sep, { key: 'ArrowRight', shiftKey: true });
    expect(sep.getAttribute('aria-valuenow')).toBe('304');
    expect(localStorage.getItem('iql.evidenceWidth')).toBe('304');
  });

  it('Enter hides the panel and says so; Enter again brings it back', () => {
    frame();
    const sep = screen.getByRole('separator');
    fireEvent.keyDown(sep, { key: 'Enter' });
    expect(sep.getAttribute('aria-valuetext')).toBe('Evidence panel hidden');
    fireEvent.keyDown(sep, { key: 'Enter' });
    expect(sep.getAttribute('aria-valuetext')).toBe('Evidence panel 352 pixels wide');
  });

  it('"." hides it from anywhere but a field', () => {
    frame(<input aria-label="a field" />);
    const sep = screen.getByRole('separator');
    act(() => { fireEvent.keyDown(screen.getByLabelText('a field'), { key: '.' }); });
    expect(sep.getAttribute('aria-valuenow')).toBe('352');
    act(() => { fireEvent.keyDown(window, { key: '.' }); });
    expect(sep.getAttribute('aria-valuenow')).toBe('0');
  });
});
