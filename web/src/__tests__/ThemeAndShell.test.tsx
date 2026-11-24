import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ThemeProvider, useTheme, THEME_STORAGE_KEY } from '../contexts/ThemeContext';
import ThemeSwitcher from '../components/ThemeSwitcher';
import LibraryFormModal from '../components/LibraryFormModal';

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

describe('ThemeSwitcher', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    localStorage.clear();
    delete document.documentElement.dataset.theme;
    document.documentElement.classList.remove('dark');
    document.documentElement.style.colorScheme = '';
    mockMatchMedia(false);
  });

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      configurable: true,
      value: originalMatchMedia,
    });
    vi.restoreAllMocks();
  });

  it('renders segmented buttons with aria-pressed and updates localStorage and <html data-theme>', async () => {
    const user = userEvent.setup();
    render(
      <ThemeProvider>
        <ThemeSwitcher variant="segmented" />
      </ThemeProvider>
    );

    const group = screen.getByRole('group', { name: '主题模式' });
    expect(group).toBeInTheDocument();

    const lightBtn = screen.getByRole('button', { name: '浅色' });
    const darkBtn = screen.getByRole('button', { name: '深色' });
    const systemBtn = screen.getByRole('button', { name: '自动' });

    expect(systemBtn).toHaveAttribute('aria-pressed', 'true');
    expect(lightBtn).toHaveAttribute('aria-pressed', 'false');
    expect(darkBtn).toHaveAttribute('aria-pressed', 'false');

    await user.click(darkBtn);
    expect(darkBtn).toHaveAttribute('aria-pressed', 'true');
    expect(systemBtn).toHaveAttribute('aria-pressed', 'false');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');

    await user.click(lightBtn);
    expect(lightBtn).toHaveAttribute('aria-pressed', 'true');
    expect(darkBtn).toHaveAttribute('aria-pressed', 'false');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
    expect(document.documentElement.dataset.theme).toBe('light');
  });

  it('renders compact button and cycles theme modes on click', async () => {
    const user = userEvent.setup();
    render(
      <ThemeProvider>
        <ThemeSwitcher variant="compact" tooltipPlacement="right" />
      </ThemeProvider>
    );

    const toggleBtn = screen.getByRole('button', { name: '切换主题模式' });
    expect(document.documentElement.dataset.theme).toBe('light');

    // system -> light -> dark -> system
    await user.click(toggleBtn);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
    expect(document.documentElement.dataset.theme).toBe('light');

    await user.click(toggleBtn);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');

    await user.click(toggleBtn);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('system');
    expect(document.documentElement.dataset.theme).toBe('light');
  });
});

describe('LibraryFormModal', () => {
  it('validates empty input, submits trimmed name, and closes on Escape in create mode', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    const onSubmit = vi.fn();

    render(
      <LibraryFormModal
        isOpen={true}
        mode="create"
        onClose={onClose}
        onSubmit={onSubmit}
      />
    );

    const dialog = screen.getByRole('dialog');
    expect(dialog).toBeInTheDocument();

    const input = screen.getByLabelText('新媒体库名称');
    expect(input).toHaveFocus();

    await user.click(screen.getByRole('button', { name: '确认创建' }));
    expect(screen.getByText('请输入媒体库名称')).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();

    await user.type(input, '  后端工程  ');
    await user.click(screen.getByRole('button', { name: '确认创建' }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith('后端工程');

    await user.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('prefills initialName and submits via 保存名称 in rename mode', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    const onSubmit = vi.fn();

    render(
      <LibraryFormModal
        isOpen={true}
        mode="rename"
        initialName="旧媒体库"
        onClose={onClose}
        onSubmit={onSubmit}
      />
    );

    const input = screen.getByLabelText('重命名媒体库名称');
    expect(input).toHaveValue('旧媒体库');

    await user.clear(input);
    await user.type(input, '新媒体库');
    await user.click(screen.getByRole('button', { name: '保存名称' }));

    expect(onSubmit).toHaveBeenCalledWith('新媒体库');
  });
});
