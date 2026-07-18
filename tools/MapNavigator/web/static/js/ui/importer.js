/**
 * Import controller — the front half of the two-phase JSON import. Ports app_tk's
 * `import_json` / `_try_import_assert_json` / `_prompt_zone_assignment_for_import`;
 * the backend (serve.py `/api/import/analyze` + `/finalize`) runs every decision in
 * Python so the result is byte-identical to the tk tool.
 *
 * Flow: read file text → `importAnalyze` → dispatch on `kind`:
 *   - `assert`                      → hooks.applyAssert(zone_id, target)
 *   - `path`, no assignment needed  → hooks.loadPoints(points)
 *   - `path`, assignment needed     → modal (one zone `<select>` per segment) → `importFinalize` → loadPoints
 *
 * @module ui/importer
 */

import { importAnalyze, importFinalize } from '../rpc.js';
import { setStatus } from './toast.js';

export class Importer {
  /**
   * @param {Object} els {fileInput, btnImport, dialog, dialogRows, dialogOk, dialogCancel}
   * @param {Object} hooks
   *   @param {(points:Array<Object>)=>({text?:string, color?:string}|void)} hooks.loadPoints
   *     take a finished route; may return a status lead-in (+ color) replacing the default
   *   @param {(zoneId:string, target:number[])=>void} hooks.applyAssert enter assert mode with a rect
   */
  constructor(els, hooks) {
    this.els = els;
    this.hooks = hooks;
    /** @type {?(assignments:?Array<Object>)=>void} */
    this._resolveDialog = null;
  }

  /** Wire the import button, file input, and dialog buttons. @returns {void} */
  init() {
    this.els.btnImport.addEventListener('click', () => this.openPicker());
    this.els.fileInput.addEventListener('change', () => this._onFileChosen());
    this.els.dialogCancel.addEventListener('click', () => this._closeDialog(null));
    this.els.dialogOk.addEventListener('click', () => this._confirmDialog());
  }

  /**
   * Open the file picker. Public so any mode's own "导入 JSON" button can start the
   * same import without owning a second `<input type=file>`.
   * @returns {void}
   */
  openPicker() {
    this.els.fileInput.click();
  }

  /** @returns {Promise<void>} */
  async _onFileChosen() {
    const file = this.els.fileInput.files && this.els.fileInput.files[0];
    // Allow re-selecting the same file next time (change won't fire on identical value).
    this.els.fileInput.value = '';
    if (!file) return;
    let text;
    try {
      text = await file.text();
    } catch (err) {
      setStatus(`读取文件失败: ${err && err.message ? err.message : err}`, '#ef4444');
      return;
    }
    await this.analyzeText(text);
  }

  /**
   * Run phase-1 analysis and dispatch on the result.
   * @param {string} text raw file contents
   * @returns {Promise<void>}
   */
  async analyzeText(text) {
    let result;
    try {
      result = await importAnalyze(text);
    } catch (err) {
      setStatus(`导入失败: ${err && err.message ? err.message : err}`, '#ef4444');
      return;
    }

    if (!result || result.ok === false) {
      setStatus((result && result.error) || '导入失败', '#ef4444');
      return;
    }

    if (result.kind === 'assert') {
      this._applyAssert(result);
      return;
    }

    // kind === 'path'
    if (result.needs_assignment) {
      const assignments = await this._promptZoneAssignment(result.segments || [], result.zone_options || []);
      if (!assignments) return; // cancelled
      await this._finalize(result.raw_points || [], assignments, result.converted_count || 0, result.route_count || 0);
      return;
    }

    this._loadPath(result.points || [], result.converted_count || 0, result.route_count || 0);
  }

  /**
   * Load a finished path + emit the tk import status line.
   * @param {Array<Object>} points @param {number} convertedCount @param {number} routeCount
   * @returns {void}
   */
  _loadPath(points, convertedCount, routeCount) {
    // The hook may replace the lead-in and its color (A* marks preview points instead of
    // a route; neither A* nor Assert can draw points whose zone has no navmesh basemap).
    const note = this.hooks.loadPoints(points) || {};
    let status = note.text || `已导入 ${points.length} 个路径点`;
    if (routeCount > 1) status += `（共找到 ${routeCount} 条候选路径，已加载点数最多的一条）`;
    if (convertedCount > 0) status += `，已转换 ${convertedCount} 个 MapTracker 坐标`;
    setStatus(status, note.color || '#10b981');
  }

