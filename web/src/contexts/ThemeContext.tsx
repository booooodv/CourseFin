import type { JSX, ReactNode } from 'react';
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';

export const THEME_STORAGE_KEY = 'coursefin_theme';

export type ThemeMode = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

export interface ThemeContextValue {
  isProvided: boolean;
  mode: ThemeMode;
  resolvedTheme: ResolvedTheme;
  setMode: (next: ThemeMode) => void;
  cycleMode: () => void;
}

const DARK_MEDIA_QUERY = '(prefers-color-scheme: dark)';

const DEFAULT_THEME_CONTEXT: ThemeContextValue = {
  isProvided: false,
  mode: 'system',
  resolvedTheme: 'light',
  setMode: () => {},
  cycleMode: () => {},
};

const ThemeContext = createContext<ThemeContextValue>(DEFAULT_THEME_CONTEXT);

function isValidThemeMode(value: unknown): value is ThemeMode {
  return value === 'light' || value === 'dark' || value === 'system';
}

function readStoredMode(): ThemeMode {
  try {
    const saved = localStorage.getItem(THEME_STORAGE_KEY);
    return isValidThemeMode(saved) ? saved : 'system';
  } catch {
    return 'system';
  }
}

function writeStoredMode(mode: ThemeMode): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, mode);
  } catch {
    // Ignore storage write errors (e.g. private browsing or restricted storage).
  }
}

function getSystemPrefersDark(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return false;
  }
  try {
    return window.matchMedia(DARK_MEDIA_QUERY).matches;
  } catch {
    return false;
  }
}

/**
 * Provides dual-theme state ('light' | 'dark' | 'system'), persists mode to localStorage,
 * listens to OS color-scheme changes, and synchronizes documentElement theme attributes.
 */
export function ThemeProvider({ children }: { children: ReactNode }): JSX.Element {
  const [mode, setModeState] = useState<ThemeMode>(readStoredMode);
  const [systemDark, setSystemDark] = useState<boolean>(getSystemPrefersDark);

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return;
    }

    let mql: MediaQueryList;
    try {
      mql = window.matchMedia(DARK_MEDIA_QUERY);
    } catch {
      return;
    }

    setSystemDark(mql.matches);
    const handleChange = (event: MediaQueryListEvent) => {
      setSystemDark(event.matches);
    };

    if (typeof mql.addEventListener === 'function') {
      mql.addEventListener('change', handleChange);
      return () => mql.removeEventListener('change', handleChange);
    }

    if (typeof mql.addListener === 'function') {
      mql.addListener(handleChange);
      return () => mql.removeListener(handleChange);
    }

    return;
  }, []);

  const resolvedTheme: ResolvedTheme = mode === 'system' ? (systemDark ? 'dark' : 'light') : mode;

  useEffect(() => {
    if (typeof document === 'undefined') {
      return;
    }
    const root = document.documentElement;
    root.dataset.theme = resolvedTheme;
    root.classList.toggle('dark', resolvedTheme === 'dark');
    root.style.colorScheme = resolvedTheme;
  }, [resolvedTheme]);

  const setMode = useCallback((next: ThemeMode) => {
    const normalized = isValidThemeMode(next) ? next : 'system';
    setModeState(normalized);
    writeStoredMode(normalized);
  }, []);

  const cycleMode = useCallback(() => {
    setModeState((prev) => {
      const next: ThemeMode = prev === 'light' ? 'dark' : prev === 'dark' ? 'system' : 'light';
      writeStoredMode(next);
      return next;
    });
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({
      isProvided: true,
      mode,
      resolvedTheme,
      setMode,
      cycleMode,
    }),
    [mode, resolvedTheme, setMode, cycleMode]
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

/**
 * Reads the current theme context; safely returns a default object when rendered outside ThemeProvider.
 */
export function useTheme(): ThemeContextValue {
  return useContext(ThemeContext);
}
