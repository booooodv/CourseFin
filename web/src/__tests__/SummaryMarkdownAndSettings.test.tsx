import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ApiError, api } from '../api/client';
import { ToastProvider } from '../components/Toast';
import SettingsPage from '../pages/SettingsPage';
import type { LessonSummary } from '../types/coursefin';
import { formatLessonSummaryMarkdown, formatTimestamp } from '../utils/summaryMarkdown';

const LEGACY_CLASS_REGEX = /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald|sky)-/;

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('summaryMarkdown utility', () => {
  it('formatTimestamp formats seconds cleanly and clamps negative or non-finite inputs', () => {
    expect(formatTimestamp(0)).toBe('00:00');
    expect(formatTimestamp(330)).toBe('05:30');
    expect(formatTimestamp(3725)).toBe('1:02:05');
    expect(formatTimestamp(59.9)).toBe('00:59');
    expect(formatTimestamp(-42)).toBe('00:00');
    expect(formatTimestamp(Number.NaN)).toBe('00:00');
  });

  it('formatLessonSummaryMarkdown renders exact Spec 5.7 Markdown when has_timestamps is true', () => {
    const summary: LessonSummary = {
      lesson_id: 'lesson-1',
      media_file_id: 'mf-1',
      source_type: 'external_subtitle',
      source_label: '外挂字幕 (zh)',
      has_timestamps: true,
      model: 'gpt-4o-mini',
      overview: '本节讲解 Go 并发模型的核心原语与调度思想。',
      outline: [
        {
          title: 'Goroutine 调度基础',
          summary: '介绍 GMP 模型与用户态协程切换成本。',
          start_sec: 0,
        },
        {
          title: 'Channel 通信模式',
          summary: '演示无缓冲与有缓冲通道的同步语义。',
          start_sec: 330,
        },
        {
          title: '长时任务上下文控制',
          summary: '使用 context.WithCancel 防止协程泄漏。',
          start_sec: 3725,
        },
      ],
      timeline: [
        {
          start_sec: 0,
          end_sec: 330,
          title: 'Goroutine 启动与 GMP 模型',
          summary: '对比系统线程与 Goroutine 的内存栈与调度开销。',
          key_points: ['初始栈仅 2KB 并按需扩缩容', 'M:N 调度由 Go runtime 负责'],
        },
        {
          start_sec: 330,
          end_sec: 3725,
          title: 'Channel 与 select 多路复用',
          summary: '通过通信共享内存，并结合 select 处理超时。',
          key_points: ['向已关闭 channel 发送会 panic', 'select default 分支实现非阻塞读写'],
        },
      ],
      created_at: '2026-11-25T00:00:00Z',
      updated_at: '2026-11-25T00:00:00Z',
    };

    const md = formatLessonSummaryMarkdown({
      courseTitle: 'Go 并发编程实战',
      chapterTitle: '第二章 并发核心',
      lessonTitle: '第 1 节：Goroutine 与 Channel',
      summary,
    });

    expect(md).toBe(
      [
        '# 第 1 节：Goroutine 与 Channel',
        '',
        '> 课程：Go 并发编程实战 · 章节：第二章 并发核心',
        '',
        '## AI 总结视频大纲',
        '',
        '本节讲解 Go 并发模型的核心原语与调度思想。',
        '',
        '1. **Goroutine 调度基础**（`00:00`）：介绍 GMP 模型与用户态协程切换成本。',
        '2. **Channel 通信模式**（`05:30`）：演示无缓冲与有缓冲通道的同步语义。',
        '3. **长时任务上下文控制**（`1:02:05`）：使用 context.WithCancel 防止协程泄漏。',
        '',
        '## AI 总结视频内容（按时间顺序）',
        '',
        '### [00:00 - 05:30] Goroutine 启动与 GMP 模型',
        '',
        '对比系统线程与 Goroutine 的内存栈与调度开销。',
        '',
        '- 初始栈仅 2KB 并按需扩缩容',
        '- M:N 调度由 Go runtime 负责',
        '',
        '### [05:30 - 1:02:05] Channel 与 select 多路复用',
        '',
        '通过通信共享内存，并结合 select 处理超时。',
        '',
        '- 向已关闭 channel 发送会 panic',
        '- select default 分支实现非阻塞读写',
      ].join('\n'),
    );
  });

  it('formatLessonSummaryMarkdown omits timestamps cleanly when has_timestamps is false or timestamps are null', () => {
    const summaryNoTs: LessonSummary = {
      lesson_id: 'lesson-2',
      media_file_id: 'mf-2',
      source_type: 'manual',
      source_label: '手动文稿',
      has_timestamps: false,
      model: 'gpt-4o-mini',
      overview: '梳理 HTTP 服务优雅停机的完整流程。',
      outline: [
        {
          title: '信号监听',
          summary: '捕获 SIGINT 与 SIGTERM。',
          start_sec: 12,
        },
      ],
      timeline: [
        {
          start_sec: 0,
          end_sec: 120,
          title: '信号监听与 Shutdown 调用',
          summary: '阻止新连接接入并等待存量请求结束。',
          key_points: ['server.Shutdown 接收带超时的 context'],
        },
      ],
      created_at: '2026-11-25T00:00:00Z',
      updated_at: '2026-11-25T00:00:00Z',
    };

    const mdNoTs = formatLessonSummaryMarkdown({
      courseTitle: 'Go Web 服务架构',
      chapterTitle: '第三章 生产实践',
      lessonTitle: '优雅停机指南',
      summary: summaryNoTs,
    });

    expect(mdNoTs).toBe(
      [
        '# 优雅停机指南',
        '',
        '> 课程：Go Web 服务架构 · 章节：第三章 生产实践',
        '',
        '## AI 总结视频大纲',
        '',
        '梳理 HTTP 服务优雅停机的完整流程。',
        '',
        '1. **信号监听**：捕获 SIGINT 与 SIGTERM。',
        '',
        '## AI 总结视频内容（按时间顺序）',
        '',
        '### 信号监听与 Shutdown 调用',
        '',
        '阻止新连接接入并等待存量请求结束。',
        '',
        '- server.Shutdown 接收带超时的 context',
      ].join('\n'),
    );

    const summaryNullFields: LessonSummary = {
      ...summaryNoTs,
      has_timestamps: true,
      outline: [{ title: '无时间大纲', summary: '概览描述。', start_sec: null }],
      timeline: [
        {
          start_sec: null,
          end_sec: null,
          title: '无时间段落',
          summary: '段落内容。',
          key_points: ['要点 A'],
        },
      ],
    };

    const mdNullFields = formatLessonSummaryMarkdown({
      courseTitle: 'Go Web 服务架构',
      chapterTitle: '第三章 生产实践',
      lessonTitle: '优雅停机指南',
      summary: summaryNullFields,
    });

    expect(mdNullFields).not.toContain('[');
    expect(mdNullFields).not.toContain('（`');
    expect(mdNullFields).toContain('1. **无时间大纲**：概览描述。');
    expect(mdNullFields).toContain('### 无时间段落');
  });
});

