import type { ReactNode } from 'react';
import { useState } from 'react';
import { NavLink, useNavigate } from 'react-router-dom';
import { BookOpen, FolderInput, GraduationCap, LogOut, Settings } from 'lucide-react';
import { api, clearToken } from '../api/client';
import Tooltip from './Tooltip';

interface LayoutProps {
  children: ReactNode;
}

const navItems = [
  { to: '/', label: '课程库', tip: '浏览全部课程、继续学习与管理媒体库', icon: BookOpen, end: true },
  { to: '/storage', label: '存储与导入', tip: '配置本地目录、扫描视频并一键导入课程', icon: FolderInput, end: false },
  { to: '/settings', label: '设置', tip: '查看 Jellyfin 连接信息、配置 AI 模型与会话', icon: Settings, end: false },
];

export default function Layout({ children }: LayoutProps) {
  const navigate = useNavigate();
  const [loggingOut, setLoggingOut] = useState(false);

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
                <Icon className="h-3.5 w-3.5" />
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
