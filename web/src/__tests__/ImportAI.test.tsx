import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import ImportCourseModal from '../components/ImportCourseModal';
import { detectBrowserVideoCodecs } from '../utils/mediaCapabilities';
import type { Library } from '../types/coursefin';

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const LIBRARIES: Library[] = [
  {
    id: 'lib1',
    name: '编程开发',
    cover_path: '',
    sort_order: 1,
    course_count: 1,
    created_at: '2026-11-20T00:00:00Z',
    updated_at: '2026-11-20T00:00:00Z',
  },
];

describe('detectBrowserVideoCodecs', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('includes hevc when canPlayType returns probably for hvc1', () => {
    vi.spyOn(HTMLVideoElement.prototype, 'canPlayType').mockImplementation((type: string) => {
      if (type.includes('avc1') || type.includes('hvc1') || type.includes('vp9') || type.includes('av01')) {
        return 'probably';
      }
      return '';
    });

    expect(detectBrowserVideoCodecs()).toEqual(['h264', 'hevc', 'vp9', 'av1']);
  });

  it('omits hevc when canPlayType only returns maybe or empty', () => {
    vi.spyOn(HTMLVideoElement.prototype, 'canPlayType').mockImplementation((type: string) => {
      if (type.includes('avc1') || type.includes('vp9') || type.includes('av01')) {
        return 'probably';
      }
      if (type.includes('hvc1') || type.includes('hev1')) {
        return 'maybe';
      }
      return '';
    });

    expect(detectBrowserVideoCodecs()).toEqual(['h264', 'vp9', 'av1']);
  });
});

describe('ImportCourseModal AI organize', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('previews AI proposal, applies checked fields to draft preserving durations, supports reset, and never commits early', async () => {
    let commitCalled = false;
    let aiRequestBody: Record<string, unknown> | null = null;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/import/preview' && init?.method === 'POST') {
          return jsonResponse({
            source_id: 'src1',
            dir_rel_path: 'RawGoCourse',
            course_title: 'RawGoCourse_1080p',
            total_files: 2,
            chapters: [
              {
                title: '正片',
                sort_order: 1,
                lessons: [
                  {
                    media_file_id: 'mf1',
                    relative_path: 'RawGoCourse/01_intro.mp4',
                    file_name: '01_intro.mp4',
                    title: '01_intro',
                    duration_ms: 125000,
                    sort_order: 1,
                  },
                  {
                    media_file_id: 'mf2',
                    relative_path: 'RawGoCourse/02_channels.mp4',
                    file_name: '02_channels.mp4',
                    title: '02_channels',
                    duration_ms: 245000,
                    sort_order: 2,
                  },
                ],
              },
            ],
          });
        }
        if (url === '/api/v1/import/ai/organize' && init?.method === 'POST') {
          aiRequestBody = JSON.parse(String(init.body ?? '{}')) as Record<string, unknown>;
          return jsonResponse({
            title: 'Go 并发编程实战',
            overview: 'AI 生成的课程简介',
            tags: ['Go', '并发'],
            chapters: [
              {
                title: '第二部分：进阶',
                lessons: [{ media_file_id: 'mf2', title: 'Channel 深入解析' }],
              },
              {
                title: '第一部分：基础',
                lessons: [{ media_file_id: 'mf1', title: '课程导论' }],
              },
            ],
          });
        }
        if (url === '/api/v1/import/commit') {
          commitCalled = true;
          return jsonResponse({}, 201);
        }
        return jsonResponse({}, 404);
      }),
    );

    const onClose = vi.fn();
    const { container } = render(
      <MemoryRouter>
        <ImportCourseModal
          isOpen
          sourceId="src1"
          dirRelPath="RawGoCourse"
          libraries={LIBRARIES}
          onClose={onClose}
          onImported={vi.fn()}
        />
      </MemoryRouter>,
    );

    const titleInput = (await screen.findByLabelText('课程标题')) as HTMLInputElement;
    expect(titleInput.value).toBe('RawGoCourse_1080p');

    const user = userEvent.setup();
    const aiBtn = screen.getByRole('button', { name: /AI 智能整理/ });
    await user.click(aiBtn);

    expect(await screen.findByText('Go 并发编程实战')).toBeInTheDocument();
    expect(aiRequestBody).toMatchObject({
      source_id: 'src1',
      dir_rel_path: 'RawGoCourse',
    });

    // Uncheck overview so it stays empty
    const overviewCheckbox = screen.getByLabelText('应用建议简介') as HTMLInputElement;
    await user.click(overviewCheckbox);
    expect(overviewCheckbox.checked).toBe(false);

    await user.click(screen.getByRole('button', { name: /应用到导入表单/ }));

    await waitFor(() => {
      expect(titleInput.value).toBe('Go 并发编程实战');
    });
    const overviewInput = screen.getByLabelText('课程简介（可选）') as HTMLTextAreaElement;
    expect(overviewInput.value).toBe('');
    expect(screen.getByText('Go')).toBeInTheDocument();
    expect(screen.getByText('并发')).toBeInTheDocument();

    // Regrouped lessons keep accurate durations via media_file_id lookup
    expect(screen.getByDisplayValue('Channel 深入解析')).toBeInTheDocument();
    expect(screen.getByText('4:05')).toBeInTheDocument();
    expect(screen.getByDisplayValue('课程导论')).toBeInTheDocument();
    expect(screen.getByText('2:05')).toBeInTheDocument();
    expect(commitCalled).toBe(false);

    // Reset restores initial preview values
    await user.click(screen.getByRole('button', { name: /恢复初始识别/ }));
    expect(titleInput.value).toBe('RawGoCourse_1080p');
    expect(screen.getByDisplayValue('01_intro')).toBeInTheDocument();

    // Ensure no legacy palette classes
    expect(container.innerHTML).not.toMatch(/\b(slate|indigo|rose|emerald)-/);
  });

  it('shows settings link and retry button when AI provider is not configured and closes only AI overlay on Escape', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/import/preview' && init?.method === 'POST') {
          return jsonResponse({
            source_id: 'src1',
            dir_rel_path: 'Demo',
            course_title: 'Demo',
            total_files: 1,
            chapters: [
              {
                title: '正片',
                sort_order: 1,
                lessons: [
                  {
                    media_file_id: 'mf1',
                    relative_path: 'Demo/01.mp4',
                    file_name: '01.mp4',
                    title: '01',
                    duration_ms: 60000,
                    sort_order: 1,
                  },
                ],
              },
            ],
          });
        }
        if (url === '/api/v1/import/ai/organize' && init?.method === 'POST') {
          return jsonResponse({ error: 'AI provider is not configured' }, 400);
        }
        return jsonResponse({}, 404);
      }),
    );

    const onClose = vi.fn();
    render(
      <MemoryRouter>
        <ImportCourseModal
          isOpen
          sourceId="src1"
          dirRelPath="Demo"
          libraries={LIBRARIES}
          onClose={onClose}
          onImported={vi.fn()}
        />
      </MemoryRouter>,
    );

    await screen.findByLabelText('课程标题');
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /AI 智能整理/ }));

    expect(await screen.findByText('AI provider is not configured')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '系统设置' })).toHaveAttribute('href', '/settings');
    expect(screen.getAllByRole('button', { name: /重新生成/ }).length).toBeGreaterThanOrEqual(1);

    // Escape closes AI overlay first, NOT the parent ImportCourseModal
    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByText('AI provider is not configured')).not.toBeInTheDocument();
    });
    expect(onClose).not.toHaveBeenCalled();
  });
});
