import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import {
  ArrowLeft,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Circle,
  Clock,
  ListVideo,
  Loader2,
  PanelRightClose,
  PanelRightOpen,
  Pencil,
  Play,
  SkipBack,
  SkipForward,
} from 'lucide-react';
import { api, itemPrimaryImageUrl } from '../api/client';
import { useToast } from '../components/Toast';
import Tooltip from '../components/Tooltip';
import VideoPlayer from '../components/VideoPlayer';
import type {
  CourseDetail,
  LessonDetail,
  LessonPlaybackInfo,
  ProgressResponse,
} from '../types/coursefin';

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
  const [togglingPlayed, setTogglingPlayed] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [collapsedChapters, setCollapsedChapters] = useState<Record<string, boolean>>({});

  const loadCourse = useCallback(async () => {
    if (!courseId) return;
    setLoadingCourse(true);
    setCourseError(null);
    setCoverFailed(false);
    setActiveLessonId(null);
    setPlayback(null);
    try {
      const data = await api.getCourse(courseId);
      setCourse(data);
      const initialId = pickInitialLessonId(data, requestedLessonId);
      setActiveLessonId(initialId);
    } catch (err) {
      setCourseError(err instanceof Error ? err.message : '加载课程详情失败');
    } finally {
      setLoadingCourse(false);
    }
  }, [courseId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    void loadCourse();
  }, [loadCourse]);

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

    api
      .getLessonPlayback(activeLessonId)
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
  }, [activeLessonId, playbackRetryNonce]);

  const selectLesson = useCallback(
    (lessonId: string) => {
      setActiveLessonId(lessonId);
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

  const handleProgressSaved = useCallback((progress: ProgressResponse) => {
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
      <div className="flex flex-col items-center justify-center py-24 text-slate-400 gap-3">
        <Loader2 className="h-7 w-7 animate-spin text-indigo-400" />
        <p className="text-sm">正在加载课程详情...</p>
      </div>
    );
  }

  if (courseError || !course) {
    return (
      <div className="rounded-2xl border border-rose-500/30 bg-rose-500/10 p-6 text-center max-w-lg mx-auto my-12 space-y-4">
        <p className="text-rose-200 font-medium">{courseError ?? '未找到课程'}</p>
        <div className="flex items-center justify-center gap-3">
          <Link
            to="/"
            className="inline-flex items-center gap-1.5 rounded-xl border border-slate-700 bg-slate-800 px-4 py-2 text-sm text-slate-200 hover:bg-slate-700 transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            <span>返回课程库</span>
          </Link>
          <button
            type="button"
            onClick={() => void loadCourse()}
            className="inline-flex items-center rounded-xl bg-rose-600 px-4 py-2 text-sm font-medium text-white hover:bg-rose-500 transition-colors"
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

  return (
    <div className="space-y-6">
      {/* Top breadcrumb & actions */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Tooltip content="返回首页课程列表">
          <Link
            to="/"
            className="inline-flex items-center gap-1.5 text-sm font-medium text-slate-400 hover:text-white transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            <span>返回课程库</span>
          </Link>
        </Tooltip>

        <div className="flex items-center gap-2.5">
          <Tooltip content={sidebarOpen ? '收起右侧章节目录以扩大播放画面' : '展开右侧章节目录'}>
            <button
              type="button"
              onClick={() => setSidebarOpen((v) => !v)}
              className="inline-flex items-center gap-1.5 rounded-xl border border-slate-800 bg-slate-900 px-3 py-1.5 text-xs font-medium text-slate-300 hover:border-slate-700 hover:text-white transition-colors"
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
              className="inline-flex items-center gap-1.5 rounded-xl border border-slate-700/80 bg-slate-800/80 px-3.5 py-1.5 text-xs font-semibold text-slate-200 hover:bg-slate-800 hover:text-white transition-colors"
            >
              <Pencil className="h-3.5 w-3.5" />
              <span>编辑课程</span>
            </Link>
          </Tooltip>
        </div>
      </div>

      {/* Course Metadata Header */}
      <section className="rounded-2xl border border-slate-800/90 bg-slate-900/70 p-4 sm:p-5">
        <div className="flex flex-col sm:flex-row gap-5">
          <div className="w-full sm:w-48 aspect-video sm:aspect-[16/10] shrink-0 overflow-hidden rounded-xl bg-slate-800 border border-slate-800">
            {hasCover ? (
              <img
                src={itemPrimaryImageUrl(course.id, course.updated_at)}
                alt={course.title}
                onError={() => setCoverFailed(true)}
                className="h-full w-full object-cover"
              />
            ) : (
              <div className="h-full w-full bg-gradient-to-br from-indigo-600 via-indigo-700 to-slate-900 flex items-center justify-center">
                <span className="text-3xl font-bold text-white/90">
                  {course.title.trim().slice(0, 1).toUpperCase() || '课'}
                </span>
              </div>
            )}
          </div>

          <div className="flex-1 min-w-0 flex flex-col justify-between gap-3">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2.5">
                <h1 className="text-xl sm:text-2xl font-bold text-white tracking-tight">
                  {course.title}
                </h1>
                {course.played && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/20 border border-emerald-500/40 px-2.5 py-0.5 text-xs font-medium text-emerald-300">
                    <CheckCircle2 className="h-3.5 w-3.5" />
                    已完成
                  </span>
                )}
              </div>

              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-slate-400">
                {course.instructor && <span>讲师：{course.instructor}</span>}
                <span>
                  共 {course.chapter_count} 章 · {course.lesson_count} 节
                </span>
                {totalDurationText && (
                  <span className="inline-flex items-center gap-1">
                    <Clock className="h-3.5 w-3.5" />
                    {totalDurationText}
                  </span>
                )}
              </div>

              {course.overview && (
                <p className="text-sm text-slate-300 leading-relaxed line-clamp-2">
                  {course.overview}
                </p>
              )}

              {course.tags && course.tags.length > 0 && (
                <div className="flex flex-wrap gap-1.5 pt-0.5">
                  {course.tags.map((tag) => (
                    <span
                      key={tag}
                      className="rounded-md border border-slate-700/70 bg-slate-800/80 px-2 py-0.5 text-[11px] text-slate-300"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              )}
            </div>

            <div className="space-y-1.5 max-w-md">
              <div className="flex items-center justify-between text-xs text-slate-400">
                <span>
                  已学 {course.played_count} / {course.lesson_count} 节
                </span>
                <span className="font-semibold text-slate-200">{coursePct}%</span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-slate-800">
                <div
                  className={`h-full rounded-full transition-all ${
                    course.played ? 'bg-emerald-500' : 'bg-indigo-500'
                  }`}
                  style={{ width: `${coursePct}%` }}
                />
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Player + Collapsible Playlist Layout */}
      <div
        className={`grid gap-6 items-start ${
          sidebarOpen ? 'lg:grid-cols-[minmax(0,1fr)_360px]' : 'grid-cols-1'
        }`}
      >
        {/* Main Player Column */}
        <div className="space-y-4 min-w-0">
          {flatLessons.length === 0 ? (
            <div className="aspect-video w-full rounded-2xl border border-dashed border-slate-800 bg-slate-900/50 flex flex-col items-center justify-center text-center p-6">
              <ListVideo className="h-10 w-10 text-slate-500 mb-3" />
              <p className="text-base font-medium text-slate-200">该课程暂无课时</p>
              <p className="mt-1 text-xs text-slate-400">
                请前往课程编辑页面添加章节和视频文件。
              </p>
            </div>
          ) : loadingPlayback ? (
            <div className="aspect-video w-full rounded-2xl border border-slate-800 bg-slate-900/90 flex flex-col items-center justify-center text-slate-400 gap-3">
              <Loader2 className="h-7 w-7 animate-spin text-indigo-400" />
              <p className="text-sm">正在准备播放地址...</p>
            </div>
          ) : playbackError ? (
            <div className="aspect-video w-full rounded-2xl border border-rose-500/30 bg-rose-500/10 flex flex-col items-center justify-center p-6 text-center gap-3">
              <p className="text-sm font-medium text-rose-200">{playbackError}</p>
              {activeLessonId && (
                <button
                  type="button"
                  onClick={() => setPlaybackRetryNonce((n) => n + 1)}
                  className="rounded-xl bg-rose-600 px-4 py-2 text-xs font-semibold text-white hover:bg-rose-500 transition-colors"
                >
                  重新获取
                </button>
              )}
            </div>
          ) : playback ? (
            <VideoPlayer
              playback={playback}
              lessonTitle={activeEntry?.lesson.title}
              onProgressSaved={handleProgressSaved}
              onEnded={handleVideoEnded}
            />
          ) : null}

          {/* Current Lesson Action Strip */}
          {activeEntry && (
            <div className="rounded-2xl border border-slate-800/90 bg-slate-900/70 p-4 space-y-3">
              <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                <div className="min-w-0 space-y-0.5">
                  <p className="text-xs font-medium text-indigo-300">{activeEntry.chapterTitle}</p>
                  <h2 className="text-base sm:text-lg font-semibold text-white truncate">
                    {activeEntry.lesson.title}
                  </h2>
                </div>

                <div className="flex flex-wrap items-center gap-2 shrink-0">
                  <Tooltip content={prevLesson ? `切换到上一节：${prevLesson.title}` : '已是第一节'}>
                    <button
                      type="button"
                      disabled={!prevLesson}
                      onClick={() => prevLesson && selectLesson(prevLesson.id)}
                      className="inline-flex items-center gap-1 rounded-xl border border-slate-700/80 bg-slate-800/70 px-3 py-1.5 text-xs font-medium text-slate-200 hover:bg-slate-800 disabled:opacity-40 disabled:pointer-events-none transition-colors"
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
                      className="inline-flex items-center gap-1 rounded-xl border border-slate-700/80 bg-slate-800/70 px-3 py-1.5 text-xs font-medium text-slate-200 hover:bg-slate-800 disabled:opacity-40 disabled:pointer-events-none transition-colors"
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
                      className={`inline-flex items-center gap-1.5 rounded-xl px-3.5 py-1.5 text-xs font-semibold transition-colors disabled:opacity-50 ${
                        activeEntry.lesson.played
                          ? 'border border-emerald-500/40 bg-emerald-500/15 text-emerald-300 hover:bg-emerald-500/25'
                          : 'bg-indigo-600 text-white hover:bg-indigo-500'
                      }`}
                    >
                      <CheckCircle2 className="h-3.5 w-3.5" />
                      <span>{activeEntry.lesson.played ? '标记未看' : '标记已看'}</span>
                    </button>
                  </Tooltip>
                </div>
              </div>

              {actionError && (
                <p className="text-xs text-rose-400">{actionError}</p>
              )}

              {activeEntry.lesson.overview && (
                <p className="text-sm text-slate-400 leading-relaxed border-t border-slate-800/80 pt-3">
                  {activeEntry.lesson.overview}
                </p>
              )}
            </div>
          )}
        </div>

        {/* Collapsible Chapter & Lesson Playlist Sidebar */}
        {sidebarOpen && (
          <aside
            aria-label="课程目录"
            className="rounded-2xl border border-slate-800/90 bg-slate-900/70 overflow-hidden"
          >
            <div className="flex items-center justify-between border-b border-slate-800/90 px-4 py-3">
              <div className="flex items-center gap-2">
                <ListVideo className="h-4 w-4 text-indigo-400" />
                <h2 className="text-sm font-semibold text-white">课程目录</h2>
              </div>
              <span className="text-xs text-slate-400">
                {course.played_count}/{course.lesson_count} 节
              </span>
            </div>

            <div className="max-h-[640px] overflow-y-auto divide-y divide-slate-800/70">
              {(course.chapters ?? []).map((chapter) => {
                const isCollapsed = Boolean(collapsedChapters[chapter.id]);
                const lessons = chapter.lessons ?? [];
                return (
                  <div key={chapter.id}>
                    <button
                      type="button"
                      onClick={() => toggleChapterCollapse(chapter.id)}
                      aria-expanded={!isCollapsed}
                      className="w-full flex items-center justify-between gap-2 px-4 py-3 text-left bg-slate-900/90 hover:bg-slate-800/60 transition-colors"
                    >
                      <div className="flex items-center gap-2 min-w-0">
                        {isCollapsed ? (
                          <ChevronRight className="h-4 w-4 text-slate-400 shrink-0" />
                        ) : (
                          <ChevronDown className="h-4 w-4 text-slate-400 shrink-0" />
                        )}
                        <span className="text-xs font-semibold text-slate-200 truncate">
                          {chapter.title}
                        </span>
                      </div>
                      <span className="text-[11px] text-slate-400 shrink-0">
                        {chapter.played_count}/{lessons.length}
                      </span>
                    </button>

                    {!isCollapsed && (
                      <ul className="divide-y divide-slate-800/40 bg-slate-950/40">
                        {lessons.map((lesson) => {
                          const isActive = lesson.id === activeLessonId;
                          return (
                            <li key={lesson.id}>
                              <button
                                type="button"
                                onClick={() => selectLesson(lesson.id)}
                                aria-current={isActive ? 'true' : undefined}
                                className={`w-full flex items-start gap-2.5 px-4 py-2.5 text-left transition-colors ${
                                  isActive
                                    ? 'bg-indigo-500/15 border-l-2 border-indigo-400 text-white'
                                    : 'hover:bg-slate-800/50 text-slate-300 border-l-2 border-transparent'
                                }`}
                              >
                                <span className="mt-0.5 shrink-0">
                                  {lesson.played ? (
                                    <CheckCircle2 className="h-4 w-4 text-emerald-400" />
                                  ) : isActive ? (
                                    <Play className="h-4 w-4 text-indigo-400 fill-current" />
                                  ) : (
                                    <Circle className="h-4 w-4 text-slate-600" />
                                  )}
                                </span>
                                <span className="flex-1 min-w-0">
                                  <span
                                    className={`block text-xs leading-snug line-clamp-2 ${
                                      isActive ? 'font-semibold text-white' : 'font-medium'
                                    }`}
                                  >
                                    {lesson.title}
                                  </span>
                                </span>
                                <span className="shrink-0 text-[11px] tabular-nums text-slate-500 mt-0.5">
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
          </aside>
        )}
      </div>
    </div>
  );
}
