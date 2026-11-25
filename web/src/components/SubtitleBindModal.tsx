import type { FormEvent } from 'react';
import { useCallback, useEffect, useState } from 'react';
import { Link2, Loader2, Search, Subtitles, Unlink, X } from 'lucide-react';
import { api } from '../api/client';
import { useToast } from './Toast';
import Tooltip from './Tooltip';
import type { IndexedSubtitle, LessonDetail } from '../types/coursefin';

export interface SubtitleBindModalProps {
  isOpen: boolean;
  lesson: LessonDetail | null;
  fallbackSourceId?: string;
  onClose: () => void;
  onChanged: () => Promise<void> | void;
}

export default function SubtitleBindModal({
  isOpen,
  lesson,
  fallbackSourceId,
  onClose,
  onChanged,
}: SubtitleBindModalProps) {
  const { toast } = useToast();
  const [filterMode, setFilterMode] = useState<'unmatched' | 'all'>('unmatched');
  const [searchInput, setSearchInput] = useState('');
  const [appliedSearch, setAppliedSearch] = useState('');
  const [subtitles, setSubtitles] = useState<IndexedSubtitle[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  const sourceId = lesson?.source_id || fallbackSourceId || '';

  const loadSubtitles = useCallback(async () => {
    if (!isOpen || !sourceId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await api.listSourceSubtitles(sourceId, {
        unmatched: filterMode === 'unmatched' ? true : undefined,
        search: appliedSearch.trim() || undefined,
      });
      setSubtitles(res.subtitles ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载字幕列表失败');
    } finally {
      setLoading(false);
    }
  }, [isOpen, sourceId, filterMode, appliedSearch]);

  useEffect(() => {
    if (!isOpen) return;
    setFilterMode('unmatched');
    setSearchInput('');
    setAppliedSearch('');
    setError(null);
  }, [isOpen, lesson?.id]);

  useEffect(() => {
    void loadSubtitles();
  }, [loadSubtitles]);

  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen || !lesson) return null;

  const boundSubtitles = lesson.subtitles ?? [];

  async function handleBind(subId: string) {
    if (!lesson || busyId) return;
    setBusyId(subId);
    setError(null);
    try {
      await api.bindSubtitle(subId, { media_file_id: lesson.media_file_id });
      toast.success('已关联字幕到该课时');
      await Promise.all([loadSubtitles(), Promise.resolve(onChanged())]);
    } catch (err) {
      const msg = err instanceof Error ? err.message : '关联字幕失败';
      setError(msg);
      toast.error(msg);
    } finally {
      setBusyId(null);
    }
  }

  async function handleUnbind(subId: string) {
    if (busyId) return;
    setBusyId(subId);
    setError(null);
    try {
      await api.bindSubtitle(subId, { media_file_id: null });
      toast.success('已解除该字幕关联');
      await Promise.all([loadSubtitles(), Promise.resolve(onChanged())]);
    } catch (err) {
      const msg = err instanceof Error ? err.message : '解绑字幕失败';
      setError(msg);
      toast.error(msg);
    } finally {
      setBusyId(null);
    }
  }

  function handleSearchSubmit(e: FormEvent) {
    e.preventDefault();
    setAppliedSearch(searchInput);
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-label={`管理课时字幕 ${lesson.title}`}
    >
      <div className="flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float">
        {/* Header */}
        <div className="flex items-center justify-between gap-3 border-b border-subtle px-5 py-4">
          <div className="flex items-center gap-2.5 min-w-0">
            <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-accent-soft text-accent-ink shrink-0">
              <Subtitles className="h-4 w-4" />
            </span>
            <div className="min-w-0">
              <h3 className="text-base font-bold text-ink-primary truncate">管理课时字幕</h3>
              <p className="text-xs text-ink-muted truncate">{lesson.title}</p>
            </div>
          </div>
          <Tooltip content="关闭弹窗">
            <button
              type="button"
              aria-label="关闭"
              onClick={onClose}
              className="rounded-lg p-1.5 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
            >
              <X className="h-5 w-5" />
            </button>
          </Tooltip>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-5 space-y-5">
          {error && (
            <div className="rounded-2xl border border-status-danger/30 bg-status-danger-soft px-3.5 py-2.5 text-xs text-status-danger-ink">
              {error}
            </div>
          )}

          {/* Currently Bound Subtitles */}
          <section className="space-y-2.5">
            <div className="flex items-center justify-between">
              <h4 className="text-xs font-semibold text-ink-primary">
                当前课时已关联字幕（{boundSubtitles.length}）
              </h4>
            </div>
            {boundSubtitles.length === 0 ? (
              <p className="rounded-2xl border border-dashed border-default bg-subtle/40 px-4 py-3 text-xs text-ink-muted">
                当前课时暂无已关联的外挂字幕，可在下方存储源字幕列表中搜索并手动绑定。
              </p>
            ) : (
              <div className="space-y-1.5">
                {boundSubtitles.map((sub) => (
                  <div
                    key={sub.id}
                    className="flex items-center justify-between gap-3 rounded-2xl border border-accent-ring/40 bg-accent-soft/50 px-3.5 py-2.5"
                  >
                    <div className="min-w-0 flex items-center gap-2">
                      <span className="text-xs font-semibold text-ink-primary truncate">
                        {sub.title || sub.language || '外挂字幕'}
                      </span>
                      <span className="rounded-md border border-accent-ring/40 bg-card px-1.5 py-0.5 text-[10px] font-medium uppercase text-accent-ink">
                        {sub.format}
                      </span>
                      {sub.language && (
                        <span className="text-[11px] text-ink-secondary">{sub.language}</span>
                      )}
                    </div>
                    <button
                      type="button"
                      disabled={busyId !== null}
                      onClick={() => void handleUnbind(sub.id)}
                      className="inline-flex items-center gap-1 rounded-xl border border-status-danger/30 bg-card px-2.5 py-1 text-xs font-medium text-status-danger-ink hover:bg-status-danger-soft disabled:opacity-50 transition-colors cursor-pointer shrink-0"
                    >
                      <Unlink className="h-3 w-3" />
                      <span>解绑</span>
                    </button>
                  </div>
                ))}
              </div>
            )}
          </section>

          {/* Source Subtitles Browser */}
          <section className="space-y-3 border-t border-subtle pt-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h4 className="text-xs font-semibold text-ink-primary">存储源字幕文件</h4>
              <div className="inline-flex rounded-xl border border-subtle bg-subtle p-0.5 text-xs">
                <button
                  type="button"
                  onClick={() => setFilterMode('unmatched')}
                  className={`rounded-lg px-2.5 py-1 font-medium transition-colors cursor-pointer ${
                    filterMode === 'unmatched'
                      ? 'bg-card text-ink-primary shadow-card'
                      : 'text-ink-secondary hover:text-ink-primary'
                  }`}
                >
                  仅看未匹配
                </button>
                <button
                  type="button"
                  onClick={() => setFilterMode('all')}
                  className={`rounded-lg px-2.5 py-1 font-medium transition-colors cursor-pointer ${
                    filterMode === 'all'
                      ? 'bg-card text-ink-primary shadow-card'
                      : 'text-ink-secondary hover:text-ink-primary'
                  }`}
                >
                  全部字幕
                </button>
              </div>
            </div>

            <form onSubmit={handleSearchSubmit} className="flex gap-2">
              <div className="relative flex-1">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-ink-muted" />
                <input
                  type="text"
                  value={searchInput}
                  onChange={(e) => setSearchInput(e.target.value)}
                  placeholder="按字幕文件名或相对路径搜索..."
                  aria-label="搜索字幕文件"
                  className="w-full rounded-xl border border-default bg-subtle/60 pl-8 pr-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors"
                />
              </div>
              <button
                type="submit"
                className="rounded-xl border border-default bg-card px-3 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
              >
                筛选
              </button>
            </form>

            {!sourceId ? (
              <p className="py-6 text-center text-xs text-ink-muted">
                无法确定该课时所属的存储源，请刷新后重试。
              </p>
            ) : loading ? (
              <div className="flex items-center justify-center py-8 gap-2 text-xs text-ink-secondary">
                <Loader2 className="h-4 w-4 animate-spin text-accent" />
                <span>加载字幕列表中...</span>
              </div>
            ) : subtitles.length === 0 ? (
              <p className="py-8 text-center text-xs text-ink-muted">
                {filterMode === 'unmatched'
                  ? '该存储源下暂无未匹配的字幕文件，可切换到「全部字幕」查看。'
                  : '未找到符合条件的字幕文件。'}
              </p>
            ) : (
              <div className="space-y-1.5">
                {subtitles.map((sub) => {
                  const isBoundToCurrent = sub.media_file_id === lesson.media_file_id;
                  const isBoundToOther = Boolean(sub.media_file_id) && !isBoundToCurrent;
                  return (
                    <div
                      key={sub.id}
                      className="flex items-center justify-between gap-3 rounded-2xl border border-subtle bg-subtle/40 px-3.5 py-2.5 hover:border-default transition-colors"
                    >
                      <div className="min-w-0 space-y-0.5">
                        <div className="flex items-center gap-1.5">
                          <p className="text-xs font-medium text-ink-primary truncate">
                            {sub.file_name}
                          </p>
                          <span className="rounded-md border border-subtle bg-card px-1.5 py-0.5 text-[10px] font-medium uppercase text-ink-secondary shrink-0">
                            {sub.format}
                          </span>
                          {isBoundToCurrent && (
                            <span className="rounded-full bg-status-success-soft px-2 py-0.5 text-[10px] font-medium text-status-success-ink shrink-0">
                              已关联本节
                            </span>
                          )}
                          {isBoundToOther && (
                            <span className="rounded-full bg-status-warning-soft px-2 py-0.5 text-[10px] font-medium text-status-warning-ink shrink-0">
                              已关联其他视频
                            </span>
                          )}
                        </div>
                        <p className="text-[11px] text-ink-muted truncate">{sub.relative_path}</p>
                      </div>

                      {!isBoundToCurrent && (
                        <button
                          type="button"
                          disabled={busyId !== null}
                          onClick={() => void handleBind(sub.id)}
                          className="inline-flex items-center gap-1 rounded-xl bg-accent px-3 py-1.5 text-xs font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer shrink-0"
                        >
                          <Link2 className="h-3.5 w-3.5" />
                          <span>{isBoundToOther ? '改绑到本节' : '绑定到本节'}</span>
                        </button>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}
