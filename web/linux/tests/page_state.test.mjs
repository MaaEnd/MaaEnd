// page_state.test.mjs —— 把 page_js.py 里的 PAGE_STATE_JS 抽出来，用假 DOM 跑状态判定。
//
// 做法与上游 MaaEnd#6746 的 agent/cpp-algo/cloud-webview/tests/page_state.test.mjs 一致：
// JS 住在源码字符串里，测试用正则取出来，在 node:vm 里配假 DOM 执行。
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(new URL("../page_js.py", import.meta.url), "utf8");
const script = source.match(/PAGE_STATE_JS = r"""([\s\S]*?)"""/)?.[1];
assert.ok(script, "没能从 page_js.py 抽出 PAGE_STATE_JS");

function el(tag, spec = {}) {
    const node = {
        tagName: tag.toUpperCase(),
        textContent: spec.text ?? "",
        children: spec.children ?? [],
        type: spec.type,
        placeholder: spec.placeholder,
        disabled: !!spec.disabled,
        ariaDisabled: !!spec.ariaDisabled,
        rect: spec.rect ?? {x: 0, y: 0, width: 120, height: 48},
        visible: spec.visible ?? true,
        menuSvg: !!spec.menuSvg,
        getClientRects() {
            return this.visible ? [{}] : [];
        },
        getBoundingClientRect() {
            return this.rect;
        },
        closest(sel) {
            if (sel === '[aria-disabled="true"]') return this.ariaDisabled ? this : null;
            if (sel === "li") return spec.parentLi ?? null;
            return null;
        },
        querySelector(sel) {
            if (sel === ":scope > svg") return this.menuSvg ? {} : null;
            return null;
        },
        querySelectorAll(sel) {
            return collect([this]).filter(match(sel));
        },
        contains(other) {
            if (other === this) return true;
            return function walk(list) {
                return list.some((c) => c === other || walk(c.children ?? []));
            }(this.children);
        },
    };
    return node;
}

function collect(roots) {
    const out = [];
    (function walk(list) {
        for (const n of list) {
            out.push(n);
            walk(n.children ?? []);
        }
    })(roots);
    return out;
}

function match(sel) {
    const tags = sel.split(",").map((s) => s.trim().toUpperCase());
    return (n) => tags.includes(n.tagName);
}

function inspect({
    text = "",
    videos = 0,
    videoReady = false,
    videoPaused = false,
    startButton = false,
    startDecorated = false,
    startCovered = false,
    startDisabled = false,
    startAriaDisabled = false,
    inputs = [],
    userCenter = false,
    userCenterMenu = false,
    logout = false,
    loginLink = false,
    busyDialog = false,
    busyConfirm = false,
    busyConfirmElsewhere = false,
    busyConfirmDisabled = false,
    ended = false,
    starting = false,
    readyState = "complete",
    startupErrors = [],
    withGameSurface = false,
} = {}) {
    const roots = [];
    const videoNodes = [];
    for (let i = 0; i < videos; i++) {
        const v = el("video", {});
        Object.assign(v, {
            readyState: videoReady ? 3 : 0,
            paused: videoPaused,
            ended: false,
            videoWidth: videoReady ? 1920 : 0,
            videoHeight: videoReady ? 1080 : 0,
        });
        videoNodes.push(v);
        roots.push(v);
    }

    let start = null;
    if (startButton) {
        start = el("button", {text: "开始游戏", rect: {x: 1044, y: 570, width: 121, height: 48},
            children: startDecorated ? [el("span", {text: "开始游戏"})] : []});
        start.disabled = startDisabled;
        start.ariaDisabled = startAriaDisabled;
        roots.push(start);
    }

    for (const i of inputs) {
        roots.push(el("input", {type: i.type, placeholder: i.placeholder}));
    }

    let li = null;
    if (userCenter) {
        const label = el("span", {text: "用户中心"});
        li = el("li", {menuSvg: userCenterMenu});
        label.closest = (sel) => (sel === "li" ? li : null);
        roots.push(label);
    }
    if (logout) roots.push(el("a", {text: "退出登录"}));
    if (loginLink) roots.push(el("a", {text: "登录"}));

    let busyMessage = null;
    let busyBtn = null;
    if (busyDialog) {
        busyBtn = el("button", {text: "知道了", disabled: busyConfirmDisabled});
        const wrap = el("div", {children: [busyBtn]});
        busyMessage = el("div", {text: "您上次游戏的数据正在火速处理中，请稍后再试"});
        const root = el("div", {children: [busyMessage, wrap]});
        busyMessage.parentElement = wrap;
        wrap.parentElement = root;
        roots.push(root);
        void busyBtn;
    }
    if (busyConfirmElsewhere) roots.push(el("button", {text: "知道了"}));

    const bodyText = [text, ended ? "本次游戏已自动结束" : "", starting ? "游戏启动中，请耐心等待" : ""]
        .filter(Boolean).join("\n");

    let hitElement = start;
    if (busyDialog && busyConfirm && !busyConfirmDisabled) hitElement = busyBtn;
    const document = {
        readyState,
        body: {innerText: bodyText},
        querySelectorAll: (sel) => collect(roots).filter(match(sel)),
        getElementById: (id) => {
            if (id === "WelinkGameVideo" && withGameSurface) return el("video", {});
            return null;
        },
        elementFromPoint: () => (startCovered ? el("div", {text: "遮罩"}) : hitElement),
    };

    const sandbox = {
        document,
        getComputedStyle: (e) => ({visibility: e.visible ? "visible" : "hidden"}),
        performance: {timeOrigin: 1730000000000},
        innerWidth: 1280,
        innerHeight: 720,
        screen: {width: 1920, height: 1080},
        devicePixelRatio: 1.5,
        window: {__maaCloudStartupErrors: startupErrors},
        MouseEvent: class {},
        Object,
        JSON,
        Math,
        RegExp,
    };
    sandbox.window.document = document;
    const value = vm.runInNewContext(script, sandbox);
    return value;
}

