export interface User {
  id: string;
  username: string;
  created_at: string;
  updated_at: string;
}

export interface Session {
  token: string;
  user_id: string;
  device_id: string;
  device_name: string;
  client_name: string;
  client_version: string;
  last_active_at: string;
  created_at: string;
}

export interface SetupStatusResponse {
  needs_setup: boolean;
}

export interface SetupRequest {
  username: string;
  password: string;
  server_name?: string;
}

export interface SetupResponse {
  user: User;
  needs_setup: boolean;
}

export interface LoginRequest {
  username: string;
  password: string;
  device_id?: string;
  device_name?: string;
  client_name?: string;
  client_version?: string;
}

export interface LoginResponse {
  token: string;
  user: User;
  session: Session;
}

export interface AuthMeResponse {
  user: User;
  session: Session;
}

export interface Library {
  id: string;
  name: string;
  cover_path: string;
  sort_order: number;
  course_count: number;
  created_at: string;
  updated_at: string;
}

export interface DeleteLibraryOptions {
  moveToLibraryId?: string;
  force?: boolean;
}

export interface FsDirEntry {
  name: string;
  path: string;
}

export interface FsRootShortcut {
  label: string;
  path: string;
}

export interface FsBrowseResponse {
  current_path: string;
  parent_path: string;
  entries: FsDirEntry[];
  roots: FsRootShortcut[];
}

export interface Tag {
  id: string;
  name: string;
  course_count: number;
}

export interface CourseSummary {
  id: string;
  library_id: string;
  title: string;
  sort_title: string;
  overview: string;
  instructor: string;
  cover_path: string;
  backdrop_path: string;
  source_id?: string;
  source_root_path?: string;
  tags: string[];
  chapter_count: number;
  lesson_count: number;
  played_count: number;
  unplayed_item_count: number;
  played_percentage: number;
  played: boolean;
  total_duration_ms: number;
  first_lesson_id?: string;
  created_at: string;
  updated_at: string;
}

export interface SubtitleInfo {
  id: string;
  language: string;
  title: string;
  format: string;
  is_default: boolean;
  status: string;
}

export interface LessonDetail {
  id: string;
  course_id: string;
  chapter_id: string;
  media_file_id: string;
  source_id?: string;
  title: string;
  overview: string;
  sort_order: number;
  cover_path: string;
  duration_ms: number;
  container: string;
  video_codec: string;
  audio_codec: string;
  width: number;
  height: number;
  bitrate: number;
  thumbnail_path: string;
  media_status: string;
  position_ticks: number;
  played: boolean;
  play_count: number;
  last_played_at?: string;
  subtitles?: SubtitleInfo[];
  created_at: string;
  updated_at: string;
}

export interface ChapterDetail {
  id: string;
  course_id: string;
  title: string;
  sort_order: number;
  overview: string;
  lesson_count: number;
  played_count: number;
  unplayed_item_count: number;
  played_percentage: number;
  played: boolean;
  lessons: LessonDetail[];
  created_at: string;
  updated_at: string;
}

export type CourseCoverSource =
  | 'ai'
  | 'ai_batch'
  | 'upload'
  | 'frame'
  | 'import'
  | 'legacy';

export interface CourseCover {
  id: string;
  course_id: string;
  file_path: string;
  source: CourseCoverSource | string;
  style_id?: string;
  style_name?: string;
  prompt?: string;
  width: number;
  height: number;
  is_active: boolean;
  image_url: string;
  created_at: string;
}

export interface CourseDetail extends CourseSummary {
  chapters: ChapterDetail[];
  covers?: CourseCover[];
}

export interface LessonWithContext extends LessonDetail {
  course_title: string;
  course_cover_path: string;
  library_id: string;
  chapter_title: string;
  chapter_sort_order: number;
}

export interface HomeResponse {
  resume: LessonWithContext[];
  next_up: LessonWithContext[];
  libraries: Library[];
  courses: CourseSummary[];
}

export interface CourseFilterParams {
  library_id?: string;
  tag?: string;
  search?: string;
  is_played?: boolean;
  sort_by?: string;
  sort_desc?: boolean;
  limit?: number;
  offset?: number;
}

export interface CreateCourseInput {
  library_id: string;
  title: string;
  sort_title?: string;
  overview?: string;
  instructor?: string;
  tags?: string[];
}

