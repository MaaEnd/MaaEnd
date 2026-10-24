(() => {
    const resourceName = (value) => {
        try {
            const u = new URL(value, location.href);
            return u.origin + u.pathname;
        } catch {
            return "";
        }
    };
    return {
        ready: document.readyState,
        online: navigator.onLine,
        react: typeof window.React,
        react_dom: typeof window.ReactDOM,
        webtransport: typeof window.WebTransport,
        video_decoder: typeof window.VideoDecoder,
        webgl2: !!document.createElement("canvas").getContext("webgl2"),
        media: [...document.querySelectorAll("video,canvas")].map((e) => ({
            tag: e.tagName.toLowerCase(),
            game: e.id === "WelinkGameVideo",
            connected: e.isConnected,
            width: e.videoWidth ?? e.width,
            height: e.videoHeight ?? e.height,
            ready: e.readyState,
            paused: e.paused,
            ended: e.ended,
            decoded_frames: e.getVideoPlaybackQuality?.().totalVideoFrames,
        })),
        scripts: [...document.scripts].filter((s) => s.src).map((s) => resourceName(s.src)),
        resources: performance.getEntriesByType("resource").map((r) => ({
            name: resourceName(r.name),
            type: r.initiatorType,
            duration: Math.round(r.duration),
            transfer: r.transferSize,
            status: r.responseStatus,
        })),
        errors: window.__maaCloudStartupErrors || [],
    };
})();
