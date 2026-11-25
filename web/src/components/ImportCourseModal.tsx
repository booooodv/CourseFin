import type { FormEvent } from 'react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  ArrowRight,
  Check,
  FolderInput,
  Loader2,
  Plus,
  RefreshCw,
  RotateCcw,
  Sparkles,
  Video,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import { useToast } from './Toast';
import Tooltip from './Tooltip';
import type {
  CommitImportChapter,
  CourseDetail,
  ImportOrganizeProposal,
  ImportPreview,
  Library,
  PreviewLesson,
} from '../types/coursefin';

interface ImportCourseModalProps {
  isOpen: boolean;
  sourceId: string;
  dirRelPath: string;
  libraries: Library[];
  onClose: () => void;
  onImported: (course: CourseDetail) => void;
  onLibraryCreated?: (lib: Library) => void;
}

const FIELD_INPUT_CLASS =
  'w-full rounded-xl border border-default bg-subtle/60 px-3.5 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors';
const SECTION_CARD_CLASS = 'rounded-2xl border border-subtle bg-card p-4 space-y-3 shadow-card';
const ORIGINAL_PANE_CLASS = 'rounded-xl border border-subtle bg-subtle p-3';
const SUGGESTION_PANE_CLASS = 'rounded-xl border border-accent-ring bg-accent-soft p-3';
const CHECKBOX_CLASS =
  'h-4 w-4 rounded border-default bg-card text-accent focus:ring-2 focus:ring-accent-ring';

function formatDuration(durationMs: number): string {
  if (!durationMs || durationMs <= 0) return '';
  const totalSec = Math.round(durationMs / 1000);
  const mins = Math.floor(totalSec / 60);
  const secs = totalSec % 60;
  return `${mins}:${String(secs).padStart(2, '0')}`;
}

function buildInitialChapters(data: ImportPreview): CommitImportChapter[] {
  return (data.chapters ?? []).map((ch) => ({
    title: ch.title,
    lessons: (ch.lessons ?? []).map((l) => ({
      media_file_id: l.media_file_id,
      title: l.title,
    })),
  }));
}

