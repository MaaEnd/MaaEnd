import assert from "node:assert/strict";
import {test} from "node:test";

import {parseJsonc} from "../jsonc.mjs";
import {buildPipeline, extractClicks, nodeName, successName, updatePipeline} from "./generate.mjs";

test("只提取点击点，拒绝非 720p、异常状态和越界坐标", () => {
    const source = {
        _meta: {
            screen_size: [
                1280,
                720,
            ],
        },
        region01: {
            map01_lv001: {
                status: "ok",
                center_px: [
                    455,
                    478,
                ],
                area: {polygon: []},
            },
        },
    };
    assert.deepEqual(extractClicks(source), {
        region01: {
            map01_lv001: [
                455,
                478,
            ],
        },
    });
    assert.throws(
        () =>
            extractClicks({
                ...source,
                _meta: {
                    screen_size: [
                        1920,
                        1080,
                    ],
                },
            }),
        /1280/,
    );
    source.region01.map01_lv001.status = "failed";
    assert.throws(() => extractClicks(source), /状态异常/);
    source.region01.map01_lv001.status = "ok";
    source.region01.map01_lv001.center_px = [
        1280,
        478,
    ];
    assert.throws(() => extractClicks(source), /坐标非法/);
});

test("按地区和地图 key 生成点击节点和 Success 引用，并从名称表生成 desc", () => {
    const clicks = {
        region01: {
            map01_lv001: [
                455,
                478,
            ],
            map01_lv002: [
                295,
                261,
            ],
        },
    };
    const nodes = buildPipeline(clicks, {
        map01_lv001: {names: {zh_cn: "枢纽区"}},
    });
    assert.equal(nodeName("region01", "map01_lv001"), "__SceneMapOverviewClick_region01_map01_lv001");
    assert.deepEqual(nodes[nodeName("region01", "map01_lv001")].next, [
        "__SceneMapOverviewSuccess_region01_map01_lv001",
    ]);
    assert.equal(nodes[nodeName("region01", "map01_lv001")].desc, "在地图总览对应区域中，点击进入枢纽区地图");
    assert.equal(nodes[nodeName("region01", "map01_lv002")].desc, "在地图总览对应区域中，点击进入map01_lv002地图");
    assert.deepEqual(nodes[nodeName("region01", "map01_lv002")].next, [successName("region01", "map01_lv002")]);
});

test("新地区无需登记，在线优先使用远端地图名，名称缺失时回退名称表或地图 key", () => {
    const clicks = {
        region03: {
            map03_lv001: [
                1,
                2,
            ],
            map03_lv002: [
                3,
                4,
            ],
            map03_lv003: [
                5,
                6,
            ],
        },
    };
    const nodes = buildPipeline(
        clicks,
        {map03_lv001: {names: {zh_cn: "旧名称"}}, map03_lv002: {names: {zh_cn: "同步名称"}}},
        {region03: {map03_lv001: {name: "远端新名称"}, map03_lv002: {name: " "}, map03_lv003: {name: 123}}},
    );
    assert.equal(nodes[nodeName("region03", "map03_lv001")].desc, "在地图总览对应区域中，点击进入远端新名称地图");
    assert.equal(nodes[nodeName("region03", "map03_lv002")].desc, "在地图总览对应区域中，点击进入同步名称地图");
    assert.equal(nodes[nodeName("region03", "map03_lv003")].desc, "在地图总览对应区域中，点击进入map03_lv003地图");
    assert.deepEqual(nodes[nodeName("region03", "map03_lv001")].next, [successName("region03", "map03_lv001")]);
});

test("只补缺失占位到文件末尾，保留原 Success、其他手写节点和注释，并清理过期点击", () => {
    const clicks = buildPipeline(
        {
            region01: {
                map01_lv001: [
                    1,
                    2,
                ],
                map01_lv002: [
                    3,
                    4,
                ],
                map01_lv003: [
                    5,
                    6,
                ],
            },
        },
        {},
    );
    const external = successName("region01", "map01_lv001");
    const local = successName("region01", "map01_lv002");
    const missing = successName("region01", "map01_lv003");
    const stale = nodeName("region01", "removed_map");
    const manual = {recognition: "OCR", expected: ["手写成功确认"]};
    const original = JSON.stringify({[stale]: {}, [local]: manual, OtherManualNode: {enabled: false}}, null, 4).replace(
        `"${local}"`,
        `// 保留成功节点注释\n    "${local}"`,
    );
    const updated = updatePipeline(original, clicks, new Set([external]));
    const nodes = parseJsonc(updated);
    assert.equal(stale in nodes, false);
    assert.equal(external in nodes, false);
    assert.deepEqual(nodes[local], manual);
    assert.deepEqual(nodes.OtherManualNode, {enabled: false});
    assert.ok(updated.includes("// 保留成功节点注释"));
    assert.equal(nodes[missing].enabled, false);
    assert.equal(Object.keys(nodes).at(-1), missing);
    assert.deepEqual(Object.keys(nodes).slice(0, 3), Object.keys(clicks));
    assert.equal(updatePipeline(updated, clicks, new Set([external])), updated);
});

test("过期点击位于末尾时也保留 Success 的内容与注释", () => {
    const manual = successName("region01", "map01_lv001");
    const stale = nodeName("region01", "removed_map");
    const updated = updatePipeline(
        `{\n// 手写确认\n"${manual}": {"enabled": false},\n"${stale}": {}\n}`,
        {},
        new Set(),
    );
    assert.deepEqual(parseJsonc(updated), {[manual]: {enabled: false}});
    assert.ok(updated.includes("// 手写确认"));
});
