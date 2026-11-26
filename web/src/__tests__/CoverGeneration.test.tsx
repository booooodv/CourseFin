import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import CoverStylePicker, { FALLBACK_COVER_STYLES } from '../components/CoverStylePicker';
import { ToastProvider } from '../components/Toast';
import CourseEditorPage from '../pages/CourseEditorPage';
import HomePage from '../pages/HomePage';
import SettingsPage from '../pages/SettingsPage';
import type { CourseDetail, CourseSummary } from '../types/coursefin';

const LEGACY_CLASS_REGEX = /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald|sky)-/;

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const sampleCourseDetail: CourseDetail = {
  id: 'course-1',
  library_id: 'lib-1',
  title: 'Go 语言并发编程实战',
  sort_title: 'Go 语言并发编程实战',
  overview: '系统讲解 Goroutine 与 Channel 并发模式',
  instructor: '陈默',
  cover_path: '',
  backdrop_path: '',
  source_id: 'src-1',
  source_root_path: '/courses/go',
  tags: ['Go', '并发'],
  chapter_count: 2,
  lesson_count: 4,
  played_count: 0,
  unplayed_item_count: 4,
  played_percentage: 0,
  played: false,
  total_duration_ms: 1200000,
  first_lesson_id: 'lesson-1',
  created_at: '2026-11-26T00:00:00Z',
  updated_at: '2026-11-26T00:00:00Z',
  chapters: [
    {
      id: 'chap-1',
      course_id: 'course-1',
      title: '第一章 并发基础',
      sort_order: 1,
      overview: '',
      lesson_count: 1,
      played_count: 0,
      unplayed_item_count: 1,
      played_percentage: 0,
      played: false,
      created_at: '2026-11-26T00:00:00Z',
      updated_at: '2026-11-26T00:00:00Z',
      lessons: [],
    },
  ],
};

