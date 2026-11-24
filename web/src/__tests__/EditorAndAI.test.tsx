import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import AIOrganizeModal from '../components/AIOrganizeModal';
import ImportCourseModal from '../components/ImportCourseModal';
import { ToastProvider } from '../components/Toast';
import { LibraryProvider, useLibrary } from '../contexts/LibraryContext';
import CourseEditorPage, { resolveDragReorder } from '../pages/CourseEditorPage';
import StoragePage from '../pages/StoragePage';
import SettingsPage from '../pages/SettingsPage';
import type { CourseDetail, Library } from '../types/coursefin';

const LEGACY_CLASS_REGEX = /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald|sky)-/;

function LibraryObserver() {
  const { catalogVersion, libraries } = useLibrary();
  return (
    <div>
      <span data-testid="catalog-version">{catalogVersion}</span>
      <span data-testid="library-counts">
        {libraries.map((lib) => `${lib.id}:${lib.course_count}`).join(',')}
      </span>
    </div>
  );
}

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const sampleCourse: CourseDetail = {
  id: 'course-1',
  library_id: 'lib-1',
  title: '原始 Go 视频合集',
  sort_title: '原始 Go 视频合集',
  overview: '旧课程简介',
  instructor: '陈老师',
  cover_path: '',
  backdrop_path: '',
  source_id: 'src-1',
  source_root_path: '/courses/go',
  tags: ['Go'],
  chapter_count: 2,
  lesson_count: 2,
  played_count: 0,
  unplayed_item_count: 2,
  played_percentage: 0,
  played: false,
  total_duration_ms: 1200000,
  first_lesson_id: 'lesson-1',
  created_at: '2026-11-20T00:00:00Z',
  updated_at: '2026-11-20T00:00:00Z',
  chapters: [
    {
      id: 'chap-1',
      course_id: 'course-1',
      title: '01_basics',
      sort_order: 1,
      overview: '',
      lesson_count: 1,
      played_count: 0,
      unplayed_item_count: 1,
      played_percentage: 0,
      played: false,
      created_at: '2026-11-20T00:00:00Z',
      updated_at: '2026-11-20T00:00:00Z',
      lessons: [
        {
          id: 'lesson-1',
          course_id: 'course-1',
          chapter_id: 'chap-1',
          media_file_id: 'mf-1',
          title: '01_intro.mp4',
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
          thumbnail_path: 'mf-1.jpg',
          media_status: 'active',
          position_ticks: 0,
          played: false,
          play_count: 0,
          subtitles: [
            {
              id: 'sub-1',
              language: 'zh',
              title: '简体中文',
              format: 'srt',
              is_default: true,
              status: 'active',
            },
          ],
          created_at: '2026-11-20T00:00:00Z',
          updated_at: '2026-11-20T00:00:00Z',
        },
      ],
    },
    {
      id: 'chap-2',
      course_id: 'course-1',
      title: '02_concurrency',
      sort_order: 2,
      overview: '',
      lesson_count: 1,
      played_count: 0,
      unplayed_item_count: 1,
      played_percentage: 0,
      played: false,
      created_at: '2026-11-20T00:00:00Z',
      updated_at: '2026-11-20T00:00:00Z',
      lessons: [
        {
          id: 'lesson-2',
          course_id: 'course-1',
          chapter_id: 'chap-2',
          media_file_id: 'mf-2',
          title: '02_goroutine.mp4',
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
          thumbnail_path: 'mf-2.jpg',
          media_status: 'active',
          position_ticks: 0,
          played: false,
          play_count: 0,
          created_at: '2026-11-20T00:00:00Z',
          updated_at: '2026-11-20T00:00:00Z',
        },
      ],
    },
  ],
};

const sampleLibraries: Library[] = [
  {
    id: 'lib-1',
    name: '编程开发',
    cover_path: '',
    sort_order: 1,
    course_count: 1,
    created_at: '2026-11-20T00:00:00Z',
    updated_at: '2026-11-20T00:00:00Z',
  },
  {
    id: 'lib-2',
    name: '系统架构',
    cover_path: '',
    sort_order: 2,
    course_count: 0,
    created_at: '2026-11-20T00:00:00Z',
    updated_at: '2026-11-20T00:00:00Z',
  },
];

