import { StrictMode, useState } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { api } from '../api/client';
import { ToastProvider } from '../components/Toast';
import { UpdateProvider, useUpdate } from '../components/UpdateContext';
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
});
