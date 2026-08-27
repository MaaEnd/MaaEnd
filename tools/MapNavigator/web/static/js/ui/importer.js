/**
 * Import controller — the front half of the two-phase JSON import. Ports app_tk's
 * `import_json` / `_try_import_assert_json` / `_prompt_zone_assignment_for_import`;
 * the backend (serve.py `/api/import/analyze` + `/finalize`) runs every decision in
 * Python so the result is byte-identical to the tk tool.
 *
 * Primary flow: scan project assets → choose a mode-appropriate Pipeline node → load it.
 * Edit / A* consume MapNavigateAction paths; Assert consumes MapLocateAssertLocation
 * by default and can switch to MapNavigateAction reference routes.
 * Every mode can also read JSON from the clipboard and enter at the `importAnalyze` step:
 *   - `assert`                      → hooks.applyAssert(zone_id, target)
 *   - `path`, no assignment needed  → hooks.loadPoints(points)
 *   - `path`, assignment needed     → modal (one zone `<select>` per segment) → `importFinalize` → loadPoints
 *
 * @module ui/importer
 */

import { getProjectNodes, importAnalyze, importFinalize, loadProjectNode } from '../rpc.js';
import { setStatus } from './toast.js';

/**
 * Match one kind of project node by resource path, Pipeline node name, or zone.
 * @param {Array<Object>} nodes @param {string} query @param {'path'|'assert'} kind
 * @returns {Array<Object>}
 */
export function filterProjectNodes(nodes, query, kind) {
  const needle = String(query || '')
    .trim()
    .toLocaleLowerCase();
  return nodes.filter((node) => {
    if (node.kind !== kind) return false;
    if (!needle) return true;
    const zones = Array.isArray(node.zone_ids) ? node.zone_ids.join('\n') : '';
    return `${node.resource_path || ''}\n${node.node_name || ''}\n${node.zone_id || ''}\n${zones}`
      .toLocaleLowerCase()
      .includes(needle);
  });
}

/**
 * Resolve imported route coordinates into one navmesh geometry's base frame.
 * `target_tier` describes the coordinate frame of that point and therefore takes
 * precedence over the route's `ZONE` context.
 * @param {Array<Object>} points
 * @param {(zone:string)=>number} resolveZoneId
 * @param {(zoneId:number)=>number} geometryZoneId
 * @param {(zoneId:number, x:number, y:number)=>number[]} pointToBase
 * @returns {{firstZoneId:?number, basePoints:number[][], decks:Array<?number>, skipped:number}}
 */
export function collectAstarImportBasePoints(points, resolveZoneId, geometryZoneId, pointToBase) {
  const resolved = points.map((point) => ({
    point,
    zoneId: resolveZoneId((point && point.target_tier) || (point && point.zone) || ''),
  }));
  const first = resolved.find(({ zoneId }) => Number.isFinite(zoneId));
  if (!first) return { firstZoneId: null, basePoints: [], decks: [], skipped: points.length };

  const geometryId = geometryZoneId(first.zoneId);
  const basePoints = [];
  const decks = [];
  let skipped = 0;
  for (const { point, zoneId } of resolved) {
    if (!Number.isFinite(zoneId) || geometryZoneId(zoneId) !== geometryId) {
      skipped += 1;
      continue;
    }
    const base = pointToBase(zoneId, point.x, point.y);
    if (!Array.isArray(base) || base.length < 2 || !base.slice(0, 2).every(Number.isFinite)) {
      skipped += 1;
      continue;
    }
    basePoints.push(base.slice(0, 2));
    const rawDeck = point && point.target_deck_y;
    const deck = Number(rawDeck);
    decks.push(
      rawDeck !== null && rawDeck !== '' && typeof rawDeck !== 'boolean' && Number.isFinite(deck) ? deck : null,
    );
  }
  return { firstZoneId: first.zoneId, basePoints, decks, skipped };
}

