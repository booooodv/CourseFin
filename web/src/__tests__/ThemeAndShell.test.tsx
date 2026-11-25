import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { ThemeProvider, useTheme, THEME_STORAGE_KEY } from '../contexts/ThemeContext';
import {
  LibraryProvider,
  useLibrary,
  SIDEBAR_COLLAPSED_STORAGE_KEY,
} from '../contexts/LibraryContext';
import { ToastProvider } from '../components/Toast';
import { UpdateProvider } from '../components/UpdateContext';
import ThemeSwitcher from '../components/ThemeSwitcher';
import LibraryFormModal from '../components/LibraryFormModal';
import Layout from '../components/Layout';
import { AppRoutes } from '../App';
import type { Library } from '../types/coursefin';

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

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeLibrary(overrides: Partial<Library> = {}): Library {
  return {
    id: 'lib-1',
    name: '默认库',
    cover_path: '',
    sort_order: 1,
    course_count: 0,
    created_at: '2026-11-24T00:00:00Z',
    updated_at: '2026-11-24T00:00:00Z',
    ...overrides,
  };
}

function ShellLocationProbe() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <div>
      <div data-testid="shell-location">
        {location.pathname}
        {location.search}
      </div>
      <button type="button" onClick={() => navigate('/')}>
        Go Home
      </button>
      <button type="button" onClick={() => navigate('/courses/c1')}>
        Go Player
      </button>
      <button type="button" onClick={() => navigate('/courses/c1/edit')}>
        Go Editor
      </button>
    </div>
  );
}

