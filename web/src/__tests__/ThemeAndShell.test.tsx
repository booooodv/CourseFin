import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ThemeProvider, useTheme, THEME_STORAGE_KEY } from '../contexts/ThemeContext';

type MediaQueryListener = (event: MediaQueryListEvent) => void;

function mockMatchMedia(initialDark: boolean) {
  let matches = initialDark;
  const listeners = new Set<MediaQueryListener>();

  const mql = {
    get matches() {
      return matches;
    },
    media: '(prefers-color-scheme: dark)',
    onchange: null as MediaQueryListener | null,
    addEventListener: vi.fn((type: string, listener: MediaQueryListener) => {
      if (type === 'change') listeners.add(listener);
    }),
    removeEventListener: vi.fn((type: string, listener: MediaQueryListener) => {
      if (type === 'change') listeners.delete(listener);
    }),
    addListener: vi.fn((listener: MediaQueryListener) => {
      listeners.add(listener);
    }),
    removeListener: vi.fn((listener: MediaQueryListener) => {
      listeners.delete(listener);
    }),
    dispatchEvent: vi.fn(),
  };

  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: vi.fn().mockImplementation(() => mql),
  });

  return {
    mql,
    triggerChange(nextDark: boolean) {
      matches = nextDark;
      const event = { matches: nextDark, media: mql.media } as MediaQueryListEvent;
      listeners.forEach((listener) => listener(event));
    },
  };
}

function ThemeProbe() {
  const { isProvided, mode, resolvedTheme, setMode, cycleMode } = useTheme();
  return (
    <div>
      <span data-testid="is-provided">{String(isProvided)}</span>
      <span data-testid="mode">{mode}</span>
      <span data-testid="resolved-theme">{resolvedTheme}</span>
      <button type="button" onClick={() => setMode('light')}>
        Set Light
      </button>
      <button type="button" onClick={() => setMode('dark')}>
        Set Dark
      </button>
      <button type="button" onClick={() => setMode('system')}>
        Set System
      </button>
      <button type="button" onClick={cycleMode}>
        Cycle
      </button>
    </div>
  );
}

describe('ThemeContext', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    localStorage.clear();
    delete document.documentElement.dataset.theme;
    document.documentElement.classList.remove('dark');
    document.documentElement.style.colorScheme = '';
  });

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      configurable: true,
      value: originalMatchMedia,
    });
    vi.restoreAllMocks();
  });

  it('defaults to system mode, syncs dark DOM attributes from matchMedia, and reacts to change events', () => {
    const { triggerChange } = mockMatchMedia(true);

    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>
    );

    expect(screen.getByTestId('is-provided')).toHaveTextContent('true');
    expect(screen.getByTestId('mode')).toHaveTextContent('system');
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(document.documentElement.style.colorScheme).toBe('dark');

    act(() => {
      triggerChange(false);
    });

    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('light');
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
    expect(document.documentElement.style.colorScheme).toBe('light');
  });

  it('persists setMode calls to localStorage and cycles modes deterministically', async () => {
    const user = userEvent.setup();
    mockMatchMedia(false);

    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>
    );

    await user.click(screen.getByRole('button', { name: 'Set Dark' }));
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(screen.getByTestId('mode')).toHaveTextContent('dark');
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);

    await user.click(screen.getByRole('button', { name: 'Set Light' }));
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
    expect(screen.getByTestId('mode')).toHaveTextContent('light');
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('light');
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);

    await user.click(screen.getByRole('button', { name: 'Set System' }));
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('system');
    expect(screen.getByTestId('mode')).toHaveTextContent('system');

    // Deterministic cycle: system -> light -> dark -> system -> light
    await user.click(screen.getByRole('button', { name: 'Cycle' }));
    expect(screen.getByTestId('mode')).toHaveTextContent('light');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');

    await user.click(screen.getByRole('button', { name: 'Cycle' }));
    expect(screen.getByTestId('mode')).toHaveTextContent('dark');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');

    await user.click(screen.getByRole('button', { name: 'Cycle' }));
    expect(screen.getByTestId('mode')).toHaveTextContent('system');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('system');

    await user.click(screen.getByRole('button', { name: 'Cycle' }));
    expect(screen.getByTestId('mode')).toHaveTextContent('light');
  });

  it('safely falls back on invalid localStorage, throwing storage, missing matchMedia, and missing provider', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      configurable: true,
      value: undefined,
    });

    // Outside provider
    const { unmount } = render(<ThemeProbe />);
    expect(screen.getByTestId('is-provided')).toHaveTextContent('false');
    expect(screen.getByTestId('mode')).toHaveTextContent('system');
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('light');
    unmount();

    // Invalid localStorage value
    localStorage.setItem(THEME_STORAGE_KEY, 'invalid-theme');
    const { unmount: unmountInvalid } = render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>
    );
    expect(screen.getByTestId('mode')).toHaveTextContent('system');
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('light');
    unmountInvalid();

    // Throwing Storage.prototype.getItem and window.localStorage.getItem
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage blocked');
    });
    vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('storage blocked');
    });

    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>
    );
    expect(screen.getByTestId('mode')).toHaveTextContent('system');
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('light');
  });
});
