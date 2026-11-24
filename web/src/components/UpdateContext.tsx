import type { ReactNode } from 'react';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, api, getToken } from '../api/client';
import type {
  HealthStatus,
  UpdateConfigInput,
  UpdateState,
  UpdateStatus,
} from '../types/coursefin';
import { useToast } from './Toast';

export type RestartPhase = 'idle' | 'waiting' | 'succeeded' | 'rolled_back' | 'timeout';

export interface UpdateContextValue {
  isProvided: boolean;
  status: UpdateStatus | null;
  loading: boolean;
  checking: boolean;
  applying: boolean;
  restartPhase: RestartPhase;
  restartError: string | null;
  refreshStatus: () => Promise<UpdateStatus | null>;
  checkNow: () => Promise<UpdateStatus | null>;
  saveConfig: (input: UpdateConfigInput) => Promise<UpdateStatus | null>;
  startApply: () => Promise<UpdateStatus | null>;
}

export interface UpdateProviderProps {
  children: ReactNode;
  pollIntervalMs?: number;
  healthPollIntervalMs?: number;
  restartTimeoutMs?: number;
  idlePollIntervalMs?: number;
  onReload?: () => void;
}

const DEFAULT_POLL_INTERVAL_MS = 800;
const DEFAULT_HEALTH_POLL_INTERVAL_MS = 1000;
const DEFAULT_RESTART_TIMEOUT_MS = 60000;
const DEFAULT_IDLE_POLL_INTERVAL_MS = 5 * 60 * 1000;

function defaultReload(): void {
  if (typeof window !== 'undefined' && window.location) {
    window.location.reload();
  }
}

function normalizeVersion(raw?: string | null): string {
  const trimmed = (raw ?? '').trim();
  return trimmed.replace(/^[vV](?=\d)/, '');
}

function isActiveUpdateState(state?: UpdateState | string | null): boolean {
  return (
    state === 'downloading' ||
    state === 'verifying' ||
    state === 'applying' ||
    state === 'restarting'
  );
}

function isRestartDisconnectError(err: unknown): boolean {
  if (err instanceof ApiError) {
    return err.status === 502 || err.status === 503;
  }
  return !(err instanceof ApiError);
}

const defaultUpdateContext: UpdateContextValue = {
  isProvided: false,
  status: null,
  loading: false,
  checking: false,
  applying: false,
  restartPhase: 'idle',
  restartError: null,
  refreshStatus: async () => null,
  checkNow: async () => null,
  saveConfig: async () => null,
  startApply: async () => null,
};

const UpdateContext = createContext<UpdateContextValue>(defaultUpdateContext);

/**
 * Access shared self-update status, controls, and restart recovery state.
 * Safe to invoke outside <UpdateProvider> for isolated component tests.
 */
export function useUpdate(): UpdateContextValue {
  return useContext(UpdateContext);
}

/**
 * Provides shared self-update state, periodic background checks, active apply polling,
 * and /health restart + rollback detection across the authenticated application.
 */
