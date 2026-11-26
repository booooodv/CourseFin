import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';
import { Check, Eye, X } from 'lucide-react';
import type { CoverStyleTemplate } from '../types/coursefin';

export const FALLBACK_COVER_STYLES: CoverStyleTemplate[] = [
  {
    id: 'editorial_modern',
    name: '现代书封',
    tagline: '温润米白 · 瑞士双栏',
    description: '温润米白纸纹底搭配左侧粗黑大标题与右侧现代几何矢量主视觉，整齐耐看。',
    recommended_for: '编程、通识、商业与综合类课程',
    preview_image_url: '/web/cover-styles/editorial_modern.jpg',
    palette: {
      background: '#F7F5F0',
      surface: '#E6EEFF',
      text: '#181B26',
      accent: '#1E5EFF',
      accent_soft: '#F97316',
    },
  },
  {
    id: 'dark_tech',
    name: '暗夜科技',
    tagline: '深邃蓝黑 · 玻璃光效',
    description: '深石墨蓝黑背景搭配细微坐标点阵、磨砂青蓝角标与发光玻璃态科技主视觉。',
    recommended_for: '工程架构、AI 算法、后端与运维课程',
    preview_image_url: '/web/cover-styles/dark_tech.jpg',
    palette: {
      background: '#0B1220',
      surface: '#131F36',
      text: '#F8FAFC',
      accent: '#38BDF8',
      accent_soft: '#1E40AF',
    },
  },
  {
    id: 'clay_3d',
    name: '柔和 3D 黏土',
    tagline: '莫兰迪渐变 · 磨砂立体',
    description: '柔和淡紫蓝渐变背景搭配圆润深海军蓝标题与右侧精巧磨砂 3D 黏土微缩模型。',
    recommended_for: '设计、产品、效率工具与入门通识课',
    preview_image_url: '/web/cover-styles/clay_3d.jpg',
    palette: {
      background: '#EEF2FF',
      surface: '#E0F2FE',
      text: '#1E293B',
      accent: '#4F46E5',
      accent_soft: '#FB923C',
    },
  },
  {
    id: 'warm_illustration',
    name: '温暖扁平插画',
    tagline: '暖杏纸纹 · 编辑部插画',
    description: '暖杏奶油纸感底搭配深炭文字与右侧孔版印刷质感扁平插画，亲和富有故事感。',
    recommended_for: '语言、人文、摄影与生活方式课程',
    preview_image_url: '/web/cover-styles/warm_illustration.jpg',
    palette: {
      background: '#FBF3E4',
      surface: '#FDE8D7',
      text: '#231F20',
      accent: '#EA580C',
      accent_soft: '#2563EB',
    },
  },
  {
    id: 'blueprint_grid',
    name: '极简蓝图',
    tagline: '普鲁士蓝 · 精密线稿',
    description: '深普鲁士蓝工程坐标网格搭配锐利纯白文字与白色等轴测技术线稿，极具工科秩序感。',
    recommended_for: '计算机原理、数学、硬件与底层源码课',
    preview_image_url: '/web/cover-styles/blueprint_grid.jpg',
    palette: {
      background: '#0F3566',
      surface: '#174680',
      text: '#FFFFFF',
      accent: '#BAE6FD',
      accent_soft: '#38BDF8',
    },
  },
  {
    id: 'cinematic_studio',
    name: '质感影院',
    tagline: '暗调影棚 · 香槟金边',
    description: '低键黑曜石影棚光影与暗角搭配香槟金细线、暖白大标题与微景深主题静物摄影。',
    recommended_for: '影视制作、音乐艺术、大师课系列',
    preview_image_url: '/web/cover-styles/cinematic_studio.jpg',
    palette: {
      background: '#121318',
      surface: '#1F2129',
      text: '#F8F6F0',
      accent: '#D4AF6A',
      accent_soft: '#A8A29E',
    },
  },
];

export interface CoverStylePickerProps {
  styles?: CoverStyleTemplate[];
  value: string;
  onChange: (styleId: string) => void;
  defaultStyleId?: string;
  disabled?: boolean;
  ariaLabel?: string;
}

interface PreviewTarget {
  style: CoverStyleTemplate;
  anchorEl: HTMLElement;
  pinned: boolean;
}

const POPOVER_EDGE_PX = 12;
const POPOVER_GAP_PX = 10;
const HOVER_OPEN_DELAY_MS = 60;

