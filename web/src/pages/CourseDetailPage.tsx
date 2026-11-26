import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import {
  ArrowLeft,
  Captions,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock,
  FileText,
  ListVideo,
  Loader2,
  Mic,
  PanelRightClose,
  PanelRightOpen,
  Pencil,
  SkipBack,
  SkipForward,
  Sparkles,
} from 'lucide-react';
import { api, itemPrimaryImageUrl } from '../api/client';
import LessonProgressIcon from '../components/LessonProgressIcon';
import LessonSummaryPanel, {
  BatchSummaryBanner,
  formatBatchResultSummary,
  shouldShowBatchBanner,
} from '../components/LessonSummaryPanel';
import { useToast } from '../components/Toast';
import Tooltip from '../components/Tooltip';
import VideoPlayer, { type VideoPlayerHandle } from '../components/VideoPlayer';
import { detectBrowserVideoCodecs } from '../utils/mediaCapabilities';
import type {
  CourseBatchInput,
  CourseBatchSummaryJob,
  CourseDetail,
  CoursePipelineCounts,
  LessonDetail,
  LessonPipelineStatus,
  LessonPlaybackInfo,
  PlaybackSubtitleItem,
  ProgressResponse,
} from '../types/coursefin';

function arePlaybackSubtitlesEqual(
  a: PlaybackSubtitleItem[],
  b: PlaybackSubtitleItem[],
): boolean {
  if (a === b) return true;
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i += 1) {
    const prev = a[i];
    const next = b[i];
    if (
      prev.index !== next.index ||
      prev.url !== next.url ||
      prev.title !== next.title ||
      prev.language !== next.language ||
      prev.is_default !== next.is_default ||
      prev.is_external !== next.is_external ||
      prev.is_text !== next.is_text ||
      prev.format !== next.format
    ) {
      return false;
    }
  }
  return true;
}

function hasAISubtitleTrack(subtitles: PlaybackSubtitleItem[]): boolean {
  return subtitles.some((sub) => sub.title === 'AI 转写字幕' || sub.title.includes('AI 转写'));
}

