// input_policy.test.mjs —— 抽 page_js.py 里的 INPUT_POLICY_JS，用假环境验证六条策略。
//
// 与上游 MaaEnd#6746 的 tests/stream_policy.test.mjs 同一思路：
// 策略是页面侧 JS，直接在 node:vm 里跑，不需要浏览器。
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(new URL("../page_js.py", import.meta.url), "utf8");
const script = source.match(/INPUT_POLICY_JS = r"""([\s\S]*?)"""/)?.[1];
assert.ok(script, "没能从 page_js.py 抽出 INPUT_POLICY_JS");

function makeEnv({
    videoReady = true,
    videoPaused = false,
    hasVideo = true,
    hasLayer = true,
} = {}) {
    const listeners = new Map();
    const dispatched = [];
    const prevented = [];

    class HTMLCanvasElement {
        getContext(type) {
            if (type === "2d") return {kind: "2d"};
            if (type === "webgl") return {kind: "webgl"};
            if (type === "webgl2") return {kind: "webgl2"};
            return null;
        }
    }
    class Element {
        constructor(tag) {
            this.tag = tag;
            this.inside = false;
        }
        contains(other) {
            return other === this || other?.inside === true;
        }
        dispatchEvent(ev) {
            dispatched.push(ev);
            return true;
        }
    }
    const video = new Element("video");
    video.readyState = videoReady ? 3 : 0;
    video.paused = videoPaused;
    const layer = new Element("div");
    const other = new Element("div");

    const document = {
        getElementById: (id) => {
            if (id === "WelinkGameVideo") return hasVideo ? video : null;
            if (id === "gamePlayLayer") return hasLayer ? layer : null;
            return null;
        },
    };

    const window = {
        addEventListener(type, fn, capture) {
            listeners.set(type, {fn, capture});
        },
        document,
    };

    class MouseEvent {
        constructor(type, init = {}) {
            this.type = type;
            this.view = init.view;
            this.isTrusted = true;
        }
    }

    const sandbox = {
        HTMLCanvasElement,
        Element,
        MouseEvent,
        document,
        window,
        location: {href: "https://game.skland.com/"},
        URL,
        Object,
        Promise,
        DOMException,
        JSON,
        RegExp,
        Math,
        Array,
    };
    vm.runInNewContext(script, sandbox);

    function fire(type, event) {
        const entry = listeners.get(type);
        if (!entry) throw new Error(`没有注册 ${type} 监听`);
        event.prevented = false;
        event.preventDefault = () => {
            event.prevented = true;
            prevented.push(type);
        };
        entry.fn(event);
        return event;
    }

    return {sandbox, listeners, dispatched, prevented, fire, video, layer, other, HTMLCanvasElement, window, Element};
}

test("禁掉 webgl2，2d / webgl 仍可用", () => {
    const env = makeEnv();
    const c = new env.HTMLCanvasElement();
    assert.equal(c.getContext("webgl2"), null);
    assert.deepEqual(c.getContext("2d"), {kind: "2d"});
    assert.deepEqual(c.getContext("webgl"), {kind: "webgl"});
});

test("可信 keydown → 给 gamePlayLayer 补一次 mouseenter", () => {
    const env = makeEnv();
    env.fire("keydown", {isTrusted: true});
    assert.equal(env.dispatched.length, 1);
    assert.equal(env.dispatched[0].type, "mouseenter");
});

test("不可信 keydown（脚本合成）→ 不补", () => {
    const env = makeEnv();
    env.fire("keydown", {isTrusted: false});
    assert.equal(env.dispatched.length, 0);
});

test("没有 video / 视频没就绪 → 不补 mouseenter", () => {
    assert.equal(makeEnv({hasVideo: false}).dispatched.length, 0);
    const env = makeEnv({videoReady: false});
    env.fire("keydown", {isTrusted: true});
    assert.equal(env.dispatched.length, 0);
    const paused = makeEnv({videoPaused: true});
    paused.fire("keydown", {isTrusted: true});
    assert.equal(paused.dispatched.length, 0);
});

