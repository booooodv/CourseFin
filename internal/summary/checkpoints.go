package summary

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"coursefin/internal/ai"
)

const checkpointWriteTimeout = 5 * time.Second

type chunkCheckpoint struct {
	mediaFileID      string
	chunkIndex       int
	chunkTotal       int
	segmentSec       int
	mediaFingerprint string
	offsetSec        float64
	durationSec      float64
	language         string
	model            string
	cues             []ai.TranscriptCue
	updatedAt        string
}

type embeddedStreamEntry struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
}

func formatMediaFingerprint(fileSize int64, modTime string) string {
	mt := strings.TrimSpace(modTime)
	if fileSize <= 0 && mt == "" {
		return ""
	}
	return fmt.Sprintf("%d:%s", fileSize, mt)
}

func (s *Service) computeMediaFingerprint(ctx context.Context, mediaFileID, absPath string) string {
	if strings.TrimSpace(absPath) != "" {
		if st, err := os.Stat(absPath); err == nil {
			return fmt.Sprintf("%d:%s", st.Size(), st.ModTime().UTC().Format(time.RFC3339Nano))
		}
	}
	var fileSize int64
	var modTime string
	err := s.db.QueryRowContext(ctx, `SELECT file_size, mod_time FROM media_files WHERE id = ?`, mediaFileID).Scan(&fileSize, &modTime)
	if err != nil {
		return ""
	}
	return formatMediaFingerprint(fileSize, modTime)
}

func (s *Service) loadChunkCheckpoints(ctx context.Context, mediaFileID string) ([]chunkCheckpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT media_file_id, chunk_index, chunk_total, segment_sec, media_fingerprint,
		       offset_sec, duration_sec, language, model, cues_json, updated_at
		FROM media_transcript_chunks
		WHERE media_file_id = ?
		ORDER BY chunk_index ASC`, mediaFileID)
	if err != nil {
		return nil, fmt.Errorf("query media_transcript_chunks: %w", err)
	}
	defer rows.Close()

	var out []chunkCheckpoint
	for rows.Next() {
		var cp chunkCheckpoint
		var cuesJSON string
		if err := rows.Scan(
			&cp.mediaFileID, &cp.chunkIndex, &cp.chunkTotal, &cp.segmentSec, &cp.mediaFingerprint,
			&cp.offsetSec, &cp.durationSec, &cp.language, &cp.model, &cuesJSON, &cp.updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan media_transcript_chunks: %w", err)
		}
		if strings.TrimSpace(cuesJSON) != "" {
			if err := json.Unmarshal([]byte(cuesJSON), &cp.cues); err != nil {
				// Treat corrupted cue payload as invalid so caller re-transcribes cleanly.
				return nil, fmt.Errorf("unmarshal chunk cues_json: %w", err)
			}
		}
		if cp.cues == nil {
			cp.cues = []ai.TranscriptCue{}
		}
		out = append(out, cp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func validateChunkCheckpoints(rows []chunkCheckpoint, chunkTotal, segmentSec int, fingerprint string) (map[int]chunkCheckpoint, bool) {
	if len(rows) == 0 || chunkTotal <= 0 {
		return nil, false
	}
	valid := make(map[int]chunkCheckpoint, len(rows))
	for _, r := range rows {
		if r.chunkTotal != chunkTotal ||
			r.segmentSec != segmentSec ||
			r.mediaFingerprint != fingerprint ||
			r.chunkIndex < 0 ||
			r.chunkIndex >= chunkTotal {
			return nil, false
		}
		valid[r.chunkIndex] = r
	}
	return valid, true
}

func (s *Service) persistChunkCheckpoint(ctx context.Context, cp chunkCheckpoint) error {
	if s.lifecycleCtx != nil && s.lifecycleCtx.Err() != nil {
		return s.lifecycleCtx.Err()
	}
	cues := cp.cues
	if cues == nil {
		cues = []ai.TranscriptCue{}
	}
	cuesBytes, err := json.Marshal(cues)
	if err != nil {
		return fmt.Errorf("marshal chunk cues_json: %w", err)
	}

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkpointWriteTimeout)
	defer cancel()

	now := nowUTC()
	_, err = s.db.ExecContext(writeCtx, `
		INSERT INTO media_transcript_chunks (
			media_file_id, chunk_index, chunk_total, segment_sec, media_fingerprint,
			offset_sec, duration_sec, language, model, cues_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(media_file_id, chunk_index) DO UPDATE SET
			chunk_total = excluded.chunk_total,
			segment_sec = excluded.segment_sec,
			media_fingerprint = excluded.media_fingerprint,
			offset_sec = excluded.offset_sec,
			duration_sec = excluded.duration_sec,
			language = excluded.language,
			model = excluded.model,
			cues_json = excluded.cues_json,
			updated_at = excluded.updated_at`,
		cp.mediaFileID, cp.chunkIndex, cp.chunkTotal, cp.segmentSec, cp.mediaFingerprint,
		cp.offsetSec, cp.durationSec, cp.language, cp.model, string(cuesBytes), now, now,
	)
	if err != nil {
		return fmt.Errorf("upsert media_transcript_chunks: %w", err)
	}
	return nil
}

func (s *Service) deleteChunkCheckpoints(ctx context.Context, mediaFileID string) error {
	if mediaFileID == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM media_transcript_chunks WHERE media_file_id = ?`, mediaFileID); err != nil {
		return fmt.Errorf("delete media_transcript_chunks: %w", err)
	}
	return nil
}