describe('AI Course Cover Generation', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token-cover');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('CoverStylePicker supports hover & touch/click sample image preview and semantic styling', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();

    const { container } = render(
      <CoverStylePicker
        styles={FALLBACK_COVER_STYLES}
        value="editorial_modern"
        defaultStyleId="editorial_modern"
        onChange={onChange}
      />,
    );

    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);

    const darkTechRadio = screen.getByRole('radio', { name: /暗夜科技/ });
    await user.hover(darkTechRadio);

    const hoverImg = await screen.findByAltText('暗夜科技 参考封面');
    expect(hoverImg).toHaveAttribute('src', '/web/cover-styles/dark_tech.jpg');

    await user.unhover(darkTechRadio);
    await waitFor(() => {
      expect(screen.queryByAltText('暗夜科技 参考封面')).not.toBeInTheDocument();
    });

    const previewBtn = screen.getByRole('button', { name: '预览 极简蓝图 样图' });
    await user.click(previewBtn);

    const pinnedDialog = await screen.findByRole('dialog', { name: '极简蓝图 样图预览' });
    expect(within(pinnedDialog).getByAltText('极简蓝图 参考封面')).toHaveAttribute(
      'src',
      '/web/cover-styles/blueprint_grid.jpg',
    );
    expect(pinnedDialog.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);

    await user.click(within(pinnedDialog).getByRole('button', { name: '关闭样图预览' }));
    expect(screen.queryByRole('dialog', { name: '极简蓝图 样图预览' })).not.toBeInTheDocument();

    await user.click(darkTechRadio);
    expect(onChange).toHaveBeenCalledWith('dark_tech');
  });

  it('SettingsPage configures, tests, clears key, and saves AI image generation settings', async () => {
    const user = userEvent.setup();
    let testPayload: Record<string, unknown> | null = null;
    let savedPayload: Record<string, unknown> | null = null;

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = (init?.method ?? 'GET').toUpperCase();

      if (url.endsWith('/api/v1/settings') && method === 'GET') {
        return jsonResponse({
          server_name: 'CourseFin',
          scan_interval_minutes: 60,
          base_url_hint: 'http://localhost:8096',
          jellyfin_username: 'admin',
          ai_base_url: 'https://api.openai.com/v1',
          ai_api_key: '****',
          ai_api_key_set: true,
          ai_model: 'gpt-4o-mini',
          ai_max_subtitle_chars: 4000,
          ai_image_protocol: 'openai_images',
          ai_image_base_url: 'https://img.example.com/v1',
          ai_image_api_key: '****',
          ai_image_api_key_set: true,
          ai_image_model: 'gpt-image-1',
          ai_image_size: '1536x1024',
          ai_image_configured: true,
          ai_cover_default_style: 'editorial_modern',
        });
      }
      if (url.endsWith('/api/v1/cover/styles')) {
        return jsonResponse({
          styles: FALLBACK_COVER_STYLES,
          default_style_id: 'editorial_modern',
          ai_image_configured: true,
        });
      }
      if (url.endsWith('/api/v1/sessions')) {
        return jsonResponse({ sessions: [] });
      }
      if (url.endsWith('/api/v1/auth/me')) {
        return jsonResponse({ user: { id: 'u1', username: 'admin' }, session: { token: 't1' } });
      }
      if (url.endsWith('/api/v1/system/update')) {
        return jsonResponse({
          current_version: '1.4.0',
          latest_version: '1.4.0',
          update_available: false,
          deploy_mode: 'binary',
          platform: 'darwin/arm64',
          can_apply: false,
          cannot_apply_reason: '',
          auto_check_enabled: false,
          auto_check_locked_by_env: false,
          check_interval_hours: 24,
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
      if (url.endsWith('/api/v1/settings/ai/image/test') && method === 'POST') {
        testPayload = JSON.parse(String(init?.body ?? '{}'));
        return jsonResponse({ ok: true });
      }
      if (url.endsWith('/api/v1/settings') && method === 'PUT') {
        savedPayload = JSON.parse(String(init?.body ?? '{}'));
        return jsonResponse({
          server_name: 'CourseFin',
          scan_interval_minutes: 60,
          base_url_hint: 'http://localhost:8096',
          jellyfin_username: 'admin',
          ai_base_url: 'https://api.openai.com/v1',
          ai_api_key: '****',
          ai_api_key_set: true,
          ai_model: 'gpt-4o-mini',
          ai_max_subtitle_chars: 4000,
          ai_image_protocol: savedPayload?.ai_image_protocol ?? 'gemini',
          ai_image_base_url: savedPayload?.ai_image_base_url ?? '',
          ai_image_api_key: '',
          ai_image_api_key_set: false,
          ai_image_model: savedPayload?.ai_image_model ?? 'gemini-2.5-flash-image',
          ai_image_size: savedPayload?.ai_image_size ?? '1792x1024',
          ai_image_configured: true,
          ai_cover_default_style: savedPayload?.ai_cover_default_style ?? 'blueprint_grid',
        });
      }
      return jsonResponse({});
    });

    const { container } = render(
      <MemoryRouter>
        <ToastProvider>
          <SettingsPage />
        </ToastProvider>
      </MemoryRouter>,
    );

    const protocolSelect = await screen.findByLabelText('生图接口协议');
    expect(protocolSelect).toHaveValue('openai_images');

    await user.selectOptions(protocolSelect, 'gemini');
    const modelInput = screen.getByLabelText('生图模型名称');
    await user.clear(modelInput);
    await user.type(modelInput, 'gemini-2.5-flash-image');

    await user.click(screen.getByRole('button', { name: '1792x1024' }));
    const clearImgKey = document.getElementById('settings-image-clear-key') as HTMLInputElement;
    await user.click(clearImgKey);

    const styleGroup = screen.getByRole('radiogroup', { name: '全局默认封面风格' });
    await user.click(within(styleGroup).getByRole('radio', { name: /极简蓝图/ }));

    await user.click(screen.getByRole('button', { name: '测试生图接口' }));
    await screen.findByText('AI 封面生图接口测试成功！');
    expect(testPayload).toMatchObject({
      image_protocol: 'gemini',
      image_model: 'gemini-2.5-flash-image',
      image_size: '1792x1024',
      clear_ai_image_api_key: true,
    });

    await user.click(screen.getByRole('button', { name: '保存设置' }));
    await waitFor(() => {
      expect(savedPayload).toMatchObject({
        ai_image_protocol: 'gemini',
        ai_image_model: 'gemini-2.5-flash-image',
        ai_image_size: '1792x1024',
        ai_cover_default_style: 'blueprint_grid',
        clear_ai_image_api_key: true,
      });
    });

    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);
  });

  it('CourseEditorPage + AICoverModal previews and applies AI generated cover without premature overwrite', async () => {
    const user = userEvent.setup();
    let generateBody: Record<string, unknown> | null = null;
    let applyBody: Record<string, unknown> | null = null;

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = (init?.method ?? 'GET').toUpperCase();

      if (url.endsWith('/api/v1/courses/course-1') && method === 'GET') {
        return jsonResponse(sampleCourseDetail);
      }
      if (url.endsWith('/api/v1/libraries')) {
        return jsonResponse({
          libraries: [
            {
              id: 'lib-1',
              name: '我的课程',
              cover_path: '',
              sort_order: 1,
              course_count: 1,
              created_at: '2026-11-26T00:00:00Z',
              updated_at: '2026-11-26T00:00:00Z',
            },
          ],
        });
      }
      if (url.endsWith('/api/v1/cover/styles')) {
        return jsonResponse({
          styles: FALLBACK_COVER_STYLES,
          default_style_id: 'dark_tech',
          ai_image_configured: true,
        });
      }
      if (url.endsWith('/api/v1/courses/course-1/cover/generate') && method === 'POST') {
        generateBody = JSON.parse(String(init?.body ?? '{}'));
        return jsonResponse({
          preview_id: 'prev123',
          course_id: 'course-1',
          style_id: 'clay_3d',
          style_name: '柔和 3D 黏土',
          preview_url: '/api/v1/cover/previews/prev123',
          prompt: 'Style: 柔和 3D 黏土 ...',
          width: 1536,
          height: 1024,
          created_at: '2026-11-26T00:01:00Z',
        });
      }
      if (url.endsWith('/api/v1/courses/course-1/cover/apply') && method === 'POST') {
        applyBody = JSON.parse(String(init?.body ?? '{}'));
        return jsonResponse({
          ...sampleCourseDetail,
          cover_path: 'course-1_cover.png',
          updated_at: '2026-11-26T00:02:00Z',
        });
      }
      return jsonResponse({});
    });

    render(
      <MemoryRouter initialEntries={['/courses/course-1/edit']}>
        <ToastProvider>
          <Routes>
            <Route path="/courses/:courseId/edit" element={<CourseEditorPage />} />
          </Routes>
        </ToastProvider>
      </MemoryRouter>,
    );

    const openBtn = await screen.findByRole('button', { name: 'AI 生成封面' });
    await user.click(openBtn);

    const dialog = await screen.findByRole('dialog', { name: 'AI 生成课程封面' });
    expect(dialog.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);

    await waitFor(() => {
      expect(within(dialog).getByRole('radio', { name: /暗夜科技/ })).toHaveAttribute(
        'aria-checked',
        'true',
      );
    });

    await user.click(within(dialog).getByRole('radio', { name: /柔和 3D 黏土/ }));
    const titleInput = within(dialog).getByLabelText('封面主标题');
    await user.clear(titleInput);
    await user.type(titleInput, 'Go 并发实战进阶');

    await user.click(within(dialog).getByRole('button', { name: '生成预览' }));

    const previewImg = await within(dialog).findByAltText('Go 语言并发编程实战 AI 封面预览');
    expect(previewImg).toHaveAttribute(
      'src',
      '/api/v1/cover/previews/prev123?api_key=test-token-cover',
    );
    expect(generateBody).toMatchObject({
      style_id: 'clay_3d',
      title: 'Go 并发实战进阶',
      subtitle: '讲师 · 陈默',
      include_badge: true,
      badge: '4 节 · 2 章',
    });
    expect(applyBody).toBeNull();

    await user.click(within(dialog).getByRole('button', { name: '应用为课程封面' }));
    await waitFor(() => {
      expect(applyBody).toEqual({ preview_id: 'prev123' });
      expect(screen.queryByRole('dialog', { name: 'AI 生成课程封面' })).not.toBeInTheDocument();
    });
    expect((await screen.findAllByText('已应用 AI 生成封面')).length).toBeGreaterThanOrEqual(1);
  });

  it('HomePage + BatchCoverModal starts batch generation, polls progress, refreshes cards, and supports cancel', async () => {
    const user = userEvent.setup();
    let batchPollCount = 0;
    let cancelCalled = false;
    let startBody: Record<string, unknown> | null = null;

    const courses: CourseSummary[] = [
      {
        ...sampleCourseDetail,
        id: 'c-has-cover',
        title: '已有封面课程',
        cover_path: 'c-has-cover_cover.jpg',
      },
      {
        ...sampleCourseDetail,
        id: 'c-missing',
        title: '待生成封面课程',
        cover_path: '',
      },
    ];

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = (init?.method ?? 'GET').toUpperCase();

      if (url.endsWith('/api/v1/home')) {
        return jsonResponse({
          resume: [],
          next_up: [],
          libraries: [
            {
              id: 'lib-1',
              name: '编程开发',
              cover_path: '',
              sort_order: 1,
              course_count: 2,
              created_at: '2026-11-26T00:00:00Z',
              updated_at: '2026-11-26T00:00:00Z',
            },
          ],
          courses,
        });
      }
      if (url.endsWith('/api/v1/courses')) {
        return jsonResponse({ courses });
      }
      if (url.endsWith('/api/v1/libraries')) {
        return jsonResponse({
          libraries: [
            {
              id: 'lib-1',
              name: '编程开发',
              cover_path: '',
              sort_order: 1,
              course_count: 2,
              created_at: '2026-11-26T00:00:00Z',
              updated_at: '2026-11-26T00:00:00Z',
            },
          ],
        });
      }
      if (url.endsWith('/api/v1/tags')) {
        return jsonResponse({ tags: [] });
      }
      if (url.endsWith('/api/v1/cover/styles')) {
        return jsonResponse({
          styles: FALLBACK_COVER_STYLES,
          default_style_id: 'editorial_modern',
          ai_image_configured: true,
        });
      }
      if (url.endsWith('/api/v1/courses/covers/batch/status')) {
        batchPollCount += 1;
        if (batchPollCount === 1) {
          return jsonResponse({
            running: false,
            mode: '',
            style_id: '',
            state: 'idle',
            total: 0,
            completed: 0,
            skipped: 0,
            failed: 0,
            current_course_id: '',
            current_course_title: '',
            errors: [],
          });
        }
        return jsonResponse({
          running: !cancelCalled,
          mode: 'missing_only',
          style_id: 'warm_illustration',
          state: cancelCalled ? 'cancelled' : 'running',
          total: 2,
          completed: 0,
          skipped: 1,
          failed: 0,
          current_course_id: 'c-missing',
          current_course_title: '待生成封面课程',
          errors: [],
        });
      }
      if (url.endsWith('/api/v1/courses/covers/batch/generate') && method === 'POST') {
        startBody = JSON.parse(String(init?.body ?? '{}'));
        return jsonResponse(
          {
            running: true,
            mode: 'missing_only',
            style_id: 'warm_illustration',
            state: 'running',
            total: 2,
            completed: 0,
            skipped: 1,
            failed: 0,
            current_course_id: 'c-missing',
            current_course_title: '待生成封面课程',
            errors: [],
          },
          202,
        );
      }
      if (url.endsWith('/api/v1/courses/covers/batch/cancel') && method === 'POST') {
        cancelCalled = true;
        return jsonResponse({
          running: false,
          mode: 'missing_only',
          style_id: 'warm_illustration',
          state: 'cancelled',
          total: 2,
          completed: 0,
          skipped: 1,
          failed: 0,
          current_course_id: '',
          current_course_title: '',
          errors: [],
        });
      }
      return jsonResponse({});
    });

    render(
      <MemoryRouter>
        <ToastProvider>
          <HomePage />
        </ToastProvider>
      </MemoryRouter>,
    );

    const batchBtn = await screen.findByRole('button', { name: 'AI 批量封面' });
    await user.click(batchBtn);

    const dialog = await screen.findByRole('dialog', { name: 'AI 批量生成课程封面' });
    expect(dialog.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);
    expect(within(dialog).getByText(/仅补齐缺失封面/)).toBeInTheDocument();

    await user.click(within(dialog).getByRole('radio', { name: /温暖扁平插画/ }));
    await user.click(within(dialog).getByRole('button', { name: /开始批量生成 \(1 门\)/ }));

    await waitFor(() => {
      expect(startBody).toMatchObject({
        mode: 'missing_only',
        style_id: 'warm_illustration',
        include_badge: true,
      });
    });

    const cancelBtn = await within(dialog).findByRole('button', { name: '取消批量任务' });
    await user.click(cancelBtn);

    await waitFor(() => {
      expect(cancelCalled).toBe(true);
      expect(within(dialog).getByText('批量任务已取消')).toBeInTheDocument();
    });
  });
});