describe('API client summary methods & ApiError.code', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('captures optional error code on ApiError and calls summary endpoints correctly', async () => {
    const calls: Array<{ url: string; method: string; body?: string }> = [];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        const body = init?.body ? String(init.body) : undefined;
        calls.push({ url, method, body });

        if (url === '/api/v1/lessons/l-missing/summary/generate' && method === 'POST') {
          return jsonResponse(
            { error: 'asr provider is not configured', code: 'asr_not_configured' },
            400,
          );
        }
        if (url === '/api/v1/lessons/l-1/summary' && method === 'GET') {
          return jsonResponse({
            lesson_id: 'l-1',
            summary: null,
            job: null,
            capabilities: {
              has_subtitle: false,
              has_cached_transcript: false,
              transcript_source: '',
              ai_configured: true,
              asr_configured: false,
              has_ffmpeg: true,
            },
          });
        }
        if (url === '/api/v1/lessons/l-1/summary/generate' && method === 'POST') {
          return jsonResponse(
            {
              lesson_id: 'l-1',
              course_id: 'c-1',
              running: true,
              stage: 'queued',
              message: '等待生成 AI 总结...',
              chunk_index: 0,
              chunk_total: 0,
              source_type: '',
              started_at: '2026-11-25T00:00:00Z',
            },
            202,
          );
        }
        if (url === '/api/v1/lessons/l-1/summary/cancel' && method === 'POST') {
          return jsonResponse({
            lesson_id: 'l-1',
            course_id: 'c-1',
            running: false,
            stage: 'cancelled',
            message: '已取消',
            chunk_index: 0,
            chunk_total: 0,
            source_type: '',
            started_at: '2026-11-25T00:00:00Z',
          });
        }
        if (url.startsWith('/api/v1/lessons/l-1/summary') && method === 'DELETE') {
          return jsonResponse({ ok: true });
        }
        if (url === '/api/v1/courses/c-1/summaries/status' && method === 'GET') {
          return jsonResponse({
            course_id: 'c-1',
            summary_lesson_ids: ['l-1'],
            batch_job: null,
          });
        }
        if (url === '/api/v1/courses/c-1/summaries/generate' && method === 'POST') {
          return jsonResponse(
            {
              course_id: 'c-1',
              running: true,
              total: 3,
              completed: 0,
              skipped: 1,
              failed: 0,
              current_lesson_id: 'l-2',
              current_lesson_title: 'Lesson 2',
              current_stage: 'queued',
              chunk_index: 0,
              chunk_total: 0,
              errors: [],
              started_at: '2026-11-25T00:00:00Z',
            },
            202,
          );
        }
        if (url === '/api/v1/courses/c-1/summaries/cancel' && method === 'POST') {
          return jsonResponse({
            course_id: 'c-1',
            running: false,
            total: 3,
            completed: 0,
            skipped: 1,
            failed: 0,
            current_lesson_id: '',
            current_lesson_title: '',
            current_stage: 'cancelled',
            chunk_index: 0,
            chunk_total: 0,
            errors: [],
            started_at: '2026-11-25T00:00:00Z',
          });
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    let caughtErr: unknown;
    try {
      await api.generateLessonSummary('l-missing');
    } catch (err) {
      caughtErr = err;
    }
    expect(caughtErr).toBeInstanceOf(ApiError);
    expect((caughtErr as ApiError).status).toBe(400);
    expect((caughtErr as ApiError).code).toBe('asr_not_configured');

    const state = await api.getLessonSummary('l-1');
    expect(state.capabilities.has_ffmpeg).toBe(true);

    await api.generateLessonSummary('l-1', { force: true, force_retranscribe: true });
    await api.cancelLessonSummary('l-1');
    await api.deleteLessonSummary('l-1');
    await api.deleteLessonSummary('l-1', true);
    await api.getCourseSummariesStatus('c-1');
    await api.generateCourseSummaries('c-1', 'all');
    await api.cancelCourseSummaries('c-1');

    expect(calls).toContainEqual({
      url: '/api/v1/lessons/l-1/summary/generate',
      method: 'POST',
      body: JSON.stringify({ force: true, force_retranscribe: true }),
    });
    expect(calls).toContainEqual({
      url: '/api/v1/lessons/l-1/summary',
      method: 'DELETE',
      body: undefined,
    });
    expect(calls).toContainEqual({
      url: '/api/v1/lessons/l-1/summary?delete_transcript=true',
      method: 'DELETE',
      body: undefined,
    });
    expect(calls).toContainEqual({
      url: '/api/v1/courses/c-1/summaries/generate',
      method: 'POST',
      body: JSON.stringify({ mode: 'all' }),
    });
  });
});

