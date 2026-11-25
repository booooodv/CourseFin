-- Cached transcripts (ASR-generated WebVTT or manually pasted text) keyed by media_file_id
CREATE TABLE IF NOT EXISTS media_transcripts (
    media_file_id TEXT PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
    source TEXT NOT NULL,              -- 'asr' | 'manual'
    language TEXT NOT NULL DEFAULT 'und',
    model TEXT NOT NULL DEFAULT '',
    content_vtt TEXT NOT NULL DEFAULT '',
    plain_text TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Persisted per-lesson AI summaries keyed by lesson_id
CREATE TABLE IF NOT EXISTS lesson_summaries (
    lesson_id TEXT PRIMARY KEY REFERENCES lessons(id) ON DELETE CASCADE,
    media_file_id TEXT NOT NULL DEFAULT '',
    source_type TEXT NOT NULL DEFAULT '',   -- 'external_subtitle' | 'embedded_subtitle' | 'asr' | 'manual'
    source_label TEXT NOT NULL DEFAULT '',  -- human-readable source note, e.g. '外挂字幕 (zh)' / 'AI 语音转写'
    has_timestamps INTEGER NOT NULL DEFAULT 1,
    model TEXT NOT NULL DEFAULT '',
    overview TEXT NOT NULL DEFAULT '',
    outline_json TEXT NOT NULL DEFAULT '[]',
    timeline_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
