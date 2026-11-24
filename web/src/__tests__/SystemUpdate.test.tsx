import { StrictMode, useState } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { api } from '../api/client';
import Layout from '../components/Layout';
import { ToastProvider } from '../components/Toast';
import { UpdateProvider, useUpdate } from '../components/UpdateContext';
import SettingsPage from '../pages/SettingsPage';
import type { UpdateStatus } from '../types/coursefin';

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeStatus(overrides: Partial<UpdateStatus> = {}): UpdateStatus {
  return {
    current_version: '1.0.1',
    latest_version: '1.0.2',
    update_available: true,
    release_name: 'CourseFin v1.0.2',
    release_notes: 'Bug fixes and improvements',
    release_url: 'https://github.com/booooodv/CourseFin/releases/tag/v1.0.2',
    published_at: '2026-11-24T12:00:00Z',
    asset_name: 'coursefin_1.0.2_linux_amd64.tar.gz',
    asset_size: 14820352,
    deploy_mode: 'docker',
    platform: 'linux/amd64',
    can_apply: true,
    cannot_apply_reason: '',
    auto_check_enabled: true,
    auto_check_locked_by_env: false,
    check_interval_hours: 6,
    mirror_url: '',
    state: 'idle',
    target_version: '',
    progress_bytes: 0,
    total_bytes: 0,
    progress_percent: 0,
    last_checked_at: '2026-11-24T14:30:00Z',
    last_error: '',
    ...overrides,
  };
}

function UpdateHarness() {
  const {
    isProvided,
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
  } = useUpdate();
  const [actionResult, setActionResult] = useState<string>('unset');

  return (
    <div>
      <div data-testid="is-provided">{String(isProvided)}</div>
      <div data-testid="loading">{String(loading)}</div>
      <div data-testid="checking">{String(checking)}</div>
      <div data-testid="applying">{String(applying)}</div>
      <div data-testid="current-version">{status?.current_version ?? 'none'}</div>
      <div data-testid="latest-version">{status?.latest_version ?? 'none'}</div>
      <div data-testid="state">{status?.state ?? 'none'}</div>
      <div data-testid="progress">{status?.progress_percent ?? 0}</div>
      <div data-testid="mirror">{status?.mirror_url ?? ''}</div>
      <div data-testid="restart-phase">{restartPhase}</div>
      <div data-testid="restart-error">{restartError ?? ''}</div>
      <div data-testid="action-result">{actionResult}</div>
      <button
        type="button"
        onClick={async () => {
          const res = await refreshStatus();
          setActionResult(res ? `refresh:${res.state}` : 'null');
        }}
      >
        刷新状态
      </button>
      <button
        type="button"
        onClick={async () => {
          const res = await checkNow();
          setActionResult(res ? `check:${res.latest_version}` : 'null');
        }}
      >
        检查更新
      </button>
      <button
        type="button"
        onClick={async () => {
          const res = await saveConfig({
            auto_check_enabled: false,
            check_interval_hours: 12,
            mirror_url: 'https://gh-proxy.com',
          });
          setActionResult(res ? `config:${res.mirror_url}` : 'null');
        }}
      >
        保存配置
      </button>
      <button
        type="button"
        onClick={async () => {
          const res = await startApply();
          setActionResult(res ? `apply:${res.state}` : 'null');
        }}
      >
        开始更新
      </button>
    </div>
  );
}