test("中键落在游戏区域 → 阻止浏览器自动滚屏", () => {
    const env = makeEnv();
    env.other.inside = true;
    const ev = env.fire("mousedown", {button: 1, target: env.other, preventDefault() {}});
    assert.equal(ev.prevented, true);
});

test("中键落在游戏区域外 / 左键 → 不阻止", () => {
    const env = makeEnv();
    const outside = env.fire("mousedown", {button: 1, target: {inside: false}, preventDefault() {}});
    assert.notEqual(outside.prevented, true);
    const left = env.fire("mousedown", {button: 0, target: env.other, preventDefault() {}});
    assert.notEqual(left.prevented, true);
});

test("相对移动：中心点的可信 mousemove 被改写成 movementX/Y", () => {
    const env = makeEnv();
    assert.equal(env.window.__maaCloudRelativeMove(12, -7), true);
    const ev = env.fire("mousemove", {
        isTrusted: true, clientX: 640, clientY: 360, movementX: 0, movementY: 0,
    });
    assert.equal(ev.movementX, 12);
    assert.equal(ev.movementY, -7);
});

test("相对移动只用一次：第二次移动不再改写", () => {
    const env = makeEnv();
    env.window.__maaCloudRelativeMove(5, 5);
    env.fire("mousemove", {isTrusted: true, clientX: 640, clientY: 360, movementX: 0, movementY: 0});
    const second = env.fire("mousemove", {
        isTrusted: true, clientX: 640, clientY: 360, movementX: 0, movementY: 0,
    });
    assert.equal(second.movementX, 0);
});

test("非中心点 / 不可信的 mousemove → 不改写", () => {
    const env = makeEnv();
    env.window.__maaCloudRelativeMove(9, 9);
    const offCenter = env.fire("mousemove", {
        isTrusted: true, clientX: 100, clientY: 100, movementX: 0, movementY: 0,
    });
    assert.equal(offCenter.movementX, 0);
    const untrusted = env.fire("mousemove", {
        isTrusted: false, clientX: 640, clientY: 360, movementX: 3, movementY: 3,
    });
    assert.equal(untrusted.movementX, 3);
});

test("window.focus 被废掉（网页不能抢焦点）", () => {
    const env = makeEnv();
    assert.equal(typeof env.window.focus, "function");
    env.window.focus();
    assert.equal(env.window.focus.length, 0);
});

test("requestPointerLock 直接 reject（不能锁光标）", async () => {
    const env = makeEnv();
    const p = new env.Element("div").requestPointerLock();
    await assert.rejects(p, (err) => err.name === "NotAllowedError");
});

test("启动失败记录：资源错误只留 origin + pathname", () => {
    const env = makeEnv();
    env.fire("error", {
        target: {src: "https://web.hycdn.cn/static/js/a.js?v=3#frag"},
        preventDefault() {},
    });
    const list = env.window.__maaCloudStartupErrors;
    assert.equal(list.length, 1);
    assert.equal(list[0].type, "resource");
    assert.equal(list[0].source, "https://web.hycdn.cn/static/js/a.js");
});

test("启动失败记录：脚本错误与未处理 promise 也记，最多 20 条", () => {
    const env = makeEnv();
    env.fire("error", {error: {name: "TypeError"}, lineno: 3, colno: 9, preventDefault() {}});
    env.fire("unhandledrejection", {reason: {name: "AbortError"}});
    const list = env.window.__maaCloudStartupErrors;
    assert.equal(list[0].type, "script");
    assert.equal(list[0].name, "TypeError");
    assert.equal(list[1].type, "promise");
    for (let i = 0; i < 30; i++) {
        env.fire("error", {error: {name: "E"}, lineno: 1, colno: 1, preventDefault() {}});
    }
    assert.equal(list.length, 20);
});
