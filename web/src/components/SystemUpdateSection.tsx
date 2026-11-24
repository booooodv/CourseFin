import type { FormEvent } from 'react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  AlertTriangle,
  ArrowUpCircle,
  CheckCircle2,
  Download,
  ExternalLink,
  Loader2,
  RefreshCw,
  RotateCcw,
  Save,
  ShieldAlert,
  X,
} from 'lucide-react';
import { api, getToken } from '../api/client';
import type { UpdateConfigInput, UpdateState, UpdateStatus } from '../types/coursefin';
import { useToast } from './Toast';
import Tooltip from './Tooltip';
import { useUpdate } from './UpdateContext';

export interface SystemUpdateSectionProps {
  onReload?: () => void;
}

const DEFAULT_INTERVAL_OPTIONS = [1, 6, 12, 24];

const FIELD_INPUT_CLASS =
  'w-full rounded-xl border border-default bg-subtle/60 px-3.5 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors disabled:opacity-50';

function defaultReload(): void {
  if (typeof window !== 'undefined' && window.location) {
    window.location.reload();
  }
}

function normalizeVersion(raw?: string | null): string {
  const trimmed = (raw ?? '').trim();
  return trimmed.replace(/^[vV](?=\d)/, '');
}

function formatVersionLabel(raw?: string | null): string {
  const normalized = normalizeVersion(raw);
  return normalized ? `v${normalized}` : '--';
}

function getSafeHttpUrl(raw?: string | null): string | null {
  const trimmed = (raw ?? '').trim();
  if (!trimmed) return null;
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
      return parsed.toString();
    }
    return null;
  } catch {
    return null;
  }
}

function validateMirrorUrl(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return '加速代理地址格式无效，请输入以 http:// 或 https:// 开头的完整 URL';
  }
  if ((parsed.protocol !== 'http:' && parsed.protocol !== 'https:') || !parsed.hostname) {
    return '加速代理地址必须以 http:// 或 https:// 开头并包含有效主机名';
  }
  return null;
}

function formatMegabytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0.0 MB';
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatTimestamp(raw?: string | null): string {
  const trimmed = (raw ?? '').trim();
  if (!trimmed) return '尚未检查';
  const date = new Date(trimmed);
  if (Number.isNaN(date.getTime())) {
    return trimmed;
  }
  return date.toLocaleString('zh-CN', { hour12: false });
}

function isActiveApplyState(state?: UpdateState | string | null): boolean {
  return (
    state === 'downloading' ||
    state === 'verifying' ||
    state === 'applying' ||
    state === 'restarting'
  );
}

function getProgressStageLabel(state: UpdateState): string {
  switch (state) {
    case 'downloading':
      return '正在下载更新包...';
    case 'verifying':
      return '正在校验 SHA-256 签名并执行预检...';
    case 'applying':
      return '正在替换可执行文件并保存备份...';
    case 'restarting':
      return '正在优雅停机并热重启服务...';
    default:
      return '正在处理更新...';
  }
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(100, Math.round(value)));
}

