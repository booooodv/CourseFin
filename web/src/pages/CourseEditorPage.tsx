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
import AICoverModal from '../components/AICoverModal';
import AIOrganizeModal from '../components/AIOrganizeModal';
import CourseCoverGallery from '../components/CourseCoverGallery';
import SubtitleBindModal from '../components/SubtitleBindModal';
import { useToast } from '../components/Toast';
import Tooltip from '../components/Tooltip';
import { useLibrary } from '../contexts/LibraryContext';
import type {
  ChapterDetail,
  CourseCover,
  CourseDetail,
  LessonDetail,
  LessonPlacement,
  Library,
  MediaFile,
  StorageSource,
} from '../types/coursefin';

const FIELD_INPUT_CLASS =
  'w-full rounded-xl border border-default bg-subtle/60 px-3.5 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors';

const ICON_ACTION_BTN_CLASS =
  'rounded-lg p-1.5 text-ink-muted hover:bg-card hover:text-ink-primary disabled:opacity-30 disabled:pointer-events-none transition-colors cursor-pointer';

const DANGER_ICON_BTN_CLASS =
  'rounded-lg p-1.5 text-ink-muted hover:bg-status-danger-soft hover:text-status-danger-ink transition-colors cursor-pointer';

const SECONDARY_BTN_CLASS =
  'inline-flex items-center gap-1.5 rounded-xl border border-default bg-subtle px-3 py-1.5 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors';

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

export type DragReorderResult =
  | { type: 'chapter'; chapters: ChapterDetail[] }
  | { type: 'lesson'; chapters: ChapterDetail[] };

export function resolveDragReorder(
  chapters: ChapterDetail[],
  activeStr: string,
  overStr: string,
): DragReorderResult | null {
  if (!overStr || activeStr === overStr) return null;

  // Dragging a chapter (including dropping onto a lesson inside a populated chapter card)
  if (activeStr.startsWith('chapter-')) {
    const activeId = activeStr.slice('chapter-'.length);
    let overChapterId = '';
    if (overStr.startsWith('chapter-')) {
      overChapterId = overStr.slice('chapter-'.length);
    } else if (overStr.startsWith('lesson-')) {
      const overLessonId = overStr.slice('lesson-'.length);
      const parentChapter = chapters.find((ch) =>
        (ch.lessons ?? []).some((l) => l.id === overLessonId),
      );
      if (parentChapter) {
        overChapterId = parentChapter.id;
      }
    }
    if (!overChapterId) return null;
    const oldIdx = chapters.findIndex((c) => c.id === activeId);
    const newIdx = chapters.findIndex((c) => c.id === overChapterId);
    if (oldIdx >= 0 && newIdx >= 0 && oldIdx !== newIdx) {
      return { type: 'chapter', chapters: arrayMove(chapters, oldIdx, newIdx) };
    }
    return null;
  }

  // Dragging a lesson
  if (activeStr.startsWith('lesson-')) {
    const activeLessonId = activeStr.slice('lesson-'.length);
    let sourceChapIdx = -1;
    let sourceLessonIdx = -1;
    chapters.forEach((ch, ci) => {
      const li = (ch.lessons ?? []).findIndex((l) => l.id === activeLessonId);
      if (li >= 0) {
        sourceChapIdx = ci;
        sourceLessonIdx = li;
      }
    });
    if (sourceChapIdx < 0) return null;

    let targetChapIdx = -1;
    let targetLessonIdx = -1;

    if (overStr.startsWith('lesson-')) {
      const overLessonId = overStr.slice('lesson-'.length);
      chapters.forEach((ch, ci) => {
        const li = (ch.lessons ?? []).findIndex((l) => l.id === overLessonId);
        if (li >= 0) {
          targetChapIdx = ci;
          targetLessonIdx = li;
        }
      });
    } else if (overStr.startsWith('chapter-')) {
      const overChapterId = overStr.slice('chapter-'.length);
      targetChapIdx = chapters.findIndex((ch) => ch.id === overChapterId);
      if (targetChapIdx >= 0) {
        targetLessonIdx = (chapters[targetChapIdx].lessons ?? []).length;
      }
    }

    if (targetChapIdx < 0) return null;

    if (sourceChapIdx === targetChapIdx) {
      const currentLessons = chapters[sourceChapIdx].lessons ?? [];
      const clampedTarget = Math.min(targetLessonIdx, currentLessons.length - 1);
      if (sourceLessonIdx === clampedTarget) return null;
      const nextChapters = chapters.map((ch, idx) =>
        idx === sourceChapIdx
          ? { ...ch, lessons: arrayMove(currentLessons, sourceLessonIdx, clampedTarget) }
          : ch,
      );
      return { type: 'lesson', chapters: nextChapters };
    }

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
    return { type: 'lesson', chapters: nextChapters };
  }

  return null;
}

interface SortableLessonRowProps {
  lesson: LessonDetail;
  chapterId: string;
  chapters: ChapterDetail[];
  courseSourceId?: string;
  lessonIdx: number;
  totalLessonsInChapter: number;
  onRename: (lesson: LessonDetail, nextTitle: string) => Promise<void>;
  onDelete: (lessonId: string) => Promise<void>;
  onMoveStep: (chapterId: string, lessonIdx: number, delta: -1 | 1) => Promise<void>;
  onMoveToChapter: (lessonId: string, targetChapterId: string) => Promise<void>;
  onManageSubtitles: (lesson: LessonDetail) => void;
}