describe('SettingsPage ASR section', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('renders ASR inputs, updates key-isolation hint dynamically, saves ASR settings, and tests unsaved ASR config', async () => {
    const savedPayloads: Array<Record<string, unknown>> = [];
    const testASRPayloads: Array<Record<string, unknown>> = [];
    let shouldFailASRTest = false;

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
            ai_api_key: '****',
            ai_api_key_set: true,
            ai_model: 'gpt-4o-mini',
            ai_max_subtitle_chars: 4000,
            ai_asr_base_url: '',
            ai_asr_api_key: '****',
            ai_asr_api_key_set: true,
            ai_asr_model: 'whisper-1',
            ai_asr_language: 'zh',
            ai_configured: true,
            ai_asr_configured: true,
          });
        }
        if (url.endsWith('/api/v1/settings') && method === 'PUT') {
          const body = JSON.parse(String(init?.body ?? '{}')) as Record<string, unknown>;
          savedPayloads.push(body);
          return jsonResponse({
            server_name: 'CourseFin Studio',
            scan_interval_minutes: 60,
            base_url_hint: 'http://localhost:8096',
            jellyfin_username: 'admin',
            ai_base_url: 'https://api.openai.com/v1',
            ai_api_key: '****',
            ai_api_key_set: true,
            ai_model: 'gpt-4o-mini',
            ai_max_subtitle_chars: 4000,
            ai_asr_base_url: body.ai_asr_base_url ?? '',
            ai_asr_api_key: body.clear_ai_asr_api_key ? '' : '****',
            ai_asr_api_key_set: !body.clear_ai_asr_api_key,
            ai_asr_model: body.ai_asr_model ?? '',
            ai_asr_language: body.ai_asr_language ?? '',
            ai_configured: true,
            ai_asr_configured: true,
          });
        }
        if (url.endsWith('/api/v1/settings/ai/asr/test') && method === 'POST') {
          const body = JSON.parse(String(init?.body ?? '{}')) as Record<string, unknown>;
          testASRPayloads.push(body);
          if (shouldFailASRTest) {
            return jsonResponse({ error: 'ASR upstream 401 unauthorized' }, 502);
          }
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

    const { container } = render(
      <ToastProvider>
        <MemoryRouter>
          <SettingsPage />
        </MemoryRouter>
      </ToastProvider>,
    );

    const asrModelInput = await screen.findByLabelText(/ASR 模型名称/);
    const asrBaseUrlInput = screen.getByLabelText(/ASR Base URL/);
    const asrApiKeyInput = screen.getByLabelText(/ASR API Key/);
    const asrLangInput = screen.getByLabelText(/识别语言提示/);
    const testAsrBtn = screen.getByRole('button', { name: /测试语音转文字/ });

    expect(asrModelInput).toHaveValue('whisper-1');
    expect(asrBaseUrlInput).toHaveValue('');
    expect(asrApiKeyInput).toHaveValue('');
    expect(asrLangInput).toHaveValue('zh');

    // Legacy CSS guard across SettingsPage
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);

    // Key-isolation hint when ASR Base URL is empty
    expect(screen.getByText('留空复用上方 AI API Key')).toBeInTheDocument();

    const user = userEvent.setup();

    // Entering an independent ASR Base URL switches the hint to explicit key-isolation warning
    await user.type(asrBaseUrlInput, 'http://whisper:8000/v1');
    expect(
      screen.getByText('留空表示该服务无需认证（不透传主 AI 密钥）'),
    ).toBeInTheDocument();

    // Test unsaved ASR config before saving
    await user.clear(asrModelInput);
    await user.type(asrModelInput, 'whisper-large-v3-turbo');
    await user.clear(asrLangInput);
    await user.type(asrLangInput, 'en');
    await user.type(asrApiKeyInput, 'sk-asr-unsaved');

    await user.click(testAsrBtn);

    await waitFor(() => {
      expect(testASRPayloads).toHaveLength(1);
    });
    expect(testASRPayloads[0]).toEqual({
      asr_base_url: 'http://whisper:8000/v1',
      asr_model: 'whisper-large-v3-turbo',
      asr_language: 'en',
      asr_api_key: 'sk-asr-unsaved',
      ai_base_url: 'https://api.openai.com/v1',
    });
    expect(await screen.findByText('语音转文字接口连接测试成功！')).toBeInTheDocument();

    // Save ASR settings
    await user.click(screen.getByRole('button', { name: /保存设置/ }));
    await waitFor(() => {
      expect(savedPayloads).toHaveLength(1);
    });
    expect(savedPayloads[0]).toMatchObject({
      ai_asr_base_url: 'http://whisper:8000/v1',
      ai_asr_model: 'whisper-large-v3-turbo',
      ai_asr_language: 'en',
      ai_asr_api_key: 'sk-asr-unsaved',
    });

    // After reload/save, ASR key is cleared from input; checking "清除已存 Key" inside ASR section sends clear_ai_asr_api_key on test & save
    const asrKeyFieldGroup = asrApiKeyInput.closest('div')!;
    const clearAsrCheckbox = within(asrKeyFieldGroup).getByRole('checkbox');
    await user.click(clearAsrCheckbox);
    expect(asrApiKeyInput).toBeDisabled();

    // Error feedback when ASR test fails
    shouldFailASRTest = true;
    await user.click(testAsrBtn);
    await waitFor(() => {
      expect(testASRPayloads).toHaveLength(2);
    });
    expect(testASRPayloads[1]).toMatchObject({
      clear_ai_asr_api_key: true,
      asr_base_url: 'http://whisper:8000/v1',
      asr_model: 'whisper-large-v3-turbo',
      asr_language: 'en',
      ai_base_url: 'https://api.openai.com/v1',
    });
    expect(testASRPayloads[1].asr_api_key).toBeUndefined();
    expect(
      (await screen.findAllByText('ASR upstream 401 unauthorized')).length,
    ).toBeGreaterThanOrEqual(1);
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);

    // Quick preset pill updates language to "自动检测" (empty string) and saving sends clear_ai_asr_api_key: true
    await user.click(screen.getByRole('button', { name: '自动检测' }));
    expect(asrLangInput).toHaveValue('');
    await user.click(screen.getByRole('button', { name: /保存设置/ }));
    await waitFor(() => {
      expect(savedPayloads).toHaveLength(2);
    });
    expect(savedPayloads[1].clear_ai_asr_api_key).toBe(true);
    expect(savedPayloads[1].ai_asr_language).toBe('');
    expect(savedPayloads[1].ai_asr_api_key).toBeUndefined();
  });
});
