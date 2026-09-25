import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";

import {ConnectionPanel} from "./static/js/ui/connection.js";
import {NavTestController} from "./static/js/ui/navtest.js";
import {RecordingController} from "./static/js/ui/recording.js";

class FakeButton {
  constructor() {
    this.disabled = false;
    this.textContent = "";
  }

  addEventListener() {}
}

class FakeConnection {
  constructor() {
    this.connected = false;
    this.listeners = [];
  }

  onStatusChange(listener) {
    this.listeners.push(listener);
    listener(this.connected);
  }

  setConnected(connected) {
    this.connected = connected;
    for (const listener of this.listeners) listener(connected);
  }
}

const fakeClassList = () => ({add() {}, remove() {}});

test("connection panel publishes readiness changes", () => {
  globalThis.document = {getElementById: () => null};
  const panel = new ConnectionPanel({});
  const observed = [];

  panel.onStatusChange((connected) => observed.push(connected));
  panel._setConnected(true);
  panel._setConnected(true);
  panel._setConnected(false);

  assert.deepEqual(observed, [false, true, false]);
  assert.equal(panel.isConnected(), false);
});

// 暂停探测后还留着绿灯，等于告诉开发者游戏连着，实际早断了。
test("suspending connection probes clears stale status-dot styles", () => {
  const removed = [];
  const dot = {classList: {remove: (...args) => removed.push(...args)}};
  globalThis.document = {getElementById: (id) => (id === "status-dot" ? dot : null)};
  const panel = new ConnectionPanel({});

  panel.setSuspended(true);

  assert.deepEqual(removed.sort(), ["connected", "connecting"]);
  assert.equal(panel.isConnected(), false);
});

test("controls that need a live game session start disabled", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  for (const id of ["btn-edit-locate", "btn-assert-locate", "btn-edit-start-locate", "btn-start", "btn-navtest-run"]) {
    const tag = html.match(new RegExp(`<button[^>]*id="${id}"[^>]*>`))?.[0] || "";
    assert.match(tag, /\bdisabled\b/);
  }
  assert.doesNotMatch(html, /id="(?:tab|panel|btn|tool)-astar/);
});

test("live location stays off until the user enables it", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const source = readFileSync(new URL("./static/js/main.js", import.meta.url), "utf8");
  const button = html.match(/<button[^>]*id="btn-live-locate"[^>]*>[\s\S]*?<\/button>/)?.[0] || "";

  assert.match(button, /class="[^"]*position-live-btn[^"]*"/);
  assert.doesNotMatch(button, /class="[^"]*btn-danger[^"]*"/);
  assert.match(button, /\bdisabled\b/);
  assert.match(button, /开启实时定位/);
  assert.doesNotMatch(source, /_liveLocateAutoStarted/);
  assert.equal(source.match(/this\._toggleLiveLocate\(\)/g)?.length, 1);
});

