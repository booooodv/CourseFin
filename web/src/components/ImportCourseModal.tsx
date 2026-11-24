import type { FormEvent } from 'react';
import { useCallback, useEffect, useState } from 'react';
import {
  Check,
  FolderInput,
  Loader2,
  Plus,
  Video,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import { useToast } from './Toast';
import Tooltip from './Tooltip';
import type {
  CommitImportChapter,
  CourseDetail,
  ImportPreview,
  Library,
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

function formatDuration(durationMs: number): string {
  if (!durationMs || durationMs <= 0) return '';
  const totalSec = Math.round(durationMs / 1000);
  const mins = Math.floor(totalSec / 60);
  const secs = totalSec % 60;
  return `${mins}:${String(secs).padStart(2, '0')}`;
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
    try {
      const data = await api.previewImport(sourceId, dirRelPath);
      setPreview(data);
      setTitle(data.course_title);
      setChapters(
        (data.chapters ?? []).map((ch) => ({
          title: ch.title,
          lessons: (ch.lessons ?? []).map((l) => ({
            media_file_id: l.media_file_id,
            title: l.title,
          })),
        })),
      );
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

  if (!isOpen) return null;

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

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/80 backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="import-course-modal-title"
    >
      <form
        onSubmit={handleSubmit}
        className="flex max-h-[90vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl border border-slate-800 bg-slate-900 shadow-2xl"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-slate-800 px-6 py-4">
          <div className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-indigo-500/15 text-indigo-400">
              <FolderInput className="h-4 w-4" />
            </span>
            <div>
              <h2 id="import-course-modal-title" className="text-base font-bold text-white">
                导入为课程
              </h2>
              <p className="text-xs text-slate-400">
                目录：{dirRelPath || '（存储根目录）'}
              </p>
            </div>
          </div>

          <Tooltip content="关闭导入弹窗">
            <button
              type="button"
              aria-label="关闭"
              onClick={onClose}
              className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white transition-colors"
            >
              <X className="h-5 w-5" />
            </button>
          </Tooltip>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {loading && (
            <div className="flex flex-col items-center justify-center py-16 text-slate-300 gap-3">
              <Loader2 className="h-8 w-8 animate-spin text-indigo-400" />
              <p className="text-sm">正在解析目录结构与视频文件...</p>
            </div>
          )}

          {error && (
            <div className="rounded-xl border border-rose-500/30 bg-rose-500/10 p-3.5 text-sm text-rose-200">
              {error}
            </div>
          )}

          {!loading && preview && (
            <>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="space-y-1.5 sm:col-span-2">
                  <label htmlFor="import-course-title" className="block text-xs font-medium text-slate-300">
                    课程标题
                  </label>
                  <input
                    id="import-course-title"
                    type="text"
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white focus:border-indigo-500 focus:outline-none"
                    required
                  />
                </div>

                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <label htmlFor="import-library-id" className="block text-xs font-medium text-slate-300">
                      所属媒体库
                    </label>
                    <Tooltip content="在此快速新建一个目标媒体库">
                      <button
                        type="button"
                        onClick={() => setShowNewLib((v) => !v)}
                        className="inline-flex items-center gap-1 text-[11px] text-indigo-300 hover:text-indigo-200"
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
                    className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white focus:border-indigo-500 focus:outline-none"
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
                        className="flex-1 rounded-lg border border-slate-700 bg-slate-950 px-2.5 py-1.5 text-xs text-white focus:border-indigo-500 focus:outline-none"
                      />
                      <button
                        type="button"
                        onClick={() => void handleCreateInlineLibrary()}
                        disabled={creatingLib}
                        className="rounded-lg bg-indigo-600 px-2.5 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
                      >
                        创建
                      </button>
                    </div>
                  )}
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="import-instructor" className="block text-xs font-medium text-slate-300">
                    讲师（可选）
                  </label>
                  <input
                    id="import-instructor"
                    type="text"
                    value={instructor}
                    onChange={(e) => setInstructor(e.target.value)}
                    placeholder="例如：张老师"
                    className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white focus:border-indigo-500 focus:outline-none"
                  />
                </div>

                <div className="space-y-1.5 sm:col-span-2">
                  <label htmlFor="import-overview" className="block text-xs font-medium text-slate-300">
                    课程简介（可选）
                  </label>
                  <textarea
                    id="import-overview"
                    rows={2}
                    value={overview}
                    onChange={(e) => setOverview(e.target.value)}
                    placeholder="课程介绍或学习目标..."
                    className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white focus:border-indigo-500 focus:outline-none"
                  />
                </div>

                <div className="space-y-1.5 sm:col-span-2">
                  <label htmlFor="import-tag-input" className="block text-xs font-medium text-slate-300">
                    标签（可选）
                  </label>
                  <div className="flex flex-wrap items-center gap-1.5 mb-1.5">
                    {tags.map((t) => (
                      <span
                        key={t}
                        className="inline-flex items-center gap-1 rounded-full border border-indigo-500/30 bg-indigo-500/15 px-2.5 py-0.5 text-xs text-indigo-200"
                      >
                        {t}
                        <button
                          type="button"
                          aria-label={`移除标签 ${t}`}
                          onClick={() => setTags((prev) => prev.filter((item) => item !== t))}
                          className="hover:text-white"
                        >
                          <X className="h-3 w-3" />
                        </button>
                      </span>
                    ))}
                  </div>
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
                      className="flex-1 rounded-xl border border-slate-700 bg-slate-950 px-3 py-1.5 text-xs text-white focus:border-indigo-500 focus:outline-none"
                    />
                    <Tooltip content="将输入的标签添加到课程中">
                      <button
                        type="button"
                        onClick={handleAddTag}
                        className="rounded-xl border border-slate-700 bg-slate-800 px-3 py-1.5 text-xs text-slate-200 hover:bg-slate-700"
                      >
                        添加标签
                      </button>
                    </Tooltip>
                  </div>
                </div>
              </div>

              {/* Inferred Chapters & Lessons */}
              <div className="space-y-3">
                <div className="flex items-center justify-between text-xs text-slate-400">
                  <span className="font-semibold text-slate-200">
                    识别到的章节与课时（共 {preview.total_files} 个视频）
                  </span>
                  {preview.cover_rel_path && (
                    <span className="text-emerald-300">
                      已自动检测封面：{preview.cover_rel_path}
                    </span>
                  )}
                </div>

                <div className="space-y-3">
                  {chapters.map((ch, chIdx) => {
                    const previewChapter = preview.chapters[chIdx];
                    return (
                      <div
                        key={`ch-${chIdx}`}
                        className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-2.5"
                      >
                        <div className="flex items-center gap-2">
                          <span className="text-xs text-slate-500 shrink-0">第 {chIdx + 1} 章</span>
                          <input
                            type="text"
                            aria-label={`第 ${chIdx + 1} 章标题`}
                            value={ch.title}
                            onChange={(e) => updateChapterTitle(chIdx, e.target.value)}
                            className="flex-1 rounded-lg border border-slate-700 bg-slate-900 px-2.5 py-1 text-sm font-semibold text-white focus:border-indigo-500 focus:outline-none"
                          />
                        </div>

                        <div className="space-y-1.5 pl-4">
                          {ch.lessons.map((lesson, lIdx) => {
                            const orig = previewChapter?.lessons[lIdx];
                            const durationText = orig ? formatDuration(orig.duration_ms) : '';
                            return (
                              <div key={lesson.media_file_id} className="flex items-center gap-2">
                                <Video className="h-3.5 w-3.5 text-slate-500 shrink-0" />
                                <input
                                  type="text"
                                  aria-label={`课时 ${lIdx + 1} 标题`}
                                  value={lesson.title}
                                  onChange={(e) => updateLessonTitle(chIdx, lIdx, e.target.value)}
                                  className="flex-1 rounded-lg border border-slate-800 bg-slate-900/80 px-2.5 py-1 text-xs text-slate-200 focus:border-indigo-500 focus:outline-none"
                                />
                                {durationText && (
                                  <span className="text-[11px] tabular-nums text-slate-500 shrink-0">
                                    {durationText}
                                  </span>
                                )}
                              </div>
                            );
                          })}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2.5 border-t border-slate-800 px-6 py-4 bg-slate-900/95">
          <button
            type="button"
            onClick={onClose}
            disabled={submitting}
            className="rounded-xl border border-slate-700 px-4 py-2 text-sm font-medium text-slate-300 hover:bg-slate-800 transition-colors"
          >
            取消
          </button>
          <Tooltip content="按上方结构创建课程（不会改动磁盘文件）">
            <button
              type="submit"
              disabled={loading || submitting || !preview}
              className="inline-flex items-center gap-1.5 rounded-xl bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors"
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
    </div>
  );
}
