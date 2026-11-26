import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertCircle,
  Check,
  ChevronDown,
  ChevronUp,
  Clock,
  Copy,
  FileText,
  ListOrdered,
  Loader2,
  Mic,
  RefreshCw,
  RotateCcw,
  Settings,
  Sparkles,
  Square,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import { useToast } from './Toast';
import Tooltip from './Tooltip';
import { copyTextToClipboard } from '../utils/clipboard';
import { formatLessonSummaryMarkdown, formatTimestamp } from '../utils/summaryMarkdown';
import type {
  CourseBatchInput,
  CourseBatchSummaryJob,
  CourseDetail,
  GenerateLessonSummaryInput,
  LessonDetail,
  LessonSummary,
  LessonSummaryCapabilities,
  LessonSummaryJob,
  SummaryJobStage,
  TimelineSection,
  TranscribeLessonInput,
} from '../types/coursefin';

const POLL_INTERVAL_MS = 1500;
const COPY_FEEDBACK_MS = 2000;

export function formatSummaryJobStageText(
  stage: SummaryJobStage,
  chunkIndex = 0,
  chunkTotal = 0,
  message?: string,
): string {
  if (message && message.trim()) {
    return message.trim();
  }
  switch (stage) {
    case 'queued':
      return '排队等待中...';
    case 'reading_subtitle':
      return '正在读取字幕...';
    case 'extracting_audio':
      return '正在提取视频音频...';
    case 'transcribing':
      return chunkTotal > 0
        ? `正在语音转写（分片 ${chunkIndex}/${chunkTotal}）...`
        : '正在语音转写...';
    case 'summarizing':
      return chunkTotal > 0
        ? `正在生成 AI 总结（分段 ${chunkIndex}/${chunkTotal}）...`
        : '正在生成 AI 总结...';
    case 'completed':
      return 'AI 总结生成完成';
    case 'cancelled':
      return '任务已取消';
    case 'failed':
      return '生成失败';
    default:
      return '正在处理...';
  }
}

function formatSourceBadge(sourceType: string, sourceLabel: string): string {
  if (sourceLabel && sourceLabel.trim()) {
    return sourceLabel.trim();
  }
  switch (sourceType) {
    case 'external_subtitle':
      return '外挂字幕';
    case 'embedded_subtitle':
      return '内嵌字幕';
    case 'asr':
      return 'AI 语音转写';
    case 'manual':
      return '手动文稿';
    default:
      return 'AI 总结';
  }
}

function findActiveTimelineIndex(
  timeline: TimelineSection[],
  hasTimestamps: boolean,
  currentTimeSec: number,
): number {
  if (!hasTimestamps || !Number.isFinite(currentTimeSec) || currentTimeSec < 0) {
    return -1;
  }

  for (let i = 0; i < timeline.length; i += 1) {
    const sec = timeline[i];
    if (sec.start_sec === null || sec.start_sec === undefined) {
      continue;
    }
    const isLast = i === timeline.length - 1;
    if (sec.end_sec !== null && sec.end_sec !== undefined) {
      if (
        currentTimeSec >= sec.start_sec &&
        (currentTimeSec < sec.end_sec || (isLast && currentTimeSec <= sec.end_sec))
      ) {
        return i;
      }
    } else {
      const nextWithStart = timeline
        .slice(i + 1)
        .find((item) => item.start_sec !== null && item.start_sec !== undefined);
      const nextStart = nextWithStart?.start_sec ?? null;
      if (currentTimeSec >= sec.start_sec && (nextStart === null || currentTimeSec < nextStart)) {
        return i;
      }
    }
  }

  return -1;
}

export function formatBatchResultSummary(batchJob: CourseBatchSummaryJob): string {
  const prefix = batchJob.kind === 'transcribe' ? '批量转写' : '批量总结';
  const chapterSuffix = batchJob.chapter_title ? `（${batchJob.chapter_title}）` : '';
  const verb = batchJob.current_stage === 'cancelled' ? '已取消' : '完成';
  const skippedNoTranscript = batchJob.skipped_no_transcript ?? 0;
  let skipText = `跳过 ${batchJob.skipped}`;
  if (skippedNoTranscript > 0) {
    const skippedAlready =
      batchJob.skipped_already_done ?? Math.max(0, batchJob.skipped - skippedNoTranscript);
    skipText = `跳过 ${batchJob.skipped}（已总结 ${skippedAlready}，缺文稿 ${skippedNoTranscript}）`;
  }
  return `${prefix}${chapterSuffix}${verb}：成功 ${batchJob.completed}，${skipText}，失败 ${batchJob.failed}`;
}

export function shouldShowBatchBanner(
  batchJob: CourseBatchSummaryJob | null,
  dismissed = false,
): batchJob is CourseBatchSummaryJob {
  if (!batchJob) return false;
  if (batchJob.running) return true;
  if (dismissed) return false;
  return batchJob.failed > 0 || (batchJob.errors?.length ?? 0) > 0;
}

export interface BatchSummaryBannerProps {
  batchJob: CourseBatchSummaryJob;
  cancelling?: boolean;
  onCancel: () => void | Promise<void>;
  onDismiss?: () => void;
}

