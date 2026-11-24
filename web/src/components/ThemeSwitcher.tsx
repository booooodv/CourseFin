import type { JSX } from 'react';
import { Monitor, Moon, Sun } from 'lucide-react';
import { useTheme, type ThemeMode } from '../contexts/ThemeContext';
import Tooltip, { type TooltipPlacement } from './Tooltip';

const THEME_OPTIONS: ReadonlyArray<{
  id: ThemeMode;
  label: string;
  tip: string;
  Icon: typeof Sun;
}> = [
  { id: 'light', label: '浅色', tip: '浅色外观', Icon: Sun },
  { id: 'dark', label: '深色', tip: '深色外观', Icon: Moon },
  { id: 'system', label: '自动', tip: '跟随系统主题（自动切换）', Icon: Monitor },
];

export interface ThemeSwitcherProps {
  variant?: 'segmented' | 'compact';
  tooltipPlacement?: TooltipPlacement;
}

/**
 * Dual-theme switcher supporting full segmented pill control and compact icon-cycling trigger.
 */
export default function ThemeSwitcher({
  variant = 'segmented',
  tooltipPlacement = 'top',
}: ThemeSwitcherProps): JSX.Element {
  const { mode, setMode, cycleMode } = useTheme();

  if (variant === 'compact') {
    const current = THEME_OPTIONS.find((opt) => opt.id === mode) ?? THEME_OPTIONS[2];
    const CurrentIcon = current.Icon;

    return (
      <Tooltip
        content={`当前：${current.label}（点击切换主题模式）`}
        placement={tooltipPlacement}
      >
        <button
          type="button"
          aria-label="切换主题模式"
          onClick={cycleMode}
          className="inline-flex h-9 w-9 items-center justify-center rounded-xl border border-subtle bg-subtle text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors cursor-pointer"
        >
          <CurrentIcon className="h-4 w-4" />
        </button>
      </Tooltip>
    );
  }

  return (
    <div
      role="group"
      aria-label="主题模式"
      className="inline-flex items-center gap-0.5 rounded-full border border-subtle bg-muted p-1"
    >
      {THEME_OPTIONS.map(({ id, label, tip, Icon }) => {
        const active = mode === id;
        return (
          <Tooltip key={id} content={tip} placement={tooltipPlacement}>
            <button
              type="button"
              aria-pressed={active}
              onClick={() => setMode(id)}
              className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium transition-all cursor-pointer ${
                active
                  ? 'bg-card text-ink-primary shadow-card'
                  : 'text-ink-secondary hover:text-ink-primary'
              }`}
            >
              <Icon className="h-3.5 w-3.5 shrink-0" />
              <span>{label}</span>
            </button>
          </Tooltip>
        );
      })}
    </div>
  );
}
