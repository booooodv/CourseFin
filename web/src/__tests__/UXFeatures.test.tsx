import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ToastProvider } from '../components/Toast';
import Layout from '../components/Layout';
import DirectoryPickerModal from '../components/DirectoryPickerModal';
import { LibraryProvider } from '../contexts/LibraryContext';
import HomePage from '../pages/HomePage';
import SettingsPage from '../pages/SettingsPage';
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

  it('clears stale DirectoryPickerModal data on re-open when next fetch fails', async () => {
    let shouldFail = false;
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        if (shouldFail) {
          return jsonResponse({ error: '服务器暂时不可用' }, 500);
        }
        return jsonResponse({
          current_path: '/media/courses',
          parent_path: '/media',
          entries: [{ name: 'StaleFolder', path: '/media/courses/StaleFolder' }],
          roots: [{ label: '媒体目录 (/media)', path: '/media' }],
        });
      }),
    );

    const onClose = vi.fn();
    const onSelect = vi.fn();
    const { rerender } = render(
      <DirectoryPickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />,
    );

    const dialog = await screen.findByRole('dialog', { name: /选择本地目录/ });
    expect(await within(dialog).findByText('StaleFolder')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: '选择此目录' })).toBeEnabled();

    // Close modal, then re-open with failing request
    rerender(<DirectoryPickerModal isOpen={false} onClose={onClose} onSelect={onSelect} />);
    shouldFail = true;
    rerender(<DirectoryPickerModal isOpen={true} onClose={onClose} onSelect={onSelect} />);

    const dialog2 = await screen.findByRole('dialog', { name: /选择本地目录/ });
    expect(await within(dialog2).findByText('服务器暂时不可用')).toBeInTheDocument();
    expect(within(dialog2).queryByText('StaleFolder')).not.toBeInTheDocument();
    expect(within(dialog2).getByRole('button', { name: '选择此目录' })).toBeDisabled();
  });

  it('syncs newly created libraries and imported courses from ImportCourseModal to LibraryProvider sidebar', async () => {
    let libraries = [
      {
        id: 'lib-1',
        name: '基础课程',
        cover_path: '',
        sort_order: 1,
        course_count: 2,
        created_at: '2026-11-24T00:00:00Z',
        updated_at: '2026-11-24T00:00:00Z',
      },
    ];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/auth/me') && method === 'GET') {
          return jsonResponse({
            user: { id: 'u1', username: 'admin', created_at: '', updated_at: '' },
            session: {
              token: 'test-token',
              user_id: 'u1',
              device_id: 'web',
              device_name: 'Web',
              client_name: 'CourseFin Web',
              client_version: '0.1.0',
              last_active_at: '',
              created_at: '',
            },
          });
        }
        if (url.endsWith('/api/v1/sources') && method === 'GET') {
          return jsonResponse({
            sources: [
              {
                id: 'src-1',
                name: '主存储盘',
                driver: 'local',
                root_path: '/data/courses',
                config_json: '{}',
                enabled: true,
                media_count: 3,
                created_at: '2026-11-24T00:00:00Z',
                updated_at: '2026-11-24T00:00:00Z',
              },
            ],
          });
        }
        if (url.endsWith('/api/v1/sources/src-1/scan/status') && method === 'GET') {
          return jsonResponse({ source_id: 'src-1', running: false });
        }
        if (url.includes('/api/v1/sources/src-1/dirs') && method === 'GET') {
          return jsonResponse({
            dirs: [
              {
                rel_path: 'rust-course',
                name: 'rust-course',
                file_count: 3,
                direct_files: 3,
                has_subdirs: false,
              },
            ],
          });
        }
        if (url.includes('/api/v1/sources/src-1/files') && method === 'GET') {
          return jsonResponse({ files: [] });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({ libraries });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'POST') {
          const body = JSON.parse(String(init?.body)) as { name: string };
          const createdLib = {
            id: 'lib-rust',
            name: body.name,
            cover_path: '',
            sort_order: 2,
            course_count: 0,
            created_at: '2026-11-24T00:00:00Z',
            updated_at: '2026-11-24T00:00:00Z',
          };
          libraries = [...libraries, createdLib];
          return jsonResponse(createdLib, 201);
        }
        if (url.endsWith('/api/v1/import/preview') && method === 'POST') {
          return jsonResponse({
            source_id: 'src-1',
            dir_rel_path: 'rust-course',
            course_title: 'rust-course',
            total_files: 1,
            chapters: [
              {
                title: '正片',
                sort_order: 1,
                lessons: [
                  {
                    media_file_id: 'mf-1',
                    relative_path: 'rust-course/01.mp4',
                    file_name: '01.mp4',
                    title: '01',
                    duration_ms: 120000,
                    sort_order: 1,
                  },
                ],
              },
            ],
          });
        }
        if (url.endsWith('/api/v1/import/commit') && method === 'POST') {
          const body = JSON.parse(String(init?.body)) as { library_id: string; title: string };
          libraries = libraries.map((lib) =>
            lib.id === body.library_id ? { ...lib, course_count: lib.course_count + 1 } : lib,
          );
          return jsonResponse(
            {
              id: 'course-rust',
              library_id: body.library_id,
              title: body.title,
              sort_title: body.title,
              overview: '',
              instructor: '',
              cover_path: '',
              backdrop_path: '',
              source_id: 'src-1',
              source_root_path: '/data/courses',
              tags: [],
              chapter_count: 1,
              lesson_count: 1,
              played_count: 0,
              unplayed_item_count: 1,
              played_percentage: 0,
              played: false,
              total_duration_ms: 120000,
              first_lesson_id: 'lesson-1',
              created_at: '2026-11-24T00:00:00Z',
              updated_at: '2026-11-24T00:00:00Z',
              chapters: [],
            },
            201,
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <LibraryProvider>
          <MemoryRouter initialEntries={['/storage']}>
            <Layout>
              <StoragePage />
            </Layout>
          </MemoryRouter>
        </LibraryProvider>
      </ToastProvider>,
    );

    const sidebar = screen.getByRole('complementary', { name: '主侧边栏' });
    expect(await within(sidebar).findByRole('group', { name: '媒体库 基础课程' })).toBeInTheDocument();
    expect(await screen.findByText('rust-course')).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /一键导入为课程/ }));

    const dialog = await screen.findByRole('dialog', { name: /导入为课程/ });
    await within(dialog).findByLabelText(/课程标题/);

    // Create a new library inline inside ImportCourseModal
    await user.click(within(dialog).getByRole('button', { name: '新建媒体库' }));
    await user.type(within(dialog).getByPlaceholderText('新媒体库名称'), 'Rust 专题');
    await user.click(within(dialog).getByRole('button', { name: '创建' }));

    // Immediately reflected in the left sidebar via LibraryContext.registerLibrary
    const newLibGroup = await within(sidebar).findByRole('group', { name: '媒体库 Rust 专题' });
    expect(within(newLibGroup).getByText('0')).toBeInTheDocument();

    // Commit import and verify sidebar course count refreshes via bumpCatalogVersion
    await user.click(within(dialog).getByRole('button', { name: /确认导入/ }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
    const mainArea = screen.getByRole('main');
    expect(await within(mainArea).findByText(/已成功导入课程「rust-course」/)).toBeInTheDocument();
    await waitFor(() => {
      expect(within(newLibGroup).getByText('1')).toBeInTheDocument();
    });

    // Dual-theme check: no legacy dark-only palette classes in StoragePage main content
    expect(mainArea.innerHTML).not.toMatch(
      /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald|amber|sky)-/,
    );
  });

  it('closes ImportCourseModal and DirectoryPickerModal on Escape and uses semantic dual-theme classes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url.endsWith('/api/v1/sources') && method === 'GET') {
          return jsonResponse({
            sources: [
              {
                id: 'src-1',
                name: '主存储盘',
                driver: 'local',
                root_path: '/data/courses',
                config_json: '{}',
                enabled: true,
                media_count: 1,
                created_at: '2026-11-24T00:00:00Z',
                updated_at: '2026-11-24T00:00:00Z',
              },
            ],
          });
        }
        if (url.endsWith('/api/v1/sources/src-1/scan/status') && method === 'GET') {
          return jsonResponse({ source_id: 'src-1', running: false });
        }
        if (url.includes('/api/v1/sources/src-1/dirs') && method === 'GET') {
          return jsonResponse({
            dirs: [
              {
                rel_path: 'ts-course',
                name: 'ts-course',
                file_count: 1,
                direct_files: 1,
                has_subdirs: false,
              },
            ],
          });
        }
        if (url.includes('/api/v1/sources/src-1/files') && method === 'GET') {
          return jsonResponse({ files: [] });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({ libraries: [] });
        }
        if (url.startsWith('/api/v1/fs/dirs') && method === 'GET') {
          return jsonResponse({
            current_path: '/data',
            parent_path: '/',
            entries: [{ name: 'courses', path: '/data/courses' }],
            roots: [{ label: '根目录 (/)', path: '/' }],
          });
        }
        if (url.endsWith('/api/v1/import/preview') && method === 'POST') {
          return jsonResponse({
            source_id: 'src-1',
            dir_rel_path: 'ts-course',
            course_title: 'ts-course',
            total_files: 1,
            chapters: [],
          });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const { container } = render(
      <ToastProvider>
        <MemoryRouter>
          <StoragePage />
        </MemoryRouter>
      </ToastProvider>,
    );

    expect(await screen.findByText('ts-course')).toBeInTheDocument();
    const user = userEvent.setup();

    // Open DirectoryPickerModal and verify semantic classes + Escape close
    await user.click(screen.getByRole('button', { name: /添加存储源/ }));
    await user.click(screen.getByRole('button', { name: /浏览目录/ }));
    const dirDialog = await screen.findByRole('dialog', { name: /选择本地目录/ });
    expect(dirDialog.className).toContain('bg-overlay');
    expect(container.innerHTML).not.toMatch(
      /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald|amber|sky)-/,
    );
    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: /选择本地目录/ })).not.toBeInTheDocument();
    });

    // Open ImportCourseModal and verify semantic classes + Escape close
    await user.click(screen.getByRole('button', { name: /一键导入为课程/ }));
    const importDialog = await screen.findByRole('dialog', { name: /导入为课程/ });
    await within(importDialog).findByLabelText(/课程标题/);
    expect(importDialog.className).toContain('bg-overlay');
    expect(container.innerHTML).not.toMatch(
      /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald|amber|sky)-/,
    );
    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: /导入为课程/ })).not.toBeInTheDocument();
    });
  });
});

