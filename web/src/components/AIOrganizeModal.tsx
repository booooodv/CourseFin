import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  ArrowRight,
  Check,
  Loader2,
  RefreshCw,
  Sparkles,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import Tooltip from './Tooltip';
import type { CourseDetail, CourseOrganizeProposal } from '../types/coursefin';

interface AIOrganizeModalProps {
  course: CourseDetail;
  isOpen: boolean;
  onClose: () => void;
  onApplied: (updatedCourse: CourseDetail) => void;
}

const SECTION_CARD_CLASS = 'rounded-2xl border border-subtle bg-card p-4 space-y-3 shadow-card';
const ORIGINAL_PANE_CLASS = 'rounded-xl border border-subtle bg-subtle p-3';
const SUGGESTION_PANE_CLASS = 'rounded-xl border border-accent-ring bg-accent-soft p-3';
const CHECKBOX_CLASS =
  'h-4 w-4 rounded border-default bg-card text-accent focus:ring-2 focus:ring-accent-ring';

export default function AIOrganizeModal({
  course,
  isOpen,
  onClose,
  onApplied,
}: AIOrganizeModalProps) {
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [proposal, setProposal] = useState<CourseOrganizeProposal | null>(null);

  const [applyTitle, setApplyTitle] = useState(true);
  const [applyOverview, setApplyOverview] = useState(true);
  const [applyTags, setApplyTags] = useState(true);
  const [applyChapters, setApplyChapters] = useState(true);

  const fetchProposal = useCallback(async () => {
    setLoading(true);
    setError(null);
    setProposal(null);
    try {
      const data = await api.aiOrganizeCourse(course.id);
      setProposal(data);
      setApplyTitle(true);
      setApplyOverview(true);
      setApplyTags(true);
      setApplyChapters(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : '生成 AI 整理建议失败');
    } finally {
      setLoading(false);
    }
  }, [course.id]);

  useEffect(() => {
    if (!isOpen) return;
    void fetchProposal();
  }, [isOpen, fetchProposal]);

  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !submitting) {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, submitting, onClose]);

  if (!isOpen) return null;

  const nothingSelected = !applyTitle && !applyOverview && !applyTags && !applyChapters;

  async function handleConfirm() {
    if (!proposal || submitting || nothingSelected) return;
    setSubmitting(true);
    setError(null);
    try {
      const updated = await api.aiApplyCourse(course.id, {
        apply_title: applyTitle,
        apply_overview: applyOverview,
        apply_tags: applyTags,
        apply_chapters: applyChapters,
        title: proposal.title,
        overview: proposal.overview,
        tags: proposal.tags ?? [],
        chapters: proposal.chapters ?? [],
      });
      onApplied(updated);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : '应用 AI 整理建议失败');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="ai-organize-modal-title"
    >
      <div className="flex max-h-[90vh] w-full max-w-4xl flex-col overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-subtle px-6 py-4">
          <div className="flex items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
              <Sparkles className="h-4 w-4" />
            </span>
            <div>
              <h2 id="ai-organize-modal-title" className="text-base font-bold text-ink-primary">
                AI 智能整理预览
              </h2>
              <p className="text-xs text-ink-muted">
                以下建议仅作预览，只有您勾选并点击“确认应用”的项目才会写入课程数据。
              </p>
            </div>
          </div>

          <Tooltip content="关闭 AI 整理预览">
            <button
              type="button"
              aria-label="关闭"
              disabled={submitting}
              onClick={onClose}
              className="rounded-lg p-1.5 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors disabled:opacity-50 cursor-pointer"
            >
              <X className="h-5 w-5" />
            </button>
          </Tooltip>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {loading && (
            <div className="flex flex-col items-center justify-center py-16 text-ink-secondary gap-3">
              <Loader2 className="h-8 w-8 animate-spin text-accent" />
              <p className="text-sm font-medium">正在分析课程目录与字幕内容，生成整理方案...</p>
            </div>
          )}

          {error && (
            <div className="rounded-2xl border border-status-danger/30 bg-status-danger-soft p-4 space-y-3">
              <div className="flex items-start gap-2.5 text-status-danger-ink text-sm">
                <AlertTriangle className="h-5 w-5 shrink-0 text-status-danger-ink mt-0.5" />
                <div className="space-y-1">
                  <p className="font-medium">{error}</p>
                  <p className="text-xs text-status-danger-ink/85">
                    请确认已在{' '}
                    <Link
                      to="/settings"
                      className="underline font-semibold hover:opacity-80"
                      onClick={onClose}
                    >
                      系统设置
                    </Link>{' '}
                    中配置有效的 AI Base URL 与模型名称。
                  </p>
                </div>
              </div>
              <button
                type="button"
                onClick={() => void fetchProposal()}
                className="inline-flex items-center gap-1.5 rounded-xl border border-status-danger/30 bg-card px-3 py-1.5 text-xs font-medium text-status-danger-ink hover:bg-subtle transition-colors cursor-pointer"
              >
                <RefreshCw className="h-3.5 w-3.5" />
                <span>重新生成</span>
              </button>
            </div>
          )}

          {!loading && proposal && (
            <div className="space-y-5">
              {/* Title Comparison */}
              <div className={SECTION_CARD_CLASS}>
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={applyTitle}
                    onChange={(e) => setApplyTitle(e.target.checked)}
                    className={CHECKBOX_CLASS}
                  />
                  <span className="text-sm font-semibold text-ink-primary">应用建议标题</span>
                </label>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                  <div className={ORIGINAL_PANE_CLASS}>
                    <p className="text-xs text-ink-muted mb-1">当前标题</p>
                    <p className="text-ink-secondary">{course.title}</p>
                  </div>
                  <div className={SUGGESTION_PANE_CLASS}>
                    <p className="text-xs font-medium text-accent-ink mb-1">AI 建议标题</p>
                    <p className="font-medium text-ink-primary">{proposal.title}</p>
                  </div>
                </div>
              </div>

              {/* Overview Comparison */}
              <div className={SECTION_CARD_CLASS}>
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={applyOverview}
                    onChange={(e) => setApplyOverview(e.target.checked)}
                    className={CHECKBOX_CLASS}
                  />
                  <span className="text-sm font-semibold text-ink-primary">应用建议简介</span>
                </label>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                  <div className={ORIGINAL_PANE_CLASS}>
                    <p className="text-xs text-ink-muted mb-1">当前简介</p>
                    <p className="text-ink-secondary whitespace-pre-wrap">
                      {course.overview || '（暂无简介）'}
                    </p>
                  </div>
                  <div className={SUGGESTION_PANE_CLASS}>
                    <p className="text-xs font-medium text-accent-ink mb-1">AI 建议简介</p>
                    <p className="text-ink-primary whitespace-pre-wrap">
                      {proposal.overview || '（空）'}
                    </p>
                  </div>
                </div>
              </div>

              {/* Tags Comparison */}
              <div className={SECTION_CARD_CLASS}>
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={applyTags}
                    onChange={(e) => setApplyTags(e.target.checked)}
                    className={CHECKBOX_CLASS}
                  />
                  <span className="text-sm font-semibold text-ink-primary">应用建议标签</span>
                </label>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                  <div className={ORIGINAL_PANE_CLASS}>
                    <p className="text-xs text-ink-muted mb-2">当前标签</p>
                    <div className="flex flex-wrap gap-1.5">
                      {(course.tags ?? []).length > 0 ? (
                        course.tags.map((tag) => (
                          <span
                            key={tag}
                            className="rounded-md border border-default bg-card px-2 py-0.5 text-xs text-ink-secondary"
                          >
                            {tag}
                          </span>
                        ))
                      ) : (
                        <span className="text-xs text-ink-muted">无标签</span>
                      )}
                    </div>
                  </div>
                  <div className={SUGGESTION_PANE_CLASS}>
                    <p className="text-xs font-medium text-accent-ink mb-2">AI 建议标签</p>
                    <div className="flex flex-wrap gap-1.5">
                      {(proposal.tags ?? []).length > 0 ? (
                        proposal.tags.map((tag) => (
                          <span
                            key={tag}
                            className="rounded-md border border-accent-ring bg-card/85 px-2 py-0.5 text-xs font-medium text-accent-ink"
                          >
                            {tag}
                          </span>
                        ))
                      ) : (
                        <span className="text-xs text-ink-muted">无建议标签</span>
                      )}
                    </div>
                  </div>
                </div>
              </div>

              {/* Chapters & Lessons Comparison */}
              <div className={SECTION_CARD_CLASS}>
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={applyChapters}
                    onChange={(e) => setApplyChapters(e.target.checked)}
                    className={CHECKBOX_CLASS}
                  />
                  <span className="text-sm font-semibold text-ink-primary">
                    应用建议章节与课时结构
                  </span>
                </label>

                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3 text-xs">
                  <div className={`${ORIGINAL_PANE_CLASS} space-y-2.5`}>
                    <p className="text-ink-muted font-medium">当前结构</p>
                    {(course.chapters ?? []).map((ch) => (
                      <div key={ch.id} className="space-y-1">
                        <p className="font-semibold text-ink-primary">{ch.title}</p>
                        <ul className="pl-3 space-y-0.5 text-ink-secondary">
                          {(ch.lessons ?? []).map((l) => (
                            <li key={l.id} className="truncate">
                              • {l.title}
                            </li>
                          ))}
                        </ul>
                      </div>
                    ))}
                  </div>

                  <div className={`${SUGGESTION_PANE_CLASS} space-y-2.5`}>
                    <p className="text-accent-ink font-medium flex items-center gap-1">
                      <span>AI 建议结构</span>
                      <ArrowRight className="h-3 w-3" />
                    </p>
                    {(proposal.chapters ?? []).map((ch, idx) => (
                      <div key={`${ch.title}-${idx}`} className="space-y-1">
                        <p className="font-semibold text-ink-primary">{ch.title}</p>
                        <ul className="pl-3 space-y-0.5 text-ink-secondary">
                          {(ch.lessons ?? []).map((l) => (
                            <li key={l.lesson_id} className="truncate">
                              • {l.title}
                            </li>
                          ))}
                        </ul>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-subtle bg-subtle/40 px-6 py-4">
          <Tooltip content="重新调用 AI 模型生成新的课程整理方案">
            <button
              type="button"
              onClick={() => void fetchProposal()}
              disabled={loading || submitting}
              className="inline-flex items-center gap-1.5 rounded-xl border border-default bg-card px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
            >
              <RefreshCw className="h-3.5 w-3.5" />
              <span>重新生成</span>
            </button>
          </Tooltip>

          <div className="flex items-center gap-2.5">
            <button
              type="button"
              onClick={onClose}
              disabled={submitting}
              className="rounded-xl border border-default bg-card px-4 py-2 text-sm font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
            >
              取消
            </button>
            <Tooltip content="仅将已勾选的建议写入当前课程">
              <button
                type="button"
                onClick={() => void handleConfirm()}
                disabled={loading || submitting || !proposal || nothingSelected}
                className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-4 py-2 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
              >
                {submitting ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Check className="h-4 w-4" />
                )}
                <span>{submitting ? '正在应用...' : '确认应用'}</span>
              </button>
            </Tooltip>
          </div>
        </div>
      </div>
    </div>
  );
}
