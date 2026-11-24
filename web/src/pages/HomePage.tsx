import type { FormEvent } from 'react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  BookOpen,
  CheckCircle2,
  Clock,
  FolderInput,
  Loader2,
  Pencil,
  Play,
  Plus,
  Search,
  Sparkles,
  Tag as TagIcon,
  Trash2,
  X,
} from 'lucide-react';
import { api, itemPrimaryImageUrl } from '../api/client';
import DeleteLibraryModal, { type DeleteLibraryMode } from '../components/DeleteLibraryModal';
import { useToast } from '../components/Toast';
import Tooltip from '../components/Tooltip';
import type { CourseSummary, HomeResponse, Library, Tag } from '../types/coursefin';

const TICKS_PER_MS = 10000;

const COVER_GRADIENTS = [
  'from-indigo-600 via-indigo-700 to-slate-900',
  'from-sky-600 via-cyan-700 to-slate-900',
  'from-emerald-600 via-teal-700 to-slate-900',
  'from-violet-600 via-purple-700 to-slate-900',
  'from-amber-600 via-orange-700 to-slate-900',
  'from-rose-600 via-pink-700 to-slate-900',
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

interface CoursePosterProps {
  courseId: string;
  title: string;
  hasCover: boolean;
  tag?: string;
}

function CoursePoster({ courseId, title, hasCover, tag }: CoursePosterProps) {
  const [imgFailed, setImgFailed] = useState(false);

  useEffect(() => {
    setImgFailed(false);
  }, [courseId, tag]);

  const initial = title.trim().slice(0, 1).toUpperCase() || '课';
  const gradient = gradientForId(courseId);

  if (!hasCover || imgFailed) {
    return (
      <div
        className={`h-full w-full bg-gradient-to-br ${gradient} flex flex-col items-center justify-center p-4 text-center relative overflow-hidden`}
      >
        <span className="text-4xl font-bold text-white/90 tracking-tight drop-shadow">
          {initial}
        </span>
        <span className="mt-1.5 line-clamp-1 text-xs text-white/65 max-w-[85%]">{title}</span>
      </div>
    );
  }

  return (
    <img
      src={itemPrimaryImageUrl(courseId, tag)}
      alt={title}
      onError={() => setImgFailed(true)}
      className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
    />
  );
}

export default function HomePage() {
  const navigate = useNavigate();
  const { toast } = useToast();
  const [home, setHome] = useState<HomeResponse | null>(null);
  const [courses, setCourses] = useState<CourseSummary[]>([]);
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [tags, setTags] = useState<Tag[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [selectedLibraryId, setSelectedLibraryId] = useState<string>('all');
  const [selectedTag, setSelectedTag] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');

  // Create library inline modal state
  const [showCreateLib, setShowCreateLib] = useState(false);
  const [newLibName, setNewLibName] = useState('');
  const [creatingLib, setCreatingLib] = useState(false);
  const [createLibError, setCreateLibError] = useState<string | null>(null);

  // Rename & delete library state
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

  const loadData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [homeData, coursesData, libsData, tagsData] = await Promise.all([
        api.getHome(),
        api.listCourses(),
        api.listLibraries(),
        api.listTags(),
      ]);
      setHome(homeData);
      setCourses(coursesData.courses ?? homeData.courses ?? []);
      const resolvedLibs = libsData.libraries ?? homeData.libraries ?? [];
      setLibraries(resolvedLibs);
      setNewCourseLibraryId((prev) =>
        resolvedLibs.some((l) => l.id === prev) ? prev : (resolvedLibs[0]?.id ?? ''),
      );
      setTags(tagsData.tags ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载课程库失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

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
      setLibraries((prev) => [...prev, created]);
      setNewCourseLibraryId((prev) => prev || created.id);
      setNewLibName('');
      setShowCreateLib(false);
      setSelectedLibraryId(created.id);
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
      setLibraries((prev) => prev.map((l) => (l.id === updated.id ? updated : l)));
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
    if (selectedLibraryId === deleted.id) {
      setSelectedLibraryId('all');
    }
    if (renamingLib?.id === deleted.id) {
      setRenamingLib(null);
    }
    if (mode === 'move' && targetLibraryName) {
      toast.success(`已将课程迁移至「${targetLibraryName}」并删除媒体库「${deleted.name}」`);
    } else {
      toast.success(`已删除媒体库「${deleted.name}」`);
    }
    void loadData();
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
      navigate(`/courses/${created.id}/edit`);
    } catch (err) {
      setCreateCourseError(err instanceof Error ? err.message : '创建课程失败');
    } finally {
      setCreatingCourse(false);
    }
  }

  const allCourses: CourseSummary[] = courses;

  const filteredCourses = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return allCourses.filter((course) => {
      if (selectedLibraryId !== 'all' && course.library_id !== selectedLibraryId) {
        return false;
      }
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
  }, [allCourses, selectedLibraryId, selectedTag, searchQuery]);

  const resumeLessons = home?.resume ?? [];
  const hasActiveFilter = selectedLibraryId !== 'all' || selectedTag !== null || searchQuery.trim() !== '';

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center py-24 text-slate-400 gap-3">
        <Loader2 className="h-7 w-7 animate-spin text-indigo-400" />
        <p className="text-sm">正在加载课程库...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-2xl border border-rose-500/30 bg-rose-500/10 p-6 text-center max-w-lg mx-auto my-12">
        <p className="text-rose-200 font-medium">{error}</p>
        <button
          type="button"
          onClick={() => void loadData()}
          className="mt-4 inline-flex items-center rounded-xl bg-rose-600 px-4 py-2 text-sm font-medium text-white hover:bg-rose-500 transition-colors"
        >
          重新加载
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-8">
      {/* Continue Watching Rail */}
      {resumeLessons.length > 0 && (
        <section aria-labelledby="continue-watching-heading" className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-indigo-500/15 text-indigo-400">
                <Play className="h-4 w-4 fill-current" />
              </span>
              <h2 id="continue-watching-heading" className="text-lg font-semibold text-white">
                继续学习
              </h2>
            </div>
            <span className="text-xs text-slate-400">{resumeLessons.length} 节进行中</span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
            {resumeLessons.map((item) => {
              const pct = calcLessonProgressPct(item.position_ticks, item.duration_ms);
              const hasThumb = Boolean(item.cover_path || item.thumbnail_path || item.course_cover_path);
              return (
                <Link
                  key={item.id}
                  to={`/courses/${item.course_id}?lesson=${item.id}`}
                  className="group relative flex flex-col overflow-hidden rounded-2xl border border-slate-800/80 bg-slate-900/70 hover:border-indigo-500/50 hover:bg-slate-900 transition-all shadow-sm"
                >
                  <div className="relative aspect-video w-full overflow-hidden bg-slate-800">
                    <CoursePoster
                      courseId={item.cover_path || item.thumbnail_path ? item.id : item.course_id}
                      title={item.course_title}
                      hasCover={hasThumb}
                      tag={item.updated_at}
                    />
                    <div className="absolute inset-0 bg-gradient-to-t from-slate-950/90 via-slate-950/20 to-transparent" />
                    <div className="absolute bottom-2.5 left-3 right-3 flex items-center justify-between text-xs text-white">
                      <span className="inline-flex items-center gap-1 rounded-md bg-slate-950/75 px-2 py-0.5 backdrop-blur">
                        <Play className="h-3 w-3 text-indigo-400 fill-current" />
                        <span>继续播放</span>
                      </span>
                      <span className="rounded-md bg-indigo-600/90 px-2 py-0.5 font-semibold">
                        {pct}%
                      </span>
                    </div>
                    <div className="absolute bottom-0 left-0 right-0 h-1 bg-slate-800">
                      <div
                        className="h-full bg-indigo-500 transition-all"
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  </div>
                  <div className="p-3.5 space-y-1">
                    <p className="text-xs font-medium text-indigo-300 line-clamp-1">
                      {item.course_title}
                    </p>
                    <p className="text-sm font-semibold text-white line-clamp-1 group-hover:text-indigo-200 transition-colors">
                      {item.title}
                    </p>
                  </div>
                </Link>
              );
            })}
          </div>
        </section>
      )}

      {/* Library Tabs, Search & Tag Filters */}
      <section aria-labelledby="course-library-heading" className="space-y-5">
        <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4">
          <div className="flex flex-wrap items-center gap-2">
            <h1 id="course-library-heading" className="sr-only">
              课程库
            </h1>
            <Tooltip content="查看所有媒体库中的全部课程">
              <button
                type="button"
                onClick={() => setSelectedLibraryId('all')}
                className={`rounded-xl px-3.5 py-2 text-sm font-medium transition-colors ${
                  selectedLibraryId === 'all'
                    ? 'bg-indigo-600 text-white shadow-sm shadow-indigo-600/30'
                    : 'bg-slate-900 text-slate-300 border border-slate-800 hover:border-slate-700 hover:text-white'
                }`}
              >
                全部 ({allCourses.length})
              </button>
            </Tooltip>

            {libraries.map((lib) => {
              const active = selectedLibraryId === lib.id;
              return (
                <div
                  key={lib.id}
                  role="group"
                  aria-label={`媒体库 ${lib.name}`}
                  className={`group inline-flex items-center rounded-xl text-sm font-medium transition-colors ${
                    active
                      ? 'bg-indigo-600 text-white shadow-sm shadow-indigo-600/30'
                      : 'bg-slate-900 text-slate-300 border border-slate-800 hover:border-slate-700 hover:text-white'
                  }`}
                >
                  <Tooltip content={`仅查看「${lib.name}」中的课程`}>
                    <button
                      type="button"
                      onClick={() => setSelectedLibraryId(lib.id)}
                      className="pl-3.5 pr-1.5 py-2 text-left"
                    >
                      {lib.name}
                      <span className="ml-1.5 text-xs opacity-75">({lib.course_count})</span>
                    </button>
                  </Tooltip>

                  <div
                    className={`flex items-center gap-0.5 pr-1.5 transition-opacity ${
                      active
                        ? 'opacity-90'
                        : 'opacity-45 group-hover:opacity-100 group-focus-within:opacity-100'
                    }`}
                  >
                    <Tooltip content={`重命名「${lib.name}」`}>
                      <button
                        type="button"
                        aria-label="重命名媒体库"
                        onClick={() => startRenameLibrary(lib)}
                        className={`rounded-lg p-1 transition-colors ${
                          active
                            ? 'text-indigo-100 hover:bg-indigo-500/80 hover:text-white'
                            : 'text-slate-400 hover:bg-slate-800 hover:text-white'
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
                        className={`rounded-lg p-1 transition-colors ${
                          active
                            ? 'text-indigo-100 hover:bg-rose-500/80 hover:text-white'
                            : 'text-slate-400 hover:bg-rose-500/20 hover:text-rose-300'
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
                  setShowCreateLib((v) => !v);
                  setCreateLibError(null);
                }}
                className="inline-flex items-center gap-1.5 rounded-xl border border-dashed border-slate-700 px-3.5 py-2 text-sm font-medium text-slate-300 hover:border-indigo-500/60 hover:text-indigo-300 transition-colors"
              >
                <Plus className="h-4 w-4" />
                <span>新建媒体库</span>
              </button>
            </Tooltip>

            {libraries.length > 0 && (
              <Tooltip content="手动创建空白课程并进入编辑页编排视频">
                <button
                  type="button"
                  onClick={() => {
                    setRenamingLib(null);
                    setShowCreateCourse((v) => !v);
                    setCreateCourseError(null);
                  }}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-slate-700 bg-slate-900 px-3.5 py-2 text-sm font-medium text-slate-200 hover:border-indigo-500/60 hover:text-indigo-300 transition-colors"
                >
                  <Plus className="h-4 w-4" />
                  <span>新建空白课程</span>
                </button>
              </Tooltip>
            )}
          </div>

          {/* Search box */}
          <div className="relative w-full lg:w-72">
            <Search className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
            <input
              type="search"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="搜索课程名称、讲师或标签..."
              aria-label="搜索课程"
              className="w-full rounded-xl border border-slate-800 bg-slate-900/90 pl-10 pr-8 py-2 text-sm text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
            />
            {searchQuery && (
              <Tooltip content="清空搜索关键词">
                <button
                  type="button"
                  aria-label="清空搜索"
                  onClick={() => setSearchQuery('')}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-400 hover:text-white"
                >
                  <X className="h-4 w-4" />
                </button>
              </Tooltip>
            )}
          </div>
        </div>

        {/* Inline Rename Library Form */}
        {renamingLib && (
          <form
            onSubmit={handleRenameLibrary}
            className="flex flex-col sm:flex-row sm:items-center gap-3 rounded-2xl border border-indigo-500/40 bg-slate-900/90 p-4"
          >
            <span className="text-xs font-medium text-indigo-300 shrink-0">
              重命名「{renamingLib.name}」：
            </span>
            <input
              type="text"
              value={renameLibName}
              onChange={(e) => setRenameLibName(e.target.value)}
              placeholder="输入新的媒体库名称"
              aria-label="重命名媒体库名称"
              className="flex-1 rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
            />
            <div className="flex items-center gap-2">
              <button
                type="submit"
                disabled={savingRename}
                className="rounded-xl bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors"
              >
                {savingRename ? '保存中...' : '保存名称'}
              </button>
              <button
                type="button"
                onClick={() => setRenamingLib(null)}
                className="rounded-xl border border-slate-700 px-3.5 py-2 text-sm text-slate-300 hover:bg-slate-800 transition-colors"
              >
                取消
              </button>
            </div>
            {renameLibError && (
              <p className="text-xs text-rose-400 sm:ml-2">{renameLibError}</p>
            )}
          </form>
        )}

        {/* Inline Create Library Form */}
        {showCreateLib && (
          <form
            onSubmit={handleCreateLibrary}
            className="flex flex-col sm:flex-row sm:items-center gap-3 rounded-2xl border border-slate-800 bg-slate-900/90 p-4"
          >
            <input
              type="text"
              value={newLibName}
              onChange={(e) => setNewLibName(e.target.value)}
              placeholder="输入新媒体库名称，例如：前端工程化"
              aria-label="新媒体库名称"
              className="flex-1 rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
            />
            <div className="flex items-center gap-2">
              <button
                type="submit"
                disabled={creatingLib}
                className="rounded-xl bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors"
              >
                {creatingLib ? '创建中...' : '确认创建'}
              </button>
              <button
                type="button"
                onClick={() => setShowCreateLib(false)}
                className="rounded-xl border border-slate-700 px-3.5 py-2 text-sm text-slate-300 hover:bg-slate-800 transition-colors"
              >
                取消
              </button>
            </div>
            {createLibError && (
              <p className="text-xs text-rose-400 sm:ml-2">{createLibError}</p>
            )}
          </form>
        )}

        {/* Inline Create Blank Course Form */}
        {showCreateCourse && (
          <form
            onSubmit={handleCreateBlankCourse}
            className="flex flex-col sm:flex-row sm:items-center gap-3 rounded-2xl border border-slate-800 bg-slate-900/90 p-4"
          >
            <input
              type="text"
              value={newCourseTitle}
              onChange={(e) => setNewCourseTitle(e.target.value)}
              placeholder="输入新课程名称"
              aria-label="新课程名称"
              className="flex-1 rounded-xl border border-slate-700 bg-slate-950 px-3.5 py-2 text-sm text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
            />
            <select
              aria-label="目标媒体库"
              value={newCourseLibraryId || libraries[0]?.id || ''}
              onChange={(e) => setNewCourseLibraryId(e.target.value)}
              className="rounded-xl border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white focus:border-indigo-500 focus:outline-none"
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
                className="rounded-xl bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors"
              >
                {creatingCourse ? '创建中...' : '创建并编辑'}
              </button>
              <button
                type="button"
                onClick={() => setShowCreateCourse(false)}
                className="rounded-xl border border-slate-700 px-3.5 py-2 text-sm text-slate-300 hover:bg-slate-800 transition-colors"
              >
                取消
              </button>
            </div>
            {createCourseError && (
              <p className="text-xs text-rose-400 sm:ml-2">{createCourseError}</p>
            )}
          </form>
        )}

        {/* Tag Filter Chips */}
        {tags.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <span className="inline-flex items-center gap-1 text-xs font-medium text-slate-400 mr-1">
              <TagIcon className="h-3.5 w-3.5" />
              <span>标签筛选</span>
            </span>
            {tags.map((tag) => {
              const active = selectedTag === tag.name;
              return (
                <Tooltip
                  key={tag.id}
                  content={active ? `取消按标签「${tag.name}」筛选` : `按标签「${tag.name}」筛选课程`}
                >
                  <button
                    type="button"
                    onClick={() => setSelectedTag(active ? null : tag.name)}
                    className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                      active
                        ? 'bg-sky-500/20 text-sky-300 border border-sky-500/40'
                        : 'bg-slate-900 text-slate-400 border border-slate-800 hover:border-slate-700 hover:text-slate-200'
                    }`}
                  >
                    {tag.name}
                    <span className="ml-1 opacity-60">{tag.course_count}</span>
                  </button>
                </Tooltip>
              );
            })}
          </div>
        )}

        {/* Empty State vs Course Grid */}
        {allCourses.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-slate-800 bg-slate-900/40 px-6 py-16 text-center">
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-indigo-500/10 text-indigo-400 mb-4">
              <Sparkles className="h-7 w-7" />
            </div>
            <h3 className="text-lg font-semibold text-white">还没有导入任何课程</h3>
            <p className="mt-2 text-sm text-slate-400 max-w-md mx-auto">
              请先前往“存储与导入”添加本地或远程课程目录，扫描视频后即可一键导入并结构化整理课程。
            </p>
            <Link
              to="/storage"
              className="mt-6 inline-flex items-center gap-2 rounded-xl bg-indigo-600 px-5 py-2.5 text-sm font-semibold text-white shadow-lg shadow-indigo-600/25 hover:bg-indigo-500 transition-colors"
            >
              <FolderInput className="h-4 w-4" />
              <span>前往存储与导入</span>
            </Link>
          </div>
        ) : filteredCourses.length === 0 ? (
          <div className="rounded-2xl border border-slate-800 bg-slate-900/40 px-6 py-14 text-center">
            <BookOpen className="mx-auto h-9 w-9 text-slate-500 mb-3" />
            <p className="text-base font-medium text-slate-200">没有符合当前筛选条件的课程</p>
            {hasActiveFilter && (
              <Tooltip content="重置媒体库、标签和关键词筛选">
                <button
                  type="button"
                  onClick={() => {
                    setSelectedLibraryId('all');
                    setSelectedTag(null);
                    setSearchQuery('');
                  }}
                  className="mt-4 inline-flex items-center rounded-xl border border-slate-700 bg-slate-800 px-4 py-2 text-sm text-slate-200 hover:bg-slate-700 transition-colors"
                >
                  清除筛选条件
                </button>
              </Tooltip>
            )}
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-5">
            {filteredCourses.map((course) => {
              const pct = Math.max(0, Math.min(100, Math.round(course.played_percentage || 0)));
              const hasCover = Boolean(course.cover_path || course.first_lesson_id);
              const durationText = formatDuration(course.total_duration_ms);

              return (
                <article
                  key={course.id}
                  className="group flex flex-col overflow-hidden rounded-2xl border border-slate-800/80 bg-slate-900/75 hover:border-indigo-500/40 hover:bg-slate-900 transition-all shadow-sm"
                >
                  <Link
                    to={`/courses/${course.id}`}
                    className="relative aspect-[16/10] w-full overflow-hidden bg-slate-800 block"
                  >
                    <CoursePoster
                      courseId={course.id}
                      title={course.title}
                      hasCover={hasCover}
                      tag={course.updated_at}
                    />
                    <div className="absolute inset-0 bg-gradient-to-t from-slate-950/80 via-transparent to-transparent opacity-80" />
                    {course.played && (
                      <span className="absolute top-2.5 right-2.5 inline-flex items-center gap-1 rounded-full bg-emerald-500/90 px-2.5 py-0.5 text-[11px] font-semibold text-white shadow">
                        <CheckCircle2 className="h-3 w-3" />
                        已完成
                      </span>
                    )}
                    <div className="absolute bottom-2.5 left-3 right-3 flex items-center justify-between text-xs text-slate-200">
                      <span>
                        {course.lesson_count} 节 · {course.chapter_count} 章
                      </span>
                      {durationText && (
                        <span className="inline-flex items-center gap-1 text-slate-300">
                          <Clock className="h-3 w-3" />
                          {durationText}
                        </span>
                      )}
                    </div>
                  </Link>

                  <div className="flex flex-1 flex-col p-4 space-y-3">
                    <div className="space-y-1 flex-1">
                      <Link
                        to={`/courses/${course.id}`}
                        className="block text-base font-semibold text-white line-clamp-2 group-hover:text-indigo-300 transition-colors"
                      >
                        {course.title}
                      </Link>
                      {course.instructor && (
                        <p className="text-xs text-slate-400">讲师：{course.instructor}</p>
                      )}
                    </div>

                    {course.tags && course.tags.length > 0 && (
                      <div className="flex flex-wrap gap-1.5">
                        {course.tags.slice(0, 3).map((tag) => (
                          <span
                            key={tag}
                            className="rounded-md bg-slate-800/90 px-2 py-0.5 text-[11px] text-slate-300 border border-slate-700/60"
                          >
                            {tag}
                          </span>
                        ))}
                      </div>
                    )}

                    {/* Progress bar */}
                    <div className="space-y-1.5 pt-1">
                      <div className="flex items-center justify-between text-xs text-slate-400">
                        <span>
                          已学 {course.played_count} / {course.lesson_count} 节
                        </span>
                        <span className="font-medium text-slate-300">{pct}%</span>
                      </div>
                      <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-800">
                        <div
                          className={`h-full rounded-full transition-all ${
                            course.played ? 'bg-emerald-500' : 'bg-indigo-500'
                          }`}
                          style={{ width: `${pct}%` }}
                        />
                      </div>
                    </div>

                    {/* Quick actions */}
                    <div className="flex items-center gap-2 pt-2 border-t border-slate-800/80">
                      <Tooltip content="进入课程播放页并续播上次进度">
                        <Link
                          to={`/courses/${course.id}`}
                          className="flex-1 inline-flex items-center justify-center gap-1.5 rounded-xl bg-indigo-600/90 px-3 py-1.5 text-xs font-semibold text-white hover:bg-indigo-500 transition-colors"
                        >
                          <Play className="h-3.5 w-3.5 fill-current" />
                          <span>开始学习</span>
                        </Link>
                      </Tooltip>
                      <Tooltip content="编辑课程信息、章节排序或运行 AI 整理">
                        <Link
                          to={`/courses/${course.id}/edit`}
                          aria-label={`编辑 ${course.title}`}
                          className="inline-flex items-center justify-center gap-1 rounded-xl border border-slate-700/80 bg-slate-800/70 px-3 py-1.5 text-xs font-medium text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
                        >
                          <Pencil className="h-3.5 w-3.5" />
                          <span>编辑</span>
                        </Link>
                      </Tooltip>
                    </div>
                  </div>
                </article>
              );
            })}
          </div>
        )}
      </section>

      <DeleteLibraryModal
        library={deletingLib}
        libraries={libraries}
        onClose={() => setDeletingLib(null)}
        onDeleted={handleLibraryDeleted}
      />
    </div>
  );
}
