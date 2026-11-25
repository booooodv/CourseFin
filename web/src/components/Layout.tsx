import type { JSX, ReactNode } from 'react';
import { useEffect, useRef, useState } from 'react';
import {
  Link,
  NavLink,
  Outlet,
  useLocation,
  useNavigate,
  useSearchParams,
} from 'react-router-dom';
import {
  ArrowUp,
  BookOpen,
  Folder,
  FolderInput,
  LogOut,
  Menu,
  PanelLeftClose,
  PanelLeftOpen,
  Pencil,
  Plus,
  Settings,
  Trash2,
  X,
} from 'lucide-react';
import { api, clearToken, getToken } from '../api/client';
import {
  SIDEBAR_COLLAPSED_STORAGE_KEY,
  useLibrary,
  type LibraryContextValue,
} from '../contexts/LibraryContext';
import type { Library } from '../types/coursefin';
import CourseFinLogo from './CourseFinLogo';
import DeleteLibraryModal, { type DeleteLibraryMode } from './DeleteLibraryModal';
import LibraryFormModal from './LibraryFormModal';
import ThemeSwitcher from './ThemeSwitcher';
import Tooltip from './Tooltip';
import { useUpdate } from './UpdateContext';

export { SIDEBAR_COLLAPSED_STORAGE_KEY };

interface LayoutProps {
  children?: ReactNode;
}

const navItems = [
  { to: '/', label: '课程库', tip: '浏览全部课程、继续学习与管理媒体库', icon: BookOpen, end: true },
  { to: '/storage', label: '存储与导入', tip: '配置本地目录、扫描视频并一键导入课程', icon: FolderInput, end: false },
  { to: '/settings', label: '设置', tip: '查看 Jellyfin 连接信息、配置 AI 模型与会话', icon: Settings, end: false },
];

function formatVersionLabel(raw?: string | null): string {
  const trimmed = (raw ?? '').trim().replace(/^[vV](?=\d)/, '');
  return trimmed ? `v${trimmed}` : '';
}

function readStoredSidebarCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY) === 'true';
  } catch {
    return false;
  }
}

function writeStoredSidebarCollapsed(collapsed: boolean): void {
  try {
    localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, collapsed ? 'true' : 'false');
  } catch {
    // Ignore storage write errors in private or restricted environments.
  }
}

interface SidebarContentProps {
  collapsed: boolean;
  isMobileDrawer?: boolean;
  onToggleCollapse?: () => void;
  onCloseDrawer?: () => void;
  onAfterNavigate?: () => void;
  activeLibraryParam: string | null;
  isCourseSubroute: boolean;
  libCtx: LibraryContextValue;
  hasUpdate: boolean;
  latestVersionLabel: string;
  loggingOut: boolean;
  onSelectLibrary: (libraryId: string) => void;
  onOpenCreateLibrary: () => void;
  onOpenRenameLibrary: (lib: Library) => void;
  onOpenDeleteLibrary: (lib: Library) => void;
  onLogout: () => void;
}