// 参考点属于所在工作区，排在那个工作区的编辑面板之前；底图层级是「看哪一层」，
// 编辑与断言各一枚按钮，都住在画布顶部的工具条视图控制里，带缩略图打开同一个选择器。
test("edit and assert keep symmetric locate buttons in their panel and tier pickers on the map bar", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const editMapStart = html.indexOf('id="panel-edit-map"');
  const assertMapStart = html.indexOf('id="panel-assert-map"');
  const propertiesStart = html.indexOf('id="panel-properties"');
  const viewControlsStart = html.indexOf('class="map-view-controls"');
  const rightPanelsStart = html.indexOf('class="canvas-right-panels"');

  assert.ok(editMapStart > 0 && assertMapStart > editMapStart && propertiesStart > assertMapStart);
  const editMap = html.slice(editMapStart, assertMapStart);
  const assertMap = html.slice(assertMapStart, propertiesStart);
  const viewControls = html.slice(viewControlsStart, rightPanelsStart);

  for (const group of [editMap, assertMap]) {
    assert.match(group, />\s*标记当前位置\s*<\/button>/);
    assert.doesNotMatch(group, /id="btn-select-tier"|id="btn-select-assert-tier"/);
  }
  assert.match(editMap, /id="btn-edit-locate"/);
  assert.match(assertMap, /id="btn-assert-locate"/);
  for (const [btn, thumb, label] of [
    ["btn-select-tier", "edit-tier-thumb", "edit-selected-tier-label"],
    ["btn-select-assert-tier", "assert-tier-thumb", "assert-selected-tier-label"],
  ]) {
    const tag = viewControls.match(new RegExp(`<button[^>]*id="${btn}"[^>]*>[\\s\\S]*?<\\/button>`))?.[0] || "";
    assert.match(tag, /class="[^"]*tier-btn[^"]*"/);
    assert.match(tag, new RegExp(`id="${thumb}" class="tier-thumb"`));
    assert.match(tag, new RegExp(`id="${label}"`));
  }
  // 图层与层级是工具条最右侧的两枚展开按钮，图层在前。
  const layersAt = viewControls.indexOf('id="btn-map-layers"');
  assert.ok(layersAt > 0 && layersAt < viewControls.indexOf('id="btn-select-tier"'));
});

// 规划起点是路点编辑的一部分：它跟路点列表在一起，不再要求先去工具栏选中某个工具。
test("manual planning start lives with the waypoints, not in the map toolbar", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const startBlockStart = html.indexOf('class="start-block"');
  const waypointHeaderStart = html.indexOf('class="waypoint-list-header"');
  const mapToolsStart = html.indexOf('class="map-tools"');
  const viewControlsStart = html.indexOf('class="map-view-controls"');

  assert.ok(startBlockStart > 0 && waypointHeaderStart > startBlockStart);
  const startBlock = html.slice(startBlockStart, waypointHeaderStart);
  const mapTools = html.slice(mapToolsStart, viewControlsStart);

  assert.doesNotMatch(mapTools, /id="tool-edit-start"/);
  for (const id of [
    "edit-plan-start-label",
    "btn-edit-start-clear",
    "tool-edit-start",
    "btn-edit-start-locate",
    "chk-auto-plan",
    "btn-edit-plan",
    "btn-edit-plan-clear",
  ]) {
    assert.match(startBlock, new RegExp(`id="${id}"`));
  }
  assert.match(startBlock, /id="chk-auto-plan" type="checkbox" checked/);
  assert.match(html, /id="edit-inspection-box"[^>]*hidden/);
});

// 画布上只留一条常驻控件条。工具、视图控制、图例散在四个角是这次重构要消灭的东西。
test("the canvas keeps a single control bar instead of one cluster per corner", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const barStart = html.indexOf('class="map-bar"');
  const mapToolsStart = html.indexOf('class="map-tools"');
  const viewControlsStart = html.indexOf('class="map-view-controls"');
  const layerPanelStart = html.indexOf('id="map-layer-panel"');
  const rightPanelsStart = html.indexOf('class="canvas-right-panels"');

  assert.ok(barStart > 0);
  assert.ok(barStart < mapToolsStart && mapToolsStart < viewControlsStart && viewControlsStart < layerPanelStart);
  assert.ok(layerPanelStart < rightPanelsStart);
  assert.doesNotMatch(html, /class="legend-dock"/);

  // 图例是常驻说明，收进图层面板等于把它藏了：它跟着地图停在右侧。
  const layerPanel = html.slice(layerPanelStart, rightPanelsStart);
  assert.doesNotMatch(layerPanel, /id="properties-legend"/);
  assert.match(html.slice(rightPanelsStart), /class="legend-panel">\s*<div id="properties-legend"/);
});

