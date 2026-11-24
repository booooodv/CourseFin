import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { api } from '../api/client';
import { copyTextToClipboard } from '../utils/clipboard';
import { ToastProvider, useToast } from '../components/Toast';
import Tooltip from '../components/Tooltip';

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('copyTextToClipboard', () => {
  const originalClipboard = navigator.clipboard;
  const originalExecCommand = document.execCommand;
  const originalIsSecureContext = window.isSecureContext;

  afterEach(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      configurable: true,
    });
    Object.defineProperty(window, 'isSecureContext', {
      value: originalIsSecureContext,
      configurable: true,
    });
    document.execCommand = originalExecCommand;
    vi.restoreAllMocks();
  });

  it('uses navigator.clipboard.writeText when available in secure context', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    });

    const ok = await copyTextToClipboard('http://localhost:8096');
    expect(ok).toBe(true);
    expect(writeText).toHaveBeenCalledWith('http://localhost:8096');
  });

  it('falls back to document.execCommand when clipboard API is unavailable or rejects', async () => {
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true });
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
    const execMock = vi.fn().mockReturnValue(true);
    document.execCommand = execMock;

    const ok1 = await copyTextToClipboard('http://192.168.1.5:8096');
    expect(ok1).toBe(true);
    expect(execMock).toHaveBeenCalledWith('copy');

    // When clipboard rejects AND execCommand fails -> returns false
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) },
      configurable: true,
    });
    document.execCommand = vi.fn().mockReturnValue(false);

    const ok2 = await copyTextToClipboard('http://192.168.1.5:8096');
    expect(ok2).toBe(false);
  });
});

describe('ToastProvider & useToast', () => {
  function ToastTrigger() {
    const { toast } = useToast();
    return (
      <div>
        <button type="button" onClick={() => toast.success('操作成功')}>
          触发成功
        </button>
        <button type="button" onClick={() => toast.error('操作失败')}>
          触发失败
        </button>
        <button type="button" onClick={() => toast.info('提示消息')}>
          触发提示
        </button>
      </div>
    );
  }

  it('renders floating toasts and supports dismissing them', async () => {
    render(
      <ToastProvider>
        <ToastTrigger />
      </ToastProvider>,
    );

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: '触发成功' }));
    expect(screen.getByRole('status')).toHaveTextContent('操作成功');

    await user.click(screen.getByRole('button', { name: '触发失败' }));
    expect(screen.getByText('操作失败')).toBeInTheDocument();

    const closeBtns = screen.getAllByRole('button', { name: '关闭提示' });
    await user.click(closeBtns[0]);
    expect(screen.queryByText('操作成功')).not.toBeInTheDocument();
  });

  it('is safe to invoke useToast outside ToastProvider (standalone test compatibility)', async () => {
    render(<ToastTrigger />);
    const user = userEvent.setup();
    await expect(user.click(screen.getByRole('button', { name: '触发提示' }))).resolves.not.toThrow();
  });
});

describe('Tooltip', () => {
  it('shows tooltip on hover/focus, hides on unhover/blur, and preserves button accessible name', async () => {
    const onClick = vi.fn();
    render(
      <div>
        <Tooltip content="复制服务器地址到剪贴板">
          <button type="button" onClick={onClick}>
            复制
          </button>
        </Tooltip>
        <button type="button">其他按钮</button>
      </div>,
    );

    const copyBtn = screen.getByRole('button', { name: '复制' });
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.hover(copyBtn);
    await waitFor(() => {
      expect(screen.getByRole('tooltip')).toHaveTextContent('复制服务器地址到剪贴板');
    });

    await user.unhover(copyBtn);
    await waitFor(() => {
      expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
    });

    // Focus also reveals tooltip immediately without breaking click
    await user.tab();
    expect(copyBtn).toHaveFocus();
    expect(screen.getByRole('tooltip')).toHaveTextContent('复制服务器地址到剪贴板');

    await user.click(copyBtn);
    expect(onClick).toHaveBeenCalledTimes(1);

    await user.tab();
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });
});

describe('API client extensions', () => {
  beforeEach(() => {
    localStorage.setItem('coursefin_token', 'tok-1');
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('browseFsDirs and deleteLibrary pass expected query parameters', async () => {
    const calls: { url: string; method: string }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        calls.push({ url, method: init?.method ?? 'GET' });
        if (url.startsWith('/api/v1/fs/dirs')) {
          return jsonResponse({ current_path: '/media', parent_path: '/', entries: [], roots: [] });
        }
        return jsonResponse({ ok: true });
      }),
    );

    await api.browseFsDirs('/media', true);
    await api.deleteLibrary('lib-1', { moveToLibraryId: 'lib-2' });
    await api.deleteLibrary('lib-1', { force: true });
    await api.deleteLibrary('lib-1');

    expect(calls).toEqual([
      { url: '/api/v1/fs/dirs?path=%2Fmedia&show_hidden=true', method: 'GET' },
      { url: '/api/v1/libraries/lib-1?move_to_library_id=lib-2', method: 'DELETE' },
      { url: '/api/v1/libraries/lib-1?force=true', method: 'DELETE' },
      { url: '/api/v1/libraries/lib-1', method: 'DELETE' },
    ]);
  });
});
