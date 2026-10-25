// 云终末地的页面策略，按输入来源区分控制器输入与用户的物理输入。外壳调用：
//   __maaShellExpect(x, y)         派发控制器的鼠标事件前登记坐标；派发按键前不带参数调用
//   __maaShellSetForeground(bool)  外壳窗口是否在前台
(() => {
    // 只覆盖真正最小化：Chromium 会把页面标记为隐藏，SDK 随即停发心跳；遮挡由浏览器启动参数处理
    for (const [name, value] of [["hidden", false], ["visibilityState", "visible"]]) {
        Object.defineProperty(Document.prototype, name, {configurable: true, get: () => value});
    }

    // WebGL2 画质增强的像素随显卡而变，还会替换下面按键修复依赖的原始视频
    const getContext = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (type, ...args) {
        return type === "webgl2" ? null : getContext.call(this, type, ...args);
    };

    const kExpectTtlMs = 1000;
    const expected = [];
    let foreground = true;
    let lastAutomatedAt = -Infinity;
    let lastPhysicalPressAt = -Infinity;

    window.__maaShellExpect = (x, y) => {
        lastAutomatedAt = performance.now();
        if (x === undefined) return;
        expected.push({x, y, time: lastAutomatedAt});
        if (expected.length > 64) expected.shift();
    };
    window.__maaShellSetForeground = (value) => {
        foreground = !!value;
    };

    // 按事件的创建时间判断过期：主线程被长任务占住时，处理时间会比派发晚得多
    function isExpected(event) {
        while (expected.length && event.timeStamp - expected[0].time > kExpectTtlMs) expected.shift();
        return expected.some((item) => item.x === event.clientX && item.y === event.clientY);
    }

    // 点击始终放行：在后台窗口上点击属于用户的主动操作。SDK 在游戏元素上同时监听了鼠标与指针事件
    const kHoverTypes = [
        "mousemove", "mouseover", "mouseout", "mouseenter", "mouseleave", "wheel", "mousewheel",
        "pointermove", "pointerrawupdate", "pointerover", "pointerout", "pointerenter", "pointerleave",
    ];
    const kMoveTypes = new Set(["mousemove", "pointermove", "pointerrawupdate"]);
    for (const type of [...kHoverTypes, "mousedown"]) {
        window.addEventListener(
            type,
            (event) => {
                if (!event.isTrusted) return;
                const automated = isExpected(event);
                if (type === "mousedown") {
                    if (!automated) lastPhysicalPressAt = performance.now();
                    return;
                }

                if (!automated) {
                    if (!foreground) {
                        event.stopImmediatePropagation();
                        if (event.cancelable) event.preventDefault();
                    }
                    return;
                }

                // SDK 丢弃位移为 0 的移动，控制器点击前的定位移动就不会带动镜头；拖动保留位移
                if (kMoveTypes.has(type) && !event.buttons) {
                    Object.defineProperties(event, {movementX: {value: 0}, movementY: {value: 0}});
                }
            },
            {capture: true, passive: false},
        );
    }

    // 只有最近一次物理点击晚于最后一次控制器输入时才放行指针锁定。
    // 必须保持可写：SDK 在严格模式下先执行 e.requestPointerLock = e.requestPointerLock，
    // 只读属性会让赋值抛异常并被 SDK 吞掉，锁定根本发不出去
    const requestPointerLock = Element.prototype.requestPointerLock;
    Element.prototype.requestPointerLock = function (...args) {
        if (lastAutomatedAt >= lastPhysicalPressAt) {
            return Promise.reject(new DOMException("Pointer lock is not available after automated input.", "NotAllowedError"));
        }
        return requestPointerLock.apply(this, args);
    };

    // 鼠标离开视频或窗口失焦后 SDK 不再发送按键，直到视频收到 mouseenter；
    // 在同一个 keydown 里、SDK 的 document 监听之前补发
    window.addEventListener(
        "keydown",
        (event) => {
            if (!event.isTrusted) return;
            const video = document.getElementById("WelinkGameVideo");
            if (video && video.readyState >= 2 && !video.paused) {
                const target = document.getElementById("gamePlayLayer") || video;
                target.dispatchEvent(new MouseEvent("mouseenter", {view: window}));
            }
        },
        true,
    );
})();
