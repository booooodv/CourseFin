import type { FormEvent } from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  CheckCircle2,
  ChevronRight,
  Folder,
  FolderInput,
  FolderOpen,
  HardDrive,
  Loader2,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  Video,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import DirectoryPickerModal from '../components/DirectoryPickerModal';
import ImportCourseModal from '../components/ImportCourseModal';
import { useToast } from '../components/Toast';
import Tooltip from '../components/Tooltip';
import { useLibrary } from '../contexts/LibraryContext';
import type {
  CourseDetail,
  IndexedDir,
  Library,
  MediaFile,
  ScanState,
  StorageSource,
} from '../types/coursefin';

const FIELD_INPUT_CLASS =
  'w-full rounded-xl border border-default bg-subtle/60 px-3.5 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors';

function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let idx = 0;
  let val = bytes;
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024;
    idx++;
  }
  return `${val.toFixed(idx === 0 ? 0 : 1)} ${units[idx]}`;
}

function basenameFromPath(absPath: string): string {
  const cleaned = absPath.trim().replace(/[\\/]+$/, '');
  if (!cleaned || /^[A-Za-z]:$/.test(cleaned)) return '';
  const parts = cleaned.split(/[\\/]+/).filter(Boolean);
  return parts[parts.length - 1] ?? '';
}

