"""页面侧 JS：状态求值 + 输入策略。

两段都从 MaaEnd#6746（agent/cpp-algo/cloud-webview）移植，保持**逐字一致**——
它们是页面侧逻辑，与 Windows/Linux 无关；以后上游改了只要 diff 这两个字符串。

- ``PAGE_STATE_JS``  ←  ``CloudBridge.cpp`` 的 ``kPageState``
- ``INPUT_POLICY_JS`` ←  ``input_policy.h`` 的 ``kInputPolicy``

改造点只有一个：本文件用 Python raw string 承载，测试用正则从源码里抽出来跑（
和上游 ``page_state.test.mjs`` 一样的做法）。
"""

from __future__ import annotations

# 页面状态求值：一次调用返回结构化状态，决策留给调用方。
# page ∈ ended | startup_error | login | session_busy | queue | starting | stream | landing | loading
PAGE_STATE_JS = r"""
(() => {
    const visible = e => !!(e && e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden');
    const video = [...document.querySelectorAll('video')].find(visible);
    const gameSurface = document.getElementById('WelinkGameVideo');
    const text = document.body?.innerText || '';
    const normalizedText = text.replace(/\s+/g, '');
    const ended = /本次游戏已自动结束|游戏已自动结束/.test(normalizedText);
    const starting = /游戏启动中[，,]?请耐心等待/.test(normalizedText);
    const loginForm = [...document.querySelectorAll('input')].some(e => visible(e) &&
        (e.type === 'password' || /手机号|验证码|手机号码/.test(e.placeholder)));
    const loginPrompt = /扫码登录|扫描二维码|短信登录|密码登录|账号登录|手机号登录/.test(text) || loginForm;
    const userCenterLabel = [...document.querySelectorAll('a,button,span,div')].find(e =>
        visible(e) && e.children.length === 0 && e.textContent.trim() === '用户中心');
    // 只有已登录的个人菜单才会有这个下拉箭头；光有「用户中心」四个字是公开页面。
    const userCenter = userCenterLabel?.closest('li');
    const hasAccountMenu = !!userCenter?.querySelector(':scope > svg');
    const hasLogout = [...document.querySelectorAll('a,button,span,div')].some(e =>
        visible(e) && e.children.length === 0 && e.textContent.trim() === '退出登录');
    const hasLogin = [...document.querySelectorAll('a,button,span,div')].some(e =>
        visible(e) && e.children.length === 0 && /^(登录|立即登录|登 录)$/.test(e.textContent.trim()));
    const startButton = [...document.querySelectorAll('button,a,div,span')].find(e =>
        visible(e) && e.textContent.trim() === '开始游戏' &&
        ![...e.children].some(child => child.textContent.trim() === '开始游戏'));
    const clickTarget = element => {
        if (!element || !visible(element) || element.disabled || element.closest('[aria-disabled="true"]')) return null;
        const rect = element.getBoundingClientRect();
        const x = rect.x + rect.width / 2, y = rect.y + rect.height / 2;
        const hit = document.elementFromPoint(x, y);
        return hit && (element === hit || element.contains(hit) ||
            (hit.contains(element) && hit.textContent.trim() === element.textContent.trim())) ? {x, y} : null;
    };
    const startTarget = clickTarget(startButton);
    const ready = document.readyState === 'complete';
    const videoReady = !ended && !starting && !!video && video.readyState >= 2 && !video.paused &&
        !video.ended && video.videoWidth > 0 && video.videoHeight > 0;
    const authenticated = ready && !loginPrompt && !hasLogin &&
        (hasAccountMenu || hasLogout || videoReady);
    const queue = /正在排队|排队中|前方还有|预计等待/.test(text);
    const startupResourceFailed = !authenticated && !loginPrompt && !video &&
        (window.__maaCloudStartupErrors || []).some(error => error.type === 'resource' && (
            error.source === 'https://web.hycdn.cn/hg_web_sdk/lib/sdk.entry.js' ||
            error.source === 'https://web.hycdn.cn/static/js/umd/react/react@18.3.1.js' ||
            error.source === 'https://web.hycdn.cn/static/js/umd/react-dom/react-dom@18.3.1.js' ||
            /^https:\/\/cloud-game-web-static\.hycdn\.cn\/cloud-game\/prod\/[\w.-]+\.js$/.test(error.source)));
    const busyMessage = '您上次游戏的数据正在火速处理中，请稍后再试';
    const message = [...document.querySelectorAll('div,span,p')].find(e => visible(e) &&
        e.textContent.replace(/\s+/g, '') === busyMessage &&
        ![...e.children].some(child => child.textContent.replace(/\s+/g, '') === busyMessage));
    const sessionBusy = !!message;
    const busyConfirmTarget = (() => {
        // 只在这次官方报错的祖先链里找「知道了」，不碰页面上别处的同类按钮。
        for (let parent = message?.parentElement; parent && parent !== document.body; parent = parent.parentElement) {
            const confirm = [...parent.querySelectorAll('button,a,div,span')].find(e =>
                visible(e) && e.textContent.trim() === '知道了' &&
                ![...e.children].some(child => child.textContent.trim() === '知道了'));
            if (confirm) return clickTarget(confirm);
        }
        return null;
    })();
    return {account: authenticated ? 'signed_in' : (ready && (loginPrompt || hasLogin) ? 'signed_out' : 'unknown'),
        page: ended ? 'ended' : startupResourceFailed ? 'startup_error' : loginPrompt ? 'login' : sessionBusy ? 'session_busy' : queue ? 'queue' : starting ? 'starting' : video ? 'stream' : startButton ? 'landing' : 'loading',
        video_ready: !queue && !sessionBusy && videoReady,
        ended, startup_resource_failed: startupResourceFailed,
        video_width: video?.videoWidth || 0, video_height: video?.videoHeight || 0,
        game_surface: gameSurface?.tagName.toLowerCase() || 'none',
        screen_width: Math.round(screen.width * devicePixelRatio),
        screen_height: Math.round(screen.height * devicePixelRatio),
        viewport_width: innerWidth, viewport_height: innerHeight,
        device_pixel_ratio: devicePixelRatio,
        start_available: !!startTarget, start_target: startTarget,
        session_busy_confirm_target: busyConfirmTarget, document_ready: ready,
        document_epoch: performance.timeOrigin};
})()
"""

