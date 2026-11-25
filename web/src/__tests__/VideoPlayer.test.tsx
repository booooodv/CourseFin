import { createRef } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import VideoPlayer, {
  SUBTITLE_PREF_STORAGE_KEY,
  pickInitialSubtitle,
  type VideoPlayerHandle,
} from '../components/VideoPlayer';
import LessonProgressIcon from '../components/LessonProgressIcon';
import CourseDetailPage from '../pages/CourseDetailPage';
import type { CourseDetail, LessonPlaybackInfo, PlaybackSubtitleItem } from '../types/coursefin';

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
  };
  controls: {
    update: ReturnType<typeof vi.fn>;
  };
  on: ReturnType<typeof vi.fn>;
  off: ReturnType<typeof vi.fn>;
  emit: (name: string, ...args: unknown[]) => void;
  destroy: ReturnType<typeof vi.fn>;
  toggle: ReturnType<typeof vi.fn>;
  play: ReturnType<typeof vi.fn>;
  pause: ReturnType<typeof vi.fn>;
  get playbackRate(): number;
  set playbackRate(val: number);
}

const artMockInstances: MockArtInstance[] = [];

vi.mock('artplayer', () => {
  class MockArtplayer {
    static SEEK_STEP = 5;
    static PLAYBACK_RATE = [0.5, 0.75, 1, 1.25, 1.5, 2];
    static RECONNECT_TIME_MAX = 5;

    option: Record<string, any>;
    video: HTMLVideoElement;
    isFocus = false;
    isDestroy = false;
    theme = '#1E88E5';
    fullscreen = false;
    fullscreenWeb = false;
    private listeners = new Map<string, Array<(...args: unknown[]) => void>>();

    subtitle: {
      url: string;
      show: boolean;
      switch: ReturnType<typeof vi.fn>;
      style: ReturnType<typeof vi.fn>;
    };

    setting = {
      update: vi.fn(),
    };

    controls = {
      update: vi.fn(),
    };

    on = vi.fn((name: string, fn: (...args: unknown[]) => void) => {
      const list = this.listeners.get(name) ?? [];
      list.push(fn);
      this.listeners.set(name, list);
      return this;
    });

    off = vi.fn((name: string, fn?: (...args: unknown[]) => void) => {
      if (!fn) {
        this.listeners.delete(name);
      } else {
        const list = this.listeners.get(name) ?? [];
        this.listeners.set(
          name,
          list.filter((cb) => cb !== fn),
        );
      }
      return this;
    });

    emit = (name: string, ...args: unknown[]) => {
      const list = this.listeners.get(name) ?? [];
      for (const fn of list) {
        fn(...args);
      }
    };

    play = vi.fn(() => this.video.play());
    pause = vi.fn(() => this.video.pause());
    toggle = vi.fn(() => {
      if (this.video.paused) {
        void this.video.play();
      } else {
        this.video.pause();
      }
    });

    destroy = vi.fn((removeHtml?: boolean) => {
      this.isDestroy = true;
      if (removeHtml && this.option.container instanceof HTMLElement) {
        this.option.container.innerHTML = '';
      }
    });

    get playbackRate(): number {
      return this.video.playbackRate;
    }

    set playbackRate(val: number) {
      this.video.playbackRate = val;
    }

    constructor(option: Record<string, any>, readyCallback?: (art: MockArtplayer) => void) {
      this.option = option;
      this.theme = option.theme ?? '#1E88E5';

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

      if (option.type === 'm3u8' && option.customType?.m3u8) {
        option.customType.m3u8.call(this, this.video, option.url, this);
      } else if (option.url) {
        this.video.setAttribute('src', option.url);
      }

      artMockInstances.push(this as unknown as MockArtInstance);
      if (readyCallback) {
        readyCallback(this);
      }
    }
  }

  return { default: MockArtplayer };
});

