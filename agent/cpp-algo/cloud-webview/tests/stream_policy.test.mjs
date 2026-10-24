import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(new URL("../input_policy.h", import.meta.url), "utf8");
const script = source.match(/LR"JS\(([\s\S]*?)\)JS";/)?.[1];
assert.ok(script);

for (const gpu of [
    false,
    true,
]) {
    test(`default enhancement cannot replace video with a canvas (GPU=${gpu})`, () => {
        class WebGL2RenderingContext {}
        const pixels = {};
        const webgl = {};
        const calls = [];
        class HTMLCanvasElement {
            getContext(type, options) {
                calls.push({target: this, type, options});
                if (type === "webgl2") return gpu ? new WebGL2RenderingContext() : null;
                return type === "2d" ? pixels : webgl;
            }
        }
        // The SDK checks WebGL2 for enhancement on both WebRTC and WebTransport.
        const enhanced = () => new HTMLCanvasElement().getContext("webgl2") instanceof WebGL2RenderingContext;
        assert.equal(enhanced(), gpu);
        const browser = {
            WebTransport: class {},
            VideoDecoder: class {},
            RTCPeerConnection: class {},
            addEventListener() {},
        };
        const capabilities = {...browser};
        vm.runInNewContext(script, {window: browser, HTMLCanvasElement, Element: class {}});
        assert.equal(enhanced(), false);
        assert.equal(browser.VideoDecoder, capabilities.VideoDecoder);
        assert.equal(browser.WebTransport, capabilities.WebTransport);
        assert.equal(browser.RTCPeerConnection, capabilities.RTCPeerConnection);
        const canvas = new HTMLCanvasElement();
        const options = {alpha: false, willReadFrequently: true};
        assert.equal(canvas.getContext("2d", options), pixels);
        assert.deepEqual(calls.at(-1), {target: canvas, type: "2d", options});
        assert.equal(canvas.getContext("webgl"), webgl);
    });
}