export default function CoverStylePicker({
  styles,
  value,
  onChange,
  defaultStyleId,
  disabled = false,
  ariaLabel = '选择封面风格模板',
}: CoverStylePickerProps) {
  const effectiveStyles = styles && styles.length > 0 ? styles : FALLBACK_COVER_STYLES;

  const [preview, setPreview] = useState<PreviewTarget | null>(null);
  const [coords, setCoords] = useState<{ top: number; left: number; placeAbove: boolean }>({
    top: 12,
    left: 12,
    placeAbove: true,
  });

  const popoverRef = useRef<HTMLDivElement | null>(null);
  const hoverTimerRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  const clearTimers = useCallback(() => {
    if (hoverTimerRef.current !== null) {
      window.clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  }, []);

  useEffect(() => clearTimers, [clearTimers]);

  const updatePopoverPosition = useCallback((target: PreviewTarget | null) => {
    if (!target || typeof window === 'undefined') return;
    const anchorRect = target.anchorEl.getBoundingClientRect();
    const popRect = popoverRef.current?.getBoundingClientRect();
    const popWidth = popRect && popRect.width > 0 ? popRect.width : 300;
    const popHeight = popRect && popRect.height > 0 ? popRect.height : 285;

    const spaceAbove = anchorRect.top - POPOVER_EDGE_PX;
    const spaceBelow = window.innerHeight - anchorRect.bottom - POPOVER_EDGE_PX;
    const placeAbove = spaceAbove >= popHeight + POPOVER_GAP_PX || spaceAbove >= spaceBelow;

    let top = placeAbove
      ? anchorRect.top - POPOVER_GAP_PX - popHeight
      : anchorRect.bottom + POPOVER_GAP_PX;
    top = Math.max(
      POPOVER_EDGE_PX,
      Math.min(Math.max(POPOVER_EDGE_PX, window.innerHeight - POPOVER_EDGE_PX - popHeight), top),
    );

    const centerX = anchorRect.left + anchorRect.width / 2;
    const minLeft = POPOVER_EDGE_PX;
    const maxLeft = Math.max(POPOVER_EDGE_PX, window.innerWidth - POPOVER_EDGE_PX - popWidth);
    const left = Math.max(minLeft, Math.min(maxLeft, centerX - popWidth / 2));

    setCoords((prev) =>
      prev.top === top && prev.left === left && prev.placeAbove === placeAbove
        ? prev
        : { top, left, placeAbove },
    );
  }, []);

  useLayoutEffect(() => {
    if (!preview) return;
    updatePopoverPosition(preview);
  }, [preview, updatePopoverPosition]);

  useEffect(() => {
    if (!preview) return;

    const handleReposition = () => {
      if (!preview.anchorEl.isConnected) {
        setPreview(null);
        return;
      }
      updatePopoverPosition(preview);
    };

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setPreview(null);
      }
    };

    const handlePointerDown = (e: PointerEvent) => {
      if (!preview.pinned) return;
      const targetNode = e.target as Node | null;
      if (
        targetNode &&
        (popoverRef.current?.contains(targetNode) || preview.anchorEl.contains(targetNode))
      ) {
        return;
      }
      setPreview(null);
    };

    window.addEventListener('scroll', handleReposition, true);
    window.addEventListener('resize', handleReposition);
    window.addEventListener('keydown', handleKeyDown);
    document.addEventListener('pointerdown', handlePointerDown);

    return () => {
      window.removeEventListener('scroll', handleReposition, true);
      window.removeEventListener('resize', handleReposition);
      window.removeEventListener('keydown', handleKeyDown);
      document.removeEventListener('pointerdown', handlePointerDown);
    };
  }, [preview, updatePopoverPosition]);

  const openHoverPreview = (style: CoverStyleTemplate, anchorEl: HTMLElement) => {
    clearTimers();
    setPreview((cur) => {
      if (cur?.pinned) return cur;
      if (cur) {
        return { style, anchorEl, pinned: false };
      }
      hoverTimerRef.current = window.setTimeout(() => {
        setPreview((latest) =>
          latest?.pinned ? latest : { style, anchorEl, pinned: false },
        );
      }, HOVER_OPEN_DELAY_MS);
      return cur;
    });
  };

  const closeHoverPreview = (styleId: string) => {
    clearTimers();
    closeTimerRef.current = window.setTimeout(() => {
      setPreview((cur) => {
        if (!cur || cur.pinned || cur.style.id !== styleId) return cur;
        return null;
      });
    }, 50);
  };

  const togglePinnedPreview = (style: CoverStyleTemplate, anchorEl: HTMLElement) => {
    clearTimers();
    setPreview((cur) => {
      if (cur && cur.pinned && cur.style.id === style.id) {
        return null;
      }
      return { style, anchorEl, pinned: true };
    });
  };

  return (
    <div className="space-y-2">
      <div
        role="radiogroup"
        aria-label={ariaLabel}
        className="grid grid-cols-2 sm:grid-cols-3 gap-2.5"
      >
        {effectiveStyles.map((style) => {
          const isSelected = value === style.id;
          const isDefault = defaultStyleId === style.id;
          const isPreviewing = preview?.style.id === style.id;

          return (
            <div
              key={style.id}
              data-style-card={style.id}
              onMouseEnter={(e) => openHoverPreview(style, e.currentTarget)}
              onMouseLeave={() => closeHoverPreview(style.id)}
              className={`relative rounded-2xl border transition-all ${
                isSelected
                  ? 'border-accent bg-accent-soft/70 ring-2 ring-accent-ring shadow-card'
                  : 'border-subtle bg-card hover:border-default hover:bg-subtle/50'
              } ${disabled ? 'opacity-60' : ''}`}
            >
              <button
                type="button"
                role="radio"
                aria-checked={isSelected}
                disabled={disabled}
                onClick={() => onChange(style.id)}
                onFocus={(e) => {
                  const card = e.currentTarget.closest('[data-style-card]') as HTMLElement | null;
                  if (card) {
                    clearTimers();
                    setPreview((cur) => (cur?.pinned ? cur : { style, anchorEl: card, pinned: false }));
                  }
                }}
                onBlur={() => closeHoverPreview(style.id)}
                onTouchStart={(e) => {
                  if (disabled) return;
                  const card = e.currentTarget.closest('[data-style-card]') as HTMLElement | null;
                  if (card) {
                    clearTimers();
                    setPreview({ style, anchorEl: card, pinned: true });
                  }
                }}
                className="w-full p-2.5 pr-8 text-left cursor-pointer disabled:cursor-not-allowed focus:outline-none rounded-2xl"
              >
                {/* Compact Mini Swatch & Composition Preview */}
                <div
                  className="mb-2 flex h-10 w-full items-center justify-between overflow-hidden rounded-xl border border-black/10 px-2.5"
                  style={{ backgroundColor: style.palette.background }}
                >
                  <div className="flex flex-col gap-1 min-w-0">
                    <span
                      className="inline-block h-1.5 w-8 rounded-full"
                      style={{ backgroundColor: style.palette.text, opacity: 0.9 }}
                    />
                    <span
                      className="inline-block h-1 w-5 rounded-full"
                      style={{ backgroundColor: style.palette.accent }}
                    />
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    <span
                      className="h-4 w-4 rounded-md"
                      style={{ backgroundColor: style.palette.accent }}
                    />
                    <span
                      className="h-3 w-3 rounded-full"
                      style={{ backgroundColor: style.palette.accent_soft }}
                    />
                  </div>
                </div>

                <div className="flex items-center gap-1.5 min-w-0">
                  <span className="text-xs font-bold text-ink-primary truncate">{style.name}</span>
                  {isDefault && (
                    <span className="shrink-0 rounded-full bg-accent-soft px-1.5 py-0.5 text-[10px] font-semibold text-accent-ink">
                      默认
                    </span>
                  )}
                  {isSelected && (
                    <Check className="h-3.5 w-3.5 shrink-0 text-accent-ink ml-auto" aria-hidden="true" />
                  )}
                </div>
                <p className="mt-0.5 text-[11px] text-ink-muted truncate">{style.tagline}</p>
              </button>

              {/* Touch / Explicit Preview Toggle Button */}
              <button
                type="button"
                aria-label={`预览 ${style.name} 样图`}
                aria-expanded={isPreviewing && Boolean(preview?.pinned)}
                disabled={disabled}
                onClick={(e) => {
                  e.stopPropagation();
                  const card = e.currentTarget.closest('[data-style-card]') as HTMLElement | null;
                  if (card) {
                    togglePinnedPreview(style, card);
                  }
                }}
                className={`absolute top-2 right-2 flex h-6 w-6 items-center justify-center rounded-lg border transition-colors cursor-pointer disabled:opacity-50 ${
                  isPreviewing && preview?.pinned
                    ? 'border-accent bg-accent text-ink-inverse'
                    : 'border-subtle bg-card/90 text-ink-muted hover:border-default hover:text-ink-primary'
                }`}
              >
                <Eye className="h-3.5 w-3.5" />
              </button>
            </div>
          );
        })}
      </div>

      <p className="text-[11px] text-ink-muted">
        悬停、聚焦或轻触风格选项上的预览图标，可查看同款示例封面效果。
      </p>

      {preview &&
        typeof document !== 'undefined' &&
        createPortal(
          <div
            ref={popoverRef}
            role={preview.pinned ? 'dialog' : 'tooltip'}
            aria-label={`${preview.style.name} 样图预览`}
            style={{ top: `${coords.top}px`, left: `${coords.left}px` }}
            className={`fixed z-[80] w-72 sm:w-80 overflow-hidden rounded-2xl border border-default bg-popover p-2.5 shadow-float transition-opacity ${
              preview.pinned ? 'pointer-events-auto' : 'pointer-events-none'
            }`}
          >
            <div className="relative aspect-[3/2] w-full overflow-hidden rounded-xl border border-subtle bg-subtle">
              <img
                src={preview.style.preview_image_url}
                alt={`${preview.style.name} 参考封面`}
                className="h-full w-full object-cover"
              />
              {preview.pinned && (
                <button
                  type="button"
                  aria-label="关闭样图预览"
                  onClick={() => setPreview(null)}
                  className="absolute top-2 right-2 flex h-6 w-6 items-center justify-center rounded-full bg-tooltip/85 text-ink-tooltip hover:bg-tooltip transition-colors cursor-pointer"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
            <div className="mt-2.5 space-y-1 px-0.5">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-bold text-ink-primary">{preview.style.name}</span>
                <span className="text-[11px] font-medium text-accent-ink">
                  {preview.style.tagline}
                </span>
              </div>
              <p className="text-[11px] leading-relaxed text-ink-secondary">
                {preview.style.description}
              </p>
              <p className="text-[10px] text-ink-muted">
                适合：{preview.style.recommended_for}
              </p>
            </div>
          </div>,
          document.body,
        )}
    </div>
  );
}
