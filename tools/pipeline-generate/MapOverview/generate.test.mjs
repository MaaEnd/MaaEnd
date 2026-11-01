import assert from "node:assert/strict";
import {test} from "node:test";

import {parseJsonc} from "../jsonc.mjs";
import {buildPipeline, collectReferences, extractClicks, nodeName, successName, updatePipeline} from "./generate.mjs";

function sourceFixture() {
    return {
        version: "1.6.4+11262943-8",
        sha256: "fcf2e154faccd6343de2278f0c253c61e7414931bfc31ae525c73f43b1c59250",
        region01: {
            name: "四号谷地",
            area: {
                map01_lv001: {
                    polygon: [
                        [
                            0,
                            0,
                        ],
                        [
                            1,
                            1,
                        ],
                    ],
                    click: [
                        455,
                        478,
                    ],
                },
                map01_lv002: {
                    click: [
                        295,
                        261,
                    ],
                },
            },
        },
    };
}

const levels = {map01_lv001: {names: {zh_cn: "枢纽区"}}, map01_lv002: {names: {zh_cn: "谷地通道"}}};

test("按地区 name/area 和地图 click 提取数据，忽略 version、sha256 和 polygon", () => {
    const source = sourceFixture();
    assert.deepEqual(extractClicks(source), {
        region01: {
            name: "四号谷地",
            area: {
                map01_lv001: {
                    click: [
                        455,
                        478,
                    ],
                },
                map01_lv002: {
                    click: [
                        295,
                        261,
                    ],
                },
            },
        },
    });
});
test("拒绝非法点击点和缺失地区结构", () => {
    const source = sourceFixture();
    for (const invalid of [
        null,
        [],
        {},
        {version: "1.6.4", sha256: "hash"},
        {invalid: source.region01},
    ]) {
        assert.throws(() => extractClicks(invalid), /地区/);
    }
    for (const point of [
        undefined,
        [
            1280,
            478,
        ],
        [
            -1,
            0,
        ],
        [
            0,
            720,
        ],
        [
            1.5,
            2,
        ],
        [1],
        "455,478",
    ]) {
        source.region01.area.map01_lv001.click = point;
        assert.throws(() => extractClicks(source), /坐标非法/);
    }
    for (const invalid of [
        {name: "四号谷地"},
        {name: "四号谷地", area: []},
        {name: "四号谷地", area: {}},
    ]) {
        assert.throws(() => extractClicks({region01: invalid}), /area|没有地图/);
    }
    source.region01 = {
        name: " ",
        area: {
            map01_lv001: {
                click: [
                    1,
                    2,
                ],
            },
        },
    };
    assert.throws(() => extractClicks(source), /缺少地区名称/);
    assert.throws(
        () =>
            buildPipeline(
                {
                    region01: {name: "四号谷地", area: {}},
                },
                levels,
            ),
        /没有地图/,
    );
});

test("点击和占位描述包含地区及地图名，按地区和地图 key 命名并引用 Success", () => {
    const generated = buildPipeline(sourceFixture(), levels);
    const name = nodeName("region01", "map01_lv001");
    const next = successName("region01", "map01_lv001");
    assert.equal(name, "__ScenePrivateMapOverview_region01_EnterMap_map01_lv001");
    assert.equal(next, "__ScenePrivateMapAnyEnterMap_region01_map01_lv001_Success");
    assert.deepEqual(generated.clicks[name], {
        desc: "在四号谷地总览界面，点击进入枢纽区地图",
        recognition: "DirectHit",
        action: {
            type: "Click",
            param: {
                target: [
                    455,
                    478,
                ],
            },
        },
        next: [next],
    });
    assert.deepEqual(generated.successes[next], {
        desc: "在地图界面，成功进入四号谷地-枢纽区地图；待实现地图成功确认，实现后启用",
        enabled: false,
    });
});

test("新地区无需登记，地图名使用名称表或 key", () => {
    const source = {
        region03: {
            name: "新地区",
            area: {
                map03_lv001: {
                    click: [
                        1,
                        2,
                    ],
                },
                map03_lv002: {
                    click: [
                        3,
                        4,
                    ],
                },
                map03_lv003: {
                    click: [
                        5,
                        6,
                    ],
                },
            },
        },
    };
    const cached = JSON.parse(JSON.stringify(extractClicks(source)));
    const generated = buildPipeline(cached, {
        map03_lv001: {names: {zh_cn: "名称表名称"}},
        map03_lv002: {names: {zh_cn: "同步名称"}},
    });
    assert.equal(
        generated.clicks[nodeName("region03", "map03_lv001")].desc,
        "在新地区总览界面，点击进入名称表名称地图",
    );
    assert.equal(generated.clicks[nodeName("region03", "map03_lv002")].desc, "在新地区总览界面，点击进入同步名称地图");
    assert.equal(
        generated.clicks[nodeName("region03", "map03_lv003")].desc,
        "在新地区总览界面，点击进入map03_lv003地图",
    );
});

