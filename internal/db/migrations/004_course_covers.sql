-- Multiple associated covers per course (courses.cover_path remains the active cover filename)
CREATE TABLE IF NOT EXISTS course_covers (
    id TEXT PRIMARY KEY,
    course_id TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'legacy',
    style_id TEXT NOT NULL DEFAULT '',
    style_name TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    UNIQUE(course_id, file_path)
);

CREATE INDEX IF NOT EXISTS idx_course_covers_course_created
    ON course_covers(course_id, created_at DESC, id DESC);

-- Backfill existing course covers so previously generated/uploaded covers remain selectable
INSERT INTO course_covers (id, course_id, file_path, source, style_id, style_name, prompt, width, height, created_at)
SELECT lower(hex(randomblob(16))), id, cover_path, 'legacy', '', '', '', 0, 0, updated_at
FROM courses
WHERE TRIM(cover_path) != ''
  AND NOT EXISTS (
      SELECT 1 FROM course_covers cc WHERE cc.course_id = courses.id AND cc.file_path = courses.cover_path
  );
