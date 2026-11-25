import type { LessonSummary } from '../types/coursefin';

/**
 * Formats non-negative seconds as `MM:SS` (or `H:MM:SS` when >= 1 hour).
 * Clamps negative or non-finite values to `00:00`.
 */
export function formatTimestamp(seconds: number): string {
  const safeSeconds = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
  const hours = Math.floor(safeSeconds / 3600);
  const minutes = Math.floor((safeSeconds % 3600) / 60);
  const secs = safeSeconds % 60;

  const mm = String(minutes).padStart(2, '0');
  const ss = String(secs).padStart(2, '0');

  if (hours > 0) {
    return `${hours}:${mm}:${ss}`;
  }
  return `${mm}:${ss}`;
}

export interface LessonSummaryMarkdownParams {
  courseTitle: string;
  chapterTitle: string;
  lessonTitle: string;
  summary: LessonSummary;
}

/**
 * Formats a persisted lesson summary into clean Markdown per Spec §5.7,
 * omitting timestamps when `has_timestamps` is false or item timestamps are null.
 */
export function formatLessonSummaryMarkdown({
  courseTitle,
  chapterTitle,
  lessonTitle,
  summary,
}: LessonSummaryMarkdownParams): string {
  const blocks: string[] = [];

  blocks.push(`# ${lessonTitle.trim() || '未命名课时'}`);

  const metaParts: string[] = [];
  if (courseTitle.trim()) {
    metaParts.push(`课程：${courseTitle.trim()}`);
  }
  if (chapterTitle.trim()) {
    metaParts.push(`章节：${chapterTitle.trim()}`);
  }
  if (metaParts.length > 0) {
    blocks.push(`> ${metaParts.join(' · ')}`);
  }

  blocks.push('## AI 总结视频大纲');

  if (summary.overview.trim()) {
    blocks.push(summary.overview.trim());
  }

  if (summary.outline.length > 0) {
    const outlineLines = summary.outline.map((item, idx) => {
      const ts =
        summary.has_timestamps && item.start_sec !== null && item.start_sec !== undefined
          ? `（\`${formatTimestamp(item.start_sec)}\`）`
          : '';
      const itemSummary = item.summary.trim();
      return itemSummary
        ? `${idx + 1}. **${item.title.trim()}**${ts}：${itemSummary}`
        : `${idx + 1}. **${item.title.trim()}**${ts}`;
    });
    blocks.push(outlineLines.join('\n'));
  }

  blocks.push('## AI 总结视频内容（按时间顺序）');

  for (const section of summary.timeline) {
    const hasStart =
      summary.has_timestamps &&
      section.start_sec !== null &&
      section.start_sec !== undefined;
    const hasEnd =
      summary.has_timestamps &&
      section.end_sec !== null &&
      section.end_sec !== undefined;

    let timePrefix = '';
    if (hasStart && hasEnd) {
      timePrefix = `[${formatTimestamp(section.start_sec!)} - ${formatTimestamp(section.end_sec!)}] `;
    } else if (hasStart) {
      timePrefix = `[${formatTimestamp(section.start_sec!)}] `;
    }

    blocks.push(`### ${timePrefix}${section.title.trim()}`);

    if (section.summary.trim()) {
      blocks.push(section.summary.trim());
    }

    const keyPoints = (section.key_points ?? [])
      .map((kp) => kp.trim())
      .filter(Boolean)
      .map((kp) => `- ${kp}`);
    if (keyPoints.length > 0) {
      blocks.push(keyPoints.join('\n'));
    }
  }

  return blocks.join('\n\n');
}