function formatLessonDuration(durationMs: number): string {
  if (!durationMs || durationMs <= 0) return '--:--';
  const totalSeconds = Math.floor(durationMs / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const mm = String(minutes).padStart(2, '0');
  const ss = String(seconds).padStart(2, '0');
  if (hours > 0) {
    return `${hours}:${mm}:${ss}`;
  }
  return `${mm}:${ss}`;
}

function formatTotalDuration(durationMs: number): string {
  if (!durationMs || durationMs <= 0) return '';
  const totalMinutes = Math.round(durationMs / 60000);
  if (totalMinutes < 60) return `${Math.max(1, totalMinutes)} 分钟`;
  const hours = Math.floor(totalMinutes / 60);
  const mins = totalMinutes % 60;
  return mins > 0 ? `${hours} 小时 ${mins} 分钟` : `${hours} 小时`;
}

function pickInitialLessonId(course: CourseDetail, requestedId: string | null): string | null {
  const allLessons: LessonDetail[] = [];
  for (const ch of course.chapters ?? []) {
    for (const lesson of ch.lessons ?? []) {
      allLessons.push(lesson);
    }
  }
  if (allLessons.length === 0) return null;

  if (requestedId && allLessons.some((l) => l.id === requestedId)) {
    return requestedId;
  }

  const resumable = allLessons.find((l) => l.position_ticks > 0 && !l.played);
  if (resumable) return resumable.id;

  const firstUnplayed = allLessons.find((l) => !l.played);
  if (firstUnplayed) return firstUnplayed.id;

  return allLessons[0].id;
}

function applyLessonProgressToCourse(
  course: CourseDetail,
  lessonId: string,
  progress: ProgressResponse,
): CourseDetail {
  let totalLessons = 0;
  let totalPlayed = 0;

  const updatedChapters = (course.chapters ?? []).map((ch) => {
    const updatedLessons = (ch.lessons ?? []).map((l) =>
      l.id === lessonId
        ? {
            ...l,
            position_ticks: progress.position_ticks,
            played: progress.played,
            play_count: progress.play_count,
            last_played_at: progress.last_played_at ?? l.last_played_at,
          }
        : l,
    );
    const lessonCount = updatedLessons.length;
    const playedCount = updatedLessons.filter((l) => l.played).length;
    totalLessons += lessonCount;
    totalPlayed += playedCount;

    return {
      ...ch,
      lesson_count: lessonCount,
      played_count: playedCount,
      unplayed_item_count: Math.max(0, lessonCount - playedCount),
      played_percentage: lessonCount > 0 ? (playedCount / lessonCount) * 100 : 0,
      played: lessonCount > 0 && playedCount === lessonCount,
      lessons: updatedLessons,
    };
  });

  return {
    ...course,
    lesson_count: totalLessons,
    played_count: totalPlayed,
    unplayed_item_count: Math.max(0, totalLessons - totalPlayed),
    played_percentage: totalLessons > 0 ? (totalPlayed / totalLessons) * 100 : 0,
    played: totalLessons > 0 && totalPlayed === totalLessons,
    chapters: updatedChapters,
  };
}

export default function CourseDetailPage() {
  const { courseId } = useParams<{ courseId: string }>();
  const { toast } = useToast();
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedLessonId = searchParams.get('lesson');

  const [course, setCourse] = useState<CourseDetail | null>(null);
  const [loadingCourse, setLoadingCourse] = useState(true);
  const [courseError, setCourseError] = useState<string | null>(null);
  const [coverFailed, setCoverFailed] = useState(false);

  const [activeLessonId, setActiveLessonId] = useState<string | null>(null);
  const [playback, setPlayback] = useState<LessonPlaybackInfo | null>(null);
  const [loadingPlayback, setLoadingPlayback] = useState(false);
  const [playbackError, setPlaybackError] = useState<string | null>(null);
  const [playbackRetryNonce, setPlaybackRetryNonce] = useState(0);
  const [forceTranscodeLessonId, setForceTranscodeLessonId] = useState<string | null>(null);
  const fallbackRequestedForRef = useRef<string | null>(null);
  const [activeLiveProgress, setActiveLiveProgress] = useState<{
    lessonId: string;
    ratio: number;
  } | null>(null);
  const [togglingPlayed, setTogglingPlayed] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const browserCodecs = useMemo(() => detectBrowserVideoCodecs(), []);

  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [rightTab, setRightTab] = useState<'syllabus' | 'summary'>('syllabus');
  const [summaryTabOpened, setSummaryTabOpened] = useState(false);
  const [collapsedChapters, setCollapsedChapters] = useState<Record<string, boolean>>({});

  const videoPlayerRef = useRef<VideoPlayerHandle>(null);
  const activeLessonIdRef = useRef<string | null>(null);
  activeLessonIdRef.current = activeLessonId;
  const activeCourseIdRef = useRef<string | undefined>(courseId);
  activeCourseIdRef.current = courseId;
  const forceTranscodeLessonIdRef = useRef<string | null>(null);
  forceTranscodeLessonIdRef.current = forceTranscodeLessonId;
  const playbackRef = useRef<LessonPlaybackInfo | null>(null);
  playbackRef.current = playback;

  const [activeCurrentTimeSec, setActiveCurrentTimeSec] = useState(0);
  const [summaryLessonIds, setSummaryLessonIds] = useState<string[]>([]);
  const [lessonStatuses, setLessonStatuses] = useState<LessonPipelineStatus[]>([]);
  const [pipelineCounts, setPipelineCounts] = useState<CoursePipelineCounts | null>(null);
  const [batchJob, setBatchJob] = useState<CourseBatchSummaryJob | null>(null);
  const [transcribeBatchJob, setTranscribeBatchJob] = useState<CourseBatchSummaryJob | null>(null);
  const [batchResultDismissed, setBatchResultDismissed] = useState(false);
  const [transcribeBatchResultDismissed, setTranscribeBatchResultDismissed] = useState(false);
  const [startingBatch, setStartingBatch] = useState(false);
  const [cancellingBatch, setCancellingBatch] = useState(false);
  const [startingTranscribeBatch, setStartingTranscribeBatch] = useState(false);
  const [cancellingTranscribeBatch, setCancellingTranscribeBatch] = useState(false);

  const batchJobRef = useRef<CourseBatchSummaryJob | null>(null);
  batchJobRef.current = batchJob;
  const transcribeBatchJobRef = useRef<CourseBatchSummaryJob | null>(null);
  transcribeBatchJobRef.current = transcribeBatchJob;
  const prevBatchRunningRef = useRef(false);
  const userCancelledBatchRef = useRef(false);
  const prevTranscribeBatchRunningRef = useRef(false);
  const userCancelledTranscribeBatchRef = useRef(false);
  const summaryStatusInitializedRef = useRef(false);
  const knownSummaryLessonIdsRef = useRef<Set<string>>(new Set());
  const knownAISubtitleUpdatedAtRef = useRef<Map<string, string>>(new Map());
  const subtitlesRefreshInFlightRef = useRef<string | null>(null);
  const latestKnownSubtitlesRef = useRef<{
    lessonId: string;
    subtitles: PlaybackSubtitleItem[];
  } | null>(null);
  const lastSubToastRef = useRef<{ lessonId: string; at: number } | null>(null);

  const summaryLessonSet = useMemo(() => new Set(summaryLessonIds), [summaryLessonIds]);
  const lessonStatusMap = useMemo(() => {
    const map = new Map<string, LessonPipelineStatus>();
    for (const item of lessonStatuses) {
      map.set(item.lesson_id, item);
    }
    return map;
  }, [lessonStatuses]);

  const refreshActiveLessonSubtitles = useCallback(
    async (lessonId: string, options?: { fromBatch?: boolean }) => {
      if (subtitlesRefreshInFlightRef.current === lessonId) {
        return;
      }
      subtitlesRefreshInFlightRef.current = lessonId;

      const forceTranscode = forceTranscodeLessonIdRef.current === lessonId;
      const prevSubs =
        latestKnownSubtitlesRef.current?.lessonId === lessonId
          ? latestKnownSubtitlesRef.current.subtitles
          : playbackRef.current?.lesson_id === lessonId
            ? playbackRef.current.subtitles
            : [];

      try {
        const latest = await api.getLessonPlayback(lessonId, {
          codecs: browserCodecs,
          forceTranscode,
        });
        if (activeLessonIdRef.current !== lessonId) return;

        const hadAiSub = hasAISubtitleTrack(prevSubs);
        const hasAiSub = hasAISubtitleTrack(latest.subtitles);
        const listChanged = !arePlaybackSubtitlesEqual(prevSubs, latest.subtitles);

        latestKnownSubtitlesRef.current = { lessonId, subtitles: latest.subtitles };

        if (listChanged) {
          setPlayback((prev) =>
            prev && prev.lesson_id === lessonId
              ? { ...prev, subtitles: latest.subtitles }
              : prev,
          );
        }

        const recentlyToasted =
          lastSubToastRef.current?.lessonId === lessonId &&
          Date.now() - lastSubToastRef.current.at < 5000;

        const shouldToast = options?.fromBatch
          ? hasAiSub && (!hadAiSub || listChanged) && !recentlyToasted
          : hasAiSub && !recentlyToasted;

        if (shouldToast) {
          lastSubToastRef.current = { lessonId, at: Date.now() };
          toast.success('已生成 AI 转写字幕，可在播放器字幕菜单开启');
        }
      } catch {
        // Degrade silently so existing summary remains usable
      } finally {
        if (subtitlesRefreshInFlightRef.current === lessonId) {
          subtitlesRefreshInFlightRef.current = null;
        }
      }
    },
    [browserCodecs, toast],
  );

  const fetchCourseSummariesStatus = useCallback(
    async (targetCourseId: string, options?: { checkBatchAdditions?: boolean }) => {
      try {
        const wasAnyBatchRunning = Boolean(
          batchJobRef.current?.running || transcribeBatchJobRef.current?.running,
        );
        const status = await api.getCourseSummariesStatus(targetCourseId);
        if (activeCourseIdRef.current !== targetCourseId) return null;

        const isInitialized = summaryStatusInitializedRef.current;
        summaryStatusInitializedRef.current = true;

        const nextIds = status.summary_lesson_ids ?? [];
        const nextLessons = status.lessons;
        const nextCounts = status.counts ?? null;
        const nextSummaryBatch = status.summary_batch_job ?? status.batch_job ?? null;
        const nextTranscribeBatch = status.transcribe_batch_job ?? null;
        const nowAnyBatchRunning = Boolean(
          nextSummaryBatch?.running || nextTranscribeBatch?.running,
        );
        const activeId = activeLessonIdRef.current;

        const hasLessonsList = Array.isArray(nextLessons);
        const nextAISubMap = new Map<string, string>();
        if (hasLessonsList) {
          for (const lp of nextLessons) {
            if (lp.ai_subtitle_updated_at) {
              nextAISubMap.set(lp.lesson_id, lp.ai_subtitle_updated_at);
            }
          }
        }

        const shouldCheckAdditions =
          isInitialized &&
          (Boolean(options?.checkBatchAdditions) || wasAnyBatchRunning || nowAnyBatchRunning);

        let shouldRefreshSubtitles = false;
        if (shouldCheckAdditions && activeId) {
          if (hasLessonsList) {
            const nextTS = nextAISubMap.get(activeId) ?? '';
            const prevTS = knownAISubtitleUpdatedAtRef.current.get(activeId) ?? '';
            shouldRefreshSubtitles = Boolean(nextTS) && nextTS !== prevTS;
          } else {
            shouldRefreshSubtitles =
              nextIds.includes(activeId) && !knownSummaryLessonIdsRef.current.has(activeId);
          }
        }

        if (hasLessonsList) {
          knownAISubtitleUpdatedAtRef.current = nextAISubMap;
        }
        knownSummaryLessonIdsRef.current = new Set(nextIds);
        setSummaryLessonIds(nextIds);
        setLessonStatuses(nextLessons ?? []);
        setPipelineCounts(nextCounts);
        setBatchJob(nextSummaryBatch);
        setTranscribeBatchJob(nextTranscribeBatch);

        if (shouldRefreshSubtitles && activeId) {
          void refreshActiveLessonSubtitles(activeId, { fromBatch: true });
        }
        return status;
      } catch {
        return null;
      }
    },
    [refreshActiveLessonSubtitles],
  );

  const loadCourse = useCallback(async () => {
    if (!courseId) return;
    setLoadingCourse(true);
    setCourseError(null);
    setCoverFailed(false);
    setActiveLessonId(null);
    setPlayback(null);
    setForceTranscodeLessonId(null);
    fallbackRequestedForRef.current = null;
    setActiveLiveProgress(null);
    setActiveCurrentTimeSec(0);
    setSummaryLessonIds([]);
    setLessonStatuses([]);
    setPipelineCounts(null);
    setBatchJob(null);
    setTranscribeBatchJob(null);
    setBatchResultDismissed(false);
    setTranscribeBatchResultDismissed(false);
    prevBatchRunningRef.current = false;
    userCancelledBatchRef.current = false;
    prevTranscribeBatchRunningRef.current = false;
    userCancelledTranscribeBatchRef.current = false;
    setRightTab('syllabus');
    setSummaryTabOpened(false);
    summaryStatusInitializedRef.current = false;
    knownSummaryLessonIdsRef.current = new Set();
    knownAISubtitleUpdatedAtRef.current = new Map();
    latestKnownSubtitlesRef.current = null;
    try {
      const data = await api.getCourse(courseId);
      setCourse(data);
      const initialId = pickInitialLessonId(data, requestedLessonId);
      setActiveLessonId(initialId);
      void fetchCourseSummariesStatus(courseId);
    } catch (err) {
      setCourseError(err instanceof Error ? err.message : '加载课程详情失败');
    } finally {
      setLoadingCourse(false);
    }
  }, [courseId, fetchCourseSummariesStatus]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    void loadCourse();
  }, [loadCourse]);

  // Reset live playback timestamp whenever switching lessons
  useEffect(() => {
    setActiveCurrentTimeSec(0);
    latestKnownSubtitlesRef.current = null;
  }, [activeLessonId]);

  // Detect summary batch running transitions to clear dismissed errors on start and surface completion toast
  useEffect(() => {
    const wasRunning = prevBatchRunningRef.current;
    const nowRunning = Boolean(batchJob?.running);
    prevBatchRunningRef.current = nowRunning;

    if (!wasRunning && nowRunning) {
      setBatchResultDismissed(false);
      return;
    }

    if (wasRunning && !nowRunning) {
      const wasUserCancelled = userCancelledBatchRef.current;
      userCancelledBatchRef.current = false;

      if (batchJob && !wasUserCancelled && batchJob.current_stage !== 'cancelled') {
        const msg = formatBatchResultSummary(batchJob);
        if (batchJob.failed > 0 || (batchJob.errors?.length ?? 0) > 0) {
          toast.error(msg);
        } else {
          toast.success(msg);
        }
      }
    }
  }, [batchJob, toast]);

  // Detect transcribe batch running transitions independently
  useEffect(() => {
    const wasRunning = prevTranscribeBatchRunningRef.current;
    const nowRunning = Boolean(transcribeBatchJob?.running);
    prevTranscribeBatchRunningRef.current = nowRunning;

    if (!wasRunning && nowRunning) {
      setTranscribeBatchResultDismissed(false);
      return;
    }

    if (wasRunning && !nowRunning) {
      const wasUserCancelled = userCancelledTranscribeBatchRef.current;
      userCancelledTranscribeBatchRef.current = false;

      if (
        transcribeBatchJob &&
        !wasUserCancelled &&
        transcribeBatchJob.current_stage !== 'cancelled'
      ) {
        const msg = formatBatchResultSummary(transcribeBatchJob);
        if (transcribeBatchJob.failed > 0 || (transcribeBatchJob.errors?.length ?? 0) > 0) {
          toast.error(msg);
        } else {
          toast.success(msg);
        }
      }
    }
  }, [transcribeBatchJob, toast]);

  // Visibility-aware 1.5s polling while either course batch job is running
  const isAnyBatchRunning = Boolean(batchJob?.running || transcribeBatchJob?.running);
  useEffect(() => {
    if (!courseId || !isAnyBatchRunning) {
      return;
    }

    const poll = () => {
      if (typeof document !== 'undefined' && document.visibilityState === 'hidden') {
        return;
      }
      void fetchCourseSummariesStatus(courseId);
    };

    const intervalId = window.setInterval(poll, 1500);
    const handleVisibilityChange = () => {
      if (typeof document !== 'undefined' && document.visibilityState !== 'hidden') {
        poll();
      }
    };

    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', handleVisibilityChange);
    }

    return () => {
      window.clearInterval(intervalId);
      if (typeof document !== 'undefined') {
        document.removeEventListener('visibilitychange', handleVisibilityChange);
      }
    };
  }, [courseId, isAnyBatchRunning, fetchCourseSummariesStatus]);

  // Sync URL ?lesson= changes after course is loaded
  useEffect(() => {
    if (!course || !requestedLessonId) return;
    const exists = (course.chapters ?? []).some((ch) =>
      (ch.lessons ?? []).some((l) => l.id === requestedLessonId),
    );
    if (exists) {
      setActiveLessonId((prev) => (prev === requestedLessonId ? prev : requestedLessonId));
    }
  }, [course, requestedLessonId]);

  const flatLessons = useMemo(() => {
    if (!course) return [];
    return (course.chapters ?? []).flatMap((ch) =>
      (ch.lessons ?? []).map((lesson) => ({
        lesson,
        chapterTitle: ch.title,
      })),
    );
  }, [course]);

  const activeEntry = useMemo(
    () => flatLessons.find((item) => item.lesson.id === activeLessonId) ?? null,
    [flatLessons, activeLessonId],
  );
  const activeIndex = useMemo(
    () => flatLessons.findIndex((item) => item.lesson.id === activeLessonId),
    [flatLessons, activeLessonId],
  );
  const prevLesson = activeIndex > 0 ? flatLessons[activeIndex - 1].lesson : null;
  const nextLesson =
    activeIndex >= 0 && activeIndex < flatLessons.length - 1
      ? flatLessons[activeIndex + 1].lesson
      : null;

  // Load playback info when activeLessonId changes
  useEffect(() => {
    if (!activeLessonId) {
      setPlayback(null);
      return;
    }

    let cancelled = false;
    setLoadingPlayback(true);
    setPlaybackError(null);

    const forceTranscode = forceTranscodeLessonId === activeLessonId;
    api
      .getLessonPlayback(activeLessonId, {
        codecs: browserCodecs,
        forceTranscode,
      })
      .then((info) => {
        if (!cancelled) {
          setPlayback(info);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setPlayback(null);
          setPlaybackError(err instanceof Error ? err.message : '获取播放信息失败');
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingPlayback(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [activeLessonId, playbackRetryNonce, forceTranscodeLessonId, browserCodecs]);

  const selectLesson = useCallback(
    (lessonId: string) => {
      setActiveLessonId(lessonId);
      if (fallbackRequestedForRef.current !== lessonId) {
        fallbackRequestedForRef.current = null;
      }
      setForceTranscodeLessonId((prev) => (prev === lessonId ? prev : null));
      setActiveLiveProgress(null);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set('lesson', lessonId);
          return next;
        },
        { replace: false },
      );
    },
    [setSearchParams],
  );

  const handleRequestTranscodeFallback = useCallback(() => {
    if (!activeLessonId) return;
    if (fallbackRequestedForRef.current === activeLessonId) return;
    fallbackRequestedForRef.current = activeLessonId;
    toast.info('浏览器无法直接解码该视频，已自动切换为兼容转码模式');
    setForceTranscodeLessonId(activeLessonId);
  }, [activeLessonId, toast]);

  const handleTimeProgress = useCallback(
    (lessonId: string, currentSec: number, durationSec: number) => {
      if (lessonId === activeLessonIdRef.current && Number.isFinite(currentSec) && currentSec >= 0) {
        setActiveCurrentTimeSec(currentSec);
      }
      if (durationSec <= 0) return;
      const ratio = Math.min(1, Math.max(0, currentSec / durationSec));
      setActiveLiveProgress({ lessonId, ratio });
    },
    [],
  );

  const handleSummaryUpdated = useCallback(
    (lessonId: string, sourceType?: string, isNewlyCompleted = false) => {
      knownSummaryLessonIdsRef.current.add(lessonId);
      setSummaryLessonIds((prev) => (prev.includes(lessonId) ? prev : [...prev, lessonId]));
      if (isNewlyCompleted && activeCourseIdRef.current) {
        void fetchCourseSummariesStatus(activeCourseIdRef.current);
      }
      if (isNewlyCompleted && sourceType === 'asr' && lessonId === activeLessonIdRef.current) {
        void refreshActiveLessonSubtitles(lessonId, { fromBatch: false });
      }
    },
    [fetchCourseSummariesStatus, refreshActiveLessonSubtitles],
  );

  const handleTranscriptUpdated = useCallback(
    (lessonId: string, isNewlyCompleted = false) => {
      if (activeCourseIdRef.current) {
        void fetchCourseSummariesStatus(activeCourseIdRef.current);
      }
      if (isNewlyCompleted && lessonId === activeLessonIdRef.current) {
        void refreshActiveLessonSubtitles(lessonId, { fromBatch: false });
      }
    },
    [fetchCourseSummariesStatus, refreshActiveLessonSubtitles],
  );

  const handleStartBatch = useCallback(
    async (options: 'missing_only' | 'all' | CourseBatchInput = 'missing_only') => {
      if (!courseId || startingBatch) return;
      const input: CourseBatchInput =
        typeof options === 'string' ? { mode: options } : options;
      setStartingBatch(true);
      userCancelledBatchRef.current = false;
      setBatchResultDismissed(false);
      try {
        const job = await api.generateCourseSummaries(courseId, input);
        if (activeCourseIdRef.current !== courseId) return;
        setBatchJob(job);
        if (!job.running) {
          void fetchCourseSummariesStatus(courseId, { checkBatchAdditions: job.completed > 0 });
          if ((job.skipped_no_transcript ?? 0) > 0 && job.completed === 0) {
            toast.info(
              `暂无可总结课时：仍有 ${job.skipped_no_transcript} 节未完成语音转文字，请先批量转写`,
            );
          } else if (job.total === 0 || job.skipped === job.total) {
            toast.info('当前范围全部课时均已生成 AI 总结');
          }
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : '启动批量 AI 总结失败';
        toast.error(msg);
      } finally {
        setStartingBatch(false);
      }
    },
    [courseId, startingBatch, fetchCourseSummariesStatus, toast],
  );

  const handleCancelBatch = useCallback(async () => {
    if (!courseId || cancellingBatch) return;
    setCancellingBatch(true);
    userCancelledBatchRef.current = true;
    try {
      const job = await api.cancelCourseSummaries(courseId);
      if (activeCourseIdRef.current !== courseId) return;
      setBatchJob(job);
      void fetchCourseSummariesStatus(courseId);
    } catch (err) {
      userCancelledBatchRef.current = false;
      const msg = err instanceof Error ? err.message : '取消批量任务失败';
      toast.error(msg);
    } finally {
      setCancellingBatch(false);
    }
  }, [courseId, cancellingBatch, fetchCourseSummariesStatus, toast]);

  const handleStartTranscribeBatch = useCallback(
    async (options: 'missing_only' | 'all' | CourseBatchInput = 'missing_only') => {
      if (!courseId || startingTranscribeBatch) return;
      const input: CourseBatchInput =
        typeof options === 'string' ? { mode: options } : options;
      setStartingTranscribeBatch(true);
      userCancelledTranscribeBatchRef.current = false;
      setTranscribeBatchResultDismissed(false);
      try {
        const job = await api.generateCourseTranscripts(courseId, input);
        if (activeCourseIdRef.current !== courseId) return;
        setTranscribeBatchJob(job);
        if (!job.running) {
          void fetchCourseSummariesStatus(courseId, { checkBatchAdditions: job.completed > 0 });
          if (job.total === 0 || job.skipped === job.total) {
            toast.info('当前范围全部课时均已有字幕或转写文稿');
          }
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : '启动批量语音转文字失败';
        toast.error(msg);
      } finally {
        setStartingTranscribeBatch(false);
      }
    },
    [courseId, startingTranscribeBatch, fetchCourseSummariesStatus, toast],
  );

  const handleCancelTranscribeBatch = useCallback(async () => {
    if (!courseId || cancellingTranscribeBatch) return;
    setCancellingTranscribeBatch(true);
    userCancelledTranscribeBatchRef.current = true;
    try {
      const job = await api.cancelCourseTranscripts(courseId);
      if (activeCourseIdRef.current !== courseId) return;
      setTranscribeBatchJob(job);
      void fetchCourseSummariesStatus(courseId);
    } catch (err) {
      userCancelledTranscribeBatchRef.current = false;
      const msg = err instanceof Error ? err.message : '取消批量转写任务失败';
      toast.error(msg);
    } finally {
      setCancellingTranscribeBatch(false);
    }
  }, [courseId, cancellingTranscribeBatch, fetchCourseSummariesStatus, toast]);

  const handleSeekFromSummary = useCallback((seconds: number) => {
    videoPlayerRef.current?.seekTo(seconds, true);
  }, []);

  const handleProgressSaved = useCallback((progress: ProgressResponse) => {
    setActiveLiveProgress((prev) => (prev?.lessonId === progress.lesson_id ? null : prev));
    setCourse((prev) => (prev ? applyLessonProgressToCourse(prev, progress.lesson_id, progress) : prev));
    setPlayback((prev) =>
      prev && prev.lesson_id === progress.lesson_id
        ? {
            ...prev,
            progress: {
              position_ticks: progress.position_ticks,
              played: progress.played,
              play_count: progress.play_count,
              last_played_at: progress.last_played_at,
            },
          }
        : prev,
    );
  }, []);

  const handleVideoEnded = useCallback(() => {
    if (nextLesson) {
      selectLesson(nextLesson.id);
    }
  }, [nextLesson, selectLesson]);

  async function handleTogglePlayed() {
    if (!activeEntry || togglingPlayed) return;
    const targetLesson = activeEntry.lesson;
    const nextPlayed = !targetLesson.played;
    setTogglingPlayed(true);
    setActionError(null);
    try {
      const updated = await api.setLessonPlayed(targetLesson.id, nextPlayed);
      handleProgressSaved(updated);
      toast.success(nextPlayed ? '已标记本节为已看' : '已标记本节为未看');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '更新观看状态失败';
      setActionError(msg);
      toast.error(msg);
    } finally {
      setTogglingPlayed(false);
    }
  }

  function toggleChapterCollapse(chapterId: string) {
    setCollapsedChapters((prev) => ({
      ...prev,
      [chapterId]: !prev[chapterId],
    }));
  }

  if (loadingCourse) {
    return (
      <div className="flex flex-col items-center justify-center py-24 text-ink-secondary gap-3">
        <Loader2 className="h-7 w-7 animate-spin text-accent" />
        <p className="text-sm">正在加载课程详情...</p>
      </div>
    );
  }

  if (courseError || !course) {
    return (
      <div className="rounded-3xl border border-status-danger/30 bg-status-danger-soft p-6 text-center max-w-lg mx-auto my-12 space-y-4 shadow-card">
        <p className="text-status-danger-ink font-medium">{courseError ?? '未找到课程'}</p>
        <div className="flex items-center justify-center gap-3">
          <Link
            to="/"
            className="inline-flex items-center gap-1.5 rounded-full border border-default bg-card px-4 py-2 text-sm font-medium text-ink-primary hover:bg-subtle transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            <span>返回课程库</span>
          </Link>
          <button
            type="button"
            onClick={() => void loadCourse()}
            className="inline-flex items-center rounded-full bg-status-danger px-4 py-2 text-sm font-medium text-ink-inverse hover:opacity-90 transition-opacity cursor-pointer"
          >
            重试
          </button>
        </div>
      </div>
    );
  }

  const coursePct = Math.max(0, Math.min(100, Math.round(course.played_percentage || 0)));
  const hasCover = Boolean(course.cover_path || course.first_lesson_id) && !coverFailed;
  const totalDurationText = formatTotalDuration(course.total_duration_ms);
  const allLessonsSummarized =
    flatLessons.length > 0 && flatLessons.every((item) => summaryLessonSet.has(item.lesson.id));
  const totalLessonsCount = pipelineCounts?.total_lessons ?? course.lesson_count;
  const transcriptReadyCount = pipelineCounts?.transcript_ready ?? 0;
  const partialCount = pipelineCounts?.partial_lessons ?? 0;
  const summarizedCount = pipelineCounts?.summarized_lessons ?? summaryLessonIds.length;
  const allTranscriptsReady =
    Boolean(pipelineCounts) &&
    totalLessonsCount > 0 &&
    (pipelineCounts?.missing_transcript ?? 0) === 0;
  const summarySyncKey = batchJob
    ? `${batchJob.running ? 1 : 0}:${batchJob.completed}:${batchJob.skipped}:${batchJob.failed}:${batchJob.current_lesson_id}:${batchJob.current_stage}`
    : '';
  const transcribeSyncKey = transcribeBatchJob
    ? `${transcribeBatchJob.running ? 1 : 0}:${transcribeBatchJob.completed}:${transcribeBatchJob.skipped}:${transcribeBatchJob.failed}:${transcribeBatchJob.current_lesson_id}:${transcribeBatchJob.current_stage}`
    : '';
  const batchSyncKey = `${summarySyncKey}|${transcribeSyncKey}`;

  return (
    <div className="space-y-5">
      {/* Compact Top Header Strip */}
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2.5 min-w-0">
          <Tooltip content="返回首页课程列表">
            <Link
              to="/"
              className="inline-flex items-center gap-1.5 rounded-full border border-subtle bg-card px-3.5 py-1.5 text-xs font-medium text-ink-secondary hover:border-default hover:text-ink-primary shadow-card transition-colors shrink-0"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              <span>返回课程库</span>
            </Link>
          </Tooltip>

          <h1 className="text-lg sm:text-xl font-bold text-ink-primary tracking-tight truncate max-w-full sm:max-w-md lg:max-w-xl">
            {course.title}
          </h1>

          {course.played && (
            <span className="inline-flex items-center gap-1 rounded-full bg-status-success-soft px-2.5 py-0.5 text-xs font-semibold text-status-success-ink shrink-0">
              <CheckCircle2 className="h-3.5 w-3.5" />
              <span>已完成</span>
            </span>
          )}

          <span className="inline-flex items-center rounded-full border border-subtle bg-subtle px-2.5 py-0.5 text-xs font-medium tabular-nums text-ink-secondary shrink-0">
            已学 {course.played_count}/{course.lesson_count} 节 · {coursePct}%
          </span>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <Tooltip content={sidebarOpen ? '收起右侧章节目录以扩大播放画面' : '展开右侧章节目录'}>
            <button
              type="button"
              onClick={() => setSidebarOpen((v) => !v)}
              className="inline-flex items-center gap-1.5 rounded-full border border-subtle bg-card px-3.5 py-1.5 text-xs font-medium text-ink-secondary hover:border-default hover:text-ink-primary shadow-card transition-colors cursor-pointer"
            >
              {sidebarOpen ? (
                <>
                  <PanelRightClose className="h-4 w-4" />
                  <span>收起目录</span>
                </>
              ) : (
                <>
                  <PanelRightOpen className="h-4 w-4" />
                  <span>展开目录</span>
                </>
              )}
            </button>
          </Tooltip>

          <Tooltip content="编辑课程信息、章节排序、封面或运行 AI 整理">
            <Link
              to={`/courses/${course.id}/edit`}
              className="inline-flex items-center gap-1.5 rounded-full border border-default bg-card px-3.5 py-1.5 text-xs font-semibold text-ink-primary hover:bg-subtle shadow-card transition-colors"
            >
              <Pencil className="h-3.5 w-3.5" />
              <span>编辑课程</span>
            </Link>
          </Tooltip>
        </div>
      </header>

      {/* Theater-First Player + Sticky Playlist Layout */}
      <div
        className={`grid gap-6 items-start ${
          sidebarOpen ? 'lg:grid-cols-[minmax(0,1fr)_320px] xl:grid-cols-[minmax(0,1fr)_360px]' : 'grid-cols-1'
        }`}
      >
        {/* Left Theater Column: (1) VideoPlayer, (2) Lesson Action Card, (3) Course Overview Card */}
        <div className="space-y-4 min-w-0">
          {flatLessons.length === 0 ? (
            <div className="aspect-video w-full rounded-3xl border border-dashed border-default bg-card shadow-card flex flex-col items-center justify-center text-center p-6">
              <ListVideo className="h-10 w-10 text-ink-muted mb-3" />
              <p className="text-base font-semibold text-ink-primary">该课程暂无课时</p>
              <p className="mt-1 text-xs text-ink-secondary">
                请前往课程编辑页面添加章节和视频文件。
              </p>
            </div>
          ) : loadingPlayback ? (
            <div className="aspect-video w-full rounded-3xl border border-subtle bg-card shadow-card flex flex-col items-center justify-center text-ink-secondary gap-3">
              <Loader2 className="h-7 w-7 animate-spin text-accent" />
              <p className="text-sm">正在准备播放地址...</p>
            </div>
          ) : playbackError ? (
            <div className="aspect-video w-full rounded-3xl border border-status-danger/30 bg-status-danger-soft flex flex-col items-center justify-center p-6 text-center gap-3 shadow-card">
              <p className="text-sm font-medium text-status-danger-ink">{playbackError}</p>
              {activeLessonId && (
                <button
                  type="button"
                  onClick={() => setPlaybackRetryNonce((n) => n + 1)}
                  className="rounded-full bg-status-danger px-4 py-2 text-xs font-semibold text-ink-inverse hover:opacity-90 transition-opacity cursor-pointer"
                >
                  重新获取
                </button>
              )}
            </div>
          ) : playback ? (
            <VideoPlayer
              ref={videoPlayerRef}
              playback={playback}
              lessonTitle={activeEntry?.lesson.title}
              onProgressSaved={handleProgressSaved}
              onEnded={handleVideoEnded}
              onTimeProgress={handleTimeProgress}
              onRequestTranscodeFallback={handleRequestTranscodeFallback}
            />
          ) : null}

          {/* Current Lesson Action Card */}
          {activeEntry && (
            <section className="rounded-3xl border border-subtle bg-card shadow-card p-5 space-y-3">
              <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                <div className="min-w-0 space-y-0.5">
                  <p className="text-xs font-semibold text-accent-ink">{activeEntry.chapterTitle}</p>
                  <h2 className="text-base sm:text-lg font-bold text-ink-primary line-clamp-2 break-words">
                    {activeEntry.lesson.title}
                  </h2>
                </div>

                <div className="flex flex-wrap items-center gap-2 shrink-0">
                  <Tooltip content={prevLesson ? `切换到上一节：${prevLesson.title}` : '已是第一节'}>
                    <button
                      type="button"
                      disabled={!prevLesson}
                      onClick={() => prevLesson && selectLesson(prevLesson.id)}
                      className="inline-flex items-center gap-1 rounded-full border border-default bg-subtle px-3.5 py-1.5 text-xs font-medium text-ink-primary hover:bg-muted disabled:opacity-40 disabled:pointer-events-none transition-colors cursor-pointer"
                    >
                      <SkipBack className="h-3.5 w-3.5" />
                      <span>上一节</span>
                    </button>
                  </Tooltip>

                  <Tooltip content={nextLesson ? `切换到下一节：${nextLesson.title}` : '已是最后一节'}>
                    <button
                      type="button"
                      disabled={!nextLesson}
                      onClick={() => nextLesson && selectLesson(nextLesson.id)}
                      className="inline-flex items-center gap-1 rounded-full border border-default bg-subtle px-3.5 py-1.5 text-xs font-medium text-ink-primary hover:bg-muted disabled:opacity-40 disabled:pointer-events-none transition-colors cursor-pointer"
                    >
                      <span>下一节</span>
                      <SkipForward className="h-3.5 w-3.5" />
                    </button>
                  </Tooltip>

                  <Tooltip content={activeEntry.lesson.played ? '将本节恢复为未学状态' : '手动将本节标记为已学完'}>
                    <button
                      type="button"
                      onClick={() => void handleTogglePlayed()}
                      disabled={togglingPlayed}
                      className={`inline-flex items-center gap-1.5 rounded-full px-4 py-1.5 text-xs font-semibold transition-all disabled:opacity-50 cursor-pointer ${
                        activeEntry.lesson.played
                          ? 'border border-status-success/40 bg-status-success-soft text-status-success-ink hover:opacity-90'
                          : 'bg-accent text-ink-inverse hover:bg-accent-hover shadow-accent'
                      }`}
                    >
                      <CheckCircle2 className="h-3.5 w-3.5" />
                      <span>{activeEntry.lesson.played ? '标记未看' : '标记已看'}</span>
                    </button>
                  </Tooltip>
                </div>
              </div>

              {actionError && (
                <p className="text-xs text-status-danger-ink">{actionError}</p>
              )}

              {activeEntry.lesson.overview && (
                <p className="text-sm text-ink-secondary leading-relaxed border-t border-subtle pt-3">
                  {activeEntry.lesson.overview}
                </p>
              )}
            </section>
          )}

          {/* Course Overview & Metadata Card (below video for theater-first flow) */}
          <section className="rounded-3xl border border-subtle bg-card shadow-card p-5">
            <div className="flex flex-col sm:flex-row gap-5">
              <div className="w-full sm:w-44 aspect-video sm:aspect-[16/10] shrink-0 overflow-hidden rounded-2xl bg-subtle border border-subtle">
                {hasCover ? (
                  <img
                    src={itemPrimaryImageUrl(course.id, course.updated_at)}
                    alt={course.title}
                    onError={() => setCoverFailed(true)}
                    className="h-full w-full object-cover"
                  />
                ) : (
                  <div className="h-full w-full bg-gradient-to-br from-blue-500 to-sky-600 flex items-center justify-center">
                    <span className="text-3xl font-bold text-white/95">
                      {course.title.trim().slice(0, 1).toUpperCase() || '课'}
                    </span>
                  </div>
                )}
              </div>

              <div className="flex-1 min-w-0 flex flex-col justify-between gap-3">
                <div className="space-y-2">
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-ink-secondary">
                    {course.instructor && <span>讲师：{course.instructor}</span>}
                    <span>
                      共 {course.chapter_count} 章 · {course.lesson_count} 节
                    </span>
                    {totalDurationText && (
                      <span className="inline-flex items-center gap-1">
                        <Clock className="h-3.5 w-3.5 text-ink-muted" />
                        {totalDurationText}
                      </span>
                    )}
                  </div>

                  {course.overview && (
                    <p className="text-sm text-ink-secondary leading-relaxed">
                      {course.overview}
                    </p>
                  )}

                  {course.tags && course.tags.length > 0 && (
                    <div className="flex flex-wrap gap-1.5 pt-0.5">
                      {course.tags.map((tag) => (
                        <span
                          key={tag}
                          className="rounded-full border border-subtle bg-subtle px-2.5 py-0.5 text-[11px] font-medium text-ink-secondary"
                        >
                          {tag}
                        </span>
                      ))}
                    </div>
                  )}
                </div>

                <div className="space-y-1.5 max-w-md pt-1">
                  <div className="flex items-center justify-between text-xs text-ink-secondary">
                    <span>
                      已学 {course.played_count} / {course.lesson_count} 节
                    </span>
                    <span className="font-semibold tabular-nums text-ink-primary">{coursePct}%</span>
                  </div>
                  <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                    <div
                      className={`h-full rounded-full transition-all ${
                        course.played ? 'bg-status-success' : 'bg-accent'
                      }`}
                      style={{ width: `${coursePct}%` }}
                    />
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>

        {/* Right Sticky Chapter & Lesson Playlist / AI Summary Sidebar */}
        <aside
          aria-label={rightTab === 'syllabus' ? '课程目录' : 'AI 总结'}
          hidden={!sidebarOpen}
          className={
            sidebarOpen
              ? 'rounded-3xl bg-card border border-subtle shadow-card overflow-hidden lg:sticky lg:top-6'
              : 'hidden'
          }
        >
            <div
              role="tablist"
              aria-label="课程右侧栏"
              className="grid grid-cols-2 border-b border-subtle bg-subtle/40 p-1.5 gap-1"
            >
              <button
                type="button"
                role="tab"
                aria-selected={rightTab === 'syllabus'}
                onClick={() => setRightTab('syllabus')}
                className={`inline-flex items-center justify-center gap-1.5 rounded-2xl px-3 py-2 text-xs font-semibold transition-colors cursor-pointer ${
                  rightTab === 'syllabus'
                    ? 'bg-card text-ink-primary shadow-card'
                    : 'text-ink-secondary hover:text-ink-primary'
                }`}
              >
                <ListVideo className="h-3.5 w-3.5 text-accent" />
                <span>课程目录</span>
              </button>

              <button
                type="button"
                role="tab"
                aria-selected={rightTab === 'summary'}
                onClick={() => {
                  setRightTab('summary');
                  setSummaryTabOpened(true);
                }}
                className={`inline-flex items-center justify-center gap-1.5 rounded-2xl px-3 py-2 text-xs font-semibold transition-colors cursor-pointer ${
                  rightTab === 'summary'
                    ? 'bg-card text-ink-primary shadow-card'
                    : 'text-ink-secondary hover:text-ink-primary'
                }`}
              >
                <Sparkles className="h-3.5 w-3.5 text-accent" />
                <span>AI 总结</span>
              </button>
            </div>

            <div className={rightTab === 'syllabus' ? undefined : 'hidden'} hidden={rightTab !== 'syllabus'}>
              <div className="border-b border-subtle px-4 py-2.5 space-y-2">
                <div className="flex items-center justify-between gap-2">
                  {pipelineCounts ? (
                    <Tooltip
                      content={`字幕 ${pipelineCounts.subtitle_lessons} · AI 转写 ${pipelineCounts.transcribed_lessons}${
                        partialCount > 0 ? ` · 断点 ${partialCount}` : ''
                      } · 待转写 ${pipelineCounts.missing_transcript}`}
                    >
                      <span className="text-xs tabular-nums text-ink-muted truncate">
                        已学 {course.played_count}/{course.lesson_count} · 文稿{' '}
                        {transcriptReadyCount}/{totalLessonsCount} · 总结{' '}
                        {summarizedCount}/{totalLessonsCount}
                      </span>
                    </Tooltip>
                  ) : (
                    <span className="text-xs tabular-nums text-ink-muted">
                      {course.played_count}/{course.lesson_count} 节
                      {summaryLessonIds.length > 0 && ` · 已总结 ${summaryLessonIds.length}`}
                    </span>
                  )}
                </div>

                <div className="flex items-center gap-2">
                  <Tooltip
                    content={
                      transcribeBatchJob?.running
                        ? '正在批量语音转文字'
                        : allTranscriptsReady
                          ? '全部课时已有字幕或转写文稿'
                          : partialCount > 0
                            ? `将从未完成及断点课时继续转写（${partialCount} 节有断点）`
                            : '为尚无字幕和转写文稿的课时批量语音转文字'
                    }
                  >
                    <button
                      type="button"
                      onClick={() => void handleStartTranscribeBatch({ mode: 'missing_only' })}
                      disabled={
                        flatLessons.length === 0 ||
                        startingTranscribeBatch ||
                        Boolean(transcribeBatchJob?.running) ||
                        allTranscriptsReady
                      }
                      className="inline-flex flex-1 items-center justify-center gap-1 rounded-full border border-default bg-card px-2.5 py-1 text-[11px] font-medium text-ink-primary hover:bg-subtle disabled:opacity-50 transition-colors cursor-pointer"
                    >
                      <Mic className="h-3 w-3 text-accent shrink-0" />
                      <span>
                        {transcribeBatchJob?.running
                          ? '转写中'
                          : allTranscriptsReady
                            ? '文稿已齐'
                            : partialCount > 0 || transcriptReadyCount > 0
                              ? '继续转写'
                              : '批量转写'}
                      </span>
                    </button>
                  </Tooltip>

                  <Tooltip
                    content={
                      batchJob?.running
                        ? '正在批量生成 AI 总结'
                        : allLessonsSummarized
                          ? '全部课时已有总结，点击可重新批量生成'
                          : pipelineCounts &&
                              pipelineCounts.ready_to_summarize === 0 &&
                              pipelineCounts.missing_transcript > 0
                            ? `尚有 ${pipelineCounts.missing_transcript} 节待转写，请先批量转写`
                            : '为已有字幕或已转写的课时批量生成 AI 总结'
                    }
                  >
                    <button
                      type="button"
                      onClick={() =>
                        void handleStartBatch({
                          mode: allLessonsSummarized ? 'all' : 'missing_only',
                        })
                      }
                      disabled={
                        flatLessons.length === 0 || startingBatch || Boolean(batchJob?.running)
                      }
                      className="inline-flex flex-1 items-center justify-center gap-1 rounded-full border border-default bg-card px-2.5 py-1 text-[11px] font-medium text-ink-primary hover:bg-subtle disabled:opacity-50 transition-colors cursor-pointer"
                    >
                      <Sparkles className="h-3 w-3 text-accent shrink-0" />
                      <span>
                        {batchJob?.running
                          ? '总结中'
                          : allLessonsSummarized
                            ? '全部已总结'
                            : summaryLessonIds.length > 0
                              ? '继续总结'
                              : '批量总结'}
                      </span>
                    </button>
                  </Tooltip>
                </div>
              </div>

              {(shouldShowBatchBanner(transcribeBatchJob, transcribeBatchResultDismissed) ||
                shouldShowBatchBanner(batchJob, batchResultDismissed)) && (
                <div className="border-b border-subtle p-3 space-y-2">
                  {shouldShowBatchBanner(transcribeBatchJob, transcribeBatchResultDismissed) && (
                    <BatchSummaryBanner
                      batchJob={transcribeBatchJob}
                      cancelling={cancellingTranscribeBatch}
                      onCancel={handleCancelTranscribeBatch}
                      onDismiss={() => setTranscribeBatchResultDismissed(true)}
                    />
                  )}
                  {shouldShowBatchBanner(batchJob, batchResultDismissed) && (
                    <BatchSummaryBanner
                      batchJob={batchJob}
                      cancelling={cancellingBatch}
                      onCancel={handleCancelBatch}
                      onDismiss={() => setBatchResultDismissed(true)}
                    />
                  )}
                </div>
              )}

              <div className="max-h-[calc(100vh-11rem)] overflow-y-auto divide-y divide-subtle">
                {(course.chapters ?? []).map((chapter) => {
                  const isCollapsed = Boolean(collapsedChapters[chapter.id]);
                  const lessons = chapter.lessons ?? [];
                  const chapterStatuses = lessons
                    .map((l) => lessonStatusMap.get(l.id))
                    .filter((s): s is LessonPipelineStatus => Boolean(s));
                  const chapterMissingTranscript = chapterStatuses.filter(
                    (s) => !s.transcript_ready,
                  ).length;
                  const chapterAllTranscriptsReady =
                    lessonStatuses.length > 0 &&
                    lessons.length > 0 &&
                    chapterMissingTranscript === 0;
                  const chapterAllSummarized =
                    lessons.length > 0 &&
                    lessons.every((l) => summaryLessonSet.has(l.id));
                  const chapterReadyToSummarize = chapterStatuses.filter(
                    (s) => s.transcript_ready && !s.has_summary,
                  ).length;

                  return (
                    <div key={chapter.id}>
                      <div className="flex items-center justify-between gap-2 px-4 py-2.5 bg-subtle/70 hover:bg-subtle transition-colors">
                        <button
                          type="button"
                          onClick={() => toggleChapterCollapse(chapter.id)}
                          aria-expanded={!isCollapsed}
                          className="flex flex-1 items-center gap-2 min-w-0 text-left cursor-pointer"
                        >
                          {isCollapsed ? (
                            <ChevronRight className="h-4 w-4 text-ink-muted shrink-0" />
                          ) : (
                            <ChevronDown className="h-4 w-4 text-ink-muted shrink-0" />
                          )}
                          <span className="text-xs font-semibold text-ink-primary truncate">
                            {chapter.title}
                          </span>
                        </button>

                        <div className="flex items-center gap-1 shrink-0">
                          {lessons.length > 0 && (
                            <>
                              <Tooltip
                                content={
                                  chapterAllTranscriptsReady
                                    ? '本章文稿已齐'
                                    : chapterMissingTranscript > 0
                                      ? `批量转写本章（剩余 ${chapterMissingTranscript} 节）`
                                      : `批量转写章节：${chapter.title}`
                                }
                              >
                                <button
                                  type="button"
                                  aria-label={`转写本章：${chapter.title}`}
                                  disabled={
                                    startingTranscribeBatch ||
                                    Boolean(transcribeBatchJob?.running) ||
                                    chapterAllTranscriptsReady
                                  }
                                  onClick={() =>
                                    void handleStartTranscribeBatch({
                                      mode: 'missing_only',
                                      chapter_id: chapter.id,
                                    })
                                  }
                                  className="rounded-lg p-1 text-ink-secondary hover:bg-card hover:text-accent-ink disabled:opacity-40 disabled:pointer-events-none transition-colors cursor-pointer"
                                >
                                  <Mic className="h-3.5 w-3.5" aria-hidden="true" />
                                </button>
                              </Tooltip>

                              <Tooltip
                                content={
                                  chapterAllSummarized
                                    ? `重新总结本章：${chapter.title}`
                                    : chapterReadyToSummarize > 0
                                      ? `总结本章（可总结 ${chapterReadyToSummarize} 节）`
                                      : `总结本章：${chapter.title}`
                                }
                              >
                                <button
                                  type="button"
                                  aria-label={`总结本章：${chapter.title}`}
                                  disabled={startingBatch || Boolean(batchJob?.running)}
                                  onClick={() =>
                                    void handleStartBatch({
                                      mode: chapterAllSummarized ? 'all' : 'missing_only',
                                      chapter_id: chapter.id,
                                    })
                                  }
                                  className="rounded-lg p-1 text-ink-secondary hover:bg-card hover:text-accent-ink disabled:opacity-40 disabled:pointer-events-none transition-colors cursor-pointer"
                                >
                                  <Sparkles className="h-3.5 w-3.5" aria-hidden="true" />
                                </button>
                              </Tooltip>
                            </>
                          )}

                          <span className="text-[11px] tabular-nums text-ink-muted ml-1">
                            {chapter.played_count}/{lessons.length}
                          </span>
                        </div>
                      </div>

                      {!isCollapsed && (
                        <ul className="divide-y divide-subtle bg-card">
                          {lessons.map((lesson) => {
                            const isActive = lesson.id === activeLessonId;
                            const isSummarized = summaryLessonSet.has(lesson.id);
                            const lp = lessonStatusMap.get(lesson.id);
                            const baseRatio =
                              lesson.duration_ms > 0
                                ? lesson.position_ticks / (lesson.duration_ms * 10_000)
                                : 0;
                            const progressRatio =
                              isActive &&
                              !lesson.played &&
                              activeLiveProgress?.lessonId === lesson.id
                                ? activeLiveProgress.ratio
                                : baseRatio;
                            return (
                              <li key={lesson.id}>
                                <button
                                  type="button"
                                  onClick={() => selectLesson(lesson.id)}
                                  aria-current={isActive ? 'true' : undefined}
                                  className={`relative w-full flex items-start gap-2.5 px-4 py-2.5 text-left transition-colors cursor-pointer ${
                                    isActive
                                      ? 'bg-accent-soft text-ink-primary'
                                      : 'hover:bg-subtle/70 text-ink-secondary'
                                  }`}
                                >
                                  {isActive && (
                                    <span
                                      aria-hidden="true"
                                      className="absolute inset-y-1.5 left-0 w-1 rounded-r-full bg-accent"
                                    />
                                  )}
                                  <span className="mt-0.5 shrink-0">
                                    <LessonProgressIcon
                                      played={lesson.played}
                                      progressRatio={progressRatio}
                                      isActive={isActive}
                                    />
                                  </span>
                                  <span className="flex-1 min-w-0">
                                    <span
                                      className={`block text-xs leading-snug line-clamp-2 ${
                                        isActive
                                          ? 'font-bold text-ink-primary'
                                          : 'font-medium text-ink-secondary'
                                      }`}
                                    >
                                      {lesson.title}
                                    </span>
                                  </span>
                                  {(lp?.transcript_status === 'external_subtitle' ||
                                    lp?.transcript_status === 'embedded_subtitle') && (
                                    <Tooltip content="已有字幕">
                                      <span
                                        aria-label="已有字幕"
                                        className="mt-0.5 inline-flex items-center text-ink-muted shrink-0"
                                      >
                                        <Captions className="h-3.5 w-3.5" aria-hidden="true" />
                                      </span>
                                    </Tooltip>
                                  )}
                                  {(lp?.transcript_status === 'asr' ||
                                    lp?.transcript_status === 'manual') && (
                                    <Tooltip
                                      content={
                                        lp.transcript_status === 'asr'
                                          ? '已完成语音转写'
                                          : '已有手动文稿'
                                      }
                                    >
                                      <span
                                        aria-label="已完成转写"
                                        className="mt-0.5 inline-flex items-center text-accent-ink shrink-0"
                                      >
                                        {lp.transcript_status === 'asr' ? (
                                          <Mic className="h-3.5 w-3.5" aria-hidden="true" />
                                        ) : (
                                          <FileText className="h-3.5 w-3.5" aria-hidden="true" />
                                        )}
                                      </span>
                                    </Tooltip>
                                  )}
                                  {lp?.transcript_status === 'partial' && (
                                    <Tooltip
                                      content={`转写中断（已完成 ${lp.transcribed_chunks ?? 0}/${lp.total_chunks ?? 0} 分片），续跑将自动接着转`}
                                    >
                                      <span
                                        aria-label={`转写中断（已完成 ${lp.transcribed_chunks ?? 0}/${lp.total_chunks ?? 0} 分片）`}
                                        className="mt-0.5 inline-flex items-center text-status-warning-ink shrink-0"
                                      >
                                        <Clock className="h-3.5 w-3.5" aria-hidden="true" />
                                      </span>
                                    </Tooltip>
                                  )}
                                  {isSummarized && (
                                    <Tooltip content="已生成 AI 总结">
                                      <span
                                        aria-label="已生成 AI 总结"
                                        className="mt-0.5 inline-flex items-center text-accent-ink shrink-0"
                                      >
                                        <Sparkles className="h-3.5 w-3.5" aria-hidden="true" />
                                      </span>
                                    </Tooltip>
                                  )}
                                  <span className="shrink-0 text-[11px] tabular-nums text-ink-muted mt-0.5">
                                    {formatLessonDuration(lesson.duration_ms)}
                                  </span>
                                </button>
                              </li>
                            );
                          })}
                        </ul>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>

            {summaryTabOpened && (
              <div className={rightTab === 'summary' ? undefined : 'hidden'} hidden={rightTab !== 'summary'}>
                <LessonSummaryPanel
                  course={course}
                  activeEntry={activeEntry}
                  currentTimeSec={activeCurrentTimeSec}
                  onSeek={handleSeekFromSummary}
                  onSummaryUpdated={handleSummaryUpdated}
                  onTranscriptUpdated={handleTranscriptUpdated}
                  batchJob={batchJob}
                  transcribeBatchJob={transcribeBatchJob}
                  batchSyncKey={batchSyncKey}
                  batchResultDismissed={batchResultDismissed}
                  transcribeBatchResultDismissed={transcribeBatchResultDismissed}
                  onDismissBatch={() => setBatchResultDismissed(true)}
                  onDismissTranscribeBatch={() => setTranscribeBatchResultDismissed(true)}
                  onStartBatch={handleStartBatch}
                  onCancelBatch={handleCancelBatch}
                  onStartTranscribeBatch={handleStartTranscribeBatch}
                  onCancelTranscribeBatch={handleCancelTranscribeBatch}
                  startingBatch={startingBatch}
                  cancellingBatch={cancellingBatch}
                  startingTranscribeBatch={startingTranscribeBatch}
                  cancellingTranscribeBatch={cancellingTranscribeBatch}
                />
              </div>
            )}
          </aside>
      </div>
    </div>
  );
}
