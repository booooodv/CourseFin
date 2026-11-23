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
import type { CourseDetail, CourseOrganizeProposal } from '../types/coursefin';

interface AIOrganizeModalProps {
  course: CourseDetail;
  isOpen: boolean;
  onClose: () => void;
  onApplied: (updatedCourse: CourseDetail) => void;
}

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
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/80 backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="ai-organize-modal-title"
    >
      <div className="flex max-h-[90vh] w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-slate-800 bg-slate-900 shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-slate-800 px-6 py-4">
          <div className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-indigo-500/15 text-indigo-400">
              <Sparkles className="h-4 w-4" />
            </span>
            <div>
              <h2 id="ai-organize-modal-title" className="text-base font-bold text-white">
                AI 智能整理预览
              </h2>
              <p className="text-xs text-slate-400">
                以下建议仅作预览，只有您勾选并点击“确认应用”的项目才会写入课程数据。
              </p>
            </div>
          </div>

          <button
            type="button"
            aria-label="关闭"
            onClick={onClose}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white transition-colors"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {loading && (
            <div className="flex flex-col items-center justify-center py-16 text-slate-300 gap-3">
              <Loader2 className="h-8 w-8 animate-spin text-indigo-400" />
              <p className="text-sm font-medium">正在分析课程目录与字幕内容，生成整理方案...</p>
            </div>
          )}

          {error && (
            <div className="rounded-xl border border-rose-500/30 bg-rose-500/10 p-4 space-y-3">
              <div className="flex items-start gap-2.5 text-rose-200 text-sm">
                <AlertTriangle className="h-5 w-5 shrink-0 text-rose-400 mt-0.5" />
                <div className="space-y-1">
                  <p className="font-medium">{error}</p>
                  <p className="text-xs text-rose-300/80">
                    请确认已在{' '}
                    <Link to="/settings" className="underline hover:text-white" onClick={onClose}>
                      系统设置
                    </Link>{' '}
                    中配置有效的 AI Base URL 与模型名称。
                  </p>
                </div>
              </div>
              <button
                type="button"
                onClick={() => void fetchProposal()}
                className="inline-flex items-center gap-1.5 rounded-xl border border-rose-400/30 bg-rose-500/20 px-3 py-1.5 text-xs font-medium text-rose-100 hover:bg-rose-500/30 transition-colors"
              >
                <RefreshCw className="h-3.5 w-3.5" />
                <span>重新生成</span>
              </button>
            </div>
          )}

          {!loading && proposal && (
            <div className="space-y-5">
              {/* Title Comparison */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 space-y-3">
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={applyTitle}
                    onChange={(e) => setApplyTitle(e.target.checked)}
                    className="h-4 w-4 rounded border-slate-600 bg-slate-800 text-indigo-500 focus:ring-indigo-500"
                  />
                  <span className="text-sm font-semibold text-white">应用建议标题</span>
                </label>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                  <div className="rounded-lg border border-slate-800/80 bg-slate-900/70 p-3">
                    <p className="text-xs text-slate-500 mb-1">当前标题</p>
                    <p className="text-slate-300">{course.title}</p>
                  </div>
                  <div className="rounded-lg border border-indigo-500/30 bg-indigo-500/5 p-3">
                    <p className="text-xs text-indigo-300 mb-1">AI 建议标题</p>
                    <p className="font-medium text-white">{proposal.title}</p>
                  </div>
                </div>
              </div>

              {/* Overview Comparison */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 space-y-3">
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={applyOverview}
                    onChange={(e) => setApplyOverview(e.target.checked)}
                    className="h-4 w-4 rounded border-slate-600 bg-slate-800 text-indigo-500 focus:ring-indigo-500"
                  />
                  <span className="text-sm font-semibold text-white">应用建议简介</span>
                </label>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                  <div className="rounded-lg border border-slate-800/80 bg-slate-900/70 p-3">
                    <p className="text-xs text-slate-500 mb-1">当前简介</p>
                    <p className="text-slate-300 whitespace-pre-wrap">
                      {course.overview || '（暂无简介）'}
                    </p>
                  </div>
                  <div className="rounded-lg border border-indigo-500/30 bg-indigo-500/5 p-3">
                    <p className="text-xs text-indigo-300 mb-1">AI 建议简介</p>
                    <p className="text-slate-100 whitespace-pre-wrap">
                      {proposal.overview || '（空）'}
                    </p>
                  </div>
                </div>
              </div>

              {/* Tags Comparison */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 space-y-3">
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={applyTags}
                    onChange={(e) => setApplyTags(e.target.checked)}
                    className="h-4 w-4 rounded border-slate-600 bg-slate-800 text-indigo-500 focus:ring-indigo-500"
                  />
                  <span className="text-sm font-semibold text-white">应用建议标签</span>
                </label>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                  <div className="rounded-lg border border-slate-800/80 bg-slate-900/70 p-3">
                    <p className="text-xs text-slate-500 mb-2">当前标签</p>
                    <div className="flex flex-wrap gap-1.5">
                      {(course.tags ?? []).length > 0 ? (
                        course.tags.map((tag) => (
                          <span
                            key={tag}
                            className="rounded-md border border-slate-700 bg-slate-800 px-2 py-0.5 text-xs text-slate-300"
                          >
                            {tag}
                          </span>
                        ))
                      ) : (
                        <span className="text-xs text-slate-500">无标签</span>
                      )}
                    </div>
                  </div>
                  <div className="rounded-lg border border-indigo-500/30 bg-indigo-500/5 p-3">
                    <p className="text-xs text-indigo-300 mb-2">AI 建议标签</p>
                    <div className="flex flex-wrap gap-1.5">
                      {(proposal.tags ?? []).length > 0 ? (
                        proposal.tags.map((tag) => (
                          <span
                            key={tag}
                            className="rounded-md border border-indigo-500/40 bg-indigo-500/15 px-2 py-0.5 text-xs text-indigo-200"
                          >
                            {tag}
                          </span>
                        ))
                      ) : (
                        <span className="text-xs text-slate-500">无建议标签</span>
                      )}
                    </div>
                  </div>
                </div>
              </div>

              {/* Chapters & Lessons Comparison */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 space-y-3">
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={applyChapters}
                    onChange={(e) => setApplyChapters(e.target.checked)}
                    className="h-4 w-4 rounded border-slate-600 bg-slate-800 text-indigo-500 focus:ring-indigo-500"
                  />
                  <span className="text-sm font-semibold text-white">应用建议章节与课时结构</span>
                </label>

                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3 text-xs">
                  <div className="rounded-lg border border-slate-800/80 bg-slate-900/70 p-3 space-y-2.5">
                    <p className="text-slate-500 font-medium">当前结构</p>
                    {(course.chapters ?? []).map((ch) => (
                      <div key={ch.id} className="space-y-1">
                        <p className="font-semibold text-slate-200">{ch.title}</p>
                        <ul className="pl-3 space-y-0.5 text-slate-400">
                          {(ch.lessons ?? []).map((l) => (
                            <li key={l.id} className="truncate">
                              • {l.title}
                            </li>
                          ))}
                        </ul>
                      </div>
                    ))}
                  </div>

                  <div className="rounded-lg border border-indigo-500/30 bg-indigo-500/5 p-3 space-y-2.5">
                    <p className="text-indigo-300 font-medium flex items-center gap-1">
                      <span>AI 建议结构</span>
                      <ArrowRight className="h-3 w-3" />
                    </p>
                    {(proposal.chapters ?? []).map((ch, idx) => (
                      <div key={`${ch.title}-${idx}`} className="space-y-1">
                        <p className="font-semibold text-white">{ch.title}</p>
                        <ul className="pl-3 space-y-0.5 text-indigo-200/90">
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
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-800 px-6 py-4 bg-slate-900/95">
          <button
            type="button"
            onClick={() => void fetchProposal()}
            disabled={loading || submitting}
            className="inline-flex items-center gap-1.5 rounded-xl border border-slate-700 px-3.5 py-2 text-xs font-medium text-slate-300 hover:bg-slate-800 hover:text-white disabled:opacity-50 transition-colors"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            <span>重新生成</span>
          </button>

          <div className="flex items-center gap-2.5">
            <button
              type="button"
              onClick={onClose}
              disabled={submitting}
              className="rounded-xl border border-slate-700 px-4 py-2 text-sm font-medium text-slate-300 hover:bg-slate-800 transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => void handleConfirm()}
              disabled={loading || submitting || !proposal || nothingSelected}
              className="inline-flex items-center gap-1.5 rounded-xl bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors"
            >
              {submitting ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Check className="h-4 w-4" />
              )}
              <span>{submitting ? '正在应用...' : '确认应用'}</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
