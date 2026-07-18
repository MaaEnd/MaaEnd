/**
 * Connection panel — Win32 / ADB target selection, ports of app_tk's
 * `_connection_kind` / `_sync_connection_controls` / `_refresh_connection_summary`
 * / `refresh_adb_devices` / `_persist_settings` / `_build_recording_session`.
 *
 * Owns the connection row's controls + the `~/.maaend/mapnavigator.json` settings
 * round-trip (GET/PUT via {@link module:rpc}). Exposes {@link ConnectionPanel#buildSession}
 * so the recording controller can start a session with the current target.
 *
 * @module ui/connection
 */

import { getPlatform, getSettings, putSettings, getAdbDevices, checkConnection } from '../rpc.js';
import { setStatus } from './toast.js';

export class ConnectionPanel {
  /**
   * @param {Object} els bound DOM elements:
   *   {kindCombo, win32Group, win32Entry, playcoverGroup, playcoverAddrEntry, playcoverUuidEntry,
   *    adbGroup, adbPathEntry, adbTargetInput, adbTargetList, btnRefreshAdb, summary}
   */
  constructor(els) {
    this.els = els;
    this.statusDot = document.getElementById('status-dot');
    /** @type {{connection_kind:string, adb_path:string, adb_address:string, win32_window_title:string, playcover_address:string, playcover_uuid:string, recent_adb_targets:string[]}} */
    this.settings = {
      connection_kind: '',
      adb_path: '',
      adb_address: '',
      win32_window_title: 'Endfield',
      playcover_address: '127.0.0.1:1717',
      playcover_uuid: 'maa.playcover',
      recent_adb_targets: [],
    };
    this._devicesLoadedOnce = false;
    this._persistTimer = 0;
    this._checkTimer = 0;
    /** @type {boolean} last observed connected state; drives auto-collapse on the rising edge. */
    this._wasConnected = false;
    /** @type {{platform:string, supported_kinds:string[], default_kind:string}} */
    this.platform = { platform: '', supported_kinds: ['win32', 'adb', 'playcover'], default_kind: 'win32' };
  }

  /**
   * Load the backend's platform + persisted settings, apply them to the controls, wire
   * events, and show the ADB group's initial device list if ADB is the active kind.
   * @returns {Promise<void>}
   */
  async init() {
    try {
      const platform = await getPlatform();
      if (platform && Array.isArray(platform.supported_kinds) && platform.supported_kinds.length) {
        this.platform = platform;
      }
    } catch {
      // leave every kind selectable; the connection check still reports the real failure
    }

    try {
      const loaded = await getSettings();
      if (loaded && typeof loaded === 'object') this.settings = { ...this.settings, ...loaded };
    } catch {
      // fall back to defaults; a missing settings file is not an error
    }

    this.els.kindCombo.value = this.settings.connection_kind || this.platform.default_kind;
    this._applyPlatform();
    this.els.win32Entry.value = this.settings.win32_window_title || 'Endfield';
    this.els.playcoverAddrEntry.value = this.settings.playcover_address || '127.0.0.1:1717';
    this.els.playcoverUuidEntry.value = this.settings.playcover_uuid || 'maa.playcover';
    this.els.adbPathEntry.value = this.settings.adb_path || '';
    this.els.adbTargetInput.value = this.settings.adb_address || '';

    this._wire();
    this.syncControls();
    this.refreshSummary();

    if (this.kind() === 'adb' && !this._devicesLoadedOnce) {
      await this.refreshDevices();
    }
  }

  /** @returns {void} */
  _wire() {
    const btnRefresh = document.getElementById('btn-refresh-connection');
    if (btnRefresh) {
      btnRefresh.addEventListener('click', () => this.checkConnectionStatus());
    }
    const headerToggle = document.getElementById('connection-header-toggle');
    if (headerToggle) {
      headerToggle.addEventListener('click', () => this.toggleCollapsed());
    }
    this.els.kindCombo.addEventListener('change', () => {
      this.syncControls();
      this.refreshSummary();
      this.persist();
      if (this.kind() === 'adb' && !this._devicesLoadedOnce) this.refreshDevices();
    });
    this.els.win32Entry.addEventListener('input', () => {
      this.refreshSummary();
      this._persistDebounced();
    });
    this.els.playcoverAddrEntry.addEventListener('input', () => {
      this.refreshSummary();
      this._persistDebounced();
    });
    this.els.playcoverUuidEntry.addEventListener('input', () => {
      this.refreshSummary();
      this._persistDebounced();
    });
    this.els.adbPathEntry.addEventListener('input', () => {
      this.refreshSummary();
      this._persistDebounced();
    });
    this.els.adbTargetInput.addEventListener('input', () => {
      this.refreshSummary();
      this._persistDebounced();
    });
    this.els.adbTargetInput.addEventListener('change', () => {
      this.refreshSummary();
      this.persist();
    });
    this.els.btnRefreshAdb.addEventListener('click', () => this.refreshDevices());
  }