describe('AIOrganizeModal', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('AIOrganizeModal previews proposal and only calls apply endpoint with user-selected checkboxes on confirm', async () => {
    const applyRequests: unknown[] = [];
    let organizeCalls = 0;

    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v1/courses/course-1/ai/organize') && init?.method === 'POST') {
        organizeCalls++;
        return jsonResponse({
          title: 'Go 语言并发编程实战',
          overview: '系统讲解 Go 并发原语与工程最佳实践',
          tags: ['Go', '并发编程', '后端'],
          chapters: [
            {
              title: '第一章：语言基础回顾',
              lessons: [{ lesson_id: 'lesson-1', title: '第 1 节：课程导学与环境准备' }],
            },
            {
              title: '第二章：并发模型核心',
              lessons: [{ lesson_id: 'lesson-2', title: '第 2 节：Goroutine 调度机制' }],
            },
          ],
        });
      }
      if (url.endsWith('/api/v1/courses/course-1/ai/apply') && init?.method === 'POST') {
        applyRequests.push(JSON.parse(String(init.body)));
        return jsonResponse({
          ...sampleCourse,
          overview: '系统讲解 Go 并发原语与工程最佳实践',
          tags: ['Go', '并发编程', '后端'],
        });
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    const onApplied = vi.fn();
    const onClose = vi.fn();

    render(
      <MemoryRouter>
        <AIOrganizeModal
          course={sampleCourse}
          isOpen={true}
          onClose={onClose}
          onApplied={onApplied}
        />
      </MemoryRouter>,
    );

    // Wait for proposal preview to load
    expect(await screen.findByText('Go 语言并发编程实战')).toBeInTheDocument();
    expect(screen.getByText('系统讲解 Go 并发原语与工程最佳实践')).toBeInTheDocument();
    expect(screen.getByText('第一章：语言基础回顾')).toBeInTheDocument();
    expect(screen.getByText(/第 1 节：课程导学与环境准备/)).toBeInTheDocument();

    // Critical safety check: /apply must NOT have been called during preview
    expect(organizeCalls).toBe(1);
    expect(applyRequests).toHaveLength(0);

    const user = userEvent.setup();
    const titleCheckbox = screen.getByLabelText(/应用建议标题/);
    const overviewCheckbox = screen.getByLabelText(/应用建议简介/);
    const tagsCheckbox = screen.getByLabelText(/应用建议标签/);
    const chaptersCheckbox = screen.getByLabelText(/应用建议章节/);

    expect(titleCheckbox).toBeChecked();
    expect(overviewCheckbox).toBeChecked();
    expect(tagsCheckbox).toBeChecked();
    expect(chaptersCheckbox).toBeChecked();

    // Uncheck title and chapters; keep only overview & tags
    await user.click(titleCheckbox);
    await user.click(chaptersCheckbox);
    expect(titleCheckbox).not.toBeChecked();
    expect(chaptersCheckbox).not.toBeChecked();
    expect(applyRequests).toHaveLength(0);

    await user.click(screen.getByRole('button', { name: /确认应用/ }));

    await waitFor(() => {
      expect(applyRequests).toHaveLength(1);
    });

    expect(applyRequests[0]).toEqual({
      apply_title: false,
      apply_overview: true,
      apply_tags: true,
      apply_chapters: false,
      title: 'Go 语言并发编程实战',
      overview: '系统讲解 Go 并发原语与工程最佳实践',
      tags: ['Go', '并发编程', '后端'],
      chapters: [
        {
          title: '第一章：语言基础回顾',
          lessons: [{ lesson_id: 'lesson-1', title: '第 1 节：课程导学与环境准备' }],
        },
        {
          title: '第二章：并发模型核心',
          lessons: [{ lesson_id: 'lesson-2', title: '第 2 节：Goroutine 调度机制' }],
        },
      ],
    });
    expect(onApplied).toHaveBeenCalledTimes(1);
  });

  it('AIOrganizeModal renders dual-theme semantic surfaces without legacy dark-only classes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/course-1/ai/organize') && init?.method === 'POST') {
          return jsonResponse({
            title: 'Go 语言并发编程实战',
            overview: '系统讲解 Go 并发原语',
            tags: ['Go'],
            chapters: [
              {
                title: '第一章：并发基础',
                lessons: [{ lesson_id: 'lesson-1', title: '第 1 节：导学' }],
              },
            ],
          });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const { container } = render(
      <MemoryRouter>
        <AIOrganizeModal
          course={sampleCourse}
          isOpen={true}
          onClose={vi.fn()}
          onApplied={vi.fn()}
        />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Go 语言并发编程实战')).toBeInTheDocument();
    const dialog = screen.getByRole('dialog');
    expect(dialog.className).toContain('bg-overlay');
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);
  });
});