describe('HomePage library delete & rename', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  function makeCourse(id: string, libraryId: string, title: string) {
    return {
      id,
      library_id: libraryId,
      title,
      sort_title: title,
      overview: '',
      instructor: '',
      cover_path: '',
      backdrop_path: '',
      tags: [],
      chapter_count: 1,
      lesson_count: 2,
      played_count: 0,
      unplayed_item_count: 2,
      played_percentage: 0,
      played: false,
      total_duration_ms: 600000,
      first_lesson_id: '',
      created_at: '2026-11-24T00:00:00Z',
      updated_at: '2026-11-24T00:00:00Z',
    };
  }

  it('deletes an empty library, resets active selection to all, and shows toast', async () => {
    const deleteUrls: string[] = [];
    let libraries = [
      {
        id: 'lib-empty',
        name: '临时空库',
        cover_path: '',
        sort_order: 1,
        course_count: 0,
        created_at: '2026-11-24T00:00:00Z',
        updated_at: '2026-11-24T00:00:00Z',
      },
    ];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({ resume: [], next_up: [], libraries, courses: [] });
        }
        if (url.endsWith('/api/v1/courses')) {
          return jsonResponse({ courses: [] });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({ libraries });
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({ tags: [] });
        }
        if (url.startsWith('/api/v1/libraries/lib-empty') && method === 'DELETE') {
          deleteUrls.push(url);
          libraries = [];
          return jsonResponse({ ok: true });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <MemoryRouter>
          <HomePage />
        </MemoryRouter>
      </ToastProvider>,
    );

    const user = userEvent.setup();
    const libTabBtn = await screen.findByRole('button', { name: /临时空库/ });
    await user.click(libTabBtn);

    const group = screen.getByRole('group', { name: '媒体库 临时空库' });
    await user.click(within(group).getByRole('button', { name: '删除媒体库' }));

    const dialog = await screen.findByRole('dialog', { name: /删除媒体库/ });
    expect(within(dialog).getByText(/临时空库/)).toBeInTheDocument();
    await user.click(within(dialog).getByRole('button', { name: '确认删除' }));

    await waitFor(() => {
      expect(deleteUrls).toEqual(['/api/v1/libraries/lib-empty']);
    });
    await waitFor(() => {
      expect(screen.queryByRole('group', { name: '媒体库 临时空库' })).not.toBeInTheDocument();
    });
    expect(screen.getByText(/已删除媒体库「临时空库」/)).toBeInTheDocument();
  });

  it('migrates courses to another library when deleting a non-empty library', async () => {
    const deleteUrls: string[] = [];
    let libraries = [
      {
        id: 'lib-a',
        name: '前端开发',
        cover_path: '',
        sort_order: 1,
        course_count: 1,
        created_at: '2026-11-24T00:00:00Z',
        updated_at: '2026-11-24T00:00:00Z',
      },
      {
        id: 'lib-b',
        name: '全栈工程',
        cover_path: '',
        sort_order: 2,
        course_count: 0,
        created_at: '2026-11-24T00:00:00Z',
        updated_at: '2026-11-24T00:00:00Z',
      },
    ];
    let courses = [makeCourse('c-1', 'lib-a', 'React 19 课程')];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({ resume: [], next_up: [], libraries, courses });
        }
        if (url.endsWith('/api/v1/courses')) {
          return jsonResponse({ courses });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({ libraries });
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({ tags: [] });
        }
        if (url.startsWith('/api/v1/libraries/lib-a') && method === 'DELETE') {
          deleteUrls.push(url);
          libraries = [{ ...libraries[1], course_count: 1 }];
          courses = [{ ...courses[0], library_id: 'lib-b' }];
          return jsonResponse({ ok: true });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <MemoryRouter>
          <HomePage />
        </MemoryRouter>
      </ToastProvider>,
    );

    const user = userEvent.setup();
    await screen.findByRole('link', { name: 'React 19 课程' });

    const group = screen.getByRole('group', { name: '媒体库 前端开发' });
    await user.click(within(group).getByRole('button', { name: '删除媒体库' }));

    const dialog = await screen.findByRole('dialog', { name: /删除媒体库/ });
    const moveRadio = within(dialog).getByLabelText(/迁移课程到其他媒体库并删除/);
    expect(moveRadio).toBeChecked();
    expect(within(dialog).getByLabelText('迁移目标媒体库')).toHaveValue('lib-b');

    await user.click(within(dialog).getByRole('button', { name: '迁移并删除' }));

    await waitFor(() => {
      expect(deleteUrls).toEqual(['/api/v1/libraries/lib-a?move_to_library_id=lib-b']);
    });
    expect(await screen.findByText(/已将课程迁移至「全栈工程」并删除媒体库「前端开发」/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'React 19 课程' })).toBeInTheDocument();
  });

  it('disables migrate option when deleting the only non-empty library and supports force delete', async () => {
    const deleteUrls: string[] = [];
    let libraries = [
      {
        id: 'lib-only',
        name: '唯一库',
        cover_path: '',
        sort_order: 1,
        course_count: 1,
        created_at: '2026-11-24T00:00:00Z',
        updated_at: '2026-11-24T00:00:00Z',
      },
    ];
    let courses = [makeCourse('c-only', 'lib-only', '唯一课程')];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({ resume: [], next_up: [], libraries, courses });
        }
        if (url.endsWith('/api/v1/courses')) {
          return jsonResponse({ courses });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({ libraries });
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({ tags: [] });
        }
        if (url.startsWith('/api/v1/libraries/lib-only') && method === 'DELETE') {
          deleteUrls.push(url);
          libraries = [];
          courses = [];
          return jsonResponse({ ok: true });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <MemoryRouter>
          <HomePage />
        </MemoryRouter>
      </ToastProvider>,
    );

    const user = userEvent.setup();
    await screen.findByRole('link', { name: '唯一课程' });

    const group = screen.getByRole('group', { name: '媒体库 唯一库' });
    await user.click(within(group).getByRole('button', { name: '删除媒体库' }));

    const dialog = await screen.findByRole('dialog', { name: /删除媒体库/ });
    expect(within(dialog).getByLabelText(/迁移课程到其他媒体库并删除/)).toBeDisabled();
    const forceRadio = within(dialog).getByLabelText(/连带删除这 1 门课程记录/);
    expect(forceRadio).toBeChecked();

    await user.click(within(dialog).getByRole('button', { name: '确认删除' }));

    await waitFor(() => {
      expect(deleteUrls).toEqual(['/api/v1/libraries/lib-only?force=true']);
    });
    expect(await screen.findByText(/已删除媒体库「唯一库」/)).toBeInTheDocument();
  });

  it('renames a library inline via PUT /api/v1/libraries/:id', async () => {
    const renameBodies: unknown[] = [];
    const libraries = [
      {
        id: 'lib-1',
        name: '旧库名',
        cover_path: '',
        sort_order: 1,
        course_count: 0,
        created_at: '2026-11-24T00:00:00Z',
        updated_at: '2026-11-24T00:00:00Z',
      },
    ];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({ resume: [], next_up: [], libraries, courses: [] });
        }
        if (url.endsWith('/api/v1/courses')) {
          return jsonResponse({ courses: [] });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({ libraries });
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({ tags: [] });
        }
        if (url.endsWith('/api/v1/libraries/lib-1') && method === 'PUT') {
          const body = JSON.parse(String(init?.body));
          renameBodies.push(body);
          return jsonResponse({ ...libraries[0], name: body.name });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <MemoryRouter>
          <HomePage />
        </MemoryRouter>
      </ToastProvider>,
    );

    const user = userEvent.setup();
    await screen.findByRole('button', { name: /旧库名/ });

    const group = screen.getByRole('group', { name: '媒体库 旧库名' });
    await user.click(within(group).getByRole('button', { name: '重命名媒体库' }));

    const input = screen.getByLabelText('重命名媒体库名称');
    expect(input).toHaveValue('旧库名');
    await user.clear(input);
    await user.type(input, '新库名');
    await user.click(screen.getByRole('button', { name: '保存名称' }));

    await waitFor(() => {
      expect(renameBodies).toEqual([{ name: '新库名' }]);
    });
    expect(await screen.findByRole('button', { name: /新库名/ })).toBeInTheDocument();
    expect(screen.getByText(/已将媒体库重命名为「新库名」/)).toBeInTheDocument();
  });
});

