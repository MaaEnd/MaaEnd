// MaaEnd CloudShell 注入脚本：解除落地页开始按钮上的空文本透明遮罩，并点掉启动期的 SDK 弹窗。
// 页面「开始游戏」按钮被透明 DIV 覆盖，Win32 点击落在覆盖层上无效；脚本只处理遮罩，
// 「开始游戏」的唯一点击方是 CloudWebStart Pipeline，避免两边同时点击把云会话打回起点。
// 纯浏览器 IIFE，无依赖；由 cloud-shell.exe --inject 加载。界面文案仅简中。
(() => {
    "use strict";

    if (window.__maaCloudStart && window.__maaCloudStart.running) {
        return;
    }
    if (window.__maaCloudStart && window.__maaCloudStart.timer) {
        clearInterval(window.__maaCloudStart.timer);
    }
    if (window.__maaCloudStart && window.__maaCloudStart.ballTimer) {
        clearInterval(window.__maaCloudStart.ballTimer);
    }
    window.__maaCloudStart = { running: true, timer: 0, ballTimer: 0 };

    const POLL_INTERVAL_MS = 1000;
    const CLICK_COOLDOWN_MS = 5000;
    const MAX_RUNTIME_MS = 30 * 60 * 1000;
    // 启动期浮球随页面一起出现，需要更密地检查；进入游戏后常驻低频复查即可。
    const BALL_POLL_ACTIVE_MS = 2000;
    const BALL_POLL_IDLE_MS = 5000;

    // 登录态标记：页面文本包含其一即视为已登录
    const LOGIN_MARKERS = ["通行证UID", "用户中心"];
    // 上次数据弹窗的确认按钮文案
    const ACK_TEXT = "知道了";
    // 落地页开始按钮文案（只用于解除遮罩，不点击）
    const START_TEXT = "开始游戏";
    // SDK 快速登录按钮文案（只点精确的「登录」，不点「登录其他账号」）
    const LOGIN_TEXT = "登录";
    // 启动中/排队/连接异常时只等待，不重复点击开始
    const BUSY_TEXTS = ["游戏启动中", "请耐心等待", "正在排队", "连接服务器超时", "数据正在处理中"];
    // CloudShell 左上角浮球的尺寸与位置上限（实测 ≤180px，留余量）
    const BALL_MAX_SIZE = 180;
    const BALL_MAX_RIGHT = 240;
    const BALL_MAX_BOTTOM = 240;
    // canonical 视频的判定下限
    const CANONICAL_VIDEO_MIN_WIDTH = 640;

    const startedAt = Date.now();
    let lastClickAt = 0;
    const hiddenBalls = new WeakSet();

    function log(...args) {
        console.info("[cloud_start]", ...args);
    }

    // CloudShell 左侧浮球是左上角的小型 fixed 容器，正好压在小地图上。
    // 只设 pointer-events:none 仍会留在 PrintWindow 帧里遮挡 MapLocator，
    // 必须直接隐藏；沿祖先链向上找会命中承载 #game-video 的全屏输入层。
    function disableCloudBall() {
        const nodes = document.querySelectorAll("body *");
        for (let i = 0; i < nodes.length; i += 1) {
            const node = nodes[i];
            if (hiddenBalls.has(node) && node.style.display === "none") {
                continue;
            }
            const style = window.getComputedStyle(node);
            if (style.position !== "fixed" && style.position !== "absolute") {
                continue;
            }
            if (style.display === "none") {
                continue;
            }
            const rect = node.getBoundingClientRect();
            if (rect.width <= 0 || rect.height <= 0) {
                continue;
            }
            if (rect.width > BALL_MAX_SIZE || rect.height > BALL_MAX_SIZE) {
                continue;
            }
            if (rect.right > BALL_MAX_RIGHT || rect.bottom > BALL_MAX_BOTTOM) {
                continue;
            }
            node.style.display = "none";
            hiddenBalls.add(node);
        }
    }

    function scheduleBallPoll() {
        const delay = window.__maaCloudStart.running ? BALL_POLL_ACTIVE_MS : BALL_POLL_IDLE_MS;
        window.__maaCloudStart.ballTimer = setInterval(disableCloudBall, delay);
    }

    function stop() {
        if (!window.__maaCloudStart.running) {
            return;
        }
        window.__maaCloudStart.running = false;
        clearInterval(window.__maaCloudStart.timer);
        window.__maaCloudStart.timer = 0;
        disableCloudBall();
        // 浮球常驻隐藏，不随 tick 生命周期结束；切到低频轮询
        if (window.__maaCloudStart.ballTimer) {
            clearInterval(window.__maaCloudStart.ballTimer);
        }
        scheduleBallPoll();
        log("stopped");
    }

    function isLoggedIn() {
        const text = document.body ? document.body.innerText : "";
        return LOGIN_MARKERS.some((marker) => text.indexOf(marker) !== -1);
    }

    // 落地页自带小尺寸预览 video；只有 canonical 视频且登录标记消失才算游戏已启动
    function isGameRunning() {
        const videos = document.querySelectorAll("video");
        let canonical = false;
        for (let i = 0; i < videos.length; i++) {
            if (
                videos[i].videoWidth >= CANONICAL_VIDEO_MIN_WIDTH &&
                videos[i].readyState >= 2 &&
                !videos[i].paused
            ) {
                canonical = true;
                break;
            }
        }
        return canonical && !isLoggedIn();
    }

    function isVisible(el) {
        if (!el || !el.getClientRects().length) {
            return false;
        }
        const style = window.getComputedStyle(el);
        return style.display !== "none" && style.visibility !== "hidden" && style.opacity !== "0";
    }

    // 精确匹配文本的可见叶子元素；隐藏的登录弹窗/按钮在 DOM 中仍存在，不能点击
    function findLeafByText(targetText) {
        const elements = document.querySelectorAll("*");
        for (let i = 0; i < elements.length; i++) {
            const el = elements[i];
            if (el.children.length !== 0 || !isVisible(el)) {
                continue;
            }
            if ((el.textContent || "").trim() === targetText) {
                return el;
            }
        }
        return null;
    }

    function clickLeaf(el) {
        const clickable = el.closest("button") || el;
        clickable.click();
        lastClickAt = Date.now();
    }

    function bodyHas(tokens) {
        const text = document.body ? document.body.innerText : "";
        return tokens.some((token) => text.indexOf(token) !== -1);
    }

    // 只解除开始按钮中心上的空文本透明遮罩；带文字的 SDK 登录/数据弹窗不动
    function neutralizeStartOverlay(button) {
        const rect = button.getBoundingClientRect();
        const x = Math.round(rect.x + rect.width / 2);
        const y = Math.round(rect.y + rect.height / 2);
        const at = document.elementFromPoint(x, y);
        if (!at || at === button || button.contains(at) || at.contains(button)) {
            return false;
        }
        if ((at.innerText || "").trim()) {
            return false;
        }
        at.style.pointerEvents = "none";
        log("start overlay neutralized");
        return true;
    }

    let loggedIn = false;
    let lastErrorAt = 0;

    function tick() {
        if (Date.now() - startedAt >= MAX_RUNTIME_MS) {
            stop();
            return;
        }
        if (isGameRunning()) {
            log("game running");
            stop();
            return;
        }
        // 登录框存在且未见 UID 时 isLoggedIn 为 false，这里统一拦截，不做任何点击
        if (!isLoggedIn()) {
            return;
        }
        if (!loggedIn) {
            loggedIn = true;
            log("login detected");
        }
        if (Date.now() - lastClickAt < CLICK_COOLDOWN_MS) {
            return;
        }
        const ack = findLeafByText(ACK_TEXT);
        if (ack) {
            clickLeaf(ack);
            log("ack clicked");
            return;
        }
        const login = findLeafByText(LOGIN_TEXT);
        if (login) {
            clickLeaf(login);
            log("login clicked");
            return;
        }
        // 云端启动/排队/连接异常期间只等待，避免重复点击把会话打回落地页
        if (bodyHas(BUSY_TEXTS)) {
            return;
        }
        const start = findLeafByText(START_TEXT);
        if (start) {
            // 只解除遮罩；开始游戏由 Pipeline 唯一负责
            neutralizeStartOverlay(start.closest("button") || start);
        }
    }

    function guardedTick() {
        try {
            tick();
        } catch (error) {
            const now = Date.now();
            if (now - lastErrorAt > 10000) {
                lastErrorAt = now;
                console.error("[cloud_start] tick failed", error);
            }
        }
    }

    window.__maaCloudStart.timer = setInterval(guardedTick, POLL_INTERVAL_MS);
    scheduleBallPoll();
})();