const hlsMockInstances: Array<{
  config?: unknown;
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
    config?: unknown;
    loadSource = vi.fn();
    attachMedia = vi.fn();
    destroy = vi.fn();
    on = vi.fn();
    off = vi.fn();
    constructor(config?: unknown) {
      this.config = config;
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

describe('pickInitialSubtitle', () => {
  const zhDefault: PlaybackSubtitleItem = {
    index: 1,
    language: 'zh',
    title: '中文',
    format: 'vtt',
    is_default: true,
    is_external: true,
    is_text: true,
    url: '/sub/1.vtt',
  };
  const enTrack: PlaybackSubtitleItem = {
    index: 2,
    language: 'en',
    title: 'English',
    format: 'vtt',
    is_default: false,
    is_external: true,
    is_text: true,
    url: '/sub/2.vtt',
  };
  const zhNonDefault: PlaybackSubtitleItem = {
    index: 3,
    language: 'zh-CN',
    title: '简体中文',
    format: 'vtt',
    is_default: false,
    is_external: true,
    is_text: true,
    url: '/sub/3.vtt',
  };
  const jaTrack: PlaybackSubtitleItem = {
    index: 4,
    language: 'ja',
    title: '日本語',
    format: 'vtt',
    is_default: false,
    is_external: true,
    is_text: true,
    url: '/sub/4.vtt',
  };
  const bitmapTrack: PlaybackSubtitleItem = {
    index: 5,
    language: 'zh',
    title: 'PGS 中文',
    format: 'pgssub',
    is_default: true,
    is_external: false,
    is_text: false,
    url: '',
  };

  it('respects savedPref === "off" and returns null', () => {
    expect(pickInitialSubtitle([zhDefault, enTrack], 'off')).toBeNull();
  });

  it('matches savedPref === "en" over default Chinese track', () => {
    expect(pickInitialSubtitle([zhDefault, enTrack], 'en')).toEqual(enTrack);
  });

  it('falls back to is_default -> Chinese -> first text track when no savedPref', () => {
    expect(pickInitialSubtitle([enTrack, zhDefault], null)).toEqual(zhDefault);
    expect(pickInitialSubtitle([enTrack, zhNonDefault], null)).toEqual(zhNonDefault);
    expect(pickInitialSubtitle([bitmapTrack, enTrack, jaTrack], null)).toEqual(enTrack);
    expect(pickInitialSubtitle([bitmapTrack], null)).toBeNull();
  });
});

describe('VideoPlayer', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token-123');
    artMockInstances.length = 0;
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
    expect(artMockInstances).toHaveLength(0);
    expect(container.innerHTML).not.toMatch(/\bbg-slate-900\b/);
    expect(container.innerHTML).not.toMatch(/\b(slate|indigo|rose|emerald)-/);
  });

  it('mounts Artplayer with zh-cn options, auto-selected subtitle, subtitle switch/off persistence, and subtitle size control', async () => {
    const playback = makePlayback();
    render(<VideoPlayer playback={playback} />);

    expect(artMockInstances).toHaveLength(1);
    const art = artMockInstances[0];
    expect(art.option.lang).toBe('zh-cn');
    expect(art.option.fullscreenWeb).toBe(true);
    expect(art.option.pip).toBe(true);
    expect(art.option.miniProgressBar).toBe(true);
    expect(art.option.screenshot).toBe(true);
    expect(art.option.subtitleOffset).toBe(true);
    expect(art.option.url).toBe(
      '/Videos/l1/stream.mp4?Static=true&MediaSourceId=mf1&api_key=test-token-123',
    );
    expect(art.option.subtitle.url).toBe(
      '/Videos/l1/mf1/Subtitles/2/Stream.vtt?api_key=test-token-123',
    );
    expect(art.option.subtitle.type).toBe('vtt');
    expect(art.option.subtitle.escape).toBe(true);

    // Control bar subtitle selector & settings subtitle selector must NOT share mutable item refs
    const subtitleControl = (art.option.controls as Array<Record<string, any>>).find(
      (c) => c.name === 'coursefin-subtitle',
    );
    const subtitleSetting = (art.option.settings as Array<Record<string, any>>).find(
      (s) => s.html === '字幕轨道',
    );
    expect(subtitleControl).toBeTruthy();
    expect(subtitleSetting).toBeTruthy();
    expect(subtitleControl!.selector).not.toBe(subtitleSetting!.selector);
    expect(subtitleControl!.selector[0]).not.toBe(subtitleSetting!.selector[0]);
    expect(subtitleControl!.selector.map((s: any) => s.html)).toEqual(['关闭', '中文', 'English']);

    // Switch to English via control selector -> syncs settings menu
    const enItem = subtitleControl!.selector[2];
    let controlLabel = '';
    act(() => {
      controlLabel = subtitleControl!.onSelect.call(art, enItem);
    });
    expect(controlLabel).toContain('English');
    expect(art.subtitle.switch).toHaveBeenCalledWith(
      '/Videos/l1/mf1/Subtitles/3/Stream.vtt?api_key=test-token-123',
      expect.objectContaining({ name: 'English', type: 'vtt', escape: true }),
    );
    expect(art.subtitle.show).toBe(true);
    expect(localStorage.getItem(SUBTITLE_PREF_STORAGE_KEY)).toBe('en');
    expect(art.setting.update).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'coursefin-subtitle-setting',
        tooltip: 'English',
      }),
    );

    // Turn off via setting selector -> syncs control bar menu
    const offItem = subtitleSetting!.selector[0];
    let settingLabel = '';
    act(() => {
      settingLabel = subtitleSetting!.onSelect.call(art, offItem);
    });
    expect(settingLabel).toBe('关闭');
    expect(art.subtitle.show).toBe(false);
    expect(localStorage.getItem(SUBTITLE_PREF_STORAGE_KEY)).toBe('off');
    expect(art.controls.update).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'coursefin-subtitle',
        html: '字幕',
      }),
    );

    // Change subtitle size via settings
    const sizeSetting = (art.option.settings as Array<Record<string, any>>).find(
      (s) => s.html === '字幕字号',
    );
    expect(sizeSetting).toBeTruthy();
    const largeItem = sizeSetting!.selector.find((s: any) => String(s.html).includes('24px'));
    expect(largeItem).toBeTruthy();
    act(() => {
      sizeSetting!.onSelect.call(art, largeItem);
    });
    expect(art.subtitle.style).toHaveBeenCalledWith({ fontSize: '24px' });
  });

  it('persists playbackRate to localStorage on ratechange and restores saved rate on loadedmetadata', () => {
    localStorage.setItem('coursefin_playback_rate', '1.5');
    const playback = makePlayback();
    const { container } = render(<VideoPlayer playback={playback} />);

    const video = container.querySelector('video') as HTMLVideoElement;
    expect(video).toBeTruthy();

    fireEvent.loadedMetadata(video);
    expect(video.playbackRate).toBe(1.5);

    video.playbackRate = 1.75;
    fireEvent.rateChange(video);
    expect(localStorage.getItem('coursefin_playback_rate')).toBe('1.75');
  });

  it('reports playback progress ticks to API on pause and ended, resumes from position_ticks, and emits onTimeProgress', async () => {
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
    const onTimeProgress = vi.fn();
    const playback = makePlayback({
      progress: {
        position_ticks: 30 * 10_000_000,
        played: false,
        play_count: 0,
      },
    });

    const { container } = render(
      <VideoPlayer playback={playback} onEnded={onEnded} onTimeProgress={onTimeProgress} />,
    );
    const video = container.querySelector('video') as HTMLVideoElement;

    Object.defineProperty(video, 'duration', { value: 120, configurable: true });

    fireEvent.loadedMetadata(video);
    expect(video.currentTime).toBe(30);

    video.currentTime = 42.5;
    fireEvent.timeUpdate(video);
    expect(onTimeProgress).toHaveBeenCalledWith('l1', 42.5, 120);

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

  it('initializes hls.js for hls playback mode, shows transcoding badge when transcoding, and cleans up ActiveEncodings without clearing token on 401', async () => {
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
      transcoding: true,
      play_session_id: 'hls-sess-99',
      stream_url: '/Videos/l1/master.m3u8?PlaySessionId=hls-sess-99&MediaSourceId=mf1&TranscodeVideo=1',
    });

    const { unmount } = render(<VideoPlayer playback={playback} />);

    expect(screen.getByText('兼容转码播放')).toBeInTheDocument();
    expect(hlsMockInstances).toHaveLength(1);
    expect(hlsMockInstances[0].loadSource).toHaveBeenCalledWith(
      '/Videos/l1/master.m3u8?PlaySessionId=hls-sess-99&MediaSourceId=mf1&TranscodeVideo=1&api_key=test-token-123',
    );
    expect(hlsMockInstances[0].attachMedia).toHaveBeenCalled();

    unmount();
    expect(hlsMockInstances[0].destroy).toHaveBeenCalled();
    expect(artMockInstances[0].destroy).toHaveBeenCalled();
    await waitFor(() => {
      expect(deleteCalls).toHaveLength(1);
    });
    expect(deleteCalls[0]).toContain('PlaySessionId=hls-sess-99');
    expect(deleteCalls[0]).toContain('api_key=test-token-123');
    expect(localStorage.getItem('coursefin_token')).toBe('test-token-123');
  });

  it('fires onRequestTranscodeFallback once on pre-play DirectPlay error, and not after playing or in HLS mode', () => {
    const onFallback = vi.fn();
    const { container, unmount } = render(
      <VideoPlayer playback={makePlayback({ mode: 'direct' })} onRequestTranscodeFallback={onFallback} />,
    );
    const video = container.querySelector('video') as HTMLVideoElement;

    fireEvent.error(video);
    fireEvent.error(video);
    expect(onFallback).toHaveBeenCalledTimes(1);
    unmount();

    // After playing started -> should NOT trigger fallback, shows error banner instead
    const onFallbackAfterPlay = vi.fn();
    const { container: c2, unmount: u2 } = render(
      <VideoPlayer
        playback={makePlayback({ mode: 'direct', play_session_id: 'sess-2' })}
        onRequestTranscodeFallback={onFallbackAfterPlay}
      />,
    );
    const v2 = c2.querySelector('video') as HTMLVideoElement;
    fireEvent.playing(v2);
    fireEvent.error(v2);
    expect(onFallbackAfterPlay).not.toHaveBeenCalled();
    expect(screen.getByText(/视频播放出错/)).toBeInTheDocument();
    u2();

    // In HLS mode -> should NOT trigger fallback
    const onFallbackHls = vi.fn();
    const { container: c3, unmount: u3 } = render(
      <VideoPlayer
        playback={makePlayback({
          mode: 'hls',
          play_session_id: 'sess-hls',
          stream_url: '/Videos/l1/master.m3u8?PlaySessionId=sess-hls&MediaSourceId=mf1',
        })}
        onRequestTranscodeFallback={onFallbackHls}
      />,
    );
    const v3 = c3.querySelector('video') as HTMLVideoElement;
    fireEvent.error(v3);
    expect(onFallbackHls).not.toHaveBeenCalled();
    u3();
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

    fireEvent.keyDown(window, { key: ' ', code: 'Space' });
    expect(playSpy).toHaveBeenCalledTimes(1);

    Object.defineProperty(video, 'paused', { value: false, configurable: true });
    fireEvent.keyDown(window, { key: ' ', code: 'Space' });
    expect(pauseSpy).toHaveBeenCalledTimes(1);
  });

  it('passes { startPosition } to Hls on unplayed resume and omits it when played or zero', () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({}, 204)));

    const resumePlayback = makePlayback({
      mode: 'hls',
      play_session_id: 'hls-resume-1',
      stream_url: '/Videos/l1/master.m3u8?PlaySessionId=hls-resume-1&MediaSourceId=mf1',
      progress: {
        position_ticks: 45 * 10_000_000,
        played: false,
        play_count: 0,
      },
    });
    const { unmount: unmountResume } = render(<VideoPlayer playback={resumePlayback} />);
    expect(hlsMockInstances).toHaveLength(1);
    expect(hlsMockInstances[0].config).toEqual({ startPosition: 45 });
    unmountResume();

    const playedPlayback = makePlayback({
      mode: 'hls',
      play_session_id: 'hls-played-2',
      stream_url: '/Videos/l1/master.m3u8?PlaySessionId=hls-played-2&MediaSourceId=mf1',
      progress: {
        position_ticks: 45 * 10_000_000,
        played: true,
        play_count: 1,
      },
    });
    const { unmount: unmountPlayed } = render(<VideoPlayer playback={playedPlayback} />);
    expect(hlsMockInstances).toHaveLength(2);
    expect(hlsMockInstances[1].config).toBeUndefined();
    unmountPlayed();
  });

  it('flushes latest progress on unmount only when not already flushed or ended', async () => {
    const progressCalls: Array<{ body: { position_ticks: number }; keepalive?: boolean }> = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/lessons/l1/progress') && init?.method === 'POST') {
          const body = JSON.parse(String(init.body ?? '{}')) as { position_ticks: number };
          progressCalls.push({ body, keepalive: init.keepalive });
          return jsonResponse({
            lesson_id: 'l1',
            position_ticks: body.position_ticks,
            played: false,
            play_count: 0,
            updated_at: '2026-11-23T00:00:00Z',
          });
        }
        return jsonResponse({}, 404);
      }),
    );

    const { container, unmount } = render(<VideoPlayer playback={makePlayback()} />);
    const video = container.querySelector('video') as HTMLVideoElement;
    video.currentTime = 53.2;
    fireEvent.timeUpdate(video);

    unmount();
    await waitFor(() => {
      expect(progressCalls).toHaveLength(1);
    });
    expect(progressCalls[0].body.position_ticks).toBe(532_000_000);
    expect(progressCalls[0].keepalive).toBe(true);

    progressCalls.length = 0;
    const { container: c2, unmount: unmount2 } = render(<VideoPlayer playback={makePlayback()} />);
    const v2 = c2.querySelector('video') as HTMLVideoElement;
    v2.currentTime = 18;
    fireEvent.timeUpdate(v2);
    fireEvent.pause(v2);
    await waitFor(() => {
      expect(progressCalls).toHaveLength(1);
    });
    unmount2();
    expect(progressCalls).toHaveLength(1);

    progressCalls.length = 0;
    const { container: c3, unmount: unmount3 } = render(<VideoPlayer playback={makePlayback()} />);
    const v3 = c3.querySelector('video') as HTMLVideoElement;
    Object.defineProperty(v3, 'duration', { value: 120, configurable: true });
    v3.currentTime = 120;
    fireEvent.timeUpdate(v3);
    fireEvent.ended(v3);
    await waitFor(() => {
      expect(progressCalls).toHaveLength(1);
    });
    unmount3();
    expect(progressCalls).toHaveLength(1);
  });

  it('exposes seekTo via ref to jump video playback safely clamped to duration', () => {
    const ref = createRef<VideoPlayerHandle>();
    const onTimeProgress = vi.fn();
    const playback = makePlayback({ duration_ms: 120_000 });

    const { container, rerender } = render(
      <VideoPlayer ref={ref} playback={playback} onTimeProgress={onTimeProgress} />,
    );
    const video = container.querySelector('video') as HTMLVideoElement;
    expect(video).toBeTruthy();
    expect(ref.current).toBeTruthy();

    Object.defineProperty(video, 'duration', { value: 200, configurable: true });
    const playSpy = vi.spyOn(video, 'play').mockResolvedValue();

    act(() => {
      ref.current!.seekTo(75);
    });
    expect(video.currentTime).toBe(75);
    expect(playSpy).toHaveBeenCalledTimes(1);
    expect(onTimeProgress).toHaveBeenCalledWith('l1', 75, 200);

    act(() => {
      ref.current!.seekTo(999, false);
    });
    expect(video.currentTime).toBe(200);
    expect(playSpy).toHaveBeenCalledTimes(1);

    act(() => {
      ref.current!.seekTo(-15, false);
    });
    expect(video.currentTime).toBe(0);
    expect(playSpy).toHaveBeenCalledTimes(1);

    // Fallback to playback.duration_ms when video.duration is not ready (NaN)
    Object.defineProperty(video, 'duration', { value: Number.NaN, configurable: true });
    rerender(<VideoPlayer ref={ref} playback={playback} onTimeProgress={onTimeProgress} />);
    act(() => {
      ref.current!.seekTo(500, false);
    });
    expect(video.currentTime).toBe(120);
  });

  it('updates subtitle selectors in-place without destroying Artplayer when subtitles prop updates for the same lesson', () => {
    const initialPlayback = makePlayback({ subtitles: [] });
    const { rerender } = render(<VideoPlayer playback={initialPlayback} />);

    expect(artMockInstances).toHaveLength(1);
    const art = artMockInstances[0];
    expect(art.destroy).not.toHaveBeenCalled();
    expect(screen.getByText('无可用字幕')).toBeInTheDocument();

    const aiSub: PlaybackSubtitleItem = {
      index: 100,
      language: 'zh',
      title: 'AI 转写字幕',
      format: 'vtt',
      is_default: false,
      is_external: true,
      is_text: true,
      url: '/Videos/l1/mf1/Subtitles/100/Stream.vtt',
    };
    const updatedPlayback = makePlayback({ subtitles: [aiSub] });
    rerender(<VideoPlayer playback={updatedPlayback} />);

    expect(artMockInstances).toHaveLength(1);
    expect(art.destroy).not.toHaveBeenCalled();

    const lastControlCall = art.controls.update.mock.calls.at(-1)?.[0] as Record<string, any>;
    expect(lastControlCall).toEqual(
      expect.objectContaining({
        name: 'coursefin-subtitle',
        position: 'right',
        index: 15,
      }),
    );
    expect(lastControlCall.selector.map((s: any) => s.html)).toEqual(['关闭', 'AI 转写字幕']);

    const settingSubCall = art.setting.update.mock.calls
      .map((c) => c[0] as Record<string, any>)
      .filter((arg) => arg.name === 'coursefin-subtitle-setting')
      .at(-1);
    expect(settingSubCall).toBeTruthy();
    expect(settingSubCall!.selector.map((s: any) => s.html)).toEqual(['关闭', 'AI 转写字幕']);

    const settingSizeCall = art.setting.update.mock.calls
      .map((c) => c[0] as Record<string, any>)
      .filter((arg) => arg.html === '字幕字号')
      .at(-1);
    expect(settingSizeCall).toBeTruthy();

    // Selecting newly injected AI subtitle via control bar works (no stale closure)
    const aiItem = lastControlCall.selector[1];
    act(() => {
      lastControlCall.onSelect.call(art, aiItem);
    });
    expect(art.subtitle.switch).toHaveBeenCalledWith(
      '/Videos/l1/mf1/Subtitles/100/Stream.vtt?api_key=test-token-123',
      expect.objectContaining({ name: 'AI 转写字幕', type: 'vtt', escape: true }),
    );
    expect(art.subtitle.show).toBe(true);
    expect(screen.getByText('AI 转写字幕')).toBeInTheDocument();

    // Subsequent subtitle update keeps already-active selection intact
    const enSub: PlaybackSubtitleItem = {
      index: 101,
      language: 'en',
      title: 'English',
      format: 'vtt',
      is_default: false,
      is_external: true,
      is_text: true,
      url: '/Videos/l1/mf1/Subtitles/101/Stream.vtt',
    };
    art.subtitle.switch.mockClear();
    rerender(<VideoPlayer playback={makePlayback({ subtitles: [aiSub, enSub] })} />);
    expect(artMockInstances).toHaveLength(1);
    expect(art.destroy).not.toHaveBeenCalled();
    expect(art.subtitle.switch).not.toHaveBeenCalled();
    const afterControlCall = art.controls.update.mock.calls.at(-1)?.[0] as Record<string, any>;
    expect(afterControlCall.selector.find((s: any) => s.value === '100')?.default).toBe(true);
    expect(afterControlCall.selector.map((s: any) => s.html)).toEqual([
      '关闭',
      'AI 转写字幕',
      'English',
    ]);
  });
});

