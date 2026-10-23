import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(new URL("../CloudBridge.cpp", import.meta.url), "utf8");
const script = source.match(/constexpr char kPageState\[\] = R"JS\(([\s\S]*?)\)JS";/)?.[1];
assert.ok(script);

function inspect({
    text = "",
    video = true,
    canvas = false,
    button = false,
    decorated = false,
    blocked = false,
    signedIn = false,
    width = 1280,
    busyDialog = false,
    busyHidden = false,
    busyBlocked = false,
    busyDisabled = false,
    unrelatedConfirm = false,
    sdkFailed = false,
    resourceErrors = [],
} = {}) {
    const element = (label) => ({
        textContent: label,
        children: [],
        getClientRects: () => [{}],
        closest: () => null,
        getBoundingClientRect: () => ({x: 1044, y: 570, width: 121, height: 48}),
        contains: () => false,
    });
    const start = button ? element("开始游戏") : null;
    if (start && decorated) start.children = [element("")];
    const message = busyDialog ? element("您上次游戏的数据正在火速处理中，请稍后再试") : null;
    const confirm = busyDialog ? element("知道了") : null;
    if (confirm) {
        confirm.disabled = busyDisabled;
        confirm.getBoundingClientRect = () => ({x: 500, y: 400, width: 100, height: 40});
        message.getClientRects = () => (busyHidden ? [] : [{}]);
        message.parentElement = {
            querySelectorAll: () => [
                message,
                confirm,
            ],
        };
    }
    const elements = [
        start,
        signedIn ? element("退出登录") : null,
        message,
        confirm,
        unrelatedConfirm ? element("知道了") : null,
    ].filter(Boolean);
    const movie = {
        readyState: 4,
        paused: false,
        ended: false,
        videoWidth: width,
        videoHeight: 720,
        getClientRects: () => [{}],
    };
    return vm.runInNewContext(script, {
        window: {
            __maaCloudStartupErrors: sdkFailed
                ? [{type: "resource", source: "https://web.hycdn.cn/hg_web_sdk/lib/sdk.entry.js"}]
                : resourceErrors.map(source => ({type: "resource", source})),
        },
        performance: {timeOrigin: 1},
        document: {
            readyState: "complete",
            body: {innerText: text},
            getElementById: () => (canvas ? {tagName: "CANVAS"} : video ? {tagName: "VIDEO"} : null),
            querySelectorAll: (selector) =>
                selector === "video" ? (video ? [movie] : []) : selector === "input" ? [] : elements,
            elementFromPoint: (x) =>
                x === 550 ? (busyBlocked ? element("遮罩") : confirm) : blocked ? element("遮罩") : start,
        },
        getComputedStyle: () => ({visibility: "visible"}),
        screen: {width: 1280, height: 720},
        devicePixelRatio: 1,
        innerWidth: 1280,
        innerHeight: 720,
    });
}

test("explicit startup and queue states override a stale video", () => {
    for (const [
        text,
        expected,
    ] of [
        [
            "游戏启动中，请耐心等待",
            "starting",
        ],
        [
            "游戏启动中，\n请耐心等待",
            "starting",
        ],
        [
            "正在排队，前方还有 12 人",
            "queue",
        ],
        [
            "由于您长时间未操作，\n本次游戏已自动结束",
            "ended",
        ],
    ]) {
        const state = inspect({text});
        assert.equal(state.page, expected);
        assert.equal(state.video_ready, false);
    }
});

test("a visible previous-session notice blocks stale video and exposes its own confirmation", () => {
    const state = inspect({busyDialog: true});
    assert.equal(state.page, "session_busy");
    assert.equal(state.video_ready, false);
    assert.equal(state.session_busy_confirm_target.x, 550);
    assert.equal(state.session_busy_confirm_target.y, 420);
});

test("a hidden previous-session notice does not block a decoded stream", () => {
    const state = inspect({busyDialog: true, busyHidden: true});
    assert.equal(state.page, "stream");
    assert.equal(state.video_ready, true);
    assert.equal(state.session_busy_confirm_target, null);
});

