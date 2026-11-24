import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowUp,
  Check,
  ChevronRight,
  Folder,
  FolderOpen,
  HardDrive,
  Loader2,
  Search,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import Tooltip from './Tooltip';
import type { FsBrowseResponse } from '../types/coursefin';

interface DirectoryPickerModalProps {
  isOpen: boolean;
  initialPath?: string;
  onClose: () => void;
  onSelect: (selectedPath: string) => void;
}

interface BreadcrumbPart {
  label: string;
  path: string;
}

function buildBreadcrumbs(absPath: string): BreadcrumbPart[] {
  const trimmed = absPath.trim();
  if (!trimmed) return [];

  // Windows drive path (e.g. C:\Users\Courses)
  const winMatch = /^([A-Za-z]:)[\\/]?(.*)$/.exec(trimmed);
  if (winMatch) {
    const drive = `${winMatch[1]}\\`;
    const rest = winMatch[2].split(/[\\/]+/).filter(Boolean);
    const parts: BreadcrumbPart[] = [{ label: drive, path: drive }];
    let acc = winMatch[1];
    for (const seg of rest) {
      acc = `${acc}\\${seg}`;
      parts.push({ label: seg, path: acc });
    }
    return parts;
  }

  // POSIX path
  const segments = trimmed.split('/').filter(Boolean);
  const parts: BreadcrumbPart[] = [{ label: '/', path: '/' }];
  let acc = '';
  for (const seg of segments) {
    acc = `${acc}/${seg}`;
    parts.push({ label: seg, path: acc });
  }
  return parts;
}

