import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ToastProvider } from '../components/Toast';
import StoragePage from '../pages/StoragePage';

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('DirectoryPickerModal & StoragePage', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('opens server directory picker, navigates subdirs, auto-fills path & empty source name, and respects existing custom name', async () => {
    const browseCalls: string[] = [];

    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = init?.method ?? 'GET';

      if (url.endsWith('/api/v1/sources') && method === 'GET') {
        return jsonResponse({ sources: [] });
      }
      if (url.endsWith('/api/v1/libraries') && method === 'GET') {
        return jsonResponse({ libraries: [] });
      }
      if (url.startsWith('/api/v1/fs/dirs') && method === 'GET') {
        browseCalls.push(url);
        const parsed = new URL(url, 'http://localhost');
        const p = parsed.searchParams.get('path') ?? '';
        const showHidden = parsed.searchParams.get('show_hidden') === 'true';

        if (!p || p === '/Users/finn') {
          const entries = [
            { name: 'Courses', path: '/Users/finn/Courses' },
            { name: 'Movies', path: '/Users/finn/Movies' },
          ];
          if (showHidden) {
            entries.push({ name: '.secret', path: '/Users/finn/.secret' });
          }
          return jsonResponse({
            current_path: '/Users/finn',
            parent_path: '/Users',
            entries,
            roots: [
              { label: '主目录', path: '/Users/finn' },
              { label: '根目录 (/)', path: '/' },
            ],
          });
        }
        if (p === '/Users/finn/Courses') {
          return jsonResponse({
            current_path: '/Users/finn/Courses',
            parent_path: '/Users/finn',
            entries: [{ name: 'Go实战', path: '/Users/finn/Courses/Go实战' }],
            roots: [
              { label: '主目录', path: '/Users/finn' },
              { label: '根目录 (/)', path: '/' },
            ],
          });
        }
        return jsonResponse({ error: '目录不存在' }, 404);
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    render(
      <ToastProvider>
        <MemoryRouter>
          <StoragePage />
        </MemoryRouter>
      </ToastProvider>,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /添加存储源/ }));

    // Open directory picker
    const browseBtn = screen.getByRole('button', { name: /浏览目录/ });
    await user.hover(browseBtn);
    await waitFor(() => {
      expect(screen.getByRole('tooltip')).toBeInTheDocument();
    });
    await user.click(browseBtn);

    const dialog = await screen.findByRole('dialog', { name: /选择本地目录/ });
    expect(await within(dialog).findByText('Courses')).toBeInTheDocument();
    expect(within(dialog).getByText('Movies')).toBeInTheDocument();
    expect(within(dialog).queryByText('.secret')).not.toBeInTheDocument();

    // Toggle show hidden directories
    await user.click(within(dialog).getByLabelText(/显示隐藏目录/));
    expect(await within(dialog).findByText('.secret')).toBeInTheDocument();

    // Filter subdirectories
    const filterInput = within(dialog).getByPlaceholderText(/筛选当前子目录/);
    await user.type(filterInput, 'Cour');
    expect(within(dialog).getByText('Courses')).toBeInTheDocument();
    expect(within(dialog).queryByText('Movies')).not.toBeInTheDocument();

    // Enter Courses directory
    await user.click(within(dialog).getByRole('button', { name: /Courses/ }));
    expect(await within(dialog).findByText('Go实战')).toBeInTheDocument();

    // Confirm selection
    await user.click(within(dialog).getByRole('button', { name: '选择此目录' }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
    expect(screen.getByLabelText('本地目录绝对路径')).toHaveValue('/Users/finn/Courses');
    expect(screen.getByLabelText('存储源名称')).toHaveValue('Courses');
    expect(screen.getByRole('status')).toHaveTextContent(/已选择目录/);

    // Now set a custom name and pick again -> custom name must be preserved
    const nameInput = screen.getByLabelText('存储源名称');
    await user.clear(nameInput);
    await user.type(nameInput, '我的自定义课程盘');

    await user.click(screen.getByRole('button', { name: /浏览目录/ }));
    const dialog2 = await screen.findByRole('dialog', { name: /选择本地目录/ });
    await within(dialog2).findByText('Go实战');
    await user.click(within(dialog2).getByRole('button', { name: /返回上级/ }));
    await within(dialog2).findByText('Movies');
    await user.click(within(dialog2).getByRole('button', { name: '选择此目录' }));

    expect(screen.getByLabelText('本地目录绝对路径')).toHaveValue('/Users/finn');
    expect(screen.getByLabelText('存储源名称')).toHaveValue('我的自定义课程盘');
    expect(browseCalls.some((u) => u.includes('show_hidden=true'))).toBe(true);
  });

  it('falls back to server default path with notice when pre-filled path does not exist', async () => {
    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = init?.method ?? 'GET';
      if (url.endsWith('/api/v1/sources') && method === 'GET') {
        return jsonResponse({ sources: [] });
      }
      if (url.endsWith('/api/v1/libraries') && method === 'GET') {
        return jsonResponse({ libraries: [] });
      }
      if (url.startsWith('/api/v1/fs/dirs') && method === 'GET') {
        const parsed = new URL(url, 'http://localhost');
        const p = parsed.searchParams.get('path') ?? '';
        if (p === '/typo/not/exist') {
          return jsonResponse({ error: '目录不存在' }, 404);
        }
        return jsonResponse({
          current_path: '/media',
          parent_path: '/',
          entries: [{ name: 'courses', path: '/media/courses' }],
          roots: [{ label: '媒体目录 (/media)', path: '/media' }],
        });
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    render(
      <ToastProvider>
        <MemoryRouter>
          <StoragePage />
        </MemoryRouter>
      </ToastProvider>,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /添加存储源/ }));
    await user.type(screen.getByLabelText('本地目录绝对路径'), '/typo/not/exist');
    await user.click(screen.getByRole('button', { name: /浏览目录/ }));

    const dialog = await screen.findByRole('dialog', { name: /选择本地目录/ });
    expect(await within(dialog).findByText('courses')).toBeInTheDocument();
    expect(within(dialog).getByText(/已回退到默认目录/)).toBeInTheDocument();
  });

  it('shows toast feedback when creating and deleting a storage source', async () => {
    let sources: unknown[] = [];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url.endsWith('/api/v1/sources') && method === 'GET') {
          return jsonResponse({ sources });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({ libraries: [] });
        }
        if (url.endsWith('/api/v1/sources') && method === 'POST') {
          const created = {
            id: 'src-new',
            name: '硬盘A',
            driver: 'local',
            root_path: '/media/a',
            config_json: '{}',
            enabled: true,
            media_count: 0,
            created_at: '2026-11-24T00:00:00Z',
            updated_at: '2026-11-24T00:00:00Z',
          };
          sources = [created];
          return jsonResponse(created, 201);
        }
        if (url.includes('/api/v1/sources/src-new/dirs')) {
          return jsonResponse({ dirs: [] });
        }
        if (url.includes('/api/v1/sources/src-new/files')) {
          return jsonResponse({ files: [] });
        }
        if (url.endsWith('/api/v1/sources/src-new') && method === 'DELETE') {
          sources = [];
          return jsonResponse({ ok: true });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <MemoryRouter>
          <StoragePage />
        </MemoryRouter>
      </ToastProvider>,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /添加存储源/ }));
    await user.type(screen.getByLabelText('存储源名称'), '硬盘A');
    await user.type(screen.getByLabelText('本地目录绝对路径'), '/media/a');
    await user.click(screen.getByRole('button', { name: '确认添加' }));

    expect(await screen.findByText(/已添加存储源「硬盘A」/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '删除存储源 硬盘A' }));
    expect(await screen.findByText(/已删除存储源/)).toBeInTheDocument();
  });
});
