/**
 * Parse a pasted JSON coordinate pair. Text copied from formatted documents may
 * contain Unicode spaces that JSON does not accept, so normalize them first.
 * @param {unknown} text
 * @returns {?number[]}
 */
export function parsePastedCoordinatePair(text) {
  const normalized = String(text ?? '')
    .replace(/[\u00a0\u1680\u2000-\u200a\u202f\u205f\u3000]/g, ' ')
    .trim();
  if (!normalized) return null;

  let value;
  try {
    value = JSON.parse(normalized);
  } catch {
    return null;
  }
  if (!Array.isArray(value) || value.length !== 2 || !value.every(Number.isFinite)) return null;
  return value.slice(0, 2);
}
