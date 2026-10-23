#pragma once

namespace cloud
{

inline constexpr char kCaptureVideoPng[] = R"JS(
(() => {
    const video = [...document.querySelectorAll('video')].find(v =>
        v.isConnected && v.readyState >= 2 && !v.paused && !v.ended && v.videoWidth > 0);
    if (!video) return null;
    const canvas = document.createElement('canvas');
    canvas.width = 1280; canvas.height = 720;
    canvas.getContext('2d', {alpha: false}).drawImage(video, 0, 0, 1280, 720);
    return {data: canvas.toDataURL('image/png').split(',')[1],
        media_time: video.currentTime,
        decoded_frames: video.getVideoPlaybackQuality().totalVideoFrames};
})()
)JS";

// Installed lazily in the current document, so a normal navigation discards all
// stream state. Pixels always come from the site's existing decoded video.
inline constexpr char kCaptureVideoFrame[] = R"JS(
(() => {
    const video = [...document.querySelectorAll('video')].find(v =>
        v.isConnected && v.readyState >= 2 && !v.paused && !v.ended && v.videoWidth > 0);
    if (!video) return null;
    if (!Object.hasOwn(window, '__maaEndVideoFrame')) {
        let pixels = null;
        let receive;
        const ready = new Promise(resolve => { receive = resolve; });
        const canvas = document.createElement('canvas');
        canvas.width = 1280; canvas.height = 720;
        const context = canvas.getContext('2d', {alpha: false, willReadFrequently: true});
        chrome.webview.addEventListener('sharedbufferreceived', event => {
            if (event.additionalData?.purpose !== 'maaend-video-frame') return;
            pixels = new Uint8Array(event.getBuffer());
            receive();
        });
        Object.defineProperty(window, '__maaEndVideoFrame', {value: {
            get ready() { return pixels !== null; },
            capture: async () => {
                await ready;
                const v = [...document.querySelectorAll('video')].find(v =>
                    v.isConnected && v.readyState >= 2 && !v.paused && !v.ended && v.videoWidth > 0);
                if (!v) return null;
                context.drawImage(v, 0, 0, 1280, 720);
                pixels.set(context.getImageData(0, 0, 1280, 720).data);
                return {media_time: v.currentTime,
                    decoded_frames: v.getVideoPlaybackQuality().totalVideoFrames,
                    width: 1280, height: 720, stride: 5120, format: 'RGBA8'};
            }
        }});
    }
    return window.__maaEndVideoFrame.ready ? window.__maaEndVideoFrame.capture() : {needs_buffer: true};
})()
)JS";

} // namespace cloud
