import type { FormEvent } from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import {
  Check,
  Copy,
  ImagePlus,
  KeyRound,
  Laptop,
  Loader2,
  Mic,
  Save,
  Server,
  Sparkles,
  Trash2,
  Tv,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import CoverStylePicker, { FALLBACK_COVER_STYLES } from '../components/CoverStylePicker';
import SystemUpdateSection from '../components/SystemUpdateSection';
import { useToast } from '../components/Toast';
import Tooltip from '../components/Tooltip';
import { copyTextToClipboard } from '../utils/clipboard';
import type {
  AIImageProtocol,
  CoverStyleTemplate,
  Session,
  SettingsPayload,
  TestAIImageOverrides,
  TestASROverrides,
  UpdateSettingsInput,
} from '../types/coursefin';

const MASKED_KEY_PLACEHOLDER = '****';

const FIELD_INPUT_CLASS =
  'w-full rounded-xl border border-default bg-subtle/60 px-3.5 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:bg-card focus:outline-none focus:ring-2 focus:ring-accent-ring transition-colors disabled:opacity-50';

export default function SettingsPage() {
  const { toast } = useToast();
  const location = useLocation();
  const [settings, setSettings] = useState<SettingsPayload | null>(null);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [currentToken, setCurrentToken] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Server & AI settings form
  const [serverName, setServerName] = useState('CourseFin');
  const [scanIntervalMinutes, setScanIntervalMinutes] = useState('60');
  const [aiBaseUrl, setAiBaseUrl] = useState('');
  const [aiApiKey, setAiApiKey] = useState('');
  const [clearAiKey, setClearAiKey] = useState(false);
  const [aiModel, setAiModel] = useState('');
  const [aiMaxSubtitleChars, setAiMaxSubtitleChars] = useState('4000');

  // ASR (speech-to-text) settings form
  const [aiAsrBaseUrl, setAiAsrBaseUrl] = useState('');
  const [aiAsrApiKey, setAiAsrApiKey] = useState('');
  const [clearAiAsrKey, setClearAiAsrKey] = useState(false);
  const [aiAsrModel, setAiAsrModel] = useState('');
  const [aiAsrLanguage, setAiAsrLanguage] = useState('');

  // AI cover image generation settings form
  const [coverStyles, setCoverStyles] = useState<CoverStyleTemplate[]>(FALLBACK_COVER_STYLES);
  const [aiImageProtocol, setAiImageProtocol] = useState<AIImageProtocol>('openai_images');
  const [aiImageBaseUrl, setAiImageBaseUrl] = useState('');
  const [aiImageApiKey, setAiImageApiKey] = useState('');
  const [clearAiImageKey, setClearAiImageKey] = useState(false);
  const [aiImageModel, setAiImageModel] = useState('');
  const [aiImageSize, setAiImageSize] = useState('');
  const [aiCoverDefaultStyle, setAiCoverDefaultStyle] = useState('editorial_modern');

  const [savingSettings, setSavingSettings] = useState(false);
  const [settingsSuccess, setSettingsSuccess] = useState<string | null>(null);
  const [testingAI, setTestingAI] = useState(false);
  const [aiTestMessage, setAiTestMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const [testingASR, setTestingASR] = useState(false);
  const [asrTestMessage, setAsrTestMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const [testingImage, setTestingImage] = useState(false);
  const [imageTestMessage, setImageTestMessage] = useState<{ ok: boolean; text: string } | null>(null);

  // Copy button feedback states
  const [copiedUrl, setCopiedUrl] = useState(false);
  const [copiedUser, setCopiedUser] = useState(false);
  const copyUrlTimerRef = useRef<number | null>(null);
  const copyUserTimerRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (copyUrlTimerRef.current !== null) window.clearTimeout(copyUrlTimerRef.current);
      if (copyUserTimerRef.current !== null) window.clearTimeout(copyUserTimerRef.current);
    };
  }, []);

  // Password form
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [changingPassword, setChangingPassword] = useState(false);
  const [passwordFeedback, setPasswordFeedback] = useState<{ ok: boolean; text: string } | null>(
    null,
  );

  const applySettingsPayload = useCallback((data: SettingsPayload) => {
    setSettings(data);
    setServerName(data.server_name || 'CourseFin');
    setScanIntervalMinutes(String(data.scan_interval_minutes ?? 60));
    setAiBaseUrl(data.ai_base_url || '');
    setAiApiKey('');
    setClearAiKey(false);
    setAiModel(data.ai_model || '');
    setAiMaxSubtitleChars(String(data.ai_max_subtitle_chars ?? 4000));
    setAiAsrBaseUrl(data.ai_asr_base_url || '');
    setAiAsrApiKey('');
    setClearAiAsrKey(false);
    setAiAsrModel(data.ai_asr_model || '');
    setAiAsrLanguage(data.ai_asr_language || '');
    setAiImageProtocol(data.ai_image_protocol || 'openai_images');
    setAiImageBaseUrl(data.ai_image_base_url || '');
    setAiImageApiKey('');
    setClearAiImageKey(false);
    setAiImageModel(data.ai_image_model || '');
    setAiImageSize(data.ai_image_size || '');
    setAiCoverDefaultStyle(data.ai_cover_default_style || 'editorial_modern');
  }, []);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [settingsData, sessionsData, meData, stylesData] = await Promise.all([
        api.getSettings(),
        api.listSessions(),
        api.getMe().catch(() => null),
        api.listCoverStyles().catch(() => null),
      ]);
      if (stylesData?.styles && stylesData.styles.length > 0) {
        setCoverStyles(stylesData.styles);
      }
      applySettingsPayload(settingsData);
      setSessions(sessionsData.sessions ?? []);
      if (meData?.session?.token) {
        setCurrentToken(meData.session.token);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载系统设置失败');
    } finally {
      setLoading(false);
    }
  }, [applySettingsPayload]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  useEffect(() => {
    if (loading) return;
    const isTargetHash =
      location.hash === '#system-update' ||
      (typeof window !== 'undefined' && window.location.hash === '#system-update');
    if (isTargetHash && typeof document !== 'undefined') {
      const el = document.getElementById('system-update');
      if (el && typeof el.scrollIntoView === 'function') {
        el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }
    }
  }, [loading, location.hash, location.key]);

  const jellyfinUrl =
    settings?.base_url_hint?.trim() ||
    (typeof window !== 'undefined' ? window.location.origin : 'http://localhost:8096');
  const jellyfinUsername = settings?.jellyfin_username || 'admin';

  async function handleCopyServerUrl() {
    const ok = await copyTextToClipboard(jellyfinUrl);
    if (ok) {
      setCopiedUrl(true);
      if (copyUrlTimerRef.current !== null) window.clearTimeout(copyUrlTimerRef.current);
      copyUrlTimerRef.current = window.setTimeout(() => setCopiedUrl(false), 2000);
      toast.success('已复制服务器地址到剪贴板');
    } else {
      toast.error('复制失败，请手动选中复制');
    }
  }

  async function handleCopyUsername() {
    const ok = await copyTextToClipboard(jellyfinUsername);
    if (ok) {
      setCopiedUser(true);
      if (copyUserTimerRef.current !== null) window.clearTimeout(copyUserTimerRef.current);
      copyUserTimerRef.current = window.setTimeout(() => setCopiedUser(false), 2000);
      toast.success('已复制用户名到剪贴板');
    } else {
      toast.error('复制失败，请手动选中复制');
    }
  }

  async function handleSaveSettings(e: FormEvent) {
    e.preventDefault();
    if (savingSettings) return;
    setSavingSettings(true);
    setSettingsSuccess(null);
    setError(null);

    const parsedInterval = Number.parseInt(scanIntervalMinutes, 10);
    const intervalNum = Number.isNaN(parsedInterval) ? 60 : Math.max(1, parsedInterval);
    const parsedMaxSub = Number.parseInt(aiMaxSubtitleChars, 10);
    const maxSubNum = Number.isNaN(parsedMaxSub) ? 4000 : Math.max(0, parsedMaxSub);

    const payload: UpdateSettingsInput = {
      server_name: serverName.trim() || 'CourseFin',
      scan_interval_minutes: intervalNum,
      ai_base_url: aiBaseUrl.trim(),
      ai_model: aiModel.trim(),
      ai_max_subtitle_chars: maxSubNum,
      ai_asr_base_url: aiAsrBaseUrl.trim(),
      ai_asr_model: aiAsrModel.trim(),
      ai_asr_language: aiAsrLanguage.trim(),
      ai_image_protocol: aiImageProtocol,
      ai_image_base_url: aiImageBaseUrl.trim(),
      ai_image_model: aiImageModel.trim(),
      ai_image_size: aiImageSize.trim(),
      ai_cover_default_style: aiCoverDefaultStyle,
    };

    if (clearAiKey) {
      payload.clear_ai_api_key = true;
    } else {
      const trimmedKey = aiApiKey.trim();
      if (trimmedKey !== '' && trimmedKey !== MASKED_KEY_PLACEHOLDER) {
        payload.ai_api_key = trimmedKey;
      }
    }

    if (clearAiAsrKey) {
      payload.clear_ai_asr_api_key = true;
    } else {
      const trimmedAsrKey = aiAsrApiKey.trim();
      if (trimmedAsrKey !== '' && trimmedAsrKey !== MASKED_KEY_PLACEHOLDER) {
        payload.ai_asr_api_key = trimmedAsrKey;
      }
    }

    if (clearAiImageKey) {
      payload.clear_ai_image_api_key = true;
    } else {
      const trimmedImgKey = aiImageApiKey.trim();
      if (trimmedImgKey !== '' && trimmedImgKey !== MASKED_KEY_PLACEHOLDER) {
        payload.ai_image_api_key = trimmedImgKey;
      }
    }

    try {
      const updated = await api.updateSettings(payload);
      applySettingsPayload(updated);
      setSettingsSuccess('系统与 AI 设置已保存');
      toast.success('系统与 AI 设置已保存');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '保存设置失败';
      setError(msg);
      toast.error(msg);
    } finally {
      setSavingSettings(false);
    }
  }

  async function handleTestAI() {
    if (testingAI) return;
    setTestingAI(true);
    setAiTestMessage(null);

    const trimmedKey = aiApiKey.trim();
    const parsedMaxSub = Number.parseInt(aiMaxSubtitleChars, 10);
    const overrides: {
      base_url?: string;
      api_key?: string;
      model?: string;
      max_subtitle_chars?: number;
    } = {
      base_url: aiBaseUrl.trim(),
      model: aiModel.trim(),
      max_subtitle_chars: Number.isNaN(parsedMaxSub) ? 4000 : Math.max(0, parsedMaxSub),
    };
    if (trimmedKey !== '' && trimmedKey !== MASKED_KEY_PLACEHOLDER) {
      overrides.api_key = trimmedKey;
    }

    try {
      await api.testAI(overrides);
      setAiTestMessage({ ok: true, text: 'AI 接口连接测试成功！' });
      toast.success('AI 接口连接测试成功');
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'AI 接口连接测试失败';
      setAiTestMessage({
        ok: false,
        text: msg,
      });
      toast.error(msg);
    } finally {
      setTestingAI(false);
    }
  }

  async function handleTestASR() {
    if (testingASR) return;
    setTestingASR(true);
    setAsrTestMessage(null);

    const overrides: TestASROverrides = {
      asr_base_url: aiAsrBaseUrl.trim(),
      asr_model: aiAsrModel.trim(),
      asr_language: aiAsrLanguage.trim(),
      ai_base_url: aiBaseUrl.trim(),
    };

    if (clearAiAsrKey) {
      overrides.clear_ai_asr_api_key = true;
    } else {
      const trimmedAsrKey = aiAsrApiKey.trim();
      if (trimmedAsrKey !== '' && trimmedAsrKey !== MASKED_KEY_PLACEHOLDER) {
        overrides.asr_api_key = trimmedAsrKey;
      }
    }

    if (clearAiKey) {
      overrides.clear_ai_api_key = true;
    } else {
      const trimmedAiKey = aiApiKey.trim();
      if (trimmedAiKey !== '' && trimmedAiKey !== MASKED_KEY_PLACEHOLDER) {
        overrides.ai_api_key = trimmedAiKey;
      }
    }

    try {
      await api.testASR(overrides);
      setAsrTestMessage({ ok: true, text: '语音转文字接口连接测试成功！' });
      toast.success('语音转文字接口连接测试成功');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '语音转文字接口连接测试失败';
      setAsrTestMessage({
        ok: false,
        text: msg,
      });
      toast.error(msg);
    } finally {
      setTestingASR(false);
    }
  }

  async function handleTestImage() {
    if (testingImage) return;
    setTestingImage(true);
    setImageTestMessage(null);

    const overrides: TestAIImageOverrides = {
      image_protocol: aiImageProtocol,
      image_base_url: aiImageBaseUrl.trim(),
      image_model: aiImageModel.trim(),
      image_size: aiImageSize.trim(),
      ai_base_url: aiBaseUrl.trim(),
    };

    if (clearAiImageKey) {
      overrides.clear_ai_image_api_key = true;
    } else {
      const trimmedImgKey = aiImageApiKey.trim();
      if (trimmedImgKey !== '' && trimmedImgKey !== MASKED_KEY_PLACEHOLDER) {
        overrides.image_api_key = trimmedImgKey;
      }
    }

    if (clearAiKey) {
      overrides.clear_ai_api_key = true;
    } else {
      const trimmedAiKey = aiApiKey.trim();
      if (trimmedAiKey !== '' && trimmedAiKey !== MASKED_KEY_PLACEHOLDER) {
        overrides.ai_api_key = trimmedAiKey;
      }
    }

    try {
      await api.testAIImage(overrides);
      setImageTestMessage({ ok: true, text: 'AI 封面生图接口测试成功！' });
      toast.success('AI 封面生图接口测试成功');
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'AI 封面生图接口测试失败';
      setImageTestMessage({
        ok: false,
        text: msg,
      });
      toast.error(msg);
    } finally {
      setTestingImage(false);
    }
  }

  async function handleChangePassword(e: FormEvent) {
    e.preventDefault();
    if (changingPassword) return;
    setPasswordFeedback(null);

    if (!oldPassword || !newPassword) {
      setPasswordFeedback({ ok: false, text: '请输入当前密码和新密码' });
      return;
    }
    if (newPassword !== confirmPassword) {
      setPasswordFeedback({ ok: false, text: '两次输入的新密码不一致' });
      return;
    }

    setChangingPassword(true);
    try {
      await api.changePassword(oldPassword, newPassword);
      setOldPassword('');
      setNewPassword('');
      setConfirmPassword('');
      setPasswordFeedback({ ok: true, text: '管理员密码已更新' });
      toast.success('管理员密码已更新');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '修改密码失败';
      setPasswordFeedback({
        ok: false,
        text: msg,
      });
      toast.error(msg);
    } finally {
      setChangingPassword(false);
    }
  }

  async function handleRevokeSession(token: string) {
    setError(null);
    try {
      await api.revokeSession(token);
      setSessions((prev) => prev.filter((s) => s.token !== token));
      toast.success('已注销该设备会话');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '注销设备会话失败';
      setError(msg);
      toast.error(msg);
    }
  }

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center py-24 text-ink-muted gap-3">
        <Loader2 className="h-7 w-7 animate-spin text-accent" />
        <p className="text-sm font-medium">正在加载系统设置...</p>
      </div>
    );
  }

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-ink-primary">系统与连接设置</h1>
        <p className="text-xs text-ink-muted mt-1">
          配置 Jellyfin 客户端连接、AI 课程整理模型、管理员密码与多端登录会话。
        </p>
      </div>

      {error && (
        <div className="flex items-center justify-between rounded-2xl border border-status-danger/30 bg-status-danger-soft px-4 py-3 text-sm text-status-danger-ink">
          <span>{error}</span>
          <button
            type="button"
            aria-label="关闭错误提示"
            onClick={() => setError(null)}
            className="rounded-lg p-1 text-status-danger-ink hover:bg-card/60 transition-colors cursor-pointer"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      {/* Jellyfin Client Connection Guide */}
      <section className="relative overflow-hidden rounded-3xl bg-card border border-accent-ring/60 shadow-card p-6 space-y-5">
        <div
          aria-hidden="true"
          className="pointer-events-none absolute -top-24 -right-24 h-56 w-56 rounded-full bg-accent/10 blur-3xl"
        />

        <div className="relative flex items-center gap-3">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
            <Tv className="h-4 w-4" />
          </span>
          <div>
            <h2 className="text-base font-semibold text-ink-primary">Jellyfin 客户端连接指南</h2>
            <p className="text-xs text-ink-muted">
              在支持 Jellyfin 协议的播放器中填入以下信息，即可同步课程目录与观看进度。
            </p>
          </div>
        </div>

        <div className="relative grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="rounded-2xl border border-subtle bg-subtle p-4 space-y-1.5">
            <span className="text-xs text-ink-muted">服务器地址（Server URL）</span>
            <div className="flex items-center justify-between gap-2">
              <code className="text-sm font-mono font-medium text-accent-ink break-all">
                {jellyfinUrl}
              </code>
              <Tooltip content="复制 Jellyfin 服务器地址到剪贴板">
                <button
                  type="button"
                  aria-label="复制服务器地址"
                  onClick={() => void handleCopyServerUrl()}
                  className="shrink-0 inline-flex items-center gap-1.5 rounded-xl border border-default bg-card px-2.5 py-1.5 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors cursor-pointer shadow-card"
                >
                  {copiedUrl ? (
                    <Check className="h-3.5 w-3.5 text-status-success-ink" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                  <span>{copiedUrl ? '已复制' : '复制'}</span>
                </button>
              </Tooltip>
            </div>
          </div>

          <div className="rounded-2xl border border-subtle bg-subtle p-4 space-y-1.5">
            <span className="text-xs text-ink-muted">登录用户名（Username）</span>
            <div className="flex items-center justify-between gap-2">
              <p className="text-sm font-mono font-semibold text-ink-primary">
                {jellyfinUsername}
              </p>
              <Tooltip content="复制 Jellyfin 登录用户名">
                <button
                  type="button"
                  aria-label="复制用户名"
                  onClick={() => void handleCopyUsername()}
                  className="shrink-0 inline-flex items-center gap-1.5 rounded-xl border border-default bg-card px-2.5 py-1.5 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary transition-colors cursor-pointer shadow-card"
                >
                  {copiedUser ? (
                    <Check className="h-3.5 w-3.5 text-status-success-ink" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                  <span>{copiedUser ? '已复制' : '复制'}</span>
                </button>
              </Tooltip>
            </div>
          </div>
        </div>

        <div className="relative grid grid-cols-1 md:grid-cols-3 gap-3 text-xs text-ink-secondary">
          <div className="rounded-2xl border border-subtle bg-subtle/60 p-3.5 space-y-1">
            <p className="font-semibold text-ink-primary">Infuse (iOS / macOS / tvOS)</p>
            <p className="text-ink-muted leading-relaxed">
              新增文件来源 → 连接 Emby / Jellyfin → 协议选择 Jellyfin，输入服务器地址与管理员账号密码即可。
            </p>
          </div>
          <div className="rounded-2xl border border-subtle bg-subtle/60 p-3.5 space-y-1">
            <p className="font-semibold text-ink-primary">VidHub</p>
            <p className="text-ink-muted leading-relaxed">
              添加媒体库 → 选择 Jellyfin → 填写服务器地址和端口，开启进度同步即可按课程章节顺序连播。
            </p>
          </div>
          <div className="rounded-2xl border border-subtle bg-subtle/60 p-3.5 space-y-1">
            <p className="font-semibold text-ink-primary">SenPlayer</p>
            <p className="text-ink-muted leading-relaxed">
              媒体服务 → 添加 Jellyfin 服务器 → 填入上方地址、用户名与密码，支持外挂字幕与续播进度同步。
            </p>
          </div>
        </div>
      </section>

      {/* System Version & Self-Update */}
      <SystemUpdateSection />

      {/* Server & AI Config Form */}
      <form
        onSubmit={handleSaveSettings}
        className="rounded-3xl bg-card border border-subtle shadow-card p-6 space-y-5"
      >
        <div className="flex items-center gap-3">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
            <Server className="h-4 w-4" />
          </span>
          <h2 className="text-base font-semibold text-ink-primary">服务器与 AI 接口配置</h2>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="space-y-1.5">
            <label htmlFor="settings-server-name" className="block text-xs font-medium text-ink-secondary">
              服务器名称
            </label>
            <input
              id="settings-server-name"
              type="text"
              value={serverName}
              onChange={(e) => setServerName(e.target.value)}
              className={FIELD_INPUT_CLASS}
              required
            />
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-scan-interval" className="block text-xs font-medium text-ink-secondary">
              定时扫描间隔（分钟）
            </label>
            <input
              id="settings-scan-interval"
              type="number"
              min={1}
              value={scanIntervalMinutes}
              onChange={(e) => setScanIntervalMinutes(e.target.value)}
              className={FIELD_INPUT_CLASS}
              required
            />
          </div>

          <div className="space-y-1.5 sm:col-span-2 pt-3 border-t border-subtle">
            <div className="flex items-center gap-1.5 text-xs font-semibold text-accent-ink">
              <Sparkles className="h-3.5 w-3.5" />
              <span>OpenAI 兼容接口（用于 AI 智能整理课程）</span>
            </div>
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-ai-base-url" className="block text-xs font-medium text-ink-secondary">
              AI Base URL
            </label>
            <input
              id="settings-ai-base-url"
              type="url"
              value={aiBaseUrl}
              onChange={(e) => setAiBaseUrl(e.target.value)}
              placeholder="https://api.openai.com/v1"
              className={FIELD_INPUT_CLASS}
            />
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-ai-model" className="block text-xs font-medium text-ink-secondary">
              AI 模型名称
            </label>
            <input
              id="settings-ai-model"
              type="text"
              value={aiModel}
              onChange={(e) => setAiModel(e.target.value)}
              placeholder="gpt-4o-mini"
              className={FIELD_INPUT_CLASS}
            />
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label htmlFor="settings-ai-key" className="block text-xs font-medium text-ink-secondary">
                AI API Key
              </label>
              {settings?.ai_api_key_set && (
                <label className="inline-flex items-center gap-1.5 text-[11px] text-ink-muted cursor-pointer">
                  <input
                    type="checkbox"
                    checked={clearAiKey}
                    onChange={(e) => setClearAiKey(e.target.checked)}
                    className="h-3.5 w-3.5 rounded border-default bg-card text-status-danger focus:ring-2 focus:ring-accent-ring"
                  />
                  <span>清除已存 Key</span>
                </label>
              )}
            </div>
            <input
              id="settings-ai-key"
              type="password"
              value={aiApiKey}
              disabled={clearAiKey}
              onChange={(e) => setAiApiKey(e.target.value)}
              placeholder={
                settings?.ai_api_key_set ? '已配置（留空则保留当前密钥）' : '输入 API Key（sk-...）'
              }
              autoComplete="off"
              className={FIELD_INPUT_CLASS}
            />
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-ai-sub-chars" className="block text-xs font-medium text-ink-secondary">
              最大字幕上下文字符数
            </label>
            <input
              id="settings-ai-sub-chars"
              type="number"
              min={0}
              value={aiMaxSubtitleChars}
              onChange={(e) => setAiMaxSubtitleChars(e.target.value)}
              className={FIELD_INPUT_CLASS}
            />
          </div>

          <div className="space-y-1.5 sm:col-span-2 pt-3 border-t border-subtle">
            <div className="flex items-center gap-1.5 text-xs font-semibold text-accent-ink">
              <Mic className="h-3.5 w-3.5" />
              <span>语音转文字接口（用于无字幕视频自动转写并生成 AI 字幕）</span>
            </div>
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-asr-model" className="block text-xs font-medium text-ink-secondary">
              ASR 模型名称
            </label>
            <input
              id="settings-asr-model"
              type="text"
              value={aiAsrModel}
              onChange={(e) => setAiAsrModel(e.target.value)}
              placeholder="whisper-1（留空表示关闭自动转写）"
              className={FIELD_INPUT_CLASS}
            />
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-asr-base-url" className="block text-xs font-medium text-ink-secondary">
              ASR Base URL
            </label>
            <input
              id="settings-asr-base-url"
              type="url"
              value={aiAsrBaseUrl}
              onChange={(e) => setAiAsrBaseUrl(e.target.value)}
              placeholder="留空默认复用上方 AI Base URL"
              className={FIELD_INPUT_CLASS}
            />
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label htmlFor="settings-asr-key" className="block text-xs font-medium text-ink-secondary">
                ASR API Key
              </label>
              {settings?.ai_asr_api_key_set && (
                <label className="inline-flex items-center gap-1.5 text-[11px] text-ink-muted cursor-pointer">
                  <input
                    id="settings-asr-clear-key"
                    type="checkbox"
                    checked={clearAiAsrKey}
                    onChange={(e) => setClearAiAsrKey(e.target.checked)}
                    className="h-3.5 w-3.5 rounded border-default bg-card text-status-danger focus:ring-2 focus:ring-accent-ring"
                  />
                  <span>清除已存 Key</span>
                </label>
              )}
            </div>
            <input
              id="settings-asr-key"
              type="password"
              value={aiAsrApiKey}
              disabled={clearAiAsrKey}
              onChange={(e) => setAiAsrApiKey(e.target.value)}
              placeholder={
                settings?.ai_asr_api_key_set
                  ? '已配置（留空则保留当前密钥）'
                  : aiAsrBaseUrl.trim() === ''
                    ? '留空复用上方 AI API Key'
                    : '留空表示该服务无需认证'
              }
              autoComplete="off"
              className={FIELD_INPUT_CLASS}
            />
            <p className="text-[11px] text-ink-muted">
              {settings?.ai_asr_api_key_set && !clearAiAsrKey
                ? '已保存独立 ASR 密钥，留空将保留当前密钥；勾选上方「清除已存 Key」可清空'
                : aiAsrBaseUrl.trim() === ''
                  ? '留空复用上方 AI API Key'
                  : '留空表示该服务无需认证（不透传主 AI 密钥）'}
            </p>
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-asr-language" className="block text-xs font-medium text-ink-secondary">
              识别语言提示
            </label>
            <input
              id="settings-asr-language"
              type="text"
              list="settings-asr-language-presets"
              value={aiAsrLanguage}
              onChange={(e) => setAiAsrLanguage(e.target.value)}
              placeholder="留空为自动检测（如 zh、en）"
              className={FIELD_INPUT_CLASS}
            />
            <datalist id="settings-asr-language-presets">
              <option value="zh">中文 (zh)</option>
              <option value="en">英文 (en)</option>
            </datalist>
            <div className="flex flex-wrap items-center gap-1.5 text-[11px]">
              <span className="text-ink-muted">常用：</span>
              {[
                { label: '自动检测', value: '' },
                { label: '中文 (zh)', value: 'zh' },
                { label: '英文 (en)', value: 'en' },
              ].map((preset) => {
                const active = aiAsrLanguage.trim() === preset.value;
                return (
                  <button
                    key={preset.label}
                    type="button"
                    onClick={() => setAiAsrLanguage(preset.value)}
                    className={`rounded-lg border px-2 py-0.5 transition-colors cursor-pointer ${
                      active
                        ? 'border-accent/40 bg-accent-soft text-accent-ink font-medium'
                        : 'border-default bg-subtle text-ink-secondary hover:bg-muted hover:text-ink-primary'
                    }`}
                  >
                    {preset.label}
                  </button>
                );
              })}
            </div>
          </div>

          <div className="space-y-1.5 sm:col-span-2 pt-3 border-t border-subtle">
            <div className="flex items-center gap-1.5 text-xs font-semibold text-accent-ink">
              <ImagePlus className="h-3.5 w-3.5" />
              <span>AI 封面生图接口（用于为课程统一生成风格化封面）</span>
            </div>
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-image-protocol" className="block text-xs font-medium text-ink-secondary">
              生图接口协议
            </label>
            <select
              id="settings-image-protocol"
              value={aiImageProtocol}
              onChange={(e) => setAiImageProtocol(e.target.value as AIImageProtocol)}
              className={FIELD_INPUT_CLASS}
            >
              <option value="openai_images">OpenAI 标准生图 (/images/generations)</option>
              <option value="openai_chat">OpenAI 对话出图 (/chat/completions)</option>
              <option value="gemini">Google Gemini 原生 (:generateContent)</option>
            </select>
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-image-model" className="block text-xs font-medium text-ink-secondary">
              生图模型名称
            </label>
            <input
              id="settings-image-model"
              type="text"
              value={aiImageModel}
              onChange={(e) => setAiImageModel(e.target.value)}
              placeholder={
                aiImageProtocol === 'gemini'
                  ? 'gemini-2.5-flash-image（留空表示关闭 AI 生图）'
                  : aiImageProtocol === 'openai_chat'
                    ? 'google/gemini-2.5-flash-image 或 gpt-4o-image'
                    : 'gpt-image-1（留空表示关闭 AI 生图）'
              }
              className={FIELD_INPUT_CLASS}
            />
          </div>

          <div className="space-y-1.5">
            <label htmlFor="settings-image-base-url" className="block text-xs font-medium text-ink-secondary">
              生图 Base URL
            </label>
            <input
              id="settings-image-base-url"
              type="url"
              value={aiImageBaseUrl}
              onChange={(e) => setAiImageBaseUrl(e.target.value)}
              placeholder={
                aiImageProtocol === 'gemini'
                  ? '留空默认 https://generativelanguage.googleapis.com/v1beta'
                  : '留空默认复用上方 AI Base URL'
              }
              className={FIELD_INPUT_CLASS}
            />
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label htmlFor="settings-image-key" className="block text-xs font-medium text-ink-secondary">
                生图 API Key
              </label>
              {settings?.ai_image_api_key_set && (
                <label className="inline-flex items-center gap-1.5 text-[11px] text-ink-muted cursor-pointer">
                  <input
                    id="settings-image-clear-key"
                    type="checkbox"
                    checked={clearAiImageKey}
                    onChange={(e) => setClearAiImageKey(e.target.checked)}
                    className="h-3.5 w-3.5 rounded border-default bg-card text-status-danger focus:ring-2 focus:ring-accent-ring"
                  />
                  <span>清除已存 Key</span>
                </label>
              )}
            </div>
            <input
              id="settings-image-key"
              type="password"
              value={aiImageApiKey}
              disabled={clearAiImageKey}
              onChange={(e) => setAiImageApiKey(e.target.value)}
              placeholder={
                settings?.ai_image_api_key_set
                  ? '已配置（留空则保留当前密钥）'
                  : aiImageProtocol === 'gemini'
                    ? '填写 Google AI Studio API Key'
                    : aiImageBaseUrl.trim() === ''
                      ? '留空默认复用上方主 AI API Key'
                      : '输入该生图服务 API Key'
              }
              autoComplete="off"
              className={FIELD_INPUT_CLASS}
            />
            <p className="text-[11px] text-ink-muted">
              {settings?.ai_image_api_key_set && !clearAiImageKey
                ? '已保存独立生图密钥，留空将保留当前密钥；勾选上方「清除已存 Key」可清空'
                : aiImageProtocol === 'gemini' || aiImageBaseUrl.trim() !== ''
                  ? '独立地址或 Gemini 原生模式不会透传主 AI 密钥，请按需单独填写'
                  : '留空默认复用上方主 AI API Key'}
            </p>
          </div>

          <div className="space-y-1.5 sm:col-span-2">
            <label htmlFor="settings-image-size" className="block text-xs font-medium text-ink-secondary">
              默认出图尺寸 / 画幅
            </label>
            <div className="flex flex-col sm:flex-row sm:items-center gap-2">
              <input
                id="settings-image-size"
                type="text"
                value={aiImageSize}
                onChange={(e) => setAiImageSize(e.target.value)}
                placeholder="留空为自动适配横向 16:10 / 3:2（支持 1536x1024、1792x1024 等）"
                className={FIELD_INPUT_CLASS}
              />
              <div className="flex flex-wrap items-center gap-1.5 shrink-0 text-[11px]">
                {[
                  { label: '自动适配', value: '' },
                  { label: '1536x1024', value: '1536x1024' },
                  { label: '1792x1024', value: '1792x1024' },
                  { label: '1024x1024', value: '1024x1024' },
                ].map((preset) => {
                  const active = aiImageSize.trim() === preset.value;
                  return (
                    <button
                      key={preset.label}
                      type="button"
                      onClick={() => setAiImageSize(preset.value)}
                      className={`rounded-lg border px-2 py-1 transition-colors cursor-pointer ${
                        active
                          ? 'border-accent/40 bg-accent-soft text-accent-ink font-medium'
                          : 'border-default bg-subtle text-ink-secondary hover:bg-muted hover:text-ink-primary'
                      }`}
                    >
                      {preset.label}
                    </button>
                  );
                })}
              </div>
            </div>
          </div>

          <div className="space-y-2 sm:col-span-2">
            <span className="block text-xs font-medium text-ink-secondary">
              全局默认封面风格（用于单课与批量生图默认选中）
            </span>
            <CoverStylePicker
              styles={coverStyles}
              value={aiCoverDefaultStyle}
              onChange={setAiCoverDefaultStyle}
              defaultStyleId={aiCoverDefaultStyle}
              ariaLabel="全局默认封面风格"
            />
          </div>
        </div>

        {aiTestMessage && (
          <div
            className={`rounded-2xl border px-3.5 py-2.5 text-xs font-medium ${
              aiTestMessage.ok
                ? 'border-status-success/30 bg-status-success-soft text-status-success-ink'
                : 'border-status-danger/30 bg-status-danger-soft text-status-danger-ink'
            }`}
          >
            {aiTestMessage.text}
          </div>
        )}

        {asrTestMessage && (
          <div
            className={`rounded-2xl border px-3.5 py-2.5 text-xs font-medium ${
              asrTestMessage.ok
                ? 'border-status-success/30 bg-status-success-soft text-status-success-ink'
                : 'border-status-danger/30 bg-status-danger-soft text-status-danger-ink'
            }`}
          >
            {asrTestMessage.text}
          </div>
        )}

        {imageTestMessage && (
          <div
            className={`rounded-2xl border px-3.5 py-2.5 text-xs font-medium ${
              imageTestMessage.ok
                ? 'border-status-success/30 bg-status-success-soft text-status-success-ink'
                : 'border-status-danger/30 bg-status-danger-soft text-status-danger-ink'
            }`}
          >
            {imageTestMessage.text}
          </div>
        )}

        {settingsSuccess && (
          <div className="rounded-2xl border border-status-success/30 bg-status-success-soft px-3.5 py-2.5 text-xs font-medium text-status-success-ink">
            {settingsSuccess}
          </div>
        )}

        <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
          <div className="flex flex-wrap items-center gap-2.5">
            <Tooltip content="验证当前填写的 AI 接口地址、模型与密钥是否可用">
              <button
                type="button"
                disabled={testingAI}
                onClick={() => void handleTestAI()}
                className="inline-flex items-center gap-1.5 rounded-xl border border-default bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
              >
                {testingAI ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Sparkles className="h-3.5 w-3.5" />}
                <span>{testingAI ? '测试中...' : '测试连接'}</span>
              </button>
            </Tooltip>

            <Tooltip content="发送一段 0.5 秒静音音频验证当前语音转文字接口与模型是否可用">
              <button
                type="button"
                disabled={testingASR}
                onClick={() => void handleTestASR()}
                className="inline-flex items-center gap-1.5 rounded-xl border border-default bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
              >
                {testingASR ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Mic className="h-3.5 w-3.5" />}
                <span>{testingASR ? '转写测试中...' : '测试语音转文字'}</span>
              </button>
            </Tooltip>

            <Tooltip content="请求当前填写的生图接口验证能否正常返回图片">
              <button
                type="button"
                disabled={testingImage}
                onClick={() => void handleTestImage()}
                className="inline-flex items-center gap-1.5 rounded-xl border border-default bg-subtle px-3.5 py-2 text-xs font-medium text-ink-secondary hover:bg-muted hover:text-ink-primary disabled:opacity-50 transition-colors cursor-pointer"
              >
                {testingImage ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ImagePlus className="h-3.5 w-3.5" />}
                <span>{testingImage ? '生图测试中...' : '测试生图接口'}</span>
              </button>
            </Tooltip>
          </div>

          <Tooltip content="保存服务器名称、定时扫描、AI 与语音转文字接口配置">
            <button
              type="submit"
              disabled={savingSettings}
              className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-4 py-2 text-sm font-semibold text-ink-inverse shadow-accent hover:bg-accent-hover disabled:opacity-50 transition-colors cursor-pointer"
            >
              {savingSettings ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              <span>{savingSettings ? '保存中...' : '保存设置'}</span>
            </button>
          </Tooltip>
        </div>
      </form>

      {/* Admin Password Change */}
      <form
        onSubmit={handleChangePassword}
        className="rounded-3xl bg-card border border-subtle shadow-card p-6 space-y-4"
      >
        <div className="flex items-center gap-3">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
            <KeyRound className="h-4 w-4" />
          </span>
          <h2 className="text-base font-semibold text-ink-primary">修改管理员密码</h2>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div className="space-y-1.5">
            <label htmlFor="pwd-old" className="block text-xs font-medium text-ink-secondary">
              当前密码
            </label>
            <input
              id="pwd-old"
              type="password"
              value={oldPassword}
              onChange={(e) => setOldPassword(e.target.value)}
              className={FIELD_INPUT_CLASS}
            />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="pwd-new" className="block text-xs font-medium text-ink-secondary">
              新密码
            </label>
            <input
              id="pwd-new"
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              className={FIELD_INPUT_CLASS}
            />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="pwd-confirm" className="block text-xs font-medium text-ink-secondary">
              确认新密码
            </label>
            <input
              id="pwd-confirm"
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              className={FIELD_INPUT_CLASS}
            />
          </div>
        </div>

        {passwordFeedback && (
          <p
            className={`text-xs font-medium ${
              passwordFeedback.ok ? 'text-status-success-ink' : 'text-status-danger-ink'
            }`}
          >
            {passwordFeedback.text}
          </p>
        )}

        <div className="flex justify-end">
          <Tooltip content="更新管理员密码（同时影响 Jellyfin 客户端新登录）">
            <button
              type="submit"
              disabled={changingPassword}
              className="rounded-xl border border-default bg-subtle px-4 py-2 text-xs font-semibold text-ink-primary hover:bg-muted disabled:opacity-50 transition-colors cursor-pointer"
            >
              {changingPassword ? '更新中...' : '更新密码'}
            </button>
          </Tooltip>
        </div>
      </form>

      {/* Active Sessions */}
      <section className="rounded-3xl bg-card border border-subtle shadow-card p-6 space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
              <Laptop className="h-4 w-4" />
            </span>
            <h2 className="text-base font-semibold text-ink-primary">
              已登录设备会话 ({sessions.length})
            </h2>
          </div>
        </div>

        {sessions.length === 0 ? (
          <p className="text-xs text-ink-muted py-6 text-center rounded-2xl border border-dashed border-default">
            暂无活动会话。
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead>
                <tr className="border-b border-subtle text-ink-muted">
                  <th className="py-2.5 pr-4 font-medium">客户端</th>
                  <th className="py-2.5 pr-4 font-medium">设备名称</th>
                  <th className="py-2.5 pr-4 font-medium">设备 ID</th>
                  <th className="py-2.5 pr-4 font-medium">最近活跃</th>
                  <th className="py-2.5 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-subtle">
                {sessions.map((sess) => {
                  const isCurrent = currentToken && sess.token === currentToken;
                  return (
                    <tr key={sess.token} className="text-ink-primary hover:bg-subtle/40 transition-colors">
                      <td className="py-3 pr-4 font-medium">
                        {sess.client_name} {sess.client_version}
                        {isCurrent && (
                          <span className="ml-2 inline-flex items-center rounded-full border border-status-success/30 bg-status-success-soft px-2 py-0.5 text-[10px] font-medium text-status-success-ink">
                            当前会话
                          </span>
                        )}
                      </td>
                      <td className="py-3 pr-4 text-ink-secondary">{sess.device_name || '未知设备'}</td>
                      <td className="py-3 pr-4 font-mono text-ink-muted">{sess.device_id}</td>
                      <td className="py-3 pr-4 text-ink-muted">{sess.last_active_at}</td>
                      <td className="py-3 text-right">
                        <Tooltip content="强制下线该设备会话，需重新登录">
                          <button
                            type="button"
                            onClick={() => void handleRevokeSession(sess.token)}
                            className="inline-flex items-center gap-1 rounded-lg border border-status-danger/30 bg-status-danger-soft px-2.5 py-1 text-[11px] font-medium text-status-danger-ink hover:opacity-85 transition-colors cursor-pointer"
                          >
                            <Trash2 className="h-3 w-3" />
                            <span>注销</span>
                          </button>
                        </Tooltip>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
