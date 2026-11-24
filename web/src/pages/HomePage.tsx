import type { FormEvent, JSX } from 'react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import {
  ArrowUpDown,
  BookOpen,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock,
  FolderInput,
  Loader2,
  Moon,
  Pencil,
  Play,
  Plus,
  Search,
  Sparkles,
  Sun,
  Tag as TagIcon,
  Trash2,
  X,
} from 'lucide-react';
import { api, itemPrimaryImageUrl } from '../api/client';
import DeleteLibraryModal, { type DeleteLibraryMode } from '../components/DeleteLibraryModal';
import { useToast } from '../components/Toast';
import Tooltip from '../components/Tooltip';
import { useLibrary } from '../contexts/LibraryContext';
import type {
  CourseSummary,
  HomeResponse,
  LessonWithContext,
  Library,
  Tag,
} from '../types/coursefin';

const TICKS_PER_MS = 10000;

type SortOption = 'default' | 'updated' | 'progress' | 'title';

const COVER_GRADIENTS = [
  'from-blue-500 via-blue-600 to-sky-700',
  'from-cyan-500 via-teal-600 to-emerald-700',
  'from-emerald-500 via-teal-600 to-cyan-700',
  'from-violet-500 via-purple-600 to-fuchsia-700',
  'from-amber-500 via-orange-500 to-rose-600',
  'from-rose-500 via-pink-600 to-purple-700',
];

function gradientForId(id: string): string {
  let hash = 0;
  for (let i = 0; i < id.length; i++) {
    hash = (hash * 31 + id.charCodeAt(i)) | 0;
  }
  return COVER_GRADIENTS[Math.abs(hash) % COVER_GRADIENTS.length];
}

function calcLessonProgressPct(positionTicks: number, durationMs: number): number {
  if (!durationMs || durationMs <= 0) return 0;
  const pct = Math.round((positionTicks / (durationMs * TICKS_PER_MS)) * 100);
  return Math.max(0, Math.min(100, pct));
}

function formatDuration(durationMs: number): string {
  if (!durationMs || durationMs <= 0) return '';
  const totalMinutes = Math.round(durationMs / 60000);
  if (totalMinutes < 60) return `${Math.max(1, totalMinutes)} 分钟`;
  const hours = Math.floor(totalMinutes / 60);
  const mins = totalMinutes % 60;
  return mins > 0 ? `${hours} 小时 ${mins} 分钟` : `${hours} 小时`;
}

function getTimeGreeting(date = new Date()): { label: string; isNight: boolean } {
  const hour = date.getHours();
  if (hour >= 5 && hour < 9) return { label: '早上好', isNight: false };
  if (hour >= 9 && hour < 12) return { label: '上午好', isNight: false };
  if (hour >= 12 && hour < 18) return { label: '下午好', isNight: false };
  return { label: '晚上好', isNight: true };
}

interface CoursePosterProps {
  courseId: string;
  title: string;
  hasCover: boolean;
  tag?: string;
  hoverScaleClass?: string;
}

function CoursePoster({
  courseId,
  title,
  hasCover,
  tag,
  hoverScaleClass = 'group-hover:scale-105',
}: CoursePosterProps) {
  const [imgFailed, setImgFailed] = useState(false);

  useEffect(() => {
    setImgFailed(false);
  }, [courseId, tag]);

  const initial = title.trim().slice(0, 1).toUpperCase() || '课';
  const gradient = gradientForId(courseId);

  if (!hasCover || imgFailed) {
    return (
      <div
        className={`relative h-full w-full bg-gradient-to-br ${gradient} flex flex-col items-center justify-center p-4 text-center overflow-hidden`}
      >
        <div
          aria-hidden="true"
          className="pointer-events-none absolute -top-10 -right-10 h-28 w-28 rounded-full bg-white/15 blur-xl"
        />
        <span className="relative flex h-12 w-12 items-center justify-center rounded-2xl bg-white/15 text-2xl font-bold text-white shadow-sm backdrop-blur-xs">
          {initial}
        </span>
        <span className="relative mt-2 line-clamp-1 max-w-[85%] text-xs font-medium text-white/85">
          {title}
        </span>
      </div>
    );
  }

  return (
    <img
      src={itemPrimaryImageUrl(courseId, tag)}
      alt={title}
      onError={() => setImgFailed(true)}
      className={`h-full w-full object-cover transition-transform duration-300 ${hoverScaleClass}`}
    />
  );
}

interface SpotlightHeroProps {
  items: LessonWithContext[];
  activeIdx: number;
  onSelectIdx: (updater: number | ((prev: number) => number)) => void;
}

