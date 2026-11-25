import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import Artplayer from 'artplayer';
import type { Option as ArtOption, Setting } from 'artplayer';
import Hls from 'hls.js';
import {
  AlertTriangle,
  Captions,
  Cpu,
  MonitorPlay,
  Tv,
} from 'lucide-react';
import { api, withApiKey } from '../api/client';
import Tooltip from './Tooltip';
import type {
  LessonPlaybackInfo,
  PlaybackSubtitleItem,
  ProgressResponse,
} from '../types/coursefin';

export const PLAYBACK_RATE_STORAGE_KEY = 'coursefin_playback_rate';
export const SUBTITLE_PREF_STORAGE_KEY = 'coursefin_subtitle_pref';
export const SUBTITLE_SIZE_STORAGE_KEY = 'coursefin_subtitle_size';
export const PLAYBACK_RATES = [0.75, 1.0, 1.25, 1.5, 1.75, 2.0] as const;

const SUBTITLE_FONT_SIZES = [
  { label: '小 (16px)', fontSize: '16px' },
  { label: '中 (20px)', fontSize: '20px' },
  { label: '大 (24px)', fontSize: '24px' },
] as const;

const TICKS_PER_SECOND = 10_000_000;
const TICKS_PER_MS = 10_000;
const PROGRESS_SYNC_INTERVAL_MS = 10_000;
const DEFAULT_ACCENT_COLOR = '#1E88E5';

function readSavedPlaybackRate(): number {
  try {
    const raw = localStorage.getItem(PLAYBACK_RATE_STORAGE_KEY);
    if (!raw) return 1.0;
    const parsed = Number(raw);
    if (Number.isFinite(parsed) && parsed >= 0.5 && parsed <= 4.0) {
      return parsed;
    }
  } catch {
    // ignore storage errors
  }
  return 1.0;
}

function savePlaybackRate(rate: number): void {
  try {
    localStorage.setItem(PLAYBACK_RATE_STORAGE_KEY, String(rate));
  } catch {
    // ignore storage errors
  }
}

function readSavedSubtitlePref(): string | null {
  try {
    return localStorage.getItem(SUBTITLE_PREF_STORAGE_KEY);
  } catch {
    return null;
  }
}

function saveSubtitlePref(pref: string): void {
  try {
    localStorage.setItem(SUBTITLE_PREF_STORAGE_KEY, pref);
  } catch {
    // ignore storage errors
  }
}

function readSavedSubtitleSize(): string {
  try {
    const raw = localStorage.getItem(SUBTITLE_SIZE_STORAGE_KEY);
    if (raw && SUBTITLE_FONT_SIZES.some((s) => s.fontSize === raw)) {
      return raw;
    }
  } catch {
    // ignore storage errors
  }
  return '20px';
}

function saveSubtitleSize(size: string): void {
  try {
    localStorage.setItem(SUBTITLE_SIZE_STORAGE_KEY, size);
  } catch {
    // ignore storage errors
  }
}

