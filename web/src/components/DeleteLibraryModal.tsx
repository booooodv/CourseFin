import { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, ArrowRightLeft, Loader2, Trash2, X } from 'lucide-react';
import { api } from '../api/client';
import Tooltip from './Tooltip';
import type { Library } from '../types/coursefin';

export type DeleteLibraryMode = 'empty' | 'move' | 'force';

interface DeleteLibraryModalProps {
  library: Library | null;
  libraries: Library[];
  onClose: () => void;
  onDeleted: (deleted: Library, mode: DeleteLibraryMode, targetLibraryName?: string) => void;
}

export default function DeleteLibraryModal({
  library,
  libraries,
  onClose,
  onDeleted,
}: DeleteLibraryModalProps) {
  const otherLibraries = useMemo(
    () => (library ? libraries.filter((l) => l.id !== library.id) : []),
    [library, libraries],
  );

  const [mode, setMode] = useState<'move' | 'force'>('move');
  const [targetLibraryId, setTargetLibraryId] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!library) return;
    setError(null);
    setSubmitting(false);
    if (otherLibraries.length > 0) {
      setMode('move');
      setTargetLibraryId(otherLibraries[0].id);
    } else {
      setMode('force');
      setTargetLibraryId('');
    }
  }, [library, otherLibraries]);

  useEffect(() => {
    if (!library) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !submitting) {
        onClose();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [library, submitting, onClose]);

  if (!library) return null;

  const isEmpty = library.course_count <= 0;
  const canMove = otherLibraries.length > 0;

  async function handleConfirm() {
    if (!library || submitting) return;
    if (!isEmpty && mode === 'move' && !targetLibraryId) {
      setError('请选择要迁入的目标媒体库');
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      if (isEmpty) {
        await api.deleteLibrary(library.id);
        onDeleted(library, 'empty');
      } else if (mode === 'move') {
        const target = otherLibraries.find((l) => l.id === targetLibraryId);
        await api.deleteLibrary(library.id, { moveToLibraryId: targetLibraryId });
        onDeleted(library, 'move', target?.name);
      } else {
        await api.deleteLibrary(library.id, { force: true });
        onDeleted(library, 'force');
      }
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除媒体库失败');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="delete-library-modal-title"
    >
      <div className="w-full max-w-lg overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-subtle px-5 py-4">
          <div className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-status-danger-soft text-status-danger-ink">
              <Trash2 className="h-4 w-4" />
            </span>
            <h2 id="delete-library-modal-title" className="text-base font-bold text-ink-primary">
              删除媒体库
            </h2>
          </div>

          <Tooltip content="关闭弹窗">
            <button
              type="button"
              aria-label="关闭"
              disabled={submitting}
              onClick={onClose}
              className="rounded-lg p-1.5 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors disabled:opacity-50"
            >
              <X className="h-5 w-5" />
            </button>
          </Tooltip>
        </div>

        {/* Body */}
        <div className="p-5 space-y-4">
          {isEmpty ? (
            <div className="space-y-2 text-sm text-ink-secondary">
              <p>
                确定要删除空媒体库 <strong className="text-ink-primary">「{library.name}」</strong> 吗？
              </p>
              <p className="text-xs text-ink-muted">
                删除媒体库仅移除分组配置，不会影响本地硬盘上的任何视频文件。
              </p>
            </div>
          ) : (
            <div className="space-y-3.5">
              <p className="text-sm text-ink-secondary">
                媒体库 <strong className="text-ink-primary">「{library.name}」</strong> 下仍有{' '}
                <strong className="text-accent-ink">{library.course_count}</strong> 门课程，请选择删除方式：
              </p>

              {/* Option 1: Migrate courses */}
              <div
                className={`rounded-2xl border p-3.5 space-y-2.5 transition-colors ${
                  !canMove
                    ? 'border-subtle bg-subtle/50 opacity-60'
                    : mode === 'move'
                      ? 'border-accent/50 bg-accent-soft'
                      : 'border-subtle bg-subtle hover:border-default'
                }`}
              >
                <label className="flex items-start gap-2.5 cursor-pointer">
                  <input
                    type="radio"
                    name="delete-library-mode"
                    value="move"
                    disabled={!canMove || submitting}
                    checked={mode === 'move'}
                    onChange={() => setMode('move')}
                    className="mt-0.5 h-4 w-4 border-default bg-card text-accent focus:ring-accent-ring"
                  />
                  <div className="space-y-0.5">
                    <span className="inline-flex items-center gap-1.5 text-sm font-semibold text-ink-primary">
                      <ArrowRightLeft className="h-3.5 w-3.5 text-accent-ink" />
                      迁移课程到其他媒体库并删除
                    </span>
                    <p className="text-xs text-ink-muted">
                      {canMove
                        ? '保留全部课程章节、封面、标签与观看进度，迁移完成后删除当前媒体库。'
                        : '暂无其他媒体库可迁入，可先新建一个媒体库，或选择下方连带删除。'}
                    </p>
                  </div>
                </label>

                {canMove && mode === 'move' && (
                  <div className="pl-6">
                    <label
                      htmlFor="move-target-library"
                      className="block text-[11px] font-medium text-ink-secondary mb-1"
                    >
                      迁移目标媒体库
                    </label>
                    <select
                      id="move-target-library"
                      aria-label="迁移目标媒体库"
                      value={targetLibraryId}
                      disabled={submitting}
                      onChange={(e) => setTargetLibraryId(e.target.value)}
                      className="w-full rounded-xl border border-default bg-card px-3 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring"
                    >
                      {otherLibraries.map((lib) => (
                        <option key={lib.id} value={lib.id}>
                          {lib.name} ({lib.course_count} 门课程)
                        </option>
                      ))}
                    </select>
                  </div>
                )}
              </div>

              {/* Option 2: Force delete courses metadata */}
              <div
                className={`rounded-2xl border p-3.5 transition-colors ${
                  mode === 'force'
                    ? 'border-status-danger/50 bg-status-danger-soft'
                    : 'border-subtle bg-subtle hover:border-default'
                }`}
              >
                <label className="flex items-start gap-2.5 cursor-pointer">
                  <input
                    type="radio"
                    name="delete-library-mode"
                    value="force"
                    disabled={submitting}
                    checked={mode === 'force'}
                    onChange={() => setMode('force')}
                    className="mt-0.5 h-4 w-4 border-default bg-card text-status-danger focus:ring-status-danger/40"
                  />
                  <div className="space-y-1">
                    <span className="inline-flex items-center gap-1.5 text-sm font-semibold text-status-danger-ink">
                      <AlertTriangle className="h-3.5 w-3.5 text-status-danger-ink" />
                      连带删除这 {library.course_count} 门课程记录
                    </span>
                    <p className="text-xs text-ink-muted leading-relaxed">
                      仅删除 CourseFin 中的课程目录、章节与观看进度记录，<strong className="text-ink-primary">绝不删除本地硬盘上的视频文件</strong>。
                    </p>
                  </div>
                </label>
              </div>
            </div>
          )}

          {error && (
            <div className="rounded-xl border border-status-danger/30 bg-status-danger-soft px-3.5 py-2 text-xs text-status-danger-ink">
              {error}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2.5 border-t border-subtle bg-subtle/40 px-5 py-3.5">
          <button
            type="button"
            disabled={submitting}
            onClick={onClose}
            className="rounded-xl border border-default bg-card px-4 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 transition-colors"
          >
            取消
          </button>

          <button
            type="button"
            disabled={submitting || (!isEmpty && mode === 'move' && !canMove)}
            onClick={() => void handleConfirm()}
            className={`inline-flex items-center gap-1.5 rounded-xl px-4 py-2 text-xs font-semibold text-ink-inverse disabled:opacity-50 transition-colors ${
              !isEmpty && mode === 'move'
                ? 'bg-accent hover:bg-accent-hover shadow-accent'
                : 'bg-status-danger hover:opacity-90'
            }`}
          >
            {submitting ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Trash2 className="h-3.5 w-3.5" />
            )}
            <span>
              {submitting
                ? '处理中...'
                : !isEmpty && mode === 'move'
                  ? '迁移并删除'
                  : '确认删除'}
            </span>
          </button>
        </div>
      </div>
    </div>
  );
}
