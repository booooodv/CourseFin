import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ImagePlus,
  Loader2,
  RefreshCw,
  Sparkles,
  X,
} from 'lucide-react';
import { api, courseCoverImageUrl, itemPrimaryImageUrl } from '../api/client';
import CourseCoverGallery, { formatCoverSourceLabel } from './CourseCoverGallery';
import CoverStylePicker, { FALLBACK_COVER_STYLES } from './CoverStylePicker';
import Tooltip from './Tooltip';
import type {
  CourseCover,
  CourseDetail,
  CoverStyleTemplate,
} from '../types/coursefin';

export interface AICoverModalProps {
  course: CourseDetail;
  isOpen: boolean;
  onClose: () => void;
  onCourseUpdated: (updatedCourse: CourseDetail) => void;
}

const INPUT_CLASS =
  'w-full rounded-xl border border-default bg-subtle/60 px-3.5 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors';

export default function AICoverModal({
  course,
  isOpen,
  onClose,
  onCourseUpdated,
}: AICoverModalProps) {
  const [styles, setStyles] = useState<CoverStyleTemplate[]>(FALLBACK_COVER_STYLES);
  const [defaultStyleId, setDefaultStyleId] = useState<string>('editorial_modern');
  const [styleId, setStyleId] = useState<string>('editorial_modern');
  const [aiImageConfigured, setAiImageConfigured] = useState<boolean>(true);
  const [stylesLoaded, setStylesLoaded] = useState<boolean>(false);

  const [title, setTitle] = useState<string>('');
  const [subtitle, setSubtitle] = useState<string>('');
  const [includeBadge, setIncludeBadge] = useState<boolean>(true);
  const [badge, setBadge] = useState<string>('');
  const [extraPrompt, setExtraPrompt] = useState<string>('');

  const [generating, setGenerating] = useState<boolean>(false);
  const [busyCoverId, setBusyCoverId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [showPrompt, setShowPrompt] = useState<boolean>(false);
  const [currentCoverFailed, setCurrentCoverFailed] = useState<boolean>(false);

  useEffect(() => {
    if (!isOpen) return;

    const defaultBadge =
      course.lesson_count > 0 || course.chapter_count > 0
        ? `${course.lesson_count} 节 · ${course.chapter_count} 章`
        : '';

    setTitle(course.title);
    setSubtitle(course.instructor ? `讲师 · ${course.instructor}` : '');
    setIncludeBadge(Boolean(defaultBadge));
    setBadge(defaultBadge);
    setExtraPrompt('');
    setBusyCoverId(null);
    setError(null);
    setNotice(null);
    setShowPrompt(false);
    setCurrentCoverFailed(false);
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
  }, [isOpen, course.id, course.title, course.instructor, course.lesson_count, course.chapter_count]);

  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !generating && !busyCoverId) {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, generating, busyCoverId, onClose]);

  if (!isOpen) return null;

  const covers = course.covers ?? [];
  const activeCover = covers.find((c) => c.is_active) ?? covers[0] ?? null;
  const hasExistingCover =
    Boolean(activeCover || course.cover_path || course.first_lesson_id) && !currentCoverFailed;
  const isBusy = generating || Boolean(busyCoverId);

  async function handleGenerate() {
    if (isBusy) return;
    setGenerating(true);
    setError(null);
    setNotice(null);
    try {
      const result = await api.generateCourseCover(course.id, {
        style_id: styleId,
        title: title.trim() || course.title,
        subtitle: subtitle.trim(),
        include_badge: includeBadge,
        badge: badge.trim(),
        extra_prompt: extraPrompt.trim(),
      });
      setCurrentCoverFailed(false);
      setNotice('已生成新封面并设为当前，原有封面已保留');
      onCourseUpdated(result.course);
    } catch (err) {
      setError(err instanceof Error ? err.message : '生成 AI 封面失败');
    } finally {
      setGenerating(false);
    }
  }

  async function handleActivateCover(cover: CourseCover) {
    if (isBusy || cover.is_active) return;
    setBusyCoverId(cover.id);
    setError(null);
    setNotice(null);
    try {
      const updated = await api.activateCourseCover(course.id, cover.id);
      setCurrentCoverFailed(false);
      setNotice('已切换为当前课程封面');
      onCourseUpdated(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : '切换封面失败');
    } finally {
      setBusyCoverId(null);
    }
  }

  async function handleDeleteCover(cover: CourseCover) {
    if (isBusy) return;
    setBusyCoverId(cover.id);
    setError(null);
    setNotice(null);
    try {
      const updated = await api.deleteCourseCover(course.id, cover.id);
      setCurrentCoverFailed(false);
      setNotice(
        cover.is_active
          ? updated.cover_path
            ? '已删除当前封面并自动切换历史封面'
            : '已删除封面'
          : '已删除该历史封面',
      );
      onCourseUpdated(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除封面失败');
    } finally {
      setBusyCoverId(null);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="ai-cover-modal-title"
    >
      <div className="flex max-h-[92vh] w-full max-w-4xl flex-col overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-subtle px-6 py-4">
          <div className="flex items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
              <ImagePlus className="h-4 w-4" />
            </span>
            <div>
              <h2 id="ai-cover-modal-title" className="text-base font-bold text-ink-primary">
                AI 生成课程封面
              </h2>
              <p className="text-xs text-ink-muted">
                选择风格模板生成新封面；每次生成都会自动保存为课程关联封面并设为当前，历史封面全部保留，随时可切回。
              </p>
            </div>
          </div>

          <Tooltip content="关闭封面生成弹窗">
            <button
              type="button"
              aria-label="关闭"
              disabled={isBusy}
              onClick={onClose}
              className="rounded-lg p-1.5 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors disabled:opacity-50 cursor-pointer"
            >
              <X className="h-5 w-5" />
            </button>
          </Tooltip>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
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
                  填写接口地址、密钥与生图模型名称后再试。
                </p>
              </div>
            </div>
          )}

          {/* Style Template Picker */}
          <div className="space-y-2">
            <label className="block text-xs font-semibold text-ink-secondary">
              选择封面风格模板
            </label>
            <CoverStylePicker
              styles={styles}
              value={styleId}
              onChange={setStyleId}
              defaultStyleId={defaultStyleId}
              disabled={isBusy}
            />
          </div>

          {/* Copy & Optional Prompt Inputs */}
          <div className="rounded-2xl border border-subtle bg-card p-4 space-y-3 shadow-card">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div className="space-y-1">
                <label htmlFor="ai-cover-title" className="block text-xs font-medium text-ink-secondary">
                  封面主标题
                </label>
                <input
                  id="ai-cover-title"
                  type="text"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  disabled={isBusy}
                  placeholder={course.title}
                  className={INPUT_CLASS}
                />
              </div>

              <div className="space-y-1">
                <label htmlFor="ai-cover-subtitle" className="block text-xs font-medium text-ink-secondary">
                  副标题 / 讲师（留空则不显示）
                </label>
                <input
                  id="ai-cover-subtitle"
                  type="text"
                  value={subtitle}
                  onChange={(e) => setSubtitle(e.target.value)}
                  disabled={isBusy}
                  placeholder="例如：讲师 · 陈默"
                  className={INPUT_CLASS}
                />
              </div>

              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <label htmlFor="ai-cover-badge" className="block text-xs font-medium text-ink-secondary">
                    角标文案
                  </label>
                  <label className="inline-flex items-center gap-1.5 text-[11px] text-ink-muted cursor-pointer">
                    <input
                      id="ai-cover-include-badge"
                      type="checkbox"
                      checked={includeBadge}
                      disabled={isBusy}
                      onChange={(e) => setIncludeBadge(e.target.checked)}
                      className="h-3.5 w-3.5 rounded border-default bg-card text-accent focus:ring-2 focus:ring-accent-ring"
                    />
                    <span>显示角标</span>
                  </label>
                </div>
                <input
                  id="ai-cover-badge"
                  type="text"
                  value={badge}
                  onChange={(e) => setBadge(e.target.value)}
                  disabled={!includeBadge || isBusy}
                  placeholder="例如：24 节 · 5 章"
                  className={`${INPUT_CLASS} disabled:opacity-50`}
                />
              </div>

              <div className="space-y-1">
                <label htmlFor="ai-cover-extra-prompt" className="block text-xs font-medium text-ink-secondary">
                  画面补充词（选填）
                </label>
                <input
                  id="ai-cover-extra-prompt"
                  type="text"
                  value={extraPrompt}
                  onChange={(e) => setExtraPrompt(e.target.value)}
                  disabled={isBusy}
                  placeholder="例如：右侧加入齿轮与数据流元素"
                  className={INPUT_CLASS}
                />
              </div>
            </div>
          </div>

          {error && (
            <div className="rounded-2xl border border-status-danger/30 bg-status-danger-soft px-4 py-3 text-xs text-status-danger-ink space-y-1">
              <div className="flex items-center gap-2 font-semibold">
                <AlertTriangle className="h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
              <p className="text-status-danger-ink/85">
                如接口未配置或密钥错误，可前往{' '}
                <Link to="/settings" onClick={onClose} className="underline font-semibold">
                  系统设置
                </Link>{' '}
                检查生图配置。
              </p>
            </div>
          )}

          {notice && (
            <div className="flex items-center gap-2 rounded-2xl border border-status-success/30 bg-status-success-soft px-4 py-2.5 text-xs font-medium text-status-success-ink">
              <CheckCircle2 className="h-4 w-4 shrink-0" />
              <span>{notice}</span>
            </div>
          )}

          {/* Current Active Cover Showcase */}
          <div className="rounded-2xl border border-accent-ring bg-accent-soft/35 p-4 space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="text-xs font-semibold text-accent-ink">当前激活封面 (16:10)</span>
              {activeCover && (
                <span className="text-[11px] text-ink-muted tabular-nums">
                  {formatCoverSourceLabel(activeCover)}
                  {activeCover.width > 0 && activeCover.height > 0
                    ? ` · ${activeCover.width}×${activeCover.height}`
                    : ''}
                </span>
              )}
            </div>

            <div className="relative aspect-[16/10] w-full max-w-xl mx-auto overflow-hidden rounded-xl border border-subtle bg-card flex items-center justify-center">
              {generating ? (
                <div className="flex flex-col items-center gap-2 text-ink-secondary">
                  <Loader2 className="h-7 w-7 animate-spin text-accent" />
                  <span className="text-xs font-medium">正在调用生图模型绘制并保存新封面...</span>
                </div>
              ) : activeCover && !currentCoverFailed ? (
                <img
                  src={courseCoverImageUrl(activeCover.image_url, 960)}
                  alt={`${course.title} 当前封面`}
                  onError={() => setCurrentCoverFailed(true)}
                  className="h-full w-full object-cover"
                />
              ) : hasExistingCover ? (
                <img
                  src={itemPrimaryImageUrl(course.id, course.updated_at, 960)}
                  alt={`${course.title} 当前封面`}
                  onError={() => setCurrentCoverFailed(true)}
                  className="h-full w-full object-cover"
                />
              ) : (
                <span className="text-xs text-ink-muted px-4 text-center">
                  暂无封面，选择风格模板后点击下方「生成新封面」
                </span>
              )}
            </div>

            {activeCover?.prompt && (
              <div>
                <button
                  type="button"
                  onClick={() => setShowPrompt((v) => !v)}
                  className="inline-flex items-center gap-1 text-[11px] font-medium text-ink-secondary hover:text-ink-primary cursor-pointer"
                >
                  {showPrompt ? (
                    <ChevronDown className="h-3.5 w-3.5" />
                  ) : (
                    <ChevronRight className="h-3.5 w-3.5" />
                  )}
                  <span>查看生成 Prompt</span>
                </button>
                {showPrompt && (
                  <pre className="mt-1.5 max-h-32 overflow-y-auto whitespace-pre-wrap rounded-xl border border-subtle bg-card p-2.5 text-[11px] leading-relaxed text-ink-secondary">
                    {activeCover.prompt}
                  </pre>
                )}
              </div>
            )}
          </div>

          {/* Course Cover History Gallery */}
          {covers.length > 0 && (
            <div className="space-y-2.5">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-xs font-semibold text-ink-secondary">
                  关联封面历史 ({covers.length})
                </h3>
                <span className="text-[11px] text-ink-muted">
                  点击缩略图即可切回该封面，支持删除不需要的历史版本
                </span>
              </div>
              <CourseCoverGallery
                covers={covers}
                busyCoverId={busyCoverId}
                disabled={generating}
                onActivate={handleActivateCover}
                onDelete={handleDeleteCover}
              />
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex flex-wrap items-center justify-end gap-2.5 border-t border-subtle px-6 py-4">
          <button
            type="button"
            disabled={isBusy}
            onClick={onClose}
            className="rounded-xl border border-default bg-card px-4 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
          >
            关闭
          </button>

          <button
            type="button"
            disabled={isBusy || (stylesLoaded && !aiImageConfigured)}
            onClick={() => void handleGenerate()}
            className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
          >
            {generating ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : covers.length > 0 ? (
              <RefreshCw className="h-3.5 w-3.5" />
            ) : (
              <Sparkles className="h-3.5 w-3.5" />
            )}
            <span>{generating ? '生成中...' : covers.length > 0 ? '再生成一张' : '生成新封面'}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