export function SystemUpdateSection({ onReload }: SystemUpdateSectionProps) {
  const ctx = useUpdate();
  const { toast } = useToast();

  // Standalone fallback state when rendered outside <UpdateProvider> (e.g., isolated unit tests)
  const [standaloneStatus, setStandaloneStatus] = useState<UpdateStatus | null>(null);
  const [standaloneLoading, setStandaloneLoading] = useState(false);
  const [standaloneChecking, setStandaloneChecking] = useState(false);
  const [standaloneApplying, setStandaloneApplying] = useState(false);
  const requestedProviderRef = useRef(false);

  useEffect(() => {
    if (ctx.isProvided) {
      if (!requestedProviderRef.current && ctx.status === null && !ctx.loading && getToken()) {
        requestedProviderRef.current = true;
        void ctx.refreshStatus();
      }
      return;
    }

    let cancelled = false;
    setStandaloneLoading(true);
    void api
      .getUpdateStatus()
      .then((data) => {
        if (!cancelled) {
          setStandaloneStatus(data);
        }
      })
      .catch(() => null)
      .finally(() => {
        if (!cancelled) {
          setStandaloneLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [ctx.isProvided, ctx.loading, ctx.refreshStatus, ctx.status]);

  const status = ctx.isProvided ? ctx.status : standaloneStatus;
  const loading = ctx.isProvided ? ctx.loading : standaloneLoading;
  const checking = ctx.isProvided ? ctx.checking : standaloneChecking;
  const applying = ctx.isProvided ? ctx.applying : standaloneApplying;
  const restartPhase = ctx.isProvided ? ctx.restartPhase : 'idle';
  const restartError = ctx.isProvided ? ctx.restartError : null;

  const [inlineError, setInlineError] = useState<string | null>(null);

  // Config sub-form state
  const [autoCheckEnabled, setAutoCheckEnabled] = useState(true);
  const [checkIntervalHours, setCheckIntervalHours] = useState('6');
  const [mirrorUrl, setMirrorUrl] = useState('');
  const [configDirty, setConfigDirty] = useState(false);
  const [savingConfig, setSavingConfig] = useState(false);
  const [configError, setConfigError] = useState<string | null>(null);
  const initializedFromStatusRef = useRef(false);

  const syncFormFromStatus = useCallback((next: UpdateStatus) => {
    setAutoCheckEnabled(next.auto_check_enabled);
    setCheckIntervalHours(String(next.check_interval_hours || 6));
    setMirrorUrl(next.mirror_url || '');
    setConfigDirty(false);
  }, []);

  useEffect(() => {
    if (!status) return;
    if (!initializedFromStatusRef.current || !configDirty) {
      initializedFromStatusRef.current = true;
      syncFormFromStatus(status);
    }
  }, [configDirty, status, syncFormFromStatus]);

  // Apply confirmation modal state
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [submittingApply, setSubmittingApply] = useState(false);
  const [modalError, setModalError] = useState<string | null>(null);

  useEffect(() => {
    if (!confirmOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !submittingApply) {
        setConfirmOpen(false);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [confirmOpen, submittingApply]);

  const intervalOptions = useMemo(() => {
    const currentVal = Number.parseInt(checkIntervalHours, 10);
    if (
      !Number.isNaN(currentVal) &&
      currentVal > 0 &&
      !DEFAULT_INTERVAL_OPTIONS.includes(currentVal)
    ) {
      return [...DEFAULT_INTERVAL_OPTIONS, currentVal].sort((a, b) => a - b);
    }
    return DEFAULT_INTERVAL_OPTIONS;
  }, [checkIntervalHours]);

  const hasActiveApplyStage =
    !restartError &&
    (restartPhase === 'idle' || restartPhase === 'waiting') &&
    isActiveApplyState(status?.state);

  const isUpdateBusy =
    applying ||
    restartPhase === 'waiting' ||
    hasActiveApplyStage ||
    status?.state === 'checking';

  const safeReleaseUrl = useMemo(() => getSafeHttpUrl(status?.release_url), [status?.release_url]);

  async function handleCheckNow() {
    if (checking || isUpdateBusy) return;
    setInlineError(null);
    try {
      let next: UpdateStatus | null = null;
      if (ctx.isProvided) {
        next = await ctx.checkNow();
      } else {
        setStandaloneChecking(true);
        try {
          next = await api.checkForUpdate();
          setStandaloneStatus(next);
        } finally {
          setStandaloneChecking(false);
        }
      }

      if (next) {
        if (next.last_error) {
          setInlineError(next.last_error);
          toast.error(next.last_error);
        } else if (next.update_available) {
          toast.info(`发现新版本 ${formatVersionLabel(next.latest_version)}`);
        } else {
          toast.success('当前已是最新版本');
        }
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : '检查更新失败';
      setInlineError(msg);
      toast.error(msg);
    }
  }

  async function handleSaveConfig(e: FormEvent) {
    e.preventDefault();
    if (savingConfig || !status) return;

    setConfigError(null);
    const trimmedMirror = mirrorUrl.trim();
    const mirrorErr = validateMirrorUrl(trimmedMirror);
    if (mirrorErr) {
      setConfigError(mirrorErr);
      toast.error(mirrorErr);
      return;
    }

    const parsedInterval = Number.parseInt(checkIntervalHours, 10);
    const intervalNum = Number.isNaN(parsedInterval) ? 6 : Math.max(1, Math.min(168, parsedInterval));

    const payload: UpdateConfigInput = {
      check_interval_hours: intervalNum,
      mirror_url: trimmedMirror,
    };
    if (!status.auto_check_locked_by_env) {
      payload.auto_check_enabled = autoCheckEnabled;
    }

    setSavingConfig(true);
    try {
      let updated: UpdateStatus | null = null;
      if (ctx.isProvided) {
        updated = await ctx.saveConfig(payload);
      } else {
        updated = await api.updateUpdateConfig(payload);
        setStandaloneStatus(updated);
      }
      if (updated) {
        syncFormFromStatus(updated);
      } else {
        setConfigDirty(false);
      }
      toast.success('更新配置已保存');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '保存更新配置失败';
      setConfigError(msg);
      toast.error(msg);
    } finally {
      setSavingConfig(false);
    }
  }

  async function handleConfirmApply() {
    if (submittingApply || !status?.can_apply) return;
    setModalError(null);
    setInlineError(null);
    setSubmittingApply(true);
    try {
      if (ctx.isProvided) {
        await ctx.startApply();
      } else {
        setStandaloneApplying(true);
        try {
          const next = await api.applyUpdate();
          setStandaloneStatus(next);
        } finally {
          setStandaloneApplying(false);
        }
      }
      setConfirmOpen(false);
      toast.info(`正在更新到 ${formatVersionLabel(status.latest_version)}...`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : '启动更新失败';
      setModalError(msg);
      toast.error(msg);
    } finally {
      setSubmittingApply(false);
    }
  }

  const deployModeLabel =
    status?.deploy_mode === 'docker' ? 'Docker 容器' : '本地二进制';
  const currentVersionLabel = formatVersionLabel(status?.current_version);
  const latestVersionLabel = formatVersionLabel(status?.latest_version);
  const progressPercent = clampPercent(status?.progress_percent ?? 0);
  const showProgressPanel = hasActiveApplyStage;
  const displayError =
    restartPhase === 'rolled_back' || restartPhase === 'timeout'
      ? inlineError
      : inlineError || (restartPhase === 'idle' ? restartError : null) || status?.last_error || null;

  return (
    <section
      id="system-update"
      className="rounded-3xl bg-card border border-subtle shadow-card p-6 space-y-5"
    >
      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-status-success-soft text-status-success-ink">
            <ArrowUpCircle className="h-4 w-4" aria-hidden="true" />
          </span>
          <div>
            <h2 className="text-base font-semibold text-ink-primary">系统版本与在线更新</h2>
            <p className="text-xs text-ink-muted">
              查看当前运行版本、自动检查 GitHub Releases 新版本并一键热更新。
            </p>
          </div>
        </div>

        <Tooltip content="立即向 GitHub Releases 检查是否有可用新版本">
          <button
            type="button"
            disabled={checking || isUpdateBusy || loading}
            onClick={() => void handleCheckNow()}
            className="inline-flex items-center gap-1.5 rounded-xl border border-default bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
          >
            {checking ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
            ) : (
              <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" />
            )}
            <span>{checking ? '检查中...' : '检查更新'}</span>
          </button>
        </Tooltip>
      </div>

      {/* Status metadata badges */}
      {loading && !status ? (
        <div className="flex items-center gap-2 text-xs text-ink-muted py-2">
          <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" aria-hidden="true" />
          <span>正在获取版本与更新状态...</span>
        </div>
      ) : !status ? (
        <p className="text-xs text-ink-muted">暂未获取到版本更新状态。</p>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="inline-flex items-center gap-1.5 rounded-xl border border-subtle bg-subtle px-2.5 py-1 text-ink-secondary">
              <span className="text-ink-muted">当前版本</span>
              <span className="font-mono font-semibold text-ink-primary">{currentVersionLabel}</span>
            </span>
            <span className="inline-flex items-center rounded-xl border border-accent-ring bg-accent-soft px-2.5 py-1 font-medium text-accent-ink">
              {deployModeLabel}
            </span>
            {status.platform && (
              <span className="inline-flex items-center rounded-xl border border-subtle bg-subtle px-2.5 py-1 font-mono text-ink-secondary">
                {status.platform}
              </span>
            )}
            <span className="text-ink-muted ml-auto">
              上次检查：{formatTimestamp(status.last_checked_at)}
            </span>
          </div>

          {/* Restart recovery banners */}
          {restartPhase === 'waiting' && (
            <div className="flex items-center gap-2.5 rounded-2xl border border-accent-ring bg-accent-soft px-4 py-3 text-xs font-medium text-accent-ink">
              <Loader2 className="h-4 w-4 shrink-0 animate-spin text-accent" aria-hidden="true" />
              <span>正在等待服务重启完成，新版本就绪后将自动刷新页面...</span>
            </div>
          )}

          {restartPhase === 'succeeded' && (
            <div className="flex items-center gap-2.5 rounded-2xl border border-status-success/30 bg-status-success-soft px-4 py-3 text-xs font-medium text-status-success-ink">
              <CheckCircle2 className="h-4 w-4 shrink-0 text-status-success-ink" aria-hidden="true" />
              <span>已更新至 {currentVersionLabel}，正在刷新页面...</span>
            </div>
          )}

          {restartPhase === 'rolled_back' && (
            <div className="flex items-start gap-2.5 rounded-2xl border border-status-danger/30 bg-status-danger-soft px-4 py-3 text-xs font-medium text-status-danger-ink">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-status-danger-ink" aria-hidden="true" />
              <span>{restartError || '新版本启动失败，已自动回滚至原版本'}</span>
            </div>
          )}

          {restartPhase === 'timeout' && (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-status-warning/30 bg-status-warning-soft px-4 py-3 text-xs font-medium text-status-warning-ink">
              <div className="flex items-center gap-2">
                <AlertTriangle className="h-4 w-4 shrink-0 text-status-warning-ink" aria-hidden="true" />
                <span>{restartError || '等待服务重启超时，请点击“立即刷新”检查服务状态。'}</span>
              </div>
              <button
                type="button"
                onClick={() => (onReload ?? defaultReload)()}
                className="inline-flex items-center gap-1.5 rounded-xl border border-status-warning/40 bg-card px-3 py-1.5 text-xs font-semibold text-status-warning-ink hover:bg-subtle transition-colors cursor-pointer"
              >
                <RotateCcw className="h-3.5 w-3.5" aria-hidden="true" />
                <span>立即刷新</span>
              </button>
            </div>
          )}

          {displayError && (
            <div className="flex items-start gap-2 rounded-2xl border border-status-danger/30 bg-status-danger-soft px-3.5 py-2.5 text-xs font-medium text-status-danger-ink">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-status-danger-ink" aria-hidden="true" />
              <span>{displayError}</span>
            </div>
          )}

          {/* Live download / verify / apply progress */}
          {showProgressPanel && (
            <div className="rounded-2xl border border-accent-ring bg-subtle p-4 space-y-2.5">
              <div className="flex items-center justify-between gap-2 text-xs">
                <div className="flex items-center gap-2 font-medium text-accent-ink">
                  <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" aria-hidden="true" />
                  <span>{getProgressStageLabel(status.state)}</span>
                  {(status.target_version || status.latest_version) && (
                    <span className="font-mono font-semibold text-status-success-ink">
                      {formatVersionLabel(status.target_version || status.latest_version)}
                    </span>
                  )}
                </div>
                <span className="font-mono font-semibold text-ink-primary">{progressPercent}%</span>
              </div>

              <div
                role="progressbar"
                aria-label="更新进度"
                aria-valuenow={progressPercent}
                aria-valuemin={0}
                aria-valuemax={100}
                className="h-2.5 w-full overflow-hidden rounded-full bg-muted"
              >
                <div
                  className="h-full rounded-full bg-gradient-to-r from-status-success to-accent transition-all duration-300"
                  style={{ width: `${progressPercent}%` }}
                />
              </div>

              {(status.state === 'downloading' || status.total_bytes > 0 || status.progress_bytes > 0) && (
                <div className="flex items-center justify-between text-[11px] text-ink-muted">
                  <span>{status.asset_name || '更新包'}</span>
                  <span className="font-mono">
                    {status.total_bytes > 0
                      ? `${formatMegabytes(status.progress_bytes)} / ${formatMegabytes(status.total_bytes)}`
                      : formatMegabytes(status.progress_bytes)}
                  </span>
                </div>
              )}
            </div>
          )}

          {/* Up-to-date vs Update available card */}
          {!status.update_available ? (
            !showProgressPanel && (
              <div className="flex items-center gap-2.5 rounded-2xl border border-subtle bg-subtle/70 px-4 py-3 text-xs text-ink-secondary">
                <CheckCircle2 className="h-4 w-4 shrink-0 text-status-success-ink" aria-hidden="true" />
                <span>当前已是最新版本（{currentVersionLabel}）。</span>
              </div>
            )
          ) : (
            <div className="rounded-2xl border border-status-success/30 bg-status-success-soft p-4 space-y-3.5">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-semibold text-ink-primary">
                      {status.release_name || `新版本 ${latestVersionLabel}`}
                    </span>
                    <span className="rounded-full border border-status-success/40 bg-card px-2.5 py-0.5 font-mono text-[11px] font-semibold text-status-success-ink">
                      {latestVersionLabel}
                    </span>
                  </div>
                  <div className="flex flex-wrap items-center gap-3 text-[11px] text-ink-secondary">
                    {status.published_at && (
                      <span>发布时间：{formatTimestamp(status.published_at)}</span>
                    )}
                    {status.asset_name && (
                      <span className="font-mono">
                        {status.asset_name}
                        {status.asset_size > 0 ? ` (${formatMegabytes(status.asset_size)})` : ''}
                      </span>
                    )}
                  </div>
                </div>

                {safeReleaseUrl && (
                  <a
                    href={safeReleaseUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-xs font-medium text-accent-ink hover:opacity-80 underline underline-offset-2"
                  >
                    <span>GitHub Releases</span>
                    <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
                  </a>
                )}
              </div>

              {status.release_notes && (
                <div className="rounded-2xl border border-subtle bg-card/85 p-3.5 max-h-52 overflow-y-auto">
                  <p className="text-[11px] font-medium text-ink-muted mb-1.5">
                    更新说明（Release Notes）
                  </p>
                  <pre className="whitespace-pre-wrap break-words font-sans text-xs leading-relaxed text-ink-primary">
                    {status.release_notes}
                  </pre>
                </div>
              )}

              {!status.can_apply && (
                <div className="flex items-start gap-2 rounded-2xl border border-status-warning/30 bg-status-warning-soft px-3.5 py-2.5 text-xs font-medium text-status-warning-ink">
                  <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-status-warning-ink" aria-hidden="true" />
                  <span>
                    {status.cannot_apply_reason ||
                      '当前环境不支持应用内一键更新，请前往 GitHub Releases 手动下载更新。'}
                  </span>
                </div>
              )}

              <div className="flex flex-wrap items-center justify-end gap-2.5">
                <Tooltip
                  content={
                    status.can_apply
                      ? `下载、校验 SHA-256 并热重启升级到 ${latestVersionLabel}`
                      : status.cannot_apply_reason || '当前环境不支持自动更新'
                  }
                >
                  <button
                    type="button"
                    disabled={!status.can_apply || isUpdateBusy}
                    onClick={() => {
                      setModalError(null);
                      setConfirmOpen(true);
                    }}
                    className="inline-flex items-center gap-1.5 rounded-xl bg-status-success px-4 py-2 text-xs font-semibold text-ink-inverse shadow-card hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed transition-all cursor-pointer"
                  >
                    <Download className="h-3.5 w-3.5" aria-hidden="true" />
                    <span>立即更新到 {latestVersionLabel}</span>
                  </button>
                </Tooltip>
              </div>
            </div>
          )}

          {/* Update preferences sub-form */}
          <form onSubmit={handleSaveConfig} className="border-t border-subtle pt-5 space-y-4">
            <h3 className="text-xs font-semibold text-ink-secondary">自动检查与加速代理设置</h3>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <label className="inline-flex items-center gap-2 text-xs font-medium text-ink-primary cursor-pointer">
                  <input
                    id="update-auto-check"
                    type="checkbox"
                    checked={autoCheckEnabled}
                    disabled={Boolean(status.auto_check_locked_by_env) || savingConfig || isUpdateBusy}
                    onChange={(e) => {
                      setAutoCheckEnabled(e.target.checked);
                      setConfigDirty(true);
                      setConfigError(null);
                    }}
                    className="h-4 w-4 rounded border-default bg-card text-accent focus:ring-2 focus:ring-accent-ring disabled:opacity-50"
                  />
                  <span>自动检查更新</span>
                </label>
                {status.auto_check_locked_by_env ? (
                  <p className="text-[11px] font-medium text-status-warning-ink">
                    已由环境变量 COURSEFIN_UPDATE_AUTO_CHECK 锁定，无法在此修改。
                  </p>
                ) : (
                  <p className="text-[11px] text-ink-muted">
                    开启后将在后台按设定周期静默检测新版本并提示。
                  </p>
                )}
              </div>

              <div className="space-y-1.5">
                <label
                  htmlFor="update-check-interval"
                  className="block text-xs font-medium text-ink-secondary"
                >
                  检查间隔
                </label>
                <select
                  id="update-check-interval"
                  value={checkIntervalHours}
                  disabled={savingConfig || isUpdateBusy}
                  onChange={(e) => {
                    setCheckIntervalHours(e.target.value);
                    setConfigDirty(true);
                    setConfigError(null);
                  }}
                  className={FIELD_INPUT_CLASS}
                >
                  {intervalOptions.map((hrs) => (
                    <option key={hrs} value={String(hrs)}>
                      {hrs} 小时
                    </option>
                  ))}
                </select>
              </div>

              <div className="space-y-1.5 sm:col-span-2">
                <label
                  htmlFor="update-mirror-url"
                  className="block text-xs font-medium text-ink-secondary"
                >
                  GitHub 加速代理前缀
                </label>
                <input
                  id="update-mirror-url"
                  type="text"
                  inputMode="url"
                  value={mirrorUrl}
                  disabled={savingConfig || isUpdateBusy}
                  onChange={(e) => {
                    setMirrorUrl(e.target.value);
                    setConfigDirty(true);
                    setConfigError(null);
                  }}
                  placeholder="https://gh-proxy.com"
                  className={FIELD_INPUT_CLASS}
                />
                <p className="text-[11px] text-ink-muted">
                  可选。仅填写可信加速代理地址（支持 http:// 或 https://），留空则直连 GitHub；失败时将自动回退直连。
                </p>
              </div>
            </div>

            {configError && (
              <div className="rounded-2xl border border-status-danger/30 bg-status-danger-soft px-3.5 py-2.5 text-xs font-medium text-status-danger-ink">
                {configError}
              </div>
            )}

            <div className="flex justify-end">
              <Tooltip content="保存自动检查间隔与 GitHub 加速代理配置">
                <button
                  type="submit"
                  disabled={savingConfig || isUpdateBusy}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-default bg-subtle px-4 py-2 text-xs font-semibold text-ink-primary hover:bg-muted disabled:opacity-50 transition-colors cursor-pointer"
                >
                  {savingConfig ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
                  ) : (
                    <Save className="h-3.5 w-3.5" aria-hidden="true" />
                  )}
                  <span>{savingConfig ? '保存中...' : '保存更新配置'}</span>
                </button>
              </Tooltip>
            </div>
          </form>
        </>
      )}

      {/* Confirmation Modal */}
      {confirmOpen && status && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-overlay backdrop-blur-sm p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby="system-update-confirm-title"
        >
          <div className="w-full max-w-md overflow-hidden rounded-3xl border border-subtle bg-popover shadow-float">
            <div className="flex items-center justify-between border-b border-subtle px-6 py-4">
              <div className="flex items-center gap-2.5">
                <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-status-success-soft text-status-success-ink">
                  <ArrowUpCircle className="h-4 w-4" aria-hidden="true" />
                </span>
                <h3 id="system-update-confirm-title" className="text-base font-bold text-ink-primary">
                  确认更新到 {latestVersionLabel}
                </h3>
              </div>

              <Tooltip content="关闭弹窗">
                <button
                  type="button"
                  aria-label="关闭"
                  disabled={submittingApply}
                  onClick={() => setConfirmOpen(false)}
                  className="rounded-lg p-1.5 text-ink-muted hover:bg-subtle hover:text-ink-primary transition-colors disabled:opacity-50 cursor-pointer"
                >
                  <X className="h-5 w-5" />
                </button>
              </Tooltip>
            </div>

            <div className="p-6 space-y-3.5">
              <p className="text-sm text-ink-secondary">
                即将把 CourseFin 从{' '}
                <strong className="font-mono text-ink-primary">{currentVersionLabel}</strong> 更新到{' '}
                <strong className="font-mono text-status-success-ink">{latestVersionLabel}</strong>（
                {deployModeLabel}）。
              </p>

              <div className="rounded-2xl border border-status-warning/30 bg-status-warning-soft p-3.5 text-xs leading-relaxed text-status-warning-ink">
                更新校验完成后服务将自动重启，正在进行的视频播放会短暂中断；您的全部课程目录、媒体库配置与观看进度将完整保留，若新版本启动异常会自动回滚。
              </div>

              {modalError && (
                <div className="rounded-2xl border border-status-danger/30 bg-status-danger-soft px-3.5 py-2.5 text-xs font-medium text-status-danger-ink">
                  {modalError}
                </div>
              )}
            </div>

            <div className="flex items-center justify-end gap-2.5 border-t border-subtle bg-subtle/40 px-6 py-3.5">
              <button
                type="button"
                disabled={submittingApply}
                onClick={() => setConfirmOpen(false)}
                className="rounded-xl border border-default bg-card px-4 py-2 text-xs font-medium text-ink-secondary hover:bg-subtle hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
              >
                取消
              </button>
              <button
                type="button"
                disabled={submittingApply}
                onClick={() => void handleConfirmApply()}
                className="inline-flex items-center gap-1.5 rounded-xl bg-status-success px-4 py-2 text-xs font-semibold text-ink-inverse shadow-card hover:opacity-90 disabled:opacity-50 transition-all cursor-pointer"
              >
                {submittingApply ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
                ) : (
                  <Download className="h-3.5 w-3.5" aria-hidden="true" />
                )}
                <span>{submittingApply ? '启动中...' : '确认更新'}</span>
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

export default SystemUpdateSection;
