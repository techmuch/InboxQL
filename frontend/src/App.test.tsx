import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import App from './App';

describe('App component', () => {
  it('renders the login view when unauthenticated', async () => {
    render(<App />);
    expect(await screen.findByRole('heading', { name: 'InboxQL' })).toBeDefined();
    expect(screen.getByText('Email for Engineers')).toBeDefined();
    expect(screen.getByText('Sign In')).toBeDefined();
  });
});
