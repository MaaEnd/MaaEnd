import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(new URL("../input_policy.h", import.meta.url), "utf8");
const script = source.match(/LR"JS\(([\s\S]*?)\)JS";/)?.[1];
assert.ok(script);

function browser({video = null, menu = null, layer = null, hitTest = null} = {}) {
    const listeners = new Map();
    const window = {
        addEventListener(type, callback) {
            const callbacks = listeners.get(type) ?? [];
            callbacks.push(callback);
            listeners.set(type, callbacks);
        },
    };
    const document = {
        getElementById: (id) => (id === "WelinkGameVideo" ? video : layer),
        elementFromPoint: (x, y) => (hitTest ? hitTest(x, y) : menu),
        querySelector(selector) {
            assert.equal(selector, 'button[aria-label="游戏菜单"]');
            return menu ? {parentElement: menu} : null;
        },
    };
    vm.runInNewContext(script, {
        window,
        document,
        HTMLCanvasElement: class {},
        Element: class {},
        URL,
        location: {href: "https://endfield.hypergryph.com/cloud"},
    });
    return {
        prepare: window.__maaCloudMouseMove,
        input: (active, x = 30, y = 80) => window.__maaCloudMouseInput(active, x, y),
        replaceMenu: (replacement) => {
            menu = replacement;
        },
        resource(type, source) {
            const event = {target: {src: source}};
            for (const listener of listeners.get(type) ?? []) listener(event);
        },
        errors: () => window.__maaCloudStartupErrors,
        move(x, y, dx, dy, trusted = true, buttons = 0) {
            const event = {clientX: x, clientY: y, movementX: dx, movementY: dy, isTrusted: trusted, buttons};
            for (const listener of listeners.get("mousemove") ?? []) listener(event);
            return [
                event.movementX,
                event.movementY,
            ];
        },
    };
}

test("a successful resource retry removes its stale startup error without hiding unrelated failures", () => {
    const page = browser();
    const sdk = "https://web.hycdn.cn/hg_web_sdk/lib/sdk.entry.js";
    const react = "https://web.hycdn.cn/static/js/umd/react/react@18.3.1.js";
    page.resource("error", sdk + "?first=1#fragment");
    page.resource("error", react);
    assert.equal(page.errors().length, 2);
    assert.equal(page.errors()[0].source, sdk);
    page.resource("load", sdk + "?retry=2");
    assert.equal(page.errors().length, 1);
    assert.equal(page.errors()[0].source, react);
    page.resource("load", sdk);
    assert.equal(page.errors().length, 1);
});

test("automation excludes the official game menu only during its mouse transaction", () => {
    const menu = {
        inert: false,
        contains(element) {
            return element === this;
        },
    };
    const page = browser({video: {readyState: 2, paused: false, ended: false}, menu});
    assert.equal(page.input(true), true);
    assert.equal(menu.inert, true);
    assert.equal(page.input(false), true);
    assert.equal(menu.inert, false);
    // Cleanup is idempotent and another transaction restores its own state.
    page.input(false);
    menu.inert = true;
    page.input(true);
    page.input(false);
    assert.equal(menu.inert, true);
});

test("landing, paused and ended pages keep their website controls", () => {
    for (const video of [
        null,
        {readyState: 1, paused: false},
        {readyState: 2, paused: true},
        {readyState: 2, paused: false, ended: true},
    ]) {
        const menu = {
            inert: false,
            contains(element) {
                return element === this;
            },
        };
        const page = browser({video, menu});
        page.input(true);
        assert.equal(menu.inert, false);
        page.input(false);
    }
});

test("an absent menu and an ancestor of the game surface are not disabled", () => {
    const video = {readyState: 2, paused: false};
    const menu = {inert: false, contains: (element) => element === video};
    const page = browser({video, menu});
    page.input(true);
    assert.equal(menu.inert, false);
    page.input(false);
    const absent = browser({video});
    assert.equal(absent.input(true), false);
    assert.equal(absent.input(false), true);
});

test("replaced menu nodes and interrupted transactions restore the previous menu", () => {
    const menu = {
        inert: false,
        contains(element) {
            return element === this;
        },
    };
    const page = browser({video: {readyState: 2, paused: false}, menu});
    page.input(true);
    page.input(true);
    const replacement = {
        inert: false,
        contains(element) {
            return element === this;
        },
    };
    page.replaceMenu(replacement);
    page.input(true);
    assert.equal(menu.inert, false);
    assert.equal(replacement.inert, true);
    page.input(false);
    assert.equal(menu.inert, false);
    assert.equal(replacement.inert, false);
});

test("unobstructed game input does not change menu hit testing", () => {
    const menu = {
        inert: false,
        contains(element) {
            return element === this;
        },
    };
    let changes = 0;
    Object.defineProperty(menu, "inert", {
        get: () => false,
        set: () => {
            changes++;
        },
    });
    const page = browser({video: {readyState: 2, paused: false}, menu, hitTest: () => null});
    assert.equal(page.input(true, 640, 360), false);
    assert.equal(changes, 0);
});

test("absolute automation moves do not become camera deltas", () => {
    const page = browser();
    for (const x of [
        640,
        740,
        840,
        640,
    ]) {
        assert.equal(page.prepare(x, 360, 0, 0), true);
        assert.deepEqual(
            page.move(x, 360, 100, -10),
            [
                0,
                0,
            ],
        );
    }
    assert.deepEqual(
        page.move(650, 360, 10, 5),
        [
            10,
            5,
        ],
    );
});

test("relative camera movement is delivered once at its anchor", () => {
    const page = browser();
    page.prepare(640, 360, 127, -42);
    assert.deepEqual(
        page.move(640, 360, 0, 0),
        [
            127,
            -42,
        ],
    );
    assert.deepEqual(
        page.move(640, 360, 4, 3),
        [
            4,
            3,
        ],
    );
});

test("absolute dragging retains motion while relative dragging uses its requested delta", () => {
    const page = browser();
    page.prepare(740, 360, 0, 0);
    assert.deepEqual(
        page.move(740, 360, 100, 5, true, 1),
        [
            100,
            5,
        ],
    );
    page.prepare(640, 360, 40, -10);
    assert.deepEqual(
        page.move(640, 360, -100, 0, true, 1),
        [
            40,
            -10,
        ],
    );
    page.prepare(740, 360, 0, 0);
    assert.deepEqual(
        page.move(740, 360, 100, 5),
        [
            0,
            0,
        ],
    );
});

test("unrelated and synthetic events cannot consume prepared movement", () => {
    const page = browser();
    page.prepare(640, 360, 80, 0);
    assert.deepEqual(
        page.move(641, 360, 1, 0),
        [
            1,
            0,
        ],
    );
    assert.deepEqual(
        page.move(640, 361, 0, 1),
        [
            0,
            1,
        ],
    );
    assert.deepEqual(
        page.move(640, 360, 2, 3, false),
        [
            2,
            3,
        ],
    );
    assert.deepEqual(
        page.move(640, 360, 0, 0),
        [
            80,
            0,
        ],
    );
});

test("cancelled dispatch leaves subsequent user movement intact", () => {
    const page = browser();
    page.prepare(640, 360, 80, 0);
    page.prepare(null, null, null, null);
    assert.deepEqual(
        page.move(640, 360, 7, -6),
        [
            7,
            -6,
        ],
    );
});