export default function DirectoryPickerModal({
  isOpen,
  initialPath,
  onClose,
  onSelect,
}: DirectoryPickerModalProps) {
  const [data, setData] = useState<FsBrowseResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fallbackNotice, setFallbackNotice] = useState<string | null>(null);
  const [showHidden, setShowHidden] = useState(false);
  const [filterText, setFilterText] = useState('');

  const reqSeqRef = useRef(0);

  const loadDirectory = useCallback(
    async (targetPath?: string, hidden = false, allowFallback = false) => {
      const seq = ++reqSeqRef.current;
      setLoading(true);
      setError(null);
      if (!allowFallback) {
        setFallbackNotice(null);
      }

      try {
        const res = await api.browseFsDirs(targetPath, hidden);
        if (seq !== reqSeqRef.current) return;
        setData(res);
        setFilterText('');
      } catch (err) {
        if (seq !== reqSeqRef.current) return;
        if (allowFallback && targetPath?.trim()) {
          try {
            const fallbackRes = await api.browseFsDirs(undefined, hidden);
            if (seq !== reqSeqRef.current) return;
            setData(fallbackRes);
            setFilterText('');
            setFallbackNotice(`原路径「${targetPath.trim()}」无法访问，已回退到默认目录`);
          } catch (fallbackErr) {
            if (seq !== reqSeqRef.current) return;
            setError(fallbackErr instanceof Error ? fallbackErr.message : '读取服务器目录失败');
          }
        } else {
          setError(err instanceof Error ? err.message : '读取服务器目录失败');
        }
      } finally {
        if (seq === reqSeqRef.current) {
          setLoading(false);
        }
      }
    },
    [],
  );

  useEffect(() => {
    if (!isOpen) return;
    setData(null);
    setError(null);
    setShowHidden(false);
    setFallbackNotice(null);
    setFilterText('');
    const init = initialPath?.trim() || undefined;
    void loadDirectory(init, false, Boolean(init));
  }, [isOpen, initialPath, loadDirectory]);

  useEffect(() => {
    if (!isOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [isOpen, onClose]);

  const breadcrumbs = useMemo(
    () => buildBreadcrumbs(data?.current_path ?? ''),
    [data?.current_path],
  );

  const filteredEntries = useMemo(() => {
    const entries = data?.entries ?? [];
    const q = filterText.trim().toLowerCase();
    if (!q) return entries;
    return entries.filter((entry) => entry.name.toLowerCase().includes(q));
  }, [data?.entries, filterText]);

  if (!isOpen) return null;

  function handleToggleHidden(nextHidden: boolean) {
    setShowHidden(nextHidden);
    void loadDirectory(data?.current_path || undefined, nextHidden, false);
  }

  function handleConfirm() {
    if (!data?.current_path) return;
    onSelect(data.current_path);
    onClose();
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/80 backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="dir-picker-title"
    >
      <div className="flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-slate-800 bg-slate-900 shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-slate-800 px-5 py-4">
          <div className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-indigo-500/15 text-indigo-400">
              <FolderOpen className="h-4 w-4" />
            </span>
            <div>
              <h2 id="dir-picker-title" className="text-base font-bold text-white">
                选择本地目录
              </h2>
              <p className="text-xs text-slate-400">
                浏览 CourseFin 运行主机上的文件夹，点击目录可逐级进入。
              </p>
            </div>
          </div>

          <Tooltip content="关闭目录选择器">
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

        {/* Quick roots + toolbar */}
        <div className="border-b border-slate-800/80 bg-slate-950/50 px-5 py-3 space-y-2.5">
          {data?.roots && data.roots.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-[11px] font-medium text-slate-500 mr-1">快捷入口：</span>
              {data.roots.map((root) => {
                const isActive = data.current_path === root.path;
                return (
                  <Tooltip key={root.path} content={`跳转到 ${root.path}`}>
                    <button
                      type="button"
                      onClick={() => void loadDirectory(root.path, showHidden, false)}
                      className={`inline-flex items-center gap-1 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors ${
                        isActive
                          ? 'bg-indigo-600 text-white'
                          : 'border border-slate-800 bg-slate-900 text-slate-300 hover:border-slate-700 hover:text-white'
                      }`}
                    >
                      <HardDrive className="h-3 w-3" />
                      <span>{root.label}</span>
                    </button>
                  </Tooltip>
                );
              })}
            </div>
          )}

          {/* Breadcrumb + Up button */}
          <div className="flex flex-wrap items-center gap-2">
            <Tooltip content="返回上一级目录">
              <button
                type="button"
                disabled={loading || !data?.parent_path}
                onClick={() => data?.parent_path && void loadDirectory(data.parent_path, showHidden, false)}
                className="inline-flex items-center gap-1 rounded-lg border border-slate-700 bg-slate-800/80 px-2.5 py-1 text-xs font-medium text-slate-200 hover:bg-slate-800 disabled:opacity-40 disabled:pointer-events-none transition-colors"
              >
                <ArrowUp className="h-3.5 w-3.5" />
                <span>返回上级</span>
              </button>
            </Tooltip>

            <div className="flex flex-1 flex-wrap items-center gap-0.5 rounded-lg border border-slate-800 bg-slate-900/90 px-2.5 py-1 text-xs min-w-[200px]">
              {breadcrumbs.map((part, idx) => (
                <span key={part.path} className="inline-flex items-center">
                  {idx > 0 && <ChevronRight className="h-3 w-3 text-slate-600 mx-0.5" />}
                  <button
                    type="button"
                    onClick={() => void loadDirectory(part.path, showHidden, false)}
                    className={`rounded px-1.5 py-0.5 transition-colors ${
                      idx === breadcrumbs.length - 1
                        ? 'font-semibold text-indigo-300'
                        : 'text-slate-400 hover:text-white'
                    }`}
                  >
                    {part.label}
                  </button>
                </span>
              ))}
            </div>
          </div>

          {/* Search filter + show hidden */}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="relative flex-1 min-w-[180px]">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-slate-500" />
              <input
                type="search"
                value={filterText}
                onChange={(e) => setFilterText(e.target.value)}
                placeholder="筛选当前子目录..."
                aria-label="筛选当前子目录"
                className="w-full rounded-lg border border-slate-800 bg-slate-900 pl-8 pr-3 py-1.5 text-xs text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none"
              />
            </div>

            <label className="inline-flex items-center gap-1.5 text-xs text-slate-300 cursor-pointer select-none">
              <input
                type="checkbox"
                checked={showHidden}
                onChange={(e) => handleToggleHidden(e.target.checked)}
                className="h-3.5 w-3.5 rounded border-slate-600 bg-slate-800 text-indigo-500"
              />
              <span>显示隐藏目录</span>
            </label>
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-5 space-y-3">
          {fallbackNotice && (
            <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-3.5 py-2 text-xs text-amber-200">
              {fallbackNotice}
            </div>
          )}

          {error && (
            <div className="rounded-xl border border-rose-500/30 bg-rose-500/10 px-3.5 py-2.5 text-xs text-rose-200">
              {error}
            </div>
          )}

          {loading ? (
            <div className="flex flex-col items-center justify-center py-12 text-slate-400 gap-2 text-xs">
              <Loader2 className="h-6 w-6 animate-spin text-indigo-400" />
              <span>正在读取目录列表...</span>
            </div>
          ) : filteredEntries.length === 0 ? (
            <div className="rounded-xl border border-dashed border-slate-800 py-10 text-center text-xs text-slate-500">
              {filterText.trim()
                ? '没有匹配的子目录。'
                : '当前目录下没有子目录，可直接点击右下角「选择此目录」。'}
            </div>
          ) : (
            <div className="divide-y divide-slate-800/70 rounded-xl border border-slate-800 bg-slate-950/60">
              {filteredEntries.map((entry) => (
                <button
                  key={entry.path}
                  type="button"
                  onClick={() => void loadDirectory(entry.path, showHidden, false)}
                  className="flex w-full items-center justify-between gap-3 px-3.5 py-2.5 text-left hover:bg-slate-800/60 transition-colors group"
                >
                  <span className="flex items-center gap-2.5 min-w-0">
                    <Folder className="h-4 w-4 shrink-0 text-amber-400" />
                    <span className="text-sm font-medium text-slate-200 group-hover:text-indigo-300 truncate">
                      {entry.name}
                    </span>
                  </span>
                  <ChevronRight className="h-4 w-4 shrink-0 text-slate-600 group-hover:text-slate-300" />
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-800 bg-slate-900/95 px-5 py-3.5">
          <div className="min-w-0 flex-1">
            <p className="text-[11px] text-slate-500">当前选中路径</p>
            <p className="truncate font-mono text-xs text-indigo-300">
              {data?.current_path || '—'}
            </p>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <button
              type="button"
              onClick={onClose}
              className="rounded-xl border border-slate-700 px-3.5 py-2 text-xs font-medium text-slate-300 hover:bg-slate-800 transition-colors"
            >
              取消
            </button>
            <Tooltip content="将当前目录填入本地目录绝对路径">
              <button
                type="button"
                disabled={loading || !data?.current_path}
                onClick={handleConfirm}
                className="inline-flex items-center gap-1.5 rounded-xl bg-indigo-600 px-4 py-2 text-xs font-semibold text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors"
              >
                <Check className="h-3.5 w-3.5" />
                <span>选择此目录</span>
              </button>
            </Tooltip>
          </div>
        </div>
      </div>
    </div>
  );
}