describe('ImportCourseModal', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('ImportCourseModal displays inferred chapters/lessons and submits edited course title on confirm', async () => {
    const commitPayloads: unknown[] = [];

    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v1/import/preview') && init?.method === 'POST') {
        return jsonResponse({
          source_id: 'src-1',
          dir_rel_path: 'go-course',
          course_title: 'go-course',
          cover_rel_path: 'go-course/cover.jpg',
          total_files: 2,
          chapters: [
            {
              title: '01_intro',
              sort_order: 1,
              lessons: [
                {
                  media_file_id: 'mf-10',
                  relative_path: 'go-course/01_intro/01_hello.mp4',
                  file_name: '01_hello.mp4',
                  title: '01_hello',
                  duration_ms: 300000,
                  sort_order: 1,
                },
                {
                  media_file_id: 'mf-11',
                  relative_path: 'go-course/01_intro/02_types.mp4',
                  file_name: '02_types.mp4',
                  title: '02_types',
                  duration_ms: 420000,
                  sort_order: 2,
                },
              ],
            },
          ],
        });
      }
      if (url.endsWith('/api/v1/import/commit') && init?.method === 'POST') {
        commitPayloads.push(JSON.parse(String(init.body)));
        return jsonResponse(
          {
            ...sampleCourse,
            id: 'course-imported',
            library_id: 'lib-2',
            title: 'Go 语言全栈入门',
          },
          201,
        );
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    const onImported = vi.fn();
    const onClose = vi.fn();

    render(
      <MemoryRouter>
        <ImportCourseModal
          isOpen={true}
          sourceId="src-1"
          dirRelPath="go-course"
          libraries={sampleLibraries}
          onClose={onClose}
          onImported={onImported}
        />
      </MemoryRouter>,
    );

    const titleInput = await screen.findByLabelText(/课程标题/);
    expect(titleInput).toHaveValue('go-course');
    expect(screen.getByDisplayValue('01_intro')).toBeInTheDocument();
    expect(screen.getByDisplayValue('01_hello')).toBeInTheDocument();
    expect(screen.getByDisplayValue('02_types')).toBeInTheDocument();

    const user = userEvent.setup();
    await user.clear(titleInput);
    await user.type(titleInput, 'Go 语言全栈入门');

    const libSelect = screen.getByLabelText(/所属媒体库/);
    await user.selectOptions(libSelect, 'lib-2');

    await user.click(screen.getByRole('button', { name: /确认导入/ }));

    await waitFor(() => {
      expect(commitPayloads).toHaveLength(1);
    });

    expect(commitPayloads[0]).toMatchObject({
      source_id: 'src-1',
      dir_rel_path: 'go-course',
      library_id: 'lib-2',
      title: 'Go 语言全栈入门',
      cover_rel_path: 'go-course/cover.jpg',
      chapters: [
        {
          title: '01_intro',
          lessons: [
            { media_file_id: 'mf-10', title: '01_hello' },
            { media_file_id: 'mf-11', title: '02_types' },
          ],
        },
      ],
    });
    expect(onImported).toHaveBeenCalledTimes(1);
  });
});