test("只补缺失占位到文件末尾，保留手写 Success 和注释，并清理过期点击", () => {
    const source = sourceFixture();
    source.region01.area.map01_lv003 = {
        click: [
            5,
            6,
        ],
    };
    const generated = buildPipeline(extractClicks(source), levels);
    const external = successName("region01", "map01_lv001");
    const local = successName("region01", "map01_lv002");
    const missing = successName("region01", "map01_lv003");
    const stale = nodeName("region01", "removed_map");
    const manual = {recognition: "OCR", expected: ["手写成功确认"]};
    const original = JSON.stringify({[stale]: {}, [local]: manual, OtherManualNode: {enabled: false}}, null, 4).replace(
        `"${local}"`,
        `// 保留成功节点注释\n    "${local}"`,
    );
    const updated = updatePipeline(original, generated, new Set([external]));
    const nodes = parseJsonc(updated);
    assert.equal(stale in nodes, false);
    assert.equal(external in nodes, false);
    assert.deepEqual(nodes[local], manual);
    assert.deepEqual(nodes.OtherManualNode, {enabled: false});
    assert.ok(updated.includes("// 保留成功节点注释"));
    assert.deepEqual(nodes[missing], generated.successes[missing]);
    assert.equal(Object.keys(nodes).at(-1), missing);
    assert.deepEqual(Object.keys(nodes).slice(0, 3), Object.keys(generated.clicks));
    assert.equal(updatePipeline(updated, generated, new Set([external])), updated);
});

test("已有 Success 的占位描述、识别逻辑、启用状态和自定义内容均保留", () => {
    const generated = buildPipeline(extractClicks(sourceFixture()), levels);
    const next = successName("region01", "map01_lv001");
    const placeholder = {...generated.successes[next], desc: "作者维护的目标描述；待实现地图成功确认，实现后启用"};
    const original = `{// 保留占位注释\n"${next}": ${JSON.stringify(placeholder)}}`;
    const updated = updatePipeline(original, generated, new Set());
    assert.deepEqual(parseJsonc(updated)[next], placeholder);
    assert.ok(updated.includes("// 保留占位注释"));
    assert.equal(updatePipeline(updated, generated, new Set()), updated);
    for (const manual of [
        {...placeholder, recognition: {type: "OCR", param: {expected: ["枢纽区"]}}},
        {...placeholder, enabled: true},
        {...placeholder, desc: "作者维护的禁用节点"},
    ]) {
        const result = updatePipeline(JSON.stringify({[next]: manual}), generated, new Set());
        assert.deepEqual(parseJsonc(result)[next], manual);
    }
});

test("过期点击位于末尾时也保留 Success 的内容与注释", () => {
    const manual = successName("region01", "map01_lv001");
    const stale = nodeName("region01", "removed_map");
    const updated = updatePipeline(
        `{\n// 手写确认\n"${manual}": {"enabled": false},\n"${stale}": {}\n}`,
        {clicks: {}, successes: {}},
        new Set(),
    );
    assert.deepEqual(parseJsonc(updated), {[manual]: {enabled: false}});
    assert.ok(updated.includes("// 手写确认"));
});

test("收集 next 和 on_error 静态引用，识别 JumpBack，忽略描述与运行时 Anchor", () => {
    const target = nodeName("region01", "map01_lv001");
    assert.deepEqual(
        collectReferences(
            {
                Entry: {
                    desc: target,
                    next: [
                        target,
                        `[JumpBack]${target}`,
                        `[Anchor]${target}`,
                    ],
                    on_error: target,
                },
            },
            "SceneValleyIV.json",
        ),
        [
            {target, source: "SceneValleyIV.json: Entry.next[0]"},
            {target, source: "SceneValleyIV.json: Entry.next[1]"},
            {target, source: "SceneValleyIV.json: Entry.on_error[0]"},
        ],
    );
});

test("地区只遗漏一张地图时，仍有外部流程引用就拒绝删除并报告引用位置", () => {
    const complete = buildPipeline(extractClicks(sourceFixture()), levels);
    const partialSource = sourceFixture();
    delete partialSource.region01.area.map01_lv001;
    const partial = buildPipeline(extractClicks(partialSource), levels);
    const target = nodeName("region01", "map01_lv001");
    const original = JSON.stringify({...complete.clicks, ...complete.successes});
    for (const field of [
        "next",
        "on_error",
    ]) {
        const references = collectReferences({TheHubEntry: {[field]: [`[JumpBack]${target}`]}}, "SceneValleyIV.json");
        assert.throws(
            () => updatePipeline(original, partial, new Set(), references),
            (error) => {
                assert.match(error.message, /拒绝删除仍被流程引用/);
                assert.ok(error.message.includes(target));
                assert.ok(error.message.includes(`SceneValleyIV.json: TheHubEntry.${field}[0]`));
                return true;
            },
        );
    }
});

test("本文件手写流程仍有引用时拒绝删除，解除引用后正常删除并保留 Success", () => {
    const complete = buildPipeline(extractClicks(sourceFixture()), levels);
    const partialSource = sourceFixture();
    delete partialSource.region01.area.map01_lv001;
    const partial = buildPipeline(extractClicks(partialSource), levels);
    const target = nodeName("region01", "map01_lv001");
    const nodes = {...complete.clicks, ...complete.successes, LocalEntry: {next: [target]}};
    assert.throws(
        () => updatePipeline(JSON.stringify(nodes), partial, new Set()),
        /SceneMapOverview\.json: LocalEntry\.next\[0\]/,
    );
    nodes.LocalEntry.next = [nodeName("region01", "map01_lv002")];
    const updated = parseJsonc(updatePipeline(JSON.stringify(nodes), partial, new Set()));
    assert.equal(target in updated, false);
    assert.deepEqual(updated.LocalEntry, nodes.LocalEntry);
    assert.deepEqual(
        updated[successName("region01", "map01_lv001")],
        complete.successes[successName("region01", "map01_lv001")],
    );
});
