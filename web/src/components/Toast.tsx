import type { ReactNode } from 'react';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { AlertCircle, CheckCircle2, Info, X } from 'lucide-react';

export type ToastVariant = 'success' | 'error' | 'info';

interface ToastItem {
  id: number;
  variant: ToastVariant;
  message: string;
}

export interface ToastAPI {
  success: (message: string) => void;
  error: (message: string) => void;
  info: (message: string) => void;
}

interface ToastContextValue {
  toast: ToastAPI;
}

const MAX_TOASTS = 4;
const AUTO_DISMISS_MS = 2800;

const noopToast: ToastAPI = {
  success: () => {},
  error: () => {},
  info: () => {},
};

const ToastContext = createContext<ToastContextValue>({ toast: noopToast });

export function useToast(): ToastContextValue {
  return useContext(ToastContext);
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const nextIdRef = useRef(1);
  const timersRef = useRef<Record<number, number>>({});

  const dismiss = useCallback((id: number) => {
    const timer = timersRef.current[id];
    if (timer !== undefined) {
      window.clearTimeout(timer);
      delete timersRef.current[id];
    }
    setItems((prev) => prev.filter((item) => item.id !== id));
  }, []);

  const push = useCallback(
    (variant: ToastVariant, message: string) => {
      const trimmed = message.trim();
      if (!trimmed) return;

      const id = nextIdRef.current++;
      setItems((prev) => {
        const next = [...prev, { id, variant, message: trimmed }];
        if (next.length > MAX_TOASTS) {
          const dropped = next.slice(0, next.length - MAX_TOASTS);
          for (const d of dropped) {
            const t = timersRef.current[d.id];
            if (t !== undefined) {
              window.clearTimeout(t);
              delete timersRef.current[d.id];
            }
          }
          return next.slice(-MAX_TOASTS);
        }
        return next;
      });

      timersRef.current[id] = window.setTimeout(() => {
        dismiss(id);
      }, AUTO_DISMISS_MS);
    },
    [dismiss],
  );

  useEffect(() => {
    const timers = timersRef.current;
    return () => {
      for (const key of Object.keys(timers)) {
        window.clearTimeout(timers[Number(key)]);
        delete timers[Number(key)];
      }
    };
  }, []);

  const value = useMemo<ToastContextValue>(
    () => ({
      toast: {
        success: (msg) => push('success', msg),
        error: (msg) => push('error', msg),
        info: (msg) => push('info', msg),
      },
    }),
    [push],
  );

  return (
    <ToastContext.Provider value={value}>
      {children}
      {items.length > 0 && (
        <div
          aria-live="polite"
          className="pointer-events-none fixed top-4 right-4 z-[60] flex w-full max-w-sm flex-col gap-2 px-4 sm:px-0"
        >
          {items.map((item) => {
            const Icon =
              item.variant === 'success'
                ? CheckCircle2
                : item.variant === 'error'
                  ? AlertCircle
                  : Info;

            const iconPillClass =
              item.variant === 'success'
                ? 'bg-status-success-soft text-status-success-ink'
                : item.variant === 'error'
                  ? 'bg-status-danger-soft text-status-danger-ink'
                  : 'bg-accent-soft text-accent-ink';

            return (
              <div
                key={item.id}
                role="status"
                className="pointer-events-auto flex items-center gap-2.5 rounded-2xl border border-subtle bg-popover px-3.5 py-2.5 text-xs sm:text-sm text-ink-primary shadow-float backdrop-blur-md transition-all"
              >
                <span
                  className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-lg ${iconPillClass}`}
                >
                  <Icon className="h-3.5 w-3.5" />
                </span>
                <span className="flex-1 leading-snug">{item.message}</span>
                <button
                  type="button"
                  aria-label="关闭提示"
                  onClick={() => dismiss(item.id)}
                  className="shrink-0 rounded-lg p-1 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            );
          })}
        </div>
      )}
    </ToastContext.Provider>
  );
}
