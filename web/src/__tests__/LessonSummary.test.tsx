import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ToastProvider } from '../components/Toast';
import CourseDetailPage from '../pages/CourseDetailPage';
import type {
  CourseBatchSummaryJob,
  CourseDetail,
  CourseSummariesStatusResponse,
  LessonPlaybackInfo,
  LessonSummary,
  LessonSummaryStateResponse,
} from '../types/coursefin';

const LEGACY_CLASS_REGEX = /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald|sky)-/;

interface MockArtInstance {
  option: Record<string, any>;
  video: HTMLVideoElement;
  isFocus: boolean;
  isDestroy: boolean;
  theme: string;
  fullscreen: boolean;
  fullscreenWeb: boolean;
  subtitle: {
    url: string;
    show: boolean;
    switch: ReturnType<typeof vi.fn>;
    style: ReturnType<typeof vi.fn>;
  };
  setting: {
    update: ReturnType<typeof vi.fn>;
    find?: ReturnType<typeof vi.fn>;
    remove?: ReturnType<typeof vi.fn>;
  };
  controls: {
    update: ReturnType<typeof vi.fn>;
    remove?: ReturnType<typeof vi.fn>;
  };
  on: ReturnType<typeof vi.fn>;
  off: ReturnType<typeof vi.fn>;
  destroy: ReturnType<typeof vi.fn>;
  toggle: ReturnType<typeof vi.fn>;
  play: ReturnType<typeof vi.fn>;
  pause: ReturnType<typeof vi.fn>;
}

const artMockInstances: MockArtInstance[] = [];

vi.mock('artplayer', () => {
  class MockArtplayer {
    static SEEK_STEP = 5;
    static PLAYBACK_RATE = [0.5, 0.75, 1, 1.25, 1.5, 2];
    static RECONNECT_TIME_MAX = 5;
    static LOG_VERSION = true;

    option: Record<string, any>;
    video: HTMLVideoElement;
    isFocus = false;
    isDestroy = false;
    theme = '#1E88E5';
    fullscreen = false;
    fullscreenWeb = false;

    subtitle: MockArtInstance['subtitle'];
    setting = {
      update: vi.fn(),
      find: vi.fn(),
      remove: vi.fn(),
    };
    controls = {
      update: vi.fn(),
      remove: vi.fn(),
    };
    on = vi.fn(() => this);
    off = vi.fn(() => this);
    play = vi.fn(() => this.video.play());
    pause = vi.fn(() => this.video.pause());
    toggle = vi.fn();
    destroy = vi.fn(() => {
      this.isDestroy = true;
    });

    constructor(option: Record<string, any>) {
      this.option = option;
      const container =
        option.container instanceof HTMLElement
          ? option.container
          : (document.querySelector(option.container) as HTMLElement | null);

      const video = document.createElement('video');
      video.className = 'art-video';
      if (container) {
        container.innerHTML = '';
        container.appendChild(video);
      }
      this.video = video;
      this.subtitle = {
        url: option.subtitle?.url ?? '',
        show: Boolean(option.subtitle?.url),
        switch: vi.fn(async (url: string) => {
          this.subtitle.url = url;
          return url;
        }),
        style: vi.fn(),
      };
      if (option.url) {
        this.video.setAttribute('src', option.url);
      }
      artMockInstances.push(this as unknown as MockArtInstance);
    }
  }

  return { default: MockArtplayer };
});

