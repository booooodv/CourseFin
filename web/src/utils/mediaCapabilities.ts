const FALLBACK_CODECS = ['h264', 'vp9', 'av1'];

/**
 * Probes the current browser's native HTML5 video codec support via canPlayType.
 * HEVC is only advertised when the browser reports "probably" (conservative to
 * avoid false positives; runtime decode failures still auto-fallback to transcode).
 */
export function detectBrowserVideoCodecs(): string[] {
  if (typeof document === 'undefined') {
    return [...FALLBACK_CODECS];
  }

  let video: HTMLVideoElement;
  try {
    video = document.createElement('video');
  } catch {
    return [...FALLBACK_CODECS];
  }

  if (!video || typeof video.canPlayType !== 'function') {
    return [...FALLBACK_CODECS];
  }

  const codecs: string[] = [];

  if (video.canPlayType('video/mp4; codecs="avc1.42E01E"') !== '') {
    codecs.push('h264');
  }

  if (
    video.canPlayType('video/mp4; codecs="hvc1.1.6.L93.B0"') === 'probably' ||
    video.canPlayType('video/mp4; codecs="hev1.1.6.L93.B0"') === 'probably'
  ) {
    codecs.push('hevc');
  }

  if (video.canPlayType('video/webm; codecs="vp9"') !== '') {
    codecs.push('vp9');
  }

  if (video.canPlayType('video/mp4; codecs="av01.0.04M.08"') !== '') {
    codecs.push('av1');
  }

  // jsdom / non-media environments return '' for every probe; preserve default set.
  if (codecs.length === 0) {
    return [...FALLBACK_CODECS];
  }

  return codecs;
}