test("空白页 → loading", () => {
    const s = inspect({});
    assert.equal(s.page, "loading");
    assert.equal(s.account, "unknown");
    assert.equal(s.video_ready, false);
});

test("落地页有可点的开始游戏 → landing + start_target", () => {
    const s = inspect({startButton: true});
    assert.equal(s.page, "landing");
    assert.equal(s.start_available, true);
    assert.equal(s.start_target.x, 1104.5);
    assert.equal(s.start_target.y, 594);
});

test("开始游戏被遮罩挡住 → landing 但没有 start_target", () => {
    const s = inspect({startButton: true, startCovered: true});
    assert.equal(s.page, "landing");
    assert.equal(s.start_available, false);
    assert.equal(s.start_target, null);
});

test("开始游戏 disabled / aria-disabled → 不可点", () => {
    assert.equal(inspect({startButton: true, startDisabled: true}).start_available, false);
    assert.equal(inspect({startButton: true, startAriaDisabled: true}).start_available, false);
});

test("按钮里套了同名子节点 → 点到叶子，不点外层容器", () => {
    const s = inspect({startButton: true, startDecorated: true});
    assert.equal(s.page, "landing");
    assert.ok(s.start_target, "叶子节点本身可以点");
});

test("有 <video> 但没出帧 → stream 但 video_ready=false", () => {
    const s = inspect({videos: 1});
    assert.equal(s.page, "stream");
    assert.equal(s.video_ready, false);
});

test("有 <video> 且在播 → stream + video_ready", () => {
    const s = inspect({videos: 1, videoReady: true});
    assert.equal(s.page, "stream");
    assert.equal(s.video_ready, true);
    assert.equal(s.video_width, 1920);
});

test("排队文案 → queue，且 video_ready 被压掉", () => {
    const s = inspect({videos: 1, videoReady: true, text: "正在排队 前方还有 12 人"});
    assert.equal(s.page, "queue");
    assert.equal(s.video_ready, false);
});

test("登录页（密码框）→ login + signed_out", () => {
    const s = inspect({inputs: [{type: "password"}]});
    assert.equal(s.page, "login");
    assert.equal(s.account, "signed_out");
});

test("已登录（用户中心带下拉箭头）→ signed_in", () => {
    const s = inspect({userCenter: true, userCenterMenu: true});
    assert.equal(s.account, "signed_in");
});

test("只有「用户中心」文字、没有下拉箭头 → 不算已登录", () => {
    const s = inspect({userCenter: true, userCenterMenu: false});
    assert.equal(s.account, "unknown");
});

test("忙提示弹窗 → session_busy + 能定位到「知道了」", () => {
    const s = inspect({busyDialog: true, busyConfirm: true});
    assert.equal(s.page, "session_busy");
    assert.ok(s.session_busy_confirm_target);
});

test("「知道了」在别处 → 不给确认坐标", () => {
    const s = inspect({busyDialog: true, busyConfirmElsewhere: true});
    assert.equal(s.page, "session_busy");
    assert.equal(s.session_busy_confirm_target, null);
});

test("会话结束文案 → ended", () => {
    const s = inspect({ended: true});
    assert.equal(s.page, "ended");
    assert.equal(s.ended, true);
});

test("SDK 资源加载失败 → startup_error", () => {
    const s = inspect({
        startupErrors: [{type: "resource", source: "https://web.hycdn.cn/hg_web_sdk/lib/sdk.entry.js"}],
    });
    assert.equal(s.page, "startup_error");
    assert.equal(s.startup_resource_failed, true);
});

test("无关资源失败 → 不误判 startup_error", () => {
    const s = inspect({startupErrors: [{type: "resource", source: "https://example.com/a.js"}]});
    assert.equal(s.page, "loading");
});

test("视口 / DPR / document_epoch 都带回来", () => {
    const s = inspect({});
    assert.equal(s.viewport_width, 1280);
    assert.equal(s.viewport_height, 720);
    assert.equal(s.device_pixel_ratio, 1.5);
    assert.equal(s.screen_width, 2880);
    assert.equal(s.document_epoch, 1730000000000);
});
