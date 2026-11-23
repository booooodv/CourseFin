import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import VideoPlayer from '../components/VideoPlayer';
import CourseDetailPage from '../pages/CourseDetailPage';
import type { CourseDetail, LessonPlaybackInfo } from '../types/coursefin';

const hlsMockInstances: Array<{
  loadSource: ReturnType<typeof vi.fn>;
  attachMedia: ReturnType<typeof vi.fn>;
  destroy: ReturnType<typeof vi.fn>;
  on: ReturnType<typeof vi.fn>;
  off: ReturnType<typeof vi.fn>;
}> = [];

const hlsIsSupportedMock = vi.fn(() => true);

vi.mock('hls.js', () => {
  class MockHls {
    static Events = {
      ERROR: 'hlsError',
    };
    static isSupported() {
      return hlsIsSupportedMock();
    }
    loadSource = vi.fn();
    attachMedia = vi.fn();
    destroy = vi.fn();
    on = vi.fn();
    off = vi.fn();
    constructor() {
      hlsMockInstances.push(this);
    }
  }
  return { default: MockHls };
});

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makePlayback(overrides: Partial<LessonPlaybackInfo> = {}): LessonPlaybackInfo {
  return {
    lesson_id: 'l1',
    course_id: 'c1',
    media_file_id: 'mf1',
    play_session_id: 'sess-1',
    mode: 'direct',
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
      {
        index: 3,
        language: 'en',
        title: 'English',
        format: 'vtt',
        is_default: false,
        is_external: false,
        is_text: true,
        url: '/Videos/l1/mf1/Subtitles/3/Stream.vtt',
      },
      {
        index: 4,
        language: 'jpn',
        title: 'PGS Bitmap',
        format: 'pgssub',
        is_default: false,
        is_external: false,
        is_text: false,
        url: '',
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
    duration_ms: 120000,
    ...overrides,
  };
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
    total_duration_ms: 300000,
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
            duration_ms: 120000,
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
            duration_ms: 180000,
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

describe('VideoPlayer', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token-123');
    hlsMockInstances.length = 0;
    hlsIsSupportedMock.mockReturnValue(true);
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('shows native Jellyfin player recommendation banner when playback mode is unsupported', () => {
    const playback = makePlayback({
      mode: 'unsupported',
      stream_url: '',
      video_codec: 'hevc',
      audio_codec: 'dts',
      container: 'mkv',
    });

    const { container } = render(<VideoPlayer playback={playback} lessonTitle="HEVC 课程" />);

    expect(screen.getByText(/Infuse/)).toBeInTheDocument();
    expect(screen.getByText(/VidHub/)).toBeInTheDocument();
    expect(screen.getByText(/SenPlayer/)).toBeInTheDocument();
    expect(screen.getByText('hevc')).toBeInTheDocument();
    expect(screen.getByText('dts')).toBeInTheDocument();
    expect(screen.getByText('mkv')).toBeInTheDocument();
    expect(container.querySelector('video')).toBeNull();
  });

  it('changes playbackRate when user selects 1.5x speed and renders subtitle selector options', async () => {
    const playback = makePlayback();
    const { container } = render(<VideoPlayer playback={playback} />);

    const video = container.querySelector('video') as HTMLVideoElement;
    expect(video).toBeTruthy();
    expect(video.getAttribute('src')).toBe(
      '/Videos/l1/stream.mp4?Static=true&MediaSourceId=mf1&api_key=test-token-123',
    );

    const tracks = container.querySelectorAll('track');
    expect(tracks).toHaveLength(2);
    expect(tracks[0].getAttribute('src')).toBe(
      '/Videos/l1/mf1/Subtitles/2/Stream.vtt?api_key=test-token-123',
    );

    const subtitleSelect = screen.getByLabelText('字幕选择') as HTMLSelectElement;
    expect(subtitleSelect).toBeInTheDocument();
    const options = Array.from(subtitleSelect.options).map((o) => o.textContent);
    expect(options).toEqual(['关闭', '中文', 'English']);

    const speedSelect = screen.getByLabelText('播放倍速') as HTMLSelectElement;
    const user = userEvent.setup();
    await user.selectOptions(speedSelect, '1.5');

    expect(video.playbackRate).toBe(1.5);
    expect(localStorage.getItem('coursefin_playback_rate')).toBe('1.5');
  });

  it('reports playback progress ticks to API on pause and ended and resumes from position_ticks', async () => {
    const progressCalls: Array<{ url: string; body: { position_ticks: number } }> = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/lessons/l1/progress') && init?.method === 'POST') {
          const body = JSON.parse(String(init.body ?? '{}')) as { position_ticks: number };
          progressCalls.push({ url, body });
          return jsonResponse({
            lesson_id: 'l1',
            position_ticks: body.position_ticks,
            played: body.position_ticks >= 1080000000,
            play_count: 1,
            updated_at: '2026-11-23T00:00:00Z',
          });
        }
        return jsonResponse({}, 404);
      }),
    );

    const onEnded = vi.fn();
    const playback = makePlayback({
      progress: {
        position_ticks: 30 * 10_000_000,
        played: false,
        play_count: 0,
      },
    });

    const { container } = render(<VideoPlayer playback={playback} onEnded={onEnded} />);
    const video = container.querySelector('video') as HTMLVideoElement;

    Object.defineProperty(video, 'duration', { value: 120, configurable: true });

    fireEvent.loadedMetadata(video);
    expect(video.currentTime).toBe(30);

    video.currentTime = 42.5;
    fireEvent.pause(video);

    await waitFor(() => {
      expect(progressCalls).toHaveLength(1);
    });
    expect(progressCalls[0].body.position_ticks).toBe(425_000_000);

    video.currentTime = 120;
    fireEvent.ended(video);

    await waitFor(() => {
      expect(progressCalls).toHaveLength(2);
      expect(onEnded).toHaveBeenCalledTimes(1);
    });
    expect(progressCalls[1].body.position_ticks).toBe(1_200_000_000);
  });

  it('initializes hls.js for hls playback mode and cleans up ActiveEncodings with api_key without clearing token on 401', async () => {
    const deleteCalls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (init?.method === 'DELETE' && url.includes('/Videos/ActiveEncodings')) {
          deleteCalls.push(url);
          return jsonResponse({ error: 'unauthorized' }, 401);
        }
        return jsonResponse({}, 404);
      }),
    );

    const playback = makePlayback({
      mode: 'hls',
      play_session_id: 'hls-sess-99',
      stream_url: '/Videos/l1/master.m3u8?PlaySessionId=hls-sess-99&MediaSourceId=mf1',
    });

    const { unmount } = render(<VideoPlayer playback={playback} />);

    expect(hlsMockInstances).toHaveLength(1);
    expect(hlsMockInstances[0].loadSource).toHaveBeenCalledWith(
      '/Videos/l1/master.m3u8?PlaySessionId=hls-sess-99&MediaSourceId=mf1&api_key=test-token-123',
    );
    expect(hlsMockInstances[0].attachMedia).toHaveBeenCalled();

    unmount();
    expect(hlsMockInstances[0].destroy).toHaveBeenCalled();
    await waitFor(() => {
      expect(deleteCalls).toHaveLength(1);
    });
    expect(deleteCalls[0]).toContain('PlaySessionId=hls-sess-99');
    expect(deleteCalls[0]).toContain('api_key=test-token-123');
    expect(localStorage.getItem('coursefin_token')).toBe('test-token-123');
  });

  it('handles Space, ArrowLeft, and ArrowRight keyboard shortcuts', () => {
    const playback = makePlayback();
    const { container } = render(<VideoPlayer playback={playback} />);
    const video = container.querySelector('video') as HTMLVideoElement;

    Object.defineProperty(video, 'duration', { value: 120, configurable: true });
    const playSpy = vi.spyOn(video, 'play').mockResolvedValue();
    const pauseSpy = vi.spyOn(video, 'pause').mockImplementation(() => {});

    video.currentTime = 25;
    fireEvent.keyDown(window, { key: 'ArrowRight', code: 'ArrowRight' });
    expect(video.currentTime).toBe(35);

    fireEvent.keyDown(window, { key: 'ArrowLeft', code: 'ArrowLeft' });
    expect(video.currentTime).toBe(25);

    // jsdom video.paused defaults to true -> Space calls play()
    fireEvent.keyDown(window, { key: ' ', code: 'Space' });
    expect(playSpy).toHaveBeenCalledTimes(1);

    Object.defineProperty(video, 'paused', { value: false, configurable: true });
    fireEvent.keyDown(window, { key: ' ', code: 'Space' });
    expect(pauseSpy).toHaveBeenCalledTimes(1);
  });
});