describe('UpdateContext & Update API client', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('provides safe standalone fallback when useUpdate is called outside UpdateProvider', async () => {
    const fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);

    render(<UpdateHarness />);

    expect(screen.getByTestId('is-provided')).toHaveTextContent('false');
    expect(screen.getByTestId('current-version')).toHaveTextContent('none');
    expect(screen.getByTestId('restart-phase')).toHaveTextContent('idle');
    expect(screen.getByTestId('restart-error')).toHaveTextContent('');

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '刷新状态' }));
    expect(screen.getByTestId('action-result')).toHaveTextContent('null');

    await user.click(screen.getByRole('button', { name: '检查更新' }));
    expect(screen.getByTestId('action-result')).toHaveTextContent('null');

    await user.click(screen.getByRole('button', { name: '保存配置' }));
    expect(screen.getByTestId('action-result')).toHaveTextContent('null');

    await user.click(screen.getByRole('button', { name: '开始更新' }));
    expect(screen.getByTestId('action-result')).toHaveTextContent('null');

    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('does not auto-fetch on mount when unauthenticated and getHealth skips auth redirect with no-store', async () => {
    localStorage.clear();
    const calls: Array<{ url: string; init?: RequestInit }> = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        calls.push({ url, init });
        if (url === '/health') {
          return jsonResponse({ error: 'unauthorized' }, 401);
        }
        return jsonResponse(makeStatus());
      }),
    );

    render(
      <UpdateProvider>
        <UpdateHarness />
      </UpdateProvider>,
    );

    await new Promise((r) => setTimeout(r, 30));
    expect(calls).toHaveLength(0);

    localStorage.setItem('coursefin_token', 'keep-me');
    await expect(api.getHealth()).rejects.toThrow();
    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe('/health');
    expect(calls[0].init?.cache).toBe('no-store');
    expect(localStorage.getItem('coursefin_token')).toBe('keep-me');
  });

  it('fetches initial status, runs startApply() restart recovery, normalizes v prefix, and invokes onReload', async () => {
    const callOrder: string[] = [];
    let statusPollCount = 0;
    let healthCallCount = 0;
    const onReload = vi.fn();

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        callOrder.push(`${method} ${url}`);

        if (url === '/api/v1/system/update' && method === 'GET') {
          statusPollCount += 1;
          if (statusPollCount === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          if (statusPollCount === 2) {
            return jsonResponse(
              makeStatus({
                state: 'downloading',
                target_version: '1.0.2',
                progress_bytes: 7000000,
                total_bytes: 14820352,
                progress_percent: 47,
              }),
            );
          }
          return jsonResponse(
            makeStatus({
              state: 'restarting',
              target_version: '1.0.2',
              progress_percent: 100,
            }),
          );
        }

        if (url === '/health' && method === 'GET') {
          healthCallCount += 1;
          if (healthCallCount === 1) {
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-old-1' });
          }
          if (healthCallCount === 2) {
            throw new TypeError('Failed to fetch');
          }
          return jsonResponse({ status: 'ok', version: 'v1.0.2', boot_id: 'boot-new-2' });
        }

        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({
              state: 'downloading',
              target_version: '1.0.2',
              progress_percent: 0,
            }),
            202,
          );
        }

        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <UpdateProvider
          pollIntervalMs={15}
          healthPollIntervalMs={15}
          restartTimeoutMs={500}
          onReload={onReload}
        >
          <UpdateHarness />
        </UpdateProvider>
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.1');
    });
    expect(screen.getByTestId('is-provided')).toHaveTextContent('true');
    expect(screen.getByTestId('latest-version')).toHaveTextContent('1.0.2');

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '开始更新' }));

    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('succeeded');
    });

    expect(onReload).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('applying')).toHaveTextContent('false');
    expect(screen.getByTestId('restart-error')).toHaveTextContent('');
    expect(await screen.findByText('已更新至 v1.0.2，正在刷新页面...')).toBeInTheDocument();

    const firstHealthIdx = callOrder.indexOf('GET /health');
    const applyIdx = callOrder.indexOf('POST /api/v1/system/update/apply');
    expect(firstHealthIdx).toBeGreaterThan(-1);
    expect(applyIdx).toBeGreaterThan(firstHealthIdx);
  });

  it('transitions to /health polling when status poll hits TypeError or HTTP 502/503 during active apply', async () => {
    let statusPollCount = 0;
    let healthCallCount = 0;
    const onReload = vi.fn();

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          statusPollCount += 1;
          if (statusPollCount === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          if (statusPollCount === 2) {
            return jsonResponse(
              makeStatus({
                state: 'applying',
                target_version: '1.0.2',
                progress_percent: 100,
              }),
            );
          }
          throw new TypeError('Failed to fetch');
        }

        if (url === '/health' && method === 'GET') {
          healthCallCount += 1;
          if (healthCallCount === 1) {
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
          }
          if (healthCallCount === 2) {
            return jsonResponse({ error: 'bad gateway' }, 503);
          }
          return jsonResponse({ status: 'ok', version: '1.0.2', boot_id: 'boot-2' });
        }

        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({ state: 'downloading', target_version: '1.0.2' }),
            202,
          );
        }

        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <UpdateProvider
        pollIntervalMs={15}
        healthPollIntervalMs={15}
        restartTimeoutMs={500}
        onReload={onReload}
      >
        <UpdateHarness />
      </UpdateProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.1');
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '开始更新' }));

    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('succeeded');
    });
    expect(onReload).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('restart-error')).toHaveTextContent('');
  });

  it('detects automatic rollback when /health returns a new boot_id with old version and does not reload', async () => {
    let statusPollCount = 0;
    let healthCallCount = 0;
    const onReload = vi.fn();

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          statusPollCount += 1;
          if (statusPollCount === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          return jsonResponse({ error: 'bad gateway' }, 502);
        }

        if (url === '/health' && method === 'GET') {
          healthCallCount += 1;
          if (healthCallCount === 1) {
            // Initial boot_id captured before apply
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-old-1' });
          }
          if (healthCallCount === 2) {
            // Old process still finishing shutdown: same boot_id -> keep waiting
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-old-1' });
          }
          // New boot_id with old version -> automatic rollback happened
          return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-rollback-2' });
        }

        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({ state: 'downloading', target_version: '1.0.2' }),
            202,
          );
        }

        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <UpdateProvider
        pollIntervalMs={15}
        healthPollIntervalMs={15}
        restartTimeoutMs={500}
        onReload={onReload}
      >
        <UpdateHarness />
      </UpdateProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.1');
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '开始更新' }));

    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('rolled_back');
    });

    expect(onReload).not.toHaveBeenCalled();
    expect(screen.getByTestId('applying')).toHaveTextContent('false');
    expect(screen.getByTestId('restart-error').textContent).toMatch(/回滚.*1\.0\.1/);
  });

  it('transitions to timeout when /health does not recover within restartTimeoutMs', async () => {
    let statusPollCount = 0;
    let healthCallCount = 0;
    const onReload = vi.fn();

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          statusPollCount += 1;
          if (statusPollCount === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          return jsonResponse(makeStatus({ state: 'restarting', target_version: '1.0.2' }));
        }

        if (url === '/health' && method === 'GET') {
          healthCallCount += 1;
          if (healthCallCount === 1) {
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
          }
          throw new TypeError('Failed to fetch');
        }

        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({ state: 'downloading', target_version: '1.0.2' }),
            202,
          );
        }

        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <UpdateProvider
        pollIntervalMs={15}
        healthPollIntervalMs={15}
        restartTimeoutMs={90}
        onReload={onReload}
      >
        <UpdateHarness />
      </UpdateProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.1');
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '开始更新' }));

    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('timeout');
    });

    expect(onReload).not.toHaveBeenCalled();
    expect(screen.getByTestId('applying')).toHaveTextContent('false');
    expect(screen.getByTestId('restart-error').textContent).toMatch(/超时/);
  });

  it('supports checkNow and saveConfig updating shared status', async () => {
    const configBodies: unknown[] = [];

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          return jsonResponse(makeStatus({ latest_version: '1.0.1', update_available: false }));
        }
        if (url === '/api/v1/system/update/check' && method === 'POST') {
          return jsonResponse(makeStatus({ latest_version: '1.0.3', update_available: true }));
        }
        if (url === '/api/v1/system/update/config' && method === 'PUT') {
          const body = JSON.parse(String(init?.body));
          configBodies.push(body);
          return jsonResponse(
            makeStatus({
              auto_check_enabled: body.auto_check_enabled,
              check_interval_hours: body.check_interval_hours,
              mirror_url: body.mirror_url,
            }),
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <UpdateProvider>
        <UpdateHarness />
      </UpdateProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('latest-version')).toHaveTextContent('1.0.1');
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '检查更新' }));
    await waitFor(() => {
      expect(screen.getByTestId('latest-version')).toHaveTextContent('1.0.3');
    });

    await user.click(screen.getByRole('button', { name: '保存配置' }));
    await waitFor(() => {
      expect(screen.getByTestId('mirror')).toHaveTextContent('https://gh-proxy.com');
    });
    expect(configBodies).toEqual([
      {
        auto_check_enabled: false,
        check_interval_hours: 12,
        mirror_url: 'https://gh-proxy.com',
      },
    ]);
  });

  it('transitions to /health on HTTP 503 during status poll and handles backend failed state cleanly', async () => {
    let statusPollCount = 0;
    let healthCallCount = 0;
    const onReload = vi.fn();

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          statusPollCount += 1;
          if (statusPollCount === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          if (statusPollCount === 2) {
            return jsonResponse({ error: 'service unavailable' }, 503);
          }
          return jsonResponse(makeStatus({ state: 'failed', last_error: 'checksum mismatch' }));
        }
        if (url === '/health' && method === 'GET') {
          healthCallCount += 1;
          if (healthCallCount === 1) {
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
          }
          return jsonResponse({ status: 'ok', version: '1.0.2', boot_id: 'boot-2' });
        }
        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({ state: 'downloading', target_version: '1.0.2' }),
            202,
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <UpdateProvider
        pollIntervalMs={15}
        healthPollIntervalMs={15}
        restartTimeoutMs={500}
        onReload={onReload}
      >
        <UpdateHarness />
      </UpdateProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.1');
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '开始更新' }));

    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('succeeded');
    });
    expect(onReload).toHaveBeenCalledTimes(1);
  });

  it('stops polling without entering restart waiting when backend transitions to failed', async () => {
    let statusPollCount = 0;
    const onReload = vi.fn();

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          statusPollCount += 1;
          if (statusPollCount === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          return jsonResponse(
            makeStatus({
              state: 'failed',
              target_version: '1.0.2',
              last_error: 'checksum mismatch',
            }),
          );
        }
        if (url === '/health' && method === 'GET') {
          return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
        }
        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({ state: 'downloading', target_version: '1.0.2' }),
            202,
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <UpdateProvider
        pollIntervalMs={15}
        healthPollIntervalMs={15}
        restartTimeoutMs={500}
        onReload={onReload}
      >
        <UpdateHarness />
      </UpdateProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.1');
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '开始更新' }));

    await waitFor(() => {
      expect(screen.getByTestId('state')).toHaveTextContent('failed');
    });
    expect(screen.getByTestId('applying')).toHaveTextContent('false');
    expect(screen.getByTestId('restart-phase')).toHaveTextContent('idle');
    expect(onReload).not.toHaveBeenCalled();
  });

  it('resets applying and rethrows when POST /api/v1/system/update/apply fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          return jsonResponse(makeStatus({ state: 'idle' }));
        }
        if (url === '/health' && method === 'GET') {
          return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
        }
        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse({ error: 'updater is busy' }, 409);
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    function ErrorCatchHarness() {
      const { applying, restartPhase, startApply } = useUpdate();
      const [errMsg, setErrMsg] = useState('');
      return (
        <div>
          <div data-testid="applying-err">{String(applying)}</div>
          <div data-testid="phase-err">{restartPhase}</div>
          <div data-testid="err-msg">{errMsg}</div>
          <button
            type="button"
            onClick={async () => {
              try {
                await startApply();
              } catch (err) {
                setErrMsg(err instanceof Error ? err.message : 'unknown');
              }
            }}
          >
            触发更新
          </button>
        </div>
      );
    }

    render(
      <UpdateProvider>
        <ErrorCatchHarness />
      </UpdateProvider>,
    );

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '触发更新' }));

    await waitFor(() => {
      expect(screen.getByTestId('err-msg')).toHaveTextContent('updater is busy');
    });
    expect(screen.getByTestId('applying-err')).toHaveTextContent('false');
    expect(screen.getByTestId('phase-err')).toHaveTextContent('idle');
  });

  it('loads initial authenticated status cleanly inside React StrictMode', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';
        if (url === '/api/v1/system/update' && method === 'GET') {
          return jsonResponse(makeStatus({ current_version: '1.0.1', latest_version: '1.0.2' }));
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <StrictMode>
        <UpdateProvider>
          <UpdateHarness />
        </UpdateProvider>
      </StrictMode>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.1');
    });
    expect(screen.getByTestId('latest-version')).toHaveTextContent('1.0.2');
    expect(screen.getByTestId('loading')).toHaveTextContent('false');
  });

  it('allows refreshStatus() to update status after timeout or rolled_back', async () => {
    let phase: 'timeout_run' | 'after_timeout' | 'rollback_run' | 'after_rollback' = 'timeout_run';
    let statusPolls = 0;
    let healthPolls = 0;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          statusPolls += 1;
          if (phase === 'timeout_run') {
            if (statusPolls === 1) {
              return jsonResponse(makeStatus({ state: 'idle' }));
            }
            return jsonResponse(makeStatus({ state: 'restarting', target_version: '1.0.2' }));
          }
          if (phase === 'after_timeout') {
            return jsonResponse(
              makeStatus({
                current_version: '1.0.2',
                latest_version: '1.0.2',
                update_available: false,
                state: 'idle',
              }),
            );
          }
          if (phase === 'rollback_run') {
            return jsonResponse({ error: 'bad gateway' }, 502);
          }
          return jsonResponse(
            makeStatus({
              current_version: '1.0.1',
              latest_version: '1.0.3',
              update_available: true,
              state: 'idle',
            }),
          );
        }

        if (url === '/health' && method === 'GET') {
          healthPolls += 1;
          if (phase === 'timeout_run') {
            if (healthPolls === 1) {
              return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
            }
            throw new TypeError('Failed to fetch');
          }
          if (phase === 'rollback_run') {
            if (healthPolls === 2) {
              return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
            }
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-2' });
          }
          return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-2' });
        }

        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({ state: 'downloading', target_version: '1.0.2' }),
            202,
          );
        }

        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <UpdateProvider pollIntervalMs={15} healthPollIntervalMs={15} restartTimeoutMs={70}>
        <UpdateHarness />
      </UpdateProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.1');
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '开始更新' }));

    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('timeout');
    });

    phase = 'after_timeout';
    await user.click(screen.getByRole('button', { name: '刷新状态' }));
    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.2');
    });
    expect(screen.getByTestId('action-result')).toHaveTextContent('refresh:idle');

    // Now test refreshStatus() after rolled_back
    phase = 'rollback_run';
    healthPolls = 1;
    await user.click(screen.getByRole('button', { name: '开始更新' }));
    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('rolled_back');
    });

    phase = 'after_rollback';
    await user.click(screen.getByRole('button', { name: '刷新状态' }));
    await waitFor(() => {
      expect(screen.getByTestId('latest-version')).toHaveTextContent('1.0.3');
    });
    expect(screen.getByTestId('action-result')).toHaveTextContent('refresh:idle');
  });

  it('does not let a late pre-restart /health probe overwrite initialBootIdRef when resuming an active update', async () => {
    let statusPolls = 0;
    let healthCalls = 0;
    let resolveLateHealth!: (res: Response) => void;
    const lateHealthPromise = new Promise<Response>((resolve) => {
      resolveLateHealth = resolve;
    });
    const onReload = vi.fn();

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          statusPolls += 1;
          if (statusPolls === 1) {
            return jsonResponse(
              makeStatus({ state: 'downloading', target_version: '1.0.2', progress_percent: 50 }),
            );
          }
          return jsonResponse({ error: 'bad gateway' }, 502);
        }

        if (url === '/health' && method === 'GET') {
          healthCalls += 1;
          if (healthCalls === 1) {
            return lateHealthPromise;
          }
          return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-rolled-back-2' });
        }

        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <UpdateProvider
        pollIntervalMs={15}
        healthPollIntervalMs={30}
        restartTimeoutMs={150}
        onReload={onReload}
      >
        <UpdateHarness />
      </UpdateProvider>,
    );

    // Wait until status poll hits 502 and transitions to restartPhase === 'waiting'
    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('waiting');
    });

    // Late pre-restart /health resolves AFTER restart waiting already started,
    // returning the new rolled-back boot_id. It must NOT overwrite initialBootIdRef.
    resolveLateHealth(
      jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-rolled-back-2' }),
    );

    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('rolled_back');
    });
    expect(onReload).not.toHaveBeenCalled();
    expect(screen.getByTestId('restart-error').textContent).toMatch(/回滚.*1\.0\.1/);
  });

  it('clears restartError on saveConfig and on refreshStatus only when restartPhase is idle', async () => {
    let mode: 'idle_err' | 'timeout_err' | 'after_timeout' = 'idle_err';
    let statusPolls = 0;
    let healthPolls = 0;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url === '/api/v1/system/update' && method === 'GET') {
          statusPolls += 1;
          if (statusPolls === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          if (mode === 'idle_err' && statusPolls === 2) {
            return jsonResponse({ error: 'poll failed 500' }, 500);
          }
          if (mode === 'timeout_err') {
            return jsonResponse(makeStatus({ state: 'restarting', target_version: '1.0.2' }));
          }
          if (mode === 'after_timeout') {
            return jsonResponse(makeStatus({ state: 'idle', latest_version: '1.0.3' }));
          }
          return jsonResponse(makeStatus({ state: 'idle', latest_version: '1.0.2' }));
        }
        if (url === '/health' && method === 'GET') {
          healthPolls += 1;
          if (mode === 'timeout_err' && healthPolls > 1) {
            throw new TypeError('Failed to fetch');
          }
          return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
        }
        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(makeStatus({ state: 'downloading', target_version: '1.0.2' }), 202);
        }
        if (url === '/api/v1/system/update/config' && method === 'PUT') {
          const body = JSON.parse(String(init?.body));
          return jsonResponse(
            makeStatus({
              state: 'idle',
              auto_check_enabled: body.auto_check_enabled,
              check_interval_hours: body.check_interval_hours,
              mirror_url: body.mirror_url,
            }),
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <UpdateProvider pollIntervalMs={15} healthPollIntervalMs={15} restartTimeoutMs={70}>
        <UpdateHarness />
      </UpdateProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('current-version')).toHaveTextContent('1.0.1');
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '开始更新' }));

    await waitFor(() => {
      expect(screen.getByTestId('restart-error')).toHaveTextContent('poll failed 500');
    });
    expect(screen.getByTestId('restart-phase')).toHaveTextContent('idle');
    expect(screen.getByTestId('applying')).toHaveTextContent('false');

    // saveConfig clears restartError
    await user.click(screen.getByRole('button', { name: '保存配置' }));
    await waitFor(() => {
      expect(screen.getByTestId('mirror')).toHaveTextContent('https://gh-proxy.com');
    });
    expect(screen.getByTestId('restart-error')).toHaveTextContent('');

    // Trigger another idle poll 500 error, then refreshStatus clears restartError
    statusPolls = 1;
    await user.click(screen.getByRole('button', { name: '开始更新' }));
    await waitFor(() => {
      expect(screen.getByTestId('restart-error')).toHaveTextContent('poll failed 500');
    });
    await user.click(screen.getByRole('button', { name: '刷新状态' }));
    await waitFor(() => {
      expect(screen.getByTestId('restart-error')).toHaveTextContent('');
    });

    // In timeout phase, refreshStatus preserves restartError when backend reports idle
    mode = 'timeout_err';
    healthPolls = 0;
    await user.click(screen.getByRole('button', { name: '开始更新' }));
    await waitFor(() => {
      expect(screen.getByTestId('restart-phase')).toHaveTextContent('timeout');
    });
    const timeoutErr = screen.getByTestId('restart-error').textContent;
    expect(timeoutErr).toMatch(/超时/);

    mode = 'after_timeout';
    await user.click(screen.getByRole('button', { name: '刷新状态' }));
    await waitFor(() => {
      expect(screen.getByTestId('latest-version')).toHaveTextContent('1.0.3');
    });
    expect(screen.getByTestId('restart-error')).toHaveTextContent(timeoutErr!);
  });
});

