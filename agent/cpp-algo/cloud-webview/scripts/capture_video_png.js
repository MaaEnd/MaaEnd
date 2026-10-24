(() => {
    const video = [...document.querySelectorAll("video")].find(
        (v) => v.isConnected && v.readyState >= 2 && !v.paused && !v.ended && v.videoWidth > 0,
    );
    if (!video) return null;
    const canvas = document.createElement("canvas");
    canvas.width = 1280;
    canvas.height = 720;
    canvas.getContext("2d", {alpha: false}).drawImage(video, 0, 0, 1280, 720);
    return {
        data: canvas.toDataURL("image/png").split(",")[1],
        media_time: video.currentTime,
        decoded_frames: video.getVideoPlaybackQuality().totalVideoFrames,
    };
})();