function SortableLessonRow({
  lesson,
  chapterId,
  chapters,
  courseSourceId,
  lessonIdx,
  totalLessonsInChapter,
  onRename,
  onDelete,
  onMoveStep,
  onMoveToChapter,
  onManageSubtitles,
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
    } catch {
      // error is surfaced via parent handleRenameLesson
    } finally {
      setSaving(false);
    }
  }

  const subtitles = lesson.subtitles ?? [];

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`rounded-2xl border px-3.5 py-2.5 transition-all ${
        isDragging
          ? 'border-accent bg-accent-soft ring-2 ring-accent shadow-card-hover'
          : 'border-subtle bg-subtle/40 hover:bg-subtle hover:border-default'
      }`}
    >
      <div className="flex flex-wrap items-center gap-2">
        <Tooltip content="按住拖拽调整课时顺序或跨章节移动">
          <button
            type="button"
            aria-label={`拖拽排序课时 ${lesson.title}`}
            className="cursor-grab rounded-lg p-1 text-ink-muted hover:bg-card/60 hover:text-ink-primary transition-colors"
            {...attributes}
            {...listeners}
          >
            <GripVertical className="h-4 w-4" />
          </button>
        </Tooltip>

        <Video className="h-4 w-4 text-accent-ink shrink-0" />

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
              className="flex-1 rounded-xl border border-accent bg-card px-2.5 py-1 text-xs text-ink-primary focus:outline-none focus:ring-2 focus:ring-accent-ring"
            />
            <button
              type="button"
              disabled={saving}
              onClick={() => void saveTitle()}
              className="rounded-xl bg-accent px-2.5 py-1 text-xs font-semibold text-ink-inverse hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
            >
              保存
            </button>
            <button
              type="button"
              onClick={() => {
                setDraftTitle(lesson.title);
                setEditing(false);
              }}
              className="rounded-xl border border-default bg-card px-2.5 py-1 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
            >
              取消
            </button>
          </div>
        ) : (
          <div className="flex-1 min-w-0">
            <p className="text-xs sm:text-sm font-medium text-ink-primary truncate">
              {lesson.title}
            </p>
            <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-ink-muted">
              {lesson.duration_ms > 0 && (
                <span className="tabular-nums">{formatDuration(lesson.duration_ms)}</span>
              )}
              {lesson.video_codec && <span>{lesson.video_codec.toUpperCase()}</span>}
              {lesson.media_status === 'missing' && (
                <span className="rounded-md border border-status-danger/30 bg-status-danger-soft px-1.5 py-0.5 font-medium text-status-danger-ink">
                  文件缺失
                </span>
              )}
              {subtitles.length > 0 && (
                <span className="inline-flex flex-wrap items-center gap-1 text-accent-ink">
                  <Subtitles className="h-3 w-3 shrink-0" />
                  {subtitles.map((sub) => (
                    <span
                      key={sub.id}
                      className={`rounded-md border px-1.5 py-0.5 font-medium ${
                        sub.status === 'missing'
                          ? 'border-status-danger/30 bg-status-danger-soft text-status-danger-ink'
                          : 'border-accent-ring bg-accent-soft text-accent-ink'
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
              className="rounded-xl border border-default bg-card px-2 py-1 text-[11px] text-ink-secondary focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring"
            >
              {chapters.map((ch) => (
                <option key={ch.id} value={ch.id}>
                  {ch.title}
                </option>
              ))}
            </select>
          )}

          {(lesson.source_id || courseSourceId) && (
            <Tooltip content="查看或手动关联外挂字幕">
              <button
                type="button"
                aria-label={`管理课时字幕 ${lesson.title}`}
                onClick={() => onManageSubtitles(lesson)}
                className={ICON_ACTION_BTN_CLASS}
              >
                <Subtitles className="h-3.5 w-3.5" />
              </button>
            </Tooltip>
          )}
          <Tooltip content="上移该课时">
            <button
              type="button"
              aria-label={`上移课时 ${lesson.title}`}
              disabled={lessonIdx === 0}
              onClick={() => void onMoveStep(chapterId, lessonIdx, -1)}
              className={ICON_ACTION_BTN_CLASS}
            >
              <ArrowUp className="h-3.5 w-3.5" />
            </button>
          </Tooltip>
          <Tooltip content="下移该课时">
            <button
              type="button"
              aria-label={`下移课时 ${lesson.title}`}
              disabled={lessonIdx === totalLessonsInChapter - 1}
              onClick={() => void onMoveStep(chapterId, lessonIdx, 1)}
              className={ICON_ACTION_BTN_CLASS}
            >
              <ArrowDown className="h-3.5 w-3.5" />
            </button>
          </Tooltip>
          <Tooltip content="重命名课时标题">
            <button
              type="button"
              aria-label={`重命名课时 ${lesson.title}`}
              onClick={() => setEditing(true)}
              className={ICON_ACTION_BTN_CLASS}
            >
              <Pencil className="h-3.5 w-3.5" />
            </button>
          </Tooltip>
          <Tooltip content="从课程中移除此课时（不删本地文件）">
            <button
              type="button"
              aria-label={`删除课时 ${lesson.title}`}
              onClick={() => void onDelete(lesson.id)}
              className={DANGER_ICON_BTN_CLASS}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </Tooltip>
        </div>
      </div>
    </div>
  );
}

interface SortableChapterCardProps {
  chapter: ChapterDetail;
  chapters: ChapterDetail[];
  courseSourceId?: string;
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
  onManageSubtitles: (lesson: LessonDetail) => void;
}

function SortableChapterCard({
  chapter,
  chapters,
  courseSourceId,
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
  onManageSubtitles,
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
    } catch {
      // error is surfaced via parent handleRenameChapter
    } finally {
      setSaving(false);
    }
  }

  const lessons = chapter.lessons ?? [];

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`rounded-3xl bg-card border shadow-card overflow-hidden transition-all ${
        isDragging ? 'border-accent ring-2 ring-accent shadow-card-hover' : 'border-subtle'
      }`}
    >
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-subtle bg-subtle/60 px-5 py-3.5">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <Tooltip content="按住拖拽调整章节顺序">
            <button
              type="button"
              aria-label={`拖拽排序章节 ${chapter.title}`}
              className="cursor-grab rounded-lg p-1 text-ink-muted hover:bg-card/60 hover:text-ink-primary transition-colors"
              {...attributes}
              {...listeners}
            >
              <GripVertical className="h-4 w-4" />
            </button>
          </Tooltip>

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
                className="flex-1 rounded-xl border border-accent bg-card px-2.5 py-1 text-sm text-ink-primary focus:outline-none focus:ring-2 focus:ring-accent-ring"
              />
              <button
                type="button"
                disabled={saving}
                onClick={() => void saveChapterTitle()}
                className="rounded-xl bg-accent px-2.5 py-1 text-xs font-semibold text-ink-inverse hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
              >
                保存
              </button>
              <button
                type="button"
                onClick={() => {
                  setDraftTitle(chapter.title);
                  setEditing(false);
                }}
                className="rounded-xl border border-default bg-card px-2.5 py-1 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
              >
                取消
              </button>
            </div>
          ) : (
            <div className="flex items-center gap-2 min-w-0">
              <h3 className="text-sm sm:text-base font-bold text-ink-primary truncate">
                {chapter.title}
              </h3>
              <span className="rounded-full border border-subtle bg-card px-2 py-0.5 text-[11px] font-medium text-ink-muted tabular-nums shrink-0">
                {lessons.length} 节
              </span>
            </div>
          )}
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          <Tooltip content="从已索引的存储源中挑选视频添加到本章节">
            <button
              type="button"
              onClick={() => onOpenAddVideo(chapter.id)}
              className="inline-flex items-center gap-1 rounded-xl border border-default bg-card px-2.5 py-1.5 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>从存储添加视频</span>
            </button>
          </Tooltip>

          <Tooltip content="上移该章节">
            <button
              type="button"
              aria-label={`上移章节 ${chapter.title}`}
              disabled={chapterIdx === 0}
              onClick={() => void onMoveChapterStep(chapterIdx, -1)}
              className={ICON_ACTION_BTN_CLASS}
            >
              <ArrowUp className="h-4 w-4" />
            </button>
          </Tooltip>
          <Tooltip content="下移该章节">
            <button
              type="button"
              aria-label={`下移章节 ${chapter.title}`}
              disabled={chapterIdx === totalChapters - 1}
              onClick={() => void onMoveChapterStep(chapterIdx, 1)}
              className={ICON_ACTION_BTN_CLASS}
            >
              <ArrowDown className="h-4 w-4" />
            </button>
          </Tooltip>
          <Tooltip content="重命名章节标题">
            <button
              type="button"
              aria-label={`重命名章节 ${chapter.title}`}
              onClick={() => setEditing(true)}
              className={ICON_ACTION_BTN_CLASS}
            >
              <Pencil className="h-4 w-4" />
            </button>
          </Tooltip>
          <Tooltip content="删除此章节及其课时记录（不删本地文件）">
            <button
              type="button"
              aria-label={`删除章节 ${chapter.title}`}
              onClick={() => void onDeleteChapter(chapter.id)}
              className={DANGER_ICON_BTN_CLASS}
            >
              <Trash2 className="h-4 w-4" />
            </button>
          </Tooltip>
        </div>
      </div>

      <div className="p-4">
        {lessons.length === 0 ? (
          <p className="py-5 text-center text-xs text-ink-muted border border-dashed border-default bg-subtle/30 rounded-2xl">
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
                  courseSourceId={courseSourceId}
                  lessonIdx={lIdx}
                  totalLessonsInChapter={lessons.length}
                  onRename={onRenameLesson}
                  onDelete={onDeleteLesson}
                  onMoveStep={onMoveLessonStep}
                  onMoveToChapter={onMoveLessonToChapter}
                  onManageSubtitles={onManageSubtitles}
                />
              ))}
            </div>
          </SortableContext>
        )}
      </div>
    </div>
  );
}

export default function CourseEditorPage() {
  const { courseId } = useParams<{ courseId: string }>();
  const navigate = useNavigate();
  const { toast } = useToast();
  const libCtx = useLibrary();

  const [course, setCourse] = useState<CourseDetail | null>(null);
  const [localLibraries, setLocalLibraries] = useState<Library[]>([]);
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
  const [busyCoverId, setBusyCoverId] = useState<string | null>(null);
  const [uploadingBackdrop, setUploadingBackdrop] = useState(false);
  const [showFramePicker, setShowFramePicker] = useState(false);
  const [coverFailed, setCoverFailed] = useState(false);
  const [backdropFailed, setBackdropFailed] = useState(false);

  // Chapter creation & delete confirm
  const [newChapterTitle, setNewChapterTitle] = useState('');
  const [addingChapter, setAddingChapter] = useState(false);
  const [confirmDeleteCourse, setConfirmDeleteCourse] = useState(false);
  const [deletingCourse, setDeletingCourse] = useState(false);

  // AI Organize & AI Cover Modals
  const [showAIModal, setShowAIModal] = useState(false);
  const [showAICoverModal, setShowAICoverModal] = useState(false);

  // Manual Subtitle Bind Modal
  const [subtitleLessonId, setSubtitleLessonId] = useState<string | null>(null);

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

  const syncCourseCoverState = useCallback((updated: CourseDetail) => {
    setCourse(updated);
    setCoverFailed(false);
    setBackdropFailed(false);
  }, []);

  const loadData = useCallback(async () => {
    if (!courseId) return;
    setLoading(true);
    setError(null);
    try {
      if (libCtx.isProvided) {
        const courseData = await api.getCourse(courseId);
        syncFormFromCourse(courseData);
      } else {
        const [courseData, libsData] = await Promise.all([
          api.getCourse(courseId),
          api.listLibraries(),
        ]);
        syncFormFromCourse(courseData);
        setLocalLibraries(libsData.libraries ?? []);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载课程编辑数据失败');
    } finally {
      setLoading(false);
    }
  }, [courseId, libCtx.isProvided, syncFormFromCourse]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  useEffect(() => {
    if (!addVideoChapterId) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setAddVideoChapterId(null);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [addVideoChapterId]);

  const libraries = libCtx.isProvided ? libCtx.libraries : localLibraries;

  const allLessons = useMemo(() => {
    if (!course) return [];
    return (course.chapters ?? []).flatMap((ch) => ch.lessons ?? []);
  }, [course]);

  const subtitleLesson = useMemo(
    () => (subtitleLessonId ? allLessons.find((l) => l.id === subtitleLessonId) ?? null : null),
    [allLessons, subtitleLessonId],
  );

  const refreshCourseSilently = useCallback(async () => {
    if (!courseId) return;
    try {
      const updated = await api.getCourse(courseId);
      setCourse(updated);
    } catch (err) {
      showError(err instanceof Error ? err.message : '刷新课程信息失败');
    }
  }, [courseId]); // eslint-disable-line react-hooks/exhaustive-deps

  function showNotice(msg: string) {
    setStatusMessage(msg);
    toast.success(msg);
  }

  function showError(msg: string) {
    setError(msg);
    toast.error(msg);
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
    const previousLibraryId = course.library_id;
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
      if (updated.library_id !== previousLibraryId || libraryId !== previousLibraryId) {
        libCtx.bumpCatalogVersion();
      }
      showNotice('课程元数据已保存');
    } catch (err) {
      showError(err instanceof Error ? err.message : '保存元数据失败');
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
      syncCourseCoverState(updated);
      showNotice('已新增并设为当前课程封面');
    } catch (err) {
      showError(err instanceof Error ? err.message : '上传封面失败');
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
      syncCourseCoverState(updated);
      setShowFramePicker(false);
      showNotice('已从课时帧新增并设为当前封面');
    } catch (err) {
      showError(err instanceof Error ? err.message : '选取课时帧封面失败');
    } finally {
      setUploadingCover(false);
    }
  }

  async function handleActivateCover(cover: CourseCover) {
    if (!course || busyCoverId || cover.is_active) return;
    setBusyCoverId(cover.id);
    setError(null);
    try {
      const updated = await api.activateCourseCover(course.id, cover.id);
      syncCourseCoverState(updated);
      showNotice('已切换为当前课程封面');
    } catch (err) {
      showError(err instanceof Error ? err.message : '切换封面失败');
    } finally {
      setBusyCoverId(null);
    }
  }

  async function handleDeleteCover(cover: CourseCover) {
    if (!course || busyCoverId) return;
    setBusyCoverId(cover.id);
    setError(null);
    try {
      const updated = await api.deleteCourseCover(course.id, cover.id);
      syncCourseCoverState(updated);
      showNotice(
        cover.is_active
          ? updated.cover_path
            ? '已删除当前封面并自动切换历史封面'
            : '已删除当前封面'
          : '已删除该历史封面',
      );
    } catch (err) {
      showError(err instanceof Error ? err.message : '删除封面失败');
    } finally {
      setBusyCoverId(null);
    }
  }

  async function handleBackdropUpload(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file || !course) return;
    setUploadingBackdrop(true);
    setError(null);
    try {
      const updated = await api.uploadCourseBackdrop(course.id, file);
      syncCourseCoverState(updated);
      showNotice('课程背景图已更新');
    } catch (err) {
      showError(err instanceof Error ? err.message : '上传背景图失败');
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
      libCtx.bumpCatalogVersion();
      toast.success(`已删除课程「${course.title}」`);
      navigate('/', { replace: true });
    } catch (err) {
      showError(err instanceof Error ? err.message : '删除课程失败');
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
      showError(err instanceof Error ? err.message : '添加章节失败');
    } finally {
      setAddingChapter(false);
    }
  }

  async function handleRenameChapter(chapter: ChapterDetail, nextTitle: string) {
    setError(null);
    try {
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
    } catch (err) {
      showError(err instanceof Error ? err.message : '重命名章节失败');
      throw err;
    }
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
      showError(err instanceof Error ? err.message : '删除章节失败');
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
      showError(err instanceof Error ? err.message : '保存章节顺序失败');
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
      showError(err instanceof Error ? err.message : '保存课时顺序失败');
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
    setError(null);
    try {
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
    } catch (err) {
      showError(err instanceof Error ? err.message : '重命名课时失败');
      throw err;
    }
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
      showError(err instanceof Error ? err.message : '删除课时失败');
    }
  }

  async function handleDragEnd(event: DragEndEvent) {
    if (!course) return;
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const result = resolveDragReorder(
      course.chapters ?? [],
      String(active.id),
      String(over.id),
    );
    if (!result) return;
    if (result.type === 'chapter') {
      await commitChapterOrder(result.chapters);
    } else {
      await commitLessonOrder(result.chapters);
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
      showError(err instanceof Error ? err.message : '检查新文件失败');
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
      showError(err instanceof Error ? err.message : '追加新文件失败');
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
      showError(err instanceof Error ? err.message : '添加视频课时失败');
    }
  }

  const effectiveLoading =
    loading || (libCtx.isProvided && libCtx.loading && libCtx.libraries.length === 0);

  if (effectiveLoading) {
    return (
      <div className="flex flex-col items-center justify-center py-24 text-ink-secondary gap-3">
        <Loader2 className="h-7 w-7 animate-spin text-accent" />
        <p className="text-sm">正在加载课程编辑器...</p>
      </div>
    );
  }

  if (!course) {
    return (
      <div className="rounded-3xl border border-status-danger/30 bg-status-danger-soft p-6 text-center max-w-lg mx-auto my-12 space-y-4 shadow-card">
        <p className="text-status-danger-ink font-medium">{error ?? '未找到课程'}</p>
        <Link
          to="/"
          className="inline-flex items-center gap-1.5 rounded-full border border-default bg-card px-4 py-2 text-sm font-medium text-ink-primary hover:bg-subtle transition-colors"
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
      {/* Sticky Top Studio Bar */}
      <header className="sticky top-14 lg:top-0 z-20 -mx-4 px-4 sm:-mx-6 sm:px-6 lg:-mx-8 lg:px-8 py-3 bg-canvas/85 backdrop-blur-md border-b border-subtle">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0">
            <Tooltip content="返回课程播放页面">
              <Link
                to={`/courses/${course.id}`}
                className="inline-flex items-center gap-1.5 rounded-full border border-subtle bg-card px-3.5 py-1.5 text-xs font-medium text-ink-secondary hover:border-default hover:text-ink-primary shadow-card transition-colors shrink-0"
              >
                <ArrowLeft className="h-3.5 w-3.5" />
                <span>返回课程播放</span>
              </Link>
            </Tooltip>
            <span className="text-ink-muted/40 hidden sm:inline">/</span>
            <h1 className="text-base sm:text-lg font-bold text-ink-primary tracking-tight truncate max-w-full sm:max-w-md">
              {course.title}
            </h1>
          </div>

          <div className="flex flex-wrap items-center gap-2 shrink-0">
            <Tooltip content="扫描原导入目录，发现新增视频并增量追加">
              <button
                type="button"
                onClick={() => void handleCheckNewFiles()}
                disabled={checkingNewFiles}
                className="inline-flex items-center gap-1.5 rounded-xl border border-default bg-card px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 shadow-card transition-colors cursor-pointer"
              >
                {checkingNewFiles ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <FolderSearch className="h-3.5 w-3.5" />
                )}
                <span>检查新文件</span>
              </button>
            </Tooltip>

            <Tooltip content="用 AI 自动生成课程简介、标签与清晰章节结构（预览后应用）">
              <button
                type="button"
                onClick={() => setShowAIModal(true)}
                className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-3.5 py-2 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover transition-colors cursor-pointer"
              >
                <Sparkles className="h-3.5 w-3.5" />
                <span>AI 智能整理</span>
              </button>
            </Tooltip>

            {!confirmDeleteCourse ? (
              <Tooltip content="删除此课程记录（不删除本地视频文件）">
                <button
                  type="button"
                  onClick={() => setConfirmDeleteCourse(true)}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-status-danger/30 bg-status-danger-soft px-3 py-2 text-xs font-medium text-status-danger-ink hover:opacity-90 transition-opacity cursor-pointer"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  <span>删除课程</span>
                </button>
              </Tooltip>
            ) : (
              <div className="inline-flex items-center gap-1.5 rounded-full border border-status-danger/35 bg-status-danger-soft px-3 py-1">
                <span className="text-xs font-medium text-status-danger-ink">确认删除?</span>
                <button
                  type="button"
                  disabled={deletingCourse}
                  onClick={() => void handleDeleteCourse()}
                  className="rounded-full bg-status-danger px-2.5 py-1 text-xs font-semibold text-ink-inverse hover:opacity-90 disabled:opacity-50 transition-opacity cursor-pointer"
                >
                  {deletingCourse ? '删除中...' : '确认'}
                </button>
                <button
                  type="button"
                  onClick={() => setConfirmDeleteCourse(false)}
                  className="rounded-full px-2.5 py-1 text-xs font-medium text-ink-secondary hover:bg-card hover:text-ink-primary transition-colors cursor-pointer"
                >
                  取消
                </button>
              </div>
            )}
          </div>
        </div>
      </header>

      {/* Feedback Banners */}
      {statusMessage && (
        <div className="flex items-center justify-between rounded-2xl border border-status-success/30 bg-status-success-soft px-4 py-3 text-sm text-status-success-ink shadow-card">
          <span className="flex items-center gap-2 font-medium">
            <Check className="h-4 w-4 text-status-success-ink shrink-0" />
            {statusMessage}
          </span>
          <button
            type="button"
            aria-label="关闭通知"
            onClick={() => setStatusMessage(null)}
            className="rounded-lg p-1 text-status-success-ink hover:bg-card/60 transition-colors cursor-pointer"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      {error && (
        <div className="flex items-center justify-between rounded-2xl border border-status-danger/30 bg-status-danger-soft px-4 py-3 text-sm text-status-danger-ink shadow-card">
          <span className="font-medium">{error}</span>
          <button
            type="button"
            aria-label="关闭错误提示"
            onClick={() => setError(null)}
            className="rounded-lg p-1 text-status-danger-ink hover:bg-card/60 transition-colors cursor-pointer"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      {/* New Files Sync Panel */}
      {newFiles !== null && (
        <section className="rounded-2xl border border-accent-ring bg-accent-soft p-5 space-y-3 shadow-card">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FilePlus2 className="h-4 w-4 text-accent-ink" />
              <h2 className="text-sm font-semibold text-ink-primary">
                新文件检查结果（{newFiles.length} 个未导入视频）
              </h2>
            </div>
            <button
              type="button"
              onClick={() => setNewFiles(null)}
              className="rounded-lg px-2 py-1 text-xs font-medium text-ink-secondary hover:bg-card hover:text-ink-primary transition-colors cursor-pointer"
            >
              关闭
            </button>
          </div>

          {newFiles.length === 0 ? (
            <p className="text-xs text-ink-secondary">当前关联目录中没有发现未导入的新视频文件。</p>
          ) : (
            <>
              <div className="max-h-56 overflow-y-auto space-y-1.5">
                {newFiles.map((file) => (
                  <label
                    key={file.id}
                    className="flex items-center gap-2.5 rounded-xl border border-subtle bg-card px-3 py-2 text-xs text-ink-primary cursor-pointer hover:border-default transition-colors"
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
                      className="h-3.5 w-3.5 rounded border-default bg-card text-accent focus:ring-2 focus:ring-accent-ring"
                    />
                    <span className="font-medium">{file.file_name}</span>
                    <span className="text-ink-muted truncate">{file.relative_path}</span>
                  </label>
                ))}
              </div>
              <div className="flex justify-end">
                <button
                  type="button"
                  disabled={appendingNewFiles}
                  onClick={() => void handleAppendNewFiles()}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
                >
                  {appendingNewFiles ? '正在追加...' : '追加到课程'}
                </button>
              </div>
            </>
          )}
        </section>
      )}

      {/* Section 1: Metadata + Cover & Backdrop */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Metadata Form */}
        <form
          onSubmit={handleSaveMetadata}
          className="lg:col-span-2 rounded-3xl bg-card border border-subtle shadow-card p-6 space-y-5"
        >
          <h2 className="text-base font-bold text-ink-primary">课程元数据</h2>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="space-y-1.5 sm:col-span-2">
              <label
                htmlFor="editor-course-title"
                className="block text-xs font-medium text-ink-secondary"
              >
                课程标题
              </label>
              <input
                id="editor-course-title"
                type="text"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                className={FIELD_INPUT_CLASS}
                required
              />
            </div>

            <div className="space-y-1.5">
              <label
                htmlFor="editor-instructor"
                className="block text-xs font-medium text-ink-secondary"
              >
                讲师
              </label>
              <input
                id="editor-instructor"
                type="text"
                value={instructor}
                onChange={(e) => setInstructor(e.target.value)}
                placeholder="讲师姓名"
                className={FIELD_INPUT_CLASS}
              />
            </div>

            <div className="space-y-1.5">
              <label
                htmlFor="editor-library"
                className="block text-xs font-medium text-ink-secondary"
              >
                所属媒体库
              </label>
              <select
                id="editor-library"
                value={libraryId}
                onChange={(e) => setLibraryId(e.target.value)}
                className={FIELD_INPUT_CLASS}
              >
                {libraries.map((lib) => (
                  <option key={lib.id} value={lib.id}>
                    {lib.name}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-1.5 sm:col-span-2">
              <label
                htmlFor="editor-overview"
                className="block text-xs font-medium text-ink-secondary"
              >
                课程简介
              </label>
              <textarea
                id="editor-overview"
                rows={4}
                value={overview}
                onChange={(e) => setOverview(e.target.value)}
                className={FIELD_INPUT_CLASS}
              />
            </div>

            <div className="space-y-2 sm:col-span-2">
              <label
                htmlFor="editor-tag-input"
                className="block text-xs font-medium text-ink-secondary"
              >
                课程标签
              </label>
              <div className="flex flex-wrap gap-1.5">
                {tags.map((t) => (
                  <span
                    key={t}
                    className="inline-flex items-center gap-1 rounded-full border border-accent-ring bg-accent-soft px-2.5 py-0.5 text-xs font-medium text-accent-ink"
                  >
                    {t}
                    <button
                      type="button"
                      aria-label={`移除标签 ${t}`}
                      onClick={() => setTags((prev) => prev.filter((x) => x !== t))}
                      className="rounded-full p-0.5 hover:bg-card/60 transition-colors cursor-pointer"
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

          <div className="flex justify-end pt-2">
            <Tooltip content="保存标题、讲师、媒体库、简介与标签">
              <button
                type="submit"
                disabled={savingMeta}
                className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-4 py-2 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
              >
                {savingMeta ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Save className="h-4 w-4" />
                )}
                <span>{savingMeta ? '保存中...' : '保存元数据'}</span>
              </button>
            </Tooltip>
          </div>
        </form>

        {/* Cover & Backdrop Panel */}
        <div className="rounded-3xl bg-card border border-subtle shadow-card p-6 space-y-5">
          <div className="space-y-3">
            <h2 className="text-base font-bold text-ink-primary">课程封面</h2>
            <div className="aspect-[16/10] w-full overflow-hidden rounded-2xl border border-subtle bg-subtle flex items-center justify-center">
              {(course.cover_path || course.first_lesson_id) && !coverFailed ? (
                <img
                  src={itemPrimaryImageUrl(course.id, course.updated_at)}
                  alt={course.title}
                  onError={() => setCoverFailed(true)}
                  className="h-full w-full object-cover"
                />
              ) : (
                <span className="text-xs text-ink-muted">暂无封面</span>
              )}
            </div>

            <div className="flex flex-wrap gap-2">
              <Tooltip content="从本地上传图片作为课程封面">
                <label className={`cursor-pointer ${SECONDARY_BTN_CLASS}`}>
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
              </Tooltip>

              <Tooltip content="直接选用某节视频的缩略图作为课程封面">
                <button
                  type="button"
                  disabled={allLessons.length === 0 || uploadingCover}
                  onClick={() => setShowFramePicker((v) => !v)}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-accent-ring bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent-ink hover:bg-accent hover:text-ink-inverse disabled:opacity-50 transition-colors cursor-pointer"
                >
                  <ImagePlus className="h-3.5 w-3.5" />
                  <span>从课时帧选取封面</span>
                </button>
              </Tooltip>

              <Tooltip content="按统一风格模板使用 AI 新增课程封面并设为当前">
                <button
                  type="button"
                  disabled={uploadingCover}
                  onClick={() => setShowAICoverModal(true)}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-accent-ring bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent-ink hover:bg-accent hover:text-ink-inverse disabled:opacity-50 transition-colors cursor-pointer"
                >
                  <Sparkles className="h-3.5 w-3.5" />
                  <span>AI 生成封面</span>
                </button>
              </Tooltip>
            </div>

            {showFramePicker && (
              <div className="rounded-2xl border border-subtle bg-subtle/60 p-3 space-y-2 max-h-48 overflow-y-auto">
                <p className="text-[11px] font-medium text-ink-muted">
                  点击下方课时，将使用其视频截图新增并设为课程封面：
                </p>
                {allLessons.map((lesson) => (
                  <button
                    key={lesson.id}
                    type="button"
                    onClick={() => void handlePickCoverFromLesson(lesson.id)}
                    className="w-full flex items-center justify-between rounded-xl border border-subtle bg-card px-2.5 py-1.5 text-left text-xs text-ink-primary hover:border-accent-ring hover:bg-accent-soft transition-colors cursor-pointer"
                  >
                    <span className="truncate">{lesson.title}</span>
                    <span className="text-[10px] font-medium text-accent-ink shrink-0 ml-2">
                      设为封面
                    </span>
                  </button>
                ))}
              </div>
            )}

            {(course.covers ?? []).length > 0 && (
              <section
                aria-label="关联封面列表"
                className="border-t border-subtle pt-4 space-y-2.5"
              >
                <div className="flex items-center justify-between gap-2">
                  <h3 className="text-xs font-semibold text-ink-secondary">
                    关联封面 ({(course.covers ?? []).length})
                  </h3>
                  <span className="text-[11px] text-ink-muted">
                    点击缩略图可随时切换
                  </span>
                </div>
                <CourseCoverGallery
                  covers={course.covers ?? []}
                  busyCoverId={busyCoverId}
                  disabled={uploadingCover}
                  compact
                  onActivate={handleActivateCover}
                  onDelete={handleDeleteCover}
                />
              </section>
            )}
          </div>

          <div className="border-t border-subtle pt-5 space-y-3">
            <h3 className="text-sm font-bold text-ink-primary">背景横幅（Backdrop）</h3>
            <div className="aspect-video w-full overflow-hidden rounded-2xl border border-subtle bg-subtle flex items-center justify-center">
              {course.backdrop_path && !backdropFailed ? (
                <img
                  src={itemBackdropImageUrl(course.id, course.updated_at)}
                  alt={`${course.title} 背景`}
                  onError={() => setBackdropFailed(true)}
                  className="h-full w-full object-cover"
                />
              ) : (
                <span className="text-xs text-ink-muted">暂无背景图</span>
              )}
            </div>
            <Tooltip content="上传宽屏背景横幅图（Jellyfin 详情页展示）">
              <label className={`cursor-pointer ${SECONDARY_BTN_CLASS}`}>
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
            </Tooltip>
          </div>
        </div>
      </div>

      {/* Section 2: Chapter & Lesson Organizer */}
      <section className="space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div>
            <h2 className="text-base font-bold text-ink-primary">章节与课时结构</h2>
            <p className="text-xs text-ink-muted">
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
              className="rounded-xl border border-default bg-card px-3.5 py-2 text-xs text-ink-primary placeholder:text-ink-muted shadow-card focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
            />
            <Tooltip content="在课程末尾新增一个章节分组">
              <button
                type="submit"
                disabled={addingChapter || !newChapterTitle.trim()}
                className="inline-flex items-center gap-1 rounded-xl bg-accent px-3.5 py-2 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
              >
                <Plus className="h-3.5 w-3.5" />
                <span>添加章节</span>
              </button>
            </Tooltip>
          </form>
        </div>

        {chapters.length === 0 ? (
          <div className="rounded-3xl border border-dashed border-default bg-card/60 p-10 text-center text-sm text-ink-muted shadow-card">
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
                    courseSourceId={course.source_id}
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
                    onManageSubtitles={(targetLesson) => setSubtitleLessonId(targetLesson.id)}
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
          className="fixed inset-0 z-50 flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
          role="dialog"
          aria-modal="true"
          aria-label="从存储添加视频"
        >
          <div className="flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float">
            <div className="flex items-center justify-between border-b border-subtle px-5 py-4">
              <div className="flex items-center gap-2.5">
                <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
                  <Video className="h-4 w-4" />
                </span>
                <h3 className="text-base font-bold text-ink-primary">从存储添加视频到章节</h3>
              </div>
              <Tooltip content="关闭弹窗">
                <button
                  type="button"
                  aria-label="关闭"
                  onClick={() => setAddVideoChapterId(null)}
                  className="rounded-lg p-1.5 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
                >
                  <X className="h-5 w-5" />
                </button>
              </Tooltip>
            </div>

            <div className="p-5 space-y-4 flex-1 overflow-y-auto">
              <div className="flex flex-col sm:flex-row gap-2">
                <select
                  aria-label="选择存储源"
                  value={selectedSourceId}
                  onChange={(e) => void handleSourceChangeInPicker(e.target.value)}
                  className="rounded-xl border border-default bg-subtle/60 px-3 py-2 text-xs text-ink-primary focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
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
                    className="flex-1 rounded-xl border border-default bg-subtle/60 px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
                  />
                  <button
                    type="submit"
                    className="rounded-xl border border-default bg-card px-3 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
                  >
                    筛选
                  </button>
                </form>
              </div>

              {loadingStorageFiles ? (
                <div className="flex items-center justify-center py-10 text-ink-secondary gap-2 text-xs">
                  <Loader2 className="h-4 w-4 animate-spin text-accent" />
                  <span>加载文件列表...</span>
                </div>
              ) : storageFiles.length === 0 ? (
                <p className="py-8 text-center text-xs text-ink-muted">
                  未找到可用的视频文件，请先在“存储与导入”中扫描存储源。
                </p>
              ) : (
                <div className="space-y-1.5">
                  {storageFiles.map((file) => (
                    <div
                      key={file.id}
                      className="flex items-center justify-between gap-3 rounded-2xl border border-subtle bg-subtle/50 px-3.5 py-2.5 hover:border-default transition-colors"
                    >
                      <div className="min-w-0">
                        <p className="text-xs font-medium text-ink-primary truncate">
                          {file.file_name}
                        </p>
                        <p className="text-[11px] text-ink-muted truncate">{file.relative_path}</p>
                      </div>
                      <button
                        type="button"
                        onClick={() => void handleAddLessonFromStorage(file)}
                        className="shrink-0 rounded-xl bg-accent px-3 py-1.5 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover transition-colors cursor-pointer"
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

      {/* AI Cover Modal */}
      <AICoverModal
        course={course}
        isOpen={showAICoverModal}
        onClose={() => setShowAICoverModal(false)}
        onCourseUpdated={(updated) => {
          syncCourseCoverState(updated);
        }}
      />

      {/* Manual Subtitle Binding Modal */}
      <SubtitleBindModal
        isOpen={Boolean(subtitleLesson)}
        lesson={subtitleLesson}
        fallbackSourceId={course.source_id}
        onClose={() => setSubtitleLessonId(null)}
        onChanged={refreshCourseSilently}
      />
    </div>
  );
}