export function UpdateProvider({
  children,
  pollIntervalMs = DEFAULT_POLL_INTERVAL_MS,
  healthPollIntervalMs = DEFAULT_HEALTH_POLL_INTERVAL_MS,
  restartTimeoutMs = DEFAULT_RESTART_TIMEOUT_MS,
  idlePollIntervalMs = DEFAULT_IDLE_POLL_INTERVAL_MS,
  onReload = defaultReload,
}: UpdateProviderProps) {
  const { toast } = useToast();
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [checking, setChecking] = useState(false);
  const [applying, setApplying] = useState(false);
  const [restartPhase, setRestartPhase] = useState<RestartPhase>('idle');
  const [restartError, setRestartError] = useState<string | null>(null);

  const mountedRef = useRef(true);
  const statusRef = useRef<UpdateStatus | null>(null);
  const checkingRef = useRef(false);
  const applyingRef = useRef(false);
  const restartPhaseRef = useRef<RestartPhase>('idle');
  const refreshPromiseRef = useRef<Promise<UpdateStatus | null> | null>(null);

  const runIdRef = useRef(0);
  const initialBootIdRef = useRef<string | null>(null);
  const targetVersionRef = useRef<string>('');
  const sawDisconnectRef = useRef(false);

  const activePollTimerRef = useRef<number | null>(null);
  const healthPollTimerRef = useRef<number | null>(null);
  const restartTimeoutTimerRef = useRef<number | null>(null);

  const toastRef = useRef(toast);
  toastRef.current = toast;
  const onReloadRef = useRef(onReload);
  onReloadRef.current = onReload;

  statusRef.current = status;

  const clearActivePollTimer = useCallback(() => {
    if (activePollTimerRef.current !== null) {
      window.clearTimeout(activePollTimerRef.current);
      activePollTimerRef.current = null;
    }
  }, []);

  const clearHealthTimers = useCallback(() => {
    if (healthPollTimerRef.current !== null) {
      window.clearTimeout(healthPollTimerRef.current);
      healthPollTimerRef.current = null;
    }
    if (restartTimeoutTimerRef.current !== null) {
      window.clearTimeout(restartTimeoutTimerRef.current);
      restartTimeoutTimerRef.current = null;
    }
  }, []);

  const clearAllTimers = useCallback(() => {
    clearActivePollTimer();
    clearHealthTimers();
  }, [clearActivePollTimer, clearHealthTimers]);

  const completeRestartSuccess = useCallback(
    (targetVersion: string) => {
      runIdRef.current += 1;
      clearAllTimers();
      applyingRef.current = false;
      restartPhaseRef.current = 'succeeded';
      setApplying(false);
      setRestartPhase('succeeded');
      setRestartError(null);
      setStatus((prev) =>
        prev
          ? {
              ...prev,
              current_version: targetVersion,
              update_available: false,
              state: 'idle',
              target_version: '',
              last_error: '',
            }
          : prev,
      );
      toastRef.current.success(`已更新至 v${targetVersion}，正在刷新页面...`);
      onReloadRef.current();
    },
    [clearAllTimers],
  );

  const completeRestartRollback = useCallback(
    (rolledBackVersion: string) => {
      runIdRef.current += 1;
      clearAllTimers();
      applyingRef.current = false;
      restartPhaseRef.current = 'rolled_back';
      const message = rolledBackVersion
        ? `新版本启动失败，已自动回滚至 v${rolledBackVersion}`
        : '新版本启动失败，已自动回滚至原版本';
      setApplying(false);
      setRestartPhase('rolled_back');
      setRestartError(message);
      setStatus((prev) =>
        prev
          ? {
              ...prev,
              current_version: rolledBackVersion || prev.current_version,
              state: 'idle',
            }
          : prev,
      );
    },
    [clearAllTimers],
  );

  const evaluateHealthProbe = useCallback(
    (health: HealthStatus): boolean => {
      const target = targetVersionRef.current;
      const healthVer = normalizeVersion(health.version);
      const bootId = health.boot_id ?? '';
      const initialBootId = initialBootIdRef.current;

      if (target && healthVer === target) {
        completeRestartSuccess(target);
        return true;
      }

      if (initialBootId && bootId && bootId !== initialBootId && target && healthVer !== target) {
        completeRestartRollback(healthVer);
        return true;
      }

      if (!initialBootId && sawDisconnectRef.current && target && healthVer !== target) {
        completeRestartRollback(healthVer);
        return true;
      }

      if (!initialBootId && bootId) {
        initialBootIdRef.current = bootId;
      }
      return false;
    },
    [completeRestartRollback, completeRestartSuccess],
  );

  const scheduleHealthPoll = useCallback(
    (runId: number) => {
      if (healthPollTimerRef.current !== null) {
        window.clearTimeout(healthPollTimerRef.current);
      }
      healthPollTimerRef.current = window.setTimeout(async () => {
        healthPollTimerRef.current = null;
        if (
          !mountedRef.current ||
          runIdRef.current !== runId ||
          restartPhaseRef.current !== 'waiting'
        ) {
          return;
        }

        try {
          const health = await api.getHealth();
          if (
            !mountedRef.current ||
            runIdRef.current !== runId ||
            restartPhaseRef.current !== 'waiting'
          ) {
            return;
          }
          if (evaluateHealthProbe(health)) {
            return;
          }
        } catch {
          if (
            !mountedRef.current ||
            runIdRef.current !== runId ||
            restartPhaseRef.current !== 'waiting'
          ) {
            return;
          }
          sawDisconnectRef.current = true;
        }

        if (
          mountedRef.current &&
          runIdRef.current === runId &&
          restartPhaseRef.current === 'waiting'
        ) {
          scheduleHealthPoll(runId);
        }
      }, healthPollIntervalMs);
    },
    [evaluateHealthProbe, healthPollIntervalMs],
  );

  const enterRestartWaiting = useCallback(
    (runId: number) => {
      if (
        !mountedRef.current ||
        runIdRef.current !== runId ||
        restartPhaseRef.current === 'waiting'
      ) {
        return;
      }
      clearActivePollTimer();
      clearHealthTimers();

      restartPhaseRef.current = 'waiting';
      setRestartPhase('waiting');
      setRestartError(null);

      restartTimeoutTimerRef.current = window.setTimeout(() => {
        restartTimeoutTimerRef.current = null;
        if (
          !mountedRef.current ||
          runIdRef.current !== runId ||
          restartPhaseRef.current !== 'waiting'
        ) {
          return;
        }
        runIdRef.current += 1;
        clearHealthTimers();
        applyingRef.current = false;
        restartPhaseRef.current = 'timeout';
        setApplying(false);
        setRestartPhase('timeout');
        setRestartError('等待服务重启超时，请点击“立即刷新”检查服务状态。');
      }, restartTimeoutMs);

      scheduleHealthPoll(runId);
    },
    [clearActivePollTimer, clearHealthTimers, restartTimeoutMs, scheduleHealthPoll],
  );

  const scheduleActivePoll = useCallback(
    (runId: number) => {
      clearActivePollTimer();
      activePollTimerRef.current = window.setTimeout(async () => {
        activePollTimerRef.current = null;
        if (
          !mountedRef.current ||
          runIdRef.current !== runId ||
          restartPhaseRef.current !== 'idle'
        ) {
          return;
        }

        try {
          const next = await api.getUpdateStatus();
          if (
            !mountedRef.current ||
            runIdRef.current !== runId ||
            restartPhaseRef.current !== 'idle'
          ) {
            return;
          }

          setStatus(next);
          if (next.target_version) {
            targetVersionRef.current = normalizeVersion(next.target_version);
          } else if (!targetVersionRef.current && next.latest_version) {
            targetVersionRef.current = normalizeVersion(next.latest_version);
          }

          if (next.state === 'restarting') {
            enterRestartWaiting(runId);
            return;
          }

          if (
            next.state === 'downloading' ||
            next.state === 'verifying' ||
            next.state === 'applying'
          ) {
            scheduleActivePoll(runId);
            return;
          }

          if (next.state === 'failed') {
            applyingRef.current = false;
            setApplying(false);
            return;
          }

          if (next.state === 'idle') {
            const target = targetVersionRef.current;
            const current = normalizeVersion(next.current_version);
            if (target && current === target) {
              completeRestartSuccess(target);
              return;
            }
            try {
              const health = await api.getHealth();
              if (!mountedRef.current || runIdRef.current !== runId) {
                return;
              }
              if (evaluateHealthProbe(health)) {
                return;
              }
            } catch {
              if (!mountedRef.current || runIdRef.current !== runId) {
                return;
              }
              if (applyingRef.current) {
                sawDisconnectRef.current = true;
                enterRestartWaiting(runId);
                return;
              }
            }
          }

          applyingRef.current = false;
          setApplying(false);
        } catch (err) {
          if (!mountedRef.current || runIdRef.current !== runId) {
            return;
          }
          if (isRestartDisconnectError(err)) {
            sawDisconnectRef.current = true;
            enterRestartWaiting(runId);
            return;
          }
          applyingRef.current = false;
          setApplying(false);
          setRestartError(err instanceof Error ? err.message : '获取更新状态失败');
        }
      }, pollIntervalMs);
    },
    [
      clearActivePollTimer,
      completeRestartSuccess,
      enterRestartWaiting,
      evaluateHealthProbe,
      pollIntervalMs,
    ],
  );

  const refreshStatus = useCallback(async (): Promise<UpdateStatus | null> => {
    if (refreshPromiseRef.current) {
      return refreshPromiseRef.current;
    }

    const promise = (async (): Promise<UpdateStatus | null> => {
      const runIdAtStart = runIdRef.current;
      if (mountedRef.current) {
        setLoading(true);
      }
      try {
        const next = await api.getUpdateStatus();
        if (
          !mountedRef.current ||
          runIdRef.current !== runIdAtStart ||
          applyingRef.current ||
          restartPhaseRef.current !== 'idle'
        ) {
          return statusRef.current;
        }
        setStatus(next);

        if (isActiveUpdateState(next.state)) {
          const runId = ++runIdRef.current;
          targetVersionRef.current = normalizeVersion(
            next.target_version || next.latest_version || '',
          );
          applyingRef.current = true;
          setApplying(true);

          if (next.state === 'restarting') {
            enterRestartWaiting(runId);
          } else {
            if (!initialBootIdRef.current) {
              void api
                .getHealth()
                .then((health) => {
                  if (
                    mountedRef.current &&
                    runIdRef.current === runId &&
                    !initialBootIdRef.current
                  ) {
                    initialBootIdRef.current = health.boot_id || null;
                  }
                })
                .catch(() => {});
            }
            scheduleActivePoll(runId);
          }
        }

        return next;
      } catch {
        return null;
      } finally {
        refreshPromiseRef.current = null;
        if (mountedRef.current) {
          setLoading(false);
        }
      }
    })();

    refreshPromiseRef.current = promise;
    return promise;
  }, [enterRestartWaiting, scheduleActivePoll]);

  const checkNow = useCallback(async (): Promise<UpdateStatus | null> => {
    if (checkingRef.current) {
      return statusRef.current;
    }
    checkingRef.current = true;
    setChecking(true);
    try {
      const next = await api.checkForUpdate();
      if (!mountedRef.current) {
        return null;
      }
      setStatus(next);
      return next;
    } finally {
      checkingRef.current = false;
      if (mountedRef.current) {
        setChecking(false);
      }
    }
  }, []);

  const saveConfig = useCallback(async (input: UpdateConfigInput): Promise<UpdateStatus | null> => {
    const next = await api.updateUpdateConfig(input);
    if (!mountedRef.current) {
      return null;
    }
    setStatus(next);
    return next;
  }, []);

  const startApply = useCallback(async (): Promise<UpdateStatus | null> => {
    if (applyingRef.current || restartPhaseRef.current === 'waiting') {
      return statusRef.current;
    }

    const runId = ++runIdRef.current;
    clearAllTimers();
    sawDisconnectRef.current = false;
    applyingRef.current = true;
    restartPhaseRef.current = 'idle';
    setApplying(true);
    setRestartPhase('idle');
    setRestartError(null);

    try {
      const health = await api.getHealth();
      if (!mountedRef.current || runIdRef.current !== runId) {
        return null;
      }
      initialBootIdRef.current = health.boot_id || null;
    } catch {
      if (!mountedRef.current || runIdRef.current !== runId) {
        return null;
      }
      initialBootIdRef.current = null;
    }

    let next: UpdateStatus;
    try {
      next = await api.applyUpdate();
    } catch (err) {
      if (mountedRef.current && runIdRef.current === runId) {
        applyingRef.current = false;
        setApplying(false);
      }
      throw err;
    }

    if (!mountedRef.current || runIdRef.current !== runId) {
      return next;
    }

    setStatus(next);
    targetVersionRef.current = normalizeVersion(
      next.target_version || next.latest_version || statusRef.current?.latest_version || '',
    );

    if (next.state === 'restarting') {
      enterRestartWaiting(runId);
    } else if (next.state === 'failed') {
      applyingRef.current = false;
      setApplying(false);
    } else {
      scheduleActivePoll(runId);
    }

    return next;
  }, [clearAllTimers, enterRestartWaiting, scheduleActivePoll]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      runIdRef.current += 1;
      clearAllTimers();
    };
  }, [clearAllTimers]);

  useEffect(() => {
    if (getToken()) {
      void refreshStatus();
    }
  }, [refreshStatus]);

  useEffect(() => {
    if (restartPhase !== 'idle' || applying || isActiveUpdateState(status?.state)) {
      return;
    }
    if (!getToken() && !status) {
      return;
    }
    const timer = window.setInterval(() => {
      if (!getToken() || applyingRef.current || restartPhaseRef.current !== 'idle') {
        return;
      }
      void refreshStatus();
    }, idlePollIntervalMs);
    return () => {
      window.clearInterval(timer);
    };
  }, [applying, idlePollIntervalMs, refreshStatus, restartPhase, status]);

  const value = useMemo<UpdateContextValue>(
    () => ({
      isProvided: true,
      status,
      loading,
      checking,
      applying,
      restartPhase,
      restartError,
      refreshStatus,
      checkNow,
      saveConfig,
      startApply,
    }),
    [
      applying,
      checkNow,
      checking,
      loading,
      refreshStatus,
      restartError,
      restartPhase,
      saveConfig,
      startApply,
      status,
    ],
  );

  return <UpdateContext.Provider value={value}>{children}</UpdateContext.Provider>;
}
