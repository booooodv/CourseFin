import type { ReactNode } from 'react';
import { useEffect, useRef, useState } from 'react';
import { Link, NavLink, useNavigate } from 'react-router-dom';
import { ArrowUp, BookOpen, FolderInput, GraduationCap, LogOut, Settings } from 'lucide-react';
import { api, clearToken, getToken } from '../api/client';
import Tooltip from './Tooltip';
import { useUpdate } from './UpdateContext';

interface LayoutProps {
  children: ReactNode;
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

export default function Layout({ children }: LayoutProps) {
  const navigate = useNavigate();
  const [loggingOut, setLoggingOut] = useState(false);
  const { isProvided, status, refreshStatus } = useUpdate();
  const requestedUpdateRef = useRef(false);

  useEffect(() => {
    if (!requestedUpdateRef.current && isProvided && status === null && getToken()) {
      requestedUpdateRef.current = true;
      void refreshStatus();
    }
  }, [isProvided, refreshStatus, status]);

  const latestVersionLabel = formatVersionLabel(status?.latest_version);
  const hasUpdate = Boolean(status?.update_available && latestVersionLabel);

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
      navigate('/login', { replace: true });
    }
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col">
      <header className="sticky top-0 z-30 border-b border-slate-800/80 bg-slate-950/85 backdrop-blur-md">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-4 sm:px-6 h-16 gap-4">
          <div className="flex items-center gap-8">
            <NavLink to="/" className="flex items-center gap-2.5 text-white group">
              <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-indigo-500 to-sky-500 shadow-lg shadow-indigo-500/20 group-hover:scale-105 transition-transform">
                <GraduationCap className="h-5 w-5 text-white" />
              </span>
              <span className="text-lg font-bold tracking-tight">CourseFin</span>
            </NavLink>

            <nav aria-label="主导航" className="hidden md:flex items-center gap-1">
              {navItems.map((item) => {
                const Icon = item.icon;
                return (
                  <Tooltip key={item.to} content={item.tip} placement="bottom">
                    <NavLink
                      to={item.to}
                      end={item.end}
                      className={({ isActive }) =>
                        `flex items-center gap-2 rounded-lg px-3.5 py-2 text-sm font-medium transition-colors ${
                          isActive
                            ? 'bg-indigo-500/15 text-indigo-300 border border-indigo-500/30'
                            : 'text-slate-300 hover:bg-slate-800/70 hover:text-white border border-transparent'
                        }`
                      }
                    >
                      <Icon className="h-4 w-4" />
                      <span>{item.label}</span>
                    </NavLink>
                  </Tooltip>
                );
              })}
            </nav>
          </div>

          <div className="flex items-center gap-3">
            {hasUpdate && (
              <Tooltip
                content={`发现新版本 ${latestVersionLabel}，点击前往设置页查看并更新`}
                placement="bottom"
              >
                <Link
                  to="/settings#system-update"
                  className="hidden md:inline-flex items-center gap-1.5 rounded-full border border-emerald-500/40 bg-emerald-500/15 px-3 py-1 text-xs font-medium text-emerald-300 hover:border-emerald-400/60 hover:bg-emerald-500/25 hover:text-emerald-200 transition-colors"
                >
                  <ArrowUp className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                  <span>新版本 {latestVersionLabel}</span>
                </Link>
              </Tooltip>
            )}

            <Tooltip content="退出当前浏览器登录会话" placement="bottom">
              <button
                type="button"
                onClick={handleLogout}
                disabled={loggingOut}
                className="inline-flex items-center gap-2 rounded-lg border border-slate-800 bg-slate-900/80 px-3 py-1.5 text-sm text-slate-300 hover:border-slate-700 hover:bg-slate-800 hover:text-white transition-colors disabled:opacity-50"
              >
                <LogOut className="h-4 w-4" />
                <span>{loggingOut ? '退出中...' : '退出登录'}</span>
              </button>
            </Tooltip>
          </div>
        </div>

        {/* Mobile navigation bar */}
        <nav
          aria-label="移动端导航"
          className="flex md:hidden border-t border-slate-800/70 px-2 py-1.5 justify-around bg-slate-950"
        >
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  `flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                    isActive
                      ? 'bg-indigo-500/15 text-indigo-300'
                      : 'text-slate-400 hover:text-slate-200'
                  }`
                }
              >
                <span className="relative inline-flex">
                  <Icon className="h-3.5 w-3.5" />
                  {item.to === '/settings' && hasUpdate && (
                    <span
                      data-testid="mobile-update-dot"
                      aria-hidden="true"
                      className="absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full bg-emerald-400 ring-2 ring-slate-950"
                    />
                  )}
                </span>
                <span>{item.label}</span>
              </NavLink>
            );
          })}
        </nav>
      </header>

      <main className="flex-1 mx-auto w-full max-w-7xl px-4 sm:px-6 py-6 sm:py-8">
        {children}
      </main>
    </div>
  );
}
