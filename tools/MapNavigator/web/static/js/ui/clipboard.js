/**
 * Clipboard write shared by the map pages: the async Clipboard API first, then the
 * hidden-textarea `execCommand("copy")` path for contexts where that API is blocked.
 *
 * @module ui/clipboard
 */

/**
 * @param {string} text
 * @returns {Promise<boolean>} whether either path reported success
 */
export async function copyText(text) {
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fall through to the legacy path
  }
  try {
    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();
    const ok = document.execCommand("copy");
    document.body.removeChild(textarea);
    return ok;
  } catch {
    return false;
  }
}
