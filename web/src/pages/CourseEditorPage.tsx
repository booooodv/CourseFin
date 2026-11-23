import type { ChangeEvent, FormEvent } from 'react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core';
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Check,
  FilePlus2,
  FolderSearch,
  GripVertical,
  ImagePlus,
  Loader2,
  Pencil,
  Plus,
  Save,
  Sparkles,
  Subtitles,
  Trash2,
  Upload,
  Video,
  X,
} from 'lucide-react';
import {
  api,
  itemBackdropImageUrl,
  itemPrimaryImageUrl,
} from '../api/client';
import AIOrganizeModal from '../components/AIOrganizeModal';
import type {
  ChapterDetail,
  CourseDetail,
  LessonDetail,
  LessonPlacement,
  Library,
  MediaFile,
  StorageSource,
} from '../types/coursefin';

function formatDuration(durationMs: number): string {
  if (!durationMs || durationMs <= 0) return '';
  const totalSec = Math.round(durationMs / 1000);
  const mins = Math.floor(totalSec / 60);
  const secs = totalSec % 60;
  return `${mins}:${String(secs).padStart(2, '0')}`;
}

function buildLessonPlacements(chapters: ChapterDetail[]): LessonPlacement[] {
  const placements: LessonPlacement[] = [];
  for (const ch of chapters) {
    (ch.lessons ?? []).forEach((lesson, idx) => {
      placements.push({
        lesson_id: lesson.id,
        chapter_id: ch.id,
        sort_order: idx + 1,
      });
    });
  }
  return placements;
}

interface SortableLessonRowProps {
  lesson: LessonDetail;
  chapterId: string;
  chapters: ChapterDetail[];
  lessonIdx: number;
  totalLessonsInChapter: number;
  onRename: (lesson: LessonDetail, nextTitle: string) => Promise<void>;
  onDelete: (lessonId: string) => Promise<void>;
  onMoveStep: (chapterId: string, lessonIdx: number, delta: -1 | 1) => Promise<void>;
  onMoveToChapter: (lessonId: string, targetChapterId: string) => Promise<void>;
}

function SortableLessonRow({
  lesson,
  chapterId,
  chapters,
  lessonIdx,
  totalLessonsInChapter,
  onRename,
  onDelete,
  onMoveStep,
  onMoveToChapter,
}: SortableLessonRowProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: `lesson-${lesson.id}` });

  const [editing, setEditing] = useState(false);
  const [draftTitle, setDraftTitle] = useState(lesson.title);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setDraftTitle(lesson.title);
  }, [lesson.title]);

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  async function saveTitle() {
    const trimmed = draftTitle.trim();
    if (!trimmed || trimmed === lesson.title) {
      setDraftTitle(lesson.title);
      setEditing(false);
      return;
    }
    setSaving(true);
    try {
      await onRename(lesson, trimmed);
      setEditing(false);
    } finally {
      setSaving(false);
    }
  }

  const subtitles = lesson.subtitles ?? [];

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`rounded-xl border px-3 py-2.5 transition-colors ${
        isDragging
          ? 'border-indigo-500 bg-indigo-500/10 shadow-lg'
          : 'border-slate-800/90 bg-slate-950/60 hover:border-slate-700'
      }`}
    >
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          aria-label={`拖拽排序课时 ${lesson.title}`}
          className="cursor-grab text-slate-500 hover:text-slate-300 p-1"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="h-4 w-4" />
        </button>

        <Video className="h-4 w-4 text-indigo-400 shrink-0" />

        {editing ? (
          <div className="flex flex-1 items-center gap-2 min-w-[180px]">
            <input
              type="text"
              aria-label={`编辑课时标题 ${lesson.title}`}
              value={draftTitle}
              onChange={(e) => setDraftTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  void saveTitle();
                } else if (e.key === 'Escape') {
                  setDraftTitle(lesson.title);
                  setEditing(false);
                }
              }}
              className="flex-1 rounded-lg border border-indigo-500/60 bg-slate-900 px-2.5 py-1 text-xs text-white focus:outline-none"
            />
            <button
              type="button"
              disabled={saving}
              onClick={() => void saveTitle()}
              className="rounded-lg bg-indigo-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-indigo-500"
            >
              保存
            </button>
            <button
              type="button"
              onClick={() => {
                setDraftTitle(lesson.title);
                setEditing(false);
              }}
              className="rounded-lg border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
            >
              取消
            </button>
          </div>
        ) : (
          <div className="flex-1 min-w-0">
            <p className="text-xs sm:text-sm font-medium text-slate-100 truncate">
              {lesson.title}
            </p>
            <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-slate-400">
              {lesson.duration_ms > 0 && <span>{formatDuration(lesson.duration_ms)}</span>}
              {lesson.video_codec && <span>{lesson.video_codec.toUpperCase()}</span>}
              {lesson.media_status === 'missing' && (
                <span className="rounded bg-rose-500/20 border border-rose-500/30 px-1.5 py-0.5 text-rose-300">
                  文件缺失
                </span>
              )}
              {subtitles.length > 0 && (
                <span className="inline-flex items-center gap-1 text-sky-300">
                  <Subtitles className="h-3 w-3" />
                  {subtitles.map((sub) => (
                    <span
                      key={sub.id}
                      className={`rounded border px-1.5 py-0.5 ${
                        sub.status === 'missing'
                          ? 'border-rose-500/30 bg-rose-500/10 text-rose-300'
                          : 'border-sky-500/30 bg-sky-500/10 text-sky-200'
                      }`}
                    >
                      {sub.title || sub.language || '字幕'} ({sub.format})
                    </span>
                  ))}
                </span>
              )}
            </div>
          </div>
        )}

        <div className="flex items-center gap-1 shrink-0">
          {chapters.length > 1 && (
            <select
              aria-label={`移动课时 ${lesson.title} 到章节`}
              value={chapterId}
              onChange={(e) => void onMoveToChapter(lesson.id, e.target.value)}
              className="rounded-lg border border-slate-800 bg-slate-900 px-2 py-1 text-[11px] text-slate-300 focus:border-indigo-500 focus:outline-none"
            >
              {chapters.map((ch) => (
                <option key={ch.id} value={ch.id}>
                  {ch.title}
                </option>
              ))}
            </select>
          )}

          <button
            type="button"
            aria-label={`上移课时 ${lesson.title}`}
            disabled={lessonIdx === 0}
            onClick={() => void onMoveStep(chapterId, lessonIdx, -1)}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white disabled:opacity-30 disabled:pointer-events-none"
          >
            <ArrowUp className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            aria-label={`下移课时 ${lesson.title}`}
            disabled={lessonIdx === totalLessonsInChapter - 1}
            onClick={() => void onMoveStep(chapterId, lessonIdx, 1)}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white disabled:opacity-30 disabled:pointer-events-none"
          >
            <ArrowDown className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            aria-label={`重命名课时 ${lesson.title}`}
            onClick={() => setEditing(true)}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white"
          >
            <Pencil className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            aria-label={`删除课时 ${lesson.title}`}
            onClick={() => void onDelete(lesson.id)}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-rose-500/20 hover:text-rose-300"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
    </div>
  );
}