export default function StoragePage() {
  const { toast } = useToast();
  const libCtx = useLibrary();

  const [sources, setSources] = useState<StorageSource[]>([]);
  const [localLibraries, setLocalLibraries] = useState<Library[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [importedCourse, setImportedCourse] = useState<CourseDetail | null>(null);

  // Add source form
  const [showAddForm, setShowAddForm] = useState(false);
  const [showDirPicker, setShowDirPicker] = useState(false);
  const [sourceName, setSourceName] = useState('');
  const [sourceRootPath, setSourceRootPath] = useState('');
  const [creatingSource, setCreatingSource] = useState(false);

  // Active source browsing
  const [selectedSourceId, setSelectedSourceId] = useState<string>('');
  const [currentPrefix, setCurrentPrefix] = useState<string>('');
  const [dirs, setDirs] = useState<IndexedDir[]>([]);
  const [files, setFiles] = useState<MediaFile[]>([]);
  const [fileStatusFilter, setFileStatusFilter] = useState<'all' | 'active' | 'missing'>('all');
  const [fileSearch, setFileSearch] = useState('');
  const [loadingBrowse, setLoadingBrowse] = useState(false);

  // Scan states by source ID
  const [scanStates, setScanStates] = useState<Record<string, ScanState>>({});
  const pollTimersRef = useRef<Record<string, number>>({});
  const pollInFlightRef = useRef<Record<string, boolean>>({});
  const mountedRef = useRef(true);
  const browseStateRef = useRef({
    selectedSourceId: '',
    currentPrefix: '',
    fileStatusFilter: 'all' as 'all' | 'active' | 'missing',
    fileSearch: '',
  });
  browseStateRef.current = {
    selectedSourceId,
    currentPrefix,
    fileStatusFilter,
    fileSearch,
  };

  // Import modal
  const [importTarget, setImportTarget] = useState<{ sourceId: string; dirRelPath: string } | null>(
    null,
  );

  const libraries = libCtx.isProvided ? libCtx.libraries : localLibraries;

  // Cleanup all polling timers on unmount
  useEffect(() => {
    mountedRef.current = true;
    const timers = pollTimersRef.current;
    return () => {
      mountedRef.current = false;
      for (const id of Object.keys(timers)) {
        window.clearInterval(timers[id]);
        delete timers[id];
      }
    };
  }, []);

  const loadBrowseData = useCallback(
    async (sourceId: string, prefix: string, status: string, search: string) => {
      if (!sourceId) {
        setDirs([]);
        setFiles([]);
        return;
      }
      setLoadingBrowse(true);
      try {
        const [dirsRes, filesRes] = await Promise.all([
          api.listDirs(sourceId, prefix),
          api.listFiles(sourceId, {
            prefix,
            status: status === 'all' ? undefined : status,
            search: search.trim() || undefined,
          }),
        ]);
        setDirs(dirsRes.dirs ?? []);
        setFiles(filesRes.files ?? []);
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载目录和文件失败');
      } finally {
        setLoadingBrowse(false);
      }
    },
    [],
  );

  const stopPolling = useCallback((sourceId: string) => {
    const timer = pollTimersRef.current[sourceId];
    if (timer !== undefined) {
      window.clearInterval(timer);
      delete pollTimersRef.current[sourceId];
    }
    delete pollInFlightRef.current[sourceId];
  }, []);

  const startPolling = useCallback(
    (sourceId: string) => {
      stopPolling(sourceId);
      const poll = async () => {
        if (pollInFlightRef.current[sourceId]) {
          return;
        }
        pollInFlightRef.current[sourceId] = true;
        try {
          const st = await api.getScanStatus(sourceId);
          if (!mountedRef.current) return;
          setScanStates((prev) => ({ ...prev, [sourceId]: st }));
          if (!st.running) {
            stopPolling(sourceId);
            const srcRes = await api.listSources();
            if (!mountedRef.current) return;
            setSources(srcRes.sources ?? []);
            const cur = browseStateRef.current;
            if (sourceId === cur.selectedSourceId) {
              await loadBrowseData(
                sourceId,
                cur.currentPrefix,
                cur.fileStatusFilter,
                cur.fileSearch,
              );
            }
          }
        } catch (err) {
          stopPolling(sourceId);
          if (mountedRef.current) {
            setError(err instanceof Error ? err.message : '查询扫描状态失败');
          }
        } finally {
          pollInFlightRef.current[sourceId] = false;
        }
      };

      pollTimersRef.current[sourceId] = window.setInterval(() => {
        void poll();
      }, 450);
    },
    [loadBrowseData, stopPolling],
  );

  const loadInitial = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      let srcList: StorageSource[] = [];
      if (libCtx.isProvided) {
        const srcRes = await api.listSources();
        srcList = srcRes.sources ?? [];
      } else {
        const [srcRes, libRes] = await Promise.all([api.listSources(), api.listLibraries()]);
        srcList = srcRes.sources ?? [];
        setLocalLibraries(libRes.libraries ?? []);
      }

      setSources(srcList);
      if (srcList.length > 0) {
        const initialId = srcList[0].id;
        setSelectedSourceId(initialId);
        await loadBrowseData(initialId, '', 'all', '');
        const statuses = await Promise.all(
          srcList.map((src) => api.getScanStatus(src.id).catch(() => null)),
        );
        if (!mountedRef.current) return;
        const nextStates: Record<string, ScanState> = {};
        for (const st of statuses) {
          if (st) {
            nextStates[st.source_id] = st;
          }
        }
        if (Object.keys(nextStates).length > 0) {
          setScanStates((prev) => ({ ...prev, ...nextStates }));
        }
        for (const st of statuses) {
          if (st?.running) {
            startPolling(st.source_id);
          }
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载存储源失败');
    } finally {
      setLoading(false);
    }
  }, [libCtx.isProvided, loadBrowseData, startPolling]);

  useEffect(() => {
    void loadInitial();
  }, [loadInitial]);

  async function handleStartScan(sourceId: string) {
    setError(null);
    try {
      const res = await api.scanSource(sourceId, true);
      if ('running' in res) {
        setScanStates((prev) => ({ ...prev, [sourceId]: res }));
        if (res.running) {
          startPolling(sourceId);
          toast.info('已启动后台扫描，正在索引视频与字幕');
        }
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : '触发扫描失败';
      setError(msg);
      toast.error(msg);
    }
  }

  async function handleCreateSource(e: FormEvent) {
    e.preventDefault();
    if (creatingSource) return;
    if (!sourceName.trim() || !sourceRootPath.trim()) {
      setError('请填写存储源名称和本地目录路径');
      return;
    }
    setCreatingSource(true);
    setError(null);
    try {
      const created = await api.createSource(sourceName.trim(), 'local', sourceRootPath.trim());
      setSources((prev) => [...prev, created]);
      setSourceName('');
      setSourceRootPath('');
      setShowAddForm(false);
      setSelectedSourceId(created.id);
      setCurrentPrefix('');
      toast.success(`已添加存储源「${created.name}」`);
      await loadBrowseData(created.id, '', fileStatusFilter, fileSearch);
    } catch (err) {
      const msg = err instanceof Error ? err.message : '添加存储源失败';
      setError(msg);
      toast.error(msg);
    } finally {
      setCreatingSource(false);
    }
  }

  async function handleDeleteSource(sourceId: string) {
    setError(null);
    const targetName = sources.find((s) => s.id === sourceId)?.name;
    stopPolling(sourceId);
    try {
      await api.deleteSource(sourceId);
      const remaining = sources.filter((s) => s.id !== sourceId);
      setSources(remaining);
      toast.success(targetName ? `已删除存储源「${targetName}」` : '已删除存储源');
      if (selectedSourceId === sourceId) {
        const nextId = remaining[0]?.id ?? '';
        setSelectedSourceId(nextId);
        setCurrentPrefix('');
        await loadBrowseData(nextId, '', fileStatusFilter, fileSearch);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : '删除存储源失败';
      setError(msg);
      toast.error(msg);
    }
  }

  function handleDirectorySelected(selectedPath: string) {
    setSourceRootPath(selectedPath);
    if (!sourceName.trim()) {
      const guessed = basenameFromPath(selectedPath);
      if (guessed) {
        setSourceName(guessed);
      }
    }
    toast.info(`已选择目录：${selectedPath}`);
  }

  async function handleSelectSource(sourceId: string) {
    setSelectedSourceId(sourceId);
    setCurrentPrefix('');
    await loadBrowseData(sourceId, '', fileStatusFilter, fileSearch);
  }

  async function handleNavigatePrefix(nextPrefix: string) {
    setCurrentPrefix(nextPrefix);
    await loadBrowseData(selectedSourceId, nextPrefix, fileStatusFilter, fileSearch);
  }

  async function handleFilterChange(nextStatus: 'all' | 'active' | 'missing') {
    setFileStatusFilter(nextStatus);
    await loadBrowseData(selectedSourceId, currentPrefix, nextStatus, fileSearch);
  }

  async function handleFileSearchSubmit(e: FormEvent) {
    e.preventDefault();
    await loadBrowseData(selectedSourceId, currentPrefix, fileStatusFilter, fileSearch);
  }

  const missingCount = files.filter((f) => f.status === 'missing').length;
  const prefixParts = currentPrefix ? currentPrefix.split('/').filter(Boolean) : [];
  const selectedSource = sources.find((s) => s.id === selectedSourceId) ?? null;

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center py-24 text-ink-muted gap-3">
        <Loader2 className="h-7 w-7 animate-spin text-accent" />
        <p className="text-sm font-medium">正在加载存储源与索引目录...</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-ink-primary">存储与导入</h1>
          <p className="text-xs text-ink-muted mt-1">
            配置本地媒体目录、运行视频扫描并一键导入为结构化课程。
          </p>
        </div>

        <Tooltip content="新增一个本地媒体目录用于扫描和导入课程">
          <button
            type="button"
            onClick={() => setShowAddForm((v) => !v)}
            className="inline-flex items-center gap-1.5 rounded-full bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover transition-colors cursor-pointer"
          >
            <Plus className="h-4 w-4" />
            <span>添加存储源</span>
          </button>
        </Tooltip>
      </div>

      {error && (
        <div className="flex items-center justify-between rounded-2xl border border-status-danger/30 bg-status-danger-soft px-4 py-3 text-sm text-status-danger-ink">
          <span>{error}</span>
          <Tooltip content="关闭错误提示">
            <button
              type="button"
              aria-label="关闭错误提示"
              onClick={() => setError(null)}
              className="rounded-lg p-1 text-status-danger-ink hover:bg-card/60 transition-colors cursor-pointer"
            >
              <X className="h-4 w-4" />
            </button>
          </Tooltip>
        </div>
      )}

      {importedCourse && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-status-success/30 bg-status-success-soft px-4 py-3 text-sm text-status-success-ink">
          <span className="flex items-center gap-2 font-medium">
            <CheckCircle2 className="h-4 w-4 text-status-success-ink shrink-0" />
            已成功导入课程「{importedCourse.title}」
          </span>
          <div className="flex items-center gap-3 text-xs font-semibold">
            <Link to={`/courses/${importedCourse.id}`} className="text-ink-primary underline hover:text-accent-ink">
              去播放
            </Link>
            <Link to={`/courses/${importedCourse.id}/edit`} className="text-accent-ink underline hover:opacity-80">
              编辑课程
            </Link>
            <button
              type="button"
              onClick={() => setImportedCourse(null)}
              className="text-status-success-ink hover:opacity-80 cursor-pointer"
            >
              关闭
            </button>
          </div>
        </div>
      )}

      {/* Add Storage Source Form */}
      {showAddForm && (
        <form
          onSubmit={handleCreateSource}
          className="rounded-3xl bg-card border border-subtle shadow-card p-6 space-y-4"
        >
          <h2 className="text-sm font-semibold text-ink-primary">添加本地目录存储源</h2>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <label htmlFor="source-name-input" className="block text-xs font-medium text-ink-secondary">
                存储源名称
              </label>
              <input
                id="source-name-input"
                type="text"
                value={sourceName}
                onChange={(e) => setSourceName(e.target.value)}
                placeholder="例如：我的课程硬盘"
                className={FIELD_INPUT_CLASS}
                required
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor="source-path-input" className="block text-xs font-medium text-ink-secondary">
                本地目录绝对路径
              </label>
              <div className="flex items-center gap-2">
                <input
                  id="source-path-input"
                  type="text"
                  value={sourceRootPath}
                  onChange={(e) => setSourceRootPath(e.target.value)}
                  placeholder="/Users/name/Courses"
                  className={`flex-1 min-w-0 ${FIELD_INPUT_CLASS}`}
                  required
                />
                <Tooltip content="浏览服务器上的本地目录并选择绝对路径">
                  <button
                    type="button"
                    onClick={() => setShowDirPicker(true)}
                    className="shrink-0 inline-flex items-center gap-1.5 rounded-xl border border-default bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors cursor-pointer"
                  >
                    <FolderOpen className="h-4 w-4 text-accent-ink" />
                    <span>浏览目录</span>
                  </button>
                </Tooltip>
              </div>
            </div>
          </div>
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setShowAddForm(false)}
              className="rounded-xl border border-default bg-card px-4 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
            >
              取消
            </button>
            <Tooltip content="保存此存储源配置，随后可执行扫描">
              <button
                type="submit"
                disabled={creatingSource}
                className="rounded-xl bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
              >
                {creatingSource ? '保存中...' : '确认添加'}
              </button>
            </Tooltip>
          </div>
        </form>
      )}

      {/* Sources List */}
      <section className="space-y-3">
        <h2 className="text-sm font-semibold text-ink-primary">已配置存储源</h2>
        {sources.length === 0 ? (
          <div className="rounded-3xl border border-dashed border-default bg-card/60 p-10 text-center text-sm text-ink-muted">
            暂未配置任何存储源，请先点击右上角“添加存储源”。
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {sources.map((src) => {
              const st = scanStates[src.id];
              const isRunning = Boolean(st?.running);
              const isSelected = src.id === selectedSourceId;

              return (
                <div
                  key={src.id}
                  className={`relative overflow-hidden rounded-3xl border bg-card p-5 space-y-3.5 shadow-card transition-all ${
                    isSelected
                      ? 'border-accent/40 ring-2 ring-accent-ring shadow-card-hover'
                      : 'border-subtle hover:border-default hover:shadow-card-hover'
                  }`}
                >
                  {isSelected && (
                    <span
                      aria-hidden="true"
                      className="absolute left-0 top-5 bottom-5 w-1 rounded-r-full bg-accent"
                    />
                  )}
                  <div className="flex items-start justify-between gap-3">
                    <button
                      type="button"
                      onClick={() => void handleSelectSource(src.id)}
                      className="flex items-start gap-3 text-left min-w-0 flex-1 cursor-pointer"
                    >
                      <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl bg-accent-soft text-accent-ink">
                        <HardDrive className="h-4 w-4" />
                      </span>
                      <div className="min-w-0">
                        <p className="text-sm font-semibold text-ink-primary truncate">{src.name}</p>
                        <p className="text-xs text-ink-secondary truncate">{src.root_path}</p>
                        <p className="text-[11px] text-ink-muted mt-1">
                          已索引 {src.media_count} 个视频
                          {src.last_scanned_at ? ` · 上次扫描 ${src.last_scanned_at}` : ''}
                        </p>
                      </div>
                    </button>

                    <div className="flex items-center gap-1.5 shrink-0">
                      <Tooltip content="扫描该目录下的视频与字幕文件并更新索引">
                        <button
                          type="button"
                          disabled={isRunning}
                          onClick={() => void handleStartScan(src.id)}
                          className="inline-flex items-center gap-1.5 rounded-xl border border-accent-ring bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent-ink hover:bg-accent hover:text-ink-inverse disabled:opacity-60 transition-colors cursor-pointer"
                        >
                          <RefreshCw className={`h-3.5 w-3.5 ${isRunning ? 'animate-spin' : ''}`} />
                          <span>{isRunning ? '扫描中...' : '扫描'}</span>
                        </button>
                      </Tooltip>

                      <Tooltip content="删除此存储源配置（不删除本地文件）">
                        <button
                          type="button"
                          aria-label={`删除存储源 ${src.name}`}
                          onClick={() => void handleDeleteSource(src.id)}
                          className="rounded-xl p-2 text-ink-muted hover:bg-status-danger-soft hover:text-status-danger-ink transition-colors cursor-pointer"
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </Tooltip>
                    </div>
                  </div>

                  {st?.last_report && !isRunning && (
                    <div className="rounded-2xl border border-subtle bg-subtle px-3.5 py-2 text-[11px] text-ink-secondary flex flex-wrap gap-x-4 gap-y-1">
                      <span>总视频：{st.last_report.total_videos}</span>
                      <span className="font-medium text-status-success-ink">
                        新增：{st.last_report.added_videos}
                      </span>
                      <span>字幕：{st.last_report.total_subtitles}</span>
                      {st.last_report.missing_videos > 0 && (
                        <span className="font-medium text-status-warning-ink">
                          缺失：{st.last_report.missing_videos}
                        </span>
                      )}
                    </div>
                  )}

                  {st?.last_error && (
                    <p className="text-xs text-status-danger-ink">扫描出错：{st.last_error}</p>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </section>

      {/* Directory & File Browser */}
      {selectedSource && (
        <section className="rounded-3xl bg-card border border-subtle shadow-card p-6 space-y-5">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-subtle pb-4">
            <div className="flex flex-wrap items-center gap-1.5 text-xs">
              <span className="font-semibold text-ink-primary mr-1">目录浏览：</span>
              <button
                type="button"
                onClick={() => void handleNavigatePrefix('')}
                className={`rounded-full px-3 py-1 transition-colors cursor-pointer ${
                  currentPrefix === ''
                    ? 'bg-accent-soft text-accent-ink font-semibold'
                    : 'text-ink-secondary hover:bg-subtle hover:text-ink-primary'
                }`}
              >
                {selectedSource.name}
              </button>
              {prefixParts.map((part, idx) => {
                const targetPrefix = prefixParts.slice(0, idx + 1).join('/');
                return (
                  <span key={targetPrefix} className="inline-flex items-center gap-1">
                    <ChevronRight className="h-3.5 w-3.5 text-ink-muted" />
                    <button
                      type="button"
                      onClick={() => void handleNavigatePrefix(targetPrefix)}
                      className={`rounded-full px-3 py-1 transition-colors cursor-pointer ${
                        targetPrefix === currentPrefix
                          ? 'bg-accent-soft text-accent-ink font-semibold'
                          : 'text-ink-secondary hover:bg-subtle hover:text-ink-primary'
                      }`}
                    >
                      {part}
                    </button>
                  </span>
                );
              })}
            </div>

            {(currentPrefix || files.length > 0) && (
              <Tooltip content="将当前浏览的目录解析并导入为一门课程">
                <button
                  type="button"
                  onClick={() =>
                    setImportTarget({ sourceId: selectedSource.id, dirRelPath: currentPrefix })
                  }
                  className="inline-flex items-center gap-1.5 rounded-full bg-accent px-3.5 py-1.5 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover transition-colors cursor-pointer"
                >
                  <FolderInput className="h-3.5 w-3.5" />
                  <span>导入当前目录</span>
                </button>
              </Tooltip>
            )}
          </div>

          {loadingBrowse ? (
            <div className="flex items-center justify-center py-12 text-ink-muted gap-2 text-xs">
              <Loader2 className="h-5 w-5 animate-spin text-accent" />
              <span>正在加载目录内容...</span>
            </div>
          ) : (
            <div className="space-y-6">
              {/* Subdirectories */}
              <div className="space-y-2.5">
                <h3 className="text-xs font-semibold uppercase tracking-wider text-ink-muted">
                  子目录 ({dirs.length})
                </h3>
                {dirs.length === 0 ? (
                  <p className="text-xs text-ink-muted py-2">当前路径下无子目录。</p>
                ) : (
                  <div className="divide-y divide-subtle rounded-2xl border border-subtle bg-subtle/40 overflow-hidden">
                    {dirs.map((dir) => (
                      <div
                        key={dir.rel_path}
                        className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 hover:bg-subtle/80 transition-colors"
                      >
                        <button
                          type="button"
                          onClick={() => void handleNavigatePrefix(dir.rel_path)}
                          className="flex items-center gap-3 text-left min-w-0 flex-1 group cursor-pointer"
                        >
                          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-status-warning-soft text-status-warning-ink">
                            <Folder className="h-4 w-4" />
                          </span>
                          <div className="min-w-0">
                            <p className="text-sm font-medium text-ink-primary group-hover:text-accent-ink truncate">
                              {dir.name}
                            </p>
                            <p className="text-[11px] text-ink-muted">
                              共 {dir.file_count} 个视频 · 直属 {dir.direct_files} 个
                            </p>
                          </div>
                        </button>

                        <Tooltip content="预览该目录结构并导入为课程">
                          <button
                            type="button"
                            onClick={() =>
                              setImportTarget({ sourceId: selectedSource.id, dirRelPath: dir.rel_path })
                            }
                            className="inline-flex items-center gap-1.5 rounded-full bg-accent px-3.5 py-1.5 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover transition-colors cursor-pointer"
                          >
                            <FolderInput className="h-3.5 w-3.5" />
                            <span>一键导入为课程</span>
                          </button>
                        </Tooltip>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {/* Files list */}
              <div className="space-y-3">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <h3 className="text-xs font-semibold uppercase tracking-wider text-ink-muted">
                      媒体文件 ({files.length})
                    </h3>
                    {missingCount > 0 && (
                      <span className="inline-flex items-center gap-1 rounded-full border border-status-warning/30 bg-status-warning-soft px-2.5 py-0.5 text-[11px] font-medium text-status-warning-ink">
                        <AlertTriangle className="h-3 w-3" />
                        <span>{missingCount} 个文件缺失</span>
                      </span>
                    )}
                  </div>

                  <div className="flex flex-wrap items-center gap-2">
                    <div className="inline-flex rounded-xl border border-subtle bg-muted p-0.5 text-xs">
                      {(
                        [
                          { id: 'all', label: '全部', tip: '显示所有状态的媒体文件' },
                          { id: 'active', label: '正常', tip: '仅显示磁盘上存在的有效文件' },
                          { id: 'missing', label: '缺失', tip: '仅显示扫描时未找到的缺失文件' },
                        ] as const
                      ).map((opt) => (
                        <Tooltip key={opt.id} content={opt.tip}>
                          <button
                            type="button"
                            onClick={() => void handleFilterChange(opt.id)}
                            className={`rounded-lg px-2.5 py-1 transition-all cursor-pointer ${
                              fileStatusFilter === opt.id
                                ? 'bg-card text-ink-primary font-semibold shadow-card'
                                : 'text-ink-secondary hover:text-ink-primary'
                            }`}
                          >
                            {opt.label}
                          </button>
                        </Tooltip>
                      ))}
                    </div>

                    <form onSubmit={handleFileSearchSubmit} className="relative">
                      <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-ink-muted" />
                      <input
                        type="search"
                        value={fileSearch}
                        onChange={(e) => setFileSearch(e.target.value)}
                        placeholder="搜索文件名..."
                        className="rounded-full border border-default bg-subtle/60 pl-8 pr-3.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
                      />
                    </form>
                  </div>
                </div>

                {files.length === 0 ? (
                  <p className="text-xs text-ink-muted py-6 text-center border border-dashed border-default rounded-2xl">
                    暂无匹配的媒体文件。
                  </p>
                ) : (
                  <div className="divide-y divide-subtle rounded-2xl border border-subtle bg-subtle/40 max-h-80 overflow-y-auto">
                    {files.map((file) => (
                      <div
                        key={file.id}
                        className="flex items-center justify-between gap-3 px-4 py-2.5 text-xs hover:bg-subtle/70 transition-colors"
                      >
                        <div className="flex items-center gap-2.5 min-w-0">
                          <Video className="h-4 w-4 text-ink-muted shrink-0" />
                          <div className="min-w-0">
                            <p className="text-ink-primary font-medium truncate">{file.file_name}</p>
                            <p className="text-[11px] text-ink-muted truncate">
                              {file.relative_path} · {formatBytes(file.file_size)}
                            </p>
                          </div>
                        </div>

                        {file.status === 'missing' ? (
                          <span className="shrink-0 rounded-full border border-status-danger/30 bg-status-danger-soft px-2.5 py-0.5 text-[11px] font-medium text-status-danger-ink">
                            缺失
                          </span>
                        ) : (
                          <span className="shrink-0 rounded-full border border-status-success/30 bg-status-success-soft px-2.5 py-0.5 text-[11px] font-medium text-status-success-ink">
                            正常
                          </span>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </section>
      )}

      {/* Server Directory Picker Modal */}
      <DirectoryPickerModal
        isOpen={showDirPicker}
        initialPath={sourceRootPath}
        onClose={() => setShowDirPicker(false)}
        onSelect={handleDirectorySelected}
      />

      {/* Import Course Modal */}
      {importTarget && (
        <ImportCourseModal
          isOpen={true}
          sourceId={importTarget.sourceId}
          dirRelPath={importTarget.dirRelPath}
          libraries={libraries}
          onClose={() => setImportTarget(null)}
          onLibraryCreated={(lib) => {
            if (!libCtx.isProvided) {
              setLocalLibraries((prev) => [...prev, lib]);
            }
            libCtx.registerLibrary(lib);
          }}
          onImported={(course) => {
            setImportedCourse(course);
            libCtx.bumpCatalogVersion();
            toast.success(`已成功导入课程「${course.title}」`);
          }}
        />
      )}
    </div>
  );
}
