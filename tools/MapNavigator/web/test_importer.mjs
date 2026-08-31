import assert from "node:assert/strict";
import test from "node:test";

import {filterProjectNodes, normalizeAssertTarget, readClipboardText} from "./static/js/ui/importer.js";

const routes = [
  {
    resource_path: "assets/resource/pipeline/AutoCollect/AutoCollectRoute7.json",
    node_name: "AutoCollectRoute7GotoFindLine4",
    desc: "自动采集净化玉参考路线",
    kind: "path",
    zone_ids: ["Wuling_Base"],
  },
  {
    resource_path: "assets/resource/pipeline/EnvironmentMonitoring/Wuling.json",
    node_name: "EnvironmentMonitoringGotoTarget",
    desc: "环境监测观察点导航",
    kind: "path",
    zone_ids: ["Wuling_Base"],
  },
  {
    resource_path: "assets/resource/pipeline/AutoCollect/AutoCollectRoute7.json",
    node_name: "AutoCollectRoute7AssertLocation",
    desc: "武陵断言位置",
    kind: "assert",
    zone_id: "Wuling_Base",
  },
];

test("project route filter matches resource paths case-insensitively", () => {
  assert.deepEqual(filterProjectNodes(routes, "AUTOCOLLECT", "path"), [routes[0]]);
});

test("project route filter matches Pipeline node names", () => {
  assert.deepEqual(filterProjectNodes(routes, "gototarget", "path"), [routes[1]]);
});

test("project route filter matches node descriptions", () => {
  assert.deepEqual(filterProjectNodes(routes, "环境监测观察点", "path"), [routes[1]]);
});

test("blank project route filter keeps the backend order", () => {
  assert.deepEqual(filterProjectNodes(routes, "  ", "path"), routes.slice(0, 2));
});

test("project node filter separates assertions and searches by zone", () => {
  assert.deepEqual(filterProjectNodes(routes, "wuling_base", "assert"), [routes[2]]);
});

test("clipboard reader returns the current JSON text unchanged", async () => {
  const text = '{"path":[[1,2]]}';
  assert.equal(await readClipboardText({readText: async () => text}), text);
});

test("clipboard reader rejects empty content", async () => {
  await assert.rejects(readClipboardText({readText: async () => " \r\n "}), /没有可导入的 JSON 内容/);
});

test("clipboard reader reports an unavailable Clipboard API", async () => {
  await assert.rejects(readClipboardText(undefined), /不支持读取剪贴板/);
});

test("clipboard reader preserves read failures for the UI to explain", async () => {
  const denied = Object.assign(new Error("permission denied"), {name: "NotAllowedError"});
  await assert.rejects(readClipboardText({readText: async () => Promise.reject(denied)}), denied);
});

test("assert import normalizes four finite numeric target values", () => {
  assert.deepEqual(normalizeAssertTarget(["1", 2, "30.5", 40]), [1, 2, 30.5, 40]);
  assert.deepEqual(normalizeAssertTarget([1, 2, 30, 40, 50]), [1, 2, 30, 40]);
});

test("assert import rejects incomplete, non-finite, and empty rectangles", () => {
  assert.equal(normalizeAssertTarget([1, 2, 30]), null);
  assert.equal(normalizeAssertTarget([1, 2, "invalid", 40]), null);
  assert.equal(normalizeAssertTarget([1, 2, 30, Number.POSITIVE_INFINITY]), null);
  assert.equal(normalizeAssertTarget([null, 2, 30, 40]), null);
  assert.equal(normalizeAssertTarget([1, 2, 0, 40]), null);
});