describe('CourseEditorPage', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('edits metadata, picks cover from lesson frame, reorders chapters, and checks new files', async () => {
    let currentCourse: CourseDetail = structuredClone(sampleCourse);
    let reorderChapterBody: unknown = null;
    let coverPickBody: unknown = null;
    let appendedBody: unknown = null;

    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = init?.method ?? 'GET';

      if (url.endsWith('/api/v1/courses/course-1') && method === 'GET') {
        return jsonResponse(currentCourse);
      }
      if (url.endsWith('/api/v1/libraries') && method === 'GET') {
        return jsonResponse({ libraries: sampleLibraries });
      }
      if (url.endsWith('/api/v1/courses/course-1') && method === 'PUT') {
        const body = JSON.parse(String(init?.body));
        currentCourse = { ...currentCourse, ...body };
        return jsonResponse(currentCourse);
      }
      if (url.endsWith('/api/v1/courses/course-1/cover') && method === 'POST') {
        coverPickBody = JSON.parse(String(init?.body));
        currentCourse = { ...currentCourse, cover_path: 'course-1_cover.jpg' };
        return jsonResponse(currentCourse);
      }
      if (url.endsWith('/api/v1/courses/course-1/chapters/reorder') && method === 'PUT') {
        reorderChapterBody = JSON.parse(String(init?.body));
        return jsonResponse({ ok: true });
      }
      if (url.endsWith('/api/v1/courses/course-1/new-files') && method === 'GET') {
        return jsonResponse({
          files: [
            {
              id: 'mf-new-1',
              source_id: 'src-1',
              relative_path: 'go/03_testing.mp4',
              file_name: '03_testing.mp4',
              file_size: 1024,
              mod_time: '2026-11-23T00:00:00Z',
              status: 'active',
              duration_ms: 480000,
              container: 'mp4',
              video_codec: 'h264',
              audio_codec: 'aac',
              width: 1920,
              height: 1080,
              bitrate: 2000000,
              thumbnail_path: '',
              created_at: '2026-11-23T00:00:00Z',
              updated_at: '2026-11-23T00:00:00Z',
            },
          ],
        });
      }
      if (url.endsWith('/api/v1/courses/course-1/new-files') && method === 'POST') {
        appendedBody = JSON.parse(String(init?.body));
        return jsonResponse(currentCourse);
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    render(
      <MemoryRouter initialEntries={['/courses/course-1/edit']}>
        <Routes>
          <Route path="/courses/:courseId/edit" element={<CourseEditorPage />} />
        </Routes>
      </MemoryRouter>,
    );

    const titleInput = await screen.findByLabelText(/课程标题/);
    expect(titleInput).toHaveValue('原始 Go 视频合集');
    // Subtitle badge rendered for lesson-1
    expect(screen.getByText(/简体中文/)).toBeInTheDocument();

    const user = userEvent.setup();
    await user.clear(titleInput);
    await user.type(titleInput, 'Go 实战精修');
    await user.click(screen.getByRole('button', { name: /保存元数据/ }));

    await waitFor(() => {
      expect(currentCourse.title).toBe('Go 实战精修');
    });

    // Pick cover from lesson frame
    await user.click(screen.getByRole('button', { name: /从课时帧选取封面/ }));
    const frameBtn = await screen.findByRole('button', { name: /01_intro\.mp4\s*设为封面/ });
    await user.click(frameBtn);

    await waitFor(() => {
      expect(coverPickBody).toEqual({ lesson_id: 'lesson-1' });
    });

    // Reorder chapters (move chap-1 down)
    const moveDownBtn = screen.getByRole('button', { name: /下移章节 01_basics/ });
    await user.click(moveDownBtn);

    await waitFor(() => {
      expect(reorderChapterBody).toEqual({ chapter_ids: ['chap-2', 'chap-1'] });
    });

    // Check new files & append
    await user.click(screen.getByRole('button', { name: /检查新文件/ }));
    expect(await screen.findByText('03_testing.mp4')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /追加到课程/ }));

    await waitFor(() => {
      expect(appendedBody).toEqual({ media_file_ids: ['mf-new-1'] });
    });
  });

  it('surfaces inline chapter and lesson rename errors without unhandled rejections', async () => {
    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = init?.method ?? 'GET';

      if (url.endsWith('/api/v1/courses/course-1') && method === 'GET') {
        return jsonResponse(structuredClone(sampleCourse));
      }
      if (url.endsWith('/api/v1/libraries') && method === 'GET') {
        return jsonResponse({ libraries: sampleLibraries });
      }
      if (url.endsWith('/api/v1/chapters/chap-1') && method === 'PUT') {
        return jsonResponse({ error: '章节重命名失败：标题重复' }, 400);
      }
      if (url.endsWith('/api/v1/lessons/lesson-1') && method === 'PUT') {
        return jsonResponse({ error: '课时重命名失败：服务器错误' }, 500);
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    render(
      <MemoryRouter initialEntries={['/courses/course-1/edit']}>
        <Routes>
          <Route path="/courses/:courseId/edit" element={<CourseEditorPage />} />
        </Routes>
      </MemoryRouter>,
    );

    await screen.findByLabelText(/课程标题/);
    const user = userEvent.setup();

    // Fail chapter rename
    await user.click(screen.getByRole('button', { name: /重命名章节 01_basics/ }));
    const chapInput = screen.getByLabelText(/编辑章节标题 01_basics/);
    await user.clear(chapInput);
    await user.type(chapInput, '新章节名称');
    await user.click(within(chapInput.parentElement!).getByRole('button', { name: '保存' }));

    expect(await screen.findByText('章节重命名失败：标题重复')).toBeInTheDocument();
    expect(screen.getByLabelText(/编辑章节标题 01_basics/)).toBeInTheDocument();

    // Fail lesson rename
    await user.click(screen.getByRole('button', { name: /重命名课时 01_intro\.mp4/ }));
    const lessonInput = screen.getByLabelText(/编辑课时标题 01_intro\.mp4/);
    await user.clear(lessonInput);
    await user.type(lessonInput, '新课时名称');
    await user.click(within(lessonInput.parentElement!).getByRole('button', { name: '保存' }));

    expect(await screen.findByText('课时重命名失败：服务器错误')).toBeInTheDocument();
    expect(screen.getByLabelText(/编辑课时标题 01_intro\.mp4/)).toBeInTheDocument();
  });

  it('resolveDragReorder resolves parent chapter when dragging a chapter over a lesson inside a populated chapter card', () => {
    const chapters = structuredClone(sampleCourse.chapters);

    // Dragging chapter 1 onto lesson-2 (inside chapter 2) reorders chapters [chap-2, chap-1]
    const overOtherChapterLesson = resolveDragReorder(chapters, 'chapter-chap-1', 'lesson-lesson-2');
    expect(overOtherChapterLesson?.type).toBe('chapter');
    expect(overOtherChapterLesson?.chapters.map((c) => c.id)).toEqual(['chap-2', 'chap-1']);

    // Dragging chapter 1 onto a lesson inside chapter 1 itself is a no-op
    const overOwnLesson = resolveDragReorder(chapters, 'chapter-chap-1', 'lesson-lesson-1');
    expect(overOwnLesson).toBeNull();

    // Dragging chapter 1 directly over chapter 2 reorders chapters
    const overChapterDirect = resolveDragReorder(chapters, 'chapter-chap-1', 'chapter-chap-2');
    expect(overChapterDirect?.type).toBe('chapter');
    expect(overChapterDirect?.chapters.map((c) => c.id)).toEqual(['chap-2', 'chap-1']);

    // Dragging lesson 1 onto chapter 2 moves lesson 1 into chapter 2
    const lessonToChapter = resolveDragReorder(chapters, 'lesson-lesson-1', 'chapter-chap-2');
    expect(lessonToChapter?.type).toBe('lesson');
    expect(lessonToChapter?.chapters[0].lessons).toHaveLength(0);
    expect(lessonToChapter?.chapters[1].lessons.map((l) => l.id)).toEqual(['lesson-2', 'lesson-1']);
  });

  it('renders sticky studio top bar without legacy classes and notifies LibraryProvider when library_id changes', async () => {
    let currentCourse: CourseDetail = structuredClone(sampleCourse);
    let currentLibraries: Library[] = structuredClone(sampleLibraries);
    let listLibrariesCalls = 0;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/courses/course-1') && method === 'GET') {
          return jsonResponse(currentCourse);
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          listLibrariesCalls += 1;
          return jsonResponse({ libraries: currentLibraries });
        }
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
        if (url.endsWith('/api/v1/courses/course-1') && method === 'PUT') {
          const body = JSON.parse(String(init?.body)) as {
            title: string;
            instructor: string;
            library_id: string;
            overview: string;
            tags: string[];
          };
          if (body.library_id !== currentCourse.library_id) {
            currentLibraries = currentLibraries.map((lib) => {
              if (lib.id === currentCourse.library_id) {
                return { ...lib, course_count: Math.max(0, lib.course_count - 1) };
              }
              if (lib.id === body.library_id) {
                return { ...lib, course_count: lib.course_count + 1 };
              }
              return lib;
            });
          }
          currentCourse = { ...currentCourse, ...body };
          return jsonResponse(currentCourse);
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const { container } = render(
      <ToastProvider>
        <LibraryProvider>
          <MemoryRouter initialEntries={['/courses/course-1/edit']}>
            <LibraryObserver />
            <Routes>
              <Route path="/courses/:courseId/edit" element={<CourseEditorPage />} />
            </Routes>
          </MemoryRouter>
        </LibraryProvider>
      </ToastProvider>,
    );

    const titleInput = await screen.findByLabelText(/课程标题/);
    expect(titleInput).toHaveValue('原始 Go 视频合集');
    await waitFor(() => {
      expect(screen.getByTestId('library-counts')).toHaveTextContent('lib-1:1,lib-2:0');
    });

    // Verify sticky studio top bar and zero legacy dark-only classes
    const backLink = screen.getByRole('link', { name: /返回课程播放/ });
    const stickyBar = backLink.closest('header');
    expect(stickyBar).not.toBeNull();
    expect(stickyBar!.className).toContain('sticky');
    expect(stickyBar!.className).toContain('top-0');
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);

    const user = userEvent.setup();

    // Saving metadata without changing library_id does not trigger redundant library catalog refresh
    const callsBeforeTitleSave = listLibrariesCalls;
    await user.clear(titleInput);
    await user.type(titleInput, 'Go 课程精修');
    await user.click(screen.getByRole('button', { name: /保存元数据/ }));
    await waitFor(() => {
      expect(currentCourse.title).toBe('Go 课程精修');
    });
    expect(screen.getByTestId('catalog-version')).toHaveTextContent('0');
    expect(listLibrariesCalls).toBe(callsBeforeTitleSave);

    // Changing 所属媒体库 and saving triggers LibraryProvider bumpCatalogVersion & library refresh
    const librarySelect = screen.getByLabelText(/所属媒体库/);
    await user.selectOptions(librarySelect, 'lib-2');
    await user.click(screen.getByRole('button', { name: /保存元数据/ }));

    await waitFor(() => {
      expect(screen.getByTestId('catalog-version')).toHaveTextContent('1');
    });
    await waitFor(() => {
      expect(screen.getByTestId('library-counts')).toHaveTextContent('lib-1:0,lib-2:1');
    });
  });

  it('notifies LibraryProvider and navigates home after deleting a course', async () => {
    let currentLibraries: Library[] = structuredClone(sampleLibraries);
    let deleteCalled = false;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/courses/course-1') && method === 'GET') {
          return jsonResponse(structuredClone(sampleCourse));
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({ libraries: currentLibraries });
        }
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
        if (url.endsWith('/api/v1/courses/course-1') && method === 'DELETE') {
          deleteCalled = true;
          currentLibraries = currentLibraries.map((lib) =>
            lib.id === 'lib-1' ? { ...lib, course_count: 0 } : lib,
          );
          return jsonResponse({ ok: true });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <LibraryProvider>
          <MemoryRouter initialEntries={['/courses/course-1/edit']}>
            <LibraryObserver />
            <Routes>
              <Route path="/" element={<div>已返回课程库首页</div>} />
              <Route path="/courses/:courseId/edit" element={<CourseEditorPage />} />
            </Routes>
          </MemoryRouter>
        </LibraryProvider>
      </ToastProvider>,
    );

    await screen.findByLabelText(/课程标题/);
    await waitFor(() => {
      expect(screen.getByTestId('library-counts')).toHaveTextContent('lib-1:1,lib-2:0');
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /删除课程/ }));
    await user.click(screen.getByRole('button', { name: '确认' }));

    await waitFor(() => {
      expect(deleteCalled).toBe(true);
    });
    expect(await screen.findByText('已返回课程库首页')).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByTestId('catalog-version')).toHaveTextContent('1');
      expect(screen.getByTestId('library-counts')).toHaveTextContent('lib-1:0,lib-2:0');
    });
  });
});