interface SortableChapterCardProps {
  chapter: ChapterDetail;
  chapters: ChapterDetail[];
  chapterIdx: number;
  totalChapters: number;
  onRenameChapter: (chapter: ChapterDetail, nextTitle: string) => Promise<void>;
  onDeleteChapter: (chapterId: string) => Promise<void>;
  onMoveChapterStep: (chapterIdx: number, delta: -1 | 1) => Promise<void>;
  onOpenAddVideo: (chapterId: string) => void;
  onRenameLesson: (lesson: LessonDetail, nextTitle: string) => Promise<void>;
  onDeleteLesson: (lessonId: string) => Promise<void>;
  onMoveLessonStep: (chapterId: string, lessonIdx: number, delta: -1 | 1) => Promise<void>;
  onMoveLessonToChapter: (lessonId: string, targetChapterId: string) => Promise<void>;
}

function SortableChapterCard({
  chapter,
  chapters,
  chapterIdx,
  totalChapters,
  onRenameChapter,
  onDeleteChapter,
  onMoveChapterStep,
  onOpenAddVideo,
  onRenameLesson,
  onDeleteLesson,
  onMoveLessonStep,
  onMoveLessonToChapter,
}: SortableChapterCardProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: `chapter-${chapter.id}` });

  const [editing, setEditing] = useState(false);
  const [draftTitle, setDraftTitle] = useState(chapter.title);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setDraftTitle(chapter.title);
  }, [chapter.title]);

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  async function saveChapterTitle() {
    const trimmed = draftTitle.trim();
    if (!trimmed || trimmed === chapter.title) {
      setDraftTitle(chapter.title);
      setEditing(false);
      return;
    }
    setSaving(true);
    try {
      await onRenameChapter(chapter, trimmed);
      setEditing(false);
    } finally {
      setSaving(false);
    }
  }

  const lessons = chapter.lessons ?? [];

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`rounded-2xl border p-4 space-y-3 transition-colors ${
        isDragging
          ? 'border-indigo-500 bg-slate-900 shadow-xl'
          : 'border-slate-800 bg-slate-900/75'
      }`}
    >
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-800/80 pb-3">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <button
            type="button"
            aria-label={`拖拽排序章节 ${chapter.title}`}
            className="cursor-grab text-slate-500 hover:text-slate-300 p-1"
            {...attributes}
            {...listeners}
          >
            <GripVertical className="h-4 w-4" />
          </button>

          {editing ? (
            <div className="flex flex-1 items-center gap-2">
              <input
                type="text"
                aria-label={`编辑章节标题 ${chapter.title}`}
                value={draftTitle}
                onChange={(e) => setDraftTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    void saveChapterTitle();
                  } else if (e.key === 'Escape') {
                    setDraftTitle(chapter.title);
                    setEditing(false);
                  }
                }}
                className="flex-1 rounded-lg border border-indigo-500/60 bg-slate-950 px-2.5 py-1 text-sm text-white focus:outline-none"
              />
              <button
                type="button"
                disabled={saving}
                onClick={() => void saveChapterTitle()}
                className="rounded-lg bg-indigo-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-indigo-500"
              >
                保存
              </button>
              <button
                type="button"
                onClick={() => {
                  setDraftTitle(chapter.title);
                  setEditing(false);
                }}
                className="rounded-lg border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
              >
                取消
              </button>
            </div>
          ) : (
            <div className="flex items-center gap-2 min-w-0">
              <h3 className="text-sm sm:text-base font-semibold text-white truncate">
                {chapter.title}
              </h3>
              <span className="text-xs text-slate-500 shrink-0">({lessons.length} 节)</span>
            </div>
          )}
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          <button
            type="button"
            onClick={() => onOpenAddVideo(chapter.id)}
            className="inline-flex items-center gap-1 rounded-xl border border-slate-700 bg-slate-800/80 px-2.5 py-1 text-xs font-medium text-slate-200 hover:bg-slate-800 hover:text-white transition-colors"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>从存储添加视频</span>
          </button>

          <button
            type="button"
            aria-label={`上移章节 ${chapter.title}`}
            disabled={chapterIdx === 0}
            onClick={() => void onMoveChapterStep(chapterIdx, -1)}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white disabled:opacity-30 disabled:pointer-events-none"
          >
            <ArrowUp className="h-4 w-4" />
          </button>
          <button
            type="button"
            aria-label={`下移章节 ${chapter.title}`}
            disabled={chapterIdx === totalChapters - 1}
            onClick={() => void onMoveChapterStep(chapterIdx, 1)}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white disabled:opacity-30 disabled:pointer-events-none"
          >
            <ArrowDown className="h-4 w-4" />
          </button>
          <button
            type="button"
            aria-label={`重命名章节 ${chapter.title}`}
            onClick={() => setEditing(true)}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white"
          >
            <Pencil className="h-4 w-4" />
          </button>
          <button
            type="button"
            aria-label={`删除章节 ${chapter.title}`}
            onClick={() => void onDeleteChapter(chapter.id)}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-rose-500/20 hover:text-rose-300"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      {lessons.length === 0 ? (
        <p className="py-4 text-center text-xs text-slate-500 border border-dashed border-slate-800 rounded-xl">
          本章节暂无课时，可点击“从存储添加视频”或从其他章节拖拽移入。
        </p>
      ) : (
        <SortableContext
          items={lessons.map((l) => `lesson-${l.id}`)}
          strategy={verticalListSortingStrategy}
        >
          <div className="space-y-2">
            {lessons.map((lesson, lIdx) => (
              <SortableLessonRow
                key={lesson.id}
                lesson={lesson}
                chapterId={chapter.id}
                chapters={chapters}
                lessonIdx={lIdx}
                totalLessonsInChapter={lessons.length}
                onRename={onRenameLesson}
                onDelete={onDeleteLesson}
                onMoveStep={onMoveLessonStep}
                onMoveToChapter={onMoveLessonToChapter}
              />
            ))}
          </div>
        </SortableContext>
      )}
    </div>
  );
}