export interface UpdateCourseInput {
  library_id?: string;
  title?: string;
  sort_title?: string;
  overview?: string;
  instructor?: string;
  cover_path?: string;
  backdrop_path?: string;
  tags?: string[];
}

export interface LessonPlacement {
  lesson_id: string;
  chapter_id: string;
  sort_order: number;
}

export interface PlaybackSubtitleItem {
  index: number;
  language: string;
  title: string;
  format: string;
  is_default: boolean;
  is_external: boolean;
  is_text: boolean;
  url: string;
}

export interface PlaybackProgressInfo {
  position_ticks: number;
  played: boolean;
  play_count: number;
  last_played_at?: string;
}

export interface LessonPlaybackInfo {
  lesson_id: string;
  course_id: string;
  media_file_id: string;
  play_session_id: string;
  mode: 'direct' | 'hls' | 'unsupported';
  stream_url: string;
  transcoding?: boolean;
  subtitles: PlaybackSubtitleItem[];
  progress: PlaybackProgressInfo;
  video_codec: string;
  audio_codec: string;
  container: string;
  duration_ms: number;
}

export interface ProgressResponse {
  lesson_id: string;
  position_ticks: number;
  played: boolean;
  play_count: number;
  last_played_at?: string;
  updated_at: string;
}

export interface CourseRef {
  id: string;
  title: string;
}

export interface StorageSource {
  id: string;
  name: string;
  driver: string;
  root_path: string;
  config_json: string;
  enabled: boolean;
  last_scanned_at?: string;
  media_count: number;
  assigned_count?: number;
  unassigned_count?: number;
  created_at: string;
  updated_at: string;
}

export interface ScanReport {
  source_id: string;
  started_at: string;
  finished_at: string;
  total_videos: number;
  added_videos: number;
  updated_videos: number;
  skipped_videos: number;
  missing_videos: number;
  restored_videos: number;
  probed_videos: number;
  total_subtitles: number;
  matched_subtitles?: number;
  errors?: string[];
}

export interface ScanState {
  source_id: string;
  running: boolean;
  started_at?: string;
  last_report?: ScanReport;
  last_error?: string;
}

export interface IndexedDir {
  rel_path: string;
  name: string;
  file_count: number;
  direct_files: number;
  assigned_count?: number;
  unassigned_count?: number;
  courses?: CourseRef[];
  has_subdirs: boolean;
}

export interface IndexedSubtitle {
  id: string;
  source_id: string;
  relative_path: string;
  file_name: string;
  media_file_id?: string;
  language: string;
  title: string;
  format: string;
  is_default: boolean;
  manually_bound: boolean;
  status: string;
}

export interface BindSubtitleInput {
  media_file_id?: string | null;
  title?: string;
  language?: string;
  is_default?: boolean;
  reset_auto?: boolean;
}

export interface MediaFile {
  id: string;
  source_id: string;
  relative_path: string;
  file_name: string;
  file_size: number;
  mod_time: string;
  status: string;
  duration_ms: number;
  container: string;
  video_codec: string;
  audio_codec: string;
  width: number;
  height: number;
  bitrate: number;
  thumbnail_path: string;
  probed_at?: string;
  assigned?: boolean;
  courses?: CourseRef[];
  created_at: string;
  updated_at: string;
}

export interface PreviewLesson {
  media_file_id: string;
  relative_path: string;
  file_name: string;
  title: string;
  duration_ms: number;
  sort_order: number;
}

export interface PreviewChapter {
  title: string;
  sort_order: number;
  lessons: PreviewLesson[];
}

export interface ImportPreview {
  source_id: string;
  dir_rel_path: string;
  course_title: string;
  cover_rel_path?: string;
  chapters: PreviewChapter[];
  total_files: number;
}

export interface CommitImportLesson {
  media_file_id: string;
  title: string;
}

export interface CommitImportChapter {
  title: string;
  lessons: CommitImportLesson[];
}

export interface CommitImportInput {
  source_id: string;
  dir_rel_path: string;
  library_id: string;
  title: string;
  overview?: string;
  instructor?: string;
  tags?: string[];
  cover_rel_path?: string;
  chapters: CommitImportChapter[];
}

export type AIImageProtocol = 'openai_images' | 'openai_chat' | 'gemini';

