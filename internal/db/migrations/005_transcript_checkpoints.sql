-- Per-chunk ASR transcription checkpoints for resumable long-video transcription.
-- Rows are removed atomically once all chunks finish and are merged into media_transcripts.
CREATE TABLE IF NOT EXISTS media_transcript_chunks (
    media_file_id TEXT NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    chunk_total INTEGER NOT NULL,
    segment_sec INTEGER NOT NULL,
    media_fingerprint TEXT NOT NULL DEFAULT '',
    offset_sec REAL NOT NULL DEFAULT 0,
    duration_sec REAL NOT NULL DEFAULT 0,
    language TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    cues_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (media_file_id, chunk_index)
);

CREATE INDEX IF NOT EXISTS idx_media_transcript_chunks_media ON media_transcript_chunks(media_file_id);
