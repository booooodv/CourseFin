import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { api, itemPrimaryImageUrl, withApiKey } from '../api/client';
import Layout from '../components/Layout';
import { ToastProvider } from '../components/Toast';
import { LibraryProvider } from '../contexts/LibraryContext';
import { ThemeProvider } from '../contexts/ThemeContext';
import HomePage from '../pages/HomePage';
import SetupLoginPage from '../pages/SetupLoginPage';

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('HomePage', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token-123');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('renders continue watching rail and filters courses by library, tag, and search query', async () => {
    const mockFetch = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v1/home')) {
        return jsonResponse({
          resume: [
            {
              id: 'lesson-1',
              course_id: 'course-go',
              chapter_id: 'chap-1',
              media_file_id: 'mf-1',
              title: '第 1 节：并发原语',
              overview: '',
              sort_order: 1,
              cover_path: '',
              duration_ms: 600000,
              container: 'mp4',
              video_codec: 'h264',
              audio_codec: 'aac',
              width: 1920,
              height: 1080,
              bitrate: 2000000,
              thumbnail_path: '',
              media_status: 'active',
              position_ticks: 300000 * 10000, // 50%
              played: false,
              play_count: 0,
              created_at: '2026-11-20T00:00:00Z',
              updated_at: '2026-11-20T00:00:00Z',
              course_title: 'Go 语言进阶实战',
              course_cover_path: 'cover.jpg',
              library_id: 'lib-dev',
              chapter_title: '第一章',
              chapter_sort_order: 1,
            },
          ],
          next_up: [],
          libraries: [
            {
              id: 'lib-dev',
              name: '编程开发',
              cover_path: '',
              sort_order: 1,
              course_count: 2,
              created_at: '2026-11-20T00:00:00Z',
              updated_at: '2026-11-20T00:00:00Z',
            },
            {
              id: 'lib-design',
              name: '设计艺术',
              cover_path: '',
              sort_order: 2,
              course_count: 1,
              created_at: '2026-11-20T00:00:00Z',
              updated_at: '2026-11-20T00:00:00Z',
            },
          ],
          courses: [
            {
              id: 'course-go',
              library_id: 'lib-dev',
              title: 'Go 语言进阶实战',
              sort_title: 'Go 语言进阶实战',
              overview: '深入 Go 并发模型与工程实践',
              instructor: '张老师',
              cover_path: 'go.jpg',
              backdrop_path: '',
              tags: ['Go', '后端'],
              chapter_count: 3,
              lesson_count: 10,
              played_count: 4,
              unplayed_item_count: 6,
              played_percentage: 40,
              played: false,
              total_duration_ms: 3600000,
              first_lesson_id: 'lesson-1',
              created_at: '2026-11-20T00:00:00Z',
              updated_at: '2026-11-20T00:00:00Z',
            },
            {
              id: 'course-react',
              library_id: 'lib-dev',
              title: 'React 19 架构精讲',
              sort_title: 'React 19 架构精讲',
              overview: '现代前端状态与渲染优化',
              instructor: '李老师',
              cover_path: '',
              backdrop_path: '',
              tags: ['React', '前端'],
              chapter_count: 2,
              lesson_count: 8,
              played_count: 0,
              unplayed_item_count: 8,
              played_percentage: 0,
              played: false,
              total_duration_ms: 2400000,
              first_lesson_id: 'lesson-2',
              created_at: '2026-11-21T00:00:00Z',
              updated_at: '2026-11-21T00:00:00Z',
            },
            {
              id: 'course-figma',
              library_id: 'lib-design',
              title: 'Figma 设计系统全解',
              sort_title: 'Figma 设计系统全解',
              overview: '从零构建企业级设计规范',
              instructor: '王老师',
              cover_path: '',
              backdrop_path: '',
              tags: ['UI设计'],
              chapter_count: 4,
              lesson_count: 12,
              played_count: 12,
              unplayed_item_count: 0,
              played_percentage: 100,
              played: true,
              total_duration_ms: 4800000,
              first_lesson_id: 'lesson-3',
              created_at: '2026-11-22T00:00:00Z',
              updated_at: '2026-11-22T00:00:00Z',
            },
          ],
        });
      }
      if (url.endsWith('/api/v1/courses')) {
        return jsonResponse({
          courses: [
            {
              id: 'course-go',
              library_id: 'lib-dev',
              title: 'Go 语言进阶实战',
              sort_title: 'Go 语言进阶实战',
              overview: '深入 Go 并发模型与工程实践',
              instructor: '张老师',
              cover_path: 'go.jpg',
              backdrop_path: '',
              tags: ['Go', '后端'],
              chapter_count: 3,
              lesson_count: 10,
              played_count: 4,
              unplayed_item_count: 6,
              played_percentage: 40,
              played: false,
              total_duration_ms: 3600000,
              first_lesson_id: 'lesson-1',
              created_at: '2026-11-20T00:00:00Z',
              updated_at: '2026-11-20T00:00:00Z',
            },
            {
              id: 'course-react',
              library_id: 'lib-dev',
              title: 'React 19 架构精讲',
              sort_title: 'React 19 架构精讲',
              overview: '现代前端状态与渲染优化',
              instructor: '李老师',
              cover_path: '',
              backdrop_path: '',
              tags: ['React', '前端'],
              chapter_count: 2,
              lesson_count: 8,
              played_count: 0,
              unplayed_item_count: 8,
              played_percentage: 0,
              played: false,
              total_duration_ms: 2400000,
              first_lesson_id: 'lesson-2',
              created_at: '2026-11-21T00:00:00Z',
              updated_at: '2026-11-21T00:00:00Z',
            },
            {
              id: 'course-figma',
              library_id: 'lib-design',
              title: 'Figma 设计系统全解',
              sort_title: 'Figma 设计系统全解',
              overview: '从零构建企业级设计规范',
              instructor: '王老师',
              cover_path: '',
              backdrop_path: '',
              tags: ['UI设计'],
              chapter_count: 4,
              lesson_count: 12,
              played_count: 12,
              unplayed_item_count: 0,
              played_percentage: 100,
              played: true,
              total_duration_ms: 4800000,
              first_lesson_id: 'lesson-3',
              created_at: '2026-11-22T00:00:00Z',
              updated_at: '2026-11-22T00:00:00Z',
            },
          ],
        });
      }
      if (url.endsWith('/api/v1/libraries')) {
        return jsonResponse({
          libraries: [
            {
              id: 'lib-dev',
              name: '编程开发',
              cover_path: '',
              sort_order: 1,
              course_count: 2,
              created_at: '2026-11-20T00:00:00Z',
              updated_at: '2026-11-20T00:00:00Z',
            },
            {
              id: 'lib-design',
              name: '设计艺术',
              cover_path: '',
              sort_order: 2,
              course_count: 1,
              created_at: '2026-11-20T00:00:00Z',
              updated_at: '2026-11-20T00:00:00Z',
            },
          ],
        });
      }
      if (url.endsWith('/api/v1/tags')) {
        return jsonResponse({
          tags: [
            { id: 't1', name: 'Go', course_count: 1 },
            { id: 't2', name: 'React', course_count: 1 },
            { id: 't3', name: 'UI设计', course_count: 1 },
          ],
        });
      }
      return jsonResponse({ error: 'not found' }, 404);
    });

    vi.stubGlobal('fetch', mockFetch);

    render(
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>,
    );

    // Continue watching rail renders
    expect(await screen.findByText('继续学习')).toBeInTheDocument();
    expect(screen.getByText('第 1 节：并发原语')).toBeInTheDocument();
    expect(screen.getByText('50%')).toBeInTheDocument();

    const resumeLink = screen.getByRole('link', { name: /第 1 节：并发原语/ });
    expect(resumeLink).toHaveAttribute('href', '/courses/course-go?lesson=lesson-1');

    // All 3 courses are visible initially (Go title appears in rail + grid)
    expect(screen.getAllByText('Go 语言进阶实战').length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText('React 19 架构精讲')).toBeInTheDocument();
    expect(screen.getByText('Figma 设计系统全解')).toBeInTheDocument();

    const user = userEvent.setup();

    // Filter by library tab: 编程开发
    await user.click(screen.getByRole('button', { name: /编程开发/ }));
    expect(screen.getByText('React 19 架构精讲')).toBeInTheDocument();
    expect(screen.queryByText('Figma 设计系统全解')).not.toBeInTheDocument();

    // Filter by tag chip: Go
    await user.click(screen.getByRole('button', { name: /^Go/ }));
    expect(screen.queryByText('React 19 架构精讲')).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: '编辑 Go 语言进阶实战' })).toBeInTheDocument();

    // Verify Authorization header was sent on protected endpoints
    expect(mockFetch).toHaveBeenCalled();
    const firstCallInit = mockFetch.mock.calls[0]?.[1];
    const headers = new Headers(firstCallInit?.headers);
    expect(headers.get('Authorization')).toBe('Bearer test-token-123');

    // Reset library & tag, then filter by search query
    await user.click(screen.getByRole('button', { name: /全部/ }));
    await user.click(screen.getByRole('button', { name: /^Go/ })); // toggle tag off
    await waitFor(() => {
      expect(screen.getByText('Figma 设计系统全解')).toBeInTheDocument();
    });

    const searchInput = screen.getByPlaceholderText(/搜索课程/);
    await user.type(searchInput, 'Figma');
    expect(screen.getByText('Figma 设计系统全解')).toBeInTheDocument();
    expect(screen.queryByText('React 19 架构精讲')).not.toBeInTheDocument();
  });

  it('shows empty state guiding to storage import when no courses exist', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({ resume: [], next_up: [], libraries: [], courses: [] });
        }
        if (url.endsWith('/api/v1/courses')) {
          return jsonResponse({ courses: [] });
        }
        if (url.endsWith('/api/v1/libraries')) {
          return jsonResponse({ libraries: [] });
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({ tags: [] });
        }
        return jsonResponse({}, 404);
      }),
    );

    render(
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>,
    );

    expect(await screen.findByText(/还没有导入任何课程/)).toBeInTheDocument();
    const importLink = screen.getByRole('link', { name: /前往存储与导入/ });
    expect(importLink).toHaveAttribute('href', '/storage');
  });

  it('allows creating a new library inline via POST /api/v1/libraries', async () => {
    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v1/home')) {
        return jsonResponse({ resume: [], next_up: [], libraries: [], courses: [] });
      }
      if (url.endsWith('/api/v1/courses')) {
        return jsonResponse({ courses: [] });
      }
      if (url.endsWith('/api/v1/libraries') && (!init?.method || init.method === 'GET')) {
        return jsonResponse({ libraries: [] });
      }
      if (url.endsWith('/api/v1/libraries') && init?.method === 'POST') {
        return jsonResponse(
          {
            id: 'lib-new',
            name: '系统架构',
            cover_path: '',
            sort_order: 1,
            course_count: 0,
            created_at: '2026-11-23T00:00:00Z',
            updated_at: '2026-11-23T00:00:00Z',
          },
          201,
        );
      }
      if (url.endsWith('/api/v1/tags')) {
        return jsonResponse({ tags: [] });
      }
      return jsonResponse({}, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    render(
      <MemoryRouter>
        <Layout>
          <HomePage />
        </Layout>
      </MemoryRouter>,
    );

    expect(await screen.findByRole('button', { name: /新建媒体库/ })).toBeInTheDocument();
    // Layout nav links exist
    expect(screen.getAllByRole('link', { name: '课程库' }).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByRole('link', { name: '存储与导入' }).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByRole('link', { name: '设置' }).length).toBeGreaterThanOrEqual(1);

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /新建媒体库/ }));
    await user.type(screen.getByLabelText('新媒体库名称'), '系统架构');
    await user.click(screen.getByRole('button', { name: '确认创建' }));

    expect(await screen.findByRole('button', { name: /系统架构/ })).toBeInTheDocument();
  });

  it('loads all >20 courses from /api/v1/courses and uses lesson id for Continue Watching thumbnail when course has no cover', async () => {
    const fullCourses = Array.from({ length: 24 }, (_, idx) => ({
      id: `course-${idx + 1}`,
      library_id: 'lib-1',
      title: `系统课程 ${idx + 1}`,
      sort_title: `系统课程 ${idx + 1}`,
      overview: '',
      instructor: '讲师',
      cover_path: '',
      backdrop_path: '',
      tags: [],
      chapter_count: 1,
      lesson_count: 1,
      played_count: 0,
      unplayed_item_count: 1,
      played_percentage: 0,
      played: false,
      total_duration_ms: 600000,
      first_lesson_id: '',
      created_at: '2026-11-23T00:00:00Z',
      updated_at: '2026-11-23T00:00:00Z',
    }));

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({
            resume: [
              {
                id: 'lesson-resume-99',
                course_id: 'course-24',
                chapter_id: 'chap-24',
                media_file_id: 'mf-24',
                title: '第 24 课：高并发收官',
                overview: '',
                sort_order: 1,
                cover_path: '',
                duration_ms: 600000,
                container: 'mp4',
                video_codec: 'h264',
                audio_codec: 'aac',
                width: 1920,
                height: 1080,
                bitrate: 2000000,
                thumbnail_path: 'mf-24.jpg',
                media_status: 'active',
                position_ticks: 150000 * 10000,
                played: false,
                play_count: 0,
                created_at: '2026-11-23T00:00:00Z',
                updated_at: '2026-11-23T12:00:00Z',
                course_title: '系统课程 24',
                course_cover_path: '',
                library_id: 'lib-1',
                chapter_title: '正片',
                chapter_sort_order: 1,
              },
            ],
            next_up: [],
            libraries: [
              {
                id: 'lib-1',
                name: '默认库',
                cover_path: '',
                sort_order: 1,
                course_count: 24,
                created_at: '2026-11-23T00:00:00Z',
                updated_at: '2026-11-23T00:00:00Z',
              },
            ],
            courses: fullCourses.slice(0, 20),
          });
        }
        if (url.endsWith('/api/v1/courses')) {
          return jsonResponse({ courses: fullCourses });
        }
        if (url.endsWith('/api/v1/libraries')) {
          return jsonResponse({
            libraries: [
              {
                id: 'lib-1',
                name: '默认库',
                cover_path: '',
                sort_order: 1,
                course_count: 24,
                created_at: '2026-11-23T00:00:00Z',
                updated_at: '2026-11-23T00:00:00Z',
              },
            ],
          });
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({ tags: [] });
        }
        return jsonResponse({}, 404);
      }),
    );

    render(
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>,
    );

    expect(await screen.findByRole('button', { name: '全部 (24)' })).toBeInTheDocument();
    expect(screen.getAllByText('系统课程 24').length).toBeGreaterThanOrEqual(2);
    expect(screen.getByRole('link', { name: '系统课程 21' })).toBeInTheDocument();

    const posterImg = screen.getByRole('img', { name: '系统课程 24' });
    expect(posterImg).toHaveAttribute(
      'src',
      '/Items/lesson-resume-99/Images/Primary?tag=2026-11-23T12%3A00%3A00Z&maxWidth=480',
    );
  });

  it('renders Citonema spotlight hero with >5 resume lessons, preserves all 7 items, and switches active preview & CTA', async () => {
    const sevenResume = Array.from({ length: 7 }, (_, idx) => ({
      id: `lesson-${idx + 1}`,
      course_id: `course-${idx + 1}`,
      chapter_id: `chap-${idx + 1}`,
      media_file_id: `mf-${idx + 1}`,
      title: `第 ${idx + 1} 节：核心专题 ${idx + 1}`,
      overview: '',
      sort_order: idx + 1,
      cover_path: '',
      duration_ms: 600000,
      container: 'mp4',
      video_codec: 'h264',
      audio_codec: 'aac',
      width: 1920,
      height: 1080,
      bitrate: 2000000,
      thumbnail_path: '',
      media_status: 'active',
      position_ticks: (idx + 1) * 60000 * 10000, // 10% .. 70%
      played: false,
      play_count: 0,
      created_at: '2026-11-20T00:00:00Z',
      updated_at: '2026-11-20T00:00:00Z',
      course_title: `课程 ${idx + 1}`,
      course_cover_path: '',
      library_id: 'lib-dev',
      chapter_title: `第 ${idx + 1} 章`,
      chapter_sort_order: idx + 1,
    }));

    const courses = Array.from({ length: 7 }, (_, idx) => ({
      id: `course-${idx + 1}`,
      library_id: 'lib-dev',
      title: `课程 ${idx + 1}`,
      sort_title: `课程 ${idx + 1}`,
      overview: '',
      instructor: '讲师',
      cover_path: '',
      backdrop_path: '',
      tags: [],
      chapter_count: 1,
      lesson_count: 10,
      played_count: idx,
      unplayed_item_count: 10 - idx,
      played_percentage: idx * 10,
      played: false,
      total_duration_ms: 600000,
      first_lesson_id: '',
      created_at: '2026-11-20T00:00:00Z',
      updated_at: '2026-11-20T00:00:00Z',
    }));

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({ resume: sevenResume, next_up: [], libraries: [], courses });
        }
        if (url.endsWith('/api/v1/courses')) {
          return jsonResponse({ courses });
        }
        if (url.endsWith('/api/v1/libraries')) {
          return jsonResponse({
            libraries: [
              {
                id: 'lib-dev',
                name: '编程开发',
                cover_path: '',
                sort_order: 1,
                course_count: 7,
                created_at: '2026-11-20T00:00:00Z',
                updated_at: '2026-11-20T00:00:00Z',
              },
            ],
          });
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({ tags: [] });
        }
        return jsonResponse({}, 404);
      }),
    );

    render(
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>,
    );

    expect(await screen.findByText('继续学习')).toBeInTheDocument();
    expect(screen.getByText('1 / 7')).toBeInTheDocument();

    // All 7 lessons are present inside the scrollable quick-switch list (none dropped)
    for (let i = 1; i <= 7; i += 1) {
      expect(
        screen.getAllByText(`第 ${i} 节：核心专题 ${i}`).length,
      ).toBeGreaterThanOrEqual(1);
    }

    const ctaLink = screen.getByRole('link', { name: /继续播放/ });
    expect(ctaLink).toHaveAttribute('href', '/courses/course-1?lesson=lesson-1');

    const user = userEvent.setup();

    // Clicking next chevron switches to item 2
    await user.click(screen.getByRole('button', { name: '下一个进行中课时' }));
    expect(screen.getByText('2 / 7')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /继续播放/ })).toHaveAttribute(
      'href',
      '/courses/course-2?lesson=lesson-2',
    );

    // Clicking the 6th item in quick-switch list switches spotlight preview to item 6
    await user.click(screen.getByRole('button', { name: /第 6 节：核心专题 6/ }));
    expect(screen.getByText('6 / 7')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /继续播放/ })).toHaveAttribute(
      'href',
      '/courses/course-6?lesson=lesson-6',
    );
  });

  it('scopes continue watching, tag chips, and header to ?library= inside LibraryProvider + Layout and returns to / on 查看全部', async () => {
    function LocationProbe() {
      const loc = useLocation();
      return (
        <div data-testid="current-location">
          {loc.pathname}
          {loc.search}
        </div>
      );
    }

    const libraries = [
      {
        id: 'lib-dev',
        name: '编程开发',
        cover_path: '',
        sort_order: 1,
        course_count: 2,
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
      },
      {
        id: 'lib-design',
        name: '设计艺术',
        cover_path: '',
        sort_order: 2,
        course_count: 1,
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
      },
    ];

    const courses = [
      {
        id: 'course-go',
        library_id: 'lib-dev',
        title: 'Go 语言进阶实战',
        sort_title: 'Go 语言进阶实战',
        overview: '',
        instructor: '张老师',
        cover_path: '',
        backdrop_path: '',
        tags: ['Go'],
        chapter_count: 3,
        lesson_count: 10,
        played_count: 4,
        unplayed_item_count: 6,
        played_percentage: 40,
        played: false,
        total_duration_ms: 3600000,
        first_lesson_id: '',
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
      },
      {
        id: 'course-react',
        library_id: 'lib-dev',
        title: 'React 19 架构精讲',
        sort_title: 'React 19 架构精讲',
        overview: '',
        instructor: '李老师',
        cover_path: '',
        backdrop_path: '',
        tags: ['React'],
        chapter_count: 2,
        lesson_count: 8,
        played_count: 0,
        unplayed_item_count: 8,
        played_percentage: 0,
        played: false,
        total_duration_ms: 2400000,
        first_lesson_id: '',
        created_at: '2026-11-21T00:00:00Z',
        updated_at: '2026-11-21T00:00:00Z',
      },
      {
        id: 'course-figma',
        library_id: 'lib-design',
        title: 'Figma 设计系统全解',
        sort_title: 'Figma 设计系统全解',
        overview: '',
        instructor: '王老师',
        cover_path: '',
        backdrop_path: '',
        tags: ['UI设计'],
        chapter_count: 4,
        lesson_count: 12,
        played_count: 12,
        unplayed_item_count: 0,
        played_percentage: 100,
        played: true,
        total_duration_ms: 4800000,
        first_lesson_id: '',
        created_at: '2026-11-22T00:00:00Z',
        updated_at: '2026-11-22T00:00:00Z',
      },
    ];

    const resume = [
      {
        id: 'lesson-1',
        course_id: 'course-go',
        chapter_id: 'chap-1',
        media_file_id: 'mf-1',
        title: '第 1 节：并发原语',
        overview: '',
        sort_order: 1,
        cover_path: '',
        duration_ms: 600000,
        container: 'mp4',
        video_codec: 'h264',
        audio_codec: 'aac',
        width: 1920,
        height: 1080,
        bitrate: 2000000,
        thumbnail_path: '',
        media_status: 'active',
        position_ticks: 300000 * 10000,
        played: false,
        play_count: 0,
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
        course_title: 'Go 语言进阶实战',
        course_cover_path: '',
        library_id: 'lib-dev',
        chapter_title: '第一章',
        chapter_sort_order: 1,
      },
    ];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/libraries')) {
          return jsonResponse({ libraries });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse({ user: { id: 'u1', username: 'alice', created_at: '', updated_at: '' } });
        }
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({ resume, next_up: [], libraries, courses });
        }
        if (url.endsWith('/api/v1/courses')) {
          return jsonResponse({ courses });
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({
            tags: [
              { id: 't1', name: 'Go', course_count: 1 },
              { id: 't2', name: 'React', course_count: 1 },
              { id: 't3', name: 'UI设计', course_count: 1 },
            ],
          });
        }
        return jsonResponse({}, 404);
      }),
    );

    render(
      <ThemeProvider>
        <ToastProvider>
          <MemoryRouter initialEntries={['/?library=lib-design']}>
            <LibraryProvider>
              <Routes>
                <Route path="*" element={<Layout />}>
                  <Route
                    index
                    element={
                      <>
                        <LocationProbe />
                        <HomePage />
                      </>
                    }
                  />
                </Route>
              </Routes>
            </LibraryProvider>
          </MemoryRouter>
        </ToastProvider>
      </ThemeProvider>,
    );

    // Header shows active library name
    expect(await screen.findByRole('heading', { level: 1, name: '设计艺术' })).toBeInTheDocument();
    // Should not duplicate standalone inline library pill bar
    expect(screen.queryByRole('button', { name: /^全部 \(/ })).not.toBeInTheDocument();

    // lib-dev resume item is filtered out (hero hidden because lib-design has 0 resume lessons)
    expect(screen.queryByRole('heading', { name: '继续学习' })).not.toBeInTheDocument();
    expect(screen.queryByText('第 1 节：并发原语')).not.toBeInTheDocument();

    // Only design course and UI设计 tag chip appear
    expect(screen.getByRole('link', { name: 'Figma 设计系统全解' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Go 语言进阶实战' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^UI设计/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Go/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^React/ })).not.toBeInTheDocument();

    // Clicking 查看全部 returns to / and shows all courses & continue watching
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /查看全部/ }));

    expect(await screen.findByRole('heading', { level: 1, name: '全部课程' })).toBeInTheDocument();
    expect(screen.getByTestId('current-location')).toHaveTextContent(/^\/$/);
    expect(screen.getByRole('heading', { name: '继续学习' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go 语言进阶实战' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Go/ })).toBeInTheDocument();
  });

  it('falls back cleanly to 全部课程 when ?library= points to a nonexistent library', async () => {
    const libraries = [
      {
        id: 'lib-dev',
        name: '编程开发',
        cover_path: '',
        sort_order: 1,
        course_count: 2,
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
      },
    ];
    const courses = [
      {
        id: 'course-go',
        library_id: 'lib-dev',
        title: 'Go 语言进阶实战',
        sort_title: 'Go 语言进阶实战',
        overview: '',
        instructor: '张老师',
        cover_path: '',
        backdrop_path: '',
        tags: ['Go'],
        chapter_count: 3,
        lesson_count: 10,
        played_count: 4,
        unplayed_item_count: 6,
        played_percentage: 40,
        played: false,
        total_duration_ms: 3600000,
        first_lesson_id: '',
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
      },
      {
        id: 'course-figma',
        library_id: 'lib-other',
        title: 'Figma 设计系统全解',
        sort_title: 'Figma 设计系统全解',
        overview: '',
        instructor: '王老师',
        cover_path: '',
        backdrop_path: '',
        tags: ['UI设计'],
        chapter_count: 4,
        lesson_count: 12,
        played_count: 12,
        unplayed_item_count: 0,
        played_percentage: 100,
        played: true,
        total_duration_ms: 4800000,
        first_lesson_id: '',
        created_at: '2026-11-22T00:00:00Z',
        updated_at: '2026-11-22T00:00:00Z',
      },
    ];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/libraries')) {
          return jsonResponse({ libraries });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse({ user: { id: 'u1', username: 'alice', created_at: '', updated_at: '' } });
        }
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({ resume: [], next_up: [], libraries, courses });
        }
        if (url.endsWith('/api/v1/courses')) {
          return jsonResponse({ courses });
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({ tags: [] });
        }
        return jsonResponse({}, 404);
      }),
    );

    render(
      <ThemeProvider>
        <ToastProvider>
          <MemoryRouter initialEntries={['/?library=nonexistent-id']}>
            <LibraryProvider>
              <Routes>
                <Route path="*" element={<Layout />}>
                  <Route index element={<HomePage />} />
                </Route>
              </Routes>
            </LibraryProvider>
          </MemoryRouter>
        </ToastProvider>
      </ThemeProvider>,
    );

    expect(await screen.findByRole('heading', { level: 1, name: '全部课程' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go 语言进阶实战' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Figma 设计系统全解' })).toBeInTheDocument();
  });

  it('reorders course cards client-side via 排序方式 and bumps catalogVersion after creating a blank course', async () => {
    let libFetches = 0;
    const libraries = [
      {
        id: 'lib-dev',
        name: '编程开发',
        cover_path: '',
        sort_order: 1,
        course_count: 3,
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
      },
    ];
    const courses = [
      {
        id: 'course-go',
        library_id: 'lib-dev',
        title: 'Go 语言进阶实战',
        sort_title: 'Go 语言进阶实战',
        overview: '',
        instructor: '张老师',
        cover_path: '',
        backdrop_path: '',
        tags: [],
        chapter_count: 3,
        lesson_count: 10,
        played_count: 4,
        unplayed_item_count: 6,
        played_percentage: 40,
        played: false,
        total_duration_ms: 3600000,
        first_lesson_id: '',
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
      },
      {
        id: 'course-react',
        library_id: 'lib-dev',
        title: 'React 19 架构精讲',
        sort_title: 'React 19 架构精讲',
        overview: '',
        instructor: '李老师',
        cover_path: '',
        backdrop_path: '',
        tags: [],
        chapter_count: 2,
        lesson_count: 8,
        played_count: 0,
        unplayed_item_count: 8,
        played_percentage: 0,
        played: false,
        total_duration_ms: 2400000,
        first_lesson_id: '',
        created_at: '2026-11-21T00:00:00Z',
        updated_at: '2026-11-21T00:00:00Z',
      },
      {
        id: 'course-figma',
        library_id: 'lib-dev',
        title: 'Figma 设计系统全解',
        sort_title: 'Figma 设计系统全解',
        overview: '',
        instructor: '王老师',
        cover_path: '',
        backdrop_path: '',
        tags: [],
        chapter_count: 4,
        lesson_count: 12,
        played_count: 12,
        unplayed_item_count: 0,
        played_percentage: 100,
        played: true,
        total_duration_ms: 4800000,
        first_lesson_id: '',
        created_at: '2026-11-22T00:00:00Z',
        updated_at: '2026-11-22T00:00:00Z',
      },
    ];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          libFetches += 1;
          return jsonResponse({ libraries });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse({ user: { id: 'u1', username: 'alice', created_at: '', updated_at: '' } });
        }
        if (url.endsWith('/api/v1/home')) {
          return jsonResponse({ resume: [], next_up: [], libraries, courses });
        }
        if (url.endsWith('/api/v1/courses') && method === 'GET') {
          return jsonResponse({ courses });
        }
        if (url.endsWith('/api/v1/courses') && method === 'POST') {
          return jsonResponse({ id: 'course-new', title: '新建课程', chapters: [] }, 201);
        }
        if (url.endsWith('/api/v1/tags')) {
          return jsonResponse({ tags: [] });
        }
        return jsonResponse({}, 404);
      }),
    );

    render(
      <ThemeProvider>
        <ToastProvider>
          <MemoryRouter initialEntries={['/']}>
            <LibraryProvider>
              <Routes>
                <Route path="*" element={<Layout />}>
                  <Route index element={<HomePage />} />
                  <Route path="courses/:courseId/edit" element={<div data-testid="course-editor">编辑页</div>} />
                </Route>
              </Routes>
            </LibraryProvider>
          </MemoryRouter>
        </ToastProvider>
      </ThemeProvider>,
    );

    expect(await screen.findByRole('link', { name: 'Go 语言进阶实战' })).toBeInTheDocument();

    const getOrder = () =>
      screen
        .getAllByRole('article')
        .map((card) => within(card).getByRole('link', { name: /^编辑 / }).getAttribute('aria-label'));

    expect(getOrder()).toEqual([
      '编辑 Go 语言进阶实战',
      '编辑 React 19 架构精讲',
      '编辑 Figma 设计系统全解',
    ]);

    const user = userEvent.setup();
    const sortSelect = screen.getByLabelText('排序方式');
    await user.selectOptions(sortSelect, 'progress');

    expect(getOrder()).toEqual([
      '编辑 Figma 设计系统全解',
      '编辑 Go 语言进阶实战',
      '编辑 React 19 架构精讲',
    ]);

    const fetchesBeforeCreate = libFetches;
    await user.click(screen.getByRole('button', { name: /新建空白课程/ }));
    await user.type(screen.getByLabelText('新课程名称'), 'Rust 系统编程');
    await user.click(screen.getByRole('button', { name: '创建并编辑' }));

    expect(await screen.findByTestId('course-editor')).toBeInTheDocument();
    await waitFor(() => {
      expect(libFetches).toBeGreaterThan(fetchesBeforeCreate);
    });
  });
});