export default function ImportCourseModal({
  isOpen,
  sourceId,
  dirRelPath,
  libraries,
  onClose,
  onImported,
  onLibraryCreated,
}: ImportCourseModalProps) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<ImportPreview | null>(null);

  const [title, setTitle] = useState('');
  const [libraryId, setLibraryId] = useState('');
  const [instructor, setInstructor] = useState('');
  const [overview, setOverview] = useState('');
  const [tags, setTags] = useState<string[]>([]);
  const [tagInput, setTagInput] = useState('');
  const [chapters, setChapters] = useState<CommitImportChapter[]>([]);

  const [localLibraries, setLocalLibraries] = useState<Library[]>(libraries);
  const [creatingLibName, setCreatingLibName] = useState('');
  const [showNewLib, setShowNewLib] = useState(false);
  const [creatingLib, setCreatingLib] = useState(false);

  // AI organize overlay state
  const [aiModalOpen, setAiModalOpen] = useState(false);
  const [aiLoading, setAiLoading] = useState(false);
  const [aiError, setAiError] = useState<string | null>(null);
  const [aiProposal, setAiProposal] = useState<ImportOrganizeProposal | null>(null);
  const [applyTitle, setApplyTitle] = useState(true);
  const [applyOverview, setApplyOverview] = useState(true);
  const [applyTags, setApplyTags] = useState(true);
  const [applyChapters, setApplyChapters] = useState(true);
  const [aiApplied, setAiApplied] = useState(false);

  const previewLessonMap = useMemo(() => {
    const map = new Map<string, PreviewLesson>();
    if (!preview) return map;
    for (const ch of preview.chapters ?? []) {
      for (const l of ch.lessons ?? []) {
        map.set(l.media_file_id, l);
      }
    }
    return map;
  }, [preview]);

  const totalLessons = useMemo(
    () => chapters.reduce((acc, ch) => acc + ch.lessons.length, 0),
    [chapters],
  );

  useEffect(() => {
    setLocalLibraries(libraries);
    if (!libraryId && libraries.length > 0) {
      setLibraryId(libraries[0].id);
    }
  }, [libraries, libraryId]);

  const loadPreview = useCallback(async () => {
    setLoading(true);
    setError(null);
    setPreview(null);
    setAiModalOpen(false);
    setAiProposal(null);
    setAiError(null);
    setAiApplied(false);
    try {
      const data = await api.previewImport(sourceId, dirRelPath);
      setPreview(data);
      setTitle(data.course_title);
      setChapters(buildInitialChapters(data));
      setInstructor('');
      setOverview('');
      setTags([]);
      setTagInput('');
      if (!libraryId && localLibraries.length > 0) {
        setLibraryId(localLibraries[0].id);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载目录导入预览失败');
    } finally {
      setLoading(false);
    }
  }, [sourceId, dirRelPath]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!isOpen) return;
    void loadPreview();
  }, [isOpen, loadPreview]);

  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (aiModalOpen) {
        if (!aiLoading) {
          setAiModalOpen(false);
        }
        return;
      }
      if (!submitting) {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, aiModalOpen, aiLoading, submitting, onClose]);

  if (!isOpen) return null;

  async function runAIOrganize() {
    if (!preview || totalLessons === 0) return;
    setAiModalOpen(true);
    setAiLoading(true);
    setAiError(null);
    setAiProposal(null);
    try {
      const proposal = await api.aiOrganizeImport({
        source_id: preview.source_id,
        dir_rel_path: preview.dir_rel_path,
        title: title.trim() || preview.course_title,
        overview: overview.trim(),
        chapters,
      });
      setAiProposal(proposal);
      setApplyTitle(true);
      setApplyOverview(true);
      setApplyTags(true);
      setApplyChapters(true);
    } catch (err) {
      setAiError(err instanceof Error ? err.message : '生成 AI 整理建议失败');
    } finally {
      setAiLoading(false);
    }
  }

  function handleApplyAIProposal() {
    if (!aiProposal) return;
    if (applyTitle && aiProposal.title.trim()) {
      setTitle(aiProposal.title.trim());
    }
    if (applyOverview) {
      setOverview(aiProposal.overview ?? '');
    }
    if (applyTags) {
      setTags(aiProposal.tags ?? []);
    }
    if (applyChapters && (aiProposal.chapters ?? []).length > 0) {
      setChapters(
        aiProposal.chapters.map((ch) => ({
          title: ch.title,
          lessons: (ch.lessons ?? []).map((l) => ({
            media_file_id: l.media_file_id,
            title: l.title,
          })),
        })),
      );
    }
    setAiApplied(true);
    setAiModalOpen(false);
    toast.success('已将 AI 建议填入导入表单，可继续微调后确认导入');
  }

  function handleResetInitialPreview() {
    if (!preview) return;
    setTitle(preview.course_title);
    setOverview('');
    setTags([]);
    setTagInput('');
    setChapters(buildInitialChapters(preview));
    setAiApplied(false);
    toast.info('已恢复目录初始识别结构');
  }

  function handleAddTag() {
    const t = tagInput.trim();
    if (!t || tags.includes(t)) {
      setTagInput('');
      return;
    }
    setTags((prev) => [...prev, t]);
    setTagInput('');
  }

  async function handleCreateInlineLibrary() {
    const trimmed = creatingLibName.trim();
    if (!trimmed || creatingLib) return;
    setCreatingLib(true);
    setError(null);
    try {
      const created = await api.createLibrary(trimmed);
      setLocalLibraries((prev) => [...prev, created]);
      setLibraryId(created.id);
      setCreatingLibName('');
      setShowNewLib(false);
      onLibraryCreated?.(created);
      toast.success(`已创建媒体库「${created.name}」`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : '创建媒体库失败';
      setError(msg);
      toast.error(msg);
    } finally {
      setCreatingLib(false);
    }
  }

  function updateChapterTitle(chIdx: number, nextTitle: string) {
    setChapters((prev) =>
      prev.map((ch, i) => (i === chIdx ? { ...ch, title: nextTitle } : ch)),
    );
  }

  function updateLessonTitle(chIdx: number, lIdx: number, nextTitle: string) {
    setChapters((prev) =>
      prev.map((ch, i) =>
        i === chIdx
          ? {
              ...ch,
              lessons: ch.lessons.map((l, j) => (j === lIdx ? { ...l, title: nextTitle } : l)),
            }
          : ch,
      ),
    );
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!preview || submitting) return;
    if (!title.trim()) {
      setError('课程标题不能为空');
      return;
    }
    if (!libraryId) {
      setError('请选择或创建一个目标媒体库');
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const created = await api.commitImport({
        source_id: preview.source_id,
        dir_rel_path: preview.dir_rel_path,
        library_id: libraryId,
        title: title.trim(),
        overview: overview.trim(),
        instructor: instructor.trim(),
        tags,
        cover_rel_path: preview.cover_rel_path ?? '',
        chapters: chapters.map((ch) => ({
          title: ch.title.trim() || '正片',
          lessons: ch.lessons.map((l) => ({
            media_file_id: l.media_file_id,
            title: l.title.trim(),
          })),
        })),
      });
      onImported(created);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : '导入课程失败');
    } finally {
      setSubmitting(false);
    }
  }

  const nothingSelected = !applyTitle && !applyOverview && !applyTags && !applyChapters;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="import-course-modal-title"
    >
      <form
        onSubmit={handleSubmit}
        className="flex max-h-[90vh] w-full max-w-3xl flex-col overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float"
      >
        {/* Header */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-subtle px-6 py-4">
          <div className="flex items-center gap-3 min-w-0">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
              <FolderInput className="h-4 w-4" />
            </span>
            <div className="min-w-0">
              <h2 id="import-course-modal-title" className="text-base font-bold text-ink-primary">
                导入为课程
              </h2>
              <p className="text-xs text-ink-muted truncate">
                目录：{dirRelPath || '（存储根目录）'}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            {!loading && preview && totalLessons > 0 && (
              <>
                {aiApplied && (
                  <Tooltip content="放弃 AI 填充结果，还原为目录初始识别内容">
                    <button
                      type="button"
                      disabled={submitting}
                      onClick={handleResetInitialPreview}
                      className="inline-flex items-center gap-1.5 rounded-xl border border-default bg-card px-3 py-1.5 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
                    >
                      <RotateCcw className="h-3.5 w-3.5" />
                      <span>恢复初始识别</span>
                    </button>
                  </Tooltip>
                )}
                <Tooltip content="调用 AI 自动整理课程标题、简介、标签及章节课时结构">
                  <button
                    type="button"
                    disabled={submitting || aiLoading}
                    onClick={() => void runAIOrganize()}
                    className="inline-flex items-center gap-1.5 rounded-xl border border-accent-ring bg-accent-soft px-3 py-1.5 text-xs font-semibold text-accent-ink hover:bg-accent hover:text-ink-inverse disabled:opacity-50 transition-colors cursor-pointer"
                  >
                    <Sparkles className="h-3.5 w-3.5" />
                    <span>AI 智能整理</span>
                  </button>
                </Tooltip>
              </>
            )}

            <Tooltip content="关闭导入弹窗">
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
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {loading && (
            <div className="flex flex-col items-center justify-center py-16 text-ink-secondary gap-3">
              <Loader2 className="h-8 w-8 animate-spin text-accent" />
              <p className="text-sm font-medium">正在解析目录结构与视频文件...</p>
            </div>
          )}

          {error && (
            <div className="rounded-2xl border border-status-danger/30 bg-status-danger-soft p-3.5 text-sm text-status-danger-ink">
              {error}
            </div>
          )}

          {!loading && preview && (
            <>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="space-y-1.5 sm:col-span-2">
                  <label htmlFor="import-course-title" className="block text-xs font-medium text-ink-secondary">
                    课程标题
                  </label>
                  <input
                    id="import-course-title"
                    type="text"
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    className={FIELD_INPUT_CLASS}
                    required
                  />
                </div>

                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <label htmlFor="import-library-id" className="block text-xs font-medium text-ink-secondary">
                      所属媒体库
                    </label>
                    <Tooltip content="在此快速新建一个目标媒体库">
                      <button
                        type="button"
                        onClick={() => setShowNewLib((v) => !v)}
                        className="inline-flex items-center gap-1 text-[11px] font-medium text-accent-ink hover:opacity-80 cursor-pointer"
                      >
                        <Plus className="h-3 w-3" />
                        <span>新建媒体库</span>
                      </button>
                    </Tooltip>
                  </div>
                  <select
                    id="import-library-id"
                    value={libraryId}
                    onChange={(e) => setLibraryId(e.target.value)}
                    className={FIELD_INPUT_CLASS}
                  >
                    {localLibraries.length === 0 && <option value="">请先创建媒体库</option>}
                    {localLibraries.map((lib) => (
                      <option key={lib.id} value={lib.id}>
                        {lib.name}
                      </option>
                    ))}
                  </select>

                  {showNewLib && (
                    <div className="flex items-center gap-2 pt-1.5">
                      <input
                        type="text"
                        value={creatingLibName}
                        onChange={(e) => setCreatingLibName(e.target.value)}
                        placeholder="新媒体库名称"
                        className="flex-1 rounded-xl border border-default bg-subtle/60 px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
                      />
                      <button
                        type="button"
                        onClick={() => void handleCreateInlineLibrary()}
                        disabled={creatingLib}
                        className="rounded-xl bg-accent px-3 py-1.5 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
                      >
                        创建
                      </button>
                    </div>
                  )}
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="import-instructor" className="block text-xs font-medium text-ink-secondary">
                    讲师（可选）
                  </label>
                  <input
                    id="import-instructor"
                    type="text"
                    value={instructor}
                    onChange={(e) => setInstructor(e.target.value)}
                    placeholder="例如：张老师"
                    className={FIELD_INPUT_CLASS}
                  />
                </div>

                <div className="space-y-1.5 sm:col-span-2">
                  <label htmlFor="import-overview" className="block text-xs font-medium text-ink-secondary">
                    课程简介（可选）
                  </label>
                  <textarea
                    id="import-overview"
                    rows={2}
                    value={overview}
                    onChange={(e) => setOverview(e.target.value)}
                    placeholder="课程介绍或学习目标..."
                    className={FIELD_INPUT_CLASS}
                  />
                </div>

                <div className="space-y-1.5 sm:col-span-2">
                  <label htmlFor="import-tag-input" className="block text-xs font-medium text-ink-secondary">
                    标签（可选）
                  </label>
                  {tags.length > 0 && (
                    <div className="flex flex-wrap items-center gap-1.5 mb-1.5">
                      {tags.map((t) => (
                        <span
                          key={t}
                          className="inline-flex items-center gap-1 rounded-full border border-accent-ring bg-accent-soft px-2.5 py-0.5 text-xs font-medium text-accent-ink"
                        >
                          {t}
                          <button
                            type="button"
                            aria-label={`移除标签 ${t}`}
                            onClick={() => setTags((prev) => prev.filter((item) => item !== t))}
                            className="hover:opacity-75 cursor-pointer"
                          >
                            <X className="h-3 w-3" />
                          </button>
                        </span>
                      ))}
                    </div>
                  )}
                  <div className="flex gap-2">
                    <input
                      id="import-tag-input"
                      type="text"
                      value={tagInput}
                      onChange={(e) => setTagInput(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          handleAddTag();
                        }
                      }}
                      placeholder="输入标签后按回车或点击添加"
                      className="flex-1 rounded-xl border border-default bg-subtle/60 px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
                    />
                    <Tooltip content="将输入的标签添加到课程中">
                      <button
                        type="button"
                        onClick={handleAddTag}
                        className="rounded-xl border border-default bg-subtle px-3 py-1.5 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors cursor-pointer"
                      >
                        添加标签
                      </button>
                    </Tooltip>
                  </div>
                </div>
              </div>

              {/* Inferred Chapters & Lessons */}
              <div className="space-y-3">
                <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-ink-muted">
                  <span className="font-semibold text-ink-primary">
                    识别到的章节与课时（共 {preview.total_files} 个视频）
                  </span>
                  {preview.cover_rel_path && (
                    <span className="font-medium text-status-success-ink">
                      已自动检测封面：{preview.cover_rel_path}
                    </span>
                  )}
                </div>

                <div className="space-y-3">
                  {chapters.map((ch, chIdx) => (
                    <div
                      key={`ch-${chIdx}`}
                      className="rounded-2xl border border-subtle bg-subtle p-4 space-y-2.5"
                    >
                      <div className="flex items-center gap-2">
                        <span className="text-xs font-medium text-ink-muted shrink-0">
                          第 {chIdx + 1} 章
                        </span>
                        <input
                          type="text"
                          aria-label={`第 ${chIdx + 1} 章标题`}
                          value={ch.title}
                          onChange={(e) => updateChapterTitle(chIdx, e.target.value)}
                          className="flex-1 rounded-xl border border-default bg-card px-3 py-1.5 text-sm font-semibold text-ink-primary focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
                        />
                      </div>

                      <div className="space-y-1.5 pl-4">
                        {ch.lessons.map((lesson, lIdx) => {
                          const orig = previewLessonMap.get(lesson.media_file_id);
                          const durationText = orig ? formatDuration(orig.duration_ms) : '';
                          return (
                            <div key={lesson.media_file_id} className="flex items-center gap-2">
                              <Video className="h-3.5 w-3.5 text-ink-muted shrink-0" />
                              <input
                                type="text"
                                aria-label={`课时 ${lIdx + 1} 标题`}
                                value={lesson.title}
                                onChange={(e) => updateLessonTitle(chIdx, lIdx, e.target.value)}
                                className="flex-1 rounded-xl border border-subtle bg-card px-2.5 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
                              />
                              {durationText && (
                                <span className="text-[11px] tabular-nums text-ink-muted shrink-0">
                                  {durationText}
                                </span>
                              )}
                            </div>
                          );
                        })}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2.5 border-t border-subtle bg-subtle/40 px-6 py-4">
          <button
            type="button"
            onClick={onClose}
            disabled={submitting}
            className="rounded-xl border border-default bg-card px-4 py-2 text-sm font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
          >
            取消
          </button>
          <Tooltip content="按上方结构创建课程（不会改动磁盘文件）">
            <button
              type="submit"
              disabled={loading || submitting || !preview}
              className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-4 py-2 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
            >
              {submitting ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Check className="h-4 w-4" />
              )}
              <span>{submitting ? '正在导入...' : '确认导入'}</span>
            </button>
          </Tooltip>
        </div>
      </form>

      {/* Nested AI Organize Preview Overlay */}
      {aiModalOpen && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby="import-ai-modal-title"
        >
          <div className="flex max-h-[90vh] w-full max-w-4xl flex-col overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float">
            <div className="flex items-center justify-between border-b border-subtle px-6 py-4">
              <div className="flex items-center gap-3">
                <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
                  <Sparkles className="h-4 w-4" />
                </span>
                <div>
                  <h3 id="import-ai-modal-title" className="text-base font-bold text-ink-primary">
                    AI 智能整理预览（导入前）
                  </h3>
                  <p className="text-xs text-ink-muted">
                    确认后将回填到当前导入表单，您可以继续微调后再点击「确认导入」。
                  </p>
                </div>
              </div>

              <button
                type="button"
                aria-label="关闭 AI 整理预览"
                disabled={aiLoading}
                onClick={() => setAiModalOpen(false)}
                className="rounded-lg p-1.5 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors disabled:opacity-50 cursor-pointer"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto p-6 space-y-5">
              {aiLoading && (
                <div className="flex flex-col items-center justify-center py-16 text-ink-secondary gap-3">
                  <Loader2 className="h-8 w-8 animate-spin text-accent" />
                  <p className="text-sm font-medium">正在分析目录结构与字幕内容，生成导入整理方案...</p>
                </div>
              )}

              {aiError && (
                <div className="rounded-2xl border border-status-danger/30 bg-status-danger-soft p-4 space-y-3">
                  <div className="flex items-start gap-2.5 text-status-danger-ink text-sm">
                    <AlertTriangle className="h-5 w-5 shrink-0 text-status-danger-ink mt-0.5" />
                    <div className="space-y-1">
                      <p className="font-medium">{aiError}</p>
                      <p className="text-xs text-status-danger-ink/85">
                        请确认已在{' '}
                        <Link
                          to="/settings"
                          className="underline font-semibold hover:opacity-80"
                          onClick={() => {
                            setAiModalOpen(false);
                            onClose();
                          }}
                        >
                          系统设置
                        </Link>{' '}
                        中配置有效的 AI Base URL 与模型名称。
                      </p>
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => void runAIOrganize()}
                    className="inline-flex items-center gap-1.5 rounded-xl border border-status-danger/30 bg-card px-3 py-1.5 text-xs font-medium text-status-danger-ink hover:bg-subtle transition-colors cursor-pointer"
                  >
                    <RefreshCw className="h-3.5 w-3.5" />
                    <span>重新生成</span>
                  </button>
                </div>
              )}

              {!aiLoading && aiProposal && (
                <div className="space-y-5">
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
                        <p className="text-ink-secondary">{title}</p>
                      </div>
                      <div className={SUGGESTION_PANE_CLASS}>
                        <p className="text-xs font-medium text-accent-ink mb-1">AI 建议标题</p>
                        <p className="font-medium text-ink-primary">{aiProposal.title}</p>
                      </div>
                    </div>
                  </div>

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
                          {overview || '（暂无简介）'}
                        </p>
                      </div>
                      <div className={SUGGESTION_PANE_CLASS}>
                        <p className="text-xs font-medium text-accent-ink mb-1">AI 建议简介</p>
                        <p className="text-ink-primary whitespace-pre-wrap">
                          {aiProposal.overview || '（空）'}
                        </p>
                      </div>
                    </div>
                  </div>

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
                          {tags.length > 0 ? (
                            tags.map((tag) => (
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
                          {(aiProposal.tags ?? []).length > 0 ? (
                            aiProposal.tags.map((tag) => (
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
                        {chapters.map((ch, idx) => (
                          <div key={`cur-${idx}`} className="space-y-1">
                            <p className="font-semibold text-ink-primary">{ch.title}</p>
                            <ul className="pl-3 space-y-0.5 text-ink-secondary">
                              {ch.lessons.map((l) => (
                                <li key={l.media_file_id} className="truncate">
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
                        {(aiProposal.chapters ?? []).map((ch, idx) => (
                          <div key={`ai-${ch.title}-${idx}`} className="space-y-1">
                            <p className="font-semibold text-ink-primary">{ch.title}</p>
                            <ul className="pl-3 space-y-0.5 text-ink-secondary">
                              {(ch.lessons ?? []).map((l) => (
                                <li key={l.media_file_id} className="truncate">
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

            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-subtle bg-subtle/40 px-6 py-4">
              <button
                type="button"
                onClick={() => void runAIOrganize()}
                disabled={aiLoading}
                className="inline-flex items-center gap-1.5 rounded-xl border border-default bg-card px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
              >
                <RefreshCw className="h-3.5 w-3.5" />
                <span>重新生成</span>
              </button>

              <div className="flex items-center gap-2.5">
                <button
                  type="button"
                  onClick={() => setAiModalOpen(false)}
                  disabled={aiLoading}
                  className="rounded-xl border border-default bg-card px-4 py-2 text-sm font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
                >
                  取消
                </button>
                <button
                  type="button"
                  onClick={handleApplyAIProposal}
                  disabled={aiLoading || !aiProposal || nothingSelected}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-4 py-2 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
                >
                  <Check className="h-4 w-4" />
                  <span>应用到导入表单</span>
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