function makeSettingsResponse() {
  return {
    server_name: 'CourseFin Studio',
    scan_interval_minutes: 60,
    base_url_hint: 'http://192.168.1.10:8096',
    jellyfin_username: 'admin',
    ai_base_url: 'https://api.openai.com/v1',
    ai_api_key: '',
    ai_api_key_set: false,
    ai_model: 'gpt-4o-mini',
    ai_max_subtitle_chars: 4000,
  };
}

function makeMeResponse() {
  return {
    user: { id: 'u1', username: 'admin', created_at: '', updated_at: '' },
    session: {
      token: 'test-token',
      user_id: 'u1',
      device_id: 'web',
      device_name: 'Web',
      client_name: 'CourseFin Web',
      client_version: '1.0.1',
      last_active_at: '',
      created_at: '',
    },
  };
}

function LocationProbe() {
  const location = useLocation();
  return (
    <div data-testid="location-probe">
      {location.pathname}
      {location.hash}
    </div>
  );
}

describe('SystemUpdate UI — Layout & SettingsPage', () => {
  const originalScrollIntoView = Element.prototype.scrollIntoView;

  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'test-token');
    Element.prototype.scrollIntoView = vi.fn();
  });

  afterEach(() => {
    localStorage.clear();
    Element.prototype.scrollIntoView = originalScrollIntoView;
    delete (window as unknown as { __xss?: unknown }).__xss;
    vi.restoreAllMocks();
  });

  it('Layout hides update pill when update_available is false and shows clickable pill + mobile dot navigating to /settings#system-update when true', async () => {
    let available = false;
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/v1/system/update') {
          return jsonResponse(
            makeStatus({
              update_available: available,
              latest_version: available ? '1.0.2' : '1.0.1',
            }),
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const firstRender = render(
      <UpdateProvider>
        <MemoryRouter initialEntries={['/']}>
          <Layout>
            <div>首页内容</div>
          </Layout>
        </MemoryRouter>
      </UpdateProvider>,
    );

    await screen.findByText('首页内容');
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByRole('link', { name: /新版本/ })).not.toBeInTheDocument();
    expect(screen.queryByTestId('mobile-update-dot')).not.toBeInTheDocument();
    firstRender.unmount();

    available = true;
    render(
      <UpdateProvider>
        <MemoryRouter initialEntries={['/']}>
          <Routes>
            <Route
              path="/"
              element={
                <Layout>
                  <div>首页内容</div>
                </Layout>
              }
            />
            <Route
              path="/settings"
              element={
                <Layout>
                  <LocationProbe />
                </Layout>
              }
            />
          </Routes>
        </MemoryRouter>
      </UpdateProvider>,
    );

    const pill = await screen.findByRole('link', { name: /新版本 v1\.0\.2/ });
    expect(pill).toHaveAttribute('href', '/settings#system-update');
    expect(screen.getByTestId('mobile-update-dot')).toBeInTheDocument();
    // Existing nav link accessible names remain intact for HomePage tests
    expect(screen.getAllByRole('link', { name: '设置' }).length).toBeGreaterThanOrEqual(1);

    const user = userEvent.setup();
    await user.hover(pill);
    await waitFor(() => {
      expect(screen.getByRole('tooltip')).toHaveTextContent(
        '发现新版本 v1.0.2，点击前往设置页查看并更新',
      );
    });

    await user.click(pill);
    expect(await screen.findByTestId('location-probe')).toHaveTextContent('/settings#system-update');
  });

  it('SettingsPage renders SystemUpdateSection with deploy mode, safe plain-text release notes, and manual check button', async () => {
    let checkCalled = false;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        if (url === '/api/v1/system/update' && method === 'GET') {
          return jsonResponse(
            makeStatus({
              current_version: '1.0.1',
              latest_version: '1.0.1',
              update_available: false,
              deploy_mode: 'docker',
              platform: 'linux/amd64',
            }),
          );
        }
        if (url === '/api/v1/system/update/check' && method === 'POST') {
          checkCalled = true;
          return jsonResponse(
            makeStatus({
              current_version: '1.0.1',
              latest_version: '1.0.2',
              update_available: true,
              deploy_mode: 'docker',
              platform: 'linux/amd64',
              release_notes:
                '<script>window.__xss=1</script><img src=x onerror="window.__xss=2">\n修复播放器与导入问题',
              release_url: 'https://github.com/booooodv/CourseFin/releases/tag/v1.0.2',
            }),
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const { container } = render(
      <ToastProvider>
        <UpdateProvider>
          <MemoryRouter initialEntries={['/settings']}>
            <SettingsPage />
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    const section = await screen.findByText('Docker 容器');
    const sectionEl = section.closest('#system-update') as HTMLElement;
    expect(sectionEl).toBeInTheDocument();
    expect(within(sectionEl).getByText('v1.0.1')).toBeInTheDocument();
    expect(within(sectionEl).getByText('linux/amd64')).toBeInTheDocument();
    expect(within(sectionEl).getByText(/当前已是最新版本/)).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(within(sectionEl).getByRole('button', { name: /检查更新/ }));

    await waitFor(() => {
      expect(checkCalled).toBe(true);
    });
    expect(await within(sectionEl).findByText(/修复播放器与导入问题/)).toBeInTheDocument();
    expect(
      within(sectionEl).getByText(/<script>window\.__xss=1<\/script>/),
    ).toBeInTheDocument();
    expect(sectionEl.querySelector('script')).toBeNull();
    expect(sectionEl.querySelector('img')).toBeNull();
    expect(container.querySelector('script')).toBeNull();
    expect((window as unknown as { __xss?: unknown }).__xss).toBeUndefined();

    const releaseLink = within(sectionEl).getByRole('link', { name: /GitHub Releases/ });
    expect(releaseLink).toHaveAttribute(
      'href',
      'https://github.com/booooodv/CourseFin/releases/tag/v1.0.2',
    );
    expect(releaseLink).toHaveAttribute('target', '_blank');
    expect(releaseLink).toHaveAttribute('rel', 'noopener noreferrer');
  });

  it('opens confirmation modal before applying update, rejects unsafe release_url, and renders 60% progress bar', async () => {
    let applyCalled = false;
    let statusCalls = 0;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        if (url === '/health' && method === 'GET') {
          return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
        }
        if (url === '/api/v1/system/update' && method === 'GET') {
          statusCalls += 1;
          if (statusCalls === 1) {
            return jsonResponse(
              makeStatus({
                deploy_mode: 'binary',
                platform: 'darwin/arm64',
                release_url: 'javascript:alert(1)',
                can_apply: true,
                update_available: true,
                latest_version: '1.0.2',
              }),
            );
          }
          return jsonResponse(
            makeStatus({
              deploy_mode: 'binary',
              platform: 'darwin/arm64',
              release_url: 'javascript:alert(1)',
              state: 'downloading',
              target_version: '1.0.2',
              progress_bytes: 9437184,
              total_bytes: 15728640,
              progress_percent: 60,
            }),
          );
        }
        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          applyCalled = true;
          return jsonResponse(
            makeStatus({
              deploy_mode: 'binary',
              platform: 'darwin/arm64',
              state: 'downloading',
              target_version: '1.0.2',
              progress_bytes: 9437184,
              total_bytes: 15728640,
              progress_percent: 60,
            }),
            202,
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const { container } = render(
      <ToastProvider>
        <UpdateProvider pollIntervalMs={50} healthPollIntervalMs={50} restartTimeoutMs={500}>
          <MemoryRouter initialEntries={['/settings']}>
            <SettingsPage />
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    expect(await screen.findByText('本地二进制')).toBeInTheDocument();
    expect(screen.getByText('darwin/arm64')).toBeInTheDocument();
    expect(container.querySelector('a[href^="javascript:"]')).toBeNull();
    expect(screen.queryByRole('link', { name: /GitHub Releases/ })).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /立即更新到 v1\.0\.2/ }));

    // Confirmation modal opens BEFORE calling POST /api/v1/system/update/apply
    expect(applyCalled).toBe(false);
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('heading', { name: /确认更新/ })).toBeInTheDocument();
    expect(within(dialog).getByText(/暂停|中断/)).toBeInTheDocument();

    await user.click(within(dialog).getByRole('button', { name: /确认更新/ }));

    await waitFor(() => {
      expect(applyCalled).toBe(true);
    });
    const bar = await screen.findByRole('progressbar');
    expect(bar).toHaveAttribute('aria-valuenow', '60');
    expect(screen.getByText('60%')).toBeInTheDocument();
  });

  it('saves update config via PUT /api/v1/system/update/config with toast confirmation and locks auto-check when locked by env', async () => {
    const configBodies: unknown[] = [];
    let lockedByEnv = false;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        if (url === '/api/v1/system/update' && method === 'GET') {
          return jsonResponse(
            makeStatus({
              auto_check_enabled: true,
              auto_check_locked_by_env: lockedByEnv,
              check_interval_hours: 6,
              mirror_url: '',
            }),
          );
        }
        if (url === '/api/v1/system/update/config' && method === 'PUT') {
          const body = JSON.parse(String(init?.body));
          configBodies.push(body);
          return jsonResponse(
            makeStatus({
              auto_check_enabled: body.auto_check_enabled ?? true,
              auto_check_locked_by_env: lockedByEnv,
              check_interval_hours: body.check_interval_hours,
              mirror_url: body.mirror_url,
            }),
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const firstRender = render(
      <ToastProvider>
        <UpdateProvider>
          <MemoryRouter initialEntries={['/settings']}>
            <SettingsPage />
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    const autoCheck = await screen.findByLabelText(/自动检查更新/);
    expect(autoCheck).toBeEnabled();
    expect(autoCheck).toBeChecked();

    const user = userEvent.setup();
    await user.click(autoCheck);
    expect(autoCheck).not.toBeChecked();

    const intervalSelect = screen.getByLabelText(/检查间隔/);
    await user.selectOptions(intervalSelect, '12');

    const mirrorInput = screen.getByLabelText(/GitHub 加速代理前缀/);
    expect(screen.getByText(/仅填写可信加速代理地址/)).toBeInTheDocument();

    // Rejects non-http(s) mirror URL before hitting backend
    await user.type(mirrorInput, 'ftp://invalid-mirror.example');
    await user.click(screen.getByRole('button', { name: '保存更新配置' }));
    expect(configBodies).toHaveLength(0);
    expect(
      (await screen.findAllByText(/加速代理地址必须以 http:\/\/ 或 https:\/\/ 开头/)).length,
    ).toBeGreaterThanOrEqual(1);

    await user.clear(mirrorInput);
    await user.type(mirrorInput, 'https://gh-proxy.com');
    await user.click(screen.getByRole('button', { name: '保存更新配置' }));

    await waitFor(() => {
      expect(configBodies).toEqual([
        {
          auto_check_enabled: false,
          check_interval_hours: 12,
          mirror_url: 'https://gh-proxy.com',
        },
      ]);
    });
    expect(await screen.findByText(/更新配置已保存/)).toBeInTheDocument();
    firstRender.unmount();

    // When auto_check_locked_by_env === true
    lockedByEnv = true;
    render(
      <ToastProvider>
        <UpdateProvider>
          <MemoryRouter initialEntries={['/settings']}>
            <SettingsPage />
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    const lockedToggle = await screen.findByLabelText(/自动检查更新/);
    expect(lockedToggle).toBeDisabled();
    expect(screen.getByText(/COURSEFIN_UPDATE_AUTO_CHECK/)).toBeInTheDocument();
  });

  it('disables apply button and renders cannot_apply_reason with GitHub Releases link when can_apply is false', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        if (url === '/api/v1/system/update' && method === 'GET') {
          return jsonResponse(
            makeStatus({
              update_available: true,
              latest_version: '1.0.2',
              can_apply: false,
              cannot_apply_reason: '当前操作系统不支持应用内热替换，请前往 GitHub Releases 手动下载更新',
              release_url: 'https://github.com/booooodv/CourseFin/releases/tag/v1.0.2',
            }),
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <UpdateProvider>
          <MemoryRouter initialEntries={['/settings']}>
            <SettingsPage />
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    expect(
      await screen.findByText('当前操作系统不支持应用内热替换，请前往 GitHub Releases 手动下载更新'),
    ).toBeInTheDocument();
    const link = screen.getByRole('link', { name: /GitHub Releases/ });
    expect(link).toHaveAttribute(
      'href',
      'https://github.com/booooodv/CourseFin/releases/tag/v1.0.2',
    );
    const applyBtn = screen.getByRole('button', { name: /立即更新到 v1\.0\.2/ });
    expect(applyBtn).toBeDisabled();
  });

  it('scrolls #system-update into view on #system-update hash and displays timeout refresh button', async () => {
    let statusCalls = 0;
    let healthCalls = 0;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        if (url === '/api/v1/system/update' && method === 'GET') {
          statusCalls += 1;
          if (statusCalls === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          return jsonResponse(makeStatus({ state: 'restarting', target_version: '1.0.2' }));
        }
        if (url === '/health' && method === 'GET') {
          healthCalls += 1;
          if (healthCalls === 1) {
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
          }
          throw new TypeError('Failed to fetch');
        }
        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({ state: 'downloading', target_version: '1.0.2' }),
            202,
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <UpdateProvider pollIntervalMs={15} healthPollIntervalMs={15} restartTimeoutMs={80}>
          <MemoryRouter initialEntries={['/settings#system-update']}>
            <SettingsPage />
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(Element.prototype.scrollIntoView).toHaveBeenCalled();
    });

    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /立即更新到 v1\.0\.2/ }));
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: /确认更新/ }));

    expect(await screen.findByText(/等待服务重启超时/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '立即刷新' })).toBeInTheDocument();
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    expect(screen.queryByText(/正在优雅停机并热重启服务/)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /检查更新/ })).toBeEnabled();
    expect(screen.getByLabelText(/自动检查更新/)).toBeEnabled();
    expect(screen.getByLabelText(/检查间隔/)).toBeEnabled();
    expect(screen.getByLabelText(/GitHub 加速代理前缀/)).toBeEnabled();
    expect(screen.getByRole('button', { name: '保存更新配置' })).toBeEnabled();
  });

  it('displays rollback banner on restart failure and degrades safely when rendered without UpdateProvider on 404', async () => {
    let statusCalls = 0;
    let healthCalls = 0;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        if (url === '/api/v1/system/update' && method === 'GET') {
          statusCalls += 1;
          if (statusCalls === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          return jsonResponse({ error: 'bad gateway' }, 502);
        }
        if (url === '/health' && method === 'GET') {
          healthCalls += 1;
          if (healthCalls === 1) {
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-old' });
          }
          return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-rollback-2' });
        }
        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({ state: 'downloading', target_version: '1.0.2' }),
            202,
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const firstRender = render(
      <ToastProvider>
        <UpdateProvider pollIntervalMs={15} healthPollIntervalMs={15} restartTimeoutMs={500}>
          <MemoryRouter initialEntries={['/settings']}>
            <SettingsPage />
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /立即更新到 v1\.0\.2/ }));
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: /确认更新/ }));

    expect(await screen.findByText(/已自动回滚至 v1\.0\.1/)).toBeInTheDocument();
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /检查更新/ })).toBeEnabled();
    expect(screen.getByLabelText(/自动检查更新/)).toBeEnabled();
    expect(screen.getByLabelText(/检查间隔/)).toBeEnabled();
    expect(screen.getByLabelText(/GitHub 加速代理前缀/)).toBeEnabled();
    expect(screen.getByRole('button', { name: '保存更新配置' })).toBeEnabled();
    firstRender.unmount();

    // Standalone SettingsPage without UpdateProvider when update endpoint returns 404
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <MemoryRouter initialEntries={['/settings']}>
          <SettingsPage />
        </MemoryRouter>
      </ToastProvider>,
    );

    expect(await screen.findByText('http://192.168.1.10:8096')).toBeInTheDocument();
    expect(screen.getByText('暂未获取到版本更新状态。')).toBeInTheDocument();
    expect(screen.queryByText(/加载系统设置失败/)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /保存设置/ })).toBeEnabled();
  });

  it('unblocks UI, hides progressbar, displays restartError on HTTP 500 poll failure during downloading, and clears restartError on checkNow', async () => {
    let statusCalls = 0;
    let resolveThirdPoll!: (res: Response) => void;
    const thirdPollPromise = new Promise<Response>((resolve) => {
      resolveThirdPoll = resolve;
    });

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        if (url === '/health' && method === 'GET') {
          return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
        }
        if (url === '/api/v1/system/update' && method === 'GET') {
          statusCalls += 1;
          if (statusCalls === 1) {
            return jsonResponse(makeStatus({ state: 'idle', update_available: true, latest_version: '1.0.2' }));
          }
          if (statusCalls === 2) {
            return jsonResponse(
              makeStatus({
                state: 'downloading',
                target_version: '1.0.2',
                progress_bytes: 5242880,
                total_bytes: 14820352,
                progress_percent: 35,
              }),
            );
          }
          return thirdPollPromise;
        }
        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(
            makeStatus({
              state: 'downloading',
              target_version: '1.0.2',
              progress_percent: 0,
            }),
            202,
          );
        }
        if (url === '/api/v1/system/update/check' && method === 'POST') {
          return jsonResponse(
            makeStatus({
              state: 'idle',
              latest_version: '1.0.3',
              update_available: true,
              last_error: '',
            }),
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    function RestartErrorProbe() {
      const { restartError } = useUpdate();
      return <div data-testid="restart-error-probe">{restartError ?? ''}</div>;
    }

    render(
      <ToastProvider>
        <UpdateProvider pollIntervalMs={15} healthPollIntervalMs={15} restartTimeoutMs={500}>
          <MemoryRouter initialEntries={['/settings']}>
            <RestartErrorProbe />
            <SettingsPage />
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /立即更新到 v1\.0\.2/ }));
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: /确认更新/ }));

    // First active poll shows 35% progressbar while downloading
    await waitFor(() => {
      expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '35');
    });
    expect(screen.getByRole('button', { name: /检查更新/ })).toBeDisabled();

    // Next status poll fails with HTTP 500 (non-disconnect error)
    resolveThirdPoll(jsonResponse({ error: 'internal server error' }, 500));

    const sectionEl = screen
      .getByRole('heading', { name: '系统版本与在线更新' })
      .closest('#system-update') as HTMLElement;
    expect(await within(sectionEl).findByText('internal server error')).toBeInTheDocument();
    expect(screen.getByTestId('restart-error-probe')).toHaveTextContent('internal server error');
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /检查更新/ })).toBeEnabled();
    expect(screen.getByLabelText(/自动检查更新/)).toBeEnabled();
    expect(screen.getByLabelText(/检查间隔/)).toBeEnabled();
    expect(screen.getByLabelText(/GitHub 加速代理前缀/)).toBeEnabled();
    expect(screen.getByRole('button', { name: '保存更新配置' })).toBeEnabled();

    // Subsequent successful checkNow clears restartError and hides error banner
    await user.click(screen.getByRole('button', { name: /检查更新/ }));
    await waitFor(() => {
      expect(screen.getByTestId('restart-error-probe')).toHaveTextContent('');
    });
    expect(within(sectionEl).queryByText('internal server error')).not.toBeInTheDocument();
  });

  it('shows inlineError during timeout, clears configError on input change, and re-scrolls when #system-update pill is clicked again', async () => {
    let statusCalls = 0;
    let healthCalls = 0;

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        if (url === '/api/v1/system/update' && method === 'GET') {
          statusCalls += 1;
          if (statusCalls === 1) {
            return jsonResponse(makeStatus({ state: 'idle' }));
          }
          return jsonResponse(makeStatus({ state: 'restarting', target_version: '1.0.2' }));
        }
        if (url === '/health' && method === 'GET') {
          healthCalls += 1;
          if (healthCalls === 1) {
            return jsonResponse({ status: 'ok', version: '1.0.1', boot_id: 'boot-1' });
          }
          throw new TypeError('Failed to fetch');
        }
        if (url === '/api/v1/system/update/apply' && method === 'POST') {
          return jsonResponse(makeStatus({ state: 'downloading', target_version: '1.0.2' }), 202);
        }
        if (url === '/api/v1/system/update/check' && method === 'POST') {
          return jsonResponse({ error: 'check failed after timeout' }, 502);
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    render(
      <ToastProvider>
        <UpdateProvider pollIntervalMs={15} healthPollIntervalMs={15} restartTimeoutMs={70}>
          <MemoryRouter initialEntries={['/settings#system-update']}>
            <Layout>
              <SettingsPage />
            </Layout>
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    await waitFor(() => {
      expect(Element.prototype.scrollIntoView).toHaveBeenCalledTimes(1);
    });

    // Re-click the top-bar pill while already on /settings#system-update -> re-scrolls
    const user = userEvent.setup();
    const pill = await screen.findByRole('link', { name: /新版本 v1\.0\.2/ });
    await user.click(pill);
    await waitFor(() => {
      expect(Element.prototype.scrollIntoView).toHaveBeenCalledTimes(2);
    });

    // Invalid mirror triggers configError; editing input clears configError
    const mirrorInput = screen.getByLabelText(/GitHub 加速代理前缀/);
    await user.type(mirrorInput, 'ftp://bad.example');
    await user.click(screen.getByRole('button', { name: '保存更新配置' }));
    const sectionEl = mirrorInput.closest('#system-update') as HTMLElement;
    expect(
      within(sectionEl).getByText(/加速代理地址必须以 http:\/\/ 或 https:\/\/ 开头/),
    ).toBeInTheDocument();

    await user.type(mirrorInput, 'x');
    expect(
      within(sectionEl).queryByText(/加速代理地址必须以 http:\/\/ 或 https:\/\/ 开头/),
    ).not.toBeInTheDocument();

    // Enter timeout phase, then trigger checkNow failure -> inlineError displays despite timeout phase
    await user.click(screen.getByRole('button', { name: /立即更新到 v1\.0\.2/ }));
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: /确认更新/ }));
    expect(await screen.findByText(/等待服务重启超时/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /检查更新/ }));
    expect(await within(sectionEl).findByText('check failed after timeout')).toBeInTheDocument();
  });

  it('renders SystemUpdateSection and confirmation modal with dual-theme semantic classes and closes modal on Escape', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method ?? 'GET';

        if (url.endsWith('/api/v1/settings')) {
          return jsonResponse(makeSettingsResponse());
        }
        if (url.endsWith('/api/v1/sessions')) {
          return jsonResponse({ sessions: [] });
        }
        if (url.endsWith('/api/v1/auth/me')) {
          return jsonResponse(makeMeResponse());
        }
        if (url === '/api/v1/system/update' && method === 'GET') {
          return jsonResponse(
            makeStatus({
              update_available: true,
              latest_version: '1.0.2',
              can_apply: true,
              release_notes: 'Semantic dual-theme release notes',
            }),
          );
        }
        return jsonResponse({ error: 'not found' }, 404);
      }),
    );

    const { container } = render(
      <ToastProvider>
        <UpdateProvider>
          <MemoryRouter initialEntries={['/settings']}>
            <SettingsPage />
          </MemoryRouter>
        </UpdateProvider>
      </ToastProvider>,
    );

    const updateBtn = await screen.findByRole('button', { name: /立即更新到 v1\.0\.2/ });
    const sectionEl = updateBtn.closest('#system-update') as HTMLElement;
    expect(sectionEl).toBeInTheDocument();
    expect(sectionEl.innerHTML).not.toMatch(
      /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald|amber|sky)-/,
    );

    const user = userEvent.setup();
    await user.click(updateBtn);

    const dialog = await screen.findByRole('dialog');
    expect(dialog.className).toContain('bg-overlay');
    expect(within(dialog).getByRole('heading', { name: '确认更新到 v1.0.2' })).toBeInTheDocument();
    expect(container.innerHTML).not.toMatch(
      /\b(?:bg|text|border|ring|from|to|via)-(?:slate|indigo|rose|emerald|amber|sky)-/,
    );

    await user.keyboard('{Escape}');
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
  });
});
