/**
 * Live test-run controller — the front half of the `/ws/navtest` bridge. Hands whatever
 * the path editor holds to the game without exporting and pasting it into another tool.
 * The current author route goes to `MapNavigateAction` to be walked.
 *
 * Two buttons, same shape as recording: after the connection probe succeeds, 开始试跑
 * opens a game session (elevating when needed) and walks the route in one click. 终止试跑 stops it. The session outlives a
 * single run, so F3 re-runs whatever is on screen at no reconnect cost. F3/F4 are
 * watched by the backend — the game holds focus while a run is in flight, so the
 * browser never sees the keypress — and the buttons do the same thing for when the
 * tool window is focused.
 *
 * Message protocol (backend → us), see serve.py `ws_navtest`:
 *   - `{type:'status', text, color}`      → status line
 *   - `{type:'phase', phase, text}`       → current connection/planning/navigation phase
 *   - `{type:'ready'}`                    → session connected, F3 is live
 *   - `{type:'armed', count, kind}`       → what the backend will run ('route' | 'assert')
 *   - `{type:'run_state', running}`       → drives the abort overlay
 *   - `{type:'position', x, y, zone, rot}` → actual MapNavigateAction locator fix
 *   - `{type:'finished', ok, reason, kind}` → one run ended (`reason` distinguishes an F4 abort)
 *   - `{type:'hotkey_degraded', message}` → hotkeys may not arrive; buttons only
 *   - `{type:'session_over'}`             → session released the game (F4 or panel)
 *   - `{type:'error', message}`           → session died
 *
 * @module ui/navtest
 */

import {NavTestSocket} from "../rpc.js";
import {setStatus} from "./toast.js";

const ARM_DEBOUNCE_MS = 300;

export class NavTestController {
  /**
   * @param {Object} opts
   *   @param {HTMLButtonElement} opts.btnRun
   *   @param {HTMLButtonElement} opts.btnStop
   *   @param {HTMLElement} opts.armedLabel
   *   @param {HTMLElement} opts.overlay full-width running banner
   *   @param {HTMLElement} opts.overlayText text inside the running banner
   *   @param {HTMLElement} opts.phaseLabel persistent phase feedback in the trial panel
   *   @param {HTMLElement} opts.hotkeyNote panel hotkey line (turns into a warning when degraded)
   *   @param {import('./connection.js').ConnectionPanel} opts.connection
   *   @param {()=>{path: Array, exported: boolean, assert_target: ?Object}} opts.getRoute
   *     what the active editor tab would run
   *   @param {(fix:{x:number,y:number,zone:string,rot:?number})=>void} opts.onPosition
   *   @param {()=>Promise<boolean>|boolean|void} opts.onBeforeOpen
   *   @param {(running:boolean)=>void} opts.onRunState
   */
  constructor(opts) {
    this.btnRun = opts.btnRun;
    this.btnStop = opts.btnStop;
    this.armedLabel = opts.armedLabel;
    this.overlay = opts.overlay;
    this.overlayText = opts.overlayText;
    this.phaseLabel = opts.phaseLabel;
    this.hotkeyNote = opts.hotkeyNote;
    this.connection = opts.connection;
    this.getRoute = opts.getRoute || (() => ({path: [], exported: false, assert_target: null}));
    this.onPosition = opts.onPosition || (() => {});
    this.onBeforeOpen = opts.onBeforeOpen || (() => {});
    this.onRunState = opts.onRunState || (() => {});
    // 存 innerHTML: 提示行里的 <kbd> 按键芯片被降级警告覆盖后, 还要能原样还原。
    this._hotkeyNoteHtml = this.hotkeyNote.innerHTML;

    /** @type {?NavTestSocket} */
    this.socket = null;
    this.connected = false;
    this.connectionReady = false;
    this.running = false;
    // 会话正在建立 / 正在等后端交还游戏会话: 两段都不能再接受新的开跑或终止点击。
    this._opening = false;
    this._closing = false;
    this._phase = "";
    this.disabled = false;
    this._armTimer = 0;
    this._armSignature = "";

    this.btnRun.addEventListener("click", () => void this.run());
    this.btnStop.addEventListener("click", () => this.stop());
    this.connection.onStatusChange((connected) => {
      this.connectionReady = connected;
      this._syncUi();
    });
    this._syncUi();
  }