describe('LibraryContext, Layout AppShell, & AppRoutes', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem('coursefin_token', 'test-token');
    mockMatchMedia(false);
  });

  afterEach(() => {
    localStorage.clear();
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      configurable: true,
      value: originalMatchMedia,
    });
    vi.restoreAllMocks();
  });

  it('fetches libraries & user, supports sidebar navigation, create, rename, and deleting active library back to /', async () => {
    let libraries: Library[] = [
      makeLibrary({ id: 'lib-dev', name: '编程开发', sort_order: 1, course_count: 3 }),
      makeLibrary({ id: 'lib-design', name: '设计艺术', sort_order: 2, course_count: 1 }),
    ];

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = init?.method ?? 'GET';

      if (url === '/api/v1/libraries' && method === 'GET') {
        return jsonResponse({ libraries });
      }
      if (url === '/api/v1/auth/me' && method === 'GET') {
        return jsonResponse({
          user: { id: 'u1', username: 'alice', created_at: '', updated_at: '' },
          session: {
            token: 'test-token',
            user_id: 'u1',
            device_id: 'web',
            device_name: 'Web',
            client_name: 'CourseFin Web',
            client_version: '1.1.0',
            last_active_at: '',
            created_at: '',
          },
        });
      }
      if (url === '/api/v1/libraries' && method === 'POST') {
        const body = JSON.parse(String(init?.body)) as { name: string };
        const created = makeLibrary({
          id: 'lib-fe',
          name: body.name,
          sort_order: 3,
          course_count: 0,
        });
        libraries = [...libraries, created];
        return jsonResponse(created, 201);
      }
      if (url === '/api/v1/libraries/lib-fe' && method === 'PUT') {
        const body = JSON.parse(String(init?.body)) as { name: string };
        const updated = makeLibrary({
          id: 'lib-fe',
          name: body.name,
          sort_order: 3,
          course_count: 0,
        });
        libraries = libraries.map((lib) => (lib.id === 'lib-fe' ? updated : lib));
        return jsonResponse(updated);
      }
      if (url === '/api/v1/libraries/lib-fe' && method === 'DELETE') {
        libraries = libraries.filter((lib) => lib.id !== 'lib-fe');
        return jsonResponse({ ok: true });
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', fetchMock);

    render(
      <ToastProvider>
        <ThemeProvider>
          <MemoryRouter initialEntries={['/']}>
            <LibraryProvider>
              <Routes>
                <Route path="*" element={<Layout />}>
                  <Route index element={<ShellLocationProbe />} />
                  <Route path="storage" element={<ShellLocationProbe />} />
                </Route>
              </Routes>
            </LibraryProvider>
          </MemoryRouter>
        </ThemeProvider>
      </ToastProvider>,
    );

    expect(await screen.findByText('alice')).toBeInTheDocument();
    expect(screen.getByText('我的媒体库')).toBeInTheDocument();
    expect(await screen.findByRole('group', { name: '媒体库 编程开发' })).toBeInTheDocument();
    expect(screen.getByRole('group', { name: '媒体库 设计艺术' })).toBeInTheDocument();

    const user = userEvent.setup();

    // Initially on /, 课程库 is active
    const courseNavLink = screen.getByRole('link', { name: '课程库' });
    expect(courseNavLink.className).toContain('bg-accent-soft');

    // Navigate to a library -> 课程库 deactivates while library row activates
    await user.click(screen.getByRole('button', { name: /编程开发/ }));
    expect(screen.getByTestId('shell-location')).toHaveTextContent('/?library=lib-dev');
    expect(courseNavLink.className).not.toContain('bg-accent-soft');
    expect(screen.getByRole('group', { name: '媒体库 编程开发' }).className).toContain('bg-accent-soft');

    // Create a new library via + button
    await user.click(screen.getByRole('button', { name: '新建媒体库' }));
    const createDialog = await screen.findByRole('dialog');
    await user.type(within(createDialog).getByLabelText('新媒体库名称'), '前端工程');
    await user.click(within(createDialog).getByRole('button', { name: '确认创建' }));

    expect(await screen.findByRole('group', { name: '媒体库 前端工程' })).toBeInTheDocument();
    expect(await screen.findByText('已创建媒体库「前端工程」')).toBeInTheDocument();

    // Rename the created library
    const createdGroup = screen.getByRole('group', { name: '媒体库 前端工程' });
    await user.click(within(createdGroup).getByRole('button', { name: '重命名媒体库' }));
    const renameDialog = await screen.findByRole('dialog');
    const renameInput = within(renameDialog).getByLabelText('重命名媒体库名称');
    expect(renameInput).toHaveValue('前端工程');
    await user.clear(renameInput);
    await user.type(renameInput, '全栈架构');
    await user.click(within(renameDialog).getByRole('button', { name: '保存名称' }));

    expect(await screen.findByRole('group', { name: '媒体库 全栈架构' })).toBeInTheDocument();
    expect(await screen.findByText('已将媒体库重命名为「全栈架构」')).toBeInTheDocument();

    // Select the renamed library so it is active, then delete it and verify navigation returns to /
    const renamedGroup = screen.getByRole('group', { name: '媒体库 全栈架构' });
    await user.click(within(renamedGroup).getByRole('button', { name: /全栈架构/ }));
    expect(screen.getByTestId('shell-location')).toHaveTextContent('/?library=lib-fe');

    await user.click(within(renamedGroup).getByRole('button', { name: '删除媒体库' }));
    const deleteDialog = await screen.findByRole('dialog', { name: /删除媒体库/ });
    await user.click(within(deleteDialog).getByRole('button', { name: '确认删除' }));

    await waitFor(() => {
      expect(screen.queryByRole('group', { name: '媒体库 全栈架构' })).not.toBeInTheDocument();
    });
    expect(await screen.findByText('已删除媒体库「全栈架构」')).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByTestId('shell-location')).toHaveTextContent(/^\/$/);
    });
  });

  it('persists desktop collapse state and falls back to expanded on invalid or throwing localStorage', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/libraries') {
          return jsonResponse({
            libraries: [makeLibrary({ id: 'lib-dev', name: '编程开发', course_count: 5 })],
          });
        }
        if (url === '/api/v1/auth/me') {
          return jsonResponse({ user: { id: 'u1', username: 'alice', created_at: '', updated_at: '' } });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const firstRender = render(
      <ToastProvider>
        <ThemeProvider>
          <MemoryRouter initialEntries={['/']}>
            <LibraryProvider>
              <Layout>
                <ShellLocationProbe />
              </Layout>
            </LibraryProvider>
          </MemoryRouter>
        </ThemeProvider>
      </ToastProvider>,
    );

    const sidebar = await screen.findByRole('complementary');
    expect(sidebar).toHaveAttribute('data-collapsed', 'false');

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '收起侧边栏' }));
    expect(sidebar).toHaveAttribute('data-collapsed', 'true');
    expect(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY)).toBe('true');

    // Collapsed mode shows library squircle with right tooltip on hover
    const libBadge = screen.getByRole('button', { name: '编程开发' });
    await user.hover(libBadge);
    await waitFor(() => {
      expect(screen.getByRole('tooltip')).toHaveTextContent('编程开发 (5)');
    });

    await user.click(screen.getByRole('button', { name: '展开侧边栏' }));
    expect(sidebar).toHaveAttribute('data-collapsed', 'false');
    expect(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY)).toBe('false');
    firstRender.unmount();

    // Invalid localStorage value falls back to expanded
    localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, 'not-a-boolean');
    const invalidRender = render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/']}>
          <Layout>
            <div>内容</div>
          </Layout>
        </MemoryRouter>
      </ThemeProvider>,
    );
    expect(screen.getByRole('complementary')).toHaveAttribute('data-collapsed', 'false');
    invalidRender.unmount();

    // Throwing localStorage falls back to expanded without crashing
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage blocked');
    });
    vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('storage blocked');
    });

    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/']}>
          <Layout>
            <div>内容</div>
          </Layout>
        </MemoryRouter>
      </ThemeProvider>,
    );
    expect(screen.getByRole('complementary')).toHaveAttribute('data-collapsed', 'false');
  });

  it('auto-collapses transiently on /courses/:courseId without overwriting localStorage and does not collapse on /courses/:courseId/edit', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/libraries') {
          return jsonResponse({ libraries: [] });
        }
        if (url === '/api/v1/auth/me') {
          return jsonResponse({ user: { id: 'u1', username: 'alice', created_at: '', updated_at: '' } });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/']}>
          <LibraryProvider>
            <Routes>
              <Route path="*" element={<Layout />}>
                <Route index element={<ShellLocationProbe />} />
                <Route path="courses/:courseId" element={<ShellLocationProbe />} />
                <Route path="courses/:courseId/edit" element={<ShellLocationProbe />} />
              </Route>
            </Routes>
          </LibraryProvider>
        </MemoryRouter>
      </ThemeProvider>,
    );

    const sidebar = screen.getByRole('complementary');
    expect(sidebar).toHaveAttribute('data-collapsed', 'false');
    expect(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY)).toBeNull();

    const user = userEvent.setup();

    // Enter player route -> transiently collapses without writing localStorage
    await user.click(screen.getByRole('button', { name: 'Go Player' }));
    expect(screen.getByTestId('shell-location')).toHaveTextContent('/courses/c1');
    expect(sidebar).toHaveAttribute('data-collapsed', 'true');
    expect(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY)).toBeNull();

    // Expand transiently on player route -> still does not overwrite localStorage
    await user.click(screen.getByRole('button', { name: '展开侧边栏' }));
    expect(sidebar).toHaveAttribute('data-collapsed', 'false');
    expect(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY)).toBeNull();

    // Navigate back to / -> restores persisted expanded state
    await user.click(screen.getByRole('button', { name: 'Go Home' }));
    expect(screen.getByTestId('shell-location')).toHaveTextContent('/');
    expect(sidebar).toHaveAttribute('data-collapsed', 'false');

    // Navigate to /courses/c1/edit -> does NOT auto-collapse
    await user.click(screen.getByRole('button', { name: 'Go Editor' }));
    expect(screen.getByTestId('shell-location')).toHaveTextContent('/courses/c1/edit');
    expect(sidebar).toHaveAttribute('data-collapsed', 'false');

    // Re-enter player route -> auto-collapses again
    await user.click(screen.getByRole('button', { name: 'Go Player' }));
    expect(sidebar).toHaveAttribute('data-collapsed', 'true');
  });

  it('mounts mobile slide-over drawer on demand and closes on Escape or nav click', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/libraries') {
          return jsonResponse({ libraries: [] });
        }
        if (url === '/api/v1/auth/me') {
          return jsonResponse({ user: { id: 'u1', username: 'alice', created_at: '', updated_at: '' } });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/']}>
          <LibraryProvider>
            <Routes>
              <Route path="*" element={<Layout />}>
                <Route index element={<ShellLocationProbe />} />
                <Route path="storage" element={<ShellLocationProbe />} />
              </Route>
            </Routes>
          </LibraryProvider>
        </MemoryRouter>
      </ThemeProvider>,
    );

    expect(screen.queryByRole('dialog', { name: '导航菜单' })).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '打开导航菜单' }));
    const drawer = await screen.findByRole('dialog', { name: '导航菜单' });
    expect(drawer).toBeInTheDocument();

    // Escape closes and unmounts drawer
    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '导航菜单' })).not.toBeInTheDocument();
    });

    // Re-open and click nav link inside drawer -> navigates and unmounts drawer
    await user.click(screen.getByRole('button', { name: '打开导航菜单' }));
    const reopened = await screen.findByRole('dialog', { name: '导航菜单' });
    await user.click(within(reopened).getByRole('link', { name: '存储与导入' }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '导航菜单' })).not.toBeInTheDocument();
    });
    expect(screen.getByTestId('shell-location')).toHaveTextContent('/storage');
  });

  it('does not call protected APIs when AppRoutes renders /login without a token', async () => {
    localStorage.clear();
    const calledUrls: string[] = [];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        calledUrls.push(url);
        if (url.endsWith('/api/v1/setup/status')) {
          return jsonResponse({ needs_setup: false });
        }
        return jsonResponse({ error: 'unexpected protected call' }, 500);
      }),
    );

    render(
      <ThemeProvider>
        <ToastProvider>
          <UpdateProvider>
            <MemoryRouter initialEntries={['/login']}>
              <AppRoutes />
            </MemoryRouter>
          </UpdateProvider>
        </ToastProvider>
      </ThemeProvider>,
    );

    expect(await screen.findByText(/登录 CourseFin/)).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 30));

    expect(calledUrls).toEqual(['/api/v1/setup/status']);
    expect(calledUrls.some((u) => u.includes('/api/v1/libraries'))).toBe(false);
    expect(calledUrls.some((u) => u.includes('/api/v1/auth/me'))).toBe(false);
    expect(calledUrls.some((u) => u.includes('/api/v1/system/update'))).toBe(false);
  });

  it('provides safe LibraryContext fallback outside provider and supports registerLibrary deduplication & bumpCatalogVersion', async () => {
    function LibraryProbe() {
      const {
        isProvided,
        libraries,
        username,
        catalogVersion,
        registerLibrary,
        bumpCatalogVersion,
      } = useLibrary();
      return (
        <div>
          <span data-testid="lib-provided">{String(isProvided)}</span>
          <span data-testid="lib-username">{username}</span>
          <span data-testid="lib-version">{catalogVersion}</span>
          <span data-testid="lib-names">{libraries.map((l) => `${l.id}:${l.name}:${l.course_count}`).join(',')}</span>
          <button
            type="button"
            onClick={() =>
              registerLibrary(
                makeLibrary({ id: 'lib-ext', name: '扩展库', course_count: 2 }),
              )
            }
          >
            Register Ext
          </button>
          <button type="button" onClick={bumpCatalogVersion}>
            Bump Version
          </button>
        </div>
      );
    }

    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url === '/api/v1/libraries') {
        return jsonResponse({
          libraries: [makeLibrary({ id: 'lib-1', name: '初始库', course_count: 4 })],
        });
      }
      if (url === '/api/v1/auth/me') {
        return jsonResponse({ error: 'unauthorized' }, 401);
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', fetchMock);

    // Outside provider -> safe defaults and zero fetch calls
    const standalone = render(<LibraryProbe />);
    expect(screen.getByTestId('lib-provided')).toHaveTextContent('false');
    expect(screen.getByTestId('lib-username')).toHaveTextContent('管理员');
    expect(screen.getByTestId('lib-version')).toHaveTextContent('0');
    expect(fetchMock).not.toHaveBeenCalled();
    standalone.unmount();

    // Inside provider -> deduplicates registerLibrary and refreshes libraries on bumpCatalogVersion
    let libFetchCount = 0;
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/libraries') {
          libFetchCount += 1;
          return jsonResponse({
            libraries: [
              makeLibrary({
                id: 'lib-1',
                name: '初始库',
                course_count: libFetchCount === 1 ? 1 : 9,
              }),
            ],
          });
        }
        if (url === '/api/v1/auth/me') {
          return jsonResponse({ error: 'failed' }, 500);
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <LibraryProvider>
          <LibraryProbe />
        </LibraryProvider>
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('lib-names')).toHaveTextContent('lib-1:初始库:1');
    });
    // Graceful fallback username when /api/v1/auth/me fails
    expect(screen.getByTestId('lib-username')).toHaveTextContent('管理员');

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Register Ext' }));
    await user.click(screen.getByRole('button', { name: 'Register Ext' }));
    expect(screen.getByTestId('lib-names')).toHaveTextContent('lib-1:初始库:1,lib-ext:扩展库:2');

    await user.click(screen.getByRole('button', { name: 'Bump Version' }));
    expect(screen.getByTestId('lib-version')).toHaveTextContent('1');
    await waitFor(() => {
      expect(screen.getByTestId('lib-names')).toHaveTextContent('lib-1:初始库:9');
    });
  });

  it('keeps AuthenticatedShell mounted without refetching libraries across routes and clears token on logout', async () => {
    let libraryFetchCount = 0;
    let meFetchCount = 0;
    let logoutCalled = false;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/libraries' && method === 'GET') {
          libraryFetchCount += 1;
          return jsonResponse({
            libraries: [makeLibrary({ id: 'lib-dev', name: '编程开发', course_count: 2 })],
          });
        }
        if (url === '/api/v1/auth/me' && method === 'GET') {
          meFetchCount += 1;
          return jsonResponse({ user: { id: 'u1', username: 'alice', created_at: '', updated_at: '' } });
        }
        if (url === '/api/v1/auth/logout' && method === 'POST') {
          logoutCalled = true;
          return jsonResponse({ ok: true });
        }
        if (url === '/api/v1/home') {
          return jsonResponse({ resume: [], next_up: [], libraries: [], courses: [] });
        }
        if (url === '/api/v1/courses') {
          return jsonResponse({ courses: [] });
        }
        if (url === '/api/v1/tags') {
          return jsonResponse({ tags: [] });
        }
        if (url === '/api/v1/sources') {
          return jsonResponse({ sources: [] });
        }
        if (url === '/api/v1/setup/status') {
          return jsonResponse({ needs_setup: false });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ThemeProvider>
        <ToastProvider>
          <MemoryRouter initialEntries={['/']}>
            <AppRoutes />
          </MemoryRouter>
        </ToastProvider>
      </ThemeProvider>,
    );

    expect(await screen.findByText('alice')).toBeInTheDocument();
    expect(meFetchCount).toBe(1);
    const libsAfterHomeMount = libraryFetchCount;

    const user = userEvent.setup();
    await user.click(screen.getByRole('link', { name: '存储与导入' }));
    expect(await screen.findByRole('heading', { name: '存储与导入' })).toBeInTheDocument();

    // Persistent AuthenticatedShell did NOT remount LibraryProvider or re-fetch /api/v1/auth/me
    expect(meFetchCount).toBe(1);
    // StoragePage consumes useLibrary() directly, so navigating to /storage triggers zero redundant fetches
    expect(libraryFetchCount).toBe(libsAfterHomeMount);

    // Logout clears token and redirects to /login
    await user.click(screen.getByRole('button', { name: '退出登录' }));
    await waitFor(() => {
      expect(logoutCalled).toBe(true);
    });
    expect(localStorage.getItem('coursefin_token')).toBeNull();
    expect(await screen.findByText(/登录 CourseFin/)).toBeInTheDocument();
  });

  it('keeps update pill accessible in collapsed sidebar and closes mobile drawer when update link or backdrop is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/system/update') {
          return jsonResponse({
            current_version: '1.0.1',
            latest_version: '1.1.0',
            update_available: true,
            release_name: 'v1.1.0',
            release_notes: '',
            release_url: '',
            published_at: '',
            asset_name: '',
            asset_size: 0,
            deploy_mode: 'docker',
            platform: 'linux/amd64',
            can_apply: true,
            cannot_apply_reason: '',
            auto_check_enabled: true,
            auto_check_locked_by_env: false,
            check_interval_hours: 6,
            mirror_url: '',
            state: 'idle',
            target_version: '',
            progress_bytes: 0,
            total_bytes: 0,
            progress_percent: 0,
            last_checked_at: '',
            last_error: '',
          });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <ThemeProvider>
          <UpdateProvider>
            <MemoryRouter initialEntries={['/']}>
              <Routes>
                <Route path="*" element={<Layout />}>
                  <Route index element={<ShellLocationProbe />} />
                  <Route path="settings" element={<ShellLocationProbe />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </UpdateProvider>
        </ThemeProvider>
      </ToastProvider>,
    );

    // Wait for update pill; mobile drawer is closed so only 1 link matches /新版本 v.../
    const pill = await screen.findByRole('link', { name: /新版本 v1\.1\.0/ });
    expect(pill).toHaveAttribute('href', '/settings#system-update');
    expect(screen.getByTestId('mobile-update-dot')).toBeInTheDocument();

    const user = userEvent.setup();

    // Collapse desktop sidebar -> update pill remains accessible with tooltip
    await user.click(screen.getByRole('button', { name: '收起侧边栏' }));
    const collapsedPill = screen.getByRole('link', { name: /新版本 v1\.1\.0/ });
    await user.hover(collapsedPill);
    await waitFor(() => {
      expect(screen.getByRole('tooltip')).toHaveTextContent(
        '发现新版本 v1.1.0，点击前往设置页查看并更新',
      );
    });

    // Open mobile drawer and click the drawer update link -> navigates and closes drawer
    await user.click(screen.getByRole('button', { name: '打开导航菜单' }));
    const drawer = await screen.findByRole('dialog', { name: '导航菜单' });
    await user.click(within(drawer).getByRole('link', { name: /新版本 v1\.1\.0/ }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '导航菜单' })).not.toBeInTheDocument();
    });
    expect(screen.getByTestId('shell-location')).toHaveTextContent('/settings');
  });

  it('keeps 课程库 active in Layout when landing on a stale /?library=nonexistent-id URL inside LibraryProvider', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/libraries') {
          return jsonResponse({
            libraries: [
              makeLibrary({ id: 'lib-dev', name: '编程开发', sort_order: 1, course_count: 3 }),
              makeLibrary({ id: 'lib-design', name: '设计艺术', sort_order: 2, course_count: 1 }),
            ],
          });
        }
        if (url === '/api/v1/auth/me') {
          return jsonResponse({ user: { id: 'u1', username: 'alice', created_at: '', updated_at: '' } });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <ThemeProvider>
          <MemoryRouter initialEntries={['/?library=nonexistent-id']}>
            <LibraryProvider>
              <Routes>
                <Route path="*" element={<Layout />}>
                  <Route index element={<ShellLocationProbe />} />
                </Route>
              </Routes>
            </LibraryProvider>
          </MemoryRouter>
        </ThemeProvider>
      </ToastProvider>,
    );

    const devLibGroup = await screen.findByRole('group', { name: '媒体库 编程开发' });
    const designLibGroup = screen.getByRole('group', { name: '媒体库 设计艺术' });
    const courseNavLink = screen.getByRole('link', { name: '课程库' });

    await waitFor(() => {
      expect(courseNavLink.className).toContain('bg-accent-soft');
      expect(courseNavLink.className).toContain('font-semibold');
    });
    expect(courseNavLink).toHaveAttribute('aria-current', 'page');
    expect(courseNavLink.querySelector('span[aria-hidden="true"]')?.className).toContain('bg-accent');
    expect(devLibGroup.className).not.toContain('bg-accent-soft');
    expect(designLibGroup.className).not.toContain('bg-accent-soft');

    // Selecting a valid library subsequently shifts active state off 课程库 onto that library
    const user = userEvent.setup();
    await user.click(within(devLibGroup).getByRole('button', { name: /编程开发/ }));
    expect(courseNavLink.className).not.toContain('bg-accent-soft');
    expect(devLibGroup.className).toContain('bg-accent-soft');
  });

  it('renders the CourseFin SVG logo in Layout (sidebar + mobile header) with unique SVG defs IDs and in SetupLoginPage', async () => {
    const { unmount } = render(
      <ToastProvider>
        <ThemeProvider>
          <MemoryRouter initialEntries={['/']}>
            <Layout>
              <div>content</div>
            </Layout>
          </MemoryRouter>
        </ThemeProvider>
      </ToastProvider>,
    );

    const logos = screen.getAllByTestId('coursefin-logo');
    expect(logos.length).toBeGreaterThanOrEqual(2);

    const defIds = logos.flatMap((svg) =>
      Array.from(svg.querySelectorAll('defs [id]')).map((el) => el.getAttribute('id')),
    );
    expect(defIds.length).toBeGreaterThan(0);
    expect(new Set(defIds).size).toBe(defIds.length);
    unmount();

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/setup/status') {
          return jsonResponse({ needs_setup: false });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <ThemeProvider>
          <MemoryRouter initialEntries={['/login']}>
            <AppRoutes />
          </MemoryRouter>
        </ThemeProvider>
      </ToastProvider>,
    );

    expect(await screen.findByText(/登录 CourseFin/)).toBeInTheDocument();
    expect(screen.getByTestId('coursefin-logo')).toBeInTheDocument();
  });
});