describe('CourseDetailPage', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token-123');
    hlsMockInstances.length = 0;
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('renders course detail, playlist sidebar, toggles played status, and auto-advances on ended', async () => {
    const course = makeCourse();
    const playbackL1 = makePlayback({ lesson_id: 'l1' });
    const playbackL2 = makePlayback({
      lesson_id: 'l2',
      media_file_id: 'mf2',
      stream_url: '/Videos/l2/stream.mp4?Static=true&MediaSourceId=mf2',
      subtitles: [],
      duration_ms: 180000,
    });

    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v1/courses/c1')) {
        return jsonResponse(course);
      }
      if (url.endsWith('/api/v1/lessons/l1/playback')) {
        return jsonResponse(playbackL1);
      }
      if (url.endsWith('/api/v1/lessons/l2/playback')) {
        return jsonResponse(playbackL2);
      }
      if (url.endsWith('/api/v1/lessons/l1/played') && init?.method === 'POST') {
        const body = JSON.parse(String(init.body ?? '{}')) as { played: boolean };
        return jsonResponse({
          lesson_id: 'l1',
          position_ticks: 0,
          played: body.played,
          play_count: body.played ? 1 : 0,
          updated_at: '2026-11-23T00:00:00Z',
        });
      }
      if (url.endsWith('/api/v1/lessons/l1/progress') && init?.method === 'POST') {
        return jsonResponse({
          lesson_id: 'l1',
          position_ticks: 1200000000,
          played: true,
          play_count: 1,
          updated_at: '2026-11-23T00:00:00Z',
        });
      }
      return jsonResponse({}, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    const { container } = render(
      <MemoryRouter initialEntries={['/courses/c1']}>
        <Routes>
          <Route path="/courses/:courseId" element={<CourseDetailPage />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByRole('heading', { name: 'Go 微服务全栈实战' })).toBeInTheDocument();
    expect(screen.getAllByText('第一章：架构基础').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('1.1 服务拆分原则').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('1.2 gRPC 实战与拦截器')).toBeInTheDocument();

    const editLink = screen.getByRole('link', { name: /编辑课程/ });
    expect(editLink).toHaveAttribute('href', '/courses/c1/edit');

    // Toggle manual played status
    const user = userEvent.setup();
    const markPlayedBtn = await screen.findByRole('button', { name: '标记已看' });
    await user.click(markPlayedBtn);

    expect(await screen.findByRole('button', { name: '标记未看' })).toBeInTheDocument();
    expect(screen.getByText(/已学 1 \/ 2 节/)).toBeInTheDocument();

    // Wait for video element and simulate ended -> should auto-advance to l2
    const video = container.querySelector('video') as HTMLVideoElement;
    expect(video).toBeTruthy();
    Object.defineProperty(video, 'duration', { value: 120, configurable: true });
    fireEvent.ended(video);

    await waitFor(() => {
      expect(
        mockFetch.mock.calls.some(([u]) => String(u).endsWith('/api/v1/lessons/l2/playback')),
      ).toBe(true);
    });
  });

  it('retries loading playback info when clicking 重新获取 after failure', async () => {
    const course = makeCourse();
    const playbackL1 = makePlayback({ lesson_id: 'l1' });
    let playbackAttempts = 0;

    const mockFetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/v1/courses/c1')) {
        return jsonResponse(course);
      }
      if (url.endsWith('/api/v1/lessons/l1/playback')) {
        playbackAttempts += 1;
        if (playbackAttempts === 1) {
          return jsonResponse({ error: 'playback unavailable' }, 500);
        }
        return jsonResponse(playbackL1);
      }
      return jsonResponse({}, 404);
    });
    vi.stubGlobal('fetch', mockFetch);

    const { container } = render(
      <MemoryRouter initialEntries={['/courses/c1']}>
        <Routes>
          <Route path="/courses/:courseId" element={<CourseDetailPage />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText('playback unavailable')).toBeInTheDocument();
    const retryBtn = screen.getByRole('button', { name: '重新获取' });

    const user = userEvent.setup();
    await user.click(retryBtn);

    await waitFor(() => {
      expect(playbackAttempts).toBe(2);
      expect(container.querySelector('video')).toBeTruthy();
    });
  });
});
