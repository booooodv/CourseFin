import type { FocusEvent, MouseEvent, ReactElement } from 'react';
import { cloneElement, useCallback, useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

type TooltipPlacement = 'top' | 'bottom';

interface TriggerProps {
  onMouseEnter?: (e: MouseEvent<HTMLElement>) => void;
  onMouseLeave?: (e: MouseEvent<HTMLElement>) => void;
  onFocus?: (e: FocusEvent<HTMLElement>) => void;
  onBlur?: (e: FocusEvent<HTMLElement>) => void;
  'aria-describedby'?: string;
}

export interface TooltipProps {
  content?: string;
  placement?: TooltipPlacement;
  children: ReactElement<TriggerProps>;
}

const HOVER_DELAY_MS = 120;
const EDGE_PADDING_PX = 10;
const GAP_PX = 8;

export default function Tooltip({ content, placement = 'top', children }: TooltipProps) {
  const tooltipId = useId();
  const [open, setOpen] = useState(false);
  const [coords, setCoords] = useState<{ top: number; left: number; resolved: TooltipPlacement }>({
    top: 0,
    left: 0,
    resolved: placement,
  });

  const triggerElRef = useRef<HTMLElement | null>(null);
  const openTimerRef = useRef<number | null>(null);

  const clearOpenTimer = useCallback(() => {
    if (openTimerRef.current !== null) {
      window.clearTimeout(openTimerRef.current);
      openTimerRef.current = null;
    }
  }, []);

  const updatePosition = useCallback(
    (el: HTMLElement | null) => {
      if (!el || typeof window === 'undefined') return;
      const rect = el.getBoundingClientRect();
      let resolved: TooltipPlacement = placement;

      if (placement === 'top' && rect.top < 44) {
        resolved = 'bottom';
      } else if (placement === 'bottom' && window.innerHeight - rect.bottom < 44) {
        resolved = 'top';
      }

      const centerX = rect.left + rect.width / 2;
      const clampedLeft = Math.max(
        EDGE_PADDING_PX,
        Math.min(window.innerWidth - EDGE_PADDING_PX, centerX),
      );
      const top = resolved === 'top' ? rect.top - GAP_PX : rect.bottom + GAP_PX;

      setCoords({ top, left: clampedLeft, resolved });
    },
    [placement],
  );

  useEffect(() => {
    return () => {
      clearOpenTimer();
    };
  }, [clearOpenTimer]);

  useEffect(() => {
    if (!open) return;

    const handleReposition = () => {
      if (!triggerElRef.current || !triggerElRef.current.isConnected) {
        setOpen(false);
        return;
      }
      updatePosition(triggerElRef.current);
    };

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false);
      }
    };

    window.addEventListener('scroll', handleReposition, true);
    window.addEventListener('resize', handleReposition);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('scroll', handleReposition, true);
      window.removeEventListener('resize', handleReposition);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [open, updatePosition]);

  const trimmed = content?.trim();
  if (!trimmed) {
    return children;
  }

  const childProps = children.props;

  const handleMouseEnter = (e: MouseEvent<HTMLElement>) => {
    childProps.onMouseEnter?.(e);
    const el = e.currentTarget;
    triggerElRef.current = el;
    clearOpenTimer();
    openTimerRef.current = window.setTimeout(() => {
      updatePosition(el);
      setOpen(true);
    }, HOVER_DELAY_MS);
  };

  const handleMouseLeave = (e: MouseEvent<HTMLElement>) => {
    childProps.onMouseLeave?.(e);
    clearOpenTimer();
    setOpen(false);
  };

  const handleFocus = (e: FocusEvent<HTMLElement>) => {
    childProps.onFocus?.(e);
    const el = e.currentTarget;
    triggerElRef.current = el;
    clearOpenTimer();
    updatePosition(el);
    setOpen(true);
  };

  const handleBlur = (e: FocusEvent<HTMLElement>) => {
    childProps.onBlur?.(e);
    clearOpenTimer();
    setOpen(false);
  };

  const mergedDescribedBy = open
    ? [childProps['aria-describedby'], tooltipId].filter(Boolean).join(' ')
    : childProps['aria-describedby'];

  const trigger = cloneElement(children, {
    onMouseEnter: handleMouseEnter,
    onMouseLeave: handleMouseLeave,
    onFocus: handleFocus,
    onBlur: handleBlur,
    'aria-describedby': mergedDescribedBy,
  });

  const translateClass =
    coords.resolved === 'top' ? '-translate-x-1/2 -translate-y-full' : '-translate-x-1/2';

  return (
    <>
      {trigger}
      {open &&
        typeof document !== 'undefined' &&
        createPortal(
          <div
            id={tooltipId}
            role="tooltip"
            style={{ top: `${coords.top}px`, left: `${coords.left}px` }}
            className={`pointer-events-none fixed z-[70] max-w-xs whitespace-nowrap rounded-lg border border-slate-700/90 bg-slate-800/95 px-2.5 py-1 text-[11px] font-medium text-slate-100 shadow-lg backdrop-blur-sm ${translateClass}`}
          >
            {trimmed}
          </div>,
          document.body,
        )}
    </>
  );
}
