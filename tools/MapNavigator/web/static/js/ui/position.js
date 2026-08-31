/** Compact position + heading readout shared by one-shot locate and live recording. */

import {formatHeading, normalizeHeading} from "../heading.js";
import {compactNumber} from "../rounding.js";

export class PositionReadout {
  /**
   * @param {Object} els
   * @param {HTMLElement} els.root
   * @param {HTMLElement} els.coordinates
   * @param {HTMLElement} els.heading
   * @param {HTMLElement} els.zone
   * @param {SVGElement} els.arrow
   */
  constructor(els) {
    this.root = els.root;
    this.coordinates = els.coordinates;
    this.heading = els.heading;
    this.zone = els.zone;
    this.arrow = els.arrow;
  }

  /** @param {string} [message='等待游戏定位'] @returns {void} */
  setPending(message = "等待游戏定位") {
    this.root.dataset.state = "pending";
    this.root.classList.add("heading-unavailable");
    this.coordinates.textContent = "位置 —";
    this.heading.textContent = "朝向 —";
    this.zone.textContent = message;
    this.arrow.style.transform = "rotate(0deg)";
    this.root.setAttribute("aria-label", message);
  }

  /**
   * @param {{x:number,y:number,zone:string,rot?:number|null}} fix
   * @returns {boolean} whether a valid position was rendered
   */
  update(fix) {
    if (!fix || typeof fix !== "object") return false;
    const x = Number(fix.x);
    const y = Number(fix.y);
    if (!Number.isFinite(x) || !Number.isFinite(y)) return false;

    const zone = String((fix && fix.zone) || "未知区域");
    const rot = normalizeHeading(fix && fix.rot);
    const coordinates = `[${compactNumber(x)}, ${compactNumber(y)}]`;
    const heading = rot === null ? "朝向不可用" : formatHeading(rot);

    this.root.dataset.state = "active";
    this.root.classList.toggle("heading-unavailable", rot === null);
    this.coordinates.textContent = coordinates;
    this.heading.textContent = rot === null ? heading : `朝向 ${heading}`;
    this.zone.textContent = zone;
    this.arrow.style.transform = `rotate(${rot === null ? 0 : rot}deg)`;
    this.root.setAttribute("aria-label", `当前位置 ${coordinates}，${heading}，区域 ${zone}`);
    return true;
  }
}