  /**
   * 开始试跑 / F3: 连接探测成功后才能创建会话 (按需提权), 连上即跑; 已连上就直接重跑。
   * @returns {Promise<void>}
   */
  async run() {
    if (this.disabled) {
      setStatus("当前模式不提供实机试跑。", "#f59e0b");
      return;
    }
    const route = this.getRoute();
    if (!route.path.length && !route.assert_target) {
      setStatus("当前页签里没有可试跑的东西: 先画出路线或断言框。", "#ef4444");
      return;
    }
    if (!this.socket) {
      if (!this.connectionReady) {
        setStatus("请先确认游戏连接状态正常。", "#ef4444");
        return;
      }
      const session = this.connection.buildSession();
      if (session.kind === "adb" && !session.adb.address) {
        setStatus("请选择 ADB 设备或手动填写设备序列号/地址。", "#ef4444");
        return;
      }
      if (session.kind === "playcover" && !session.playcover.address) {
        setStatus("请填写 PlayCover 服务地址 (PlayTools 端口)。", "#ef4444");
        return;
      }
      this.connection.persist();
      // 实时定位占着同一个游戏会话, 得等它真的交还了才能开试跑。
      this._opening = true;
      this._setPhase("switching", "正在停止实时定位, 准备试跑…");
      this._syncUi();
      try {
        const ready = await this.onBeforeOpen();
        if (ready === false) {
          // 实时定位没交还会话, 相位停在 switching 会跟已经弹出的失败提示对不上。
          this._setPhase("error", "无法停止实时定位, 试跑未启动");
          return;
        }
        this._opening = false;
        this._open(session, route);
      } catch (err) {
        this._setPhase("error", "无法停止实时定位, 试跑未启动");
        setStatus(`试跑准备失败: ${err && err.message ? err.message : err}`, "#ef4444");
      } finally {
        if (!this.socket) {
          this._opening = false;
          this._syncUi();
        }
      }
      return;
    }
    if (!this.connected || this.running || this._closing) return;
    this._armSignature = this._signature();
    this.socket.run(route);
  }

  /**
   * 终止试跑 / F4: 在跑就停这一轮, 已经停着就把会话收掉放开游戏。
   * @returns {void}
   */
  stop() {
    if (!this.socket) return;
    if (this.running) {
      this.socket.abort();
      return;
    }
    // 先只发停止指令: 会话要等后端真的放开游戏再断, 立刻 close 会让下一次开跑抢不到锁。
    this.socket.stop();
    this._closing = true;
    this._setPhase("stopping", "正在关闭试跑会话, 等待游戏会话释放…");
    setStatus("正在关闭试跑会话, 等待游戏会话释放…", "#3b82f6");
    this._syncUi();
  }

  /**
   * @param {Object} session
   * @param {{path: Array, exported: boolean, assert_target: ?Object}} route
   * @returns {void}
   */
  _open(session, route) {
    const socket = new NavTestSocket();
    this.socket = socket;
    this.connected = false;
    this._armSignature = this._signature();
    socket.onMessage = (msg) => this._handleMessage(msg);
    socket.onError = () => setStatus("试跑连接出现错误。", "#ef4444");
    socket.onClose = () => {
      this.socket = null;
      this.connected = false;
      this.running = false;
      this._closing = false;
      if (this._phase !== "finished" && this._phase !== "failed") {
        this._setPhase("idle", "试跑会话已结束");
      }
      this._syncUi();
    };
    socket.start(session, route);
    this._setPhase("connecting", "正在连接游戏…");
    setStatus("● 正在连接游戏, 连上后立即开跑…", "#3b82f6");
    this._syncUi();
  }

  /**
   * The route changed (edited, or switched editor tab): re-arm so F3 runs what is on
   * screen. Debounced, and skipped entirely when it is byte-identical to the armed one.
   * @returns {void}
   */
  routeChanged() {
    this._syncUi();
    if (!this.socket || !this.connected) return;
    window.clearTimeout(this._armTimer);
    this._armTimer = window.setTimeout(() => this._armNow(), ARM_DEBOUNCE_MS);
  }

  /**
   * Disable starting/re-running while the active mode has no test runner. An idle live
   * session is immediately armed with the empty route so the backend F3 hotkey cannot
   * replay a previously selected editor route.
   * @param {boolean} disabled
   * @returns {void}
   */
  setDisabled(disabled) {
    const next = !!disabled;
    if (this.disabled === next) {
      this._syncUi();
      return;
    }
    this.disabled = next;
    window.clearTimeout(this._armTimer);
    if (this.socket && this.connected && !this.running) {
      this._armSignature = this._signature();
      this.socket.arm(this.getRoute());
    }
    this._syncUi();
  }

  /** @returns {void} */
  _armNow() {
    if (!this.socket || !this.connected) return;
    const signature = this._signature();
    if (signature === this._armSignature) return;
    this._armSignature = signature;
    this.socket.arm(this.getRoute());
  }

  /** @returns {string} cheap change detector for what is on screen */
  _signature() {
    return JSON.stringify(this.getRoute());
  }

