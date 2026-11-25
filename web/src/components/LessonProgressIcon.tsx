import { CheckCircle2 } from 'lucide-react';
import Tooltip from './Tooltip';

const RING_RADIUS = 7.5;
const RING_CIRCUMFERENCE = 2 * Math.PI * RING_RADIUS;

export interface LessonProgressIconProps {
  played: boolean;
  progressRatio: number;
  isActive: boolean;
}

export default function LessonProgressIcon({
  played,
  progressRatio,
  isActive,
}: LessonProgressIconProps) {
  const clampedRaw = Number.isFinite(progressRatio)
    ? Math.min(1, Math.max(0, progressRatio))
    : 0;

  if (played) {
    return (
      <Tooltip content="已学完">
        <span
          role="img"
          aria-label="已学完"
          data-progress-percent={100}
          className="inline-flex h-[18px] w-[18px] items-center justify-center shrink-0"
        >
          <CheckCircle2 className="h-[18px] w-[18px] text-status-success" aria-hidden="true" />
        </span>
      </Tooltip>
    );
  }

  const pct = clampedRaw <= 0 ? 0 : Math.min(99, Math.max(1, Math.round(clampedRaw * 100)));
  const effectiveRatio = clampedRaw > 0 ? Math.min(0.99, Math.max(0.04, clampedRaw)) : 0;
  const dashOffset = RING_CIRCUMFERENCE * (1 - effectiveRatio);

  let label: string;
  if (isActive) {
    label = pct > 0 ? `正在播放 · 已观看 ${pct}%` : '正在播放';
  } else {
    label = pct > 0 ? `已观看 ${pct}%` : '未播放';
  }

  return (
    <Tooltip content={label}>
      <span
        role="img"
        aria-label={label}
        data-progress-percent={pct}
        className="inline-flex h-[18px] w-[18px] items-center justify-center shrink-0"
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 20 20"
          className="h-[18px] w-[18px]"
          aria-hidden="true"
        >
          <circle
            cx="10"
            cy="10"
            r={RING_RADIUS}
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            className={isActive ? 'text-accent/25' : 'text-ink-muted/30'}
          />
          {effectiveRatio > 0 && (
            <circle
              cx="10"
              cy="10"
              r={RING_RADIUS}
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeDasharray={RING_CIRCUMFERENCE}
              strokeDashoffset={dashOffset}
              transform="rotate(-90 10 10)"
              className="text-accent transition-[stroke-dashoffset] duration-300"
            />
          )}
          {isActive && (
            <polygon
              points="8.4,7 8.4,13 13.4,10"
              fill="currentColor"
              className="text-accent"
            />
          )}
        </svg>
      </span>
    </Tooltip>
  );
}
