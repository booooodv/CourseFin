import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  CheckCircle2,
  ImagePlus,
  Loader2,
  Sparkles,
  Square,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import CoverStylePicker, { FALLBACK_COVER_STYLES } from './CoverStylePicker';
import { useToast } from './Toast';
import Tooltip from './Tooltip';
import type {
  BatchCoverJob,
  CourseSummary,
  CoverStyleTemplate,
  Library,
} from '../types/coursefin';

export interface BatchCoverModalProps {
  isOpen: boolean;
  onClose: () => void;
  courses: CourseSummary[];
  activeLibrary: Library | null;
  batchJob: BatchCoverJob | null;
  onJobUpdated: (job: BatchCoverJob) => void;
}

export default function BatchCoverModal({
  isOpen,
  onClose,
  courses,
  activeLibrary,
  batchJob,
  onJobUpdated,
}: BatchCoverModalProps) {
  const { toast } = useToast();
  const [styles, setStyles] = useState<CoverStyleTemplate[]>(FALLBACK_COVER_STYLES);
  const [defaultStyleId, setDefaultStyleId] = useState<string>('editorial_modern');
  const [styleId, setStyleId] = useState<string>('editorial_modern');
  const [aiImageConfigured, setAiImageConfigured] = useState<boolean>(true);
  const [stylesLoaded, setStylesLoaded] = useState<boolean>(false);

  const [mode, setMode] = useState<'missing_only' | 'all'>('missing_only');
  const [includeBadge, setIncludeBadge] = useState<boolean>(true);
  const [extraPrompt, setExtraPrompt] = useState<string>('');
  const [starting, setStarting] = useState<boolean>(false);
  const [cancelling, setCancelling] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const totalCount = courses.length;
  const missingCount = useMemo(
    () => courses.filter((c) => !c.cover_path || c.cover_path.trim() === '').length,
    [courses],
  );

  useEffect(() => {
    if (!isOpen) return;
    setError(null);
    setStylesLoaded(false);

    let cancelled = false;
    api
      .listCoverStyles()
      .then((res) => {
        if (cancelled) return;
        if (res.styles && res.styles.length > 0) {
          setStyles(res.styles);
        }
        const def = res.default_style_id || 'editorial_modern';
        setDefaultStyleId(def);
        setStyleId(def);
        setAiImageConfigured(Boolean(res.ai_image_configured));
        setStylesLoaded(true);
      })
      .catch(() => {
        if (!cancelled) {
          setStylesLoaded(true);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const isRunning = Boolean(batchJob?.running);
  const targetCount = mode === 'missing_only' ? missingCount : totalCount;
  const processedCount = batchJob
    ? batchJob.completed + batchJob.failed + batchJob.skipped
    : 0;
  const progressPct =
    batchJob && batchJob.total > 0
      ? Math.min(100, Math.round((processedCount / batchJob.total) * 100))
      : 0;

  async function handleStart() {
    if (starting || isRunning) return;
    setStarting(true);
    setError(null);
    try {
      const job = await api.startBatchCovers({
        library_id: activeLibrary?.id || '',
        mode,
        style_id: styleId,
        include_badge: includeBadge,
        extra_prompt: extraPrompt.trim(),
      });
      onJobUpdated(job);
      if (!job.running && job.completed === 0 && job.skipped === job.total) {
        toast.info('当前范围内全部课程均已设置专属封面');
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : '启动批量封面生成失败';
      setError(msg);
      toast.error(msg);
    } finally {
      setStarting(false);
    }
  }

  async function handleCancel() {
    if (cancelling || !isRunning) return;
    setCancelling(true);
    setError(null);
    try {
      const job = await api.cancelBatchCovers();
      onJobUpdated(job);
      toast.info('已停止批量封面生成');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '取消批量任务失败';
      setError(msg);
      toast.error(msg);
    } finally {
      setCancelling(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="batch-cover-modal-title"
    >
      <div className="flex max-h-[92vh] w-full max-w-3xl flex-col overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-subtle px-6 py-4">
          <div className="flex items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
              <ImagePlus className="h-4 w-4" />
            </span>
            <div>
              <h2 id="batch-cover-modal-title" className="text-base font-bold text-ink-primary">
                AI 批量生成课程封面
              </h2>
              <p className="text-xs text-ink-muted">
                按统一风格模板依次生成封面，全库视觉整齐统一；生成过程中可随时关闭弹窗后台运行。
              </p>
            </div>
          </div>

          <Tooltip content="关闭弹窗">
            <button
              type="button"
              aria-label="关闭"
              onClick={onClose}
              className="rounded-lg p-1.5 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
            >
              <X className="h-5 w-5" />
            </button>
          </Tooltip>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {/* Scope Summary */}
          <div className="rounded-2xl border border-subtle bg-subtle/60 px-4 py-3 text-xs text-ink-secondary flex flex-wrap items-center justify-between gap-2">
            <span>
              当前范围：<strong className="text-ink-primary">{activeLibrary ? activeLibrary.name : '全部课程'}</strong>
            </span>
            <span className="tabular-nums">
              共 <strong className="text-ink-primary">{totalCount}</strong> 门课程 · 其中{' '}
              <strong className="text-accent-ink">{missingCount}</strong> 门尚未设置专属封面
            </span>
          </div>

          {stylesLoaded && !aiImageConfigured && (
            <div className="flex items-start gap-3 rounded-2xl border border-status-warning/35 bg-status-warning-soft px-4 py-3 text-xs text-status-warning-ink">
              <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
              <div className="space-y-1">
                <p className="font-semibold">尚未配置 AI 封面生图接口</p>
                <p>
                  请前往{' '}
                  <Link
                    to="/settings"
                    onClick={onClose}
                    className="underline font-semibold hover:opacity-80"
                  >
                    系统设置 → AI 封面生图接口
                  </Link>{' '}
                  完成配置后再运行批量生成。
                </p>
              </div>
            </div>
          )}

          {/* Mode & Style Config */}
          <div className="space-y-4">
            <div className="space-y-2">
              <span className="block text-xs font-semibold text-ink-secondary">生成范围模式</span>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                <label
                  className={`flex items-start gap-2.5 rounded-2xl border p-3 cursor-pointer transition-colors ${
                    mode === 'missing_only'
                      ? 'border-accent bg-accent-soft/60'
                      : 'border-subtle bg-card hover:bg-subtle/50'
                  }`}
                >
                  <input
                    type="radio"
                    name="batch-cover-mode"
                    value="missing_only"
                    checked={mode === 'missing_only'}
                    disabled={isRunning}
                    onChange={() => setMode('missing_only')}
                    className="mt-0.5 h-4 w-4 text-accent focus:ring-2 focus:ring-accent-ring"
                  />
                  <div className="text-xs">
                    <p className="font-semibold text-ink-primary">仅补齐缺失封面（推荐）</p>
                    <p className="mt-0.5 text-ink-muted tabular-nums">
                      仅处理 {missingCount} 门无专属封面的课程，保留已有封面
                    </p>
                  </div>
                </label>

                <label
                  className={`flex items-start gap-2.5 rounded-2xl border p-3 cursor-pointer transition-colors ${
                    mode === 'all'
                      ? 'border-accent bg-accent-soft/60'
                      : 'border-subtle bg-card hover:bg-subtle/50'
                  }`}
                >
                  <input
                    type="radio"
                    name="batch-cover-mode"
                    value="all"
                    checked={mode === 'all'}
                    disabled={isRunning}
                    onChange={() => setMode('all')}
                    className="mt-0.5 h-4 w-4 text-accent focus:ring-2 focus:ring-accent-ring"
                  />
                  <div className="text-xs">
                    <p className="font-semibold text-ink-primary">为当前范围全部课程新增封面</p>
                    <p className="mt-0.5 text-ink-muted tabular-nums">
                      为 {totalCount} 门课程统一生成新封面并设为当前，原有历史封面将全部保留
                    </p>
                  </div>
                </label>
              </div>
            </div>

            <div className="space-y-2">
              <span className="block text-xs font-semibold text-ink-secondary">统一风格模板</span>
              <CoverStylePicker
                styles={styles}
                value={styleId}
                onChange={setStyleId}
                defaultStyleId={defaultStyleId}
                disabled={isRunning}
              />
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 rounded-2xl border border-subtle bg-card p-3.5">
              <label className="flex items-center gap-2 text-xs text-ink-primary cursor-pointer">
                <input
                  type="checkbox"
                  checked={includeBadge}
                  disabled={isRunning}
                  onChange={(e) => setIncludeBadge(e.target.checked)}
                  className="h-4 w-4 rounded border-default bg-card text-accent focus:ring-2 focus:ring-accent-ring"
                />
                <span>角标显示节数与章节信息</span>
              </label>

              <input
                type="text"
                value={extraPrompt}
                onChange={(e) => setExtraPrompt(e.target.value)}
                disabled={isRunning}
                placeholder="统一画面补充词（选填）"
                aria-label="统一画面补充词"
                className="w-full rounded-xl border border-default bg-subtle/60 px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring disabled:opacity-50"
              />
            </div>
          </div>

          {error && (
            <div className="rounded-2xl border border-status-danger/30 bg-status-danger-soft px-4 py-3 text-xs font-medium text-status-danger-ink">
              {error}
            </div>
          )}

          {/* Live / Recent Batch Job Status */}
          {batchJob && batchJob.state !== 'idle' && (
            <div className="rounded-2xl border border-subtle bg-card p-4 space-y-3 shadow-card">
              <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
                <span className="inline-flex items-center gap-1.5 font-semibold text-ink-primary">
                  {batchJob.running ? (
                    <>
                      <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                      <span>正在生成：{batchJob.current_course_title || '准备中...'}</span>
                    </>
                  ) : batchJob.state === 'cancelled' ? (
                    <span>批量任务已取消</span>
                  ) : (
                    <>
                      <CheckCircle2 className="h-3.5 w-3.5 text-status-success-ink" />
                      <span>批量任务已完成</span>
                    </>
                  )}
                </span>
                <span className="tabular-nums text-ink-muted">
                  已完成 {batchJob.completed} · 跳过 {batchJob.skipped} · 失败 {batchJob.failed} / 共{' '}
                  {batchJob.total}
                </span>
              </div>

              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full rounded-full bg-accent transition-all duration-300"
                  style={{ width: `${progressPct}%` }}
                />
              </div>

              {batchJob.errors && batchJob.errors.length > 0 && (
                <div className="max-h-32 overflow-y-auto space-y-1 rounded-xl border border-status-danger/25 bg-status-danger-soft p-2.5 text-[11px] text-status-danger-ink">
                  {batchJob.errors.map((item) => (
                    <p key={item.course_id}>
                      <strong>{item.course_title}</strong>：{item.error}
                    </p>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex flex-wrap items-center justify-end gap-2.5 border-t border-subtle px-6 py-4">
          {isRunning ? (
            <>
              <button
                type="button"
                disabled={cancelling}
                onClick={() => void handleCancel()}
                className="inline-flex items-center gap-1.5 rounded-xl border border-status-danger/35 bg-status-danger-soft px-4 py-2 text-xs font-semibold text-status-danger-ink hover:opacity-90 disabled:opacity-50 transition-opacity cursor-pointer"
              >
                <Square className="h-3.5 w-3.5 fill-current" />
                <span>{cancelling ? '取消中...' : '取消批量任务'}</span>
              </button>
              <button
                type="button"
                onClick={onClose}
                className="rounded-xl bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover transition-colors cursor-pointer"
              >
                后台运行
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={onClose}
                className="rounded-xl border border-default bg-card px-4 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
              >
                关闭
              </button>
              <button
                type="button"
                disabled={
                  starting ||
                  targetCount === 0 ||
                  (stylesLoaded && !aiImageConfigured)
                }
                onClick={() => void handleStart()}
                className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
              >
                {starting ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Sparkles className="h-3.5 w-3.5" />
                )}
                <span>
                  {starting ? '启动中...' : `开始批量生成 (${targetCount} 门)`}
                </span>
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
