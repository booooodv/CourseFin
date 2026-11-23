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

export interface CourseDetail extends CourseSummary {
  chapters: ChapterDetail[];
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

export interface StorageSource {
  id: string;
  name: string;
  driver: string;
  root_path: string;
  config_json: string;
  enabled: boolean;
  last_scanned_at?: string;
  media_count: number;
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
  has_subdirs: boolean;
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
}

export interface UpdateSettingsInput {
  server_name?: string;
  scan_interval_minutes?: number;
  ai_base_url?: string;
  ai_api_key?: string;
  clear_ai_api_key?: boolean;
  ai_model?: string;
  ai_max_subtitle_chars?: number;
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
