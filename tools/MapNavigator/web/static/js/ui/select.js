/**
 * Return the adjacent option selected by a vertical wheel gesture.
 *
 * @param {number} currentIndex
 * @param {number} optionCount
 * @param {number} deltaY
 * @returns {number}
 */
export function nextWheelSelectIndex(currentIndex, optionCount, deltaY) {
    if (currentIndex < 0 || optionCount <= 0 || !Number.isFinite(deltaY) || deltaY === 0) return currentIndex;
    const step = deltaY > 0 ? 1 : -1;
    return Math.min(optionCount - 1, Math.max(0, currentIndex + step));
}