  /**
   * Apply an imported AssertLocation (tk `_try_import_assert_json` tail).
   * @param {{zone_id:string, target:number[], condition_count:number, converted_from_maptracker:boolean}} r
   * @returns {void}
   */
  _applyAssert(r) {
    const [x, y, w, h] = r.target;
    this.hooks.applyAssert(r.zone_id, r.target);
    let status = `已导入 Assert: zone=${r.zone_id} target=[${x.toFixed(1)}, ${y.toFixed(1)}, ${w.toFixed(1)}, ${h.toFixed(1)}]`;
    if (r.condition_count > 1) status += `（共找到 ${r.condition_count} 个条件，已加载第一个）`;
    if (r.converted_from_maptracker) status += '，已转换 MapTracker 坐标';
    setStatus(status, '#10b981');
  }

  /**
   * Phase-2 finalize (server assigns zones + converts) then load.
   * @param {Array<Object>} rawPoints @param {Array<Object>} assignments
   * @param {number} analyzeConverted converted count from phase 1 @param {number} routeCount
   * @returns {Promise<void>}
   */
  async _finalize(rawPoints, assignments, analyzeConverted, routeCount) {
    let result;
    try {
      result = await importFinalize(rawPoints, assignments);
    } catch (err) {
      setStatus(`导入失败: ${err && err.message ? err.message : err}`, '#ef4444');
      return;
    }
    if (!result || result.ok === false) {
      setStatus((result && result.error) || '导入失败', '#ef4444');
      return;
    }
    this._loadPath(result.points || [], analyzeConverted + (result.converted_count || 0), routeCount);
  }

  /**
   * Show the modal zone-assignment dialog and resolve with per-segment assignments
   * (or null if cancelled).
   * @param {Array<{index:number, start:number, end:number, summary:string, suggested_zone:string}>} segments
   * @param {string[]} zoneOptions
   * @returns {Promise<?Array<{start:number, end:number, zone:string}>>}
   */
  _promptZoneAssignment(segments, zoneOptions) {
    return new Promise((resolve) => {
      this._resolveDialog = resolve;
      this._dialogSegments = segments;
      const rows = this.els.dialogRows;
      rows.textContent = '';
      for (const seg of segments) {
        const row = document.createElement('div');
        row.className = 'modal-row';

        const label = document.createElement('span');
        label.className = 'modal-row-label';
        label.textContent = `片段 ${seg.index + 1}: ${seg.summary}`;
        row.appendChild(label);

        const select = document.createElement('select');
        select.className = 'combo';
        select.dataset.start = String(seg.start);
        select.dataset.end = String(seg.end);
        for (const zone of zoneOptions) {
          const opt = document.createElement('option');
          opt.value = zone;
          opt.textContent = zone;
          select.appendChild(opt);
        }
        select.value = zoneOptions.includes(seg.suggested_zone) ? seg.suggested_zone : zoneOptions[0];
        row.appendChild(select);
        rows.appendChild(row);
      }
      this.els.dialog.hidden = false;
    });
  }

  /** Collect the dialog selections and resolve (tk `confirm`). @returns {void} */
  _confirmDialog() {
    const selects = this.els.dialogRows.querySelectorAll('select');
    const assignments = [];
    for (const select of selects) {
      const zone = select.value.trim();
      if (!zone) {
        setStatus('请先为每个片段选择对应地图。', '#ef4444');
        return;
      }
      assignments.push({ start: Number(select.dataset.start), end: Number(select.dataset.end), zone });
    }
    this._closeDialog(assignments);
  }

  /**
   * Hide the dialog and resolve the pending promise.
   * @param {?Array<Object>} value
   * @returns {void}
   */
  _closeDialog(value) {
    this.els.dialog.hidden = true;
    const resolve = this._resolveDialog;
    this._resolveDialog = null;
    if (resolve) resolve(value);
  }
}
