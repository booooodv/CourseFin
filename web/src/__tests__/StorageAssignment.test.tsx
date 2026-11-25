import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import StoragePage from '../pages/StoragePage';
import type { IndexedDir, MediaFile, StorageSource } from '../types/coursefin';

const LEGACY_CLASS_REGEX = /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald)-/;

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('StoragePage course assignment indicators', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('displays unassigned counts, scan subtitle match stats, dir/file course links, assigned filter, and refreshes after import', async () => {
    let imported = false;
    const fileFetchUrls: string[] = [];

    const makeSources = (): StorageSource[] => [
      {
        id: 'src-1',
        name: '主课程盘',
        driver: 'local',
        root_path: '/courses/main',
        config_json: '{}',
        enabled: true,
        media_count: 5,
        assigned_count: imported ? 5 : 3,
        unassigned_count: imported ? 0 : 2,
        last_scanned_at: '2026-11-25T10:00:00Z',
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-25T10:00:00Z',
      },
      {
        id: 'src-2',
        name: '归档课程盘',
        driver: 'local',
        root_path: '/courses/archive',
        config_json: '{}',
        enabled: true,
        media_count: 4,
        assigned_count: 4,
        unassigned_count: 0,
        last_scanned_at: '2026-11-25T09:00:00Z',
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-25T09:00:00Z',
      },
    ];

    const makeDirs = (): IndexedDir[] => [
      {
        name: 'go-course',
        rel_path: 'go-course',
        file_count: 2,
        direct_files: 2,
        assigned_count: 2,
        unassigned_count: 0,
        courses: [{ id: 'course-go', title: 'Go 实战' }],
        has_subdirs: false,
      },
      {
        name: 'rust-course',
        rel_path: 'rust-course',
        file_count: 2,
        direct_files: 2,
        assigned_count: 1,
        unassigned_count: 1,
        courses: [{ id: 'course-rust', title: 'Rust 入门' }],
        has_subdirs: false,
      },
      {
        name: 'new-course',
        rel_path: 'new-course',
        file_count: 1,
        direct_files: 1,
        assigned_count: imported ? 1 : 0,
        unassigned_count: imported ? 0 : 1,
        courses: imported ? [{ id: 'course-new', title: '全新课程' }] : [],
        has_subdirs: false,
      },
    ];

    const allFiles: MediaFile[] = [
      {
        id: 'mf-1',
        source_id: 'src-1',
        relative_path: 'go-course/01.mp4',
        file_name: '01.mp4',
        file_size: 1024 * 1024,
        mod_time: '2026-11-25T00:00:00Z',
        status: 'active',
        duration_ms: 60000,
        container: 'mp4',
        video_codec: 'h264',
        audio_codec: 'aac',
        width: 1920,
        height: 1080,
        bitrate: 1000000,
        thumbnail_path: '',
        assigned: true,
        courses: [
          { id: 'course-go', title: 'Go 实战' },
          { id: 'course-arch', title: '架构合集' },
        ],
        created_at: '2026-11-25T00:00:00Z',
        updated_at: '2026-11-25T00:00:00Z',
      },
      {
        id: 'mf-2',
        source_id: 'src-1',
        relative_path: 'new-course/intro.mp4',
        file_name: 'intro.mp4',
        file_size: 2048 * 1024,
        mod_time: '2026-11-25T00:00:00Z',
        status: 'active',
        duration_ms: 90000,
        container: 'mp4',
        video_codec: 'h264',
        audio_codec: 'aac',
        width: 1920,
        height: 1080,
        bitrate: 1000000,
        thumbnail_path: '',
        assigned: false,
        courses: [],
        created_at: '2026-11-25T00:00:00Z',
        updated_at: '2026-11-25T00:00:00Z',
      },
    ];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/sources') && method === 'GET') {
          return jsonResponse({ sources: makeSources() });
        }
        if (url.endsWith('/api/v1/libraries') && method === 'GET') {
          return jsonResponse({
            libraries: [
              {
                id: 'lib-1',
                name: '编程开发',
                cover_path: '',
                sort_order: 1,
                course_count: 2,
                created_at: '2026-11-20T00:00:00Z',
                updated_at: '2026-11-20T00:00:00Z',
              },
            ],
          });
        }
        if (url.endsWith('/api/v1/sources/src-1/scan/status') && method === 'GET') {
          return jsonResponse({
            source_id: 'src-1',
            running: false,
            last_report: {
              source_id: 'src-1',
              total_videos: 5,
              added_videos: 1,
              updated_videos: 0,
              missing_videos: 0,
              total_subtitles: 5,
              matched_subtitles: 4,
              duration_ms: 120,
            },
          });
        }
        if (url.endsWith('/api/v1/sources/src-2/scan/status') && method === 'GET') {
          return jsonResponse({
            source_id: 'src-2',
            running: false,
          });
        }
        if (url.includes('/api/v1/sources/src-1/dirs') && method === 'GET') {
          return jsonResponse({ dirs: makeDirs() });
        }
        if (url.includes('/api/v1/sources/src-1/files') && method === 'GET') {
          fileFetchUrls.push(url);
          const parsed = new URL(url, 'http://localhost');
          const assignedParam = parsed.searchParams.get('assigned');
          const filtered =
            assignedParam === 'false'
              ? allFiles.filter((f) => !f.assigned)
              : assignedParam === 'true'
                ? allFiles.filter((f) => f.assigned)
                : allFiles;
          return jsonResponse({ files: filtered });
        }
        if (url.endsWith('/api/v1/import/preview') && method === 'POST') {
          return jsonResponse({
            source_id: 'src-1',
            dir_rel_path: 'new-course',
            course_title: '全新课程',
            cover_rel_path: '',
            total_files: 1,
            chapters: [
              {
                title: '默认章节',
                sort_order: 1,
                lessons: [
                  {
                    media_file_id: 'mf-2',
                    relative_path: 'new-course/intro.mp4',
                    file_name: 'intro.mp4',
                    title: 'intro',
                    duration_ms: 90000,
                    sort_order: 1,
                  },
                ],
              },
            ],
          });
        }
        if (url.endsWith('/api/v1/import/commit') && method === 'POST') {
          imported = true;
          return jsonResponse(
            {
              id: 'course-new',
              library_id: 'lib-1',
              title: '全新课程',
              sort_title: '全新课程',
              overview: '',
              instructor: '',
              cover_path: '',
              backdrop_path: '',
              tags: [],
              chapter_count: 1,
              lesson_count: 1,
              played_count: 0,
              unplayed_item_count: 1,
              played_percentage: 0,
              played: false,
              total_duration_ms: 90000,
              first_lesson_id: 'l-new',
              created_at: '2026-11-25T00:00:00Z',
              updated_at: '2026-11-25T00:00:00Z',
              chapters: [],
            },
            201,
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const { container } = render(
      <MemoryRouter>
        <StoragePage />
      </MemoryRouter>,
    );

    // Source cards show unassigned count badge & all-assigned badge
    expect((await screen.findAllByText('主课程盘')).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText(/未入课 2 个/).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('全部已入课')).toBeInTheDocument();

    // Scan report displays matched subtitles count
    expect(await screen.findByText(/字幕：5（已匹配 4）/)).toBeInTheDocument();

    // Subdirectory badges & course links
    expect(screen.getByText('已全部入课')).toBeInTheDocument();
    expect(screen.getByText('部分已入课')).toBeInTheDocument();
    const goCourseLinks = screen.getAllByRole('link', { name: /Go 实战/ });
    expect(goCourseLinks[0]).toHaveAttribute('href', '/courses/course-go');

    // Fully assigned dir import button is de-emphasized (secondary style, not bg-accent)
    const goRow = screen.getByText('go-course').closest('div.justify-between')!;
    const goImportBtn = within(goRow as HTMLElement).getByRole('button', {
      name: /一键导入为课程/,
    });
    expect(goImportBtn.className).not.toContain('bg-accent ');

    // Multi-course file shows +1 indicator
    expect(screen.getByText('+1')).toBeInTheDocument();

    // Zero legacy palette classes
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);

    // Filter files by 未入课 -> sends assigned=false
    const user = userEvent.setup();
    const assignFilterGroup = screen.getByRole('group', { name: '按入课状态筛选' });
    await user.click(within(assignFilterGroup).getByRole('button', { name: '未入课' }));

    await waitFor(() => {
      expect(fileFetchUrls.some((u) => u.includes('assigned=false'))).toBe(true);
    });

    // Import new-course -> refreshes sources & dir badges automatically
    const newCourseRow = screen.getByText('new-course').closest('div.justify-between')!;
    await user.click(
      within(newCourseRow as HTMLElement).getByRole('button', { name: /一键导入为课程/ }),
    );
    await screen.findByLabelText(/课程标题/);
    await user.click(screen.getByRole('button', { name: /确认导入/ }));

    // After import refresh, src-1 now also becomes 全部已入课 (2 cards show 全部已入课)
    await waitFor(() => {
      expect(screen.getAllByText('全部已入课')).toHaveLength(2);
    });
  });
});
