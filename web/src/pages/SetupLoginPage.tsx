import type { FormEvent } from 'react';
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { GraduationCap, Loader2, Lock, Server, ShieldCheck, User } from 'lucide-react';
import { ApiError, api, setToken } from '../api/client';
import ThemeSwitcher from '../components/ThemeSwitcher';

const ICON_FIELD_INPUT_CLASS =
  'w-full rounded-xl border border-default bg-subtle/60 pl-10 pr-3.5 py-2.5 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors';

export default function SetupLoginPage() {
  const navigate = useNavigate();
  const [checking, setChecking] = useState(true);
  const [needsSetup, setNeedsSetup] = useState(false);

  const [serverName, setServerName] = useState('CourseFin');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    (async () => {
      try {
        const res = await api.getSetupStatus();
        if (active) {
          setNeedsSetup(res.needs_setup);
        }
      } catch (err) {
        if (active) {
          setError(err instanceof Error ? err.message : '无法连接到服务器');
        }
      } finally {
        if (active) {
          setChecking(false);
        }
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);

    const trimmedUser = username.trim();
    if (!trimmedUser || !password) {
      setError('请输入用户名和密码');
      return;
    }

    setSubmitting(true);
    try {
      if (needsSetup) {
        await api.setup({
          username: trimmedUser,
          password,
          server_name: serverName.trim() || 'CourseFin',
        });
      }
      const loginRes = await api.login({
        username: trimmedUser,
        password,
      });
      setToken(loginRes.token);
      navigate('/', { replace: true });
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 401) {
          setError('用户名或密码错误');
        } else if (err.status === 409) {
          setError('管理员账号已初始化，请直接登录');
          setNeedsSetup(false);
        } else {
          setError(err.message);
        }
      } else {
        setError(err instanceof Error ? err.message : '操作失败，请重试');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen bg-canvas text-ink-primary flex items-center justify-center p-4 relative overflow-hidden">
      {/* Subtle ambient glow */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute -top-40 left-1/2 -translate-x-1/2 h-96 w-[42rem] rounded-full bg-accent/10 blur-3xl"
      />

      {/* Pre-login theme switcher */}
      <div className="absolute top-4 right-4 z-20 sm:top-6 sm:right-6">
        <ThemeSwitcher variant="compact" tooltipPlacement="bottom" />
      </div>

      <div className="relative w-full max-w-md rounded-3xl border border-subtle bg-card p-8 sm:p-10 shadow-float">
        <div className="flex flex-col items-center text-center mb-7">
          <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-gradient-to-br from-accent to-blue-400 text-ink-inverse shadow-accent mb-4">
            <GraduationCap className="h-6 w-6" />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-ink-primary">
            {checking
              ? '正在连接 CourseFin'
              : needsSetup
                ? '初始化管理员账号'
                : '登录 CourseFin'}
          </h1>
          <p className="mt-1.5 text-sm text-ink-muted">
            {checking
              ? '正在检查系统初始化状态...'
              : needsSetup
                ? '首次使用请创建管理员账号，该账号也将用于 Jellyfin 客户端登录'
                : '使用您的管理员账号进入私有课程库'}
          </p>
        </div>

        {checking ? (
          <div className="flex flex-col items-center justify-center py-10 text-ink-muted gap-3">
            <Loader2 className="h-6 w-6 animate-spin text-accent" />
            <span className="text-sm">加载中...</span>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="space-y-4">
            {error && (
              <div
                role="alert"
                className="rounded-2xl border border-status-danger/30 bg-status-danger-soft px-4 py-3 text-sm text-status-danger-ink"
              >
                {error}
              </div>
            )}

            {needsSetup && (
              <div>
                <label
                  htmlFor="server-name"
                  className="block text-sm font-medium text-ink-secondary mb-1.5"
                >
                  服务器名称
                </label>
                <div className="relative">
                  <Server className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-ink-muted" />
                  <input
                    id="server-name"
                    type="text"
                    value={serverName}
                    onChange={(e) => setServerName(e.target.value)}
                    placeholder="CourseFin"
                    className={ICON_FIELD_INPUT_CLASS}
                  />
                </div>
              </div>
            )}

            <div>
              <label
                htmlFor="username"
                className="block text-sm font-medium text-ink-secondary mb-1.5"
              >
                {needsSetup ? '管理员用户名' : '用户名'}
              </label>
              <div className="relative">
                <User className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-ink-muted" />
                <input
                  id="username"
                  type="text"
                  autoComplete="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="例如：admin"
                  className={ICON_FIELD_INPUT_CLASS}
                />
              </div>
            </div>

            <div>
              <label
                htmlFor="password"
                className="block text-sm font-medium text-ink-secondary mb-1.5"
              >
                {needsSetup ? '管理员密码' : '密码'}
              </label>
              <div className="relative">
                <Lock className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-ink-muted" />
                <input
                  id="password"
                  type="password"
                  autoComplete={needsSetup ? 'new-password' : 'current-password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="请输入密码"
                  className={ICON_FIELD_INPUT_CLASS}
                />
              </div>
            </div>

            <button
              type="submit"
              disabled={submitting}
              className="mt-2 w-full inline-flex items-center justify-center gap-2 rounded-xl bg-accent px-4 py-2.5 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover focus:outline-none focus:ring-2 focus:ring-accent-ring disabled:opacity-60 transition-colors cursor-pointer"
            >
              {submitting ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span>{needsSetup ? '正在初始化...' : '登录中...'}</span>
                </>
              ) : needsSetup ? (
                <>
                  <ShieldCheck className="h-4 w-4" />
                  <span>完成初始化并登录</span>
                </>
              ) : (
                <span>登录</span>
              )}
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