export function BatchSummaryBanner({
  batchJob,
  cancelling = false,
  onCancel,
  onDismiss,
}: BatchSummaryBannerProps) {
  const isTranscribe = batchJob.kind === 'transcribe';

  if (!batchJob.running) {
    const errors = batchJob.errors ?? [];
    const summaryHeading = formatBatchResultSummary(batchJob);

    return (
      <div
        role="status"
        className="rounded-2xl border border-status-danger/30 bg-status-danger-soft p-3 space-y-2"
      >
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-start gap-1.5 min-w-0 text-xs font-semibold text-status-danger-ink">
            <AlertCircle className="h-3.5 w-3.5 shrink-0 mt-0.5" />
            <span className="leading-snug break-words">{summaryHeading}</span>
          </div>

          {onDismiss && (
            <button
              type="button"
              onClick={onDismiss}
              aria-label={isTranscribe ? '关闭批量转写错误提示' : '关闭批量错误提示'}
              className="rounded-lg p-1 text-status-danger-ink hover:bg-card/60 transition-colors shrink-0 cursor-pointer"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>

        {errors.length > 0 && (
          <ul className="space-y-1.5 max-h-40 overflow-y-auto">
            {errors.map((item, idx) => (
              <li
                key={`${item.lesson_id}-${idx}`}
                className="rounded-xl border border-status-danger/20 bg-card/85 px-2.5 py-1.5 text-[11px] text-ink-secondary leading-snug break-words"
              >
                <span className="font-semibold text-status-danger-ink">
                  {item.lesson_title || item.lesson_id}
                </span>
                <span>：{item.error}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    );
  }

  const doneCount = batchJob.completed + batchJob.skipped + batchJob.failed;
  const pct =
    batchJob.total > 0 ? Math.min(100, Math.round((doneCount / batchJob.total) * 100)) : 0;
  const stageText = formatSummaryJobStageText(
    batchJob.current_stage,
    batchJob.chunk_index,
    batchJob.chunk_total,
  );
  const runningPrefix = isTranscribe ? '批量转写中' : '批量总结中';
  const runningHeading = batchJob.chapter_title
    ? `${runningPrefix} · ${batchJob.chapter_title} ${doneCount}/${batchJob.total}`
    : `${runningPrefix} ${doneCount}/${batchJob.total}`;

  return (
    <div className="rounded-2xl border border-accent-ring/40 bg-accent-soft p-3 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 space-y-0.5">
          <div className="flex items-center gap-1.5 text-xs font-semibold text-accent-ink">
            <Loader2 className="h-3.5 w-3.5 animate-spin shrink-0" />
            <span className="truncate">{runningHeading}</span>
          </div>
          <p className="text-[11px] text-ink-secondary leading-snug break-words">
            {stageText}
            {batchJob.current_lesson_title ? ` · 「${batchJob.current_lesson_title}」` : ''}
          </p>
        </div>

        <button
          type="button"
          onClick={() => void onCancel()}
          disabled={cancelling}
          className="inline-flex items-center gap-1 rounded-full border border-default bg-card px-2.5 py-1 text-[11px] font-medium text-ink-primary hover:bg-subtle disabled:opacity-50 transition-colors shrink-0 cursor-pointer"
        >
          <Square className="h-3 w-3" />
          <span>取消</span>
        </button>
      </div>

      <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
        <div
          className="h-full rounded-full bg-accent transition-all"
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

export interface LessonSummaryPanelProps {
  course: CourseDetail;
  activeEntry: { lesson: LessonDetail; chapterTitle: string } | null;
  currentTimeSec: number;
  onSeek: (seconds: number) => void;
  onSummaryUpdated: (lessonId: string, sourceType?: string, isNewlyCompleted?: boolean) => void;
  onTranscriptUpdated?: (lessonId: string, isNewlyCompleted?: boolean) => void;
  batchJob: CourseBatchSummaryJob | null;
  transcribeBatchJob?: CourseBatchSummaryJob | null;
  batchSyncKey?: string;
  batchResultDismissed?: boolean;
  transcribeBatchResultDismissed?: boolean;
  onDismissBatch?: () => void;
  onDismissTranscribeBatch?: () => void;
  onStartBatch: (options?: 'missing_only' | 'all' | CourseBatchInput) => void | Promise<void>;
  onCancelBatch: () => void | Promise<void>;
  onStartTranscribeBatch?: (options?: CourseBatchInput) => void | Promise<void>;
  onCancelTranscribeBatch?: () => void | Promise<void>;
  startingBatch?: boolean;
  cancellingBatch?: boolean;
  startingTranscribeBatch?: boolean;
  cancellingTranscribeBatch?: boolean;
}

export default function LessonSummaryPanel({
  course,
  activeEntry,
  currentTimeSec,
  onSeek,
  onSummaryUpdated,
  onTranscriptUpdated,
  batchJob,
  transcribeBatchJob = null,
  batchSyncKey,
  batchResultDismissed,
  transcribeBatchResultDismissed,
  onDismissBatch,
  onDismissTranscribeBatch,
  onStartBatch,
  onCancelBatch,
  onCancelTranscribeBatch,
  startingBatch = false,
  cancellingBatch = false,
  cancellingTranscribeBatch = false,
}: LessonSummaryPanelProps) {
  const { toast } = useToast();
  const lessonId = activeEntry?.lesson.id ?? null;

  const [summary, setSummary] = useState<LessonSummary | null>(null);
  const [job, setJob] = useState<LessonSummaryJob | null>(null);
  const [capabilities, setCapabilities] = useState<LessonSummaryCapabilities | null>(null);
  const [loading, setLoading] = useState<boolean>(Boolean(lessonId));
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [copied, setCopied] = useState(false);
  const [showManualInput, setShowManualInput] = useState(false);
  const [manualTranscript, setManualTranscript] = useState('');
  const [localBatchDismissed, setLocalBatchDismissed] = useState(false);
  const [localTranscribeDismissed, setLocalTranscribeDismissed] = useState(false);

  const scopedSummary = lessonId && summary?.lesson_id === lessonId ? summary : null;
  const scopedJob = lessonId && job?.lesson_id === lessonId ? job : null;

  useEffect(() => {
    setLocalBatchDismissed(false);
    setLocalTranscribeDismissed(false);
  }, [course.id]);

  useEffect(() => {
    if (batchJob?.running) {
      setLocalBatchDismissed(false);
    }
  }, [batchJob?.running]);

  useEffect(() => {
    if (transcribeBatchJob?.running) {
      setLocalTranscribeDismissed(false);
    }
  }, [transcribeBatchJob?.running]);

  const effectiveBatchDismissed = batchResultDismissed ?? localBatchDismissed;
  const handleDismissBatch = useCallback(() => {
    setLocalBatchDismissed(true);
    onDismissBatch?.();
  }, [onDismissBatch]);

  const effectiveTranscribeDismissed = transcribeBatchResultDismissed ?? localTranscribeDismissed;
  const handleDismissTranscribeBatch = useCallback(() => {
    setLocalTranscribeDismissed(true);
    onDismissTranscribeBatch?.();
  }, [onDismissTranscribeBatch]);

  const activeLessonIdRef = useRef<string | null>(lessonId);
  activeLessonIdRef.current = lessonId;
  const requestSeqRef = useRef(0);
  const awaitingCompletionForLessonRef = useRef<string | null>(null);
  const copyTimerRef = useRef<number | null>(null);
  const onSummaryUpdatedRef = useRef(onSummaryUpdated);
  onSummaryUpdatedRef.current = onSummaryUpdated;
  const onTranscriptUpdatedRef = useRef(onTranscriptUpdated);
  onTranscriptUpdatedRef.current = onTranscriptUpdated;

  useEffect(() => {
    return () => {
      if (copyTimerRef.current !== null) {
        window.clearTimeout(copyTimerRef.current);
        copyTimerRef.current = null;
      }
    };
  }, []);

  const fetchLessonSummaryState = useCallback(
    async (targetLessonId: string, seq: number, isInitial: boolean) => {
      try {
        const data = await api.getLessonSummary(targetLessonId);
        if (seq !== requestSeqRef.current || activeLessonIdRef.current !== targetLessonId) {
          return;
        }

        setSummary(data.summary);
        setJob(data.job);
        setCapabilities(data.capabilities);
        setFetchError(null);

        if (data.job?.running) {
          awaitingCompletionForLessonRef.current = targetLessonId;
        } else if (
          data.job &&
          data.job.stage === 'completed' &&
          awaitingCompletionForLessonRef.current === targetLessonId
        ) {
          awaitingCompletionForLessonRef.current = null;
          if (data.job.action === 'transcribe') {
            onTranscriptUpdatedRef.current?.(targetLessonId, true);
          } else {
            onSummaryUpdatedRef.current(
              targetLessonId,
              data.summary?.source_type ?? data.job.source_type,
              true,
            );
          }
        } else if (data.summary) {
          onSummaryUpdatedRef.current(targetLessonId, data.summary.source_type, false);
        }
      } catch (err) {
        if (seq !== requestSeqRef.current || activeLessonIdRef.current !== targetLessonId) {
          return;
        }
        if (isInitial) {
          setFetchError(err instanceof Error ? err.message : '获取 AI 总结状态失败');
        }
      } finally {
        if (seq === requestSeqRef.current && activeLessonIdRef.current === targetLessonId) {
          setLoading(false);
        }
      }
    },
    [],
  );

  // Reset state and load summary whenever active lesson changes
  useEffect(() => {
    requestSeqRef.current += 1;
    const seq = requestSeqRef.current;

    if (copyTimerRef.current !== null) {
      window.clearTimeout(copyTimerRef.current);
      copyTimerRef.current = null;
    }
    awaitingCompletionForLessonRef.current = null;
    setSummary(null);
    setJob(null);
    setCapabilities(null);
    setFetchError(null);
    setActionError(null);
    setSubmitting(false);
    setCancelling(false);
    setCopied(false);
    setShowManualInput(false);
    setManualTranscript('');

    if (!lessonId) {
      setLoading(false);
      return;
    }

    setLoading(true);
    void fetchLessonSummaryState(lessonId, seq, true);
  }, [lessonId, fetchLessonSummaryState]);

  // Refresh active lesson summary only when a course batch job actually progresses or finishes
  const prevBatchSyncKeyRef = useRef(batchSyncKey);
  useEffect(() => {
    if (prevBatchSyncKeyRef.current === batchSyncKey) {
      return;
    }
    prevBatchSyncKeyRef.current = batchSyncKey;
    if (!lessonId || !batchSyncKey) return;
    if (scopedJob?.running || submitting) return;
    const seq = requestSeqRef.current;
    void fetchLessonSummaryState(lessonId, seq, false);
  }, [batchSyncKey, lessonId, scopedJob?.running, submitting, fetchLessonSummaryState]);

  // Visibility-aware 1.5s polling while a single-lesson summary job is running
  const isJobRunning = Boolean(scopedJob?.running);
  useEffect(() => {
    if (!lessonId || !isJobRunning) {
      return;
    }

    const seq = requestSeqRef.current;
    const poll = () => {
      if (typeof document !== 'undefined' && document.visibilityState === 'hidden') {
        return;
      }
      if (seq !== requestSeqRef.current || activeLessonIdRef.current !== lessonId) {
        return;
      }
      void fetchLessonSummaryState(lessonId, seq, false);
    };

    const intervalId = window.setInterval(poll, POLL_INTERVAL_MS);

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
  }, [lessonId, isJobRunning, fetchLessonSummaryState]);

  const startLessonRequest = useCallback(
    async (
      starter: (targetLessonId: string) => Promise<LessonSummaryJob>,
      fallbackError: string,
    ) => {
      if (!lessonId || submitting) return;
      const targetLessonId = lessonId;
      const seq = requestSeqRef.current;

      setSubmitting(true);
      setActionError(null);
      awaitingCompletionForLessonRef.current = targetLessonId;

      try {
        const startedJob = await starter(targetLessonId);
        if (seq !== requestSeqRef.current || activeLessonIdRef.current !== targetLessonId) {
          return;
        }
        setJob(startedJob);

        if (!startedJob.running) {
          awaitingCompletionForLessonRef.current = null;
          await fetchLessonSummaryState(targetLessonId, seq, false);
        }
      } catch (err) {
        if (seq !== requestSeqRef.current || activeLessonIdRef.current !== targetLessonId) {
          return;
        }
        awaitingCompletionForLessonRef.current = null;
        const msg = err instanceof Error ? err.message : fallbackError;
        setActionError(msg);
        toast.error(msg);
      } finally {
        if (seq === requestSeqRef.current && activeLessonIdRef.current === targetLessonId) {
          setSubmitting(false);
        }
      }
    },
    [lessonId, submitting, fetchLessonSummaryState, toast],
  );

  const handleGenerate = useCallback(
    async (input: GenerateLessonSummaryInput = {}) => {
      await startLessonRequest(
        (targetLessonId) => api.generateLessonSummary(targetLessonId, input),
        '启动 AI 总结失败',
      );
    },
    [startLessonRequest],
  );

  const handleTranscribe = useCallback(
    async (input: TranscribeLessonInput = {}) => {
      await startLessonRequest(
        (targetLessonId) => api.generateLessonTranscript(targetLessonId, input),
        '启动语音转文字失败',
      );
    },
    [startLessonRequest],
  );

  const handleCancelLessonJob = useCallback(async () => {
    if (!lessonId || cancelling) return;
    const targetLessonId = lessonId;
    const seq = requestSeqRef.current;
    const isTranscribeOnly = job?.lesson_id === targetLessonId && job.action === 'transcribe';

    setCancelling(true);
    setActionError(null);

    try {
      const cancelledJob = isTranscribeOnly
        ? await api.cancelLessonTranscript(targetLessonId)
        : await api.cancelLessonSummary(targetLessonId);
      if (seq !== requestSeqRef.current || activeLessonIdRef.current !== targetLessonId) {
        return;
      }
      awaitingCompletionForLessonRef.current = null;
      setJob(cancelledJob);
      await fetchLessonSummaryState(targetLessonId, seq, false);
    } catch (err) {
      if (seq !== requestSeqRef.current || activeLessonIdRef.current !== targetLessonId) {
        return;
      }
      const msg = err instanceof Error ? err.message : '取消任务失败';
      setActionError(msg);
      toast.error(msg);
    } finally {
      if (seq === requestSeqRef.current && activeLessonIdRef.current === targetLessonId) {
        setCancelling(false);
      }
    }
  }, [lessonId, cancelling, job, fetchLessonSummaryState, toast]);

  const handleCopyMarkdown = useCallback(async () => {
    if (!scopedSummary || !activeEntry) return;
    const markdown = formatLessonSummaryMarkdown({
      courseTitle: course.title,
      chapterTitle: activeEntry.chapterTitle,
      lessonTitle: activeEntry.lesson.title,
      summary: scopedSummary,
    });

    const ok = await copyTextToClipboard(markdown);
    if (ok) {
      setCopied(true);
      if (copyTimerRef.current !== null) {
        window.clearTimeout(copyTimerRef.current);
      }
      copyTimerRef.current = window.setTimeout(() => {
        setCopied(false);
      }, COPY_FEEDBACK_MS);
      toast.success('已复制 Markdown 总结');
    } else {
      toast.error('复制失败，请检查浏览器剪贴板权限');
    }
  }, [scopedSummary, activeEntry, course.title, toast]);

  const activeTimelineIndex = useMemo(() => {
    if (!scopedSummary) return -1;
    return findActiveTimelineIndex(scopedSummary.timeline, scopedSummary.has_timestamps, currentTimeSec);
  }, [scopedSummary, currentTimeSec]);

  const transcribeBatchBanner = shouldShowBatchBanner(
    transcribeBatchJob,
    effectiveTranscribeDismissed,
  ) ? (
    <BatchSummaryBanner
      batchJob={transcribeBatchJob}
      cancelling={cancellingTranscribeBatch}
      onCancel={onCancelTranscribeBatch ?? (() => {})}
      onDismiss={handleDismissTranscribeBatch}
    />
  ) : null;

  const summaryBatchBanner = shouldShowBatchBanner(batchJob, effectiveBatchDismissed) ? (
    <BatchSummaryBanner
      batchJob={batchJob}
      cancelling={cancellingBatch}
      onCancel={onCancelBatch}
      onDismiss={handleDismissBatch}
    />
  ) : null;

  const batchBanners =
    transcribeBatchBanner || summaryBatchBanner ? (
      <div className="space-y-2">
        {transcribeBatchBanner}
        {summaryBatchBanner}
      </div>
    ) : null;

  if (!activeEntry || !lessonId) {
    return (
      <div className="p-5 space-y-4">
        {batchBanners}
        <div className="rounded-2xl border border-dashed border-default bg-subtle/50 p-6 text-center text-xs text-ink-secondary">
          请选择一个课时查看或生成 AI 总结
        </div>
      </div>
    );
  }

  if (loading && !scopedSummary && !scopedJob && !capabilities) {
    return (
      <div className="p-5 space-y-4">
        {batchBanners}
        <div className="flex flex-col items-center justify-center py-12 gap-2.5 text-ink-secondary">
          <Loader2 className="h-6 w-6 animate-spin text-accent" />
          <p className="text-xs">正在加载 AI 总结...</p>
        </div>
      </div>
    );
  }

  if (fetchError && !scopedSummary && !capabilities) {
    return (
      <div className="p-5 space-y-4">
        {batchBanners}
        <div className="rounded-2xl border border-status-danger/30 bg-status-danger-soft p-4 text-center space-y-3">
          <p className="text-xs font-medium text-status-danger-ink">{fetchError}</p>
          <button
            type="button"
            onClick={() => {
              setLoading(true);
              setFetchError(null);
              void fetchLessonSummaryState(lessonId, requestSeqRef.current, true);
            }}
            className="inline-flex items-center gap-1.5 rounded-full bg-status-danger px-3.5 py-1.5 text-xs font-semibold text-ink-inverse hover:opacity-90 transition-opacity cursor-pointer"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            <span>重试</span>
          </button>
        </div>
      </div>
    );
  }

  const aiConfigured = capabilities?.ai_configured ?? true;
  const asrConfigured = capabilities?.asr_configured ?? false;
  const hasSubtitle = capabilities?.has_subtitle ?? false;
  const hasCachedTranscript = capabilities?.has_cached_transcript ?? false;
  const partialChunks = capabilities?.partial_transcript_chunks ?? 0;
  const partialTotal = capabilities?.partial_transcript_total ?? 0;
  const hasPartialCheckpoint = !hasSubtitle && !hasCachedTranscript && partialChunks > 0 && partialTotal > 0;
  const hasFfmpeg = capabilities?.has_ffmpeg ?? true;
  const canRetranscribe =
    scopedSummary?.source_type === 'asr' || Boolean(capabilities?.has_cached_transcript);

  const partialCheckpointBanner = hasPartialCheckpoint ? (
    <p className="rounded-xl border border-status-warning/30 bg-status-warning-soft px-3 py-2 text-xs text-status-warning-ink">
      检测到上次转写断点（已完成 {partialChunks}/{partialTotal} 分片），继续转写将自动跳过已完成分片
    </p>
  ) : null;

  const showFailureBanner =
    !scopedJob?.running &&
    (Boolean(actionError) || scopedJob?.stage === 'failed' || scopedJob?.stage === 'cancelled');
  const failureMessage =
    actionError ||
    (scopedJob?.stage === 'failed'
      ? scopedJob.error ||
        scopedJob.message ||
        (scopedJob.action === 'transcribe' ? '语音转文字失败' : '生成 AI 总结失败')
      : '') ||
    (scopedJob?.stage === 'cancelled'
      ? scopedJob.action === 'transcribe'
        ? '转写已取消，已完成分片断点已保留'
        : '任务已取消，可随时重新生成'
      : '');

  return (
    <div className="max-h-[calc(100vh-8rem)] overflow-y-auto p-4 space-y-4">
      {batchBanners}

      {/* Single-lesson running stage card (kept above old summary if one exists) */}
      {scopedJob?.running && (
        <div className="rounded-2xl border border-accent-ring/40 bg-accent-soft p-3.5 space-y-2.5">
          <div className="flex items-start justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              <Loader2 className="h-4 w-4 animate-spin text-accent shrink-0" />
              <div className="min-w-0">
                <p className="text-xs font-semibold text-accent-ink break-words">
                  {formatSummaryJobStageText(
                    scopedJob.stage,
                    scopedJob.chunk_index,
                    scopedJob.chunk_total,
                    scopedJob.message,
                  )}
                </p>
                {scopedJob.stage === 'transcribing' && (
                  <p className="text-[11px] text-ink-secondary mt-0.5">
                    转写完成后将自动保存为「AI 转写字幕」轨道
                  </p>
                )}
              </div>
            </div>

            <button
              type="button"
              onClick={() => void handleCancelLessonJob()}
              disabled={cancelling}
              className="inline-flex items-center gap-1 rounded-full border border-default bg-card px-2.5 py-1 text-[11px] font-medium text-ink-primary hover:bg-subtle disabled:opacity-50 transition-colors shrink-0 cursor-pointer"
            >
              <Square className="h-3 w-3" />
              <span>取消</span>
            </button>
          </div>

          {scopedJob.chunk_total > 0 && (
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-accent transition-all"
                style={{
                  width: `${Math.min(100, Math.max(8, Math.round((scopedJob.chunk_index / scopedJob.chunk_total) * 100)))}%`,
                }}
              />
            </div>
          )}
        </div>
      )}

      {/* Non-destructive failure / cancellation banner */}
      {showFailureBanner && failureMessage && (
        <div
          className={`rounded-2xl border p-3 flex items-start gap-2 text-xs ${
            scopedJob?.stage === 'cancelled' && !actionError
              ? 'border-status-warning/30 bg-status-warning-soft text-status-warning-ink'
              : 'border-status-danger/30 bg-status-danger-soft text-status-danger-ink'
          }`}
        >
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span className="leading-relaxed break-words">{failureMessage}</span>
        </div>
      )}

      {/* Saved summary view */}
      {scopedSummary && (
        <div className="space-y-4">
          {/* Action toolbar */}
          <div className="flex flex-wrap items-center justify-between gap-2 pb-2 border-b border-subtle">
            <div className="flex flex-wrap items-center gap-1.5 min-w-0">
              <span className="inline-flex items-center gap-1 rounded-full border border-subtle bg-subtle px-2.5 py-0.5 text-[11px] font-medium text-ink-secondary">
                <Sparkles className="h-3 w-3 text-accent" />
                <span>{formatSourceBadge(scopedSummary.source_type, scopedSummary.source_label)}</span>
              </span>
              {scopedSummary.model && (
                <span className="text-[11px] text-ink-muted truncate max-w-[140px]">
                  {scopedSummary.model}
                </span>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-1.5 shrink-0">
              <button
                type="button"
                onClick={() => void handleCopyMarkdown()}
                className="inline-flex items-center gap-1 rounded-full bg-accent px-3 py-1.5 text-xs font-semibold text-ink-inverse hover:bg-accent-hover shadow-accent transition-colors cursor-pointer"
              >
                {copied ? (
                  <>
                    <Check className="h-3.5 w-3.5" />
                    <span>已复制</span>
                  </>
                ) : (
                  <>
                    <Copy className="h-3.5 w-3.5" />
                    <span>复制 Markdown</span>
                  </>
                )}
              </button>

              <Tooltip content={aiConfigured ? '使用最新字幕/文稿重新生成总结' : '请先在设置页配置 AI 接口'}>
                <button
                  type="button"
                  onClick={() => void handleGenerate({ force: true })}
                  disabled={Boolean(scopedJob?.running) || submitting || !aiConfigured}
                  className="inline-flex items-center gap-1 rounded-full border border-default bg-card px-2.5 py-1.5 text-xs font-medium text-ink-primary hover:bg-subtle disabled:opacity-50 disabled:pointer-events-none transition-colors cursor-pointer"
                >
                  <RefreshCw className="h-3 w-3" />
                  <span>重新生成</span>
                </button>
              </Tooltip>

              {canRetranscribe && (
                <Tooltip
                  content={
                    asrConfigured
                      ? '强制重新提取音频并语音转写（适合更换 ASR 模型后使用）'
                      : '请先在设置页配置语音转文字（ASR）接口'
                  }
                >
                  <button
                    type="button"
                    onClick={() => void handleGenerate({ force: true, force_retranscribe: true })}
                    disabled={
                      Boolean(scopedJob?.running) || submitting || !aiConfigured || !asrConfigured || !hasFfmpeg
                    }
                    className="inline-flex items-center gap-1 rounded-full border border-subtle bg-subtle px-2.5 py-1.5 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary disabled:opacity-50 disabled:pointer-events-none transition-colors cursor-pointer"
                  >
                    <RotateCcw className="h-3 w-3" />
                    <span>重新转写</span>
                  </button>
                </Tooltip>
              )}
            </div>
          </div>

          {/* Section 1: Outline */}
          <section className="space-y-2.5">
            <div className="flex items-center gap-1.5">
              <ListOrdered className="h-4 w-4 text-accent shrink-0" />
              <h3 className="text-sm font-bold text-ink-primary">AI 总结视频大纲</h3>
            </div>

            {scopedSummary.overview && (
              <div className="rounded-2xl border border-subtle bg-subtle/70 p-3 text-xs sm:text-sm text-ink-secondary leading-relaxed">
                {scopedSummary.overview}
              </div>
            )}

            {scopedSummary.outline.length > 0 && (
              <ol className="space-y-2">
                {scopedSummary.outline.map((item, idx) => {
                  const hasStart =
                    scopedSummary.has_timestamps &&
                    item.start_sec !== null &&
                    item.start_sec !== undefined;
                  return (
                    <li
                      key={`${idx}-${item.title}`}
                      className="rounded-2xl border border-subtle bg-card p-3 space-y-1"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="flex items-start gap-2 min-w-0">
                          <span className="mt-0.5 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[10px] font-bold text-accent-ink">
                            {idx + 1}
                          </span>
                          <span className="text-xs font-semibold text-ink-primary leading-snug">
                            {item.title}
                          </span>
                        </div>

                        {hasStart && (
                          <button
                            type="button"
                            onClick={() => onSeek(item.start_sec!)}
                            aria-label={`跳转到 ${formatTimestamp(item.start_sec!)}`}
                            className="inline-flex items-center gap-1 rounded-full border border-accent-ring/30 bg-accent-soft px-2 py-0.5 text-[11px] font-medium tabular-nums text-accent-ink hover:opacity-85 transition-opacity shrink-0 cursor-pointer"
                          >
                            <Clock className="h-3 w-3" />
                            <span>{formatTimestamp(item.start_sec!)}</span>
                          </button>
                        )}
                      </div>

                      {item.summary && (
                        <p className="pl-6 text-xs text-ink-secondary leading-relaxed">
                          {item.summary}
                        </p>
                      )}
                    </li>
                  );
                })}
              </ol>
            )}
          </section>

          {/* Section 2: Chronological Timeline */}
          <section className="space-y-2.5">
            <div className="flex items-center gap-1.5">
              <FileText className="h-4 w-4 text-accent shrink-0" />
              <h3 className="text-sm font-bold text-ink-primary">AI 总结视频内容（按时间顺序）</h3>
            </div>

            <div className="space-y-2.5">
              {scopedSummary.timeline.map((section, idx) => {
                const isActive = idx === activeTimelineIndex;
                const hasStart =
                  scopedSummary.has_timestamps &&
                  section.start_sec !== null &&
                  section.start_sec !== undefined;
                const hasEnd =
                  scopedSummary.has_timestamps &&
                  section.end_sec !== null &&
                  section.end_sec !== undefined;

                const timeLabel =
                  hasStart && hasEnd
                    ? `${formatTimestamp(section.start_sec!)} - ${formatTimestamp(section.end_sec!)}`
                    : hasStart
                      ? formatTimestamp(section.start_sec!)
                      : null;

                return (
                  <article
                    key={`${idx}-${section.title}`}
                    aria-current={isActive ? 'true' : undefined}
                    data-active={isActive ? 'true' : undefined}
                    className={`relative rounded-2xl border p-3.5 space-y-2 transition-colors ${
                      isActive
                        ? 'border-accent/40 bg-accent-soft'
                        : 'border-subtle bg-card hover:bg-subtle/40'
                    }`}
                  >
                    {isActive && (
                      <span
                        aria-hidden="true"
                        className="absolute inset-y-2.5 left-0 w-1 rounded-r-full bg-accent"
                      />
                    )}

                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <h4 className="text-xs sm:text-sm font-semibold text-ink-primary leading-snug">
                        {section.title}
                      </h4>

                      {timeLabel && hasStart && (
                        <button
                          type="button"
                          onClick={() => onSeek(section.start_sec!)}
                          className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-[11px] font-semibold tabular-nums transition-colors shrink-0 cursor-pointer ${
                            isActive
                              ? 'border-accent bg-accent text-ink-inverse'
                              : 'border-accent-ring/30 bg-accent-soft text-accent-ink hover:opacity-85'
                          }`}
                        >
                          <Clock className="h-3 w-3" />
                          <span>{timeLabel}</span>
                        </button>
                      )}
                    </div>

                    {section.summary && (
                      <p className="text-xs text-ink-secondary leading-relaxed">
                        {section.summary}
                      </p>
                    )}

                    {section.key_points && section.key_points.length > 0 && (
                      <ul className="space-y-1 pl-4 list-disc text-xs text-ink-secondary">
                        {section.key_points.map((kp, kpIdx) => (
                          <li key={`${kpIdx}-${kp}`} className="leading-relaxed">
                            {kp}
                          </li>
                        ))}
                      </ul>
                    )}
                  </article>
                );
              })}
            </div>
          </section>
        </div>
      )}

      {/* Empty states when no saved summary exists and no single-lesson job is running */}
      {!scopedSummary && !scopedJob?.running && capabilities && (
        <div className="rounded-2xl border border-subtle bg-subtle/40 p-4 space-y-3.5">
          {!aiConfigured ? (
            <div className="space-y-4">
              <div className="space-y-3 text-center py-1">
                <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-2xl bg-status-warning-soft text-status-warning-ink">
                  <Settings className="h-5 w-5" />
                </div>
                <div className="space-y-1">
                  <p className="text-sm font-semibold text-ink-primary">尚未配置 AI 接口</p>
                  <p className="text-xs text-ink-secondary leading-relaxed">
                    请先在系统设置中填写 OpenAI 兼容 Base URL、API Key 与模型名称，即可开启每课 AI 总结。
                  </p>
                </div>
                <Link
                  to="/settings"
                  className="inline-flex items-center gap-1.5 rounded-full bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse hover:bg-accent-hover shadow-accent transition-colors"
                >
                  <Settings className="h-3.5 w-3.5" />
                  <span>前往配置 AI 接口</span>
                </Link>
              </div>

              {asrConfigured && !hasSubtitle && !hasCachedTranscript && (
                <div className="space-y-3 border-t border-subtle pt-3">
                  <div className="space-y-1">
                    <div className="flex items-center gap-1.5 text-xs font-semibold text-ink-primary">
                      <Mic className="h-3.5 w-3.5 text-accent" />
                      <span>已配置语音转文字（ASR）</span>
                    </div>
                    <p className="text-xs text-ink-secondary leading-relaxed">
                      即使暂未配置 AI 总结模型，也可以先将当前课时语音转写为「AI 转写字幕」。
                    </p>
                  </div>
                  {partialCheckpointBanner}
                  {!hasFfmpeg && (
                    <p className="rounded-xl border border-status-warning/30 bg-status-warning-soft px-3 py-2 text-xs text-status-warning-ink">
                      当前服务端未检测到 FFmpeg，无法自动抽取音频。
                    </p>
                  )}
                  <button
                    type="button"
                    onClick={() => void handleTranscribe({})}
                    disabled={submitting || !hasFfmpeg}
                    className="inline-flex items-center gap-1.5 rounded-full border border-default bg-card px-4 py-2 text-xs font-semibold text-ink-primary hover:bg-subtle disabled:opacity-50 transition-colors cursor-pointer"
                  >
                    <Mic className="h-3.5 w-3.5 text-accent" />
                    <span>{hasPartialCheckpoint ? '继续转写' : '仅语音转文字'}</span>
                  </button>
                </div>
              )}

              {asrConfigured && hasCachedTranscript && (
                <div className="space-y-2 border-t border-subtle pt-3">
                  <p className="text-xs text-ink-secondary leading-relaxed">
                    本节已存在缓存转写文稿（AI 转写字幕），配置 AI 总结模型后即可直接生成总结。
                  </p>
                  {hasFfmpeg && (
                    <button
                      type="button"
                      onClick={() => void handleTranscribe({ force_retranscribe: true })}
                      disabled={submitting}
                      className="inline-flex items-center gap-1.5 rounded-full border border-default bg-card px-3.5 py-1.5 text-xs font-medium text-ink-secondary hover:text-ink-primary hover:bg-subtle disabled:opacity-50 transition-colors cursor-pointer"
                    >
                      <RotateCcw className="h-3 w-3" />
                      <span>重新转写</span>
                    </button>
                  )}
                </div>
              )}
            </div>
          ) : hasSubtitle || hasCachedTranscript ? (
            <div className="space-y-3">
              <div className="space-y-1">
                <div className="flex items-center gap-1.5 text-sm font-semibold text-ink-primary">
                  <Sparkles className="h-4 w-4 text-accent" />
                  <span>生成本节 AI 总结</span>
                </div>
                <p className="text-xs text-ink-secondary leading-relaxed">
                  {hasSubtitle
                    ? '已检测到本节可用字幕，将提炼视频大纲与按时间顺序的知识点总结。'
                    : '已检测到缓存转写文稿，可直接提炼视频大纲与时间线总结。'}
                </p>
              </div>

              <div className="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  onClick={() => void handleGenerate({})}
                  disabled={submitting}
                  className="inline-flex items-center gap-1.5 rounded-full bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse hover:bg-accent-hover shadow-accent disabled:opacity-50 transition-colors cursor-pointer"
                >
                  <Sparkles className="h-3.5 w-3.5" />
                  <span>生成 AI 总结</span>
                </button>

                <button
                  type="button"
                  onClick={() => void onStartBatch('missing_only')}
                  disabled={startingBatch || Boolean(batchJob?.running)}
                  className="inline-flex items-center gap-1.5 rounded-full border border-default bg-card px-3.5 py-2 text-xs font-medium text-ink-primary hover:bg-subtle disabled:opacity-50 transition-colors cursor-pointer"
                >
                  <span>为整门课批量生成</span>
                </button>

                {hasCachedTranscript && asrConfigured && hasFfmpeg && (
                  <Tooltip content="强制重新提取音频并语音转写（仅更新转写文稿与字幕，不生成总结）">
                    <button
                      type="button"
                      onClick={() => void handleTranscribe({ force_retranscribe: true })}
                      disabled={submitting}
                      className="inline-flex items-center gap-1 rounded-full border border-subtle bg-card px-3 py-2 text-xs font-medium text-ink-secondary hover:text-ink-primary hover:bg-subtle disabled:opacity-50 transition-colors cursor-pointer"
                    >
                      <RotateCcw className="h-3 w-3" />
                      <span>重新转写</span>
                    </button>
                  </Tooltip>
                )}
              </div>
            </div>
          ) : asrConfigured ? (
            <div className="space-y-3">
              <div className="space-y-1">
                <div className="flex items-center gap-1.5 text-sm font-semibold text-ink-primary">
                  <Mic className="h-4 w-4 text-accent" />
                  <span>自动语音转写并总结</span>
                </div>
                <p className="text-xs text-ink-secondary leading-relaxed">
                  当前课时暂无外挂或内嵌文本字幕，可仅转写生成「AI 转写字幕」，或一键转写并生成总结（约 1~3 分钟）。
                </p>
              </div>

              {partialCheckpointBanner}

              {!hasFfmpeg && (
                <p className="rounded-xl border border-status-warning/30 bg-status-warning-soft px-3 py-2 text-xs text-status-warning-ink">
                  当前服务端未检测到 FFmpeg，无法自动抽取音频。你可以改为手动粘贴文稿生成总结。
                </p>
              )}

              <div className="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  onClick={() => void handleGenerate({})}
                  disabled={submitting || !hasFfmpeg}
                  className="inline-flex items-center gap-1.5 rounded-full bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse hover:bg-accent-hover shadow-accent disabled:opacity-50 transition-colors cursor-pointer"
                >
                  <Sparkles className="h-3.5 w-3.5" />
                  <span>转写并生成总结</span>
                </button>

                <button
                  type="button"
                  onClick={() => void handleTranscribe({})}
                  disabled={submitting || !hasFfmpeg}
                  className="inline-flex items-center gap-1.5 rounded-full border border-default bg-card px-3.5 py-2 text-xs font-medium text-ink-primary hover:bg-subtle disabled:opacity-50 transition-colors cursor-pointer"
                >
                  <Mic className="h-3.5 w-3.5 text-accent" />
                  <span>{hasPartialCheckpoint ? '继续转写' : '仅语音转文字'}</span>
                </button>

                <button
                  type="button"
                  onClick={() => setShowManualInput((prev) => !prev)}
                  className="inline-flex items-center gap-1 rounded-full border border-default bg-card px-3 py-2 text-xs font-medium text-ink-secondary hover:text-ink-primary hover:bg-subtle transition-colors cursor-pointer"
                >
                  <span>改为手动粘贴文稿</span>
                  {showManualInput ? (
                    <ChevronUp className="h-3.5 w-3.5" />
                  ) : (
                    <ChevronDown className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>

              {showManualInput && (
                <div className="space-y-2.5 pt-2 border-t border-subtle">
                  <textarea
                    aria-label="手动粘贴文稿"
                    value={manualTranscript}
                    onChange={(e) => setManualTranscript(e.target.value)}
                    rows={5}
                    placeholder="在此粘贴 SRT / VTT 字幕内容或纯文本讲稿..."
                    className="w-full rounded-2xl border border-default bg-card p-3 text-xs text-ink-primary placeholder:text-ink-muted focus:outline-none focus:ring-2 focus:ring-accent-ring"
                  />
                  <button
                    type="button"
                    onClick={() =>
                      void handleGenerate({ manual_transcript: manualTranscript })
                    }
                    disabled={submitting || !manualTranscript.trim()}
                    className="inline-flex items-center gap-1.5 rounded-full bg-accent px-4 py-1.5 text-xs font-semibold text-ink-inverse hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
                  >
                    <span>根据文稿生成总结</span>
                  </button>
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-3">
              <div className="space-y-1">
                <p className="text-sm font-semibold text-ink-primary">
                  当前课时无字幕且未配置语音转文字
                </p>
                <p className="text-xs text-ink-secondary leading-relaxed">
                  你可以配置语音转文字（ASR）服务自动转写生成字幕，前往编辑页绑定外挂字幕，或在下方直接粘贴讲稿/字幕生成总结。
                </p>
              </div>

              <div className="flex flex-wrap items-center gap-2">
                <Link
                  to="/settings"
                  className="inline-flex items-center gap-1 rounded-full border border-default bg-card px-3 py-1.5 text-xs font-medium text-accent-ink hover:bg-subtle transition-colors"
                >
                  <Settings className="h-3.5 w-3.5" />
                  <span>前往配置语音转文字</span>
                </Link>
                <Link
                  to={`/courses/${course.id}/edit`}
                  className="inline-flex items-center gap-1 rounded-full border border-default bg-card px-3 py-1.5 text-xs font-medium text-ink-primary hover:bg-subtle transition-colors"
                >
                  <span>去编辑页绑定字幕</span>
                </Link>
              </div>

              <div className="space-y-2.5 pt-2 border-t border-subtle">
                <textarea
                  aria-label="手动粘贴文稿"
                  value={manualTranscript}
                  onChange={(e) => setManualTranscript(e.target.value)}
                  rows={5}
                  placeholder="在此粘贴 SRT / VTT 字幕内容或纯文本讲稿..."
                  className="w-full rounded-2xl border border-default bg-card p-3 text-xs text-ink-primary placeholder:text-ink-muted focus:outline-none focus:ring-2 focus:ring-accent-ring"
                />
                <button
                  type="button"
                  onClick={() =>
                    void handleGenerate({ manual_transcript: manualTranscript })
                  }
                  disabled={submitting || !manualTranscript.trim()}
                  className="inline-flex items-center gap-1.5 rounded-full bg-accent px-4 py-1.5 text-xs font-semibold text-ink-inverse hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
                >
                  <Sparkles className="h-3.5 w-3.5" />
                  <span>根据文稿生成总结</span>
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
