#pragma once

namespace cloud
{

// 自动化输入始终投递到网页。真实桌面焦点与指针归用户控制，
// 不允许网页在收到自动化点击后锁定系统鼠标或自行聚焦窗口。
inline constexpr wchar_t kInputPolicy[] =
    LR"JS(
(() => {
    // On GPUs with WebGL2, the site's default image enhancement replaces the
    // decoded video with an OffscreenCanvas. Shared-frame capture and keyboard
    // gating require its normal video output. Disable only the page's WebGL2
    // enhancement capability before SDK detection; transport selection, hardware
    // video decoding, WebGL1 and our 2D pixel-copy path remain available.
    const getContext = HTMLCanvasElement.prototype.getContext;
    Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
        value: function(type, ...args) {
            return type === 'webgl2' ? null : getContext.call(this, type, ...args);
        }, configurable: false, writable: false
    });
    // Keep a small page-local record of startup failures for explicit diagnosis.
    // Resource URLs exclude query strings and fragments; no form data is read.
    const startupErrors = [];
    Object.defineProperty(window, '__maaCloudStartupErrors', {value: startupErrors});
    window.addEventListener('load', event => {
        // A resource retried successfully by the website must not leave an old
        // error authorizing a reload of its now healthy document.
        if (!event.target?.src && !event.target?.href) return;
        const url = new URL(event.target.src || event.target.href, location.href);
        const source = url.origin + url.pathname;
        for (let i = startupErrors.length - 1; i >= 0; --i) {
            if (startupErrors[i].source === source) startupErrors.splice(i, 1);
        }
    }, true);
    window.addEventListener('error', event => {
        if (startupErrors.length >= 20) return;
        if (event.target?.src || event.target?.href) {
            const url = new URL(event.target.src || event.target.href, location.href);
            startupErrors.push({type: 'resource', source: url.origin + url.pathname});
        } else {
            startupErrors.push({type: 'script', name: event.error?.name || 'Error',
                line: event.lineno, column: event.colno});
        }
    }, true);
    window.addEventListener('unhandledrejection', event => {
        if (startupErrors.length < 20) startupErrors.push({type: 'promise', name: event.reason?.name || typeof event.reason});
    });
    window.addEventListener('keydown', event => {
        if (!event.isTrusted) return;
        // The streaming SDK gates keydown after video mouseleave/window blur.
        // Restore its page-local state in the same event, before its document
        // listener runs. This cannot race a blur between two browser RPCs and
        // needs no extra input dispatch, pointer movement or desktop focus.
        const video = document.getElementById('WelinkGameVideo');
        if (video && video.readyState >= 2 && !video.paused) {
            const target = document.getElementById('gamePlayLayer') || video;
            target.dispatchEvent(new MouseEvent('mouseenter', {view: window}));
        }
    }, true);
    window.addEventListener('mousedown', event => {
        if (event.button !== 1) return;
        const target = document.getElementById('gamePlayLayer') || document.getElementById('WelinkGameVideo');
        // Chromium's middle-button autoscroll consumes the next keydown.
        // Keep the game's middle-button event, but suppress browser scrolling.
        if (target?.contains(event.target)) event.preventDefault();
    }, true);
    // The official game menu overlays video pixels (including the reward
    // dismissal target). Automation addresses the video, not this website UI.
    // Exclude only that menu from hit testing during each CDP transaction;
    // restore it immediately so deliberate user clicks remain available.
    // Return whether restoration is needed; unobstructed events need no
    // cleanup RPC or changes to the menu's interaction state.
    let gameMenu = null;
    Object.defineProperty(window, '__maaCloudMouseInput', {
        value: (active, x, y) => {
            if (gameMenu) {
                gameMenu.element.inert = gameMenu.inert;
                gameMenu = null;
            }
            if (!active) return true;
            const video = document.getElementById('WelinkGameVideo');
            if (!video || video.readyState < 2 || video.paused || video.ended) return false;
            const menu = document.querySelector('button[aria-label="游戏菜单"]')?.parentElement;
            if (menu && !menu.contains(video) && !menu.contains(document.getElementById('gamePlayLayer')) &&
                menu.contains(document.elementFromPoint(x, y))) {
                gameMenu = {element: menu, inert: menu.inert};
                menu.inert = true;
            }
            return !!gameMenu;
        }, configurable: false, writable: false
    });
    // CDP exposes absolute positions only. Keep relative camera input at a
    // stable viewport anchor and attach its requested delta to the trusted
    // mouse event before the game's listeners see it. No desktop cursor warp
    // or reverse movement is needed when reaching the viewport edge.
    // Absolute automation moves must not rotate the camera: the SDK also
    // consumes movementX/Y outside pointer lock. Match only the prepared CDP
    // event so ordinary user input retains its browser-provided movement.
    let mouseMove = null;
    Object.defineProperty(window, '__maaCloudMouseMove', {
        value: (x, y, dx, dy) => { mouseMove = x === null ? null : {x, y, dx, dy}; return true; },
        configurable: false, writable: false
    });
    window.addEventListener('mousemove', event => {
        if (!mouseMove || !event.isTrusted || event.clientX !== mouseMove.x || event.clientY !== mouseMove.y) return;
        const {dx, dy} = mouseMove;
        mouseMove = null;
        // The SDK ignores moves with both deltas zero, including menu drags.
        // Keep native drag motion; only unpressed absolute positioning must
        // suppress unintended camera rotation. Explicit relative input keeps
        // its requested delta even when a button is held.
        if (dx === 0 && dy === 0 && event.buttons) return;
        Object.defineProperties(event, {
            movementX: {value: dx}, movementY: {value: dy}
        });
    }, true);
    Object.defineProperty(window, 'focus', {
        value: () => {}, configurable: false, writable: false
    });
    Object.defineProperty(Element.prototype, 'requestPointerLock', {
        value: () => Promise.reject(new DOMException(
            'Desktop pointer capture is disabled in this cloud host.', 'NotAllowedError')),
        configurable: false, writable: false
    });
})();
)JS";

} // namespace cloud
