import type { FormEvent, JSX } from 'react';
import { useEffect, useRef, useState } from 'react';
import { FolderPlus, Loader2, Pencil, X } from 'lucide-react';
import Tooltip from './Tooltip';

export interface LibraryFormModalProps {
  isOpen: boolean;
  mode: 'create' | 'rename';
  initialName?: string;
  submitting?: boolean;
  error?: string | null;
  onClose: () => void;
  onSubmit: (trimmedName: string) => Promise<void> | void;
}

/**
 * Modal dialog for creating or renaming a media library with validation and Escape handling.
 */
export default function LibraryFormModal({
  isOpen,
  mode,
  initialName = '',
  submitting = false,
  error = null,
  onClose,
  onSubmit,
}: LibraryFormModalProps): JSX.Element | null {
  const [name, setName] = useState(mode === 'rename' ? initialName : '');
  const [localError, setLocalError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (!isOpen) return;
    setName(mode === 'rename' ? initialName : '');
    setLocalError(null);
    inputRef.current?.focus();
  }, [isOpen, mode, initialName]);

  useEffect(() => {
    if (!isOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !submitting) {
        onClose();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [isOpen, submitting, onClose]);

  if (!isOpen) return null;

  const isRename = mode === 'rename';
  const title = isRename ? '重命名媒体库' : '新建媒体库';
  const fieldLabel = isRename ? '重命名媒体库名称' : '新媒体库名称';
  const placeholder = isRename
    ? '输入新的媒体库名称'
    : '输入新媒体库名称，例如：前端工程化';
  const submitLabel = isRename
    ? submitting
      ? '保存中...'
      : '保存名称'
    : submitting
      ? '创建中...'
      : '确认创建';
  const displayError = localError || error;

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (submitting) return;

    const trimmed = name.trim();
    if (!trimmed) {
      setLocalError('请输入媒体库名称');
      inputRef.current?.focus();
      return;
    }

    setLocalError(null);
    await onSubmit(trimmed);
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="library-form-modal-title"
    >
      <div className="w-full max-w-md overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-subtle px-5 py-4">
          <div className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
              {isRename ? <Pencil className="h-4 w-4" /> : <FolderPlus className="h-4 w-4" />}
            </span>
            <h2
              id="library-form-modal-title"
              className="text-base font-bold text-ink-primary"
            >
              {title}
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

        {/* Form Body */}
        <form onSubmit={(e) => void handleSubmit(e)}>
          <div className="space-y-3.5 p-5">
            <div>
              <label
                htmlFor="library-form-name-input"
                className="mb-1.5 block text-xs font-medium text-ink-secondary"
              >
                {fieldLabel}
              </label>
              <input
                ref={inputRef}
                id="library-form-name-input"
                type="text"
                value={name}
                disabled={submitting}
                onChange={(e) => {
                  setName(e.target.value);
                  if (localError) setLocalError(null);
                }}
                placeholder={placeholder}
                className="w-full rounded-xl border border-default bg-card px-3.5 py-2.5 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring disabled:opacity-60 transition-colors"
              />
            </div>

            {displayError && (
              <div className="rounded-xl border border-status-danger/30 bg-status-danger-soft px-3.5 py-2 text-xs text-status-danger-ink">
                {displayError}
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
              type="submit"
              disabled={submitting}
              className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-4 py-2 text-xs font-semibold text-ink-inverse hover:bg-accent-hover disabled:opacity-50 transition-colors shadow-accent"
            >
              {submitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>{submitLabel}</span>
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
