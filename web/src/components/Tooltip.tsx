import type { FocusEvent, MouseEvent, ReactElement } from 'react';
import {
  cloneElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';

export type TooltipPlacement = 'top' | 'bottom' | 'right';

interface TriggerProps {
  disabled?: boolean;
  onMouseEnter?: (e: MouseEvent<HTMLElement>) => void;
  onMouseLeave?: (e: MouseEvent<HTMLElement>) => void;
  onFocus?: (e: FocusEvent<HTMLElement>) => void;
  onBlur?: (e: FocusEvent<HTMLElement>) => void;
  onClick?: (e: MouseEvent<HTMLElement>) => void;
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
  const tooltipElRef = useRef<HTMLDivElement | null>(null);
  const openTimerRef = useRef<number | null>(null);

  const isDisabled = Boolean(children.props.disabled);

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
      const tipRect = tooltipElRef.current?.getBoundingClientRect();
      const tipHeight = tipRect ? tipRect.height : 32;
      const tipWidth = tipRect ? tipRect.width : 0;
      const halfWidth = tipWidth / 2;
      const halfHeight = tipHeight / 2;

      if (placement === 'right') {
        const rightStart = rect.right + GAP_PX;
        let left = rightStart;
        if (tipWidth > 0 && rightStart + tipWidth + EDGE_PADDING_PX > window.innerWidth) {
          const leftStart = rect.left - GAP_PX - tipWidth;
          if (leftStart >= EDGE_PADDING_PX) {
            left = leftStart;
          } else {
            const maxLeft = Math.max(EDGE_PADDING_PX, window.innerWidth - EDGE_PADDING_PX - tipWidth);
            left = Math.max(EDGE_PADDING_PX, Math.min(maxLeft, rightStart));
          }
        }

        const centerY = rect.top + rect.height / 2;
        const minTop = EDGE_PADDING_PX + halfHeight;
        const maxTop = Math.max(minTop, window.innerHeight - EDGE_PADDING_PX - halfHeight);
        const top = Math.max(minTop, Math.min(maxTop, centerY));

        setCoords((prev) =>
          prev.top === top && prev.left === left && prev.resolved === 'right'
            ? prev
            : { top, left, resolved: 'right' },
        );
        return;
      }

      let resolved: TooltipPlacement = placement;
      if (placement === 'top' && rect.top < tipHeight + GAP_PX + EDGE_PADDING_PX) {
        resolved = 'bottom';
      } else if (
        placement === 'bottom' &&
        window.innerHeight - rect.bottom < tipHeight + GAP_PX + EDGE_PADDING_PX
      ) {
        resolved = 'top';
      }

      const centerX = rect.left + rect.width / 2;
      const minLeft = EDGE_PADDING_PX + halfWidth;
      const maxLeft = Math.max(minLeft, window.innerWidth - EDGE_PADDING_PX - halfWidth);
      const clampedLeft = Math.max(minLeft, Math.min(maxLeft, centerX));
      const top = resolved === 'top' ? rect.top - GAP_PX : rect.bottom + GAP_PX;

      setCoords((prev) =>
        prev.top === top && prev.left === clampedLeft && prev.resolved === resolved
          ? prev
          : { top, left: clampedLeft, resolved },
      );
    },
    [placement],
  );

  useEffect(() => {
    return () => {
      clearOpenTimer();
    };
  }, [clearOpenTimer]);

  // Immediately close tooltip if the trigger becomes disabled while hovered/focused
  useEffect(() => {
    if (isDisabled) {
      clearOpenTimer();
      setOpen(false);
    }
  }, [isDisabled, clearOpenTimer]);

  const trimmed = content?.trim();

  // Re-measure with actual portaled tooltip dimensions to prevent horizontal edge clipping
  useLayoutEffect(() => {
    if (!open || !trimmed) return;
    updatePosition(triggerElRef.current);
  }, [open, trimmed, updatePosition]);

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

  if (!trimmed) {
    return children;
  }

  const childProps = children.props;

  const handleMouseEnter = (e: MouseEvent<HTMLElement>) => {
    childProps.onMouseEnter?.(e);
    if (isDisabled) return;
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
    if (isDisabled) return;
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

  const handleClick = (e: MouseEvent<HTMLElement>) => {
    clearOpenTimer();
    setOpen(false);
    childProps.onClick?.(e);
  };

  const mergedDescribedBy = open
    ? [childProps['aria-describedby'], tooltipId].filter(Boolean).join(' ')
    : childProps['aria-describedby'];

  const trigger = cloneElement(children, {
    onMouseEnter: handleMouseEnter,
    onMouseLeave: handleMouseLeave,
    onFocus: handleFocus,
    onBlur: handleBlur,
    onClick: handleClick,
    'aria-describedby': mergedDescribedBy,
  });

  const translateClass =
    coords.resolved === 'right'
      ? '-translate-y-1/2'
      : coords.resolved === 'top'
        ? '-translate-x-1/2 -translate-y-full'
        : '-translate-x-1/2';

  return (
    <>
      {trigger}
      {open &&
        typeof document !== 'undefined' &&
        createPortal(
          <div
            ref={tooltipElRef}
            id={tooltipId}
            role="tooltip"
            style={{ top: `${coords.top}px`, left: `${coords.left}px` }}
            className={`pointer-events-none fixed z-[70] w-max max-w-xs whitespace-normal break-words text-center rounded-lg border border-white/10 bg-tooltip px-2.5 py-1 text-[11px] font-medium leading-snug text-ink-tooltip shadow-float backdrop-blur-sm ${translateClass}`}
          >
            {trimmed}
          </div>,
          document.body,
        )}
    </>
  );
}