func (s *Service) finalizeASRTranscriptTx(ctx context.Context, mediaFileID, language, model, contentVTT, plainText string) error {
	if strings.TrimSpace(language) == "" {
		language = "und"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finalize transcript tx: %w", err)
	}
	defer tx.Rollback()

	now := nowUTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at)
		VALUES (?, 'asr', ?, ?, ?, ?, ?, ?)
		ON CONFLICT(media_file_id) DO UPDATE SET
			source = excluded.source,
			language = excluded.language,
			model = excluded.model,
			content_vtt = excluded.content_vtt,
			plain_text = excluded.plain_text,
			updated_at = excluded.updated_at`,
		mediaFileID, language, model, contentVTT, plainText, now, now,
	); err != nil {
		return fmt.Errorf("upsert media_transcripts in finalize tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM media_transcript_chunks WHERE media_file_id = ?`, mediaFileID); err != nil {
		return fmt.Errorf("delete media_transcript_chunks in finalize tx: %w", err)
	}
	return tx.Commit()
}

type partialChunkSummary struct {
	done  int
	total int
}

func evaluatePartialCheckpoints(rows []chunkCheckpoint, dbFingerprint string, mediaUpdatedAt string, expectedSegSec int) (partialChunkSummary, bool) {
	if len(rows) == 0 {
		return partialChunkSummary{}, false
	}
	first := rows[0]
	if first.chunkTotal <= 0 || first.segmentSec != expectedSegSec {
		return partialChunkSummary{}, false
	}

	seen := make(map[int]bool, len(rows))
	for _, r := range rows {
		if r.chunkTotal != first.chunkTotal ||
			r.segmentSec != expectedSegSec ||
			r.mediaFingerprint != first.mediaFingerprint ||
			r.chunkIndex < 0 ||
			r.chunkIndex >= first.chunkTotal {
			return partialChunkSummary{}, false
		}
		// Invalidate if DB media record was updated after the chunk AND both fingerprints are known and differ.
		if dbFingerprint != "" && r.mediaFingerprint != "" && dbFingerprint != r.mediaFingerprint && mediaUpdatedAt > r.updatedAt {
			return partialChunkSummary{}, false
		}
		seen[r.chunkIndex] = true
	}

	return partialChunkSummary{
		done:  len(seen),
		total: first.chunkTotal,
	}, true
}

func (s *Service) loadPartialCheckpointProgress(ctx context.Context, mediaFileID string) (int, int, error) {
	if mediaFileID == "" {
		return 0, 0, nil
	}
	var fileSize int64
	var modTime, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT file_size, mod_time, updated_at FROM media_files WHERE id = ?`, mediaFileID).
		Scan(&fileSize, &modTime, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("query media_files for partial progress: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_index, chunk_total, segment_sec, media_fingerprint, updated_at
		FROM media_transcript_chunks
		WHERE media_file_id = ?
		ORDER BY chunk_index ASC`, mediaFileID)
	if err != nil {
		return 0, 0, fmt.Errorf("query partial checkpoints: %w", err)
	}
	defer rows.Close()

	var cps []chunkCheckpoint
	for rows.Next() {
		var cp chunkCheckpoint
		if err := rows.Scan(&cp.chunkIndex, &cp.chunkTotal, &cp.segmentSec, &cp.mediaFingerprint, &cp.updatedAt); err != nil {
			return 0, 0, err
		}
		cps = append(cps, cp)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	summary, ok := evaluatePartialCheckpoints(cps, formatMediaFingerprint(fileSize, modTime), updatedAt, s.audioSegmentSec)
	if !ok {
		return 0, 0, nil
	}
	return summary.done, summary.total, nil
}