/**
 * Prepend one manually clicked start to imported targets waiting in base coordinates.
 * Convert every target back into the active display frame so the regular A* planner can
 * apply its usual base conversion once. Keep each imported target's deck aligned with it.
 * @param {number[]} start display-frame start
 * @param {number[][]} pendingTargets base-frame imported targets
 * @param {Array<?number>} pendingDecks imported targets' `target_deck_y` values
 * @param {(x:number, y:number)=>number[]} baseToDisplay
 * @returns {?{points:number[][], decks:Array<?number>}}
 */
export function completeAstarImportWithStart(start, pendingTargets, pendingDecks, baseToDisplay) {
  if (
    !Array.isArray(start) ||
    start.length < 2 ||
    !start.slice(0, 2).every(Number.isFinite) ||
    !Array.isArray(pendingTargets) ||
    pendingTargets.length === 0 ||
    !Array.isArray(pendingDecks) ||
    pendingDecks.length !== pendingTargets.length
  ) {
    return null;
  }
  const targets = pendingTargets.map((target) => {
    if (!Array.isArray(target) || target.length < 2 || !target.slice(0, 2).every(Number.isFinite)) return null;
    const display = baseToDisplay(target[0], target[1]);
    return Array.isArray(display) && display.length >= 2 && display.slice(0, 2).every(Number.isFinite)
      ? display.slice(0, 2)
      : null;
  });
  if (targets.some((target) => target === null)) return null;
  return {
    points: [start.slice(0, 2), ...targets],
    decks: [null, ...pendingDecks.map((deck) => (Number.isFinite(deck) ? deck : null))],
  };
}

/**
 * Read non-empty text from an injected Clipboard API implementation.
 * Keeping this environment-independent makes the error paths testable without
 * touching the user's real clipboard.
 * @param {{readText:()=>Promise<string>}|null|undefined} clipboard
 * @returns {Promise<string>}
 */
export async function readClipboardText(clipboard) {
  if (!clipboard || typeof clipboard.readText !== 'function') {
    throw new Error('当前浏览器不支持读取剪贴板');
  }
  const text = await clipboard.readText();
  if (typeof text !== 'string' || !text.trim()) {
    throw new Error('剪贴板中没有可导入的 JSON 内容');
  }
  return text;
}

/**
 * Normalize an AssertLocation target before any UI state is changed.
 * @param {unknown} target
 * @returns {?number[]}
 */
export function normalizeAssertTarget(target) {
  if (!Array.isArray(target) || target.length < 4) return null;
  const values = target.slice(0, 4);
  if (values.some((value) => value === null || typeof value === 'boolean' || String(value).trim() === '')) {
    return null;
  }
  const normalized = values.map(Number);
  if (!normalized.every(Number.isFinite) || normalized[2] <= 0 || normalized[3] <= 0) return null;
  return normalized;
}