describe('SettingsPage copy fallback & tooltips', () => {
  const originalClipboard = navigator.clipboard;
  const originalExecCommand = document.execCommand;
  const originalIsSecureContext = window.isSecureContext;

  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse({
            server_name: 'CourseFin',
            scan_interval_minutes: 60,
            base_url_hint: 'http://192.168.1.20:8096',
            jellyfin_username: 'admin',
            ai_base_url: '',
            ai_api_key: '',
            ai_api_key_set: false,
            ai_model: '',
            ai_max_subtitle_chars: 4000,
          });
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse({
            user: { id: 'u1', username: 'admin', created_at: '', updated_at: '' },
            session: {
              token: 'test-token',
              user_id: 'u1',
              device_id: 'web',
              device_name: 'Web',
              client_name: 'CourseFin Web',
              client_version: '0.1.0',
              last_active_at: '',
              created_at: '',
            },
          });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );
  });

  afterEach(() => {
    localStorage.clear();
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      configurable: true,
    });
    Object.defineProperty(window, 'isSecureContext', {
      value: originalIsSecureContext,
      configurable: true,
    });
    document.execCommand = originalExecCommand;
    vi.restoreAllMocks();
  });

  it('copies Server URL over plain HTTP fallback with tooltip and success/error toasts, and copies username', async () => {
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true });
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
    document.execCommand = vi.fn().mockReturnValue(true);

    render(
      <ToastProvider>
        <MemoryRouter>
          <SettingsPage />
        </MemoryRouter>
      </ToastProvider>,
    );

    await screen.findByText('http://192.168.1.20:8096');

    const user = userEvent.setup();
    const copyUrlBtn = screen.getByRole('button', { name: '复制服务器地址' });

    // Hovering copy button reveals tooltip guidance
    await user.hover(copyUrlBtn);
    await waitFor(() => {
      expect(screen.getByRole('tooltip')).toHaveTextContent(/复制/);
    });

    // Clicking copy succeeds via execCommand fallback and shows toast
    await user.click(copyUrlBtn);
    expect(await screen.findByText('已复制服务器地址到剪贴板')).toBeInTheDocument();
    expect(copyUrlBtn).toHaveTextContent('已复制');

    // Copy username button works and shows toast
    const copyUserBtn = screen.getByRole('button', { name: '复制用户名' });
    await user.click(copyUserBtn);
    expect(await screen.findByText('已复制用户名到剪贴板')).toBeInTheDocument();

    // When both clipboard & execCommand fail, show explicit error toast
    document.execCommand = vi.fn().mockReturnValue(false);
    await user.click(copyUrlBtn);
    expect(await screen.findByText('复制失败，请手动选中复制')).toBeInTheDocument();
  });
});
