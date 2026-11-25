import { useId } from 'react';

export interface CourseFinLogoProps {
  className?: string;
  /** Render inside the signature azure rounded app-icon tile (default) or as a standalone mark. */
  variant?: 'tile' | 'mark';
  title?: string;
}

const PLAY_SILHOUETTE =
  'M 15.8 16.5 C 15.8 11.8, 20.9 8.8, 25.0 11.2 L 51.4 26.7 C 55.4 29.0, 55.4 35.0, 51.4 37.3 L 25.0 52.8 C 20.9 55.2, 15.8 52.2, 15.8 47.5 Z';

/**
 * CourseFin brand mark ("09E · Aura Prism"):
 * Optical-centered faceted sapphire play prism with a four-point refraction glint.
 * Uses React's useId() so multiple mounted instances (desktop sidebar + mobile header)
 * never collide on SVG gradient or clipPath IDs.
 */
export default function CourseFinLogo({
  className = 'h-9 w-9',
  variant = 'tile',
  title,
}: CourseFinLogoProps) {
  const uid = useId().replace(/:/g, '');
  const tileGradId = `${uid}-tile`;
  const glowGradId = `${uid}-glow`;
  const deepGradId = `${uid}-deep`;
  const midGradId = `${uid}-mid`;
  const topGradId = `${uid}-top`;
  const clipId = `${uid}-clip`;

  const isTile = variant === 'tile';
  const ariaProps = title
    ? { role: 'img' as const, 'aria-label': title }
    : { 'aria-hidden': true as const };

  const prismBody = (
    <g clipPath={`url(#${clipId})`}>
      <rect
        x="8"
        y="8"
        width="50"
        height="48"
        fill={`url(#${deepGradId})`}
        opacity={isTile ? 0.46 : 1}
      />
      <path
        d="M 18 8 L 58 8 L 58 32 L 29.5 32 Z"
        fill={`url(#${topGradId})`}
        opacity={0.98}
      />
      <path
        d="M 18 56 L 29.5 32 L 58 32 L 58 56 Z"
        fill={`url(#${midGradId})`}
        opacity={isTile ? 0.74 : 0.96}
      />
      <path
        d="M 29.5 24.4 Q 30.5 31.0 37.1 32.0 Q 30.5 33.0 29.5 39.6 Q 28.5 33.0 21.9 32.0 Q 28.5 31.0 29.5 24.4 Z"
        fill="#FFFFFF"
        opacity={0.96}
      />
    </g>
  );

  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 64 64"
      fill="none"
      data-testid="coursefin-logo"
      className={`block shrink-0 ${isTile ? 'rounded-[25%]' : ''} ${className}`}
      {...ariaProps}
    >
      {title && <title>{title}</title>}
      <defs>
        {isTile && (
          <>
            <linearGradient id={tileGradId} x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" style={{ stopColor: 'var(--logo-tile-start, #1565C0)' }} />
              <stop offset="52%" style={{ stopColor: 'var(--logo-tile-mid, #1E88E5)' }} />
              <stop offset="100%" style={{ stopColor: 'var(--logo-tile-end, #29B6F6)' }} />
            </linearGradient>
            <radialGradient id={glowGradId} cx="25%" cy="18%" r="72%">
              <stop offset="0%" stopColor="#BAE6FD" stopOpacity="0.28" />
              <stop offset="100%" stopColor="#FFFFFF" stopOpacity="0" />
            </radialGradient>
          </>
        )}
        <linearGradient id={deepGradId} x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor={isTile ? '#BAE6FD' : '#0D47A1'} />
          <stop offset="100%" stopColor={isTile ? '#E0F2FE' : '#1565C0'} />
        </linearGradient>
        <linearGradient id={midGradId} x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor={isTile ? '#E0F2FE' : '#1E88E5'} />
          <stop offset="100%" stopColor={isTile ? '#F0F9FF' : '#2196F3'} />
        </linearGradient>
        <linearGradient id={topGradId} x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor={isTile ? '#FFFFFF' : '#38BDF8'} />
          <stop offset="100%" stopColor={isTile ? '#F0F9FF' : '#0EA5E9'} />
        </linearGradient>
        <clipPath id={clipId}>
          <path d={PLAY_SILHOUETTE} />
        </clipPath>
      </defs>

      {isTile ? (
        <>
          <rect x="0" y="0" width="64" height="64" rx="16" fill={`url(#${tileGradId})`} />
          <rect x="0" y="0" width="64" height="64" rx="16" fill={`url(#${glowGradId})`} />
          <rect
            x="0.75"
            y="0.75"
            width="62.5"
            height="62.5"
            rx="15.25"
            stroke="#FFFFFF"
            strokeOpacity="0.2"
            strokeWidth="1.5"
          />
          <g transform="translate(8, 8) scale(0.75)">{prismBody}</g>
        </>
      ) : (
        prismBody
      )}
    </svg>
  );
}
