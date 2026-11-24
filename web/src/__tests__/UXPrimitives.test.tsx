import { useState } from 'react';
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

  it('closes immediately when trigger transitions to disabled while hovered', async () => {
    function DisablingButton() {
      const [disabled, setDisabled] = useState(false);
      return (
        <Tooltip content="扫描本地目录">
          <button type="button" disabled={disabled} onClick={() => setDisabled(true)}>
            扫描
          </button>
        </Tooltip>
      );
    }

    render(<DisablingButton />);
    const user = userEvent.setup();
    const btn = screen.getByRole('button', { name: '扫描' });

    await user.hover(btn);
    await waitFor(() => {
      expect(screen.getByRole('tooltip')).toHaveTextContent('扫描本地目录');
    });

    await user.click(btn);
    expect(btn).toBeDisabled();
    await waitFor(() => {
      expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
    });
  });

  it('clamps tooltip horizontally within viewport edges and wraps long text without overflow', async () => {
    const origInnerWidth = window.innerWidth;
    Object.defineProperty(window, 'innerWidth', { value: 1024, configurable: true });

    const origGetRect = HTMLElement.prototype.getBoundingClientRect;
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
      this: HTMLElement,
    ) {
      if (this.getAttribute('role') === 'tooltip') {
        return {
          top: 50,
          bottom: 80,
          left: 0,
          right: 200,
          width: 200,
          height: 30,
          x: 0,
          y: 50,
          toJSON: () => {},
        } as DOMRect;
      }
      if (this.tagName === 'BUTTON') {
        return {
          top: 100,
          bottom: 132,
          left: 970,
          right: 1018,
          width: 48,
          height: 32,
          x: 970,
          y: 100,
          toJSON: () => {},
        } as DOMRect;
      }
      return origGetRect.call(this);
    });

    try {
      render(
        <Tooltip content="用 AI 自动生成课程简介、标签与清晰章节结构（预览后应用）">
          <button type="button">AI 智能整理</button>
        </Tooltip>,
      );

      const user = userEvent.setup();
      await user.hover(screen.getByRole('button', { name: 'AI 智能整理' }));

      const tooltip = await screen.findByRole('tooltip');
      expect(tooltip.className).not.toContain('whitespace-nowrap');
      expect(tooltip.className).toContain('whitespace-normal');

      // With innerWidth=1024, edgePadding=10, tooltipWidth=200 -> max center left = 1024 - 10 - 100 = 914
      const leftPx = Number.parseFloat(tooltip.style.left);
      expect(leftPx).toBeLessThanOrEqual(914);
      expect(leftPx).toBeGreaterThanOrEqual(110);
    } finally {
      Object.defineProperty(window, 'innerWidth', { value: origInnerWidth, configurable: true });
    }
  });

  it('positions placement="right" to the right of trigger, vertically centers with viewport clamping, and keeps whitespace-normal', async () => {
    const origInnerWidth = window.innerWidth;
    const origInnerHeight = window.innerHeight;
    Object.defineProperty(window, 'innerWidth', { value: 1024, configurable: true });
    Object.defineProperty(window, 'innerHeight', { value: 768, configurable: true });

    let triggerTop = 100;
    let triggerBottom = 132;
    const origGetRect = HTMLElement.prototype.getBoundingClientRect;
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
      this: HTMLElement,
    ) {
      if (this.getAttribute('role') === 'tooltip') {
        return {
          top: 0,
          bottom: 40,
          left: 0,
          right: 140,
          width: 140,
          height: 40,
          x: 0,
          y: 0,
          toJSON: () => {},
        } as DOMRect;
      }
      if (this.tagName === 'BUTTON') {
        return {
          top: triggerTop,
          bottom: triggerBottom,
          left: 16,
          right: 56,
          width: 40,
          height: 32,
          x: 16,
          y: triggerTop,
          toJSON: () => {},
        } as DOMRect;
      }
      return origGetRect.call(this);
    });

    try {
      const { rerender } = render(
        <Tooltip content="折叠导航提示" placement="right">
          <button type="button">导航项</button>
        </Tooltip>,
      );

      const user = userEvent.setup();
      await user.hover(screen.getByRole('button', { name: '导航项' }));

      const tooltip = await screen.findByRole('tooltip');
      expect(tooltip).toHaveTextContent('折叠导航提示');
      expect(tooltip.className).not.toContain('whitespace-nowrap');
      expect(tooltip.className).toContain('whitespace-normal');
      expect(tooltip.className).toContain('-translate-y-1/2');
      expect(Number.parseFloat(tooltip.style.left)).toBeGreaterThanOrEqual(56 + 8);
      // trigger center = 116
      expect(Number.parseFloat(tooltip.style.top)).toBe(116);

      // Clamp near top edge: trigger center = 18, minTop = 10 + 40/2 = 30
      await user.unhover(screen.getByRole('button', { name: '导航项' }));
      triggerTop = 2;
      triggerBottom = 34;
      rerender(
        <Tooltip content="折叠导航提示" placement="right">
          <button type="button">导航项</button>
        </Tooltip>,
      );
      await user.hover(screen.getByRole('button', { name: '导航项' }));
      const topClampedTooltip = await screen.findByRole('tooltip');
      expect(Number.parseFloat(topClampedTooltip.style.top)).toBeGreaterThanOrEqual(30);

      // Clamp near bottom edge: trigger center = 756, maxTop = 768 - 10 - 40/2 = 738
      await user.unhover(screen.getByRole('button', { name: '导航项' }));
      triggerTop = 740;
      triggerBottom = 772;
      rerender(
        <Tooltip content="折叠导航提示" placement="right">
          <button type="button">导航项</button>
        </Tooltip>,
      );
      await user.hover(screen.getByRole('button', { name: '导航项' }));
      const bottomClampedTooltip = await screen.findByRole('tooltip');
      expect(Number.parseFloat(bottomClampedTooltip.style.top)).toBeLessThanOrEqual(738);
    } finally {
      Object.defineProperty(window, 'innerWidth', { value: origInnerWidth, configurable: true });
      Object.defineProperty(window, 'innerHeight', { value: origInnerHeight, configurable: true });
      vi.restoreAllMocks();
    }
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