export interface CoverStylePalette {
  background: string;
  surface: string;
  text: string;
  accent: string;
  accent_soft: string;
}

export interface CoverStyleTemplate {
  id: string;
  name: string;
  tagline: string;
  description: string;
  recommended_for: string;
  preview_image_url: string;
  palette: CoverStylePalette;
}

export interface CoverStylesResponse {
  styles: CoverStyleTemplate[];
  default_style_id: string;
  ai_image_configured: boolean;
}

export interface SettingsPayload {
  server_name: string;
  scan_interval_minutes: number;
  base_url_hint: string;
  jellyfin_username: string;
  ai_base_url: string;
  ai_api_key: string;
  ai_api_key_set: boolean;
  ai_model: string;
  ai_max_subtitle_chars: number;
  ai_asr_base_url?: string;
  ai_asr_api_key?: string;
  ai_asr_api_key_set?: boolean;
  ai_asr_model?: string;
  ai_asr_language?: string;
  ai_configured?: boolean;
  ai_asr_configured?: boolean;
  ai_image_protocol?: AIImageProtocol;
  ai_image_base_url?: string;
  ai_image_api_key?: string;
  ai_image_api_key_set?: boolean;
  ai_image_model?: string;
  ai_image_size?: string;
  ai_image_configured?: boolean;
  ai_cover_default_style?: string;
}

export interface UpdateSettingsInput {
  server_name?: string;
  scan_interval_minutes?: number;
  ai_base_url?: string;
  ai_api_key?: string;
  clear_ai_api_key?: boolean;
  ai_model?: string;
  ai_max_subtitle_chars?: number;
  ai_asr_base_url?: string;
  ai_asr_api_key?: string;
  clear_ai_asr_api_key?: boolean;
  ai_asr_model?: string;
  ai_asr_language?: string;
  ai_image_protocol?: AIImageProtocol;
  ai_image_base_url?: string;
  ai_image_api_key?: string;
  clear_ai_image_api_key?: boolean;
  ai_image_model?: string;
  ai_image_size?: string;
  ai_cover_default_style?: string;
}

export interface TestAIImageOverrides {
  image_protocol?: string;
  ai_image_protocol?: string;
  image_base_url?: string;
  ai_image_base_url?: string;
  image_api_key?: string;
  ai_image_api_key?: string;
  clear_ai_image_api_key?: boolean;
  clear_image_api_key?: boolean;
  image_model?: string;
  ai_image_model?: string;
  image_size?: string;
  ai_image_size?: string;
  base_url?: string;
  ai_base_url?: string;
  api_key?: string;
  ai_api_key?: string;
  clear_ai_api_key?: boolean;
  clear_api_key?: boolean;
}

export interface GenerateCourseCoverInput {
  style_id?: string;
  title?: string;
  subtitle?: string;
  badge?: string;
  include_badge?: boolean;
  extra_prompt?: string;
}

export interface GenerateCourseCoverResponse {
  cover: CourseCover;
  course: CourseDetail;
}

export interface BatchCourseCoverError {
  course_id: string;
  course_title: string;
  error: string;
}

export interface BatchCoverJob {
  running: boolean;
  mode: string;
  library_id?: string;
  style_id: string;
  state: 'idle' | 'running' | 'completed' | 'cancelled';
  total: number;
  completed: number;
  skipped: number;
  failed: number;
  current_course_id: string;
  current_course_title: string;
  errors: BatchCourseCoverError[];
  started_at?: string;
  finished_at?: string;
}

export interface StartBatchCoverInput {
  library_id?: string;
  mode?: 'missing_only' | 'all';
  style_id?: string;
  include_badge?: boolean;
  extra_prompt?: string;
}

export interface TestASROverrides {
  asr_base_url?: string;
  ai_asr_base_url?: string;
  asr_api_key?: string;
  ai_asr_api_key?: string;
  clear_ai_asr_api_key?: boolean;
  clear_asr_api_key?: boolean;
  asr_model?: string;
  ai_asr_model?: string;
  asr_language?: string;
  ai_asr_language?: string;
  base_url?: string;
  ai_base_url?: string;
  api_key?: string;
  ai_api_key?: string;
  clear_ai_api_key?: boolean;
  clear_api_key?: boolean;
}