  /**
   * Grey out the kinds this OS can't reach, and move off one if the saved settings
   * point at it (a config carried over from another machine, or the first-run default).
   * @returns {void}
   */
  _applyPlatform() {
    const supported = this.platform.supported_kinds;
    for (const option of this.els.kindCombo.options) {
      if (supported.includes(option.value)) continue;
      option.disabled = true;
      option.textContent = `${option.textContent}（本系统不可用）`;
    }
    if (supported.includes(this.kind())) return;

    const unsupported = this._kindLabel();
    this.els.kindCombo.value = this.platform.default_kind;
    setStatus(`当前系统不支持 ${unsupported} 连接，已切换为 ${this._kindLabel()}`, '#f59e0b');
    this.persist();
  }

  /** @returns {'win32'|'adb'|'playcover'} the active connection kind */
  kind() {
    return this.els.kindCombo.value || this.platform.default_kind;
  }

  /** @returns {string} short display label for the active kind (compact summary). */
  _kindLabel() {
    const k = this.kind();
    if (k === 'adb') return 'ADB';
    if (k === 'playcover') return 'PlayCover';
    return 'Win32';
  }

  /** Show the control group for the active kind, hide the others. @returns {void} */
  syncControls() {
    const k = this.kind();
    this.els.win32Group.hidden = (k !== 'win32');
    this.els.playcoverGroup.hidden = (k !== 'playcover');
    this.els.adbGroup.hidden = (k !== 'adb');
  }

  /** Update the summary line. @returns {void} */
  refreshSummary() {
    const k = this.kind();
    if (k === 'adb') {
      const target = this.els.adbTargetInput.value.trim();
      this.els.summary.textContent = target ? `ADB: ${target}` : 'ADB: 未选择设备';
    } else if (k === 'playcover') {
      const addr = this.els.playcoverAddrEntry.value.trim() || '127.0.0.1:1717';
      this.els.summary.textContent = `PlayCover: ${addr}`;
    } else {
      const title = this.els.win32Entry.value.trim();
      this.els.summary.textContent = `Win32: ${title || 'Endfield'}`;
    }

    this._checkDebounced();
  }

  /** @private */
  _checkDebounced() {
    clearTimeout(this._checkTimer);
    this._checkTimer = setTimeout(() => {
      this.checkConnectionStatus();
    }, 500);
  }

  /** Collapse/expand the connection card body. @param {boolean} collapsed @returns {void} */
  setCollapsed(collapsed) {
    const panel = document.getElementById('panel-connection');
    if (panel) panel.classList.toggle('collapsed', collapsed);
  }

  /** Toggle the connection card body (header click). @returns {void} */
  toggleCollapsed() {
    const panel = document.getElementById('panel-connection');
    if (panel) panel.classList.toggle('collapsed');
  }

  /**
   * Check actual connection status from backend and update indicator.
   * @returns {Promise<void>}
   */
  async checkConnectionStatus() {
    if (!this.statusDot) return;

    this.statusDot.classList.remove('connected');
    this.statusDot.classList.add('connecting');

    const k = this.kind();
    const payload = {
      connection_kind: k,
      win32_window_title: this.els.win32Entry.value.trim(),
      playcover_uuid: this.els.playcoverUuidEntry.value.trim(),
      playcover_address: this.els.playcoverAddrEntry.value.trim(),
      adb_path: this.els.adbPathEntry.value.trim(),
      adb_address: this.els.adbTargetInput.value.trim(),
    };

    try {
      const res = await checkConnection(payload);
      this.statusDot.classList.remove('connecting');
      if (res && res.connected) {
        this.statusDot.classList.add('connected');
        // Compact inline summary (the header strip is narrow); full backend message
        // stays in the hover tooltip and the expanded body carries the target detail.
        this.els.summary.textContent = `${this._kindLabel()} 在线`;
        this.els.summary.title = res.message || '';
        // Auto-collapse on the rising edge only, so a manual re-expand while still
        // connected isn't fought by the next debounced re-check.
        if (!this._wasConnected) this.setCollapsed(true);
        this._wasConnected = true;
      } else {
        this.statusDot.classList.remove('connected');
        this.els.summary.textContent = res ? (res.message || '连接失败') : '连接错误';
        this.els.summary.title = res ? (res.message || '') : '';
        if (this._wasConnected) this.setCollapsed(false);
        this._wasConnected = false;
      }
    } catch (err) {
      this.statusDot.classList.remove('connecting');
      this.statusDot.classList.remove('connected');
      this.els.summary.textContent = '无法连接后端服务';
      this.els.summary.title = String(err);
      if (this._wasConnected) this.setCollapsed(false);
      this._wasConnected = false;
    }
  }

