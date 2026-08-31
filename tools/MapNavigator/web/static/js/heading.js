/**
 * MapLocator heading helpers.
 *
 * The locator convention is north-up: 0° = north, 90° = east, and angles grow
 * clockwise. Keeping the conversion here prevents the readout and canvas overlay
 * from drifting into different conventions.
 *
 * @module heading
 */

const CARDINAL_LABELS = ["北", "东北", "东", "东南", "南", "西南", "西", "西北"];

/** @param {*} value @returns {?number} heading normalized to [0, 360), or null */
export function normalizeHeading(value) {
  if (value === null || value === undefined || value === "" || typeof value === "boolean") return null;
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return null;
  const wrapped = numeric % 360;
  if (Object.is(wrapped, -0)) return 0;
  return wrapped < 0 ? wrapped + 360 : wrapped;
}

/** @param {*} value @returns {string} eight-way Chinese compass label */
export function cardinalHeading(value) {
  const heading = normalizeHeading(value);
  if (heading === null) return "未知";
  return CARDINAL_LABELS[Math.round(heading / 45) % CARDINAL_LABELS.length];
}

/** @param {*} value @param {number} [digits=1] @returns {string} */
export function formatHeading(value, digits = 1) {
  const heading = normalizeHeading(value);
  if (heading === null) return "朝向不可用";
  return `${cardinalHeading(heading)} ${heading.toFixed(digits)}°`;
}

/**
 * Unit vector for the north-up, clockwise heading convention.
 * @param {*} value
 * @returns {?[number, number]} `[dx, dy]` in map pixels
 */
export function headingVector(value) {
  const heading = normalizeHeading(value);
  if (heading === null) return null;
  const radians = (heading * Math.PI) / 180;
  return [Math.sin(radians), -Math.cos(radians)];
}

/** @param {number} dx @param {number} dy @returns {?number} */
export function headingFromVector(dx, dy) {
  if (!Number.isFinite(dx) || !Number.isFinite(dy) || Math.hypot(dx, dy) < 1e-9) return null;
  return normalizeHeading((Math.atan2(dx, -dy) * 180) / Math.PI);
}

/**
 * Transform a heading through the same affine/projective point mapping used by a
 * displayed tier. Transforming a short direction segment also handles negative or
 * non-uniform scale without assuming every map frame has identical axes.
 *
 * @param {*} value
 * @param {(x:number, y:number)=>[number, number]} transformPoint
 * @param {number} [originX=0]
 * @param {number} [originY=0]
 * @returns {?number}
 */
export function transformHeading(value, transformPoint, originX = 0, originY = 0) {
  const vector = headingVector(value);
  if (!vector) return null;
  const start = transformPoint(originX, originY);
  const end = transformPoint(originX + vector[0], originY + vector[1]);
  if (!start || !end || start.length < 2 || end.length < 2) return null;
  return headingFromVector(end[0] - start[0], end[1] - start[1]);
}