export type SummaryJobStage =
  | 'queued'
  | 'reading_subtitle'
  | 'extracting_audio'
  | 'transcribing'
  | 'summarizing'
  | 'completed'
  | 'failed'
  | 'cancelled';

export interface OutlineItem {
  title: string;
  summary: string;
  start_sec: number | null;
}

export interface TimelineSection {
  start_sec: number | null;
  end_sec: number | null;
  title: string;
  summary: string;
  key_points: string[];
}

export interface LessonSummary {
  lesson_id: string;
  media_file_id: string;
  source_type: string;
  source_label: string;
  has_timestamps: boolean;
  model: string;
  overview: string;
  outline: OutlineItem[];
  timeline: TimelineSection[];
  created_at: string;
  updated_at: string;
}

export interface LessonSummaryJob {
  lesson_id: string;
  course_id: string;
  running: boolean;
  stage: SummaryJobStage;
  message: string;
  chunk_index: number;
  chunk_total: number;
  source_type: string;
  error?: string;
  error_code?: string;
  started_at: string;
  finished_at?: string;
}

export interface LessonSummaryCapabilities {
  has_subtitle: boolean;
  has_cached_transcript: boolean;
  transcript_source: string;
  ai_configured: boolean;
  asr_configured: boolean;
  has_ffmpeg: boolean;
}

export interface LessonSummaryStateResponse {
  lesson_id: string;
  summary: LessonSummary | null;
  job: LessonSummaryJob | null;
  capabilities: LessonSummaryCapabilities;
}

export interface GenerateLessonSummaryInput {
  force?: boolean;
  force_retranscribe?: boolean;
  manual_transcript?: string;
}

export interface CourseBatchLessonError {
  lesson_id: string;
  lesson_title: string;
  error: string;
  error_code?: string;
}

export interface CourseBatchSummaryJob {
  course_id: string;
  running: boolean;
  total: number;
  completed: number;
  skipped: number;
  failed: number;
  current_lesson_id: string;
  current_lesson_title: string;
  current_stage: SummaryJobStage;
  chunk_index: number;
  chunk_total: number;
  errors: CourseBatchLessonError[];
  started_at: string;
  finished_at?: string;
}

export interface CourseSummariesStatusResponse {
  course_id: string;
  summary_lesson_ids: string[];
  batch_job: CourseBatchSummaryJob | null;
}

export interface ProposalLesson {
  lesson_id: string;
  title: string;
}

export interface ProposalChapter {
  title: string;
  lessons: ProposalLesson[];
}

export interface CourseOrganizeProposal {
  title: string;
  overview: string;
  tags: string[];
  chapters: ProposalChapter[];
}

export interface ImportAIOrganizeInput {
  source_id: string;
  dir_rel_path: string;
  title: string;
  overview?: string;
  chapters: CommitImportChapter[];
}

export interface ImportProposalLesson {
  media_file_id: string;
  title: string;
}

export interface ImportProposalChapter {
  title: string;
  lessons: ImportProposalLesson[];
}

export interface ImportOrganizeProposal {
  title: string;
  overview: string;
  tags: string[];
  chapters: ImportProposalChapter[];
}

export interface ApplyAIProposalInput {
  apply_title: boolean;
  apply_overview: boolean;
  apply_tags: boolean;
  apply_chapters: boolean;
  title: string;
  overview: string;
  tags: string[];
  chapters: ProposalChapter[];
}

export type UpdateState =
  | 'idle'
  | 'checking'
  | 'downloading'
  | 'verifying'
  | 'applying'
  | 'restarting'
  | 'failed';

export interface UpdateStatus {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  release_name: string;
  release_notes: string;
  release_url: string;
  published_at: string;
  asset_name: string;
  asset_size: number;
  deploy_mode: string;
  platform: string;
  can_apply: boolean;
  cannot_apply_reason: string;
  auto_check_enabled: boolean;
  auto_check_locked_by_env: boolean;
  check_interval_hours: number;
  mirror_url: string;
  state: UpdateState;
  target_version: string;
  progress_bytes: number;
  total_bytes: number;
  progress_percent: number;
  last_checked_at: string;
  last_error: string;
}

export interface UpdateConfigInput {
  auto_check_enabled?: boolean;
  check_interval_hours?: number;
  mirror_url?: string;
}

export interface HealthStatus {
  status: string;
  version: string;
  boot_id: string;
}
