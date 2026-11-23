import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import Hls from 'hls.js';
import {
  AlertTriangle,
  Captions,
  Gauge,
  Maximize2,
  MonitorPlay,
  Tv,
} from 'lucide-react';
import { api, withApiKey } from '../api/client';
import type {
  LessonPlaybackInfo,
  PlaybackSubtitleItem,
  ProgressResponse,
} from '../types/coursefin';

export const PLAYBACK_RATE_STORAGE_KEY = 'coursefin_playback_rate';
export const PLAYBACK_RATES = [0.75, 1.0, 1.25, 1.5, 1.75, 2.0] as const;

const TICKS_PER_SECOND = 10_000_000;
const TICKS_PER_MS = 10_000;
const PROGRESS_SYNC_INTERVAL_MS = 10_000;

function readSavedPlaybackRate(): number {
  try {
    const raw = localStorage.getItem(PLAYBACK_RATE_STORAGE_KEY);
    if (!raw) return 1.0;
    const parsed = Number(raw);
    if ((PLAYBACK_RATES as readonly number[]).includes(parsed)) {
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

function syncTextTracks(
  video: HTMLVideoElement | null,
  subtitles: PlaybackSubtitleItem[],
  selectedIndex: number | null,
): void {
  if (!video || !video.textTracks) return;
  for (let i = 0; i < video.textTracks.length; i++) {
    const track = video.textTracks[i];
    const sub = subtitles[i];
    if (!sub) continue;
    track.mode = selectedIndex !== null && sub.index === selectedIndex ? 'showing' : 'disabled';
  }
}

function formatSubtitleLabel(sub: PlaybackSubtitleItem): string {
  if (sub.title && sub.title.trim()) return sub.title.trim();
  if (sub.language && sub.language.trim()) return sub.language.trim();
  return `字幕 ${sub.index}`;
}

export interface VideoPlayerProps {
  playback: LessonPlaybackInfo;
  lessonTitle?: string;
  onProgressSaved?: (progress: ProgressResponse) => void;
  onEnded?: () => void;
}

export default function VideoPlayer({
  playback,
  lessonTitle,
  onProgressSaved,
  onEnded,
}: VideoPlayerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const resumeAppliedRef = useRef(false);

  const onProgressSavedRef = useRef(onProgressSaved);
  onProgressSavedRef.current = onProgressSaved;
  const onEndedRef = useRef(onEnded);
  onEndedRef.current = onEnded;

  const textSubtitles = useMemo(
    () => (playback.subtitles ?? []).filter((sub) => sub.is_text && Boolean(sub.url)),
    [playback.subtitles],
  );

  const [playbackRate, setPlaybackRate] = useState<number>(() => readSavedPlaybackRate());
  const [selectedSubtitleIndex, setSelectedSubtitleIndex] = useState<number | null>(() => {
    const def = textSubtitles.find((s) => s.is_default);
    return def ? def.index : null;
  });
  const [isPlaying, setIsPlaying] = useState(false);
  const [playerError, setPlayerError] = useState<string | null>(null);
  const [hlsUnsupportedBrowser, setHlsUnsupportedBrowser] = useState(false);

  // Reset per-lesson state when lesson or session changes
  useEffect(() => {
    resumeAppliedRef.current = false;
    setIsPlaying(false);
    setPlayerError(null);
    setHlsUnsupportedBrowser(false);
    const def = textSubtitles.find((s) => s.is_default);
    setSelectedSubtitleIndex(def ? def.index : null);
  }, [playback.lesson_id, playback.play_session_id, textSubtitles]);

  const resolvedStreamUrl = useMemo(() => {
    if (!playback.stream_url) return '';
    return withApiKey(playback.stream_url);
  }, [playback.stream_url]);

  // Attach HLS.js or Safari native HLS when mode === 'hls'
  useEffect(() => {
    if (playback.mode !== 'hls' || !resolvedStreamUrl) {
      return;
    }
    const video = videoRef.current;
    if (!video) return;

    let hls: Hls | null = null;
    let usedNativeHls = false;

    if (Hls.isSupported()) {
      hls = new Hls();
      hls.loadSource(resolvedStreamUrl);
      hls.attachMedia(video);
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data?.fatal) {
          setPlayerError('HLS 流加载失败，请重试或使用原生 Jellyfin 客户端播放。');
        }
      });
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      usedNativeHls = true;
      video.src = resolvedStreamUrl;
    } else {
      setHlsUnsupportedBrowser(true);
    }

    return () => {
      if (hls) {
        hls.destroy();
      }
      if (usedNativeHls) {
        video.removeAttribute('src');
        video.load();
      }
      if (playback.play_session_id) {
        void api.stopActiveEncoding(playback.play_session_id).catch(() => {});
      }
    };
  }, [playback.mode, playback.lesson_id, playback.play_session_id, resolvedStreamUrl]);

  // Keep video.playbackRate synced
  useEffect(() => {
    if (videoRef.current) {
      videoRef.current.playbackRate = playbackRate;
    }
  }, [playbackRate, playback.lesson_id, playback.play_session_id]);

  // Keep video.textTracks synced
  useEffect(() => {
    syncTextTracks(videoRef.current, textSubtitles, selectedSubtitleIndex);
  }, [textSubtitles, selectedSubtitleIndex, playback.lesson_id, playback.play_session_id]);

  const reportTicks = useCallback(
    async (ticks: number) => {
      const safeTicks = Math.max(0, Math.floor(ticks));
      try {
        const res = await api.reportProgress(playback.lesson_id, safeTicks);
        onProgressSavedRef.current?.(res);
      } catch {
        // Best-effort background sync; avoid interrupting playback
      }
    },
    [playback.lesson_id],
  );

  // Periodic 10s progress sync while playing
  useEffect(() => {
    if (!isPlaying || playback.mode === 'unsupported' || hlsUnsupportedBrowser) {
      return;
    }
    const timer = window.setInterval(() => {
      const video = videoRef.current;
      if (!video || video.paused || video.ended) return;
      void reportTicks(Math.floor(video.currentTime * TICKS_PER_SECOND));
    }, PROGRESS_SYNC_INTERVAL_MS);

    return () => window.clearInterval(timer);
  }, [isPlaying, playback.mode, hlsUnsupportedBrowser, reportTicks]);

  // Keyboard shortcuts: Space (play/pause), ArrowLeft/ArrowRight (±10s), F (fullscreen)
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

      const video = videoRef.current;
      if (!video) return;

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
      } else if (e.key === 'ArrowRight') {
        e.preventDefault();
        const maxTime = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : Infinity;
        video.currentTime = Math.min(maxTime, video.currentTime + 10);
      } else if (e.key === 'f' || e.key === 'F') {
        e.preventDefault();
        if (document.fullscreenElement) {
          void document.exitFullscreen?.().catch(() => {});
        } else {
          void containerRef.current?.requestFullscreen?.().catch(() => {});
        }
      }
    }

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [playback.mode, hlsUnsupportedBrowser]);

  function handleLoadedMetadata() {
    const video = videoRef.current;
    if (!video) return;

    video.playbackRate = playbackRate;
    syncTextTracks(video, textSubtitles, selectedSubtitleIndex);

    if (
      !resumeAppliedRef.current &&
      playback.progress &&
      playback.progress.position_ticks > 0 &&
      !playback.progress.played
    ) {
      resumeAppliedRef.current = true;
      const resumeSec = playback.progress.position_ticks / TICKS_PER_SECOND;
      if (Number.isFinite(video.duration) && video.duration > 0) {
        video.currentTime = Math.min(resumeSec, Math.max(0, video.duration - 1));
      } else {
        video.currentTime = resumeSec;
      }
    }
  }

  function handlePlay() {
    setIsPlaying(true);
    setPlayerError(null);
  }

  function handlePause() {
    setIsPlaying(false);
    const video = videoRef.current;
    if (!video || video.ended) return;
    void reportTicks(Math.floor(video.currentTime * TICKS_PER_SECOND));
  }

  async function handleEnded() {
    setIsPlaying(false);
    const video = videoRef.current;
    let endTicks = 0;
    if (video && Number.isFinite(video.duration) && video.duration > 0) {
      endTicks = Math.floor(video.duration * TICKS_PER_SECOND);
    } else if (playback.duration_ms > 0) {
      endTicks = Math.floor(playback.duration_ms * TICKS_PER_MS);
    } else if (video) {
      endTicks = Math.floor(video.currentTime * TICKS_PER_SECOND);
    }

    await reportTicks(endTicks);
    onEndedRef.current?.();
  }

  function handleRateChange(nextRate: number) {
    setPlaybackRate(nextRate);
    savePlaybackRate(nextRate);
    if (videoRef.current) {
      videoRef.current.playbackRate = nextRate;
    }
  }

  function handleSubtitleChange(value: string) {
    if (value === 'off') {
      setSelectedSubtitleIndex(null);
    } else {
      const idx = Number(value);
      setSelectedSubtitleIndex(Number.isNaN(idx) ? null : idx);
    }
  }

  function handleToggleFullscreen() {
    if (document.fullscreenElement) {
      void document.exitFullscreen?.().catch(() => {});
    } else {
      void containerRef.current?.requestFullscreen?.().catch(() => {});
    }
  }

  if (playback.mode === 'unsupported' || hlsUnsupportedBrowser) {
    return (
      <div
        ref={containerRef}
        className="relative aspect-video w-full overflow-hidden rounded-2xl border border-amber-500/30 bg-slate-900/95 p-6 sm:p-10 flex flex-col items-center justify-center text-center shadow-lg"
      >
        <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-500/15 text-amber-400 mb-4">
          <Tv className="h-7 w-7" />
        </div>
        <h3 className="text-lg sm:text-xl font-semibold text-white">
          当前浏览器不支持直接播放此视频编码
        </h3>
        <p className="mt-2 max-w-xl text-sm text-slate-300 leading-relaxed">
          {lessonTitle ? `「${lessonTitle}」` : '该课时'}的视频流无法在网页端直接解码。推荐使用支持 Jellyfin
          协议的原生播放器连接本服务观看，以获得完整硬解与字幕体验：
        </p>

        <div className="mt-4 flex flex-wrap items-center justify-center gap-2.5">
          {['Infuse', 'VidHub', 'SenPlayer'].map((app) => (
            <span
              key={app}
              className="inline-flex items-center gap-1.5 rounded-xl border border-indigo-500/30 bg-indigo-500/15 px-3.5 py-1.5 text-sm font-semibold text-indigo-200"
            >
              <MonitorPlay className="h-4 w-4 text-indigo-400" />
              {app}
            </span>
          ))}
        </div>

        <div className="mt-5 flex flex-wrap items-center justify-center gap-2 text-xs text-slate-400">
          {playback.container && (
            <span className="rounded-lg border border-slate-700/80 bg-slate-800/80 px-2.5 py-1">
              容器: <strong className="text-slate-200 uppercase">{playback.container}</strong>
            </span>
          )}
          {playback.video_codec && (
            <span className="rounded-lg border border-slate-700/80 bg-slate-800/80 px-2.5 py-1">
              视频编码: <strong className="text-slate-200 uppercase">{playback.video_codec}</strong>
            </span>
          )}
          {playback.audio_codec && (
            <span className="rounded-lg border border-slate-700/80 bg-slate-800/80 px-2.5 py-1">
              音频编码: <strong className="text-slate-200 uppercase">{playback.audio_codec}</strong>
            </span>
          )}
        </div>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-950 shadow-xl"
    >
      <div className="relative aspect-video w-full bg-black">
        <video
          key={`${playback.lesson_id}:${playback.play_session_id}`}
          ref={videoRef}
          src={playback.mode === 'direct' ? resolvedStreamUrl : undefined}
          controls
          playsInline
          preload="metadata"
          onLoadedMetadata={handleLoadedMetadata}
          onPlay={handlePlay}
          onPause={handlePause}
          onEnded={() => void handleEnded()}
          onError={() =>
            setPlayerError('视频播放出错，请刷新重试或使用 Infuse / VidHub / SenPlayer 观看。')
          }
          className="h-full w-full"
        >
          {textSubtitles.map((sub) => (
            <track
              key={sub.index}
              kind="subtitles"
              src={withApiKey(sub.url)}
              srcLang={sub.language || 'und'}
              label={formatSubtitleLabel(sub)}
              default={selectedSubtitleIndex === sub.index}
            />
          ))}
        </video>

        {playerError && (
          <div className="absolute inset-x-4 bottom-14 rounded-xl border border-rose-500/40 bg-slate-950/90 px-4 py-2.5 text-xs text-rose-200 flex items-center gap-2 backdrop-blur">
            <AlertTriangle className="h-4 w-4 text-rose-400 shrink-0" />
            <span>{playerError}</span>
          </div>
        )}
      </div>

      {/* Player Control Strip */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-800/90 bg-slate-900/90 px-4 py-2.5 text-xs text-slate-300">
        <div className="flex flex-wrap items-center gap-4">
          <label className="inline-flex items-center gap-2">
            <Gauge className="h-4 w-4 text-indigo-400" />
            <span className="text-slate-400">倍速</span>
            <select
              aria-label="播放倍速"
              value={String(playbackRate)}
              onChange={(e) => handleRateChange(Number(e.target.value))}
              className="rounded-lg border border-slate-700 bg-slate-950 px-2 py-1 text-xs font-medium text-slate-100 focus:border-indigo-500 focus:outline-none"
            >
              {PLAYBACK_RATES.map((rate) => (
                <option key={rate} value={String(rate)}>
                  {rate === 1 || rate === 2 ? rate.toFixed(1) : String(rate)}x
                </option>
              ))}
            </select>
          </label>

          <label className="inline-flex items-center gap-2">
            <Captions className="h-4 w-4 text-sky-400" />
            <span className="text-slate-400">字幕</span>
            <select
              aria-label="字幕选择"
              value={selectedSubtitleIndex === null ? 'off' : String(selectedSubtitleIndex)}
              onChange={(e) => handleSubtitleChange(e.target.value)}
              className="rounded-lg border border-slate-700 bg-slate-950 px-2 py-1 text-xs font-medium text-slate-100 focus:border-indigo-500 focus:outline-none max-w-[180px]"
            >
              <option value="off">关闭</option>
              {textSubtitles.map((sub) => (
                <option key={sub.index} value={String(sub.index)}>
                  {formatSubtitleLabel(sub)}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="flex items-center gap-3">
          <span className="hidden sm:inline text-[11px] text-slate-500">
            快捷键：空格 播放/暂停 · ←/→ 快退/快进 10 秒 · F 全屏
          </span>
          <button
            type="button"
            onClick={handleToggleFullscreen}
            aria-label="切换全屏"
            className="inline-flex items-center gap-1 rounded-lg border border-slate-700 bg-slate-800/80 px-2.5 py-1 text-xs text-slate-200 hover:bg-slate-800 hover:text-white transition-colors"
          >
            <Maximize2 className="h-3.5 w-3.5" />
            <span>全屏</span>
          </button>
        </div>
      </div>
    </div>
  );
}