// 顶栏已经拆掉：工作区切换只在图标栏，一格对应一块侧栏面板。
test("workspaces live on the icon rail with no top bar left to hunt through", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const railStart = html.indexOf('<nav class="rail"');
  const railEnd = html.indexOf("</nav>");
  const sidebarStart = html.indexOf('id="sidebar"');

  assert.doesNotMatch(html, /<header/);
  assert.ok(railStart > 0 && railStart < railEnd && railEnd < sidebarStart);
  const rail = html.slice(railStart, railEnd);
  // 顺序就是使用顺序：画路线 → 画断言 → 读日志。
  const order = ["tab-edit", "tab-assert", "tab-log"].map((id) => rail.indexOf(`id="${id}"`));
  assert.ok(order.every((at, i) => at > 0 && (i === 0 || at > order[i - 1])));
  // 纯图标是上一版「不知道哪跟哪」的病根，每格都要带全名。
  for (const label of ["路径编辑", "断言", "日志分析"]) {
    assert.ok(rail.includes(label), `图标栏缺少「${label}」标签`);
  }
  // 缩写省不出多少宽度，只会让人猜这两个字母是什么。
  assert.doesNotMatch(rail, />MN</);
});

// 连接、录制、试跑都要求游戏活着，是同一个模块：单独一张卡钉在左列最顶上，任何工作区都先看到它。
test("connection, recording and the live run share one dock card at the top of the side column", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const columnStart = html.indexOf('class="side-column"');
  const liveStart = html.indexOf('id="panel-live"');
  const cardStart = html.indexOf('class="side-card"');
  const sidebarStart = html.indexOf('id="sidebar"');

  // 左列里先是实机卡，再是工作区卡；工作区面板全在实机卡之后。
  assert.ok(columnStart > 0 && columnStart < liveStart && liveStart < cardStart && cardStart < sidebarStart);
  for (const id of ["panel-edit-map", "panel-properties", "panel-recording", "panel-log"]) {
    assert.ok(html.indexOf(`id="${id}"`) > liveStart, `#${id} 应排在实机卡之后`);
  }
  const live = html.slice(liveStart, cardStart);
  for (const id of ["panel-connection", "btn-start", "btn-stop", "btn-navtest-run", "btn-navtest-stop"]) {
    assert.match(live, new RegExp(`id="${id}"`));
  }
  // 先答「连上了没有」再答「能不能跑」。
  assert.ok(live.indexOf('id="panel-connection"') < live.indexOf('id="panel-navtest"'));
});

// 编辑顺序是「在编辑哪条线」→ 参考点 → 起点 → 路点，导出留在底部。
test("route source leads the edit workspace ahead of the locate button and the start block", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const editMapStart = html.indexOf('id="panel-edit-map"');
  const sourceStart = html.indexOf('class="route-source"');
  const locateStart = html.indexOf('id="btn-edit-locate"');
  const startBlockStart = html.indexOf('class="start-block"');
  const recordingStart = html.indexOf('id="panel-recording"');

  assert.ok(editMapStart > 0 && editMapStart < sourceStart && sourceStart < locateStart);
  assert.ok(locateStart < startBlockStart && startBlockStart < recordingStart);
  const source = html.slice(sourceStart, locateStart);
  for (const id of ["btn-import", "btn-edit-read-clipboard", "btn-prev", "zone-label", "btn-next"]) {
    assert.match(source, new RegExp(`id="${id}"`));
  }
  assert.doesNotMatch(source, /id="btn-copy-path"/);
  assert.match(html.slice(recordingStart), /id="btn-copy-path"/);
});