test("blocked, disabled and unrelated confirmations are never actionable", () => {
    for (const options of [
        {busyBlocked: true},
        {busyDisabled: true},
    ]) {
        const state = inspect({busyDialog: true, ...options});
        assert.equal(state.page, "session_busy");
        assert.equal(state.session_busy_confirm_target, null);
    }
    const state = inspect({unrelatedConfirm: true, text: "其他提示：知道了"});
    assert.equal(state.session_busy_confirm_target, null);
});

test("landing is authenticated independently from stream readiness", () => {
    const state = inspect({video: false, button: true, signedIn: true});
    assert.equal(state.account, "signed_in");
    assert.equal(state.page, "landing");
    assert.equal(state.video_ready, false);
    assert.equal(state.start_available, true);
    assert.equal(state.start_target.x, 1104.5);
    assert.equal(state.start_target.y, 594);
});

test("an obscured start button cannot be clicked", () => {
    const state = inspect({video: false, button: true, blocked: true, signedIn: true});
    assert.equal(state.page, "landing");
    assert.equal(state.start_available, false);
    assert.equal(state.start_target, null);
});

test("the official start label can contain a decorative icon", () => {
    const state = inspect({video: false, button: true, decorated: true, signedIn: true});
    assert.equal(state.page, "landing");
    assert.equal(state.start_available, true);
});

test("only a decoded video is ready", () => {
    assert.equal(inspect().video_ready, true);
    assert.equal(inspect({width: 0}).video_ready, false);
});

test("an unsupported canvas renderer is diagnosed without authorizing daily startup", () => {
    const state = inspect({video: false, canvas: true});
    assert.equal(state.game_surface, "canvas");
    assert.equal(state.video_ready, false);
    assert.equal(state.page, "loading");
    assert.equal(inspect().game_surface, "video");
    assert.equal(inspect({video: false}).game_surface, "none");
});

test("login and expired-session pages never authorize daily startup", () => {
    const state = inspect({text: "手机号登录", video: false});
    assert.equal(state.page, "login");
    assert.equal(state.account, "signed_out");
    assert.equal(state.video_ready, false);
});

test("failed official bootstrap dependencies are distinguished from unrelated resource errors", () => {
    for (const source of [
        "https://web.hycdn.cn/static/js/umd/react/react@18.3.1.js",
        "https://web.hycdn.cn/static/js/umd/react-dom/react-dom@18.3.1.js",
        "https://cloud-game-web-static.hycdn.cn/cloud-game/prod/index.cda000.js",
        "https://cloud-game-web-static.hycdn.cn/cloud-game/prod/commons.80b897.js",
    ]) {
        const options = {video: false, resourceErrors: [source]};
        assert.equal(inspect(options).page, "startup_error");
        assert.equal(inspect({...options, text: "手机号登录"}).startup_resource_failed, false);
        assert.equal(inspect({...options, signedIn: true}).startup_resource_failed, false);
        assert.equal(inspect({...options, video: true}).startup_resource_failed, false);
    }
    for (const source of [
        "https://cloud-game-web-static.hycdn.cn/cloud-game/prod/assets/imgs/logo.146f17.png",
        "https://other.example/sdk.entry.js",
        "https://web.hycdn.cn/hg_web_sdk/lib/sdk.entry.js.extra",
    ]) {
        assert.equal(inspect({video: false, resourceErrors: [source]}).startup_resource_failed, false);
    }
});

test("a failed account SDK is distinct from waiting for user authentication", () => {
    const failed = inspect({video: false, button: true, sdkFailed: true});
    assert.equal(failed.page, "startup_error");
    assert.equal(failed.startup_resource_failed, true);
    assert.equal(inspect({video: false, text: "手机号登录", sdkFailed: true}).startup_resource_failed, false);
    assert.equal(inspect({sdkFailed: true}).startup_resource_failed, false);
    assert.equal(inspect({video: false, button: true, signedIn: true, sdkFailed: true}).startup_resource_failed, false);
});