describe('CourseDetailPage', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token-123');
    artMockInstances.length = 0;
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
      if (url.includes('/api/v1/lessons/l1/playback')) {
        return jsonResponse(playbackL1);
      }
      if (url.includes('/api/v1/lessons/l2/playback')) {
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

    const user = userEvent.setup();
    const markPlayedBtn = await screen.findByRole('button', { name: '标记已看' });
    await user.click(markPlayedBtn);

    expect(await screen.findByRole('button', { name: '标记未看' })).toBeInTheDocument();
    expect(screen.getByText(/已学 1 \/ 2 节/)).toBeInTheDocument();

    const video = container.querySelector('video') as HTMLVideoElement;
    expect(video).toBeTruthy();
    Object.defineProperty(video, 'duration', { value: 120, configurable: true });
    fireEvent.ended(video);

    await waitFor(() => {
      expect(
        mockFetch.mock.calls.some(([u]) => String(u).includes('/api/v1/lessons/l2/playback')),
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
      if (url.includes('/api/v1/lessons/l1/playback')) {
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

  it('renders theater-first layout with compact header, video before course overview, and dual-theme tokens', async () => {
    const course = makeCourse();
    const playbackL1 = makePlayback({ lesson_id: 'l1' });

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) {
          return jsonResponse(course);
        }
        if (url.includes('/api/v1/lessons/l1/playback')) {
          return jsonResponse(playbackL1);
        }
        return jsonResponse({}, 404);
      }),
    );

    const { container } = render(
      <MemoryRouter initialEntries={['/courses/c1']}>
        <Routes>
          <Route path="/courses/:courseId" element={<CourseDetailPage />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByRole('link', { name: /返回课程库/ })).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1, name: 'Go 微服务全栈实战' })).toBeInTheDocument();
    expect(screen.getByText(/已学 0\/2 节 · 0%/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /收起目录/ })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /编辑课程/ })).toBeInTheDocument();

    let video: HTMLVideoElement | null = null;
    await waitFor(() => {
      video = container.querySelector('video');
      expect(video).toBeTruthy();
    });
    const overviewEl = screen.getByText('从零设计并部署高并发 Go 微服务系统。');
    expect(overviewEl).toBeInTheDocument();

    expect(
      Boolean(video!.compareDocumentPosition(overviewEl) & Node.DOCUMENT_POSITION_FOLLOWING),
    ).toBe(true);

    expect(container.innerHTML).not.toMatch(/\bbg-slate-900\b/);
    expect(container.innerHTML).not.toMatch(/\b(slate|indigo|rose|emerald)-/);
  });

  it('sends browser codecs, renders syllabus circular progress rings, and updates active ring live on timeupdate', async () => {
    const course = makeCourse();
    // l2 has watched 50% (90s / 180s)
    course.chapters[0].lessons[1].position_ticks = 90 * 10_000_000;
    const playbackL1 = makePlayback({ lesson_id: 'l1' });
    const playbackUrls: string[] = [];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) {
          return jsonResponse(course);
        }
        if (url.includes('/api/v1/lessons/l1/playback')) {
          playbackUrls.push(url);
          return jsonResponse(playbackL1);
        }
        return jsonResponse({}, 404);
      }),
    );

    const { container } = render(
      <MemoryRouter initialEntries={['/courses/c1?lesson=l1']}>
        <Routes>
          <Route path="/courses/:courseId" element={<CourseDetailPage />} />
        </Routes>
      </MemoryRouter>,
    );

    let video: HTMLVideoElement | null = null;
    await waitFor(() => {
      video = container.querySelector('video');
      expect(video).toBeTruthy();
    });

    // Must send ?codecs= on playback request
    expect(playbackUrls).toHaveLength(1);
    expect(playbackUrls[0]).toContain('codecs=');

    // l2 should show 50% circular ring
    expect(screen.getByLabelText('已观看 50%')).toHaveAttribute('data-progress-percent', '50');

    // Simulate live playback advancing l1 to 78s / 120s (65%)
    Object.defineProperty(video!, 'duration', { value: 120, configurable: true });
    video!.currentTime = 78;
    fireEvent.timeUpdate(video!);

    await waitFor(() => {
      expect(screen.getByLabelText('正在播放 · 已观看 65%')).toHaveAttribute(
        'data-progress-percent',
        '65',
      );
    });
  });

  it('retries once with force_transcode=1 on pre-play DirectPlay error and does not loop on second error', async () => {
    const course = makeCourse();
    const playbackUrls: string[] = [];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/courses/c1')) {
          return jsonResponse(course);
        }
        if (url.includes('/api/v1/lessons/l1/playback')) {
          playbackUrls.push(url);
          if (url.includes('force_transcode=1')) {
            return jsonResponse(
              makePlayback({
                lesson_id: 'l1',
                mode: 'hls',
                transcoding: true,
                play_session_id: 'sess-transcode',
                stream_url:
                  '/Videos/l1/master.m3u8?PlaySessionId=sess-transcode&MediaSourceId=mf1&TranscodeVideo=1',
              }),
            );
          }
          return jsonResponse(
            makePlayback({
              lesson_id: 'l1',
              mode: 'direct',
              play_session_id: 'sess-direct',
            }),
          );
        }
        return jsonResponse({}, 204);
      }),
    );

    const { container } = render(
      <MemoryRouter initialEntries={['/courses/c1']}>
        <Routes>
          <Route path="/courses/:courseId" element={<CourseDetailPage />} />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(container.querySelector('video')).toBeTruthy();
    });
    expect(playbackUrls).toHaveLength(1);
    expect(playbackUrls[0]).not.toContain('force_transcode=1');

    // Direct play fails before starting -> auto-fallback to force_transcode=1
    fireEvent.error(container.querySelector('video')!);

    await waitFor(() => {
      expect(playbackUrls).toHaveLength(2);
      expect(playbackUrls[1]).toContain('force_transcode=1');
      expect(screen.getByText('兼容转码播放')).toBeInTheDocument();
    });

    // If fallback HLS video also errors, must NOT trigger another playback fetch
    fireEvent.error(container.querySelector('video')!);
    expect(playbackUrls).toHaveLength(2);
  });
});