vi.mock('hls.js', () => {
  class MockHls {
    static Events = { ERROR: 'hlsError' };
    static isSupported() {
      return true;
    }
    loadSource = vi.fn();
    attachMedia = vi.fn();
    destroy = vi.fn();
    on = vi.fn();
    off = vi.fn();
  }
  return { default: MockHls };
});

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeCourse(): CourseDetail {
  return {
    id: 'c1',
    library_id: 'lib1',
    title: 'Go 微服务全栈实战',
    sort_title: 'Go 微服务全栈实战',
    overview: '从零设计并部署高并发 Go 微服务系统。',
    instructor: '陈老师',
    cover_path: 'cover.jpg',
    backdrop_path: '',
    tags: ['Go', '微服务'],
    chapter_count: 1,
    lesson_count: 2,
    played_count: 0,
    unplayed_item_count: 2,
    played_percentage: 0,
    played: false,
    total_duration_ms: 1_500_000,
    first_lesson_id: 'l1',
    created_at: '2026-11-20T00:00:00Z',
    updated_at: '2026-11-20T00:00:00Z',
    chapters: [
      {
        id: 'ch1',
        course_id: 'c1',
        title: '第一章：架构基础',
        sort_order: 1,
        overview: '',
        lesson_count: 2,
        played_count: 0,
        unplayed_item_count: 2,
        played_percentage: 0,
        played: false,
        created_at: '2026-11-20T00:00:00Z',
        updated_at: '2026-11-20T00:00:00Z',
        lessons: [
          {
            id: 'l1',
            course_id: 'c1',
            chapter_id: 'ch1',
            media_file_id: 'mf1',
            title: '1.1 服务拆分原则',
            overview: '理解领域边界与 RPC 协议设计',
            sort_order: 1,
            cover_path: '',
            duration_ms: 900_000,
            container: 'mp4',
            video_codec: 'h264',
            audio_codec: 'aac',
            width: 1920,
            height: 1080,
            bitrate: 2000000,
            thumbnail_path: '',
            media_status: 'active',
            position_ticks: 0,
            played: false,
            play_count: 0,
            created_at: '2026-11-20T00:00:00Z',
            updated_at: '2026-11-20T00:00:00Z',
          },
          {
            id: 'l2',
            course_id: 'c1',
            chapter_id: 'ch1',
            media_file_id: 'mf2',
            title: '1.2 gRPC 实战与拦截器',
            overview: '',
            sort_order: 2,
            cover_path: '',
            duration_ms: 600_000,
            container: 'mp4',
            video_codec: 'h264',
            audio_codec: 'aac',
            width: 1920,
            height: 1080,
            bitrate: 2000000,
            thumbnail_path: '',
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
}

function makePlayback(overrides: Partial<LessonPlaybackInfo> = {}): LessonPlaybackInfo {
  return {
    lesson_id: 'l1',
    course_id: 'c1',
    media_file_id: 'mf1',
    play_session_id: 'sess-1',
    mode: 'direct',
    transcoding: false,
    stream_url: '/Videos/l1/stream.mp4?Static=true&MediaSourceId=mf1',
    subtitles: [
      {
        index: 2,
        language: 'zh',
        title: '中文',
        format: 'vtt',
        is_default: true,
        is_external: true,
        is_text: true,
        url: '/Videos/l1/mf1/Subtitles/2/Stream.vtt',
      },
    ],
    progress: {
      position_ticks: 0,
      played: false,
      play_count: 0,
    },
    video_codec: 'h264',
    audio_codec: 'aac',
    container: 'mp4',
    duration_ms: 900_000,
    ...overrides,
  };
}

function makeSampleSummary(overrides: Partial<LessonSummary> = {}): LessonSummary {
  return {
    lesson_id: 'l1',
    media_file_id: 'mf1',
    source_type: 'external_subtitle',
    source_label: '外挂字幕 (zh)',
    has_timestamps: true,
    model: 'gpt-4o-mini',
    overview: '本节深入讲解微服务拆分边界与接口契约演化规范。',
    outline: [
      {
        title: '领域划分原则',
        summary: '识别核心子域与支撑域边界。',
        start_sec: 0,
      },
      {
        title: 'RPC 契约与幂等设计',
        summary: '定义向后兼容的 Protobuf 接口及重试幂等键。',
        start_sec: 330,
      },
    ],
    timeline: [
      {
        start_sec: 0,
        end_sec: 330,
        title: '微服务拆分的粒度权衡',
        summary: '从团队规模与调用链路延迟角度权衡服务拆分粒度。',
        key_points: ['避免过早拆分为纳米服务', '按业务变更频率聚合模块'],
      },
      {
        start_sec: 330,
        end_sec: 730,
        title: '接口契约与幂等保障',
        summary: '通过幂等 Token 与错误码规范保证分布式调用可靠性。',
        key_points: ['使用唯一请求 ID 去重', '区分可重试与不可重试错误'],
      },
    ],
    created_at: '2026-11-25T00:00:00Z',
    updated_at: '2026-11-25T00:00:00Z',
    ...overrides,
  };
}

function renderCoursePage(initialPath = '/courses/c1?lesson=l1') {
  return render(
    <ToastProvider>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/courses/:courseId" element={<CourseDetailPage />} />
          <Route path="/settings" element={<div>Settings Route</div>} />
          <Route path="/courses/:courseId/edit" element={<div>Course Edit Route</div>} />
        </Routes>
      </MemoryRouter>
    </ToastProvider>,
  );
}

describe('CourseDetailPage AI Summary & LessonSummaryPanel', () => {
  const originalClipboard = navigator.clipboard;
  const originalIsSecureContext = window.isSecureContext;

  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token-123');
    artMockInstances.length = 0;
  });

  afterEach(() => {
    localStorage.clear();
    vi.useRealTimers();
    vi.restoreAllMocks();
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      configurable: true,
    });
    Object.defineProperty(window, 'isSecureContext', {
      value: originalIsSecureContext,
      configurable: true,
    });
  });

  it('renders syllabus and AI summary tabs, displays outline & chronological timeline, highlights active section, seeks on timestamp click, and copies Markdown', async () => {
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });

    const course = makeCourse();
    const playback = makePlayback();
    const summaryState: LessonSummaryStateResponse = {
      lesson_id: 'l1',
      summary: makeSampleSummary(),
      job: null,
      capabilities: {
        has_subtitle: true,
        has_cached_transcript: false,
        transcript_source: '',
        ai_configured: true,
        asr_configured: false,
        has_ffmpeg: true,
      },
    };
    const courseStatus: CourseSummariesStatusResponse = {
      course_id: 'c1',
      summary_lesson_ids: ['l1'],
      batch_job: null,
    };

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) return jsonResponse(course);
        if (url.includes('/api/v1/lessons/l1/playback')) return jsonResponse(playback);
        if (url.endsWith('/api/v1/courses/c1/summaries/status')) return jsonResponse(courseStatus);
        if (url.endsWith('/api/v1/lessons/l1/summary')) return jsonResponse(summaryState);
        return jsonResponse({}, 404);
      }),
    );

    const { container } = renderCoursePage();

    expect(await screen.findByRole('heading', { name: 'Go 微服务全栈实战' })).toBeInTheDocument();
    const video = await waitFor(() => {
      const el = container.querySelector('video') as HTMLVideoElement | null;
      expect(el).toBeTruthy();
      return el!;
    });
    Object.defineProperty(video, 'duration', { value: 900, configurable: true });
    vi.spyOn(video, 'play').mockResolvedValue();

    const user = userEvent.setup();
    const writeTextMock = vi.spyOn(navigator.clipboard, 'writeText');
    const summaryTab = screen.getByRole('tab', { name: /AI 总结/ });
    await user.click(summaryTab);
    expect(summaryTab).toHaveAttribute('aria-selected', 'true');

    expect(await screen.findByText('AI 总结视频大纲')).toBeInTheDocument();
    expect(screen.getByText('AI 总结视频内容（按时间顺序）')).toBeInTheDocument();
    expect(screen.getByText('外挂字幕 (zh)')).toBeInTheDocument();
    expect(screen.getByText('本节深入讲解微服务拆分边界与接口契约演化规范。')).toBeInTheDocument();

    // Click timeline timestamp button (05:30 - 12:10) -> seeks video to 330s
    const rangeButton = screen.getByRole('button', { name: /05:30\s*-\s*12:10/ });
    await user.click(rangeButton);
    expect(video.currentTime).toBe(330);

    // Advance video time to 400s -> timeline section [05:30 - 12:10] has aria-current="true"
    video.currentTime = 400;
    fireEvent.timeUpdate(video);

    await waitFor(() => {
      const activeSection = rangeButton.closest('[aria-current="true"]');
      expect(activeSection).toBeTruthy();
      expect(within(activeSection as HTMLElement).getByText('接口契约与幂等保障')).toBeInTheDocument();
    });

    // Click copy Markdown -> clipboard receives formatted Markdown
    const copyBtn = screen.getByRole('button', { name: /复制 Markdown/ });
    await user.click(copyBtn);

    await waitFor(() => {
      expect(writeTextMock).toHaveBeenCalledTimes(1);
    });
    const copiedText = writeTextMock.mock.calls[0][0] as string;
    expect(copiedText).toContain('# 1.1 服务拆分原则');
    expect(copiedText).toContain('## AI 总结视频大纲');
    expect(copiedText).toContain('## AI 总结视频内容（按时间顺序）');
    expect(copiedText).toContain('### [05:30 - 12:10] 接口契约与幂等保障');
    expect(copyBtn).toHaveTextContent('已复制');
    expect(screen.getByText('已复制 Markdown 总结')).toBeInTheDocument();

    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);
  });

  it('handles subtitle-less lesson when ASR is not configured via manual transcript paste', async () => {
    const course = makeCourse();
    const playback = makePlayback({ subtitles: [] });
    const generateCalls: Array<{ url: string; body: unknown }> = [];

    const emptyNoASR: LessonSummaryStateResponse = {
      lesson_id: 'l1',
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
    };

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) return jsonResponse(course);
        if (url.includes('/api/v1/lessons/l1/playback')) return jsonResponse(playback);
        if (url.endsWith('/api/v1/courses/c1/summaries/status')) {
          return jsonResponse({ course_id: 'c1', summary_lesson_ids: [], batch_job: null });
        }
        if (url.endsWith('/api/v1/lessons/l1/summary')) return jsonResponse(emptyNoASR);
        if (url.endsWith('/api/v1/lessons/l1/summary/generate') && init?.method === 'POST') {
          const body = JSON.parse(String(init.body ?? '{}'));
          generateCalls.push({ url, body });
          return jsonResponse(
            {
              lesson_id: 'l1',
              course_id: 'c1',
              running: true,
              stage: 'summarizing',
              message: '正在生成 AI 总结...',
              chunk_index: 0,
              chunk_total: 0,
              source_type: 'manual',
              started_at: '2026-11-25T00:00:00Z',
            },
            202,
          );
        }
        return jsonResponse({}, 404);
      }),
    );

    const { container } = renderCoursePage();
    expect(await screen.findByRole('heading', { name: 'Go 微服务全栈实战' })).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: /AI 总结/ }));

    const settingsLink = await screen.findByRole('link', { name: /前往配置语音转文字/ });
    expect(settingsLink).toHaveAttribute('href', '/settings');
    const editLink = screen.getByRole('link', { name: /去编辑页绑定字幕/ });
    expect(editLink).toHaveAttribute('href', '/courses/c1/edit');

    const textarea = screen.getByLabelText(/手动粘贴文稿|课程文稿/);
    await user.type(textarea, '00:00:01,000 --> 00:00:04,000\n本节介绍微服务拆分原则');

    const submitBtn = screen.getByRole('button', { name: '根据文稿生成总结' });
    await user.click(submitBtn);

    await waitFor(() => {
      expect(generateCalls).toHaveLength(1);
    });
    expect(generateCalls[0].body).toEqual({
      manual_transcript: '00:00:01,000 --> 00:00:04,000\n本节介绍微服务拆分原则',
    });
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);
  });

  it('polls ASR transcription stage progress, supports cancel, and refreshes player subtitles on ASR completion', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

    const course = makeCourse();
    const initialPlayback = makePlayback({ subtitles: [] });
    const refreshedPlayback = makePlayback({
      subtitles: [
        {
          index: 100,
          language: 'zh',
          title: 'AI 转写字幕',
          format: 'vtt',
          is_default: false,
          is_external: true,
          is_text: true,
          url: '/Videos/l1/mf1/Subtitles/100/Stream.vtt',
        },
      ],
    });

    let playbackCalls = 0;
    let summaryGetCalls = 0;
    let phase: 'empty' | 'running' | 'cancelled' | 'completed' = 'empty';
    let cancelCalled = false;

    const asrSummary = makeSampleSummary({
      source_type: 'asr',
      source_label: 'AI 语音转写',
    });

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) return jsonResponse(course);
        if (url.includes('/api/v1/lessons/l1/playback')) {
          playbackCalls += 1;
          return jsonResponse(phase === 'completed' ? refreshedPlayback : initialPlayback);
        }
        if (url.endsWith('/api/v1/courses/c1/summaries/status')) {
          return jsonResponse({
            course_id: 'c1',
            summary_lesson_ids: phase === 'completed' ? ['l1'] : [],
            batch_job: null,
          });
        }
        if (url.endsWith('/api/v1/lessons/l1/summary') && (!init?.method || init.method === 'GET')) {
          summaryGetCalls += 1;
          if (phase === 'running') {
            // Transition to completed on the poll after restarting
            if (cancelCalled && summaryGetCalls >= 3) {
              phase = 'completed';
              return jsonResponse({
                lesson_id: 'l1',
                summary: asrSummary,
                job: {
                  lesson_id: 'l1',
                  course_id: 'c1',
                  running: false,
                  stage: 'completed',
                  message: 'AI 总结生成完成',
                  chunk_index: 2,
                  chunk_total: 2,
                  source_type: 'asr',
                  started_at: '2026-11-25T00:00:00Z',
                  finished_at: '2026-11-25T00:01:00Z',
                },
                capabilities: {
                  has_subtitle: false,
                  has_cached_transcript: true,
                  transcript_source: 'asr',
                  ai_configured: true,
                  asr_configured: true,
                  has_ffmpeg: true,
                },
              });
            }
            return jsonResponse({
              lesson_id: 'l1',
              summary: null,
              job: {
                lesson_id: 'l1',
                course_id: 'c1',
                running: true,
                stage: 'transcribing',
                message: '正在语音转写（分片 1/2）...',
                chunk_index: 1,
                chunk_total: 2,
                source_type: 'asr',
                started_at: '2026-11-25T00:00:00Z',
              },
              capabilities: {
                has_subtitle: false,
                has_cached_transcript: false,
                transcript_source: '',
                ai_configured: true,
                asr_configured: true,
                has_ffmpeg: true,
              },
            });
          }
          if (phase === 'cancelled') {
            return jsonResponse({
              lesson_id: 'l1',
              summary: null,
              job: {
                lesson_id: 'l1',
                course_id: 'c1',
                running: false,
                stage: 'cancelled',
                message: '任务已取消',
                chunk_index: 1,
                chunk_total: 2,
                source_type: 'asr',
                started_at: '2026-11-25T00:00:00Z',
                finished_at: '2026-11-25T00:00:05Z',
              },
              capabilities: {
                has_subtitle: false,
                has_cached_transcript: false,
                transcript_source: '',
                ai_configured: true,
                asr_configured: true,
                has_ffmpeg: true,
              },
            });
          }
          return jsonResponse({
            lesson_id: 'l1',
            summary: null,
            job: null,
            capabilities: {
              has_subtitle: false,
              has_cached_transcript: false,
              transcript_source: '',
              ai_configured: true,
              asr_configured: true,
              has_ffmpeg: true,
            },
          });
        }
        if (url.endsWith('/api/v1/lessons/l1/summary/generate') && init?.method === 'POST') {
          phase = 'running';
          return jsonResponse(
            {
              lesson_id: 'l1',
              course_id: 'c1',
              running: true,
              stage: 'transcribing',
              message: '正在语音转写（分片 1/2）...',
              chunk_index: 1,
              chunk_total: 2,
              source_type: 'asr',
              started_at: '2026-11-25T00:00:00Z',
            },
            202,
          );
        }
        if (url.endsWith('/api/v1/lessons/l1/summary/cancel') && init?.method === 'POST') {
          cancelCalled = true;
          phase = 'cancelled';
          return jsonResponse({
            lesson_id: 'l1',
            course_id: 'c1',
            running: false,
            stage: 'cancelled',
            message: '任务已取消',
            chunk_index: 1,
            chunk_total: 2,
            source_type: 'asr',
            started_at: '2026-11-25T00:00:00Z',
            finished_at: '2026-11-25T00:00:05Z',
          });
        }
        return jsonResponse({}, 404);
      }),
    );

    const { container } = renderCoursePage();
    expect(await screen.findByText('无可用字幕')).toBeInTheDocument();
    expect(artMockInstances).toHaveLength(1);
    const initialArt = artMockInstances[0];

    await user.click(screen.getByRole('tab', { name: /AI 总结/ }));
    const startBtn = await screen.findByRole('button', { name: '转写并生成总结' });
    await user.click(startBtn);

    expect(await screen.findByText(/正在语音转写（分片 1\/2）/)).toBeInTheDocument();
    const cancelBtn = screen.getByRole('button', { name: '取消' });
    await user.click(cancelBtn);

    await waitFor(() => {
      expect(cancelCalled).toBe(true);
      expect(screen.getByRole('button', { name: '转写并生成总结' })).toBeInTheDocument();
    });

    // Restart and let polling complete
    await user.click(screen.getByRole('button', { name: '转写并生成总结' }));
    expect(await screen.findByText(/正在语音转写（分片 1\/2）/)).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });

    await waitFor(() => {
      expect(screen.getByText('AI 总结视频大纲')).toBeInTheDocument();
      expect(screen.getByText('AI 语音转写')).toBeInTheDocument();
      expect(playbackCalls).toBe(2);
    });

    // Player instance must NOT be destroyed; subtitles refreshed in-place
    expect(artMockInstances).toHaveLength(1);
    expect(initialArt.destroy).not.toHaveBeenCalled();
    expect(initialArt.controls.update).toHaveBeenCalled();
    expect(screen.getByText(/已生成 AI 转写字幕，可在播放器字幕菜单开启/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重新转写' })).toBeInTheDocument();
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);
  });

  it('displays course batch progress banner, allows starting and cancelling batch generation, and shows summarized sparkles badge in syllabus', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

    const course = makeCourse();
    const playback = makePlayback();
    let batchRunning = false;
    let cancelBatchCalled = false;
    const generateBatchBodies: unknown[] = [];

    const runningBatch: CourseBatchSummaryJob = {
      course_id: 'c1',
      running: true,
      total: 2,
      completed: 1,
      skipped: 0,
      failed: 0,
      current_lesson_id: 'l2',
      current_lesson_title: '1.2 gRPC 实战与拦截器',
      current_stage: 'transcribing',
      chunk_index: 1,
      chunk_total: 2,
      errors: [],
      started_at: '2026-11-25T00:00:00Z',
    };

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) return jsonResponse(course);
        if (url.includes('/api/v1/lessons/l1/playback')) return jsonResponse(playback);
        if (url.endsWith('/api/v1/courses/c1/summaries/status')) {
          return jsonResponse({
            course_id: 'c1',
            summary_lesson_ids: ['l1'],
            batch_job: batchRunning ? runningBatch : null,
          });
        }
        if (url.endsWith('/api/v1/courses/c1/summaries/generate') && init?.method === 'POST') {
          generateBatchBodies.push(JSON.parse(String(init.body ?? '{}')));
          batchRunning = true;
          return jsonResponse(runningBatch, 202);
        }
        if (url.endsWith('/api/v1/courses/c1/summaries/cancel') && init?.method === 'POST') {
          cancelBatchCalled = true;
          batchRunning = false;
          return jsonResponse({
            ...runningBatch,
            running: false,
            current_stage: 'cancelled',
            finished_at: '2026-11-25T00:01:00Z',
          });
        }
        return jsonResponse({}, 404);
      }),
    );

    const { container } = renderCoursePage();

    expect(await screen.findByRole('heading', { name: 'Go 微服务全栈实战' })).toBeInTheDocument();

    // Syllabus shows Sparkles badge for l1 only
    const sidebar = screen.getByRole('complementary');
    await waitFor(() => {
      expect(within(sidebar).getAllByLabelText('已生成 AI 总结')).toHaveLength(1);
    });
    const l1Button = within(sidebar).getByRole('button', { name: /1.1 服务拆分原则/ });
    const l2Button = within(sidebar).getByRole('button', { name: /1.2 gRPC 实战与拦截器/ });
    expect(within(l1Button).getByLabelText('已生成 AI 总结')).toBeInTheDocument();
    expect(within(l2Button).queryByLabelText('已生成 AI 总结')).toBeNull();

    // Click 批量总结
    const batchBtn = within(sidebar).getByRole('button', { name: /批量总结/ });
    await user.click(batchBtn);

    expect(generateBatchBodies).toEqual([{ mode: 'missing_only' }]);
    expect(await within(sidebar).findByText(/批量生成中 1\/2/)).toBeInTheDocument();
    expect(
      within(sidebar).getByText(/正在语音转写（分片 1\/2）.*1\.2 gRPC 实战与拦截器/),
    ).toBeInTheDocument();

    const cancelBatchBtn = within(sidebar).getByRole('button', { name: '取消' });
    await user.click(cancelBatchBtn);

    await waitFor(() => {
      expect(cancelBatchCalled).toBe(true);
      expect(within(sidebar).queryByText(/批量生成中/)).toBeNull();
    });
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);
  });

  it('ignores stale summary response after switching lessons', async () => {
    const course = makeCourse();
    const playbackL1 = makePlayback({ lesson_id: 'l1' });
    const playbackL2 = makePlayback({ lesson_id: 'l2', media_file_id: 'mf2' });

    let resolveL1Summary!: (res: Response) => void;
    const l1SummaryPromise = new Promise<Response>((resolve) => {
      resolveL1Summary = resolve;
    });

    const l2SummaryState: LessonSummaryStateResponse = {
      lesson_id: 'l2',
      summary: makeSampleSummary({
        lesson_id: 'l2',
        media_file_id: 'mf2',
        overview: 'L2 新课总结：gRPC 拦截器链路与统一鉴权实现。',
        outline: [
          {
            title: 'Unary 拦截器机制',
            summary: '串联日志、恢复与认证中间件。',
            start_sec: 10,
          },
        ],
        timeline: [
          {
            start_sec: 10,
            end_sec: 120,
            title: 'UnaryServerInterceptor 编写',
            summary: '演示链式拦截器的执行顺序。',
            key_points: ['context 透传用户身份'],
          },
        ],
      }),
      job: null,
      capabilities: {
        has_subtitle: true,
        has_cached_transcript: false,
        transcript_source: '',
        ai_configured: true,
        asr_configured: false,
        has_ffmpeg: true,
      },
    };

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) return jsonResponse(course);
        if (url.includes('/api/v1/lessons/l1/playback')) return jsonResponse(playbackL1);
        if (url.includes('/api/v1/lessons/l2/playback')) return jsonResponse(playbackL2);
        if (url.endsWith('/api/v1/courses/c1/summaries/status')) {
          return jsonResponse({ course_id: 'c1', summary_lesson_ids: ['l1', 'l2'], batch_job: null });
        }
        if (url.endsWith('/api/v1/lessons/l1/summary')) {
          return l1SummaryPromise;
        }
        if (url.endsWith('/api/v1/lessons/l2/summary')) {
          return jsonResponse(l2SummaryState);
        }
        return jsonResponse({}, 404);
      }),
    );

    const { container } = renderCoursePage();
    expect(await screen.findByRole('heading', { name: 'Go 微服务全栈实战' })).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: /AI 总结/ }));

    // L1 summary request is still pending; user switches to L2 via 下一节 button
    await user.click(screen.getByRole('button', { name: /下一节/ }));

    // L2 summary resolves first and renders
    expect(
      await screen.findByText('L2 新课总结：gRPC 拦截器链路与统一鉴权实现。'),
    ).toBeInTheDocument();

    // Now L1's delayed response arrives late — MUST NOT overwrite L2's summary!
    await act(async () => {
      resolveL1Summary(
        jsonResponse({
          lesson_id: 'l1',
          summary: makeSampleSummary({
            lesson_id: 'l1',
            overview: 'L1 迟到响应：绝不能覆盖 L2 的界面内容。',
          }),
          job: null,
          capabilities: {
            has_subtitle: true,
            has_cached_transcript: false,
            transcript_source: '',
            ai_configured: true,
            asr_configured: false,
            has_ffmpeg: true,
          },
        }),
      );
      await Promise.resolve();
    });

    expect(screen.getByText('L2 新课总结：gRPC 拦截器链路与统一鉴权实现。')).toBeInTheDocument();
    expect(screen.queryByText('L1 迟到响应：绝不能覆盖 L2 的界面内容。')).toBeNull();
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);
  });

  it('keeps previously saved summary visible while regenerating or when regenerate fails', async () => {
    const course = makeCourse();
    const playback = makePlayback();
    const savedSummary = makeSampleSummary({
      overview: '已保存的旧版本总结内容依然可读。',
    });

    const mockState: LessonSummaryStateResponse = {
      lesson_id: 'l1',
      summary: savedSummary,
      job: {
        lesson_id: 'l1',
        course_id: 'c1',
        running: false,
        stage: 'failed',
        message: '生成失败',
        chunk_index: 0,
        chunk_total: 0,
        source_type: 'external_subtitle',
        error: '上游 LLM 限流 429',
        started_at: '2026-11-25T00:00:00Z',
        finished_at: '2026-11-25T00:00:03Z',
      },
      capabilities: {
        has_subtitle: true,
        has_cached_transcript: false,
        transcript_source: '',
        ai_configured: true,
        asr_configured: false,
        has_ffmpeg: true,
      },
    };

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) return jsonResponse(course);
        if (url.includes('/api/v1/lessons/l1/playback')) return jsonResponse(playback);
        if (url.endsWith('/api/v1/courses/c1/summaries/status')) {
          return jsonResponse({ course_id: 'c1', summary_lesson_ids: ['l1'], batch_job: null });
        }
        if (url.endsWith('/api/v1/lessons/l1/summary')) return jsonResponse(mockState);
        return jsonResponse({}, 404);
      }),
    );

    const { container } = renderCoursePage();
    expect(await screen.findByRole('heading', { name: 'Go 微服务全栈实战' })).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: /AI 总结/ }));

    expect(await screen.findByText(/上游 LLM 限流 429/)).toBeInTheDocument();
    expect(screen.getByText('已保存的旧版本总结内容依然可读。')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /复制 Markdown/ })).toBeInTheDocument();
    expect(container.innerHTML).not.toMatch(LEGACY_CLASS_REGEX);
  });

  it('keeps ASR polling alive and refreshes player subtitles in-place after switching back from AI 总结 to 课程目录', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

    const course = makeCourse();
    const initialPlayback = makePlayback({ subtitles: [] });
    const refreshedPlayback = makePlayback({
      subtitles: [
        {
          index: 100,
          language: 'zh',
          title: 'AI 转写字幕',
          format: 'vtt',
          is_default: false,
          is_external: true,
          is_text: true,
          url: '/Videos/l1/mf1/Subtitles/100/Stream.vtt',
        },
      ],
    });

    let playbackCalls = 0;
    let summaryGetCalls = 0;
    let phase: 'empty' | 'running' | 'completed' = 'empty';

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) return jsonResponse(course);
        if (url.includes('/api/v1/lessons/l1/playback')) {
          playbackCalls += 1;
          return jsonResponse(phase === 'completed' ? refreshedPlayback : initialPlayback);
        }
        if (url.endsWith('/api/v1/courses/c1/summaries/status')) {
          return jsonResponse({
            course_id: 'c1',
            summary_lesson_ids: phase === 'completed' ? ['l1'] : [],
            batch_job: null,
          });
        }
        if (url.endsWith('/api/v1/lessons/l1/summary') && (!init?.method || init.method === 'GET')) {
          summaryGetCalls += 1;
          if (phase === 'running') {
            if (summaryGetCalls >= 3) {
              phase = 'completed';
              return jsonResponse({
                lesson_id: 'l1',
                summary: makeSampleSummary({
                  source_type: 'asr',
                  source_label: 'AI 语音转写',
                }),
                job: {
                  lesson_id: 'l1',
                  course_id: 'c1',
                  running: false,
                  stage: 'completed',
                  message: 'AI 总结生成完成',
                  chunk_index: 2,
                  chunk_total: 2,
                  source_type: 'asr',
                  started_at: '2026-11-25T00:00:00Z',
                  finished_at: '2026-11-25T00:01:00Z',
                },
                capabilities: {
                  has_subtitle: false,
                  has_cached_transcript: true,
                  transcript_source: 'asr',
                  ai_configured: true,
                  asr_configured: true,
                  has_ffmpeg: true,
                },
              });
            }
            return jsonResponse({
              lesson_id: 'l1',
              summary: null,
              job: {
                lesson_id: 'l1',
                course_id: 'c1',
                running: true,
                stage: 'transcribing',
                message: '正在语音转写（分片 1/2）...',
                chunk_index: 1,
                chunk_total: 2,
                source_type: 'asr',
                started_at: '2026-11-25T00:00:00Z',
              },
              capabilities: {
                has_subtitle: false,
                has_cached_transcript: false,
                transcript_source: '',
                ai_configured: true,
                asr_configured: true,
                has_ffmpeg: true,
              },
            });
          }
          return jsonResponse({
            lesson_id: 'l1',
            summary: null,
            job: null,
            capabilities: {
              has_subtitle: false,
              has_cached_transcript: false,
              transcript_source: '',
              ai_configured: true,
              asr_configured: true,
              has_ffmpeg: true,
            },
          });
        }
        if (url.endsWith('/api/v1/lessons/l1/summary/generate') && init?.method === 'POST') {
          phase = 'running';
          return jsonResponse(
            {
              lesson_id: 'l1',
              course_id: 'c1',
              running: true,
              stage: 'transcribing',
              message: '正在语音转写（分片 1/2）...',
              chunk_index: 1,
              chunk_total: 2,
              source_type: 'asr',
              started_at: '2026-11-25T00:00:00Z',
            },
            202,
          );
        }
        return jsonResponse({}, 404);
      }),
    );

    renderCoursePage();
    expect(await screen.findByText('无可用字幕')).toBeInTheDocument();
    await waitFor(() => {
      expect(artMockInstances).toHaveLength(1);
    });
    const initialArt = artMockInstances[0];

    await user.click(screen.getByRole('tab', { name: /AI 总结/ }));
    await user.click(await screen.findByRole('button', { name: '转写并生成总结' }));
    expect(await screen.findByText(/正在语音转写（分片 1\/2）/)).toBeInTheDocument();

    // User switches back to 课程目录 while ASR job is still running
    await user.click(screen.getByRole('tab', { name: /课程目录/ }));
    expect(screen.getByRole('tab', { name: /课程目录/ })).toHaveAttribute('aria-selected', 'true');

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });

    await waitFor(() => {
      expect(playbackCalls).toBe(2);
    });

    expect(artMockInstances).toHaveLength(1);
    expect(initialArt.destroy).not.toHaveBeenCalled();
    expect(initialArt.controls.update).toHaveBeenCalled();
    expect(screen.getByText(/已生成 AI 转写字幕，可在播放器字幕菜单开启/)).toBeInTheDocument();

    // Syllabus sparkle badge should now be visible for l1 too
    const sidebar = screen.getByRole('complementary');
    expect(within(sidebar).getByLabelText('已生成 AI 总结')).toBeInTheDocument();
  });

  it('refreshes active lesson subtitles in-place during course batch generation even when staying on 课程目录', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

    const course = makeCourse();
    const initialPlayback = makePlayback({ subtitles: [] });
    const refreshedPlayback = makePlayback({
      subtitles: [
        {
          index: 100,
          language: 'zh',
          title: 'AI 转写字幕',
          format: 'vtt',
          is_default: false,
          is_external: true,
          is_text: true,
          url: '/Videos/l1/mf1/Subtitles/100/Stream.vtt',
        },
      ],
    });

    let playbackCalls = 0;
    let statusCalls = 0;
    let batchPollCount = 0;
    let batchStarted = false;

    const batchStartJob: CourseBatchSummaryJob = {
      course_id: 'c1',
      running: true,
      total: 2,
      completed: 0,
      skipped: 0,
      failed: 0,
      current_lesson_id: 'l1',
      current_lesson_title: '1.1 服务拆分原则',
      current_stage: 'transcribing',
      chunk_index: 1,
      chunk_total: 2,
      errors: [],
      started_at: '2026-11-25T00:00:00Z',
    };

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) return jsonResponse(course);
        if (url.includes('/api/v1/lessons/l1/playback')) {
          playbackCalls += 1;
          return jsonResponse(batchPollCount >= 1 ? refreshedPlayback : initialPlayback);
        }
        if (url.endsWith('/api/v1/courses/c1/summaries/status')) {
          statusCalls += 1;
          if (!batchStarted) {
            return jsonResponse({
              course_id: 'c1',
              summary_lesson_ids: [],
              batch_job: null,
            });
          }
          batchPollCount += 1;
          if (batchPollCount === 1) {
            return jsonResponse({
              course_id: 'c1',
              summary_lesson_ids: ['l1'],
              batch_job: {
                ...batchStartJob,
                completed: 1,
                current_lesson_id: 'l2',
                current_lesson_title: '1.2 gRPC 实战与拦截器',
                current_stage: 'summarizing',
                chunk_index: 0,
                chunk_total: 0,
              },
            });
          }
          return jsonResponse({
            course_id: 'c1',
            summary_lesson_ids: ['l1', 'l2'],
            batch_job: {
              ...batchStartJob,
              running: false,
              completed: 2,
              current_lesson_id: '',
              current_lesson_title: '',
              current_stage: 'completed',
              finished_at: '2026-11-25T00:02:00Z',
            },
          });
        }
        if (url.endsWith('/api/v1/courses/c1/summaries/generate') && init?.method === 'POST') {
          batchStarted = true;
          return jsonResponse(batchStartJob, 202);
        }
        return jsonResponse({}, 404);
      }),
    );

    renderCoursePage();
    expect(await screen.findByText('无可用字幕')).toBeInTheDocument();
    await waitFor(() => {
      expect(artMockInstances).toHaveLength(1);
    });
    const initialArt = artMockInstances[0];

    // User stays on 课程目录 (never clicks AI 总结 tab) and triggers batch summary
    const sidebar = screen.getByRole('complementary');
    await user.click(within(sidebar).getByRole('button', { name: /批量总结/ }));
    expect(await within(sidebar).findByText(/批量生成中 0\/2/)).toBeInTheDocument();

    // First poll: l1 finishes with ASR subtitle while batch moves to l2
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });

    await waitFor(() => {
      expect(statusCalls).toBeGreaterThanOrEqual(2);
      expect(playbackCalls).toBe(2);
    });

    expect(artMockInstances).toHaveLength(1);
    expect(initialArt.destroy).not.toHaveBeenCalled();
    expect(initialArt.controls.update).toHaveBeenCalled();
    expect(screen.getAllByText(/已生成 AI 转写字幕，可在播放器字幕菜单开启/)).toHaveLength(1);

    // Second poll: l2 completes too -> must NOT re-toast for l1
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });

    await waitFor(() => {
      expect(within(sidebar).queryByText(/批量生成中/)).toBeNull();
    });
    expect(playbackCalls).toBe(2);
    expect(screen.getAllByText(/已生成 AI 转写字幕，可在播放器字幕菜单开启/)).toHaveLength(1);
  });
});
