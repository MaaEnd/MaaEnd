(() => {
    const visible = (e) => !!(e && e.getClientRects().length && getComputedStyle(e).visibility !== "hidden");
    const video = [...document.querySelectorAll("video")].find(visible);
    const gameSurface = document.getElementById("WelinkGameVideo");
    const text = document.body?.innerText || "";
    const normalizedText = text.replace(/\s+/g, "");
    const ended = /本次游戏已自动结束|游戏已自动结束/.test(normalizedText);
    const starting = /游戏启动中[，,]?请耐心等待/.test(normalizedText);
    const loginForm = [...document.querySelectorAll("input")].some(
        (e) => visible(e) && (e.type === "password" || /手机号|验证码|手机号码/.test(e.placeholder)),
    );
    const loginPrompt = /扫码登录|扫描二维码|短信登录|密码登录|账号登录|手机号登录/.test(text) || loginForm;
    const userCenterLabel = [...document.querySelectorAll("a,button,span,div")].find(
        (e) => visible(e) && e.children.length === 0 && e.textContent.trim() === "用户中心",
    );
    // The official desktop header renders a direct dropdown arrow only when
    // its authenticated personalItems menu exists. The label alone is public.
    const userCenter = userCenterLabel?.closest("li");
    const hasAccountMenu = !!userCenter?.querySelector(":scope > svg");
    const hasLogout = [...document.querySelectorAll("a,button,span,div")].some(
        (e) => visible(e) && e.children.length === 0 && e.textContent.trim() === "退出登录",
    );
    const hasLogin = [...document.querySelectorAll("a,button,span,div")].some(
        (e) => visible(e) && e.children.length === 0 && /^(登录|立即登录|登 录)$/.test(e.textContent.trim()),
    );
    const startButton = [...document.querySelectorAll("button,a,div,span")].find(
        (e) =>
            visible(e) &&
            e.textContent.trim() === "开始游戏" &&
            ![...e.children].some((child) => child.textContent.trim() === "开始游戏"),
    );
    const clickTarget = (element) => {
        if (!element || !visible(element) || element.disabled || element.closest('[aria-disabled="true"]')) return null;
        const rect = element.getBoundingClientRect();
        const x = rect.x + rect.width / 2,
            y = rect.y + rect.height / 2;
        const hit = document.elementFromPoint(x, y);
        return hit &&
            (element === hit ||
                element.contains(hit) ||
                (hit.contains(element) && hit.textContent.trim() === element.textContent.trim()))
            ? {x, y}
            : null;
    };
    const startTarget = clickTarget(startButton);
    const findMessage = (text) =>
        [...document.querySelectorAll("div,span,p")].find(
            (e) =>
                visible(e) &&
                e.textContent.replace(/\s+/g, "") === text &&
                ![...e.children].some((child) => child.textContent.replace(/\s+/g, "") === text),
        );
    const confirmationTarget = (message) => {
        const dialogSelector = 'dialog,[role="dialog"],[aria-modal="true"]';
        const dialog = message?.closest(dialogSelector);
        for (let parent = message?.parentElement; parent && parent !== document.body; parent = parent.parentElement) {
            const confirms = [...parent.querySelectorAll("button,a,div,span")].filter(
                (e) =>
                    visible(e) &&
                    e.textContent.trim() === "知道了" &&
                    ![...e.children].some((child) => child.textContent.trim() === "知道了"),
            );
            if (confirms.length) {
                if (confirms.length !== 1 || confirms[0].closest(dialogSelector) !== dialog) return null;
                // Some official notices have no dialog role. In that case only
                // accept a container holding this message, its button and an
                // optional notice title, never the entire application root.
                const otherText = parent.textContent
                    .replace(/\s+/g, "")
                    .replace(message.textContent.replace(/\s+/g, ""), "")
                    .replace("知道了", "");
                if (!dialog && !/^(提示|温馨提示|游戏结束|排队超时)?$/.test(otherText)) return null;
                return clickTarget(confirms[0]);
            }
            if (parent === dialog) break;
        }
        return null;
    };
    const endedMessage = findMessage("由于您长时间未操作，本次游戏已自动结束");
    const endedConfirmTarget = confirmationTarget(endedMessage);
    const queueTimeoutMessage = findMessage("排队超时，请保证排队过程网络畅通");
    const queueTimeoutConfirmTarget = confirmationTarget(queueTimeoutMessage);
    const ready = document.readyState === "complete";
    const videoReady =
        !ended &&
        !starting &&
        !!video &&
        video.readyState >= 2 &&
        !video.paused &&
        !video.ended &&
        video.videoWidth > 0 &&
        video.videoHeight > 0;
    const authenticated = ready && !loginPrompt && !hasLogin && (hasAccountMenu || hasLogout || videoReady);
    const queue = /正在排队|排队中|前方还有|预计等待/.test(text);
    const startupResourceFailed =
        ready &&
        !authenticated &&
        !loginPrompt &&
        !video &&
        !queue &&
        !starting &&
        (window.__maaCloudStartupErrors || []).some(
            (error) =>
                error.type === "resource" &&
                (error.source === "https://web.hycdn.cn/hg_web_sdk/lib/sdk.entry.js" ||
                    error.source === "https://web.hycdn.cn/static/js/umd/react/react@18.3.1.js" ||
                    error.source === "https://web.hycdn.cn/static/js/umd/react-dom/react-dom@18.3.1.js" ||
                    /^https:\/\/cloud-game-web-static\.hycdn\.cn\/cloud-game\/prod\/[\w.-]+\.js$/.test(error.source)),
        );
    const busyMessage = "您上次游戏的数据正在火速处理中，请稍后再试";
    const message = findMessage(busyMessage);
    const sessionBusy = !!message;
    const busyConfirmTarget = confirmationTarget(message);
    return {
        account: authenticated ? "signed_in" : ready && (loginPrompt || hasLogin) ? "signed_out" : "unknown",
        page: ended
            ? "ended"
            : startupResourceFailed
              ? "startup_error"
              : loginPrompt
                ? "login"
                : sessionBusy
                  ? "session_busy"
                  : queueTimeoutMessage
                    ? "queue_timeout"
                    : queue
                      ? "queue"
                      : starting
                        ? "starting"
                        : video
                          ? "stream"
                          : startButton
                            ? "landing"
                            : "loading",
        video_ready: !queue && !sessionBusy && !queueTimeoutMessage && videoReady,
        ended,
        startup_resource_failed: startupResourceFailed,
        video_width: video?.videoWidth || 0,
        video_height: video?.videoHeight || 0,
        game_surface: gameSurface?.tagName.toLowerCase() || "none",
        screen_width: Math.round(screen.width * devicePixelRatio),
        screen_height: Math.round(screen.height * devicePixelRatio),
        viewport_width: innerWidth,
        viewport_height: innerHeight,
        device_pixel_ratio: devicePixelRatio,
        start_available: !!startTarget,
        start_target: startTarget,
        session_busy_confirm_target: busyConfirmTarget,
        ended_confirm_target: endedConfirmTarget,
        queue_timeout_confirm_target: queueTimeoutConfirmTarget,
        document_ready: ready,
        document_epoch: performance.timeOrigin,
    };
})();
