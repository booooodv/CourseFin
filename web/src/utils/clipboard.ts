/**
 * Copy text to the user's clipboard with graceful fallback for non-HTTPS LAN origins
 * where navigator.clipboard is unavailable or rejected by browser security policies.
 */
export async function copyTextToClipboard(text: string): Promise<boolean> {
  if (typeof window !== 'undefined' && window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to legacy textarea fallback
    }
  }

  if (typeof document === 'undefined' || !document.body) {
    return false;
  }

  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.top = '-9999px';
  textarea.style.left = '-9999px';
  textarea.style.opacity = '0';

  document.body.appendChild(textarea);
  try {
    textarea.focus();
    textarea.select();
    textarea.setSelectionRange(0, text.length);
    return typeof document.execCommand === 'function' ? document.execCommand('copy') : false;
  } catch {
    return false;
  } finally {
    document.body.removeChild(textarea);
  }
}
