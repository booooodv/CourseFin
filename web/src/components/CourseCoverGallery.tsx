import { useState } from 'react';
import { Check, Loader2, Trash2 } from 'lucide-react';
import { courseCoverImageUrl } from '../api/client';
import type { CourseCover } from '../types/coursefin';

export interface CourseCoverGalleryProps {
  covers: CourseCover[];
  busyCoverId?: string | null;
  disabled?: boolean;
  compact?: boolean;
  onActivate: (cover: CourseCover) => void | Promise<void>;
  onDelete: (cover: CourseCover) => void | Promise<void>;
}

export function formatCoverSourceLabel(cover: CourseCover): string {
  switch (cover.source) {
    case 'ai':
      return cover.style_name ? `AI · ${cover.style_name}` : 'AI 生成';
    case 'ai_batch':
      return cover.style_name ? `批量 AI · ${cover.style_name}` : 'AI 批量生成';
    case 'upload':
      return '本地上传';
    case 'frame':
      return '课时帧';
    case 'import':
      return '目录导入';
    case 'legacy':
      return '原有封面';
    default:
      return cover.style_name || '关联封面';
  }
}

export default function CourseCoverGallery({
  covers,
  busyCoverId = null,
  disabled = false,
  compact = false,
  onActivate,
  onDelete,
}: CourseCoverGalleryProps) {
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  if (covers.length === 0) {
    return null;
  }

  return (
    <div
      className={
        compact
          ? 'grid grid-cols-2 gap-2.5'
          : 'grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-2.5'
      }
    >
      {covers.map((cover) => {
        const label = formatCoverSourceLabel(cover);
        const isBusy = busyCoverId === cover.id;
        const isConfirming = confirmDeleteId === cover.id;
        const disableActions = disabled || Boolean(busyCoverId);

        return (
          <div
            key={cover.id}
            data-cover-id={cover.id}
            className={`group relative overflow-hidden rounded-2xl border bg-card transition-all ${
              cover.is_active
                ? 'border-accent ring-2 ring-accent-ring shadow-card'
                : 'border-subtle hover:border-default'
            }`}
          >
            <button
              type="button"
              aria-pressed={cover.is_active}
              aria-label={cover.is_active ? `当前封面 ${label}` : `设为当前封面 ${label}`}
              disabled={disableActions || cover.is_active}
              onClick={() => {
                setConfirmDeleteId(null);
                void onActivate(cover);
              }}
              className={`block w-full text-left focus:outline-none ${
                cover.is_active ? 'cursor-default' : 'cursor-pointer'
              }`}
            >
              <div className="relative aspect-[16/10] w-full overflow-hidden bg-subtle">
                <img
                  src={courseCoverImageUrl(cover.image_url, 480)}
                  alt={`${label} 封面`}
                  className="h-full w-full object-cover"
                />

                {cover.is_active && (
                  <span className="absolute top-1.5 left-1.5 inline-flex items-center gap-0.5 rounded-full bg-accent px-2 py-0.5 text-[10px] font-semibold text-ink-inverse shadow-sm">
                    <Check className="h-2.5 w-2.5" aria-hidden="true" />
                    <span>当前</span>
                  </span>
                )}

                {isBusy && (
                  <div className="absolute inset-0 flex items-center justify-center bg-overlay/50">
                    <Loader2 className="h-5 w-5 animate-spin text-ink-inverse" />
                  </div>
                )}
              </div>

              <div className="flex items-center justify-between gap-1.5 px-2.5 py-1.5">
                <span className="truncate text-[11px] font-medium text-ink-primary">
                  {label}
                </span>
                {cover.width > 0 && cover.height > 0 && (
                  <span className="shrink-0 text-[10px] text-ink-muted tabular-nums">
                    {cover.width}×{cover.height}
                  </span>
                )}
              </div>
            </button>

            {isConfirming ? (
              <div className="absolute inset-x-1.5 top-1.5 z-10 flex items-center justify-end gap-1 rounded-xl border border-status-danger/35 bg-popover/95 px-2 py-1 shadow-float">
                <button
                  type="button"
                  disabled={disableActions}
                  onClick={(e) => {
                    e.stopPropagation();
                    setConfirmDeleteId(null);
                    void onDelete(cover);
                  }}
                  className="rounded-lg bg-status-danger px-2 py-0.5 text-[10px] font-semibold text-ink-inverse hover:opacity-90 disabled:opacity-50 cursor-pointer"
                >
                  确认删除
                </button>
                <button
                  type="button"
                  disabled={disableActions}
                  onClick={(e) => {
                    e.stopPropagation();
                    setConfirmDeleteId(null);
                  }}
                  className="rounded-lg border border-default bg-card px-2 py-0.5 text-[10px] font-medium text-ink-secondary hover:bg-subtle cursor-pointer"
                >
                  取消
                </button>
              </div>
            ) : (
              <button
                type="button"
                aria-label={`删除封面 ${label}`}
                disabled={disableActions}
                onClick={(e) => {
                  e.stopPropagation();
                  setConfirmDeleteId(cover.id);
                }}
                className="absolute top-1.5 right-1.5 flex h-6 w-6 items-center justify-center rounded-lg border border-subtle bg-popover/90 text-ink-muted hover:border-status-danger/40 hover:bg-status-danger-soft hover:text-status-danger-ink disabled:opacity-40 transition-colors cursor-pointer"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}