function SpotlightHero({ items, activeIdx, onSelectIdx }: SpotlightHeroProps): JSX.Element | null {
  const safeIdx = items.length > 0 ? Math.min(activeIdx, items.length - 1) : 0;
  const focused = items[safeIdx];
  if (!focused) return null;

  const pct = calcLessonProgressPct(focused.position_ticks, focused.duration_ms);
  const hasThumb = Boolean(focused.cover_path || focused.thumbnail_path || focused.course_cover_path);
  const focusHref = `/courses/${focused.course_id}?lesson=${focused.id}`;
  const durationText = formatDuration(focused.duration_ms);
  const hasQuickSwitch = items.length > 1;

  const posterLink = (
    <Link
      to={focusHref}
      aria-label={`进入 ${focused.course_title} 播放页`}
      className="group/poster relative block aspect-video w-full overflow-hidden rounded-2xl bg-subtle"
    >
      <CoursePoster
        courseId={focused.cover_path || focused.thumbnail_path ? focused.id : focused.course_id}
        title={focused.course_title}
        hasCover={hasThumb}
        tag={focused.updated_at}
        hoverScaleClass="group-hover/poster:scale-105"
      />
      <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/70 via-black/15 to-transparent" />

      <span
        aria-hidden="true"
        className="absolute right-4 bottom-5 flex h-12 w-12 items-center justify-center rounded-full bg-accent text-ink-inverse shadow-accent transition-transform duration-200 group-hover/poster:scale-110"
      >
        <Play className="h-5 w-5 fill-current ml-0.5" />
      </span>

      <div className="absolute bottom-4 left-3.5 flex items-center gap-2">
        <span className="rounded-full bg-black/65 backdrop-blur-md px-2.5 py-0.5 text-[11px] font-semibold text-white tabular-nums">
          {pct}%
        </span>
      </div>

      <div className="absolute bottom-0 left-0 right-0 h-1.5 bg-white/20">
        <div
          className="h-full bg-accent transition-all duration-300"
          style={{ width: `${pct}%` }}
        />
      </div>
    </Link>
  );

  const focusDetails = (
    <div className="space-y-4 min-w-0">
      <div className="flex flex-wrap items-center gap-2">
        <span className="inline-flex items-center rounded-full bg-accent-soft px-3 py-1 text-xs font-semibold text-accent-ink max-w-full truncate">
          {focused.course_title}
        </span>
        {focused.chapter_title && (
          <span className="text-xs font-medium text-ink-muted truncate">{focused.chapter_title}</span>
        )}
      </div>

      <h3 className="text-lg sm:text-xl font-bold text-ink-primary leading-snug">
        <Link to={focusHref} className="hover:text-accent-ink transition-colors line-clamp-2">
          {focused.title}
        </Link>
      </h3>

      <div className="space-y-2">
        <div className="flex items-center justify-between text-xs text-ink-muted">
          <span>已学习 {pct}%</span>
          {durationText && (
            <span className="inline-flex items-center gap-1 tabular-nums">
              <Clock className="h-3.5 w-3.5" />
              {durationText}
            </span>
          )}
        </div>
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
          <div
            className="h-full rounded-full bg-accent transition-all duration-300"
            style={{ width: `${pct}%` }}
          />
        </div>
      </div>

      <div className="pt-1 flex flex-wrap items-center gap-3">
        <Link
          to={focusHref}
          className="inline-flex items-center gap-2 rounded-full bg-accent px-5 py-2.5 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover transition-all"
        >
          <Play className="h-4 w-4 fill-current" />
          <span>继续播放</span>
        </Link>
      </div>
    </div>
  );

  return (
    <section aria-labelledby="continue-watching-heading" className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
            <Play className="h-4 w-4 fill-current" />
          </span>
          <h2 id="continue-watching-heading" className="text-lg font-bold tracking-tight text-ink-primary">
            继续学习
          </h2>
          <span className="rounded-full bg-subtle px-2.5 py-0.5 text-xs font-semibold text-ink-muted tabular-nums">
            {items.length} 节进行中
          </span>
        </div>
      </div>

      <div className="rounded-3xl border border-subtle bg-card p-5 sm:p-6 shadow-card">
        {!hasQuickSwitch ? (
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-center">
            <div className="lg:col-span-5">{posterLink}</div>
            <div className="lg:col-span-7">{focusDetails}</div>
          </div>
        ) : (
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-stretch">
            <div className="lg:col-span-7 xl:col-span-8 flex flex-col justify-center">
              <div className="grid grid-cols-1 md:grid-cols-12 gap-5 items-center">
                <div className="md:col-span-6">{posterLink}</div>
                <div className="md:col-span-6">{focusDetails}</div>
              </div>
            </div>

            <div className="lg:col-span-5 xl:col-span-4 flex flex-col border-t pt-5 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-6 border-subtle min-w-0">
              <div className="flex items-center justify-between mb-3 shrink-0">
                <span className="text-xs font-semibold uppercase tracking-wider text-ink-muted">
                  进行中列表
                </span>
                <div className="flex items-center gap-1.5">
                  <span className="mr-1 text-xs font-semibold text-ink-primary tabular-nums">
                    {safeIdx + 1} / {items.length}
                  </span>
                  <Tooltip content="上一个进行中课时">
                    <button
                      type="button"
                      aria-label="上一个进行中课时"
                      onClick={() => onSelectIdx((i) => (i - 1 + items.length) % items.length)}
                      className="flex h-7 w-7 items-center justify-center rounded-full border border-subtle bg-subtle text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors cursor-pointer"
                    >
                      <ChevronLeft className="h-4 w-4" />
                    </button>
                  </Tooltip>
                  <Tooltip content="下一个进行中课时">
                    <button
                      type="button"
                      aria-label="下一个进行中课时"
                      onClick={() => onSelectIdx((i) => (i + 1) % items.length)}
                      className="flex h-7 w-7 items-center justify-center rounded-full border border-subtle bg-subtle text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors cursor-pointer"
                    >
                      <ChevronRight className="h-4 w-4" />
                    </button>
                  </Tooltip>
                </div>
              </div>

              <div className="max-h-[296px] overflow-y-auto space-y-1.5 pr-1">
                {items.map((item, idx) => {
                  const isSelected = idx === safeIdx;
                  const itemPct = calcLessonProgressPct(item.position_ticks, item.duration_ms);
                  const itemHref = `/courses/${item.course_id}?lesson=${item.id}`;
                  return (
                    <div
                      key={item.id}
                      className={`relative group/item flex items-center gap-2 rounded-2xl pr-2 transition-colors ${
                        isSelected
                          ? 'bg-accent-soft text-ink-primary'
                          : 'text-ink-secondary hover:bg-subtle hover:text-ink-primary'
                      }`}
                    >
                      {isSelected && (
                        <span
                          aria-hidden="true"
                          className="absolute left-0 top-2.5 bottom-2.5 w-[3px] rounded-full bg-accent"
                        />
                      )}
                      <button
                        type="button"
                        aria-current={isSelected ? 'true' : undefined}
                        onClick={() => onSelectIdx(idx)}
                        className="flex-1 min-w-0 pl-3.5 py-2.5 text-left cursor-pointer"
                      >
                        <div className="flex items-center justify-between gap-2">
                          <span className="truncate text-[11px] font-semibold text-accent-ink">
                            {item.course_title}
                          </span>
                          <span className="shrink-0 text-[11px] font-semibold text-ink-muted tabular-nums">
                            {itemPct}%
                          </span>
                        </div>
                        <p
                          className={`mt-0.5 truncate text-xs ${
                            isSelected ? 'font-semibold text-ink-primary' : 'font-medium text-ink-secondary'
                          }`}
                        >
                          {item.title}
                        </p>
                      </button>

                      <Tooltip content={`直接播放「${item.title}」`}>
                        <Link
                          to={itemHref}
                          aria-label={`直达播放 ${item.course_title} 第 ${idx + 1} 项`}
                          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-subtle bg-card text-ink-secondary hover:bg-accent hover:text-ink-inverse hover:border-transparent transition-colors"
                        >
                          <Play className="h-3 w-3 fill-current ml-0.5" />
                        </Link>
                      </Tooltip>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}

function CourseCard({ course }: { course: CourseSummary }): JSX.Element {
  const pct = Math.max(0, Math.min(100, Math.round(course.played_percentage || 0)));
  const hasCover = Boolean(course.cover_path || course.first_lesson_id);
  const durationText = formatDuration(course.total_duration_ms);

  return (
    <article className="group flex flex-col overflow-hidden rounded-3xl border border-subtle bg-card shadow-card hover:shadow-card-hover hover:-translate-y-0.5 transition-all duration-200">
      <div className="p-2.5 pb-0">
        <Link
          to={`/courses/${course.id}`}
          aria-label={`打开课程封面：${course.title}`}
          className="relative aspect-[16/10] w-full overflow-hidden rounded-2xl bg-subtle block"
        >
          <CoursePoster
            courseId={course.id}
            title={course.title}
            hasCover={hasCover}
            tag={course.updated_at}
          />
          <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/65 via-transparent to-transparent opacity-85" />

          {course.played && (
            <span className="absolute top-2.5 right-2.5 inline-flex items-center gap-1 rounded-full bg-status-success px-2.5 py-0.5 text-[11px] font-semibold text-ink-inverse shadow-sm">
              <CheckCircle2 className="h-3 w-3" />
              已完成
            </span>
          )}

          <div className="absolute bottom-2.5 left-3 right-3 flex items-center justify-between gap-2 text-[11px] font-medium text-white">
            <span className="rounded-full bg-black/45 backdrop-blur-xs px-2.5 py-0.5 tabular-nums">
              {course.lesson_count} 节 · {course.chapter_count} 章
            </span>
            {durationText && (
              <span className="inline-flex items-center gap-1 rounded-full bg-black/45 backdrop-blur-xs px-2.5 py-0.5 tabular-nums">
                <Clock className="h-3 w-3" />
                {durationText}
              </span>
            )}
          </div>
        </Link>
      </div>

      <div className="flex flex-1 flex-col p-5 pt-4 space-y-3.5">
        <div className="space-y-1 flex-1">
          <Link
            to={`/courses/${course.id}`}
            className="block text-base font-bold text-ink-primary line-clamp-2 group-hover:text-accent-ink transition-colors leading-snug"
          >
            {course.title}
          </Link>
          {course.instructor && <p className="text-xs text-ink-muted">讲师：{course.instructor}</p>}
        </div>

        {course.tags && course.tags.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {course.tags.slice(0, 3).map((tag) => (
              <span
                key={tag}
                className="rounded-full bg-subtle px-2.5 py-0.5 text-[11px] font-medium text-ink-secondary"
              >
                {tag}
              </span>
            ))}
          </div>
        )}

        <div className="space-y-1.5 pt-0.5">
          <div className="flex items-center justify-between text-xs text-ink-muted">
            <span className="tabular-nums">
              已学 {course.played_count} / {course.lesson_count} 节
            </span>
            <span className="font-semibold text-ink-secondary tabular-nums">{pct}%</span>
          </div>
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
            <div
              className={`h-full rounded-full transition-all duration-300 ${
                course.played ? 'bg-status-success' : 'bg-accent'
              }`}
              style={{ width: `${pct}%` }}
            />
          </div>
        </div>

        <div className="flex items-center gap-2 pt-2.5 border-t border-subtle">
          <Tooltip content="进入课程播放页并续播上次进度">
            <Link
              to={`/courses/${course.id}`}
              className="flex-1 inline-flex items-center justify-center gap-1.5 rounded-full bg-accent-soft px-3.5 py-2 text-xs font-semibold text-accent-ink hover:bg-accent hover:text-ink-inverse hover:shadow-accent transition-all"
            >
              <Play className="h-3.5 w-3.5 fill-current" />
              <span>{course.played_count > 0 && !course.played ? '继续观看' : '开始学习'}</span>
            </Link>
          </Tooltip>
          <Tooltip content="编辑课程信息、章节排序或运行 AI 整理">
            <Link
              to={`/courses/${course.id}/edit`}
              aria-label={`编辑 ${course.title}`}
              className="inline-flex items-center justify-center gap-1 rounded-full border border-subtle bg-subtle px-3 py-2 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors"
            >
              <Pencil className="h-3.5 w-3.5" />
              <span>编辑</span>
            </Link>
          </Tooltip>
        </div>
      </div>
    </article>
  );
}

/**
 * Citonema-inspired HomePage with spotlight Continue Watching hero, scoped library/tag filtering,
 * client-side sorting, and seamless fallback when mounted outside LibraryProvider.
 */
export default function HomePage(): JSX.Element {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const libCtx = useLibrary();
  const { toast } = useToast();

  const [home, setHome] = useState<HomeResponse | null>(null);
  const [courses, setCourses] = useState<CourseSummary[]>([]);
  const [localLibraries, setLocalLibraries] = useState<Library[]>([]);
  const [tags, setTags] = useState<Tag[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [localSelectedLibraryId, setLocalSelectedLibraryId] = useState<string>('all');
  const [selectedTag, setSelectedTag] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [sortBy, setSortBy] = useState<SortOption>('default');
  const [activeResumeIdx, setActiveResumeIdx] = useState(0);

  // Standalone create library inline form state
  const [showCreateLib, setShowCreateLib] = useState(false);
  const [newLibName, setNewLibName] = useState('');
  const [creatingLib, setCreatingLib] = useState(false);
  const [createLibError, setCreateLibError] = useState<string | null>(null);

  // Standalone rename & delete library state
  const [renamingLib, setRenamingLib] = useState<Library | null>(null);
  const [renameLibName, setRenameLibName] = useState('');
  const [savingRename, setSavingRename] = useState(false);
  const [renameLibError, setRenameLibError] = useState<string | null>(null);
  const [deletingLib, setDeletingLib] = useState<Library | null>(null);

  // Create blank course inline form state
  const [showCreateCourse, setShowCreateCourse] = useState(false);
  const [newCourseTitle, setNewCourseTitle] = useState('');
  const [newCourseLibraryId, setNewCourseLibraryId] = useState('');
  const [creatingCourse, setCreatingCourse] = useState(false);
  const [createCourseError, setCreateCourseError] = useState<string | null>(null);

  const hasLoadedOnceRef = useRef(false);
  const fetchSeqRef = useRef(0);

  const loadData = useCallback(
    async (silent = false) => {
      const seq = ++fetchSeqRef.current;
      if (!silent) {
        setLoading(true);
      }
      setError(null);
      try {
        if (libCtx.isProvided) {
          const [homeData, coursesData, tagsData] = await Promise.all([
            api.getHome(),
            api.listCourses(),
            api.listTags(),
          ]);
          if (seq !== fetchSeqRef.current) return;
          setHome(homeData);
          setCourses(coursesData.courses ?? homeData.courses ?? []);
          setTags(tagsData.tags ?? []);
        } else {
          const [homeData, coursesData, libsData, tagsData] = await Promise.all([
            api.getHome(),
            api.listCourses(),
            api.listLibraries(),
            api.listTags(),
          ]);
          if (seq !== fetchSeqRef.current) return;
          setHome(homeData);
          setCourses(coursesData.courses ?? homeData.courses ?? []);
          setLocalLibraries(libsData.libraries ?? homeData.libraries ?? []);
          setTags(tagsData.tags ?? []);
        }
        hasLoadedOnceRef.current = true;
      } catch (err) {
        if (seq !== fetchSeqRef.current) return;
        if (!silent || !hasLoadedOnceRef.current) {
          setError(err instanceof Error ? err.message : '加载课程库失败');
        }
      } finally {
        if (seq === fetchSeqRef.current) {
          setLoading(false);
        }
      }
    },
    [libCtx.isProvided],
  );

  useEffect(() => {
    void loadData(hasLoadedOnceRef.current);
  }, [loadData, libCtx.catalogVersion]);

  const libraries = libCtx.isProvided ? libCtx.libraries : localLibraries;
  const urlLibraryParam = libCtx.isProvided ? (searchParams.get('library')?.trim() ?? '') : '';

  const activeLibraryId = useMemo(() => {
    if (!libCtx.isProvided) {
      if (localSelectedLibraryId === 'all') return 'all';
      return localLibraries.some((l) => l.id === localSelectedLibraryId)
        ? localSelectedLibraryId
        : 'all';
    }
    if (!urlLibraryParam || libCtx.loading) {
      return 'all';
    }
    return libraries.some((l) => l.id === urlLibraryParam) ? urlLibraryParam : 'all';
  }, [libCtx.isProvided, libCtx.loading, localLibraries, localSelectedLibraryId, libraries, urlLibraryParam]);

  const activeLibrary = useMemo(
    () => (activeLibraryId === 'all' ? null : (libraries.find((l) => l.id === activeLibraryId) ?? null)),
    [activeLibraryId, libraries],
  );

  useEffect(() => {
    setNewCourseLibraryId((prev) => {
      if (activeLibraryId !== 'all' && libraries.some((l) => l.id === activeLibraryId)) {
        return activeLibraryId;
      }
      return libraries.some((l) => l.id === prev) ? prev : (libraries[0]?.id ?? '');
    });
  }, [libraries, activeLibraryId]);

  const allCourses: CourseSummary[] = courses;

  const scopedCourses = useMemo(() => {
    if (activeLibraryId === 'all') return allCourses;
    return allCourses.filter((course) => course.library_id === activeLibraryId);
  }, [allCourses, activeLibraryId]);

  const resumeLessons = home?.resume ?? [];

  const scopedResume = useMemo(() => {
    if (activeLibraryId === 'all') return resumeLessons;
    return resumeLessons.filter((item) => item.library_id === activeLibraryId);
  }, [resumeLessons, activeLibraryId]);

  useEffect(() => {
    setActiveResumeIdx(0);
  }, [activeLibraryId, scopedResume.length]);

  const scopedTags = useMemo<Tag[]>(() => {
    if (activeLibraryId === 'all') {
      return tags;
    }
    const counts = new Map<string, number>();
    for (const course of scopedCourses) {
      for (const tagName of course.tags ?? []) {
        const trimmed = tagName.trim();
        if (!trimmed) continue;
        counts.set(trimmed, (counts.get(trimmed) ?? 0) + 1);
      }
    }
    return Array.from(counts.entries())
      .map(([name, course_count]) => ({ id: name, name, course_count }))
      .sort((a, b) => b.course_count - a.course_count || a.name.localeCompare(b.name, 'zh-CN'));
  }, [activeLibraryId, scopedCourses, tags]);

  useEffect(() => {
    if (selectedTag && !scopedTags.some((t) => t.name === selectedTag)) {
      setSelectedTag(null);
    }
  }, [selectedTag, scopedTags]);

  const filteredCourses = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    const list = scopedCourses.filter((course) => {
      if (selectedTag && !(course.tags ?? []).includes(selectedTag)) {
        return false;
      }
      if (q) {
        const hay = `${course.title} ${course.instructor} ${course.overview} ${(course.tags ?? []).join(' ')}`.toLowerCase();
        if (!hay.includes(q)) {
          return false;
        }
      }
      return true;
    });

    if (sortBy === 'default') {
      return list;
    }

    const sorted = [...list];
    if (sortBy === 'updated') {
      sorted.sort((a, b) => (b.updated_at || '').localeCompare(a.updated_at || ''));
    } else if (sortBy === 'progress') {
      sorted.sort(
        (a, b) =>
          (b.played_percentage || 0) - (a.played_percentage || 0) ||
          b.played_count - a.played_count,
      );
    } else if (sortBy === 'title') {
      sorted.sort((a, b) =>
        (a.sort_title || a.title).localeCompare(b.sort_title || b.title, 'zh-CN'),
      );
    }
    return sorted;
  }, [scopedCourses, selectedTag, searchQuery, sortBy]);

  async function handleCreateLibrary(e: FormEvent) {
    e.preventDefault();
    const trimmed = newLibName.trim();
    if (!trimmed) {
      setCreateLibError('请输入媒体库名称');
      return;
    }
    setCreatingLib(true);
    setCreateLibError(null);
    try {
      const created = await api.createLibrary(trimmed);
      setLocalLibraries((prev) => [...prev, created]);
      setNewCourseLibraryId((prev) => prev || created.id);
      setNewLibName('');
      setShowCreateLib(false);
      setLocalSelectedLibraryId(created.id);
      toast.success(`已创建媒体库「${created.name}」`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : '创建媒体库失败';
      setCreateLibError(msg);
      toast.error(msg);
    } finally {
      setCreatingLib(false);
    }
  }

  function startRenameLibrary(lib: Library) {
    setShowCreateLib(false);
    setShowCreateCourse(false);
    setRenamingLib(lib);
    setRenameLibName(lib.name);
    setRenameLibError(null);
  }

  async function handleRenameLibrary(e: FormEvent) {
    e.preventDefault();
    if (!renamingLib || savingRename) return;
    const trimmed = renameLibName.trim();
    if (!trimmed) {
      setRenameLibError('请输入媒体库名称');
      return;
    }
    if (trimmed === renamingLib.name) {
      setRenamingLib(null);
      return;
    }
    setSavingRename(true);
    setRenameLibError(null);
    try {
      const updated = await api.updateLibrary(renamingLib.id, trimmed);
      setLocalLibraries((prev) => prev.map((l) => (l.id === updated.id ? updated : l)));
      setRenamingLib(null);
      toast.success(`已将媒体库重命名为「${updated.name}」`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : '重命名媒体库失败';
      setRenameLibError(msg);
      toast.error(msg);
    } finally {
      setSavingRename(false);
    }
  }

  function handleLibraryDeleted(
    deleted: Library,
    mode: DeleteLibraryMode,
    targetLibraryName?: string,
  ) {
    if (localSelectedLibraryId === deleted.id) {
      setLocalSelectedLibraryId('all');
    }
    if (renamingLib?.id === deleted.id) {
      setRenamingLib(null);
    }
    if (mode === 'move' && targetLibraryName) {
      toast.success(`已将课程迁移至「${targetLibraryName}」并删除媒体库「${deleted.name}」`);
    } else {
      toast.success(`已删除媒体库「${deleted.name}」`);
    }
    void loadData(true);
  }

  async function handleCreateBlankCourse(e: FormEvent) {
    e.preventDefault();
    const trimmed = newCourseTitle.trim();
    const targetLib = newCourseLibraryId || libraries[0]?.id || '';
    if (!trimmed) {
      setCreateCourseError('请输入课程名称');
      return;
    }
    if (!targetLib) {
      setCreateCourseError('请先创建一个媒体库');
      return;
    }
    setCreatingCourse(true);
    setCreateCourseError(null);
    try {
      const created = await api.createCourse({
        library_id: targetLib,
        title: trimmed,
      });
      setNewCourseTitle('');
      setShowCreateCourse(false);
      if (libCtx.isProvided) {
        libCtx.bumpCatalogVersion();
      }
      navigate(`/courses/${created.id}/edit`);
    } catch (err) {
      setCreateCourseError(err instanceof Error ? err.message : '创建课程失败');
    } finally {
      setCreatingCourse(false);
    }
  }

  function handleResetLibrary() {
    if (libCtx.isProvided) {
      navigate('/');
    } else {
      setLocalSelectedLibraryId('all');
    }
  }

  function handleClearAllFilters() {
    setSelectedTag(null);
    setSearchQuery('');
    if (libCtx.isProvided) {
      if (activeLibraryId !== 'all') {
        navigate('/');
      }
    } else {
      setLocalSelectedLibraryId('all');
    }
  }

  const hasActiveFilter =
    activeLibraryId !== 'all' || selectedTag !== null || searchQuery.trim() !== '';

  const effectiveLoading = loading || (libCtx.isProvided && libCtx.loading && !hasLoadedOnceRef.current);

  if (effectiveLoading) {
    return (
      <div className="flex flex-col items-center justify-center py-28 text-ink-muted gap-3">
        <Loader2 className="h-7 w-7 animate-spin text-accent" />
        <p className="text-sm font-medium">正在加载课程库...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-3xl border border-status-danger/30 bg-status-danger-soft p-8 text-center max-w-lg mx-auto my-12 shadow-card">
        <p className="text-status-danger-ink font-semibold">{error}</p>
        <button
          type="button"
          onClick={() => void loadData(false)}
          className="mt-5 inline-flex items-center rounded-full bg-status-danger px-5 py-2 text-sm font-semibold text-ink-inverse hover:opacity-90 transition-opacity cursor-pointer"
        >
          重新加载
        </button>
      </div>
    );
  }

  const greeting = getTimeGreeting();

  return (
    <div className="space-y-8">
      {/* Top Greeting & Search Header */}
      <header className="flex flex-col lg:flex-row lg:items-end lg:justify-between gap-5">
        <div className="space-y-1.5 min-w-0">
          <div className="inline-flex items-center gap-1.5 text-xs font-medium text-ink-muted">
            {greeting.isNight ? (
              <Moon className="h-3.5 w-3.5 text-accent-ink" aria-hidden="true" />
            ) : (
              <Sun className="h-3.5 w-3.5 text-status-warning" aria-hidden="true" />
            )}
            <span>{greeting.label}</span>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <h1
              id="course-library-heading"
              className="text-2xl sm:text-3xl font-bold tracking-tight text-ink-primary truncate"
            >
              {activeLibrary ? activeLibrary.name : '全部课程'}
            </h1>
            <span className="inline-flex items-center rounded-full bg-subtle px-3 py-1 text-xs font-semibold text-ink-secondary tabular-nums">
              共 {scopedCourses.length} 门
            </span>
            {activeLibrary && libCtx.isProvided && (
              <Tooltip content="返回查看全部媒体库中的课程">
                <button
                  type="button"
                  onClick={handleResetLibrary}
                  className="inline-flex items-center gap-1 rounded-full border border-subtle bg-card px-3 py-1 text-xs font-medium text-ink-secondary shadow-card hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
                >
                  <X className="h-3 w-3" />
                  <span>查看全部</span>
                </button>
              </Tooltip>
            )}
          </div>
        </div>

        <div className="flex flex-col sm:flex-row sm:items-center gap-3 w-full lg:w-auto">
          <div className="relative w-full sm:flex-1 lg:flex-none lg:w-80">
            <Search
              aria-hidden="true"
              className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 h-4 w-4 text-ink-muted"
            />
            <input
              type="search"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="搜索课程名称、讲师或标签..."
              aria-label="搜索课程"
              className="w-full rounded-full border border-subtle bg-card py-2.5 pl-10 pr-9 text-sm text-ink-primary placeholder:text-ink-muted shadow-card focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring transition-all"
            />
            {searchQuery && (
              <Tooltip content="清空搜索关键词">
                <button
                  type="button"
                  aria-label="清空搜索"
                  onClick={() => setSearchQuery('')}
                  className="absolute right-3 top-1/2 -translate-y-1/2 rounded-full p-0.5 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
                >
                  <X className="h-4 w-4" />
                </button>
              </Tooltip>
            )}
          </div>

          {libraries.length > 0 && (
            <Tooltip content="手动创建空白课程并进入编辑页编排视频">
              <button
                type="button"
                onClick={() => {
                  setRenamingLib(null);
                  setShowCreateLib(false);
                  setShowCreateCourse((v) => !v);
                  setCreateCourseError(null);
                }}
                className="inline-flex self-start sm:self-auto shrink-0 items-center gap-1.5 rounded-full border border-accent/20 bg-accent-soft px-4 py-2.5 text-sm font-semibold text-accent-ink hover:bg-accent hover:text-ink-inverse hover:border-transparent hover:shadow-accent transition-all cursor-pointer"
              >
                <Plus className="h-4 w-4" />
                <span>新建空白课程</span>
              </button>
            </Tooltip>
          )}
        </div>
      </header>

      {/* Inline Create Blank Course Card */}
      {showCreateCourse && (
        <form
          onSubmit={(e) => void handleCreateBlankCourse(e)}
          className="flex flex-col sm:flex-row sm:items-center gap-3 rounded-3xl border border-subtle bg-card p-4 sm:p-5 shadow-card"
        >
          <input
            type="text"
            value={newCourseTitle}
            onChange={(e) => setNewCourseTitle(e.target.value)}
            placeholder="输入新课程名称"
            aria-label="新课程名称"
            className="flex-1 rounded-xl border border-default bg-subtle/60 px-3.5 py-2.5 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
          />
          <select
            aria-label="目标媒体库"
            value={newCourseLibraryId || libraries[0]?.id || ''}
            onChange={(e) => setNewCourseLibraryId(e.target.value)}
            className="rounded-xl border border-default bg-subtle/60 px-3.5 py-2.5 text-sm text-ink-primary focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
          >
            {libraries.map((lib) => (
              <option key={lib.id} value={lib.id}>
                {lib.name}
              </option>
            ))}
          </select>
          <div className="flex items-center gap-2">
            <button
              type="submit"
              disabled={creatingCourse}
              className="rounded-full bg-accent px-5 py-2 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-all cursor-pointer"
            >
              {creatingCourse ? '创建中...' : '创建并编辑'}
            </button>
            <button
              type="button"
              onClick={() => setShowCreateCourse(false)}
              className="rounded-full border border-default bg-card px-4 py-2 text-sm font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
            >
              取消
            </button>
          </div>
          {createCourseError && (
            <p className="text-xs font-medium text-status-danger-ink sm:ml-2">{createCourseError}</p>
          )}
        </form>
      )}

      {/* Standalone Library Bar & Inline Forms (only when outside LibraryProvider) */}
      {!libCtx.isProvided && (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <Tooltip content="查看所有媒体库中的全部课程">
              <button
                type="button"
                onClick={() => setLocalSelectedLibraryId('all')}
                className={`rounded-full px-4 py-2 text-sm font-medium transition-all cursor-pointer ${
                  activeLibraryId === 'all'
                    ? 'bg-accent text-ink-inverse shadow-accent font-semibold'
                    : 'bg-card text-ink-secondary border border-subtle shadow-card hover:bg-subtle hover:text-ink-primary'
                }`}
              >
                全部 ({allCourses.length})
              </button>
            </Tooltip>

            {libraries.map((lib) => {
              const active = activeLibraryId === lib.id;
              return (
                <div
                  key={lib.id}
                  role="group"
                  aria-label={`媒体库 ${lib.name}`}
                  className={`group inline-flex items-center rounded-full text-sm font-medium transition-all ${
                    active
                      ? 'bg-accent text-ink-inverse shadow-accent'
                      : 'bg-card text-ink-secondary border border-subtle shadow-card hover:bg-subtle hover:text-ink-primary'
                  }`}
                >
                  <Tooltip content={`仅查看「${lib.name}」中的课程`}>
                    <button
                      type="button"
                      onClick={() => setLocalSelectedLibraryId(lib.id)}
                      className="pl-4 pr-1.5 py-2 text-left cursor-pointer"
                    >
                      {lib.name}
                      <span className="ml-1.5 text-xs opacity-80 tabular-nums">({lib.course_count})</span>
                    </button>
                  </Tooltip>

                  <div
                    className={`flex items-center gap-0.5 pr-2 transition-opacity ${
                      active
                        ? 'opacity-95'
                        : 'opacity-55 group-hover:opacity-100 group-focus-within:opacity-100'
                    }`}
                  >
                    <Tooltip content={`重命名「${lib.name}」`}>
                      <button
                        type="button"
                        aria-label="重命名媒体库"
                        onClick={() => startRenameLibrary(lib)}
                        className={`rounded-full p-1 transition-colors cursor-pointer ${
                          active
                            ? 'text-ink-inverse/85 hover:bg-white/20 hover:text-ink-inverse'
                            : 'text-ink-muted hover:bg-muted hover:text-ink-primary'
                        }`}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </button>
                    </Tooltip>

                    <Tooltip content={`删除「${lib.name}」`}>
                      <button
                        type="button"
                        aria-label="删除媒体库"
                        onClick={() => setDeletingLib(lib)}
                        className={`rounded-full p-1 transition-colors cursor-pointer ${
                          active
                            ? 'text-ink-inverse/85 hover:bg-white/20 hover:text-ink-inverse'
                            : 'text-ink-muted hover:bg-status-danger-soft hover:text-status-danger-ink'
                        }`}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </Tooltip>
                  </div>
                </div>
              );
            })}

            <Tooltip content="新建一个课程分类媒体库">
              <button
                type="button"
                onClick={() => {
                  setRenamingLib(null);
                  setShowCreateCourse(false);
                  setShowCreateLib((v) => !v);
                  setCreateLibError(null);
                }}
                className="inline-flex items-center gap-1.5 rounded-full border border-dashed border-default bg-card/70 px-4 py-2 text-sm font-medium text-ink-secondary hover:border-accent hover:text-accent-ink transition-colors cursor-pointer"
              >
                <Plus className="h-4 w-4" />
                <span>新建媒体库</span>
              </button>
            </Tooltip>
          </div>

          {renamingLib && (
            <form
              onSubmit={(e) => void handleRenameLibrary(e)}
              className="flex flex-col sm:flex-row sm:items-center gap-3 rounded-3xl border border-accent/30 bg-card p-4 sm:p-5 shadow-card"
            >
              <span className="text-xs font-semibold text-accent-ink shrink-0">
                重命名「{renamingLib.name}」：
              </span>
              <input
                type="text"
                value={renameLibName}
                onChange={(e) => setRenameLibName(e.target.value)}
                placeholder="输入新的媒体库名称"
                aria-label="重命名媒体库名称"
                className="flex-1 rounded-xl border border-default bg-subtle/60 px-3.5 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring"
              />
              <div className="flex items-center gap-2">
                <button
                  type="submit"
                  disabled={savingRename}
                  className="rounded-full bg-accent px-4 py-2 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
                >
                  {savingRename ? '保存中...' : '保存名称'}
                </button>
                <button
                  type="button"
                  onClick={() => setRenamingLib(null)}
                  className="rounded-full border border-default bg-card px-4 py-2 text-sm font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
                >
                  取消
                </button>
              </div>
              {renameLibError && (
                <p className="text-xs font-medium text-status-danger-ink sm:ml-2">{renameLibError}</p>
              )}
            </form>
          )}

          {showCreateLib && (
            <form
              onSubmit={(e) => void handleCreateLibrary(e)}
              className="flex flex-col sm:flex-row sm:items-center gap-3 rounded-3xl border border-subtle bg-card p-4 sm:p-5 shadow-card"
            >
              <input
                type="text"
                value={newLibName}
                onChange={(e) => setNewLibName(e.target.value)}
                placeholder="输入新媒体库名称，例如：前端工程化"
                aria-label="新媒体库名称"
                className="flex-1 rounded-xl border border-default bg-subtle/60 px-3.5 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring"
              />
              <div className="flex items-center gap-2">
                <button
                  type="submit"
                  disabled={creatingLib}
                  className="rounded-full bg-accent px-4 py-2 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
                >
                  {creatingLib ? '创建中...' : '确认创建'}
                </button>
                <button
                  type="button"
                  onClick={() => setShowCreateLib(false)}
                  className="rounded-full border border-default bg-card px-4 py-2 text-sm font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
                >
                  取消
                </button>
              </div>
              {createLibError && (
                <p className="text-xs font-medium text-status-danger-ink sm:ml-2">{createLibError}</p>
              )}
            </form>
          )}
        </div>
      )}

      {/* Citonema Spotlight Continue Watching Hero */}
      <SpotlightHero
        items={scopedResume}
        activeIdx={activeResumeIdx}
        onSelectIdx={setActiveResumeIdx}
      />

      {/* Course Library Section: Tag Filter, Sort Strip & Poster Grid */}
      <section aria-labelledby="course-library-heading" className="space-y-6">
        {(scopedTags.length > 0 || scopedCourses.length > 0) && (
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2 min-w-0 flex-1">
              {scopedTags.length > 0 ? (
                <>
                  <span className="inline-flex items-center gap-1 text-xs font-medium text-ink-muted mr-1">
                    <TagIcon className="h-3.5 w-3.5" aria-hidden="true" />
                    <span>标签</span>
                  </span>

                  <button
                    type="button"
                    onClick={() => setSelectedTag(null)}
                    className={`rounded-full px-3.5 py-1.5 text-xs font-medium transition-all cursor-pointer ${
                      selectedTag === null
                        ? 'bg-accent text-ink-inverse shadow-accent font-semibold'
                        : 'bg-card text-ink-secondary border border-subtle shadow-card hover:bg-subtle hover:text-ink-primary'
                    }`}
                  >
                    所有标签
                  </button>

                  {scopedTags.map((tag) => {
                    const active = selectedTag === tag.name;
                    return (
                      <Tooltip
                        key={tag.id}
                        content={
                          active
                            ? `取消按标签「${tag.name}」筛选`
                            : `按标签「${tag.name}」筛选课程`
                        }
                      >
                        <button
                          type="button"
                          onClick={() => setSelectedTag(active ? null : tag.name)}
                          className={`rounded-full px-3.5 py-1.5 text-xs font-medium transition-all cursor-pointer ${
                            active
                              ? 'bg-accent text-ink-inverse shadow-accent font-semibold'
                              : 'bg-card text-ink-secondary border border-subtle shadow-card hover:bg-subtle hover:text-ink-primary'
                          }`}
                        >
                          {tag.name}
                          <span className="ml-1.5 opacity-75 tabular-nums">{tag.course_count}</span>
                        </button>
                      </Tooltip>
                    );
                  })}
                </>
              ) : (
                <span className="text-sm font-semibold text-ink-secondary">全部课程目录</span>
              )}
            </div>

            {scopedCourses.length > 0 && (
              <div className="flex items-center gap-2 shrink-0 self-end sm:self-auto">
                <ArrowUpDown className="h-3.5 w-3.5 text-ink-muted" aria-hidden="true" />
                <select
                  aria-label="排序方式"
                  value={sortBy}
                  onChange={(e) => setSortBy(e.target.value as SortOption)}
                  className="rounded-full border border-subtle bg-card px-3.5 py-1.5 text-xs font-medium text-ink-primary shadow-card focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring cursor-pointer"
                >
                  <option value="default">默认顺序</option>
                  <option value="updated">最近更新</option>
                  <option value="progress">学习进度</option>
                  <option value="title">标题排序</option>
                </select>
              </div>
            )}
          </div>
        )}

        {/* Empty State vs Course Grid */}
        {allCourses.length === 0 ? (
          <div className="rounded-3xl border border-subtle bg-card px-6 py-16 text-center shadow-card">
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-accent-soft text-accent-ink mb-4">
              <Sparkles className="h-7 w-7" />
            </div>
            <h3 className="text-lg font-bold text-ink-primary">还没有导入任何课程</h3>
            <p className="mt-2 text-sm text-ink-secondary max-w-md mx-auto leading-relaxed">
              请先前往“存储与导入”添加本地或远程课程目录，扫描视频后即可一键导入并结构化整理课程。
            </p>
            <Link
              to="/storage"
              className="mt-6 inline-flex items-center gap-2 rounded-full bg-accent px-6 py-2.5 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover transition-all"
            >
              <FolderInput className="h-4 w-4" />
              <span>前往存储与导入</span>
            </Link>
          </div>
        ) : filteredCourses.length === 0 ? (
          <div className="rounded-3xl border border-subtle bg-card px-6 py-14 text-center shadow-card">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-subtle text-ink-muted mb-3">
              <BookOpen className="h-6 w-6" />
            </div>
            <p className="text-base font-semibold text-ink-primary">没有符合当前筛选条件的课程</p>
            {hasActiveFilter && (
              <Tooltip content="重置媒体库、标签和关键词筛选">
                <button
                  type="button"
                  onClick={handleClearAllFilters}
                  className="mt-4 inline-flex items-center rounded-full border border-default bg-subtle px-5 py-2 text-sm font-medium text-ink-primary hover:bg-muted transition-colors cursor-pointer"
                >
                  清除筛选条件
                </button>
              </Tooltip>
            )}
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-6">
            {filteredCourses.map((course) => (
              <CourseCard key={course.id} course={course} />
            ))}
          </div>
        )}
      </section>

      {!libCtx.isProvided && (
        <DeleteLibraryModal
          library={deletingLib}
          libraries={libraries}
          onClose={() => setDeletingLib(null)}
          onDeleted={handleLibraryDeleted}
        />
      )}
    </div>
  );
}