describe('SetupLoginPage', () => {
  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('shows first-run setup form when setup status reports needs_setup=true', async () => {
    const mockFetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v1/setup/status')) {
        return jsonResponse({ needs_setup: true });
      }
      if (url.endsWith('/api/v1/setup')) {
        return jsonResponse(
          {
            user: {
              id: 'u1',
              username: 'admin',
              created_at: '2026-11-23T00:00:00Z',
              updated_at: '2026-11-23T00:00:00Z',
            },
            needs_setup: false,
          },
          201,
        );
      }
      if (url.endsWith('/api/v1/auth/login')) {
        return jsonResponse({
          token: 'new-session-token',
          user: {
            id: 'u1',
            username: 'admin',
            created_at: '2026-11-23T00:00:00Z',
            updated_at: '2026-11-23T00:00:00Z',
          },
          session: {
            token: 'new-session-token',
            user_id: 'u1',
            device_id: 'web',
            device_name: 'CourseFin Web',
            client_name: 'CourseFin Web',
            client_version: '0.1.0',
            last_active_at: '2026-11-23T00:00:00Z',
            created_at: '2026-11-23T00:00:00Z',
          },
        });
      }
      return jsonResponse({}, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    render(
      <MemoryRouter initialEntries={['/login']}>
        <SetupLoginPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText(/初始化管理员账号/)).toBeInTheDocument();
    expect(screen.getByLabelText(/服务器名称/)).toBeInTheDocument();
    expect(screen.getByLabelText(/管理员用户名/)).toBeInTheDocument();
    expect(screen.getByLabelText(/管理员密码/)).toBeInTheDocument();

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/管理员用户名/), 'admin');
    await user.type(screen.getByLabelText(/管理员密码/), 'secret123');
    await user.click(screen.getByRole('button', { name: /完成初始化并登录/ }));

    await waitFor(() => {
      expect(localStorage.getItem('coursefin_token')).toBe('new-session-token');
    });
  });

  it('shows login form when setup status reports needs_setup=false', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/setup/status')) {
          return jsonResponse({ needs_setup: false });
        }
        return jsonResponse({}, 404);
      }),
    );

    render(
      <MemoryRouter initialEntries={['/login']}>
        <SetupLoginPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText(/登录 CourseFin/)).toBeInTheDocument();
    expect(screen.getByLabelText(/用户名/)).toBeInTheDocument();
    expect(screen.getByLabelText(/密码/)).toBeInTheDocument();
  });
});

describe('api client helpers', () => {
  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('formats api_key query param and primary image url', () => {
    localStorage.setItem('coursefin_token', 'tok-abc');
    expect(withApiKey('/Videos/l1/master.m3u8')).toBe('/Videos/l1/master.m3u8?api_key=tok-abc');
    expect(withApiKey('/Videos/l1/stream.mp4?Static=true')).toBe(
      '/Videos/l1/stream.mp4?Static=true&api_key=tok-abc',
    );
    expect(itemPrimaryImageUrl('course-1', 'tag-9')).toBe(
      '/Items/course-1/Images/Primary?tag=tag-9&maxWidth=480',
    );
    expect(itemPrimaryImageUrl('course-1')).toBe('/Items/course-1/Images/Primary?maxWidth=480');
  });

  it('clears token on 401 responses', async () => {
    localStorage.setItem('coursefin_token', 'expired-tok');
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse({ error: 'unauthorized' }, 401)),
    );

    await expect(api.getHome()).rejects.toThrow('unauthorized');
    expect(localStorage.getItem('coursefin_token')).toBeNull();
  });
});
