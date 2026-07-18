/**
 * Recording controller — the front half of the `/ws/record` bridge. Ports
 * app_tk's `start_recording` / `stop_recording` / `_on_recording_*` / `_reset_ui`,
 * driving the backend {@link module:recording_service} unchanged over a WebSocket.
 *
 * Message protocol (backend → us), see serve.py `ws_record`:
 *   - `{type:'status', text, color}`       → status line
 *   - `{type:'locator', text}`             → locator line
 *   - `{type:'toast', coord, status}` (G)  → status line (backend already wrote the OS clipboard)
 *   - `{type:'force_waypoint', x, y, zone}` (X) → status line ONLY — the forced point is
 *      already in the backend's `recorded_path` (recording_service.py:219) and arrives via `finished`.
 *      (This is the tk truth; DESIGN §7's "insert client-side" plan was wrong.)
 *   - `{type:'finished', points}`          → normalize + load into the editor
 *   - `{type:'error', message}`            → status line + reset
 *
 * @module ui/recording
 */

import { RecordingSocket } from '../rpc.js';
import { compactNumber } from '../rounding.js';
import { setStatus, setLocator } from './toast.js';

export class RecordingController {
  /**
   * @param {Object} opts
   *   @param {HTMLButtonElement} opts.btnStart
   *   @param {HTMLButtonElement} opts.btnStop
   *   @param {HTMLElement} opts.appEl root element toggled with `.recording` for the pulse
   *   @param {import('./connection.js').ConnectionPanel} opts.connection
   *   @param {(rawPoints:Array<Object>)=>void} opts.onFinished called with the raw recorded path
   */
  constructor(opts) {
    this.btnStart = opts.btnStart;
    this.btnStop = opts.btnStop;
    this.appEl = opts.appEl;
    this.connection = opts.connection;
    this.onFinished = opts.onFinished || (() => {});
    /** @type {?RecordingSocket} */
    this.socket = null;
    this.recording = false;

    this.btnStart.addEventListener('click', () => this.start());
    this.btnStop.addEventListener('click', () => this.stop());
  }

  /** @returns {string} tk `RecordingSessionConfig.display_name()` */
  _sessionDisplayName(session) {
    if (session.kind === 'adb') {
      const target = session.adb.address || '未选择设备';
      return `ADB / ${target}`;
    }
    if (session.kind === 'playcover') {
      return `PlayCover / ${session.playcover.uuid}`;
    }
    return `Win32 / ${session.win32.window_title}`;
  }

  /** Begin a recording session (tk `start_recording`). @returns {void} */
  start() {
    if (this.recording) return;
    const session = this.connection.buildSession();
    if (session.kind === 'adb' && !session.adb.address) {
      setStatus('请选择 ADB 设备或手动填写设备序列号/地址。', '#ef4444');
      return;
    }
    if (session.kind === 'playcover' && !session.playcover.address) {
      setStatus('请填写 PlayCover 服务地址 (PlayTools 端口)。', '#ef4444');
      return;
    }

    this.connection.persist();
    this.recording = true;
    this.btnStart.disabled = true;
    this.btnStop.disabled = false;
    if (this.appEl) this.appEl.classList.add('recording');
    setStatus(`● 正在启动识别引擎... [${this._sessionDisplayName(session)}]`, '#3b82f6');
    setLocator('Locator: waiting for first result...');

    const socket = new RecordingSocket();
    this.socket = socket;
    socket.onMessage = (msg) => this._handleMessage(msg);
    socket.onError = () => {
      // transport error — surface, then let onClose reset
      setStatus('录制连接出现错误。', '#ef4444');
    };
    socket.onClose = () => {
      if (this.recording) {
        this.recording = false;
        this._resetUi();
      }
      this.socket = null;
    };
    socket.start(session);
  }

  /** Ask the backend to stop and tidy the path (tk `stop_recording`). @returns {void} */
  stop() {
    if (!this.socket) return;
    this.socket.stop();
    setStatus('正在停止录制并整理路径点...', '#f59e0b');
    this.btnStop.disabled = true;
  }

  /**
   * Dispatch one backend message.
   * @param {Object} msg
   * @returns {void}
   */
  _handleMessage(msg) {
    if (!msg || typeof msg !== 'object') return;
    switch (msg.type) {
      case 'status':
        setStatus(msg.text || '', msg.color || '#64748b');
        break;
      case 'locator':
        setLocator(msg.text || '');
        break;
      case 'toast':
        // G hotkey: backend already wrote the OS clipboard; mirror onto the status line.
        setStatus(msg.status || '', '#10b981');
        break;
      case 'force_waypoint': {
        // X hotkey: status-only; the strict point is already in the backend recorded path.
        const coord = `[${compactNumber(msg.x)}, ${compactNumber(msg.y)}]`;
        setStatus(`📌 已在当前位置强制打点: ${coord}  (zone: ${msg.zone})`, '#10b981');
        break;
      }
      case 'finished':
        this.recording = false;
        this.onFinished(Array.isArray(msg.points) ? msg.points : []);
        this._resetUi();
        break;
      case 'error':
        this.recording = false;
        setStatus(msg.message || '录制错误', '#ef4444');
        this._resetUi();
        break;
      default:
        break;
    }
  }

  /** Restore idle button + status state (tk `_reset_ui`). @returns {void} */
  _resetUi() {
    this.btnStart.disabled = false;
    this.btnStop.disabled = true;
    if (this.appEl) this.appEl.classList.remove('recording');
  }
}