describe('LessonProgressIcon', () => {
  it('renders all 4 visual states with accurate aria-label, data-progress-percent, and SVG elements', () => {
    const { container: cPlayed } = render(
      <LessonProgressIcon played={true} progressRatio={0.4} isActive={false} />,
    );
    const playedEl = within(cPlayed).getByLabelText('已学完');
    expect(playedEl).toHaveAttribute('data-progress-percent', '100');

    const { container: cActive } = render(
      <LessonProgressIcon played={false} progressRatio={0.65} isActive={true} />,
    );
    const activeEl = within(cActive).getByLabelText('正在播放 · 已观看 65%');
    expect(activeEl).toHaveAttribute('data-progress-percent', '65');
    expect(cActive.querySelector('circle[stroke-dashoffset]')).toBeTruthy();
    expect(cActive.querySelector('polygon')).toBeTruthy();

    const { container: cPartial } = render(
      <LessonProgressIcon played={false} progressRatio={0.42} isActive={false} />,
    );
    const partialEl = within(cPartial).getByLabelText('已观看 42%');
    expect(partialEl).toHaveAttribute('data-progress-percent', '42');
    expect(cPartial.querySelector('circle[stroke-dashoffset]')).toBeTruthy();
    expect(cPartial.querySelector('polygon')).toBeNull();

    const { container: cUnwatched } = render(
      <LessonProgressIcon played={false} progressRatio={0} isActive={false} />,
    );
    const unwatchedEl = within(cUnwatched).getByLabelText('未播放');
    expect(unwatchedEl).toHaveAttribute('data-progress-percent', '0');
    expect(cUnwatched.querySelector('polygon')).toBeNull();
  });
});