describe('StoragePage & SettingsPage', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('StoragePage adds source, polls async scan until done, browses dirs and opens ImportCourseModal', async () => {
    let sources = [
      {
        id: 'src-1',
        name: '本地课程盘',
        driver: 'local',
        root_path: '/data/courses',
        config_json: '{}',
        enabled: true,
        media_count: 12,
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
      },
    ];
    let scanTriggered = false;
    let statusCalls = 0;

    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = init?.method ?? 'GET';

      if (url.endsWith('/api/v1/sources') && method === 'GET') {
        return jsonResponse({ sources });
      }
      if (url.endsWith('/api/v1/libraries') && method === 'GET') {
        return jsonResponse({ libraries: sampleLibraries });
      }
      if (url.includes('/api/v1/sources/src-1/dirs') && method === 'GET') {
        return jsonResponse({
          dirs: [
            {
              rel_path: 'rust-course',
              name: 'rust-course',
              file_count: 8,
              direct_files: 8,
              has_subdirs: false,
            },
          ],
        });
      }
      if (url.includes('/api/v1/sources/src-1/files') && method === 'GET') {
        return jsonResponse({
          files: [
            {
              id: 'mf-miss',
              source_id: 'src-1',
              relative_path: 'rust-course/01.mp4',
              file_name: '01.mp4',
              file_size: 1024,
              mod_time: '2026-11-20T00:00:00Z',
              status: 'missing',
              duration_ms: 10000,
              container: 'mp4',
              video_codec: 'h264',
              audio_codec: 'aac',
              width: 1920,
              height: 1080,
              bitrate: 1000000,
              thumbnail_path: '',
              created_at: '2026-11-20T00:00:00Z',
              updated_at: '2026-11-20T00:00:00Z',
            },
          ],
        });
      }
      if (url.includes('/api/v1/sources/src-1/scan?async=true') && method === 'POST') {
        scanTriggered = true;
        return jsonResponse({ source_id: 'src-1', running: true }, 202);
      }
      if (url.endsWith('/api/v1/sources/src-1/scan/status') && method === 'GET') {
        if (!scanTriggered) {
          return jsonResponse({ source_id: 'src-1', running: false });
        }
        statusCalls++;
        if (statusCalls === 1) {
          return jsonResponse({ source_id: 'src-1', running: true });
        }
        return jsonResponse({
          source_id: 'src-1',
          running: false,
          last_report: {
            source_id: 'src-1',
            started_at: '2026-11-23T00:00:00Z',
            finished_at: '2026-11-23T00:00:02Z',
            total_videos: 8,
            added_videos: 8,
            updated_videos: 0,
            skipped_videos: 0,
            missing_videos: 1,
            restored_videos: 0,
            probed_videos: 8,
            total_subtitles: 4,
          },
        });
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
                  media_file_id: 'mf-miss',
                  relative_path: 'rust-course/01.mp4',
                  file_name: '01.mp4',
                  title: '01',
                  duration_ms: 10000,
                  sort_order: 1,
                },
              ],
            },
          ],
        });
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    render(
      <MemoryRouter>
        <StoragePage />
      </MemoryRouter>,
    );

    expect((await screen.findAllByText('本地课程盘')).length).toBeGreaterThanOrEqual(1);
    expect(await screen.findByText('rust-course')).toBeInTheDocument();
    expect(screen.getAllByText(/缺失/).length).toBeGreaterThanOrEqual(1);

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /扫描/ }));

    await waitFor(
      () => {
        expect(statusCalls).toBeGreaterThanOrEqual(2);
      },
      { timeout: 4000 },
    );

    // Launch ImportCourseModal from directory row
    await user.click(screen.getByRole('button', { name: /一键导入为课程/ }));
    expect(await screen.findByLabelText(/课程标题/)).toHaveValue('rust-course');
  });

  it('StoragePage automatically resumes polling on load when a scan is already running', async () => {
    const sources = [
      {
        id: 'src-1',
        name: '后台扫描盘',
        driver: 'local',
        root_path: '/data/active',
        config_json: '{}',
        enabled: true,
        media_count: 5,
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
      },
    ];
    let statusCalls = 0;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url.endsWith('/api/v1/sources') && method === 'GET') {
          return jsonResponse({ sources });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({ libraries: sampleLibraries });
        }
        if (url.includes('/api/v1/sources/src-1/dirs') && method === 'GET') {
          return jsonResponse({ dirs: [] });
        }
        if (url.includes('/api/v1/sources/src-1/files') && method === 'GET') {
          return jsonResponse({ files: [] });
        }
        if (url.endsWith('/api/v1/sources/src-1/scan/status') && method === 'GET') {
          statusCalls++;
          if (statusCalls <= 2) {
            return jsonResponse({ source_id: 'src-1', running: true });
          }
          return jsonResponse({
            source_id: 'src-1',
            running: false,
            last_report: {
              source_id: 'src-1',
              started_at: '2026-11-23T00:00:00Z',
              finished_at: '2026-11-23T00:00:01Z',
              total_videos: 10,
              added_videos: 5,
              updated_videos: 0,
              skipped_videos: 5,
              missing_videos: 0,
              restored_videos: 0,
              probed_videos: 5,
              total_subtitles: 2,
            },
          });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <MemoryRouter>
        <StoragePage />
      </MemoryRouter>,
    );

    expect(await screen.findByText('扫描中...')).toBeInTheDocument();
    await waitFor(
      () => {
        expect(screen.getByText('总视频：10')).toBeInTheDocument();
      },
      { timeout: 4000 },
    );
    expect(statusCalls).toBeGreaterThanOrEqual(3);
  });

  it('SettingsPage shows Jellyfin info, saves settings without overwriting masked key, tests AI, and revokes sessions', async () => {
    let savedSettingsPayload: unknown = null;
    let testAIPayload: unknown = null;
    let revokedToken = '';

    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = init?.method ?? 'GET';

      if (url.endsWith('/api/v1/settings') && method === 'GET') {
        return jsonResponse({
          server_name: 'CourseFin Studio',
          scan_interval_minutes: 60,
          base_url_hint: 'http://192.168.1.10:8096',
          jellyfin_username: 'admin',
          ai_base_url: 'https://api.openai.com/v1',
          ai_api_key: '****',
          ai_api_key_set: true,
          ai_model: 'gpt-4o-mini',
          ai_max_subtitle_chars: 4000,
        });
      }
      if (url.endsWith('/api/v1/settings') && method === 'PUT') {
        savedSettingsPayload = JSON.parse(String(init?.body));
        return jsonResponse({
          server_name: 'My Academy',
          scan_interval_minutes: 60,
          base_url_hint: 'http://192.168.1.10:8096',
          jellyfin_username: 'admin',
          ai_base_url: 'https://api.openai.com/v1',
          ai_api_key: '****',
          ai_api_key_set: true,
          ai_model: 'gpt-4o-mini',
          ai_max_subtitle_chars: 4000,
        });
      }
      if (url.endsWith('/api/v1/settings/ai/test') && method === 'POST') {
        testAIPayload = JSON.parse(String(init?.body));
        return jsonResponse({ ok: true });
      }
      if (url.endsWith('/api/v1/sessions') && method === 'GET') {
        return jsonResponse({
          sessions: [
            {
              token: 'sess-infuse-1',
              user_id: 'u1',
              device_id: 'infuse-appletv',
              device_name: 'Living Room Apple TV',
              client_name: 'Infuse',
              client_version: '8.0',
              last_active_at: '2026-11-23T10:00:00Z',
              created_at: '2026-11-22T10:00:00Z',
            },
          ],
        });
      }
      if (url.endsWith('/api/v1/auth/me') && method === 'GET') {
        return jsonResponse({
          user: { id: 'u1', username: 'admin', created_at: '', updated_at: '' },
          session: { token: 'test-token', user_id: 'u1', device_id: 'web', device_name: 'Web', client_name: 'CourseFin Web', client_version: '0.1.0', last_active_at: '', created_at: '' },
        });
      }
      if (url.endsWith('/api/v1/sessions/sess-infuse-1') && method === 'DELETE') {
        revokedToken = 'sess-infuse-1';
        return jsonResponse({ ok: true });
      }
      return jsonResponse({ error: 'not found' }, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    render(
      <MemoryRouter>
        <SettingsPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText('http://192.168.1.10:8096')).toBeInTheDocument();
    expect(screen.getAllByText(/Infuse/).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Living Room Apple TV')).toBeInTheDocument();

    const user = userEvent.setup();
    const serverNameInput = screen.getByLabelText(/服务器名称/);
    await user.clear(serverNameInput);
    await user.type(serverNameInput, 'My Academy');
    await user.click(screen.getByRole('button', { name: /保存设置/ }));

    await waitFor(() => {
      expect(savedSettingsPayload).toBeTruthy();
    });
    // Masked key must NOT be sent as a literal new key
    expect((savedSettingsPayload as Record<string, unknown>).ai_api_key).toBeUndefined();

    await user.click(screen.getByRole('button', { name: /测试连接/ }));
    await waitFor(() => {
      expect(testAIPayload).toBeTruthy();
    });

    const sessionRow = screen.getByText('Living Room Apple TV').closest('tr')!;
    await user.click(within(sessionRow).getByRole('button', { name: /注销/ }));
    await waitFor(() => {
      expect(revokedToken).toBe('sess-infuse-1');
    });
  });

  it('SettingsPage renders ai_max_subtitle_chars = 0 as "0" and submits 0 on save and test', async () => {
    let savedPayload: unknown = null;
    let testPayload: unknown = null;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url.endsWith('/api/v1/settings') && method === 'GET') {
          return jsonResponse({
            server_name: 'CourseFin Studio',
            scan_interval_minutes: 60,
            base_url_hint: 'http://localhost:8096',
            jellyfin_username: 'admin',
            ai_base_url: 'https://api.openai.com/v1',
            ai_api_key: '',
            ai_api_key_set: false,
            ai_model: 'gpt-4o-mini',
            ai_max_subtitle_chars: 0,
          });
        }
        if (url.endsWith('/api/v1/settings') && method === 'PUT') {
          savedPayload = JSON.parse(String(init?.body)) as Record<string, unknown>;
          return jsonResponse({
            server_name: 'CourseFin Studio',
            scan_interval_minutes: 60,
            base_url_hint: 'http://localhost:8096',
            jellyfin_username: 'admin',
            ai_base_url: 'https://api.openai.com/v1',
            ai_api_key: '',
            ai_api_key_set: false,
            ai_model: 'gpt-4o-mini',
            ai_max_subtitle_chars: 0,
          });
        }
        if (url.endsWith('/api/v1/settings/ai/test') && method === 'POST') {
          testPayload = JSON.parse(String(init?.body)) as Record<string, unknown>;
          return jsonResponse({ ok: true });
        }
        if (url.endsWith('/api/v1/sessions') && method === 'GET') {
          return jsonResponse({ sessions: [] });
        }
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
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <MemoryRouter>
        <SettingsPage />
      </MemoryRouter>,
    );

    const subInput = await screen.findByLabelText(/最大字幕上下文字符数/);
    expect(subInput).toHaveValue(0);

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /保存设置/ }));
    await waitFor(() => {
      expect(savedPayload).toBeTruthy();
    });
    expect((savedPayload as Record<string, unknown>).ai_max_subtitle_chars).toBe(0);

    await user.click(screen.getByRole('button', { name: /测试连接/ }));
    await waitFor(() => {
      expect(testPayload).toBeTruthy();
    });
    expect((testPayload as Record<string, unknown>).max_subtitle_chars).toBe(0);
  });
});