# 输入策略：在页面里修掉「自动化输入」这一类问题，而不是在外部加重试。
INPUT_POLICY_JS = r"""
(() => {
    // 站点的「画质增强」在有 WebGL2 的 GPU 上会把解码后的 <video> 换成 OffscreenCanvas，
    // 取帧和键盘门控都会失效。只禁 WebGL2 这一个能力，传输方式、硬解、WebGL1、2D 拷贝都还在。
    const getContext = HTMLCanvasElement.prototype.getContext;
    Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
        value: function(type, ...args) {
            return type === 'webgl2' ? null : getContext.call(this, type, ...args);
        }, configurable: false, writable: false
    });
    // 启动失败的现场（资源/脚本/未处理的 promise），只记来源，不读表单内容。
    const startupErrors = [];
    Object.defineProperty(window, '__maaCloudStartupErrors', {value: startupErrors});
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
        // 串流 SDK 在 video mouseleave / 窗口 blur 之后会门控 keydown。
        // 在同一个事件里、在它的 document 监听器之前把页面本地状态补回来——
        // 这样两个 RPC 之间没有竞态，也不需要额外输入或桌面焦点。
        const video = document.getElementById('WelinkGameVideo');
        if (video && video.readyState >= 2 && !video.paused) {
            const target = document.getElementById('gamePlayLayer') || video;
            target.dispatchEvent(new MouseEvent('mouseenter', {view: window}));
        }
    }, true);
    window.addEventListener('mousedown', event => {
        if (event.button !== 1) return;
        const target = document.getElementById('gamePlayLayer') || document.getElementById('WelinkGameVideo');
        // Chromium 的中键自动滚屏会吃掉下一次 keydown。保留游戏的中键事件，只压掉浏览器滚动。
        if (target?.contains(event.target)) event.preventDefault();
    }, true);
    // CDP 只能给绝对坐标。把相对位移挂在可信 mousemove 上，视口中心做锚点，
    // 页面读到的 movementX/Y 就是我们要的增量——不用移真实光标，也不会撞到视口边缘。
    let relativeMove = null;
    Object.defineProperty(window, '__maaCloudRelativeMove', {
        value: (dx, dy) => { relativeMove = dx === null ? null : {dx, dy}; return true; },
        configurable: false, writable: false
    });
    window.addEventListener('mousemove', event => {
        if (!relativeMove || !event.isTrusted || event.clientX !== 640 || event.clientY !== 360) return;
        const {dx, dy} = relativeMove;
        relativeMove = null;
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
"""

# 相对移动的锚点：策略脚本里写死 (640,360)，即 1280x720 视口的中心。
RELATIVE_ANCHOR = (640, 360)
