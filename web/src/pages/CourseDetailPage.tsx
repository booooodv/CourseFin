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
      <div className="flex flex-col items-center justify-center py-24 text-ink-secondary gap-3">
        <Loader2 className="h-7 w-7 animate-spin text-accent" />
        <p className="text-sm">正在加载课程详情...</p>
      </div>
    );
  }

  if (courseError || !course) {
    return (
      <div className="rounded-3xl border border-status-danger/30 bg-status-danger-soft p-6 text-center max-w-lg mx-auto my-12 space-y-4 shadow-card">
        <p className="text-status-danger-ink font-medium">{courseError ?? '未找到课程'}</p>
        <div className="flex items-center justify-center gap-3">
          <Link
            to="/"
            className="inline-flex items-center gap-1.5 rounded-full border border-default bg-card px-4 py-2 text-sm font-medium text-ink-primary hover:bg-subtle transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            <span>返回课程库</span>
          </Link>
          <button
            type="button"
            onClick={() => void loadCourse()}
            className="inline-flex items-center rounded-full bg-status-danger px-4 py-2 text-sm font-medium text-ink-inverse hover:opacity-90 transition-opacity cursor-pointer"
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
    <div className="space-y-5">
      {/* Compact Top Header Strip */}
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2.5 min-w-0">
          <Tooltip content="返回首页课程列表">
            <Link
              to="/"
              className="inline-flex items-center gap-1.5 rounded-full border border-subtle bg-card px-3.5 py-1.5 text-xs font-medium text-ink-secondary hover:border-default hover:text-ink-primary shadow-card transition-colors shrink-0"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              <span>返回课程库</span>
            </Link>
          </Tooltip>

          <h1 className="text-lg sm:text-xl font-bold text-ink-primary tracking-tight truncate max-w-full sm:max-w-md lg:max-w-xl">
            {course.title}
          </h1>

          {course.played && (
            <span className="inline-flex items-center gap-1 rounded-full bg-status-success-soft px-2.5 py-0.5 text-xs font-semibold text-status-success-ink shrink-0">
              <CheckCircle2 className="h-3.5 w-3.5" />
              <span>已完成</span>
            </span>
          )}

          <span className="inline-flex items-center rounded-full border border-subtle bg-subtle px-2.5 py-0.5 text-xs font-medium tabular-nums text-ink-secondary shrink-0">
            已学 {course.played_count}/{course.lesson_count} 节 · {coursePct}%
          </span>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <Tooltip content={sidebarOpen ? '收起右侧章节目录以扩大播放画面' : '展开右侧章节目录'}>
            <button
              type="button"
              onClick={() => setSidebarOpen((v) => !v)}
              className="inline-flex items-center gap-1.5 rounded-full border border-subtle bg-card px-3.5 py-1.5 text-xs font-medium text-ink-secondary hover:border-default hover:text-ink-primary shadow-card transition-colors cursor-pointer"
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
              className="inline-flex items-center gap-1.5 rounded-full border border-default bg-card px-3.5 py-1.5 text-xs font-semibold text-ink-primary hover:bg-subtle shadow-card transition-colors"
            >
              <Pencil className="h-3.5 w-3.5" />
              <span>编辑课程</span>
            </Link>
          </Tooltip>
        </div>
      </header>

      {/* Theater-First Player + Sticky Playlist Layout */}
      <div
        className={`grid gap-6 items-start ${
          sidebarOpen ? 'lg:grid-cols-[minmax(0,1fr)_320px] xl:grid-cols-[minmax(0,1fr)_360px]' : 'grid-cols-1'
        }`}
      >
        {/* Left Theater Column: (1) VideoPlayer, (2) Lesson Action Card, (3) Course Overview Card */}
        <div className="space-y-4 min-w-0">
          {flatLessons.length === 0 ? (
            <div className="aspect-video w-full rounded-3xl border border-dashed border-default bg-card shadow-card flex flex-col items-center justify-center text-center p-6">
              <ListVideo className="h-10 w-10 text-ink-muted mb-3" />
              <p className="text-base font-semibold text-ink-primary">该课程暂无课时</p>
              <p className="mt-1 text-xs text-ink-secondary">
                请前往课程编辑页面添加章节和视频文件。
              </p>
            </div>
          ) : loadingPlayback ? (
            <div className="aspect-video w-full rounded-3xl border border-subtle bg-card shadow-card flex flex-col items-center justify-center text-ink-secondary gap-3">
              <Loader2 className="h-7 w-7 animate-spin text-accent" />
              <p className="text-sm">正在准备播放地址...</p>
            </div>
          ) : playbackError ? (
            <div className="aspect-video w-full rounded-3xl border border-status-danger/30 bg-status-danger-soft flex flex-col items-center justify-center p-6 text-center gap-3 shadow-card">
              <p className="text-sm font-medium text-status-danger-ink">{playbackError}</p>
              {activeLessonId && (
                <button
                  type="button"
                  onClick={() => setPlaybackRetryNonce((n) => n + 1)}
                  className="rounded-full bg-status-danger px-4 py-2 text-xs font-semibold text-ink-inverse hover:opacity-90 transition-opacity cursor-pointer"
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

          {/* Current Lesson Action Card */}
          {activeEntry && (
            <section className="rounded-3xl border border-subtle bg-card shadow-card p-5 space-y-3">
              <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                <div className="min-w-0 space-y-0.5">
                  <p className="text-xs font-semibold text-accent-ink">{activeEntry.chapterTitle}</p>
                  <h2 className="text-base sm:text-lg font-bold text-ink-primary line-clamp-2 break-words">
                    {activeEntry.lesson.title}
                  </h2>
                </div>

                <div className="flex flex-wrap items-center gap-2 shrink-0">
                  <Tooltip content={prevLesson ? `切换到上一节：${prevLesson.title}` : '已是第一节'}>
                    <button
                      type="button"
                      disabled={!prevLesson}
                      onClick={() => prevLesson && selectLesson(prevLesson.id)}
                      className="inline-flex items-center gap-1 rounded-full border border-default bg-subtle px-3.5 py-1.5 text-xs font-medium text-ink-primary hover:bg-muted disabled:opacity-40 disabled:pointer-events-none transition-colors cursor-pointer"
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
                      className="inline-flex items-center gap-1 rounded-full border border-default bg-subtle px-3.5 py-1.5 text-xs font-medium text-ink-primary hover:bg-muted disabled:opacity-40 disabled:pointer-events-none transition-colors cursor-pointer"
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
                      className={`inline-flex items-center gap-1.5 rounded-full px-4 py-1.5 text-xs font-semibold transition-all disabled:opacity-50 cursor-pointer ${
                        activeEntry.lesson.played
                          ? 'border border-status-success/40 bg-status-success-soft text-status-success-ink hover:opacity-90'
                          : 'bg-accent text-ink-inverse hover:bg-accent-hover shadow-accent'
                      }`}
                    >
                      <CheckCircle2 className="h-3.5 w-3.5" />
                      <span>{activeEntry.lesson.played ? '标记未看' : '标记已看'}</span>
                    </button>
                  </Tooltip>
                </div>
              </div>

              {actionError && (
                <p className="text-xs text-status-danger-ink">{actionError}</p>
              )}

              {activeEntry.lesson.overview && (
                <p className="text-sm text-ink-secondary leading-relaxed border-t border-subtle pt-3">
                  {activeEntry.lesson.overview}
                </p>
              )}
            </section>
          )}

          {/* Course Overview & Metadata Card (below video for theater-first flow) */}
          <section className="rounded-3xl border border-subtle bg-card shadow-card p-5">
            <div className="flex flex-col sm:flex-row gap-5">
              <div className="w-full sm:w-44 aspect-video sm:aspect-[16/10] shrink-0 overflow-hidden rounded-2xl bg-subtle border border-subtle">
                {hasCover ? (
                  <img
                    src={itemPrimaryImageUrl(course.id, course.updated_at)}
                    alt={course.title}
                    onError={() => setCoverFailed(true)}
                    className="h-full w-full object-cover"
                  />
                ) : (
                  <div className="h-full w-full bg-gradient-to-br from-blue-500 to-sky-600 flex items-center justify-center">
                    <span className="text-3xl font-bold text-white/95">
                      {course.title.trim().slice(0, 1).toUpperCase() || '课'}
                    </span>
                  </div>
                )}
              </div>

              <div className="flex-1 min-w-0 flex flex-col justify-between gap-3">
                <div className="space-y-2">
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-ink-secondary">
                    {course.instructor && <span>讲师：{course.instructor}</span>}
                    <span>
                      共 {course.chapter_count} 章 · {course.lesson_count} 节
                    </span>
                    {totalDurationText && (
                      <span className="inline-flex items-center gap-1">
                        <Clock className="h-3.5 w-3.5 text-ink-muted" />
                        {totalDurationText}
                      </span>
                    )}
                  </div>

                  {course.overview && (
                    <p className="text-sm text-ink-secondary leading-relaxed">
                      {course.overview}
                    </p>
                  )}

                  {course.tags && course.tags.length > 0 && (
                    <div className="flex flex-wrap gap-1.5 pt-0.5">
                      {course.tags.map((tag) => (
                        <span
                          key={tag}
                          className="rounded-full border border-subtle bg-subtle px-2.5 py-0.5 text-[11px] font-medium text-ink-secondary"
                        >
                          {tag}
                        </span>
                      ))}
                    </div>
                  )}
                </div>

                <div className="space-y-1.5 max-w-md pt-1">
                  <div className="flex items-center justify-between text-xs text-ink-secondary">
                    <span>
                      已学 {course.played_count} / {course.lesson_count} 节
                    </span>
                    <span className="font-semibold tabular-nums text-ink-primary">{coursePct}%</span>
                  </div>
                  <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                    <div
                      className={`h-full rounded-full transition-all ${
                        course.played ? 'bg-status-success' : 'bg-accent'
                      }`}
                      style={{ width: `${coursePct}%` }}
                    />
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>

        {/* Right Sticky Chapter & Lesson Playlist Sidebar */}
        {sidebarOpen && (
          <aside
            aria-label="课程目录"
            className="rounded-3xl bg-card border border-subtle shadow-card overflow-hidden lg:sticky lg:top-6"
          >
            <div className="flex items-center justify-between border-b border-subtle px-4 py-3.5">
              <div className="flex items-center gap-2">
                <ListVideo className="h-4 w-4 text-accent" />
                <h2 className="text-sm font-semibold text-ink-primary">课程目录</h2>
              </div>
              <span className="text-xs tabular-nums text-ink-muted">
                {course.played_count}/{course.lesson_count} 节
              </span>
            </div>

            <div className="max-h-[calc(100vh-8rem)] overflow-y-auto divide-y divide-subtle">
              {(course.chapters ?? []).map((chapter) => {
                const isCollapsed = Boolean(collapsedChapters[chapter.id]);
                const lessons = chapter.lessons ?? [];
                return (
                  <div key={chapter.id}>
                    <button
                      type="button"
                      onClick={() => toggleChapterCollapse(chapter.id)}
                      aria-expanded={!isCollapsed}
                      className="w-full flex items-center justify-between gap-2 px-4 py-3 text-left bg-subtle/70 hover:bg-subtle transition-colors cursor-pointer"
                    >
                      <div className="flex items-center gap-2 min-w-0">
                        {isCollapsed ? (
                          <ChevronRight className="h-4 w-4 text-ink-muted shrink-0" />
                        ) : (
                          <ChevronDown className="h-4 w-4 text-ink-muted shrink-0" />
                        )}
                        <span className="text-xs font-semibold text-ink-primary truncate">
                          {chapter.title}
                        </span>
                      </div>
                      <span className="text-[11px] tabular-nums text-ink-muted shrink-0">
                        {chapter.played_count}/{lessons.length}
                      </span>
                    </button>

                    {!isCollapsed && (
                      <ul className="divide-y divide-subtle bg-card">
                        {lessons.map((lesson) => {
                          const isActive = lesson.id === activeLessonId;
                          return (
                            <li key={lesson.id}>
                              <button
                                type="button"
                                onClick={() => selectLesson(lesson.id)}
                                aria-current={isActive ? 'true' : undefined}
                                className={`relative w-full flex items-start gap-2.5 px-4 py-2.5 text-left transition-colors cursor-pointer ${
                                  isActive
                                    ? 'bg-accent-soft text-ink-primary'
                                    : 'hover:bg-subtle/70 text-ink-secondary'
                                }`}
                              >
                                {isActive && (
                                  <span
                                    aria-hidden="true"
                                    className="absolute inset-y-1.5 left-0 w-1 rounded-r-full bg-accent"
                                  />
                                )}
                                <span className="mt-0.5 shrink-0">
                                  {lesson.played ? (
                                    <CheckCircle2 className="h-4 w-4 text-status-success" />
                                  ) : isActive ? (
                                    <Play className="h-4 w-4 text-accent fill-current" />
                                  ) : (
                                    <Circle className="h-4 w-4 text-ink-muted/60" />
                                  )}
                                </span>
                                <span className="flex-1 min-w-0">
                                  <span
                                    className={`block text-xs leading-snug line-clamp-2 ${
                                      isActive ? 'font-bold text-ink-primary' : 'font-medium text-ink-secondary'
                                    }`}
                                  >
                                    {lesson.title}
                                  </span>
                                </span>
                                <span className="shrink-0 text-[11px] tabular-nums text-ink-muted mt-0.5">
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