// 按不动的键顶着主操作的蓝底, 就成了整屏最抢眼的东西。
test("disabled buttons give up their accent fill", () => {
  const css = readFileSync(new URL("./static/css/style.css", import.meta.url), "utf8");
  const at = css.indexOf(".btn:disabled");
  const rule = css.slice(at, css.indexOf("}", at));
  for (const prop of ["background", "border-color", "color"]) {
    assert.match(rule, new RegExp(`\\s${prop}:`), `.btn:disabled 缺少 ${prop}，配色会漏给各个变体`);
  }
  assert.doesNotMatch(rule, /var\(--accent/);
});

test("all 2D modes share one map-layer panel separate from route zipline planning", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");
  const mapToolsStart = html.indexOf('class="map-tools"');
  const viewControlsStart = html.indexOf('class="map-view-controls"');
  const mapTools = html.slice(mapToolsStart, viewControlsStart);
  const viewControls = html.slice(viewControlsStart, html.indexOf('id="map-layer-panel"'));
  const layerPanelStart = html.indexOf('id="map-layer-panel"');
  const layerPanelEnd = html.indexOf('id="context-panel"');
  const layerPanel = html.slice(layerPanelStart, layerPanelEnd);
  const logPanelStart = html.indexOf('id="panel-log"');
  const logSidebar = html.slice(logPanelStart, mapToolsStart);

  assert.match(viewControls, /id="btn-map-layers"[^>]*aria-controls="map-layer-panel"[^>]*aria-expanded="false"/);
  assert.match(mapTools, /id="btn-zipline-measure"[^>]*aria-pressed="false"[^>]*hidden/);
  assert.match(mapTools, /id="zipline-measure-divider"[^>]*hidden/);
  for (const id of ["map-show-basemap", "map-show-navmesh", "map-show-ziplines"]) {
    assert.match(layerPanel, new RegExp(`id="${id}"`));
  }
  for (const id of [
    "log-show-authored",
    "log-show-walk",
    "log-show-observed",
    "log-show-baseline",
    "log-show-zipline",
    "log-show-selected-towers",
    "log-show-estimates",
  ]) {
    assert.match(layerPanel, new RegExp(`id="${id}"`));
    assert.doesNotMatch(logSidebar, new RegExp(`id="${id}"`));
  }
  assert.doesNotMatch(html, /id="btn-toggle-ziplines"|id="log-show-recorded-towers"/);
  assert.doesNotMatch(viewControls, /id="chk-edit-zipline"/);
  assert.doesNotMatch(mapTools, /id="chk-edit-zipline"/);
  assert.match(html, /id="chk-edit-zipline" type="checkbox" \/> 滑索规划/);
});

test("edit objects and shared point details expose matching cancel-selection controls", () => {
  const html = readFileSync(new URL("./static/index.html", import.meta.url), "utf8");

  assert.match(html, /id="btn-edit-selection-clear"[^>]*>取消选择<\/button>/);
  assert.match(html, /id="btn-point-clear"[^>]*>取消选择<\/button>/);
  assert.match(html, /id="point-inspection-box"[^>]*hidden/);
  assert.match(html, /id="zipline-distance-box"[^>]*hidden/);
});

test("recording start follows the probed connection state", () => {
  const connection = new FakeConnection();
  const btnStart = new FakeButton();
  const btnStop = new FakeButton();
  new RecordingController({
    btnStart,
    btnStop,
    appEl: null,
    connection,
  });

  assert.equal(btnStart.disabled, true);
  assert.equal(btnStop.disabled, true);

  connection.setConnected(true);
  assert.equal(btnStart.disabled, false);

  connection.setConnected(false);
  assert.equal(btnStart.disabled, true);
});

test("first navtest run follows the probe while a live session keeps its own state", () => {
  const connection = new FakeConnection();
  const btnRun = new FakeButton();
  const btnStop = new FakeButton();
  const armedLabel = {textContent: ""};
  const hotkeyNote = {innerHTML: "hotkeys", textContent: "", classList: fakeClassList()};
  const controller = new NavTestController({
    btnRun,
    btnStop,
    armedLabel,
    overlay: {hidden: true},
    hotkeyNote,
    connection,
    getRoute: () => ({
      path: [[1, 2]],
      exported: false,
      assert_target: null,
    }),
  });

  assert.equal(btnRun.disabled, true);
  assert.match(armedLabel.textContent, /未连接游戏/);

  connection.setConnected(true);
  assert.equal(btnRun.disabled, false);

  controller.socket = {};
  controller.connected = true;
  connection.setConnected(false);
  assert.equal(btnRun.disabled, false);
});
