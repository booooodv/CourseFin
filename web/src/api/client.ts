import type {
  ApplyAIProposalInput,
  AuthMeResponse,
  BindSubtitleInput,
  ChapterDetail,
  CommitImportInput,
  CourseDetail,
  CourseFilterParams,
  CourseOrganizeProposal,
  CourseSummary,
  CreateCourseInput,
  DeleteLibraryOptions,
  FsBrowseResponse,
  HealthStatus,
  HomeResponse,
  ImportAIOrganizeInput,
  ImportOrganizeProposal,
  ImportPreview,
  IndexedDir,
  IndexedSubtitle,
  LessonDetail,
  LessonPlacement,
  LessonPlaybackInfo,
  Library,
  LoginRequest,
  LoginResponse,
  MediaFile,
  ProgressResponse,
  ScanReport,
  ScanState,
  Session,
  SettingsPayload,
  SetupRequest,
  SetupResponse,
  SetupStatusResponse,
  StorageSource,
  Tag,
  UpdateConfigInput,
  UpdateCourseInput,
  UpdateSettingsInput,
  UpdateStatus,
} from '../types/coursefin';

export const TOKEN_STORAGE_KEY = 'coursefin_token';

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_STORAGE_KEY);
  } catch {
    return null;
  }
}

export function setToken(token: string): void {
  try {
    localStorage.setItem(TOKEN_STORAGE_KEY, token);
  } catch {
    // ignore storage errors in restricted environments
  }
}

export function clearToken(): void {
  try {
    localStorage.removeItem(TOKEN_STORAGE_KEY);
  } catch {
    // ignore storage errors
  }
}

export function withApiKey(url: string): string {
  const token = getToken();
  if (!token || !url) {
    return url;
  }
  if (/[?&]api_key=/i.test(url)) {
    return url;
  }
  const sep = url.includes('?') ? '&' : '?';
  return `${url}${sep}api_key=${encodeURIComponent(token)}`;
}

export function itemPrimaryImageUrl(itemId: string, tag?: string, maxWidth = 480): string {
  const base = `/Items/${encodeURIComponent(itemId)}/Images/Primary`;
  if (tag) {
    return `${base}?tag=${encodeURIComponent(tag)}&maxWidth=${maxWidth}`;
  }
  return `${base}?maxWidth=${maxWidth}`;
}

export function itemBackdropImageUrl(itemId: string, tag?: string, maxWidth = 1280): string {
  const base = `/Items/${encodeURIComponent(itemId)}/Images/Backdrop`;
  if (tag) {
    return `${base}?tag=${encodeURIComponent(tag)}&maxWidth=${maxWidth}`;
  }
  return `${base}?maxWidth=${maxWidth}`;
}

function redirectToLoginOnUnauthorized(): void {
  clearToken();
  if (typeof window !== 'undefined' && window.location) {
    const currentPath = window.location.pathname;
    if (currentPath !== '/web/login' && currentPath !== '/login') {
      window.location.assign('/web/login');
    }
  }
}

async function request<T>(
  path: string,
  options: RequestInit = {},
  authRequired = true,
): Promise<T> {
  const headers = new Headers(options.headers);
  const isFormData = typeof FormData !== 'undefined' && options.body instanceof FormData;
  if (!isFormData && options.body !== undefined && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }

  if (authRequired) {
    const token = getToken();
    if (token && !headers.has('Authorization')) {
      headers.set('Authorization', `Bearer ${token}`);
    }
  }

  const res = await fetch(path, {
    ...options,
    headers,
  });

  if (res.status === 401 && authRequired) {
    redirectToLoginOnUnauthorized();
  }

  const contentType = res.headers.get('Content-Type') ?? '';
  const isJson = contentType.includes('application/json');

  if (!res.ok) {
    let message = `请求失败 (${res.status})`;
    if (isJson) {
      try {
        const errBody = (await res.json()) as { error?: string; message?: string };
        if (errBody.error) {
          message = errBody.error;
        } else if (errBody.message) {
          message = errBody.message;
        }
      } catch {
        // keep default message
      }
    }
    throw new ApiError(res.status, message);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  return (await res.json()) as T;
}

function toQueryString(params: Record<string, string | number | boolean | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') {
      search.set(key, String(value));
    }
  }
  const qs = search.toString();
  return qs ? `?${qs}` : '';
}