export class Importer {
  /**
   * @param {Object} els import controls, zone dialog, and project-node dialog
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
    /** @type {Array<Object>} */
    this._projectNodes = [];
    /** @type {?Object} */
    this._selectedProjectNode = null;
    this._projectNodesLoading = false;
    this._projectNodesError = '';
    this._projectMode = 'edit';
    this._projectKind = 'path';
    this._projectRequestId = 0;
  }

  /** Wire the project import button and dialogs. @returns {void} */
  init() {
    this.els.btnImport.addEventListener('click', () => this.openProjectPicker('edit'));
    this.els.dialogCancel.addEventListener('click', () => this._closeDialog(null));
    this.els.dialogOk.addEventListener('click', () => this._confirmDialog());
    this.els.projectCancel.addEventListener('click', () => this._closeProjectPicker());
    this.els.projectOk.addEventListener('click', () => this._loadSelectedProjectNode());
    this.els.projectSearch.addEventListener('input', () => this._renderProjectNodes());
    this.els.projectKindAssert.addEventListener('click', () => this._selectProjectKind('assert'));
    this.els.projectKindPath.addEventListener('click', () => this._selectProjectKind('path'));
    this.els.projectSearch.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' && this._selectedProjectNode) {
        event.preventDefault();
        this._loadSelectedProjectNode();
      }
    });
  }

  /** Discover and show mode-appropriate Pipeline nodes under project assets. */
  async openProjectPicker(mode) {
    const requestId = ++this._projectRequestId;
    this._projectMode = mode;
    this._projectKind = mode === 'assert' ? 'assert' : 'path';
    this.els.projectDialog.hidden = false;
    this.els.projectKinds.hidden = mode !== 'assert';
    this._syncProjectKindTabs();
    this._syncProjectHint();
    this.els.projectSearch.value = '';
    this.els.projectSearch.disabled = true;
    this._selectedProjectNode = null;
    this._projectNodesLoading = true;
    this._projectNodesError = '';
    this._renderProjectNodes();

    try {
      const result = await getProjectNodes();
      if (requestId !== this._projectRequestId) return;
      this._projectNodes = Array.isArray(result && result.nodes) ? result.nodes : [];
    } catch (err) {
      if (requestId !== this._projectRequestId) return;
      this._projectNodes = [];
      this._projectNodesError = `扫描项目节点失败: ${err && err.message ? err.message : err}`;
      setStatus(this._projectNodesError, '#ef4444');
    } finally {
      if (requestId !== this._projectRequestId) return;
      this._projectNodesLoading = false;
      this.els.projectSearch.disabled = false;
      this._renderProjectNodes();
      if (!this.els.projectDialog.hidden) this.els.projectSearch.focus();
    }
  }

  /** Switch Assert between real assertion nodes and MapNavigateAction reference routes. */
  _selectProjectKind(kind) {
    if (this._projectMode !== 'assert' || this._projectKind === kind) return;
    this._projectKind = kind;
    this._selectedProjectNode = null;
    this._syncProjectKindTabs();
    this._syncProjectHint();
    this._renderProjectNodes();
  }

  _syncProjectKindTabs() {
    for (const [button, kind] of [
      [this.els.projectKindAssert, 'assert'],
      [this.els.projectKindPath, 'path'],
    ]) {
      const active = this._projectKind === kind;
      button.classList.toggle('active', active);
      button.setAttribute('aria-pressed', active ? 'true' : 'false');
    }
  }

  _syncProjectHint() {
    if (this._projectMode === 'edit') {
      this.els.projectHint.textContent = '选择 MapNavigateAction 节点，载入其 custom_action_param.path 继续编辑。';
      return;
    }
    if (this._projectMode === 'astar') {
      this.els.projectHint.textContent =
        '选择 MapNavigateAction 节点；导入坐标后在地图上单击补充起点，再按导入顺序自动规划。';
      return;
    }
    this.els.projectHint.textContent =
      this._projectKind === 'assert'
        ? '选择 MapLocateAssertLocation 节点，载入其 zone_id 与 target 矩形。'
        : '选择 MapNavigateAction 作为参考路线，在断言底图上显示其路径点。';
  }

  /** Render the searchable project-node list and keep selection state explicit. */
  _renderProjectNodes() {
    const host = this.els.projectList;
    host.textContent = '';
    this.els.projectOk.disabled = !this._selectedProjectNode || this._projectNodesLoading;

    if (this._projectNodesLoading) {
      this._appendProjectNodeMessage('正在扫描 assets 中的导航与断言节点...');
      return;
    }
    if (this._projectNodesError) {
      this._appendProjectNodeMessage(this._projectNodesError, true);
      return;
    }

    const visible = filterProjectNodes(this._projectNodes, this.els.projectSearch.value, this._projectKind);
    if (
      this._selectedProjectNode &&
      !visible.some((node) => this._projectNodeKey(node) === this._projectNodeKey(this._selectedProjectNode))
    ) {
      this._selectedProjectNode = null;
      this.els.projectOk.disabled = true;
    }
    if (!visible.length) {
      this._appendProjectNodeMessage(
        this._projectNodes.length ? '没有匹配的资源路径、节点名或区域。' : 'assets 中没有可导入的节点。',
      );
      return;
    }

    for (const node of visible) {
      const item = document.createElement('button');
      item.type = 'button';
      item.className = 'project-node-item';
      item.setAttribute('role', 'option');
      const selected =
        this._selectedProjectNode && this._projectNodeKey(node) === this._projectNodeKey(this._selectedProjectNode);
      item.classList.toggle('selected', !!selected);
      item.setAttribute('aria-selected', selected ? 'true' : 'false');

      const name = document.createElement('span');
      name.className = 'project-node-name';
      name.textContent = node.node_name;
      item.appendChild(name);

      const path = document.createElement('span');
      path.className = 'project-node-path';
      path.textContent = node.resource_path;
      item.appendChild(path);

      const meta = document.createElement('span');
      meta.className = 'project-node-meta';
      meta.textContent = this._projectNodeMeta(node);
      item.appendChild(meta);

      item.addEventListener('click', () => {
        this._selectedProjectNode = node;
        for (const candidate of host.querySelectorAll('.project-node-item')) {
          candidate.classList.remove('selected');
          candidate.setAttribute('aria-selected', 'false');
        }
        item.classList.add('selected');
        item.setAttribute('aria-selected', 'true');
        this.els.projectOk.disabled = false;
      });
      item.addEventListener('dblclick', () => {
        this._selectedProjectNode = node;
        this._loadSelectedProjectNode();
      });
      host.appendChild(item);
    }
  }

  /** @param {string} message @param {boolean} [isError] */
  _appendProjectNodeMessage(message, isError = false) {
    const row = document.createElement('div');
    row.className = `project-node-message${isError ? ' error' : ''}`;
    row.textContent = message;
    this.els.projectList.appendChild(row);
  }

  /** @param {Object} node @returns {string} */
  _projectNodeKey(node) {
    return `${node.kind || ''}\u0000${node.resource_path || ''}\u0000${node.node_name || ''}`;
  }

  _projectNodeMeta(node) {
    if (node.kind === 'assert') {
      const target = Array.isArray(node.target) ? node.target.map((value) => Number(value).toFixed(1)).join(', ') : '';
      const count = Number(node.condition_count) > 1 ? ` · ${node.condition_count} 个断言条件` : '';
      return `MapLocateAssertLocation · ${node.zone_id || '未知区域'} · [${target}]${count}`;
    }
    const zones = Array.isArray(node.zone_ids) && node.zone_ids.length ? ` · ${node.zone_ids.join('、')}` : '';
    return `${Number(node.point_count) || 0} 个路径点 · ${Number(node.navmesh_count) || 0} 个 NAVMESH${zones}`;
  }

  /** Load the selected node through the guarded backend endpoint, then reuse import dispatch. */
  async _loadSelectedProjectNode() {
    const selected = this._selectedProjectNode;
    if (!selected || this._projectNodesLoading) return;
    this._projectNodesLoading = true;
    this.els.projectSearch.disabled = true;
    this._renderProjectNodes();
    try {
      const result = await loadProjectNode(selected.kind, selected.resource_path, selected.node_name);
      if (!result || result.ok === false) {
        throw new Error((result && result.error) || '所选项目节点无法载入');
      }
      const sourceLabel = `${selected.node_name}（${selected.resource_path}）`;
      if (result.kind === 'assert') {
        const target = normalizeAssertTarget(result.target);
        if (!target) throw new Error('所选断言节点缺少有效 target');
        this._applyAssert({ ...result, target, source_label: sourceLabel });
      } else {
        if (!Array.isArray(result.path)) throw new Error('所选导航节点缺少有效 path');
        await this.analyzeText(JSON.stringify({ path: result.path }), sourceLabel);
      }
      this._closeProjectPicker();
    } catch (err) {
      this._projectNodesError = `读取项目节点失败: ${err && err.message ? err.message : err}`;
      setStatus(this._projectNodesError, '#ef4444');
    } finally {
      this._projectNodesLoading = false;
      this.els.projectSearch.disabled = false;
      if (!this.els.projectDialog.hidden) this._renderProjectNodes();
    }
  }

  /** Hide the project-node picker. */
  _closeProjectPicker() {
    this._projectRequestId += 1;
    this.els.projectDialog.hidden = true;
    this._selectedProjectNode = null;
  }

  /** Read clipboard JSON from a user click and reuse the generic import analyzer. */
  async readClipboard() {
    let text;
    try {
      text = await readClipboardText(globalThis.navigator && globalThis.navigator.clipboard);
    } catch (err) {
      const detail = err && err.message ? err.message : String(err);
      if (err && err.name === 'NotAllowedError') {
        setStatus('无法读取剪贴板：请允许浏览器访问剪贴板后重试。', '#ef4444');
      } else if (detail === '当前浏览器不支持读取剪贴板') {
        setStatus('当前浏览器不支持读取剪贴板，请通过本地 MapNavigator 页面打开后重试。', '#ef4444');
      } else if (detail === '剪贴板中没有可导入的 JSON 内容') {
        setStatus(`${detail}，请先复制节点或 path。`, '#ef4444');
      } else {
        setStatus(`读取剪贴板失败: ${detail}`, '#ef4444');
      }
      return;
    }
    await this.analyzeText(text, '剪贴板');
  }

  /**
   * Run phase-1 analysis and dispatch on the result.
   * @param {string} text raw file contents
   * @param {string} [sourceLabel] selected project node shown in the success status
   * @returns {Promise<void>}
   */
  async analyzeText(text, sourceLabel = '') {
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
      const target = normalizeAssertTarget(result.target);
      if (!target) {
        setStatus('导入失败: 断言节点缺少有效 target', '#ef4444');
        return;
      }
      this._applyAssert({ ...result, target, source_label: sourceLabel });
      return;
    }

    // kind === 'path'
    if (result.needs_assignment) {
      const assignments = await this._promptZoneAssignment(result.segments || [], result.zone_options || []);
      if (!assignments) return; // cancelled
      await this._finalize(result.raw_points || [], assignments, result.route_count || 0, sourceLabel);
      return;
    }

    this._loadPath(result.points || [], result.route_count || 0, sourceLabel);
  }

  /**
   * Load a finished path + emit the tk import status line.
   * @param {Array<Object>} points @param {number} routeCount @param {string} [sourceLabel]
   * @returns {void}
   */
  _loadPath(points, routeCount, sourceLabel = '') {
    // The hook may replace the lead-in and its color (A* marks preview points instead of
    // a route; neither A* nor Assert can draw points whose zone has no navmesh basemap).
    const note = this.hooks.loadPoints(points) || {};
    let status;
    if (note.text) {
      status = sourceLabel ? `${note.text}（来源：${sourceLabel}）` : note.text;
    } else {
      status = sourceLabel ? `已从${sourceLabel}导入 ${points.length} 个路径点` : `已导入 ${points.length} 个路径点`;
    }
    if (routeCount > 1) status += `（共找到 ${routeCount} 条候选路径，已加载点数最多的一条）`;
    setStatus(status, note.color || '#10b981');
  }

  /**
   * Apply an imported AssertLocation (tk `_try_import_assert_json` tail).
   * @param {{zone_id:string, target:number[], condition_count:number, source_label?:string}} r
   * @returns {void}
   */
  _applyAssert(r) {
    const [x, y, w, h] = r.target;
    this.hooks.applyAssert(r.zone_id, r.target);
    const source = r.source_label ? `已从${r.source_label}载入断言` : '已导入 Assert';
    let status = `${source}: zone=${r.zone_id} target=[${x.toFixed(1)}, ${y.toFixed(1)}, ${w.toFixed(1)}, ${h.toFixed(1)}]`;
    if (r.condition_count > 1) status += `（共找到 ${r.condition_count} 个条件，已加载第一个）`;
    setStatus(status, '#10b981');
  }

  /**
   * Phase-2 finalize (server assigns zones) then load.
   * @param {Array<Object>} rawPoints @param {Array<Object>} assignments @param {number} routeCount
   * @param {string} [sourceLabel]
   * @returns {Promise<void>}
   */
  async _finalize(rawPoints, assignments, routeCount, sourceLabel = '') {
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
    this._loadPath(result.points || [], routeCount, sourceLabel);
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