  /**
   * Dedupe `addresses` ++ remembered recents, keep the first 10 (tk `_merge_recent_adb_targets`).
   * @param {string[]} addresses
   * @returns {string[]}
   */
  _mergeRecent(addresses) {
    const merged = [];
    for (const raw of [...addresses, ...(this.settings.recent_adb_targets || [])]) {
      const normalized = String(raw).trim();
      if (!normalized || merged.includes(normalized)) continue;
      merged.push(normalized);
    }
    return merged.slice(0, 10);
  }

  /**
   * Enumerate ADB devices from the backend, refill the datalist (device addresses
   * first, then remembered recents), auto-select the first online device when the
   * target is empty, and persist. Never throws to the caller.
   * @returns {Promise<void>}
   */
  async refreshDevices() {
    const adbPath = this.els.adbPathEntry.value.trim();
    let result;
    try {
      result = await getAdbDevices(adbPath);
    } catch (err) {
      setStatus(`刷新 ADB 设备失败: ${err && err.message ? err.message : err}`, '#ef4444');
      return;
    }
    this._devicesLoadedOnce = true;
    const devices = Array.isArray(result.devices) ? result.devices : [];
    const deviceAddresses = devices.map((d) => d.address).filter(Boolean);
    const recent = this._mergeRecent(deviceAddresses);

    this._fillDatalist(devices, recent);

    const current = this.els.adbTargetInput.value.trim();
    if (!current) {
      const online = devices.find((d) => d.state === 'device' && d.address);
      if (online) {
        this.els.adbTargetInput.value = online.address;
        this.refreshSummary();
      }
    }

    await this.persist();

    if (result.error) {
      setStatus('未找到 adb，可手动指定 adb 路径。', '#f59e0b');
    } else {
      setStatus(`已刷新 ADB 设备，共 ${devices.length} 个。`, '#10b981');
    }
  }

  /**
   * Rebuild the `<datalist>` options: one per discovered device (value=address,
   * label=display_name), then any remembered addresses not already present.
   * @param {Array<{address:string, display_name?:string}>} devices
   * @param {string[]} recent
   * @returns {void}
   */
  _fillDatalist(devices, recent) {
    const list = this.els.adbTargetList;
    if (!list) return;
    list.textContent = '';
    const seen = new Set();
    for (const device of devices) {
      if (!device.address || seen.has(device.address)) continue;
      seen.add(device.address);
      const opt = document.createElement('option');
      opt.value = device.address;
      if (device.display_name && device.display_name !== device.address) opt.label = device.display_name;
      list.appendChild(opt);
    }
    for (const address of recent) {
      if (!address || seen.has(address)) continue;
      seen.add(address);
      const opt = document.createElement('option');
      opt.value = address;
      list.appendChild(opt);
    }
  }

  /** Debounced settings persist for high-frequency text input. @returns {void} */
  _persistDebounced() {
    if (this._persistTimer) window.clearTimeout(this._persistTimer);
    this._persistTimer = window.setTimeout(() => {
      this._persistTimer = 0;
      this.persist();
    }, 400);
  }

  /**
   * Persist the current controls to `~/.maaend/mapnavigator.json` (tk `_persist_settings`).
   * Silently ignores backend failures, matching tk.
   * @returns {Promise<void>}
   */
  async persist() {
    const target = this.els.adbTargetInput.value.trim();
    const payload = {
      connection_kind: this.kind(),
      adb_path: this.els.adbPathEntry.value.trim(),
      adb_address: target,
      win32_window_title: this.els.win32Entry.value.trim() || 'Endfield',
      playcover_address: this.els.playcoverAddrEntry.value.trim() || '127.0.0.1:1717',
      playcover_uuid: this.els.playcoverUuidEntry.value.trim() || 'maa.playcover',
      recent_adb_targets: this._mergeRecent([target]),
    };
    try {
      const saved = await putSettings(payload);
      if (saved && typeof saved === 'object') this.settings = { ...this.settings, ...saved };
    } catch {
      // ignore — persistence is best-effort (tk swallows save errors)
    }
  }

  /**
   * The recording session config for the current target (tk `_build_recording_session`).
   * @returns {{kind:'win32'|'adb'|'playcover', win32:{window_title:string}, adb:{adb_path:string, address:string}, playcover:{uuid:string}}}
   */
  buildSession() {
    return {
      kind: this.kind(),
      win32: { window_title: this.els.win32Entry.value.trim() || 'Endfield' },
      adb: {
        adb_path: this.els.adbPathEntry.value.trim(),
        address: this.els.adbTargetInput.value.trim(),
      },
      playcover: {
        address: this.els.playcoverAddrEntry.value.trim(),
        uuid: this.els.playcoverUuidEntry.value.trim() || 'maa.playcover',
      },
    };
  }
}