export const api = {
  // Setup & Auth
  getSetupStatus(): Promise<SetupStatusResponse> {
    return request<SetupStatusResponse>('/api/v1/setup/status', {}, false);
  },
  setup(input: SetupRequest): Promise<SetupResponse> {
    return request<SetupResponse>(
      '/api/v1/setup',
      { method: 'POST', body: JSON.stringify(input) },
      false,
    );
  },
  login(input: LoginRequest): Promise<LoginResponse> {
    return request<LoginResponse>(
      '/api/v1/auth/login',
      {
        method: 'POST',
        body: JSON.stringify({
          client_name: 'CourseFin Web',
          client_version: '0.1.0',
          ...input,
        }),
      },
      false,
    );
  },
  logout(): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>('/api/v1/auth/logout', { method: 'POST' });
  },
  getMe(): Promise<AuthMeResponse> {
    return request<AuthMeResponse>('/api/v1/auth/me');
  },
  changePassword(oldPassword: string, newPassword: string): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>('/api/v1/auth/password', {
      method: 'POST',
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
    });
  },
  listSessions(): Promise<{ sessions: Session[] }> {
    return request<{ sessions: Session[] }>('/api/v1/sessions');
  },
  revokeSession(token: string): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>(`/api/v1/sessions/${encodeURIComponent(token)}`, {
      method: 'DELETE',
    });
  },

  // Home, Libraries, Tags
  getHome(): Promise<HomeResponse> {
    return request<HomeResponse>('/api/v1/home');
  },
  listLibraries(): Promise<{ libraries: Library[] }> {
    return request<{ libraries: Library[] }>('/api/v1/libraries');
  },
  createLibrary(name: string): Promise<Library> {
    return request<Library>('/api/v1/libraries', {
      method: 'POST',
      body: JSON.stringify({ name }),
    });
  },
  updateLibrary(id: string, name: string): Promise<Library> {
    return request<Library>(`/api/v1/libraries/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify({ name }),
    });
  },
  deleteLibrary(id: string, options?: DeleteLibraryOptions): Promise<{ ok: boolean }> {
    const qs = toQueryString({
      move_to_library_id: options?.moveToLibraryId,
      force: options?.force ? 'true' : undefined,
    });
    return request<{ ok: boolean }>(`/api/v1/libraries/${encodeURIComponent(id)}${qs}`, {
      method: 'DELETE',
    });
  },
  listTags(): Promise<{ tags: Tag[] }> {
    return request<{ tags: Tag[] }>('/api/v1/tags');
  },

  // Courses, Chapters, Lessons
  listCourses(filter: CourseFilterParams = {}): Promise<{ courses: CourseSummary[] }> {
    const qs = toQueryString({
      library_id: filter.library_id,
      tag: filter.tag,
      search: filter.search,
      is_played: filter.is_played,
      sort_by: filter.sort_by,
      sort_desc: filter.sort_desc,
      limit: filter.limit,
      offset: filter.offset,
    });
    return request<{ courses: CourseSummary[] }>(`/api/v1/courses${qs}`);
  },
  createCourse(input: CreateCourseInput): Promise<CourseDetail> {
    return request<CourseDetail>('/api/v1/courses', {
      method: 'POST',
      body: JSON.stringify(input),
    });
  },
  getCourse(id: string): Promise<CourseDetail> {
    return request<CourseDetail>(`/api/v1/courses/${encodeURIComponent(id)}`);
  },
  updateCourse(id: string, input: UpdateCourseInput): Promise<CourseDetail> {
    return request<CourseDetail>(`/api/v1/courses/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    });
  },
  deleteCourse(id: string): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>(`/api/v1/courses/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    });
  },
  uploadCourseCover(id: string, file: File): Promise<CourseDetail> {
    const form = new FormData();
    form.append('file', file);
    return request<CourseDetail>(`/api/v1/courses/${encodeURIComponent(id)}/cover`, {
      method: 'POST',
      body: form,
    });
  },
  pickCourseCoverFromLesson(id: string, lessonId: string): Promise<CourseDetail> {
    return request<CourseDetail>(`/api/v1/courses/${encodeURIComponent(id)}/cover`, {
      method: 'POST',
      body: JSON.stringify({ lesson_id: lessonId }),
    });
  },
  uploadCourseBackdrop(id: string, file: File): Promise<CourseDetail> {
    const form = new FormData();
    form.append('file', file);
    return request<CourseDetail>(`/api/v1/courses/${encodeURIComponent(id)}/backdrop`, {
      method: 'POST',
      body: form,
    });
  },
  getCourseNewFiles(id: string): Promise<{ files: MediaFile[] }> {
    return request<{ files: MediaFile[] }>(`/api/v1/courses/${encodeURIComponent(id)}/new-files`);
  },
  appendCourseNewFiles(id: string, mediaFileIds: string[]): Promise<CourseDetail> {
    return request<CourseDetail>(`/api/v1/courses/${encodeURIComponent(id)}/new-files`, {
      method: 'POST',
      body: JSON.stringify({ media_file_ids: mediaFileIds }),
    });
  },
  createChapter(courseId: string, title: string): Promise<ChapterDetail> {
    return request<ChapterDetail>(`/api/v1/courses/${encodeURIComponent(courseId)}/chapters`, {
      method: 'POST',
      body: JSON.stringify({ title }),
    });
  },
  updateChapter(chapterId: string, title: string, overview = ''): Promise<ChapterDetail> {
    return request<ChapterDetail>(`/api/v1/chapters/${encodeURIComponent(chapterId)}`, {
      method: 'PUT',
      body: JSON.stringify({ title, overview }),
    });
  },
  deleteChapter(chapterId: string): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>(`/api/v1/chapters/${encodeURIComponent(chapterId)}`, {
      method: 'DELETE',
    });
  },
  reorderChapters(courseId: string, chapterIds: string[]): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>(`/api/v1/courses/${encodeURIComponent(courseId)}/chapters/reorder`, {
      method: 'PUT',
      body: JSON.stringify({ chapter_ids: chapterIds }),
    });
  },
  addLesson(chapterId: string, mediaFileId: string, title?: string): Promise<LessonDetail> {
    return request<LessonDetail>(`/api/v1/chapters/${encodeURIComponent(chapterId)}/lessons`, {
      method: 'POST',
      body: JSON.stringify({ media_file_id: mediaFileId, title: title ?? '' }),
    });
  },
  updateLesson(lessonId: string, title: string, overview = '', chapterId = ''): Promise<LessonDetail> {
    return request<LessonDetail>(`/api/v1/lessons/${encodeURIComponent(lessonId)}`, {
      method: 'PUT',
      body: JSON.stringify({ title, overview, chapter_id: chapterId }),
    });
  },
  deleteLesson(lessonId: string): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>(`/api/v1/lessons/${encodeURIComponent(lessonId)}`, {
      method: 'DELETE',
    });
  },
  reorderLessons(courseId: string, placements: LessonPlacement[]): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>(`/api/v1/courses/${encodeURIComponent(courseId)}/lessons/reorder`, {
      method: 'PUT',
      body: JSON.stringify({ placements }),
    });
  },

  // Playback & Progress
  getLessonPlayback(
    lessonId: string,
    options?: { codecs?: string[]; forceTranscode?: boolean },
  ): Promise<LessonPlaybackInfo> {
    const qs = toQueryString({
      codecs: options?.codecs && options.codecs.length > 0 ? options.codecs.join(',') : undefined,
      force_transcode: options?.forceTranscode ? '1' : undefined,
    });
    return request<LessonPlaybackInfo>(`/api/v1/lessons/${encodeURIComponent(lessonId)}/playback${qs}`);
  },
  reportProgress(
    lessonId: string,
    positionTicks: number,
    options?: { keepalive?: boolean },
  ): Promise<ProgressResponse> {
    return request<ProgressResponse>(`/api/v1/lessons/${encodeURIComponent(lessonId)}/progress`, {
      method: 'POST',
      body: JSON.stringify({ position_ticks: positionTicks }),
      keepalive: options?.keepalive,
    });
  },
  setLessonPlayed(lessonId: string, played = true): Promise<ProgressResponse> {
    return request<ProgressResponse>(`/api/v1/lessons/${encodeURIComponent(lessonId)}/played`, {
      method: 'POST',
      body: JSON.stringify({ played }),
    });
  },
  async stopActiveEncoding(playSessionId: string): Promise<void> {
    if (!playSessionId) return;
    const url = withApiKey(
      `/Videos/ActiveEncodings?PlaySessionId=${encodeURIComponent(playSessionId)}`,
    );
    try {
      await fetch(url, { method: 'DELETE', keepalive: true });
    } catch {
      // Best-effort cleanup; never trigger 401 redirect on unmount/unload
    }
  },

  // Storage & Import
  browseFsDirs(path?: string, showHidden = false): Promise<FsBrowseResponse> {
    const qs = toQueryString({
      path: path?.trim() || undefined,
      show_hidden: showHidden ? 'true' : undefined,
    });
    return request<FsBrowseResponse>(`/api/v1/fs/dirs${qs}`);
  },
  listSources(): Promise<{ sources: StorageSource[] }> {
    return request<{ sources: StorageSource[] }>('/api/v1/sources');
  },
  createSource(name: string, driver: string, rootPath: string): Promise<StorageSource> {
    return request<StorageSource>('/api/v1/sources', {
      method: 'POST',
      body: JSON.stringify({ name, driver, root_path: rootPath }),
    });
  },
  deleteSource(id: string): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>(`/api/v1/sources/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    });
  },
  scanSource(id: string, asyncMode = true): Promise<ScanState | ScanReport> {
    const qs = asyncMode ? '?async=true' : '';
    return request<ScanState | ScanReport>(`/api/v1/sources/${encodeURIComponent(id)}/scan${qs}`, {
      method: 'POST',
    });
  },
  getScanStatus(id: string): Promise<ScanState> {
    return request<ScanState>(`/api/v1/sources/${encodeURIComponent(id)}/scan/status`);
  },
  listDirs(sourceId: string, prefix = ''): Promise<{ dirs: IndexedDir[] }> {
    const qs = toQueryString({ prefix });
    return request<{ dirs: IndexedDir[] }>(`/api/v1/sources/${encodeURIComponent(sourceId)}/dirs${qs}`);
  },
  listFiles(
    sourceId: string,
    params: { prefix?: string; search?: string; status?: string; assigned?: 'true' | 'false' } = {},
  ): Promise<{ files: MediaFile[] }> {
    const qs = toQueryString(params);
    return request<{ files: MediaFile[] }>(`/api/v1/sources/${encodeURIComponent(sourceId)}/files${qs}`);
  },
  listSourceSubtitles(
    sourceId: string,
    params: { unmatched?: boolean; prefix?: string; search?: string } = {},
  ): Promise<{ subtitles: IndexedSubtitle[] }> {
    const qs = toQueryString({
      unmatched: params.unmatched !== undefined ? String(params.unmatched) : undefined,
      prefix: params.prefix,
      search: params.search,
    });
    return request<{ subtitles: IndexedSubtitle[] }>(
      `/api/v1/sources/${encodeURIComponent(sourceId)}/subtitles${qs}`,
    );
  },
  bindSubtitle(subtitleId: string, input: BindSubtitleInput): Promise<IndexedSubtitle> {
    return request<IndexedSubtitle>(`/api/v1/subtitles/${encodeURIComponent(subtitleId)}/bind`, {
      method: 'PUT',
      body: JSON.stringify(input),
    });
  },
  previewImport(sourceId: string, dirRelPath: string): Promise<ImportPreview> {
    return request<ImportPreview>('/api/v1/import/preview', {
      method: 'POST',
      body: JSON.stringify({ source_id: sourceId, dir_rel_path: dirRelPath }),
    });
  },
  aiOrganizeImport(input: ImportAIOrganizeInput): Promise<ImportOrganizeProposal> {
    return request<ImportOrganizeProposal>('/api/v1/import/ai/organize', {
      method: 'POST',
      body: JSON.stringify(input),
    });
  },
  commitImport(input: CommitImportInput): Promise<CourseDetail> {
    return request<CourseDetail>('/api/v1/import/commit', {
      method: 'POST',
      body: JSON.stringify(input),
    });
  },

  // Settings & AI
  getSettings(): Promise<SettingsPayload> {
    return request<SettingsPayload>('/api/v1/settings');
  },
  updateSettings(input: UpdateSettingsInput): Promise<SettingsPayload> {
    return request<SettingsPayload>('/api/v1/settings', {
      method: 'PUT',
      body: JSON.stringify(input),
    });
  },
  testAI(overrides: Partial<{
    base_url: string;
    api_key: string;
    model: string;
    max_subtitle_chars: number;
  }> = {}): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>('/api/v1/settings/ai/test', {
      method: 'POST',
      body: JSON.stringify(overrides),
    });
  },
  aiOrganizeCourse(courseId: string): Promise<CourseOrganizeProposal> {
    return request<CourseOrganizeProposal>(`/api/v1/courses/${encodeURIComponent(courseId)}/ai/organize`, {
      method: 'POST',
    });
  },
  aiApplyCourse(courseId: string, input: ApplyAIProposalInput): Promise<CourseDetail> {
    return request<CourseDetail>(`/api/v1/courses/${encodeURIComponent(courseId)}/ai/apply`, {
      method: 'POST',
      body: JSON.stringify(input),
    });
  },

  // System Update & Health
  getUpdateStatus(): Promise<UpdateStatus> {
    return request<UpdateStatus>('/api/v1/system/update', { cache: 'no-store' });
  },
  updateUpdateConfig(input: UpdateConfigInput): Promise<UpdateStatus> {
    return request<UpdateStatus>('/api/v1/system/update/config', {
      method: 'PUT',
      body: JSON.stringify(input),
    });
  },
  checkForUpdate(): Promise<UpdateStatus> {
    return request<UpdateStatus>('/api/v1/system/update/check', {
      method: 'POST',
    });
  },
  applyUpdate(): Promise<UpdateStatus> {
    return request<UpdateStatus>('/api/v1/system/update/apply', {
      method: 'POST',
    });
  },
  getHealth(): Promise<HealthStatus> {
    return request<HealthStatus>('/health', { cache: 'no-store' }, false);
  },
};
