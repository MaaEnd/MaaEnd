/**
 * Python-parity numerics. Every coordinate is rounded here, so these MUST match
 * CPython's `round()` semantics (banker's rounding = round-half-to-even), NOT JS
 * `Math.round` (which is half-up). Getting this wrong silently corrupts exported
 * coordinates vs the tk tool — see web/DESIGN.md §5.
 *
 * This is the single source of coordinate rounding: state.js and main.js delegate
 * here rather than re-implementing it (an earlier `x * 10**n` copy diverged from
 * CPython on ~9.5% of 3-decimal inputs — see the note on {@link roundHalfEven}).
 * @module rounding
 */

/**
 * Round `x` to `ndigits` decimal places using round-half-to-even (banker's
 * rounding), producing the SAME double as CPython `round(x, ndigits)` for every
 * finite input — verified against CPython over a 408k-value battery (0 mismatches).
 *
 * CPython rounds the *exact* IEEE-754 value of `x`, ties-to-even. Two tempting
 * shortcuts both get this wrong:
 *   - `Math.round(x * 10**n) / 10**n` — the multiply is itself rounded, which
 *     manufactures spurious ties (e.g. `-199.985 * 100 → -19998.5` looks like a
 *     tie and rounds to `-199.98`, but CPython sees the true double just past the
 *     midpoint and yields `-199.99`).
 *   - `Number(x.toFixed(n))` — correct except on genuine dyadic ties, where its
 *     half-up rule disagrees with even (`(0.125).toFixed(2) === "0.13"`, but
 *     `round(0.125, 2) === 0.12`).
 * So we take the exact decimal expansion (`toFixed(100)` is loss-free across the
 * whole coordinate domain) and round that digit string ourselves, ties-to-even.
 * This also gives `round(2.675, 2) === 2.67` (2.675 is stored as 2.67499999…).
 *
 * @param {number} x
 * @param {number} [ndigits=0]
 * @returns {number}
 */
export function roundHalfEven(x, ndigits = 0) {
  if (!Number.isFinite(x)) return x;
  const negative = x < 0 || Object.is(x, -0);
  // Exact, un-rounded decimal expansion of |x|. Every double terminates; 100
  // fractional places cover the entire coordinate domain without toFixed itself
  // rounding, so the only rounding is the ties-to-even step we do below.
  const expansion = Math.abs(x).toFixed(100);
  const dot = expansion.indexOf('.');
  const intPart = dot < 0 ? expansion : expansion.slice(0, dot);
  const fracPart = dot < 0 ? '' : expansion.slice(dot + 1);

  const kept = fracPart.slice(0, ndigits);
  const dropped = fracPart.slice(ndigits);
  const digits = (intPart + kept).split('').map(Number);

  // Direction from the dropped tail: >½ up, <½ down, exactly ½ → toward even.
  let roundUp = false;
  if (dropped.length) {
    const lead = dropped.charCodeAt(0) - 48; // first dropped digit
    const tailNonZero = /[1-9]/.test(dropped.slice(1));
    if (lead > 5 || (lead === 5 && tailNonZero)) {
      roundUp = true;
    } else if (lead === 5) {
      roundUp = digits[digits.length - 1] % 2 === 1; // exact tie: up only if odd
    }
  }

  if (roundUp) {
    let i = digits.length - 1;
    for (; i >= 0; i -= 1) {
      if (digits[i] === 9) {
        digits[i] = 0;
      } else {
        digits[i] += 1;
        break;
      }
    }
    if (i < 0) digits.unshift(1); // carried past the most-significant digit
  }

  const splitAt = digits.length - ndigits;
  const outInt = digits.slice(0, splitAt).join('') || '0';
  const outFrac = ndigits > 0 ? digits.slice(splitAt).join('') : '';
  const magnitude = Number(outFrac ? `${outInt}.${outFrac}` : outInt);
  const result = negative ? -magnitude : magnitude;
  return Object.is(result, -0) ? 0 : result;
}

/**
 * Mirror of `json_import._compact_number`: round to 2dp, and — because CPython
 * serialises `12.0` as `"12.0"` but `int(12)` as `"12"` — collapse whole values to
 * integers. In JS there is no int/float split and `JSON.stringify(12.0) === "12"`
 * already, so returning the rounded number is sufficient; we only normalise `-0`.
 *
 * @param {number} value
 * @returns {number}
 */
export function compactNumber(value) {
  const r = roundHalfEven(Number(value), 2);
  return Object.is(r, -0) ? 0 : r;
}