export default function CourseEditorPage() {
  const { courseId } = useParams<{ courseId: string }>();
  const navigate = useNavigate();

  const [course, setCourse] = useState<CourseDetail | null>(null);
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);

  // Metadata form state
  const [title, setTitle] = useState('');
  const [instructor, setInstructor] = useState('');
  const [libraryId, setLibraryId] = useState('');
  const [overview, setOverview] = useState('');
  const [tags, setTags] = useState<string[]>([]);
  const [tagInput, setTagInput] = useState('');
  const [savingMeta, setSavingMeta] = useState(false);

  // Cover & backdrop state
  const [uploadingCover, setUploadingCover] = useState(false);
  const [uploadingBackdrop, setUploadingBackdrop] = useState(false);
  const [showFramePicker, setShowFramePicker] = useState(false);
  const [coverFailed, setCoverFailed] = useState(false);
  const [backdropFailed, setBackdropFailed] = useState(false);

  // Chapter creation & delete confirm
  const [newChapterTitle, setNewChapterTitle] = useState('');
  const [addingChapter, setAddingChapter] = useState(false);
  const [confirmDeleteCourse, setConfirmDeleteCourse] = useState(false);
  const [deletingCourse, setDeletingCourse] = useState(false);

  // AI Organize Modal
  const [showAIModal, setShowAIModal] = useState(false);

  // New files check
  const [checkingNewFiles, setCheckingNewFiles] = useState(false);
  const [newFiles, setNewFiles] = useState<MediaFile[] | null>(null);
  const [selectedNewFileIds, setSelectedNewFileIds] = useState<Record<string, boolean>>({});
  const [appendingNewFiles, setAppendingNewFiles] = useState(false);

  // Add video from storage modal
  const [addVideoChapterId, setAddVideoChapterId] = useState<string | null>(null);
  const [sources, setSources] = useState<StorageSource[]>([]);
  const [selectedSourceId, setSelectedSourceId] = useState('');
  const [storageFiles, setStorageFiles] = useState<MediaFile[]>([]);
  const [storageSearch, setStorageSearch] = useState('');
  const [loadingStorageFiles, setLoadingStorageFiles] = useState(false);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const syncFormFromCourse = useCallback((c: CourseDetail) => {
    setCourse(c);
    setTitle(c.title);
    setInstructor(c.instructor ?? '');
    setLibraryId(c.library_id);
    setOverview(c.overview ?? '');
    setTags(c.tags ?? []);
    setCoverFailed(false);
    setBackdropFailed(false);
  }, []);

  const loadData = useCallback(async () => {
    if (!courseId) return;
    setLoading(true);
    setError(null);
    try {
      const [courseData, libsData] = await Promise.all([
        api.getCourse(courseId),
        api.listLibraries(),
      ]);
      syncFormFromCourse(courseData);
      setLibraries(libsData.libraries ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载课程编辑数据失败');
    } finally {
      setLoading(false);
    }
  }, [courseId, syncFormFromCourse]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const allLessons = useMemo(() => {
    if (!course) return [];
    return (course.chapters ?? []).flatMap((ch) => ch.lessons ?? []);
  }, [course]);

  function showNotice(msg: string) {
    setStatusMessage(msg);
  }

  function handleAddTag() {
    const trimmed = tagInput.trim();
    if (!trimmed || tags.includes(trimmed)) {
      setTagInput('');
      return;
    }
    setTags((prev) => [...prev, trimmed]);
    setTagInput('');
  }

  async function handleSaveMetadata(e: FormEvent) {
    e.preventDefault();
    if (!course || savingMeta) return;
    if (!title.trim()) {
      setError('课程标题不能为空');
      return;
    }
    setSavingMeta(true);
    setError(null);
    try {
      const updated = await api.updateCourse(course.id, {
        title: title.trim(),
        instructor: instructor.trim(),
        library_id: libraryId,
        overview: overview.trim(),
        tags,
      });
      syncFormFromCourse(updated);
      showNotice('课程元数据已保存');
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存元数据失败');
    } finally {
      setSavingMeta(false);
    }
  }

  async function handleCoverUpload(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file || !course) return;
    setUploadingCover(true);
    setError(null);
    try {
      const updated = await api.uploadCourseCover(course.id, file);
      syncFormFromCourse(updated);
      showNotice('课程封面已更新');
    } catch (err) {
      setError(err instanceof Error ? err.message : '上传封面失败');
    } finally {
      setUploadingCover(false);
      e.target.value = '';
    }
  }

  async function handlePickCoverFromLesson(lessonId: string) {
    if (!course) return;
    setUploadingCover(true);
    setError(null);
    try {
      const updated = await api.pickCourseCoverFromLesson(course.id, lessonId);
      syncFormFromCourse(updated);
      setShowFramePicker(false);
      showNotice('已从课时帧更新封面');
    } catch (err) {
      setError(err instanceof Error ? err.message : '选取课时帧封面失败');
    } finally {
      setUploadingCover(false);
    }
  }

  async function handleBackdropUpload(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file || !course) return;
    setUploadingBackdrop(true);
    setError(null);
    try {
      const updated = await api.uploadCourseBackdrop(course.id, file);
      syncFormFromCourse(updated);
      showNotice('课程背景图已更新');
    } catch (err) {
      setError(err instanceof Error ? err.message : '上传背景图失败');
    } finally {
      setUploadingBackdrop(false);
      e.target.value = '';
    }
  }

  async function handleDeleteCourse() {
    if (!course || deletingCourse) return;
    setDeletingCourse(true);
    setError(null);
    try {
      await api.deleteCourse(course.id);
      navigate('/', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除课程失败');
      setDeletingCourse(false);
    }
  }

  async function handleCreateChapter(e: FormEvent) {
    e.preventDefault();
    if (!course || addingChapter) return;
    const trimmed = newChapterTitle.trim();
    if (!trimmed) return;
    setAddingChapter(true);
    setError(null);
    try {
      const created = await api.createChapter(course.id, trimmed);
      setCourse((prev) =>
        prev
          ? {
              ...prev,
              chapter_count: prev.chapter_count + 1,
              chapters: [...(prev.chapters ?? []), { ...created, lessons: created.lessons ?? [] }],
            }
          : prev,
      );
      setNewChapterTitle('');
      showNotice(`已添加章节「${created.title}」`);
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加章节失败');
    } finally {
      setAddingChapter(false);
    }
  }

  async function handleRenameChapter(chapter: ChapterDetail, nextTitle: string) {
    const updated = await api.updateChapter(chapter.id, nextTitle, chapter.overview);
    setCourse((prev) =>
      prev
        ? {
            ...prev,
            chapters: (prev.chapters ?? []).map((ch) =>
              ch.id === chapter.id ? { ...ch, title: updated.title } : ch,
            ),
          }
        : prev,
    );
    showNotice('章节标题已更新');
  }

  async function handleDeleteChapter(chapterId: string) {
    if (!course) return;
    setError(null);
    try {
      await api.deleteChapter(chapterId);
      const refreshed = await api.getCourse(course.id);
      syncFormFromCourse(refreshed);
      showNotice('章节已删除');
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除章节失败');
    }
  }

  async function commitChapterOrder(nextChapters: ChapterDetail[]) {
    if (!course) return;
    setCourse((prev) => (prev ? { ...prev, chapters: nextChapters } : prev));
    try {
      await api.reorderChapters(
        course.id,
        nextChapters.map((ch) => ch.id),
      );
      showNotice('章节顺序已保存');
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存章节顺序失败');
      const refreshed = await api.getCourse(course.id);
      syncFormFromCourse(refreshed);
    }
  }

  async function handleMoveChapterStep(chapterIdx: number, delta: -1 | 1) {
    if (!course) return;
    const chapters = course.chapters ?? [];
    const targetIdx = chapterIdx + delta;
    if (targetIdx < 0 || targetIdx >= chapters.length) return;
    const next = arrayMove(chapters, chapterIdx, targetIdx);
    await commitChapterOrder(next);
  }

  async function commitLessonOrder(nextChapters: ChapterDetail[]) {
    if (!course) return;
    setCourse((prev) => (prev ? { ...prev, chapters: nextChapters } : prev));
    const placements = buildLessonPlacements(nextChapters);
    try {
      await api.reorderLessons(course.id, placements);
      showNotice('课时顺序已保存');
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存课时顺序失败');
      const refreshed = await api.getCourse(course.id);
      syncFormFromCourse(refreshed);
    }
  }

  async function handleMoveLessonStep(chapterId: string, lessonIdx: number, delta: -1 | 1) {
    if (!course) return;
    const nextChapters = (course.chapters ?? []).map((ch) => {
      if (ch.id !== chapterId) return ch;
      const lessons = ch.lessons ?? [];
      const targetIdx = lessonIdx + delta;
      if (targetIdx < 0 || targetIdx >= lessons.length) return ch;
      return { ...ch, lessons: arrayMove(lessons, lessonIdx, targetIdx) };
    });
    await commitLessonOrder(nextChapters);
  }

  async function handleMoveLessonToChapter(lessonId: string, targetChapterId: string) {
    if (!course) return;
    let movedLesson: LessonDetail | null = null;
    const stripped = (course.chapters ?? []).map((ch) => {
      const found = (ch.lessons ?? []).find((l) => l.id === lessonId);
      if (found) {
        movedLesson = { ...found, chapter_id: targetChapterId };
      }
      return {
        ...ch,
        lessons: (ch.lessons ?? []).filter((l) => l.id !== lessonId),
      };
    });
    if (!movedLesson) return;

    const nextChapters = stripped.map((ch) =>
      ch.id === targetChapterId
        ? { ...ch, lessons: [...(ch.lessons ?? []), movedLesson!] }
        : ch,
    );
    await commitLessonOrder(nextChapters);
  }

  async function handleRenameLesson(lesson: LessonDetail, nextTitle: string) {
    const updated = await api.updateLesson(lesson.id, nextTitle, lesson.overview, lesson.chapter_id);
    setCourse((prev) =>
      prev
        ? {
            ...prev,
            chapters: (prev.chapters ?? []).map((ch) => ({
              ...ch,
              lessons: (ch.lessons ?? []).map((l) =>
                l.id === lesson.id ? { ...l, title: updated.title } : l,
              ),
            })),
          }
        : prev,
    );
    showNotice('课时标题已更新');
  }

  async function handleDeleteLesson(lessonId: string) {
    if (!course) return;
    setError(null);
    try {
      await api.deleteLesson(lessonId);
      const refreshed = await api.getCourse(course.id);
      syncFormFromCourse(refreshed);
      showNotice('课时已删除');
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除课时失败');
    }
  }

  async function handleDragEnd(event: DragEndEvent) {
    if (!course) return;
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const activeStr = String(active.id);
    const overStr = String(over.id);
    const chapters = course.chapters ?? [];

    // Dragging a chapter
    if (activeStr.startsWith('chapter-') && overStr.startsWith('chapter-')) {
      const activeId = activeStr.replace('chapter-', '');
      const overId = overStr.replace('chapter-', '');
      const oldIdx = chapters.findIndex((c) => c.id === activeId);
      const newIdx = chapters.findIndex((c) => c.id === overId);
      if (oldIdx >= 0 && newIdx >= 0 && oldIdx !== newIdx) {
        await commitChapterOrder(arrayMove(chapters, oldIdx, newIdx));
      }
      return;
    }

    // Dragging a lesson
    if (activeStr.startsWith('lesson-')) {
      const activeLessonId = activeStr.replace('lesson-', '');
      let sourceChapIdx = -1;
      let sourceLessonIdx = -1;
      chapters.forEach((ch, ci) => {
        const li = (ch.lessons ?? []).findIndex((l) => l.id === activeLessonId);
        if (li >= 0) {
          sourceChapIdx = ci;
          sourceLessonIdx = li;
        }
      });
      if (sourceChapIdx < 0) return;

      let targetChapIdx = -1;
      let targetLessonIdx = -1;

      if (overStr.startsWith('lesson-')) {
        const overLessonId = overStr.replace('lesson-', '');
        chapters.forEach((ch, ci) => {
          const li = (ch.lessons ?? []).findIndex((l) => l.id === overLessonId);
          if (li >= 0) {
            targetChapIdx = ci;
            targetLessonIdx = li;
          }
        });
      } else if (overStr.startsWith('chapter-')) {
        const overChapterId = overStr.replace('chapter-', '');
        targetChapIdx = chapters.findIndex((ch) => ch.id === overChapterId);
        if (targetChapIdx >= 0) {
          targetLessonIdx = (chapters[targetChapIdx].lessons ?? []).length;
        }
      }

      if (targetChapIdx < 0) return;

      if (sourceChapIdx === targetChapIdx) {
        const currentLessons = chapters[sourceChapIdx].lessons ?? [];
        const clampedTarget = Math.min(targetLessonIdx, currentLessons.length - 1);
        if (sourceLessonIdx === clampedTarget) return;
        const nextChapters = chapters.map((ch, idx) =>
          idx === sourceChapIdx
            ? { ...ch, lessons: arrayMove(currentLessons, sourceLessonIdx, clampedTarget) }
            : ch,
        );
        await commitLessonOrder(nextChapters);
      } else {
        const movingLesson = {
          ...chapters[sourceChapIdx].lessons[sourceLessonIdx],
          chapter_id: chapters[targetChapIdx].id,
        };
        const nextChapters = chapters.map((ch, idx) => {
          if (idx === sourceChapIdx) {
            return {
              ...ch,
              lessons: ch.lessons.filter((l) => l.id !== activeLessonId),
            };
          }
          if (idx === targetChapIdx) {
            const dest = [...(ch.lessons ?? [])];
            dest.splice(targetLessonIdx, 0, movingLesson);
            return { ...ch, lessons: dest };
          }
          return ch;
        });
        await commitLessonOrder(nextChapters);
      }
    }
  }

  async function handleCheckNewFiles() {
    if (!course || checkingNewFiles) return;
    setCheckingNewFiles(true);
    setError(null);
    try {
      const res = await api.getCourseNewFiles(course.id);
      const files = res.files ?? [];
      setNewFiles(files);
      const initialSel: Record<string, boolean> = {};
      for (const f of files) {
        initialSel[f.id] = true;
      }
      setSelectedNewFileIds(initialSel);
    } catch (err) {
      setError(err instanceof Error ? err.message : '检查新文件失败');
    } finally {
      setCheckingNewFiles(false);
    }
  }

  async function handleAppendNewFiles() {
    if (!course || !newFiles || appendingNewFiles) return;
    const ids = newFiles.filter((f) => selectedNewFileIds[f.id]).map((f) => f.id);
    if (ids.length === 0) return;
    setAppendingNewFiles(true);
    setError(null);
    try {
      const updated = await api.appendCourseNewFiles(course.id, ids);
      syncFormFromCourse(updated);
      setNewFiles(null);
      showNotice(`已追加 ${ids.length} 个新视频到课程`);
    } catch (err) {
      setError(err instanceof Error ? err.message : '追加新文件失败');
    } finally {
      setAppendingNewFiles(false);
    }
  }

  async function openAddVideoModal(chapterId: string) {
    setAddVideoChapterId(chapterId);
    setStorageSearch('');
    setLoadingStorageFiles(true);
    try {
      const res = await api.listSources();
      const srcList = res.sources ?? [];
      setSources(srcList);
      const preferred =
        (course?.source_id && srcList.find((s) => s.id === course.source_id)?.id) ||
        srcList[0]?.id ||
        '';
      setSelectedSourceId(preferred);
      if (preferred) {
        const filesRes = await api.listFiles(preferred, { status: 'active' });
        setStorageFiles(filesRes.files ?? []);
      } else {
        setStorageFiles([]);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载存储文件失败');
    } finally {
      setLoadingStorageFiles(false);
    }
  }

  async function handleSourceChangeInPicker(sourceId: string) {
    setSelectedSourceId(sourceId);
    setLoadingStorageFiles(true);
    try {
      const filesRes = await api.listFiles(sourceId, {
        status: 'active',
        search: storageSearch.trim(),
      });
      setStorageFiles(filesRes.files ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取文件列表失败');
    } finally {
      setLoadingStorageFiles(false);
    }
  }

  async function handleSearchStorageFiles(e: FormEvent) {
    e.preventDefault();
    if (!selectedSourceId) return;
    setLoadingStorageFiles(true);
    try {
      const filesRes = await api.listFiles(selectedSourceId, {
        status: 'active',
        search: storageSearch.trim(),
      });
      setStorageFiles(filesRes.files ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : '搜索文件失败');
    } finally {
      setLoadingStorageFiles(false);
    }
  }

  async function handleAddLessonFromStorage(file: MediaFile) {
    if (!addVideoChapterId || !course) return;
    setError(null);
    try {
      await api.addLesson(addVideoChapterId, file.id);
      const refreshed = await api.getCourse(course.id);
      syncFormFromCourse(refreshed);
      setAddVideoChapterId(null);
      showNotice(`已添加课时「${file.file_name}」`);
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加视频课时失败');
    }
  }

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center py-24 text-slate-400 gap-3">
        <Loader2 className="h-7 w-7 animate-spin text-indigo-400" />
        <p className="text-sm">正在加载课程编辑器...</p>
      </div>
    );
  }

  if (!course) {
    return (
      <div className="rounded-2xl border border-rose-500/30 bg-rose-500/10 p-6 text-center max-w-lg mx-auto my-12 space-y-4">
        <p className="text-rose-200 font-medium">{error ?? '未找到课程'}</p>
        <Link
          to="/"
          className="inline-flex items-center gap-1.5 rounded-xl border border-slate-700 bg-slate-800 px-4 py-2 text-sm text-slate-200 hover:bg-slate-700"
        >
          <ArrowLeft className="h-4 w-4" />
          <span>返回课程库</span>
        </Link>
      </div>
    );
  }

  const chapters = course.chapters ?? [];

  return (
    <div className="space-y-6">
      {/* Top Action Bar */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <Link
            to={`/courses/${course.id}`}
            className="inline-flex items-center gap-1.5 text-sm font-medium text-slate-400 hover:text-white transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            <span>返回课程播放</span>
          </Link>
          <span className="text-slate-700">/</span>
          <h1 className="text-lg font-bold text-white truncate max-w-md">{course.title}</h1>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => void handleCheckNewFiles()}
            disabled={checkingNewFiles}
            className="inline-flex items-center gap-1.5 rounded-xl border border-slate-700 bg-slate-800/80 px-3.5 py-2 text-xs font-medium text-slate-200 hover:bg-slate-800 disabled:opacity-50 transition-colors"
          >
            {checkingNewFiles ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <FolderSearch className="h-3.5 w-3.5" />
            )}
            <span>检查新文件</span>
          </button>

          <button
            type="button"
            onClick={() => setShowAIModal(true)}
            className="inline-flex items-center gap-1.5 rounded-xl bg-gradient-to-r from-indigo-600 to-sky-600 px-3.5 py-2 text-xs font-semibold text-white shadow hover:from-indigo-500 hover:to-sky-500 transition-colors"
          >
            <Sparkles className="h-3.5 w-3.5" />
            <span>AI 智能整理</span>
          </button>

          {!confirmDeleteCourse ? (
            <button
              type="button"
              onClick={() => setConfirmDeleteCourse(true)}
              className="inline-flex items-center gap-1.5 rounded-xl border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-xs font-medium text-rose-300 hover:bg-rose-500/20 transition-colors"
            >
              <Trash2 className="h-3.5 w-3.5" />
              <span>删除课程</span>
            </button>
          ) : (
            <div className="inline-flex items-center gap-1.5 rounded-xl border border-rose-500/40 bg-rose-950/80 px-2.5 py-1">
              <span className="text-xs text-rose-200">确认删除?</span>
              <button
                type="button"
                disabled={deletingCourse}
                onClick={() => void handleDeleteCourse()}
                className="rounded-lg bg-rose-600 px-2 py-1 text-xs font-semibold text-white hover:bg-rose-500"
              >
                {deletingCourse ? '删除中...' : '确认'}
              </button>
              <button
                type="button"
                onClick={() => setConfirmDeleteCourse(false)}
                className="rounded-lg px-2 py-1 text-xs text-slate-300 hover:bg-slate-800"
              >
                取消
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Feedback banners */}
      {statusMessage && (
        <div className="flex items-center justify-between rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-2.5 text-sm text-emerald-200">
          <span className="flex items-center gap-2">
            <Check className="h-4 w-4 text-emerald-400" />
            {statusMessage}
          </span>
          <button
            type="button"
            onClick={() => setStatusMessage(null)}
            className="text-emerald-300 hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      {error && (
        <div className="flex items-center justify-between rounded-xl border border-rose-500/30 bg-rose-500/10 px-4 py-2.5 text-sm text-rose-200">
          <span>{error}</span>
          <button type="button" onClick={() => setError(null)} className="text-rose-300 hover:text-white">
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      {/* New Files Sync Panel */}
      {newFiles !== null && (
        <section className="rounded-2xl border border-sky-500/30 bg-sky-500/5 p-5 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FilePlus2 className="h-4 w-4 text-sky-400" />
              <h2 className="text-sm font-semibold text-white">
                新文件检查结果（{newFiles.length} 个未导入视频）
              </h2>
            </div>
            <button
              type="button"
              onClick={() => setNewFiles(null)}
              className="text-xs text-slate-400 hover:text-white"
            >
              关闭
            </button>
          </div>

          {newFiles.length === 0 ? (
            <p className="text-xs text-slate-400">当前关联目录中没有发现未导入的新视频文件。</p>
          ) : (
            <>
              <div className="max-h-56 overflow-y-auto space-y-1.5">
                {newFiles.map((file) => (
                  <label
                    key={file.id}
                    className="flex items-center gap-2.5 rounded-lg border border-slate-800 bg-slate-900/80 px-3 py-2 text-xs text-slate-200 cursor-pointer"
                  >
                    <input
                      type="checkbox"
                      checked={Boolean(selectedNewFileIds[file.id])}
                      onChange={(e) =>
                        setSelectedNewFileIds((prev) => ({
                          ...prev,
                          [file.id]: e.target.checked,
                        }))
                      }
                      className="h-3.5 w-3.5 rounded border-slate-600 bg-slate-800 text-indigo-500"
                    />
                    <span className="font-medium">{file.file_name}</span>
                    <span className="text-slate-500 truncate">{file.relative_path}</span>
                  </label>
                ))}
              </div>
              <div className="flex justify-end">
                <button
                  type="button"
                  disabled={appendingNewFiles}
                  onClick={() => void handleAppendNewFiles()}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-sky-600 px-4 py-2 text-xs font-semibold text-white hover:bg-sky-500 disabled:opacity-50"
                >
                  {appendingNewFiles ? '正在追加...' : '追加到课程'}
                </button>
              </div>
            </>
          )}
        </section>
      )}

      {/* Main Editor Grid: Metadata + Cover/Backdrop */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Metadata Form */}
        <form
          onSubmit={handleSaveMetadata}
          className="lg:col-span-2 rounded-2xl border border-slate-800 bg-slate-900/75 p-5 space-y-4"
        >
          <h2 className="text-base font-semibold text-white">课程元数据</h2>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="space-y-1.5 sm:col-span-2">
              <label htmlFor="editor-course-title" className="block text-xs font-medium text-slate-300">
                课程标题
              </label>
              <input
                id="editor-course-title"
                type="text"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white focus:border-indigo-500 focus:outline-none"
                required
              />
            </div>

            <div className="space-y-1.5">
              <label htmlFor="editor-instructor" className="block text-xs font-medium text-slate-300">
                讲师
              </label>
              <input
                id="editor-instructor"
                type="text"
                value={instructor}
                onChange={(e) => setInstructor(e.target.value)}
                placeholder="讲师姓名"
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white focus:border-indigo-500 focus:outline-none"
              />
            </div>

            <div className="space-y-1.5">
              <label htmlFor="editor-library" className="block text-xs font-medium text-slate-300">
                所属媒体库
              </label>
              <select
                id="editor-library"
                value={libraryId}
                onChange={(e) => setLibraryId(e.target.value)}
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white focus:border-indigo-500 focus:outline-none"
              >
                {libraries.map((lib) => (
                  <option key={lib.id} value={lib.id}>
                    {lib.name}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-1.5 sm:col-span-2">
              <label htmlFor="editor-overview" className="block text-xs font-medium text-slate-300">
                课程简介
              </label>
              <textarea
                id="editor-overview"
                rows={4}
                value={overview}
                onChange={(e) => setOverview(e.target.value)}
                className="w-full rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white focus:border-indigo-500 focus:outline-none"
              />
            </div>

            <div className="space-y-2 sm:col-span-2">
              <label htmlFor="editor-tag-input" className="block text-xs font-medium text-slate-300">
                课程标签
              </label>
              <div className="flex flex-wrap gap-1.5">
                {tags.map((t) => (
                  <span
                    key={t}
                    className="inline-flex items-center gap-1 rounded-full border border-indigo-500/30 bg-indigo-500/15 px-2.5 py-0.5 text-xs text-indigo-200"
                  >
                    {t}
                    <button
                      type="button"
                      aria-label={`移除标签 ${t}`}
                      onClick={() => setTags((prev) => prev.filter((x) => x !== t))}
                      className="hover:text-white"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                ))}
              </div>
              <div className="flex gap-2">
                <input
                  id="editor-tag-input"
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
                <button
                  type="button"
                  onClick={handleAddTag}
                  className="rounded-xl border border-slate-700 bg-slate-800 px-3 py-1.5 text-xs font-medium text-slate-200 hover:bg-slate-700"
                >
                  添加标签
                </button>
              </div>
            </div>
          </div>

          <div className="flex justify-end pt-2">
            <button
              type="submit"
              disabled={savingMeta}
              className="inline-flex items-center gap-1.5 rounded-xl bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors"
            >
              {savingMeta ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Save className="h-4 w-4" />
              )}
              <span>{savingMeta ? '保存中...' : '保存元数据'}</span>
            </button>
          </div>
        </form>

        {/* Cover & Backdrop Panel */}
        <div className="rounded-2xl border border-slate-800 bg-slate-900/75 p-5 space-y-5">
          <div className="space-y-3">
            <h2 className="text-base font-semibold text-white">课程封面</h2>
            <div className="aspect-[16/10] w-full overflow-hidden rounded-xl border border-slate-800 bg-slate-950 flex items-center justify-center">
              {(course.cover_path || course.first_lesson_id) && !coverFailed ? (
                <img
                  src={itemPrimaryImageUrl(course.id, course.updated_at)}
                  alt={course.title}
                  onError={() => setCoverFailed(true)}
                  className="h-full w-full object-cover"
                />
              ) : (
                <span className="text-xs text-slate-500">暂无封面</span>
              )}
            </div>

            <div className="flex flex-wrap gap-2">
              <label className="inline-flex cursor-pointer items-center gap-1.5 rounded-xl border border-slate-700 bg-slate-800 px-3 py-1.5 text-xs font-medium text-slate-200 hover:bg-slate-700 transition-colors">
                <Upload className="h-3.5 w-3.5" />
                <span>{uploadingCover ? '上传中...' : '上传封面'}</span>
                <input
                  type="file"
                  accept="image/*"
                  className="sr-only"
                  disabled={uploadingCover}
                  onChange={(e) => void handleCoverUpload(e)}
                />
              </label>

              <button
                type="button"
                disabled={allLessons.length === 0 || uploadingCover}
                onClick={() => setShowFramePicker((v) => !v)}
                className="inline-flex items-center gap-1.5 rounded-xl border border-indigo-500/40 bg-indigo-500/10 px-3 py-1.5 text-xs font-medium text-indigo-200 hover:bg-indigo-500/20 disabled:opacity-50 transition-colors"
              >
                <ImagePlus className="h-3.5 w-3.5" />
                <span>从课时帧选取封面</span>
              </button>
            </div>

            {showFramePicker && (
              <div className="rounded-xl border border-slate-800 bg-slate-950/90 p-3 space-y-2 max-h-48 overflow-y-auto">
                <p className="text-[11px] font-medium text-slate-400">
                  点击下方课时，将使用其视频截图作为课程封面：
                </p>
                {allLessons.map((lesson) => (
                  <button
                    key={lesson.id}
                    type="button"
                    onClick={() => void handlePickCoverFromLesson(lesson.id)}
                    className="w-full flex items-center justify-between rounded-lg border border-slate-800 bg-slate-900 px-2.5 py-1.5 text-left text-xs text-slate-200 hover:border-indigo-500/50 hover:text-white transition-colors"
                  >
                    <span className="truncate">{lesson.title}</span>
                    <span className="text-[10px] text-indigo-300 shrink-0 ml-2">设为封面</span>
                  </button>
                ))}
              </div>
            )}
          </div>

          <div className="border-t border-slate-800 pt-4 space-y-3">
            <h3 className="text-sm font-semibold text-white">背景横幅（Backdrop）</h3>
            <div className="aspect-video w-full overflow-hidden rounded-xl border border-slate-800 bg-slate-950 flex items-center justify-center">
              {course.backdrop_path && !backdropFailed ? (
                <img
                  src={itemBackdropImageUrl(course.id, course.updated_at)}
                  alt={`${course.title} 背景`}
                  onError={() => setBackdropFailed(true)}
                  className="h-full w-full object-cover"
                />
              ) : (
                <span className="text-xs text-slate-500">暂无背景图</span>
              )}
            </div>
            <label className="inline-flex cursor-pointer items-center gap-1.5 rounded-xl border border-slate-700 bg-slate-800 px-3 py-1.5 text-xs font-medium text-slate-200 hover:bg-slate-700 transition-colors">
              <Upload className="h-3.5 w-3.5" />
              <span>{uploadingBackdrop ? '上传中...' : '上传背景图'}</span>
              <input
                type="file"
                accept="image/*"
                className="sr-only"
                disabled={uploadingBackdrop}
                onChange={(e) => void handleBackdropUpload(e)}
              />
            </label>
          </div>
        </div>
      </div>

      {/* Chapter & Lesson Organizer */}
      <section className="space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div>
            <h2 className="text-base font-semibold text-white">章节与课时结构</h2>
            <p className="text-xs text-slate-400">
              支持拖拽或使用上下箭头调整章节与课时顺序，也可跨章节移动课时或查看字幕信息。
            </p>
          </div>

          <form onSubmit={handleCreateChapter} className="flex items-center gap-2">
            <input
              type="text"
              value={newChapterTitle}
              onChange={(e) => setNewChapterTitle(e.target.value)}
              placeholder="输入新章节名称..."
              aria-label="新章节名称"
              className="rounded-xl border border-slate-700 bg-slate-950 px-3 py-1.5 text-xs text-white focus:border-indigo-500 focus:outline-none"
            />
            <button
              type="submit"
              disabled={addingChapter || !newChapterTitle.trim()}
              className="inline-flex items-center gap-1 rounded-xl bg-indigo-600 px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>添加章节</span>
            </button>
          </form>
        </div>

        {chapters.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-slate-800 bg-slate-900/40 p-10 text-center text-sm text-slate-400">
            暂无章节，请先添加一个章节后再从存储源添加视频课时。
          </div>
        ) : (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={(event) => void handleDragEnd(event)}
          >
            <SortableContext
              items={chapters.map((c) => `chapter-${c.id}`)}
              strategy={verticalListSortingStrategy}
            >
              <div className="space-y-4">
                {chapters.map((chapter, idx) => (
                  <SortableChapterCard
                    key={chapter.id}
                    chapter={chapter}
                    chapters={chapters}
                    chapterIdx={idx}
                    totalChapters={chapters.length}
                    onRenameChapter={handleRenameChapter}
                    onDeleteChapter={handleDeleteChapter}
                    onMoveChapterStep={handleMoveChapterStep}
                    onOpenAddVideo={(chapId) => void openAddVideoModal(chapId)}
                    onRenameLesson={handleRenameLesson}
                    onDeleteLesson={handleDeleteLesson}
                    onMoveLessonStep={handleMoveLessonStep}
                    onMoveLessonToChapter={handleMoveLessonToChapter}
                  />
                ))}
              </div>
            </SortableContext>
          </DndContext>
        )}
      </section>

      {/* Add Video From Storage Modal */}
      {addVideoChapterId && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/80 backdrop-blur-sm p-4"
          role="dialog"
          aria-modal="true"
          aria-label="从存储添加视频"
        >
          <div className="flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-slate-800 bg-slate-900 shadow-2xl">
            <div className="flex items-center justify-between border-b border-slate-800 px-5 py-4">
              <h3 className="text-base font-bold text-white">从存储添加视频到章节</h3>
              <button
                type="button"
                aria-label="关闭"
                onClick={() => setAddVideoChapterId(null)}
                className="rounded-lg p-1 text-slate-400 hover:bg-slate-800 hover:text-white"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            <div className="p-5 space-y-4 flex-1 overflow-y-auto">
              <div className="flex flex-col sm:flex-row gap-2">
                <select
                  aria-label="选择存储源"
                  value={selectedSourceId}
                  onChange={(e) => void handleSourceChangeInPicker(e.target.value)}
                  className="rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-white focus:border-indigo-500 focus:outline-none"
                >
                  {sources.map((src) => (
                    <option key={src.id} value={src.id}>
                      {src.name} ({src.root_path})
                    </option>
                  ))}
                </select>

                <form onSubmit={handleSearchStorageFiles} className="flex flex-1 gap-2">
                  <input
                    type="text"
                    value={storageSearch}
                    onChange={(e) => setStorageSearch(e.target.value)}
                    placeholder="按文件名或路径搜索..."
                    className="flex-1 rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 text-xs text-white focus:border-indigo-500 focus:outline-none"
                  />
                  <button
                    type="submit"
                    className="rounded-xl bg-slate-800 border border-slate-700 px-3 py-2 text-xs font-medium text-slate-200 hover:bg-slate-700"
                  >
                    筛选
                  </button>
                </form>
              </div>

              {loadingStorageFiles ? (
                <div className="flex items-center justify-center py-10 text-slate-400 gap-2 text-xs">
                  <Loader2 className="h-4 w-4 animate-spin text-indigo-400" />
                  <span>加载文件列表...</span>
                </div>
              ) : storageFiles.length === 0 ? (
                <p className="py-8 text-center text-xs text-slate-500">
                  未找到可用的视频文件，请先在“存储与导入”中扫描存储源。
                </p>
              ) : (
                <div className="space-y-1.5">
                  {storageFiles.map((file) => (
                    <div
                      key={file.id}
                      className="flex items-center justify-between gap-3 rounded-xl border border-slate-800 bg-slate-950/70 px-3 py-2"
                    >
                      <div className="min-w-0">
                        <p className="text-xs font-medium text-slate-100 truncate">
                          {file.file_name}
                        </p>
                        <p className="text-[11px] text-slate-500 truncate">{file.relative_path}</p>
                      </div>
                      <button
                        type="button"
                        onClick={() => void handleAddLessonFromStorage(file)}
                        className="shrink-0 rounded-lg bg-indigo-600 px-3 py-1 text-xs font-medium text-white hover:bg-indigo-500"
                      >
                        添加
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* AI Organize Modal */}
      <AIOrganizeModal
        course={course}
        isOpen={showAIModal}
        onClose={() => setShowAIModal(false)}
        onApplied={(updated) => {
          syncFormFromCourse(updated);
          showNotice('已应用 AI 整理结果');
        }}
      />
    </div>
  );
}