  /**
   * Dispatch one backend message.
   * @param {Object} msg
   * @returns {void}
   */
  _handleMessage(msg) {
    if (!msg || typeof msg !== "object") return;
    switch (msg.type) {
      case "status":
        setStatus(msg.text || "", msg.color || "#64748b");
        break;
      case "phase":
        this._setPhase(msg.phase || "", msg.text || "正在处理…");
        break;
      case "ready":
        this.connected = true;
        this._syncUi();
        break;
      case "armed":
        if (!msg.count) {
          this.armedLabel.textContent = "未装载 · 先在编辑器里画出路线或断言框";
        } else if (msg.kind === "assert") {
          this.armedLabel.textContent = "已装载断言框 · F3 检查的就是这一份";
        } else {
          this.armedLabel.textContent = `已装载 ${msg.count} 个节点 · F3 跑的就是这一份`;
        }
        break;
      case "run_state":
        this.running = !!msg.running;
        this.onRunState(this.running);
        this._syncUi();
        break;
      case "position":
        this.onPosition({x: msg.x, y: msg.y, zone: msg.zone, rot: msg.rot});
        break;
      case "finished": {
        this.running = false;
        this._syncUi();
        // 措辞按后端回报的 kind 走: 中途切页签也不会把结论说反。
        const isAssert = msg.kind === "assert";
        if (msg.reason === "aborted") {
          this._setPhase("idle", "本轮试跑已终止, 可按 F3 重新开始");
          setStatus("⏹ 试跑已终止 (F4)。会话还在, 按 F3 可直接重跑。", "#f59e0b");
        } else if (msg.ok) {
          this._setPhase("finished", isAssert ? "断言检查完成" : "路线试跑完成");
          setStatus(isAssert ? "✅ 断言通过: 人在框里。" : "✅ 试跑走完了整条路线。", "#10b981");
        } else {
          this._setPhase("failed", isAssert ? "断言检查未通过" : "路线试跑失败");
          setStatus(
            isAssert
              ? "❌ 断言不通过: 人不在框里 (或没定位到), 详见终端日志。"
              : "❌ 试跑未走完 (寻路失败或超时), 详见终端日志。",
            "#ef4444",
          );
        }
        break;
      }
      case "hotkey_degraded":
        this.hotkeyNote.classList.add("hotkey-degraded");
        this.hotkeyNote.textContent = `⚠ ${msg.message || "F3/F4 热键可能收不到, 请用面板按钮终止。"}`;
        break;
      case "session_over":
        this.connected = false;
        this.running = false;
        this._setPhase("idle", "试跑会话已结束");
        this._syncUi();
        break;
      case "error":
        this.connected = false;
        this.running = false;
        this._setPhase("error", msg.message || "试跑发生错误");
        setStatus(msg.message || "试跑错误", "#ef4444");
        this._syncUi();
        break;
      default:
        break;
    }
  }

  /** @returns {boolean} whether this controller owns or is acquiring the game session. */
  get busy() {
    return this._opening || !!this.socket;
  }

  /** @param {string} phase @param {string} text @returns {void} */
  _setPhase(phase, text) {
    this._phase = phase;
    if (this.phaseLabel) this.phaseLabel.textContent = text;
    if (this.overlayText && this.running) {
      this.overlayText.innerHTML = `${text} —— 按 <kbd>F4</kbd> 立即终止`;
    }
  }

  /** Reflect session/run state onto the buttons and the running overlay. @returns {void} */
  _syncUi() {
    const live = !!this.socket;
    const idle = live && !this.running;
    this.btnRun.textContent =
      this._opening ? "正在准备…" : this._closing ? "正在关闭…" : live ? "重跑 (F3)" : "开始试跑 (F3)";
    this.btnRun.disabled =
      this.disabled || this._opening || this._closing || this.running || (live ? !this.connected : !this.connectionReady);
    this.btnStop.textContent = idle ? "结束会话 (F4)" : "终止试跑 (F4)";
    this.btnStop.disabled = !live || this._opening || this._closing;
    if (!live) {
      const route = this.getRoute();
      if (this.disabled) {
        this.armedLabel.textContent = "当前模式不参与实机试跑";
      } else if (!this.connectionReady) {
        this.armedLabel.textContent = "连接状态未就绪 · 请先检查游戏连接";
      } else if (route.assert_target) {
        this.armedLabel.textContent = "连接状态正常 ·「开始试跑」将检查这个框";
      } else if (route.path.length) {
        this.armedLabel.textContent = "连接状态正常 ·「开始试跑」将直接跑这条线";
      } else {
        this.armedLabel.textContent = "当前页签里没有可试跑的东西";
      }
      this.hotkeyNote.classList.remove("hotkey-degraded");
      this.hotkeyNote.innerHTML = this._hotkeyNoteHtml;
    }
    this.overlay.hidden = !this.running;
    if (!this.running && this.overlayText) {
      this.overlayText.innerHTML = "试跑中 —— 按 <kbd>F4</kbd> 立即终止";
    }
  }
}