function SidebarContent({
  collapsed,
  isMobileDrawer = false,
  onToggleCollapse,
  onCloseDrawer,
  onAfterNavigate,
  activeLibraryParam,
  isCourseSubroute,
  libCtx,
  hasUpdate,
  latestVersionLabel,
  loggingOut,
  onSelectLibrary,
  onOpenCreateLibrary,
  onOpenRenameLibrary,
  onOpenDeleteLibrary,
  onLogout,
}: SidebarContentProps): JSX.Element {
  const displayUsername = libCtx.username || '管理员';
  const userInitial = displayUsername.trim().slice(0, 1).toUpperCase() || '管';
  const updateTip = `发现新版本 ${latestVersionLabel}，点击前往设置页查看并更新`;

  return (
    <div className="flex h-full w-full min-h-0 flex-col overflow-x-hidden">
      {/* Brand header */}
      <div
        className={`shrink-0 border-b border-subtle ${
          collapsed
            ? 'flex flex-col items-center gap-2.5 px-2.5 py-3.5'
            : 'flex h-16 items-center justify-between px-4'
        }`}
      >
        <NavLink
          to="/"
          aria-label="CourseFin"
          onClick={onAfterNavigate}
          className={
            collapsed
              ? 'group flex items-center justify-center'
              : 'group flex min-w-0 items-center gap-2.5'
          }
        >
          <CourseFinLogo className="h-9 w-9 shadow-accent transition-transform group-hover:scale-105" />
          {!collapsed && (
            <span className="truncate text-base font-bold tracking-tight text-ink-primary">
              CourseFin
            </span>
          )}
        </NavLink>

        {isMobileDrawer ? (
          <button
            type="button"
            aria-label="关闭导航菜单"
            onClick={onCloseDrawer}
            className="flex h-8 w-8 items-center justify-center rounded-xl text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
          >
            <X className="h-4 w-4" />
          </button>
        ) : (
          <Tooltip content={collapsed ? '展开侧边栏' : '收起侧边栏'} placement="right">
            <button
              type="button"
              aria-label={collapsed ? '展开侧边栏' : '收起侧边栏'}
              onClick={onToggleCollapse}
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
            >
              {collapsed ? (
                <PanelLeftOpen className="h-4 w-4" />
              ) : (
                <PanelLeftClose className="h-4 w-4" />
              )}
            </button>
          </Tooltip>
        )}
      </div>

      {/* Primary Navigation */}
      <div className={collapsed ? 'px-2.5 pt-4 pb-2' : 'px-3 pt-4 pb-2'}>
        {!collapsed && (
          <div className="mb-1.5 px-3 text-[11px] font-semibold uppercase tracking-wider text-ink-muted">
            导航
          </div>
        )}
        <nav aria-label={isMobileDrawer ? '移动端主导航' : '主导航'} className="space-y-1">
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <Tooltip
                key={item.to}
                content={collapsed ? item.label : item.tip}
                placement="right"
              >
                <NavLink
                  to={item.to}
                  end={item.end}
                  onClick={onAfterNavigate}
                  className={({ isActive }) => {
                    const active =
                      item.to === '/'
                        ? (isActive && !activeLibraryParam) || isCourseSubroute
                        : isActive;
                    return `relative group flex items-center text-sm transition-colors ${
                      collapsed
                        ? 'mx-auto h-10 w-10 justify-center rounded-xl'
                        : 'gap-3 rounded-xl px-3 py-2.5'
                    } ${
                      active
                        ? 'bg-accent-soft text-ink-primary font-semibold'
                        : 'text-ink-secondary font-medium hover:bg-subtle hover:text-ink-primary'
                    }`;
                  }}
                >
                  {({ isActive }) => {
                    const active =
                      item.to === '/'
                        ? (isActive && !activeLibraryParam) || isCourseSubroute
                        : isActive;
                    return (
                      <>
                        {active && !collapsed && (
                          <span
                            aria-hidden="true"
                            className="absolute left-0 top-2 bottom-2 w-[3px] rounded-full bg-accent"
                          />
                        )}
                        <Icon
                          className={`h-4 w-4 shrink-0 transition-colors ${
                            active
                              ? 'text-accent-ink'
                              : 'text-ink-muted group-hover:text-ink-primary'
                          }`}
                        />
                        <span className={collapsed ? 'sr-only' : 'truncate'}>{item.label}</span>
                      </>
                    );
                  }}
                </NavLink>
              </Tooltip>
            );
          })}
        </nav>
      </div>

      {/* My Libraries Section */}
      {libCtx.isProvided ? (
        <div className="min-h-0 flex-1 flex flex-col pt-3">
          {collapsed ? (
            <div className="mx-4 mb-2 border-t border-subtle" />
          ) : (
            <div className="mb-1.5 flex items-center justify-between px-6 shrink-0">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-ink-muted">
                我的媒体库
              </span>
              <Tooltip content="新建一个课程分类媒体库" placement="right">
                <button
                  type="button"
                  aria-label="新建媒体库"
                  onClick={onOpenCreateLibrary}
                  className="flex h-6 w-6 items-center justify-center rounded-lg text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
                >
                  <Plus className="h-3.5 w-3.5" />
                </button>
              </Tooltip>
            </div>
          )}

          <div
            className={`min-h-0 flex-1 overflow-y-auto overflow-x-hidden ${
              collapsed ? 'px-2.5 space-y-1.5' : 'px-3 space-y-1'
            }`}
          >
            {collapsed ? (
              <>
                {libCtx.libraries.map((lib) => {
                  const isLibActive = activeLibraryParam === lib.id;
                  const initial = lib.name.trim().slice(0, 1).toUpperCase() || '库';
                  return (
                    <Tooltip
                      key={lib.id}
                      content={`${lib.name} (${lib.course_count})`}
                      placement="right"
                    >
                      <button
                        type="button"
                        aria-label={lib.name}
                        onClick={() => onSelectLibrary(lib.id)}
                        className={`mx-auto flex h-10 w-10 items-center justify-center rounded-xl text-xs font-bold transition-colors cursor-pointer ${
                          isLibActive
                            ? 'bg-accent text-ink-inverse shadow-accent'
                            : 'bg-subtle text-ink-secondary hover:bg-muted hover:text-ink-primary'
                        }`}
                      >
                        <span aria-hidden="true">{initial}</span>
                      </button>
                    </Tooltip>
                  );
                })}
                <Tooltip content="新建一个课程分类媒体库" placement="right">
                  <button
                    type="button"
                    aria-label="新建媒体库"
                    onClick={onOpenCreateLibrary}
                    className="mx-auto flex h-10 w-10 items-center justify-center rounded-xl border border-dashed border-default text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors cursor-pointer"
                  >
                    <Plus className="h-4 w-4" />
                  </button>
                </Tooltip>
              </>
            ) : (
              <>
                {!libCtx.loading && libCtx.libraries.length === 0 && (
                  <p className="px-3 py-2 text-xs text-ink-muted">
                    暂无媒体库，点击上方 + 创建
                  </p>
                )}
                {libCtx.libraries.map((lib) => {
                  const isLibActive = activeLibraryParam === lib.id;
                  return (
                    <div
                      key={lib.id}
                      role="group"
                      aria-label={`媒体库 ${lib.name}`}
                      className={`relative group flex items-center gap-1 rounded-xl pr-1.5 transition-colors ${
                        isLibActive
                          ? 'bg-accent-soft text-ink-primary'
                          : 'text-ink-secondary hover:bg-subtle hover:text-ink-primary'
                      }`}
                    >
                      {isLibActive && (
                        <span
                          aria-hidden="true"
                          className="absolute left-0 top-2 bottom-2 w-[3px] rounded-full bg-accent"
                        />
                      )}
                      <button
                        type="button"
                        onClick={() => onSelectLibrary(lib.id)}
                        className={`flex-1 min-w-0 flex items-center gap-2.5 pl-3 py-2 text-left text-sm cursor-pointer ${
                          isLibActive ? 'font-semibold text-ink-primary' : 'font-medium'
                        }`}
                      >
                        <Folder
                          className={`h-4 w-4 shrink-0 ${
                            isLibActive
                              ? 'text-accent-ink'
                              : 'text-ink-muted group-hover:text-ink-primary'
                          }`}
                        />
                        <span className="truncate flex-1">{lib.name}</span>
                        <span
                          className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium tabular-nums ${
                            isLibActive
                              ? 'bg-accent text-ink-inverse'
                              : 'bg-muted text-ink-muted'
                          }`}
                        >
                          {lib.course_count}
                        </span>
                      </button>

                      <div
                        className={`flex items-center gap-0.5 shrink-0 transition-opacity ${
                          isLibActive || isMobileDrawer
                            ? 'opacity-100'
                            : 'opacity-100 lg:opacity-0 lg:group-hover:opacity-100 lg:group-focus-within:opacity-100'
                        }`}
                      >
                        <Tooltip content="重命名媒体库" placement="top">
                          <button
                            type="button"
                            aria-label="重命名媒体库"
                            onClick={() => onOpenRenameLibrary(lib)}
                            className="rounded-lg p-1 text-ink-muted hover:bg-card hover:text-ink-primary transition-colors cursor-pointer"
                          >
                            <Pencil className="h-3.5 w-3.5" />
                          </button>
                        </Tooltip>
                        <Tooltip content="删除媒体库" placement="top">
                          <button
                            type="button"
                            aria-label="删除媒体库"
                            onClick={() => onOpenDeleteLibrary(lib)}
                            className="rounded-lg p-1 text-ink-muted hover:bg-status-danger-soft hover:text-status-danger-ink transition-colors cursor-pointer"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </button>
                        </Tooltip>
                      </div>
                    </div>
                  );
                })}
              </>
            )}
          </div>
        </div>
      ) : (
        <div className="flex-1" />
      )}

      {/* Footer */}
      <div
        className={`shrink-0 border-t border-subtle ${
          collapsed ? 'flex flex-col items-center gap-2.5 px-2.5 py-3' : 'space-y-3 p-3.5'
        }`}
      >
        {hasUpdate && (
          <Tooltip content={updateTip} placement={collapsed ? 'right' : 'top'}>
            <Link
              to="/settings#system-update"
              onClick={onAfterNavigate}
              className={
                collapsed
                  ? 'flex h-9 w-9 items-center justify-center rounded-full border border-status-success/30 bg-status-success-soft text-status-success-ink hover:opacity-90 transition-opacity'
                  : 'flex w-full items-center justify-center gap-1.5 rounded-xl border border-status-success/30 bg-status-success-soft px-3 py-2 text-xs font-semibold text-status-success-ink hover:opacity-90 transition-opacity'
              }
            >
              <ArrowUp className={collapsed ? 'h-4 w-4 shrink-0' : 'h-3.5 w-3.5 shrink-0'} aria-hidden="true" />
              <span className={collapsed ? 'sr-only' : 'truncate'}>新版本 {latestVersionLabel}</span>
            </Link>
          </Tooltip>
        )}

        <div className="flex justify-center">
          <ThemeSwitcher
            variant={collapsed ? 'compact' : 'segmented'}
            tooltipPlacement={collapsed ? 'right' : 'top'}
          />
        </div>

        <div
          className={
            collapsed
              ? 'flex flex-col items-center gap-2 pt-1'
              : 'flex items-center justify-between gap-2 rounded-2xl bg-subtle/60 px-2.5 py-2'
          }
        >
          <div
            className={
              collapsed ? 'flex items-center justify-center' : 'flex min-w-0 items-center gap-2.5'
            }
          >
            <span
              aria-hidden="true"
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-accent-soft text-xs font-bold text-accent-ink"
            >
              {userInitial}
            </span>
            {!collapsed && (
              <span className="truncate text-xs font-semibold text-ink-primary">
                {displayUsername}
              </span>
            )}
          </div>

          <Tooltip content="退出当前浏览器登录会话" placement={collapsed ? 'right' : 'top'}>
            <button
              type="button"
              onClick={onLogout}
              disabled={loggingOut}
              className={
                collapsed
                  ? 'inline-flex h-9 w-9 items-center justify-center rounded-xl border border-subtle bg-card text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors disabled:opacity-50 cursor-pointer'
                  : 'inline-flex shrink-0 items-center gap-1.5 rounded-xl border border-subtle bg-card px-2.5 py-1.5 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors disabled:opacity-50 cursor-pointer'
              }
            >
              <LogOut className="h-3.5 w-3.5 shrink-0" />
              <span className={collapsed ? 'sr-only' : ''}>
                {loggingOut ? '退出中...' : '退出登录'}
              </span>
            </button>
          </Tooltip>
        </div>
      </div>
    </div>
  );
}

/**
 * Persistent Citonema-inspired left-sidebar AppShell with desktop theater auto-collapse,
 * mobile top header + slide-over drawer, and shared library CRUD modals.
 */
export default function Layout({ children }: LayoutProps): JSX.Element {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const libCtx = useLibrary();
  const { isProvided, status, refreshStatus } = useUpdate();

  const [loggingOut, setLoggingOut] = useState(false);
  const [userCollapsed, setUserCollapsed] = useState<boolean>(readStoredSidebarCollapsed);
  const [theaterOverride, setTheaterOverride] = useState<{
    pathname: string;
    collapsed: boolean;
  } | null>(null);
  const [mobileDrawerOpen, setMobileDrawerOpen] = useState(false);

  const [formModalOpen, setFormModalOpen] = useState(false);
  const [formModalMode, setFormModalMode] = useState<'create' | 'rename'>('create');
  const [editingLibrary, setEditingLibrary] = useState<Library | null>(null);
  const [formSubmitting, setFormSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [deletingLibrary, setDeletingLibrary] = useState<Library | null>(null);

  const requestedUpdateRef = useRef(false);

  useEffect(() => {
    if (!requestedUpdateRef.current && isProvided && status === null && getToken()) {
      requestedUpdateRef.current = true;
      void refreshStatus();
    }
  }, [isProvided, refreshStatus, status]);

  const isPlayerRoute = /^\/courses\/[^/]+\/?$/.test(location.pathname);
  const isCourseSubroute = location.pathname.startsWith('/courses/');
  const rawLibraryParam =
    location.pathname === '/' ? (searchParams.get('library')?.trim() || null) : null;
  const activeLibraryParam =
    libCtx.isProvided &&
    !libCtx.loading &&
    rawLibraryParam &&
    !libCtx.libraries.some((l) => l.id === rawLibraryParam)
      ? null
      : rawLibraryParam;

  useEffect(() => {
    setTheaterOverride(null);
  }, [isPlayerRoute, location.pathname]);

  useEffect(() => {
    setMobileDrawerOpen(false);
  }, [location.pathname, location.search]);

  useEffect(() => {
    if (!mobileDrawerOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !formModalOpen && !deletingLibrary) {
        setMobileDrawerOpen(false);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [mobileDrawerOpen, formModalOpen, deletingLibrary]);

  const activeTheaterOverride =
    theaterOverride && theaterOverride.pathname === location.pathname
      ? theaterOverride.collapsed
      : null;
  const effectiveCollapsed = isPlayerRoute ? (activeTheaterOverride ?? true) : userCollapsed;

  const latestVersionLabel = formatVersionLabel(status?.latest_version);
  const hasUpdate = Boolean(status?.update_available && latestVersionLabel);

  function handleToggleCollapse() {
    if (isPlayerRoute) {
      setTheaterOverride({
        pathname: location.pathname,
        collapsed: !effectiveCollapsed,
      });
      return;
    }
    const next = !userCollapsed;
    setUserCollapsed(next);
    writeStoredSidebarCollapsed(next);
  }

  function handleSelectLibrary(libraryId: string) {
    navigate(`/?library=${encodeURIComponent(libraryId)}`);
    setMobileDrawerOpen(false);
  }

  function openCreateLibraryModal() {
    setFormModalMode('create');
    setEditingLibrary(null);
    setFormError(null);
    setFormModalOpen(true);
  }

  function openRenameLibraryModal(lib: Library) {
    setFormModalMode('rename');
    setEditingLibrary(lib);
    setFormError(null);
    setFormModalOpen(true);
  }

  async function handleFormSubmit(trimmedName: string) {
    if (formSubmitting) return;
    if (formModalMode === 'rename' && editingLibrary && trimmedName === editingLibrary.name) {
      setFormModalOpen(false);
      return;
    }

    setFormSubmitting(true);
    setFormError(null);
    try {
      if (formModalMode === 'create') {
        await libCtx.createLibrary(trimmedName);
      } else if (editingLibrary) {
        await libCtx.renameLibrary(editingLibrary.id, trimmedName);
      }
      setFormModalOpen(false);
    } catch (err) {
      setFormError(
        err instanceof Error
          ? err.message
          : formModalMode === 'create'
            ? '创建媒体库失败'
            : '重命名媒体库失败',
      );
    } finally {
      setFormSubmitting(false);
    }
  }

  function handleLibraryDeleted(
    deleted: Library,
    mode: DeleteLibraryMode,
    targetLibraryName?: string,
  ) {
    libCtx.notifyLibraryDeleted(deleted, mode, targetLibraryName);
    if (location.pathname === '/' && searchParams.get('library') === deleted.id) {
      navigate('/', { replace: true });
    }
  }

  async function handleLogout() {
    if (loggingOut) return;
    setLoggingOut(true);
    try {
      await api.logout();
    } catch {
      // Ignore network/401 errors during logout; still clear local session.
    } finally {
      clearToken();
      setLoggingOut(false);
      setMobileDrawerOpen(false);
      navigate('/login', { replace: true });
    }
  }

  return (
    <div className="min-h-screen bg-canvas text-ink-primary flex flex-col lg:flex-row">
      {/* Desktop Left Sidebar */}
      <aside
        aria-label="主侧边栏"
        data-collapsed={effectiveCollapsed ? 'true' : 'false'}
        className={`hidden lg:flex sticky top-0 h-screen min-w-0 shrink-0 overflow-x-hidden bg-sidebar border-r border-subtle shadow-sidebar flex-col transition-[width,max-width] duration-200 ${
          effectiveCollapsed ? 'w-[76px] max-w-[76px]' : 'w-[260px] max-w-[260px]'
        }`}
      >
        <SidebarContent
          collapsed={effectiveCollapsed}
          onToggleCollapse={handleToggleCollapse}
          activeLibraryParam={activeLibraryParam}
          isCourseSubroute={isCourseSubroute}
          libCtx={libCtx}
          hasUpdate={hasUpdate}
          latestVersionLabel={latestVersionLabel}
          loggingOut={loggingOut}
          onSelectLibrary={handleSelectLibrary}
          onOpenCreateLibrary={openCreateLibraryModal}
          onOpenRenameLibrary={openRenameLibraryModal}
          onOpenDeleteLibrary={setDeletingLibrary}
          onLogout={() => void handleLogout()}
        />
      </aside>

      {/* Mobile Top Bar (< lg) */}
      <header className="flex lg:hidden sticky top-0 z-30 h-14 items-center justify-between bg-sidebar/90 backdrop-blur-md border-b border-subtle px-4">
        <div className="flex items-center gap-3">
          <button
            type="button"
            aria-label="打开导航菜单"
            aria-expanded={mobileDrawerOpen}
            onClick={() => setMobileDrawerOpen(true)}
            className="flex h-9 w-9 items-center justify-center rounded-xl border border-subtle bg-subtle text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors cursor-pointer"
          >
            <Menu className="h-5 w-5" />
          </button>

          <Link to="/" className="flex items-center gap-2 group">
            <CourseFinLogo className="h-8 w-8 shadow-accent transition-transform group-hover:scale-105" />
            <span className="text-base font-bold tracking-tight text-ink-primary">CourseFin</span>
          </Link>
        </div>

        <div className="flex items-center gap-2">
          {hasUpdate && (
            <Tooltip
              content={`发现新版本 ${latestVersionLabel}，点击前往设置页查看并更新`}
              placement="bottom"
            >
              <Link
                to="/settings#system-update"
                aria-label="查看新版本更新"
                className="relative inline-flex h-9 w-9 items-center justify-center rounded-xl border border-status-success/30 bg-status-success-soft text-status-success-ink hover:opacity-90 transition-opacity"
              >
                <ArrowUp className="h-4 w-4" aria-hidden="true" />
                <span
                  data-testid="mobile-update-dot"
                  aria-hidden="true"
                  className="absolute top-1.5 right-1.5 h-2 w-2 rounded-full bg-status-success ring-2 ring-sidebar"
                />
              </Link>
            </Tooltip>
          )}
          <ThemeSwitcher variant="compact" tooltipPlacement="bottom" />
        </div>
      </header>

      {/* Mobile Slide-Over Drawer */}
      {mobileDrawerOpen && (
        <div className="fixed inset-0 z-40 flex lg:hidden">
          <div
            aria-hidden="true"
            onClick={() => setMobileDrawerOpen(false)}
            className="fixed inset-0 bg-overlay backdrop-blur-sm"
          />
          <div
            role="dialog"
            aria-modal="true"
            aria-label="导航菜单"
            className="relative z-10 flex h-full w-72 max-w-[85vw] flex-col border-r border-subtle bg-sidebar shadow-float"
          >
            <SidebarContent
              collapsed={false}
              isMobileDrawer={true}
              onCloseDrawer={() => setMobileDrawerOpen(false)}
              onAfterNavigate={() => setMobileDrawerOpen(false)}
              activeLibraryParam={activeLibraryParam}
              isCourseSubroute={isCourseSubroute}
              libCtx={libCtx}
              hasUpdate={hasUpdate}
              latestVersionLabel={latestVersionLabel}
              loggingOut={loggingOut}
              onSelectLibrary={handleSelectLibrary}
              onOpenCreateLibrary={openCreateLibraryModal}
              onOpenRenameLibrary={openRenameLibraryModal}
              onOpenDeleteLibrary={setDeletingLibrary}
              onLogout={() => void handleLogout()}
            />
          </div>
        </div>
      )}

      {/* Main Content */}
      <main className="flex-1 min-w-0 px-4 sm:px-6 lg:px-8 py-6 sm:py-8">
        <div className="mx-auto w-full max-w-[1400px]">{children ?? <Outlet />}</div>
      </main>

      {/* Shared Library Modals */}
      <LibraryFormModal
        isOpen={formModalOpen}
        mode={formModalMode}
        initialName={editingLibrary?.name ?? ''}
        submitting={formSubmitting}
        error={formError}
        onClose={() => {
          if (!formSubmitting) {
            setFormModalOpen(false);
          }
        }}
        onSubmit={handleFormSubmit}
      />

      <DeleteLibraryModal
        library={deletingLibrary}
        libraries={libCtx.libraries}
        onClose={() => setDeletingLibrary(null)}
        onDeleted={handleLibraryDeleted}
      />
    </div>
  );
}
