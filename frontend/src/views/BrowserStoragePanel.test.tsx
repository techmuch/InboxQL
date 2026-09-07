import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { BrowserStoragePanel } from './BrowserStoragePanel';

describe('BrowserStoragePanel', () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    vi.restoreAllMocks();
  });

  it('renders empty storage message when no keys exist', () => {
    render(<BrowserStoragePanel />);
    expect(screen.getByText('Browser Storage')).toBeDefined();
    expect(screen.getByText('No storage keys match the current filter.')).toBeDefined();
    expect(screen.getByText('0')).toBeDefined(); // total entries
  });

  it('scans and categorizes known and custom storage keys', async () => {
    localStorage.setItem('nexus-shell-layout', JSON.stringify({ root: { type: 'row' } }));
    localStorage.setItem('nexus-shell-theme', 'dark');
    sessionStorage.setItem('iql_dev_reloaded_0.0.38_inst1', '1');
    localStorage.setItem('custom-data-key', 'plain-text-value');

    render(<BrowserStoragePanel />);

    // Check categorized items are displayed
    expect(screen.getAllByText('nexus-shell-layout').length).toBeGreaterThan(0);
    expect(screen.getByText('nexus-shell-theme')).toBeDefined();
    expect(screen.getByText('iql_dev_reloaded_0.0.38_inst1')).toBeDefined();
    expect(screen.getByText('custom-data-key')).toBeDefined();

    expect(screen.getByText('Workspace Layout')).toBeDefined();
    expect(screen.getByText('UI Theme')).toBeDefined();
    expect(screen.getByText('Dev Reload Guard')).toBeDefined();
    expect(screen.getByText('Application Data')).toBeDefined();
  });

  it('filters items by search query', async () => {
    localStorage.setItem('nexus-shell-layout', JSON.stringify({}));
    localStorage.setItem('user-preference', 'test');

    render(<BrowserStoragePanel />);
    expect(screen.getAllByText('nexus-shell-layout').length).toBeGreaterThan(0);
    expect(screen.getByText('user-preference')).toBeDefined();

    const searchInput = screen.getByPlaceholderText('Filter keys or contents...');
    fireEvent.change(searchInput, { target: { value: 'preference' } });

    // The row for nexus-shell-layout should be gone, though the explainer card still mentions it
    const rows = screen.queryAllByRole('button', { name: /Expand|Collapse/i });
    expect(rows.length).toBe(1);
    expect(screen.getByText('user-preference')).toBeDefined();
  });

  it('filters items by storage scope', async () => {
    localStorage.setItem('local-key', 'foo');
    sessionStorage.setItem('session-key', 'bar');

    render(<BrowserStoragePanel />);
    expect(screen.getByText('local-key')).toBeDefined();
    expect(screen.getByText('session-key')).toBeDefined();

    // Click Session Storage scope
    fireEvent.click(screen.getByRole('button', { name: 'Session Storage' }));
    expect(screen.queryByText('local-key')).toBeNull();
    expect(screen.getByText('session-key')).toBeDefined();

    // Click Local Storage scope
    fireEvent.click(screen.getByRole('button', { name: 'Local Storage' }));
    expect(screen.getByText('local-key')).toBeDefined();
    expect(screen.queryByText('session-key')).toBeNull();
  });

  it('allows expanding an item to inspect formatted JSON and description', async () => {
    localStorage.setItem('nexus-shell-layout', JSON.stringify({ tabs: ['desk'] }));

    render(<BrowserStoragePanel />);
    // The row key span
    const rowKeys = screen.getAllByText('nexus-shell-layout');
    // Click the row span (not the code tag in the alert)
    fireEvent.click(rowKeys[rowKeys.length - 1]);

    expect(await screen.findByText('Formatted JSON')).toBeDefined();
    expect(screen.getByText(/Serialized FlexLayout model/)).toBeDefined();
    expect(screen.getByText(/"desk"/)).toBeDefined();
  });

  it('allows deleting an individual key', async () => {
    localStorage.setItem('temp-key', 'temp-val');

    render(<BrowserStoragePanel />);
    expect(screen.getByText('temp-key')).toBeDefined();

    const deleteBtn = screen.getByRole('button', { name: 'Delete temp-key' });
    fireEvent.click(deleteBtn);

    expect(localStorage.getItem('temp-key')).toBeNull();
    await waitFor(() => {
      expect(screen.queryByText('temp-key')).toBeNull();
    });
  });

  it('resets layout when Reset Layout is clicked', async () => {
    localStorage.setItem('nexus-shell-layout', JSON.stringify({ root: {} }));

    render(<BrowserStoragePanel />);
    expect(localStorage.getItem('nexus-shell-layout')).not.toBeNull();

    const resetBtn = screen.getByRole('button', { name: /Reset Layout/i });
    fireEvent.click(resetBtn);

    expect(localStorage.getItem('nexus-shell-layout')).toBeNull();
    expect(await screen.findByText(/Workspace layout reset/)).toBeDefined();
  });

  it('copies item value to clipboard when copy button is clicked', async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, {
      clipboard: { writeText: writeTextMock },
    });

    localStorage.setItem('copy-key', 'secret-val');
    render(<BrowserStoragePanel />);

    const copyBtn = screen.getByRole('button', { name: 'Copy raw value' });
    fireEvent.click(copyBtn);

    await waitFor(() => {
      expect(writeTextMock).toHaveBeenCalledWith('secret-val');
    });
  });
});
