import type { JSX, ReactNode } from 'react';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api/client';
import type { DeleteLibraryMode } from '../components/DeleteLibraryModal';
import { useToast } from '../components/Toast';
import type { Library } from '../types/coursefin';

export const SIDEBAR_COLLAPSED_STORAGE_KEY = 'coursefin_sidebar_collapsed';

export interface LibraryContextValue {
  isProvided: boolean;
  libraries: Library[];
  username: string;
  loading: boolean;
  catalogVersion: number;
  refreshLibraries: () => Promise<Library[]>;
  createLibrary: (name: string) => Promise<Library>;
  renameLibrary: (id: string, name: string) => Promise<Library>;
  registerLibrary: (lib: Library) => void;
  notifyLibraryDeleted: (deleted: Library, mode: DeleteLibraryMode, targetLibraryName?: string) => void;
  bumpCatalogVersion: () => void;
}

const DEFAULT_LIBRARY_CONTEXT: LibraryContextValue = {
  isProvided: false,
  libraries: [],
  username: '管理员',
  loading: false,
  catalogVersion: 0,
  refreshLibraries: async () => [],
  createLibrary: async () => {
    throw new Error('LibraryProvider is not mounted');
  },
  renameLibrary: async () => {
    throw new Error('LibraryProvider is not mounted');
  },
  registerLibrary: () => {},
  notifyLibraryDeleted: () => {},
  bumpCatalogVersion: () => {},
};

const LibraryContext = createContext<LibraryContextValue>(DEFAULT_LIBRARY_CONTEXT);

function upsertLibrary(list: Library[], lib: Library): Library[] {
  const index = list.findIndex((item) => item.id === lib.id);
  if (index === -1) {
    return [...list, lib];
  }
  const next = [...list];
  next[index] = lib;
  return next;
}

/**
 * Provides shared media library state, current username, and CRUD synchronization across the authenticated shell.
 */
export function LibraryProvider({ children }: { children: ReactNode }): JSX.Element {
  const { toast } = useToast();
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [username, setUsername] = useState<string>('管理员');
  const [loading, setLoading] = useState<boolean>(true);
  const [catalogVersion, setCatalogVersion] = useState<number>(0);

  const mountedRef = useRef(true);
  const toastRef = useRef(toast);
  toastRef.current = toast;

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    let active = true;
    setLoading(true);
    Promise.all([
      api.listLibraries().catch(() => ({ libraries: [] as Library[] })),
      api.getMe().catch(() => null),
    ]).then(([libsRes, meRes]) => {
      if (!active || !mountedRef.current) return;
      setLibraries(libsRes.libraries ?? []);
      const resolvedName = meRes?.user?.username?.trim();
      setUsername(resolvedName || '管理员');
      setLoading(false);
    });

    return () => {
      active = false;
    };
  }, []);

  const fetchAndSetLibraries = useCallback(async (): Promise<Library[]> => {
    const res = await api.listLibraries();
    const next = res.libraries ?? [];
    if (mountedRef.current) {
      setLibraries(next);
    }
    return next;
  }, []);

  const refreshLibraries = useCallback(async (): Promise<Library[]> => {
    const next = await fetchAndSetLibraries();
    if (mountedRef.current) {
      setCatalogVersion((v) => v + 1);
    }
    return next;
  }, [fetchAndSetLibraries]);

  const createLibrary = useCallback(async (name: string): Promise<Library> => {
    const created = await api.createLibrary(name);
    if (mountedRef.current) {
      setLibraries((prev) => upsertLibrary(prev, created));
      setCatalogVersion((v) => v + 1);
    }
    toastRef.current.success(`已创建媒体库「${created.name}」`);
    return created;
  }, []);

  const renameLibrary = useCallback(async (id: string, name: string): Promise<Library> => {
    const updated = await api.updateLibrary(id, name);
    if (mountedRef.current) {
      setLibraries((prev) => prev.map((lib) => (lib.id === updated.id ? updated : lib)));
      setCatalogVersion((v) => v + 1);
    }
    toastRef.current.success(`已将媒体库重命名为「${updated.name}」`);
    return updated;
  }, []);

  const registerLibrary = useCallback((lib: Library) => {
    if (!mountedRef.current) return;
    setLibraries((prev) => upsertLibrary(prev, lib));
  }, []);

  const notifyLibraryDeleted = useCallback(
    (deleted: Library, mode: DeleteLibraryMode, targetLibraryName?: string) => {
      if (mode === 'move' && targetLibraryName) {
        toastRef.current.success(
          `已将课程迁移至「${targetLibraryName}」并删除媒体库「${deleted.name}」`,
        );
      } else {
        toastRef.current.success(`已删除媒体库「${deleted.name}」`);
      }

      if (mountedRef.current) {
        setLibraries((prev) => prev.filter((lib) => lib.id !== deleted.id));
      }

      void refreshLibraries().catch(() => {
        if (mountedRef.current) {
          setCatalogVersion((v) => v + 1);
        }
      });
    },
    [refreshLibraries],
  );

  const bumpCatalogVersion = useCallback(() => {
    if (!mountedRef.current) return;
    setCatalogVersion((v) => v + 1);
    void fetchAndSetLibraries().catch(() => {});
  }, [fetchAndSetLibraries]);

  const value = useMemo<LibraryContextValue>(
    () => ({
      isProvided: true,
      libraries,
      username,
      loading,
      catalogVersion,
      refreshLibraries,
      createLibrary,
      renameLibrary,
      registerLibrary,
      notifyLibraryDeleted,
      bumpCatalogVersion,
    }),
    [
      libraries,
      username,
      loading,
      catalogVersion,
      refreshLibraries,
      createLibrary,
      renameLibrary,
      registerLibrary,
      notifyLibraryDeleted,
      bumpCatalogVersion,
    ],
  );

  return <LibraryContext.Provider value={value}>{children}</LibraryContext.Provider>;
}

/**
 * Reads shared library state; returns a safe no-op default when rendered outside LibraryProvider.
 */
export function useLibrary(): LibraryContextValue {
  return useContext(LibraryContext);
}