function readThemeAccent(): string {
  try {
    const val = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();
    if (val) return val;
  } catch {
    // ignore
  }
  return DEFAULT_ACCENT_COLOR;
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function formatSubtitleLabel(sub: PlaybackSubtitleItem): string {
  if (sub.title && sub.title.trim()) return sub.title.trim();
  if (sub.language && sub.language.trim()) return sub.language.trim();
  return `字幕 ${sub.index}`;
}

function isChineseSubtitle(sub: PlaybackSubtitleItem): boolean {
  const lang = (sub.language ?? '').trim().toLowerCase();
  if (
    lang === 'zh' ||
    lang === 'zho' ||
    lang === 'chi' ||
    lang === 'chs' ||
    lang === 'cht' ||
    lang.startsWith('zh-') ||
    lang.startsWith('zh_')
  ) {
    return true;
  }
  const title = sub.title ?? '';
  return /中文|简体|繁體|繁体|双语|雙語/i.test(title);
}

function subtitlePrefValue(sub: PlaybackSubtitleItem): string {
  const lang = (sub.language ?? '').trim().toLowerCase();
  if (lang && lang !== 'und') {
    return lang;
  }
  return 'on';
}

export function pickInitialSubtitle(
  subtitles: PlaybackSubtitleItem[],
  savedPref: string | null,
): PlaybackSubtitleItem | null {
  const textSubs = subtitles.filter((sub) => sub.is_text && Boolean(sub.url));
  if (textSubs.length === 0) return null;

  const pref = (savedPref ?? '').trim().toLowerCase();
  if (pref === 'off') {
    return null;
  }

  if (pref && pref !== 'on') {
    const exactLang = textSubs.find((sub) => (sub.language ?? '').trim().toLowerCase() === pref);
    if (exactLang) return exactLang;

    const prefBase = pref.split(/[-_]/)[0];
    const baseLang = textSubs.find((sub) => {
      const lang = (sub.language ?? '').trim().toLowerCase();
      return Boolean(lang) && lang !== 'und' && lang.split(/[-_]/)[0] === prefBase;
    });
    if (baseLang) return baseLang;

    const byLabel = textSubs.find((sub) => formatSubtitleLabel(sub).toLowerCase() === pref);
    if (byLabel) return byLabel;
  }

  const defaultTrack = textSubs.find((sub) => sub.is_default);
  if (defaultTrack) return defaultTrack;

  const chineseTrack = textSubs.find((sub) => isChineseSubtitle(sub));
  if (chineseTrack) return chineseTrack;

  return textSubs[0];
}

function buildSubtitleStyle(fontSize: string): Partial<CSSStyleDeclaration> {
  return {
    fontSize,
    color: '#ffffff',
    textShadow: '0 1px 4px rgba(0, 0, 0, 0.85)',
    backgroundColor: 'rgba(0, 0, 0, 0.55)',
    padding: '3px 10px',
    borderRadius: '6px',
    marginBottom: '12px',
  };
}

export interface VideoPlayerProps {
  playback: LessonPlaybackInfo;
  lessonTitle?: string;
  onProgressSaved?: (progress: ProgressResponse) => void;
  onEnded?: () => void;
  onTimeProgress?: (lessonId: string, currentSec: number, durationSec: number) => void;
  onRequestTranscodeFallback?: () => void;
}

export default function VideoPlayer({
  playback,
  lessonTitle,
  onProgressSaved,
  onEnded,
  onTimeProgress,
  onRequestTranscodeFallback,
}: VideoPlayerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const artMountRef = useRef<HTMLDivElement>(null);
  const artRef = useRef<Artplayer | null>(null);
  const videoRef = useRef<HTMLVideoElement | null>(null);

  const resumeAppliedRef = useRef(false);
  const latestTimeRef = useRef<number>(0);
  const endedRef = useRef<boolean>(false);
  const hasPlayedRef = useRef<boolean>(false);
  const fallbackTriggeredRef = useRef<boolean>(false);
  const lastFlushedTicksRef = useRef<number>(-1);
  const lastTimeEmitRef = useRef<{ time: number; pct: number } | null>(null);

  const onProgressSavedRef = useRef(onProgressSaved);
  onProgressSavedRef.current = onProgressSaved;
  const onEndedRef = useRef(onEnded);
  onEndedRef.current = onEnded;
  const onTimeProgressRef = useRef(onTimeProgress);
  onTimeProgressRef.current = onTimeProgress;
  const onRequestTranscodeFallbackRef = useRef(onRequestTranscodeFallback);
  onRequestTranscodeFallbackRef.current = onRequestTranscodeFallback;

  const textSubtitles = useMemo(
    () => (playback.subtitles ?? []).filter((sub) => sub.is_text && Boolean(sub.url)),
    [playback.subtitles],
  );

  const [selectedSubtitleIndex, setSelectedSubtitleIndex] = useState<number | null>(() => {
    const initial = pickInitialSubtitle(textSubtitles, readSavedSubtitlePref());
    return initial ? initial.index : null;
  });
  const [isPlaying, setIsPlaying] = useState(false);
  const [playerError, setPlayerError] = useState<string | null>(null);
  const [hlsUnsupportedBrowser, setHlsUnsupportedBrowser] = useState(false);

  const resolvedStreamUrl = useMemo(() => {
    if (!playback.stream_url) return '';
    return withApiKey(playback.stream_url);
  }, [playback.stream_url]);

  const reportTicks = useCallback(
    async (ticks: number) => {
      const safeTicks = Math.max(0, Math.floor(ticks));
      lastFlushedTicksRef.current = safeTicks;
      try {
        const res = await api.reportProgress(playback.lesson_id, safeTicks);
        onProgressSavedRef.current?.(res);
      } catch {
        // Best-effort background sync; avoid interrupting playback
      }
    },
    [playback.lesson_id],
  );

  // Flush latest playback progress on unmount or lesson/session switch
  useEffect(() => {
    const lessonId = playback.lesson_id;
    latestTimeRef.current = 0;
    endedRef.current = false;
    lastTimeEmitRef.current = null;
    lastFlushedTicksRef.current =
      playback.progress && !playback.progress.played && playback.progress.position_ticks > 0
        ? playback.progress.position_ticks
        : -1;

    return () => {
      if (endedRef.current || latestTimeRef.current <= 0) {
        return;
      }
      const ticks = Math.floor(latestTimeRef.current * TICKS_PER_SECOND);
      if (ticks <= 0 || ticks === lastFlushedTicksRef.current) {
        return;
      }
      lastFlushedTicksRef.current = ticks;
      void api.reportProgress(lessonId, ticks, { keepalive: true }).catch(() => {});
    };
    // Only flush when switching lessons/sessions or unmounting.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [playback.lesson_id, playback.play_session_id]);

  // Mount and manage Artplayer + hls.js lifecycle
  useEffect(() => {
    if (playback.mode === 'unsupported' || !resolvedStreamUrl) {
      return;
    }
    const mountEl = artMountRef.current;
    if (!mountEl) return;

    resumeAppliedRef.current = false;
    hasPlayedRef.current = false;
    fallbackTriggeredRef.current = false;
    setIsPlaying(false);
    setPlayerError(null);
    setHlsUnsupportedBrowser(false);

    Artplayer.SEEK_STEP = 10;
    Artplayer.PLAYBACK_RATE = [...PLAYBACK_RATES];
    Artplayer.RECONNECT_TIME_MAX = 0;
    Artplayer.LOG_VERSION = false;

    const savedRate = readSavedPlaybackRate();
    const savedSize = readSavedSubtitleSize();
    const initialSub = pickInitialSubtitle(textSubtitles, readSavedSubtitlePref());
    setSelectedSubtitleIndex(initialSub ? initialSub.index : null);

    let hlsInstance: Hls | null = null;
    let usedNativeHls = false;
    const progressSnapshot = playback.progress;
    const durationMsSnapshot = playback.duration_ms;

    const buildSubtitleSelectors = (activeIdx: number | null) => [
      {
        default: activeIdx === null,
        html: '关闭',
        value: 'off',
      },
      ...textSubtitles.map((sub) => ({
        default: activeIdx === sub.index,
        html: escapeHtml(formatSubtitleLabel(sub)),
        value: String(sub.index),
      })),
    ];

    const applySubtitleSelection = (
      art: Artplayer,
      rawValue: string | number | undefined,
      source: 'control' | 'setting',
    ): string => {
      const value = rawValue === undefined ? 'off' : String(rawValue);
      const targetIndex = value === 'off' ? null : Number(value);
      const sub =
        targetIndex !== null && !Number.isNaN(targetIndex)
          ? textSubtitles.find((item) => item.index === targetIndex) ?? null
          : null;

      let controlLabel = '字幕';
      let settingLabel = '关闭';
      let nextIdx: number | null = null;

      if (!sub) {
        art.subtitle.show = false;
        saveSubtitlePref('off');
        setSelectedSubtitleIndex(null);
      } else {
        const label = formatSubtitleLabel(sub);
        const escapedLabel = escapeHtml(label);
        void art.subtitle.switch(withApiKey(sub.url), {
          name: label,
          type: 'vtt',
          encoding: 'utf-8',
          escape: true,
        });
        art.subtitle.show = true;
        saveSubtitlePref(subtitlePrefValue(sub));
        setSelectedSubtitleIndex(sub.index);
        controlLabel = escapedLabel;
        settingLabel = escapedLabel;
        nextIdx = sub.index;
      }

      // Sync the sibling menu with freshly cloned selector items so control bar & settings stay consistent.
      if (source === 'control') {
        art.setting?.update?.({
          name: 'coursefin-subtitle-setting',
          html: '字幕轨道',
          tooltip: settingLabel,
          selector: buildSubtitleSelectors(nextIdx),
          onSelect(item) {
            return applySubtitleSelection(this, item.value, 'setting');
          },
        });
      } else {
        art.controls?.update?.({
          name: 'coursefin-subtitle',
          position: 'right',
          index: 15,
          html: controlLabel,
          tooltip: '切换字幕',
          selector: buildSubtitleSelectors(nextIdx),
          onSelect(item) {
            return applySubtitleSelection(this, item.value, 'control');
          },
        });
      }

      return source === 'setting' ? settingLabel : controlLabel;
    };

    const controls: NonNullable<ArtOption['controls']> = [];
    const settings: Setting[] = [];

    if (textSubtitles.length > 0) {
      const initialIdx = initialSub ? initialSub.index : null;
      controls.push({
        name: 'coursefin-subtitle',
        position: 'right',
        index: 15,
        html: initialSub ? escapeHtml(formatSubtitleLabel(initialSub)) : '字幕',
        tooltip: '切换字幕',
        selector: buildSubtitleSelectors(initialIdx),
        onSelect(item) {
          return applySubtitleSelection(this, item.value, 'control');
        },
      });

      settings.push({
        name: 'coursefin-subtitle-setting',
        html: '字幕轨道',
        tooltip: initialSub ? escapeHtml(formatSubtitleLabel(initialSub)) : '关闭',
        selector: buildSubtitleSelectors(initialIdx),
        onSelect(item) {
          return applySubtitleSelection(this, item.value, 'setting');
        },
      });

      const currentSizeMeta =
        SUBTITLE_FONT_SIZES.find((s) => s.fontSize === savedSize) ?? SUBTITLE_FONT_SIZES[1];

      settings.push({
        html: '字幕字号',
        tooltip: currentSizeMeta.label,
        selector: SUBTITLE_FONT_SIZES.map((item) => ({
          default: item.fontSize === savedSize,
          html: item.label,
          fontSize: item.fontSize,
        })),
        onSelect(item) {
          const nextSize = String(item.fontSize ?? '20px');
          this.subtitle.style({ fontSize: nextSize });
          saveSubtitleSize(nextSize);
          return String(item.html ?? currentSizeMeta.label);
        },
      });
    }

    const artOption: ArtOption = {
      container: mountEl,
      url: resolvedStreamUrl,
      type: playback.mode === 'hls' ? 'm3u8' : '',
      theme: readThemeAccent(),
      lang: 'zh-cn',
      playbackRate: true,
      aspectRatio: true,
      setting: true,
      pip: true,
      fullscreen: true,
      fullscreenWeb: true,
      miniProgressBar: true,
      screenshot: true,
      subtitleOffset: true,
      hotkey: true,
      fastForward: true,
      mutex: true,
      playsInline: true,
      autoPlayback: false,
      controls,
      settings,
      subtitle: {
        url: initialSub ? withApiKey(initialSub.url) : '',
        name: initialSub ? formatSubtitleLabel(initialSub) : '',
        type: 'vtt',
        encoding: 'utf-8',
        escape: true,
        style: buildSubtitleStyle(savedSize),
      },
      customType: {
        m3u8(video: HTMLVideoElement, url: string) {
          if (Hls.isSupported()) {
            if (hlsInstance) {
              hlsInstance.destroy();
            }
            const resumeSec =
              progressSnapshot && !progressSnapshot.played && progressSnapshot.position_ticks > 0
                ? progressSnapshot.position_ticks / TICKS_PER_SECOND
                : -1;
            const hls = new Hls(resumeSec > 0 ? { startPosition: resumeSec } : undefined);
            hlsInstance = hls;
            if (resumeSec > 0) {
              resumeAppliedRef.current = true;
            }
            hls.loadSource(url);
            hls.attachMedia(video);
            hls.on(Hls.Events.ERROR, (_event, data) => {
              if (data?.fatal) {
                setPlayerError('HLS 流加载失败，请重试或使用原生 Jellyfin 客户端播放。');
              }
            });
          } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
            usedNativeHls = true;
            video.src = url;
          } else {
            setHlsUnsupportedBrowser(true);
          }
        },
      },
    };

    const art = new Artplayer(artOption);
    artRef.current = art;
    art.subtitle.show = Boolean(initialSub);

    const video = art.video;
    videoRef.current = video;
    video.playbackRate = savedRate;

    const emitTimeProgress = (force: boolean) => {
      const currentSec = video.currentTime;
      if (!Number.isFinite(currentSec) || currentSec < 0) return;
      latestTimeRef.current = currentSec;

      const durationSec =
        Number.isFinite(video.duration) && video.duration > 0
          ? video.duration
          : durationMsSnapshot > 0
            ? durationMsSnapshot / 1000
            : 0;
      if (durationSec <= 0 || !onTimeProgressRef.current) return;

      const pct = Math.min(1, Math.max(0, currentSec / durationSec));
      const prev = lastTimeEmitRef.current;
      if (
        force ||
        !prev ||
        Math.abs(pct - prev.pct) >= 0.01 ||
        Math.abs(currentSec - prev.time) >= 1
      ) {
        lastTimeEmitRef.current = { time: currentSec, pct };
        onTimeProgressRef.current(playback.lesson_id, currentSec, durationSec);
      }
    };

    const handleLoadedMetadata = () => {
      video.playbackRate = readSavedPlaybackRate();
      if (
        !resumeAppliedRef.current &&
        progressSnapshot &&
        progressSnapshot.position_ticks > 0 &&
        !progressSnapshot.played
      ) {
        resumeAppliedRef.current = true;
        const resumeSec = progressSnapshot.position_ticks / TICKS_PER_SECOND;
        if (Number.isFinite(video.duration) && video.duration > 0) {
          video.currentTime = Math.min(resumeSec, Math.max(0, video.duration - 1));
        } else {
          video.currentTime = resumeSec;
        }
      }
    };

    const handleTimeUpdate = () => emitTimeProgress(false);
    const handleSeeked = () => emitTimeProgress(true);

    const handlePlay = () => {
      setIsPlaying(true);
      setPlayerError(null);
    };

    const handlePlaying = () => {
      hasPlayedRef.current = true;
      setIsPlaying(true);
      setPlayerError(null);
    };

    const handlePause = () => {
      setIsPlaying(false);
      if (video.ended) return;
      latestTimeRef.current = video.currentTime;
      void reportTicks(Math.floor(video.currentTime * TICKS_PER_SECOND));
    };

    const handleEnded = () => {
      endedRef.current = true;
      setIsPlaying(false);
      let endTicks = 0;
      if (Number.isFinite(video.duration) && video.duration > 0) {
        endTicks = Math.floor(video.duration * TICKS_PER_SECOND);
      } else if (durationMsSnapshot > 0) {
        endTicks = Math.floor(durationMsSnapshot * TICKS_PER_MS);
      } else {
        endTicks = Math.floor(video.currentTime * TICKS_PER_SECOND);
      }
      void reportTicks(endTicks).then(() => {
        onEndedRef.current?.();
      });
    };

    const handleRateChange = () => {
      const nextRate = video.playbackRate;
      if (Number.isFinite(nextRate) && nextRate >= 0.5 && nextRate <= 4.0) {
        savePlaybackRate(nextRate);
      }
    };

    const handleError = () => {
      if (
        playback.mode === 'direct' &&
        !hasPlayedRef.current &&
        !fallbackTriggeredRef.current &&
        onRequestTranscodeFallbackRef.current
      ) {
        fallbackTriggeredRef.current = true;
        onRequestTranscodeFallbackRef.current();
        return;
      }
      setPlayerError('视频播放出错，请刷新重试或使用 Infuse / VidHub / SenPlayer 观看。');
    };

    video.addEventListener('loadedmetadata', handleLoadedMetadata);
    video.addEventListener('timeupdate', handleTimeUpdate);
    video.addEventListener('seeked', handleSeeked);
    video.addEventListener('play', handlePlay);
    video.addEventListener('playing', handlePlaying);
    video.addEventListener('pause', handlePause);
    video.addEventListener('ended', handleEnded);
    video.addEventListener('ratechange', handleRateChange);
    video.addEventListener('error', handleError);

    let observer: MutationObserver | null = null;
    if (typeof MutationObserver !== 'undefined') {
      observer = new MutationObserver(() => {
        if (artRef.current && !artRef.current.isDestroy) {
          artRef.current.theme = readThemeAccent();
        }
      });
      observer.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ['data-theme', 'class'],
      });
    }

    return () => {
      observer?.disconnect();
      video.removeEventListener('loadedmetadata', handleLoadedMetadata);
      video.removeEventListener('timeupdate', handleTimeUpdate);
      video.removeEventListener('seeked', handleSeeked);
      video.removeEventListener('play', handlePlay);
      video.removeEventListener('playing', handlePlaying);
      video.removeEventListener('pause', handlePause);
      video.removeEventListener('ended', handleEnded);
      video.removeEventListener('ratechange', handleRateChange);
      video.removeEventListener('error', handleError);

      if (hlsInstance) {
        hlsInstance.destroy();
        hlsInstance = null;
      }
      if (usedNativeHls) {
        video.removeAttribute('src');
        video.load();
      }
      art.destroy(false);
      artRef.current = null;
      videoRef.current = null;

      if (playback.mode === 'hls' && playback.play_session_id) {
        void api.stopActiveEncoding(playback.play_session_id).catch(() => {});
      }
    };
    // Re-initialize Artplayer when lesson, session, stream URL, mode, or subtitle tracks change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    playback.lesson_id,
    playback.play_session_id,
    resolvedStreamUrl,
    playback.mode,
    textSubtitles,
    reportTicks,
  ]);

  // Periodic 10s progress sync while playing
  useEffect(() => {
    if (!isPlaying || playback.mode === 'unsupported' || hlsUnsupportedBrowser) {
      return;
    }
    const timer = window.setInterval(() => {
      const video = videoRef.current;
      if (!video || video.paused || video.ended) return;
      latestTimeRef.current = video.currentTime;
      void reportTicks(Math.floor(video.currentTime * TICKS_PER_SECOND));
    }, PROGRESS_SYNC_INTERVAL_MS);

    return () => window.clearInterval(timer);
  }, [isPlaying, playback.mode, hlsUnsupportedBrowser, reportTicks]);

  // Global keyboard shortcuts: Space (play/pause), ArrowLeft/ArrowRight (±10s), F (fullscreen)
  useEffect(() => {
    if (playback.mode === 'unsupported' || hlsUnsupportedBrowser) {
      return;
    }

    function handleKeyDown(e: KeyboardEvent) {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const target = e.target as HTMLElement | null;
      if (target) {
        const tag = target.tagName;
        if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable) {
          return;
        }
        if ((e.code === 'Space' || e.key === ' ') && (tag === 'BUTTON' || tag === 'A')) {
          return;
        }
      }

      const art = artRef.current;
      const video = videoRef.current;
      if (!video) return;

      if (e.key === 'f' || e.key === 'F') {
        e.preventDefault();
        if (art) {
          art.fullscreen = !art.fullscreen;
        } else if (document.fullscreenElement) {
          void document.exitFullscreen?.().catch(() => {});
        } else {
          void containerRef.current?.requestFullscreen?.().catch(() => {});
        }
        return;
      }

      // When Artplayer itself is focused, let Artplayer's built-in hotkey handler process Space/Arrows.
      if (art?.isFocus) {
        return;
      }

      if (e.code === 'Space' || e.key === ' ') {
        e.preventDefault();
        if (video.paused) {
          void video.play().catch(() => {});
        } else {
          video.pause();
        }
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault();
        video.currentTime = Math.max(0, video.currentTime - 10);
        latestTimeRef.current = video.currentTime;
      } else if (e.key === 'ArrowRight') {
        e.preventDefault();
        const maxTime =
          Number.isFinite(video.duration) && video.duration > 0 ? video.duration : Infinity;
        video.currentTime = Math.min(maxTime, video.currentTime + 10);
        latestTimeRef.current = video.currentTime;
      }
    }

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [playback.mode, hlsUnsupportedBrowser]);

  const activeSubtitle = useMemo(
    () => textSubtitles.find((sub) => sub.index === selectedSubtitleIndex) ?? null,
    [textSubtitles, selectedSubtitleIndex],
  );

  if (playback.mode === 'unsupported' || hlsUnsupportedBrowser) {
    return (
      <div
        ref={containerRef}
        className="relative aspect-video w-full overflow-hidden rounded-3xl border border-status-warning/30 bg-card p-6 sm:p-10 flex flex-col items-center justify-center text-center shadow-card"
      >
        <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-status-warning-soft text-status-warning-ink mb-4">
          <Tv className="h-7 w-7" />
        </div>
        <h3 className="text-lg sm:text-xl font-semibold text-ink-primary">
          当前浏览器不支持直接播放此视频编码
        </h3>
        <p className="mt-2 max-w-xl text-sm text-ink-secondary leading-relaxed">
          {lessonTitle ? `「${lessonTitle}」` : '该课时'}的视频流无法在网页端直接解码。推荐使用支持 Jellyfin
          协议的原生播放器连接本服务观看，以获得完整硬解与字幕体验：
        </p>

        <div className="mt-4 flex flex-wrap items-center justify-center gap-2.5">
          {['Infuse', 'VidHub', 'SenPlayer'].map((app) => (
            <span
              key={app}
              className="inline-flex items-center gap-1.5 rounded-xl border border-accent-ring/40 bg-accent-soft px-3.5 py-1.5 text-sm font-semibold text-accent-ink"
            >
              <MonitorPlay className="h-4 w-4 text-accent-ink" />
              {app}
            </span>
          ))}
        </div>

        <div className="mt-5 flex flex-wrap items-center justify-center gap-2 text-xs text-ink-muted">
          {playback.container && (
            <span className="rounded-lg border border-subtle bg-subtle px-2.5 py-1 text-ink-secondary">
              容器: <strong className="text-ink-primary uppercase">{playback.container}</strong>
            </span>
          )}
          {playback.video_codec && (
            <span className="rounded-lg border border-subtle bg-subtle px-2.5 py-1 text-ink-secondary">
              视频编码: <strong className="text-ink-primary uppercase">{playback.video_codec}</strong>
            </span>
          )}
          {playback.audio_codec && (
            <span className="rounded-lg border border-subtle bg-subtle px-2.5 py-1 text-ink-secondary">
              音频编码: <strong className="text-ink-primary uppercase">{playback.audio_codec}</strong>
            </span>
          )}
        </div>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="overflow-hidden rounded-3xl border border-subtle bg-card shadow-card"
    >
      <div className="relative aspect-video w-full bg-black">
        <div ref={artMountRef} className="h-full w-full" />

        {playerError && (
          <div className="absolute inset-x-4 bottom-16 z-20 rounded-2xl border border-status-danger/40 bg-card/95 px-4 py-2.5 text-xs text-status-danger-ink flex items-center gap-2 backdrop-blur shadow-float">
            <AlertTriangle className="h-4 w-4 text-status-danger shrink-0" />
            <span>{playerError}</span>
          </div>
        )}
      </div>

      {/* Compact Player Status Strip */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-subtle bg-card px-4 py-2.5 text-xs text-ink-secondary">
        <div className="flex flex-wrap items-center gap-2.5">
          {playback.transcoding && (
            <Tooltip content="服务端已按需转码为兼容 H.264 视频流，支持自由拖拽进度条">
              <span className="inline-flex items-center gap-1.5 rounded-full border border-accent-ring/40 bg-accent-soft px-2.5 py-0.5 text-[11px] font-medium text-accent-ink">
                <Cpu className="h-3.5 w-3.5" />
                兼容转码播放
              </span>
            </Tooltip>
          )}

          <span className="inline-flex items-center gap-1.5 text-ink-secondary">
            <Captions className="h-3.5 w-3.5 text-accent" />
            {textSubtitles.length === 0 ? (
              <span className="text-ink-muted">无可用字幕</span>
            ) : activeSubtitle ? (
              <span>
                字幕：<strong className="font-medium text-ink-primary">{formatSubtitleLabel(activeSubtitle)}</strong>
              </span>
            ) : (
              <span className="text-ink-muted">字幕：已关闭</span>
            )}
          </span>
        </div>

        <div className="hidden sm:flex items-center gap-2.5 text-[11px] text-ink-muted">
          <span className="inline-flex items-center gap-1">
            <kbd className="rounded-md border border-default bg-subtle px-1.5 py-0.5 font-mono text-[10px] font-medium text-ink-secondary">
              Space
            </kbd>
            <span>播放/暂停</span>
          </span>
          <span className="inline-flex items-center gap-1">
            <kbd className="rounded-md border border-default bg-subtle px-1.5 py-0.5 font-mono text-[10px] font-medium text-ink-secondary">
              ←/→
            </kbd>
            <span>±10s</span>
          </span>
          <span className="inline-flex items-center gap-1">
            <kbd className="rounded-md border border-default bg-subtle px-1.5 py-0.5 font-mono text-[10px] font-medium text-ink-secondary">
              F
            </kbd>
            <span>全屏</span>
          </span>
        </div>
      </div>
    </div>
  );
}