func hasEmbeddedTextSubtitleJSON(streamsJSON string) bool {
	trimmed := strings.TrimSpace(streamsJSON)
	if trimmed == "" || trimmed == "[]" {
		return false
	}
	var streams []embeddedStreamEntry
	if err := json.Unmarshal([]byte(trimmed), &streams); err != nil {
		return false
	}
	for _, st := range streams {
		if !strings.EqualFold(strings.TrimSpace(st.CodecType), "subtitle") {
			continue
		}
		if !isBitmapCodec(st.CodecName) {
			return true
		}
	}
	return false
}

type courseLessonRow struct {
	lessonID       string
	lessonTitle    string
	chapterID      string
	mediaFileID    string
	fileSize       int64
	modTime        string
	mediaUpdatedAt string
	streamsJSON    string
}

type cachedTranscriptMeta struct {
	source    string
	hasVTT    bool
	updatedAt string
}

type coursePipelineSnapshot struct {
	lessons             []LessonPipelineStatus
	lessonTitles        map[string]string
	counts              CoursePipelineCounts
	summaryLessonIDs    []string
	transcriptLessonIDs []string
}

func (s *Service) computeCourseLessonsPipelineStatus(ctx context.Context, courseID string) (*coursePipelineSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.title, l.chapter_id, l.media_file_id,
		       COALESCE(mf.file_size, 0), COALESCE(mf.mod_time, ''), COALESCE(mf.updated_at, ''), COALESCE(mf.streams_json, '[]')
		FROM lessons l
		JOIN chapters ch ON ch.id = l.chapter_id
		LEFT JOIN media_files mf ON mf.id = l.media_file_id
		WHERE l.course_id = ?
		ORDER BY ch.sort_order ASC, ch.id ASC, l.sort_order ASC, l.id ASC`, courseID)
	if err != nil {
		return nil, fmt.Errorf("query course lessons for pipeline status: %w", err)
	}
	defer rows.Close()

	var lessonRows []courseLessonRow
	mediaMeta := make(map[string]courseLessonRow)
	for rows.Next() {
		var r courseLessonRow
		if err := rows.Scan(&r.lessonID, &r.lessonTitle, &r.chapterID, &r.mediaFileID, &r.fileSize, &r.modTime, &r.mediaUpdatedAt, &r.streamsJSON); err != nil {
			return nil, fmt.Errorf("scan course lesson pipeline row: %w", err)
		}
		lessonRows = append(lessonRows, r)
		if r.mediaFileID != "" {
			mediaMeta[r.mediaFileID] = r
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	extSubMedia := make(map[string]bool)
	extRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT l.media_file_id
		FROM lessons l
		JOIN subtitle_files sf ON sf.media_file_id = l.media_file_id
		WHERE l.course_id = ? AND sf.status = 'active'`, courseID)
	if err != nil {
		return nil, fmt.Errorf("query active course external subtitles: %w", err)
	}
	for extRows.Next() {
		var mid string
		if err := extRows.Scan(&mid); err != nil {
			extRows.Close()
			return nil, err
		}
		extSubMedia[mid] = true
	}
	if err := extRows.Err(); err != nil {
		extRows.Close()
		return nil, err
	}
	extRows.Close()

	trByMedia := make(map[string]cachedTranscriptMeta)
	trRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT mt.media_file_id, mt.source, mt.content_vtt, mt.plain_text, mt.updated_at
		FROM lessons l
		JOIN media_transcripts mt ON mt.media_file_id = l.media_file_id
		WHERE l.course_id = ?`, courseID)
	if err != nil {
		return nil, fmt.Errorf("query course media_transcripts: %w", err)
	}
	for trRows.Next() {
		var mid, src, vtt, plain, upd string
		if err := trRows.Scan(&mid, &src, &vtt, &plain, &upd); err != nil {
			trRows.Close()
			return nil, err
		}
		if strings.TrimSpace(vtt) == "" && strings.TrimSpace(plain) == "" {
			continue
		}
		trByMedia[mid] = cachedTranscriptMeta{
			source:    src,
			hasVTT:    strings.TrimSpace(vtt) != "",
			updatedAt: upd,
		}
	}
	if err := trRows.Err(); err != nil {
		trRows.Close()
		return nil, err
	}
	trRows.Close()

	chunksByMedia := make(map[string][]chunkCheckpoint)
	cpRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT mtc.media_file_id, mtc.chunk_index, mtc.chunk_total, mtc.segment_sec, mtc.media_fingerprint, mtc.updated_at
		FROM lessons l
		JOIN media_transcript_chunks mtc ON mtc.media_file_id = l.media_file_id
		WHERE l.course_id = ?
		ORDER BY mtc.media_file_id ASC, mtc.chunk_index ASC`, courseID)
	if err != nil {
		return nil, fmt.Errorf("query course chunk checkpoints: %w", err)
	}
	for cpRows.Next() {
		var cp chunkCheckpoint
		if err := cpRows.Scan(&cp.mediaFileID, &cp.chunkIndex, &cp.chunkTotal, &cp.segmentSec, &cp.mediaFingerprint, &cp.updatedAt); err != nil {
			cpRows.Close()
			return nil, err
		}
		chunksByMedia[cp.mediaFileID] = append(chunksByMedia[cp.mediaFileID], cp)
	}
	if err := cpRows.Err(); err != nil {
		cpRows.Close()
		return nil, err
	}
	cpRows.Close()

	summarySet := make(map[string]bool)
	sumRows, err := s.db.QueryContext(ctx, `
		SELECT ls.lesson_id
		FROM lesson_summaries ls
		JOIN lessons l ON l.id = ls.lesson_id
		WHERE l.course_id = ?`, courseID)
	if err != nil {
		return nil, fmt.Errorf("query course summaries set: %w", err)
	}
	for sumRows.Next() {
		var lid string
		if err := sumRows.Scan(&lid); err != nil {
			sumRows.Close()
			return nil, err
		}
		summarySet[lid] = true
	}
	if err := sumRows.Err(); err != nil {
		sumRows.Close()
		return nil, err
	}
	sumRows.Close()

	snap := &coursePipelineSnapshot{
		lessons:             make([]LessonPipelineStatus, 0, len(lessonRows)),
		lessonTitles:        make(map[string]string, len(lessonRows)),
		summaryLessonIDs:    []string{},
		transcriptLessonIDs: []string{},
	}

	for _, r := range lessonRows {
		snap.lessonTitles[r.lessonID] = r.lessonTitle
		hasExt := extSubMedia[r.mediaFileID]
		hasEmb := !hasExt && hasEmbeddedTextSubtitleJSON(r.streamsJSON)
		hasSubtitle := hasExt || hasEmb

		trMeta, hasCached := trByMedia[r.mediaFileID]
		hasSummary := summarySet[r.lessonID]

		var (
			transcriptStatus  string
			transcriptReady   bool
			transcribedChunks int
			totalChunks       int
		)

		switch {
		case hasExt:
			transcriptStatus = "external_subtitle"
			transcriptReady = true
			snap.counts.SubtitleLessons++
		case hasEmb:
			transcriptStatus = "embedded_subtitle"
			transcriptReady = true
			snap.counts.SubtitleLessons++
		case hasCached && trMeta.source == "manual":
			transcriptStatus = "manual"
			transcriptReady = true
			snap.counts.TranscribedLessons++
		case hasCached:
			transcriptStatus = "asr"
			transcriptReady = true
			snap.counts.TranscribedLessons++
		default:
			if cps := chunksByMedia[r.mediaFileID]; len(cps) > 0 {
				if pSum, ok := evaluatePartialCheckpoints(cps, formatMediaFingerprint(r.fileSize, r.modTime), r.mediaUpdatedAt, s.audioSegmentSec); ok {
					transcriptStatus = "partial"
					transcribedChunks = pSum.done
					totalChunks = pSum.total
					snap.counts.PartialLessons++
				} else {
					transcriptStatus = "none"
				}
			} else {
				transcriptStatus = "none"
			}
		}

		snap.counts.TotalLessons++
		if transcriptReady {
			snap.counts.TranscriptReady++
			snap.transcriptLessonIDs = append(snap.transcriptLessonIDs, r.lessonID)
		} else {
			snap.counts.MissingTranscript++
		}
		if hasSummary {
			snap.counts.SummarizedLessons++
			snap.summaryLessonIDs = append(snap.summaryLessonIDs, r.lessonID)
		}
		if transcriptReady && !hasSummary {
			snap.counts.ReadyToSummarize++
		}

		var aiSubUpdatedAt string
		if hasCached && trMeta.source == "asr" && trMeta.hasVTT {
			aiSubUpdatedAt = trMeta.updatedAt
		}

		snap.lessons = append(snap.lessons, LessonPipelineStatus{
			LessonID:            r.lessonID,
			ChapterID:           r.chapterID,
			HasSubtitle:         hasSubtitle,
			HasCachedTranscript: hasCached,
			TranscriptStatus:    transcriptStatus,
			TranscriptReady:     transcriptReady,
			TranscribedChunks:   transcribedChunks,
			TotalChunks:         totalChunks,
			AISubtitleUpdatedAt: aiSubUpdatedAt,
			HasSummary:          hasSummary,
		})
	}

	return snap, nil
}
