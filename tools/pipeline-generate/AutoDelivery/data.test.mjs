import assert from "node:assert/strict";
import {existsSync, readdirSync, readFileSync} from "node:fs";
import test from "node:test";

import {parseJsonc, readJsonc} from "../jsonc.mjs";
import routeRows, {buildRows} from "./routes-data.mjs";
import {buildNavmeshPath, buildYawApproachTarget, depots, destinations, runtimeCatalog} from "./model.mjs";
import {
    ambiguousRecycleBinGroups,
    ambiguousRecycleBins,
    groupAmbiguousRecycleBins,
    recycleBins,
    recycleBinAreaRows,
    recycleBinCandidateRows,
    recycleBinResolveNodes,
} from "./recycle-bins-data.mjs";
import {buildSyncedRouteConfig} from "./sync-routes.mjs";

const catalogSource = JSON.parse(readFileSync(new URL("../data/delivery_destinations.json", import.meta.url), "utf8"));
const routeSource = JSON.parse(readFileSync(new URL("./routes.json", import.meta.url), "utf8"));

test("AutoDelivery 按 yaw 正方向生成 8 米接近点", () => {
    const map = {sx: 0.75, sy: 0.75};
    const source = {u: 100, v: 200};
    const expectedByYaw = new Map([
        [
            0,
            [
                100,
                194,
            ],
        ],
        [
            90,
            [
                106,
                200,
            ],
        ],
        [
            180,
            [
                100,
                206,
            ],
        ],
        [
            270,
            [
                94,
                200,
            ],
        ],
    ]);

    for (const [
        yaw,
        expected,
    ] of expectedByYaw) {
        const target = buildYawApproachTarget({...source, yaw}, map, `测试朝向 ${yaw}`);
        assert.deepEqual(target, expected);
        assert.ok(Math.abs(Math.hypot((target[0] - source.u) / map.sx, (target[1] - source.v) / map.sy) - 8) < 1e-9);
    }
});

test("AutoDelivery 仓储和资源回收站主路线从正面接近且所有目标自动生成重试路线", () => {
    const depotOverrides = new Map(
        routeSource.depots.map((item) => [
            item.source_id,
            item,
        ]),
    );
    const destinationOverrides = new Map(
        routeSource.destinations.map((item) => [
            item.source_id,
            item,
        ]),
    );
    const sourceByDepotId = new Map(
        catalogSource.depots.map((item) => [
            item.id,
            item,
        ]),
    );
    const sourceByDestinationId = new Map(
        catalogSource.destinations.map((item) => [
            item.id,
            item,
        ]),
    );

    for (const depot of depots) {
        const source = sourceByDepotId.get(depot.id);
        const defaultPath = buildNavmeshPath(source, `仓储 ${depot.id}`, true);
        const override = depotOverrides.get(depot.id);
        if (override?.path?.length) {
            assert.deepEqual(depot.path, override.path);
        } else {
            assert.deepEqual(depot.path, defaultPath);
        }

        const expectedRetryPath = override?.retry_path?.length ? override.retry_path : defaultPath;
        assert.deepEqual(depot.retryPath, expectedRetryPath);
        assert.match(depot.retryRouteNode, /^AutoDeliveryRouteDepotRetry/);
        assert.equal(defaultPath.length, 2);
        assert.equal(defaultPath[0].required, true);
        assert.deepEqual(defaultPath[1].target, [
            source.u,
            source.v,
        ]);
        const map = catalogSource.maps[source.map];
        assert.ok(
            Math.abs(
                Math.hypot(
                    (defaultPath[0].target[0] - source.u) / map.sx,
                    (defaultPath[0].target[1] - source.v) / map.sy,
                ) - 8,
            ) < 0.002,
        );
    }

    for (const destination of destinations) {
        const source = sourceByDestinationId.get(destination.id);
        const override = destinationOverrides.get(destination.id);
        const depot = depots.find((item) => item.id === destination.depotId);
        const ownPath = destination.path.slice(depot.departurePath.length);
        const withApproachPoint = source.kind === "recycle_bin";
        const defaultPath = buildNavmeshPath(source, `终点 ${destination.id}`, withApproachPoint);
        if (override?.path?.length) {
            assert.deepEqual(ownPath, override.path);
        } else {
            assert.deepEqual(ownPath, defaultPath);
        }

        const defaultRetryPath = buildNavmeshPath(source, `终点重试 ${destination.id}`, true);
        const expectedRetryPath = override?.retry_path?.length ? override.retry_path : defaultRetryPath;
        assert.deepEqual(destination.retryPath, expectedRetryPath);
        assert.match(destination.retryRouteNode, /^AutoDeliveryRouteDestinationRetry/);

        if (!withApproachPoint) {
            assert.equal(defaultPath.length, 1);
        } else {
            assert.equal(defaultPath.length, 2);
            assert.equal(defaultPath[0].required, true);
            const map = catalogSource.maps[source.map];
            assert.ok(
                Math.abs(
                    Math.hypot(
                        (defaultPath[0].target[0] - source.u) / map.sx,
                        (defaultPath[0].target[1] - source.v) / map.sy,
                    ) - 8,
                ) < 0.002,
            );
        }
        assert.deepEqual(defaultPath.at(-1).target, [
            source.u,
            source.v,
        ]);
    }
});

test("AutoDelivery 接近点拒绝无效朝向和地图比例", () => {
    const source = {u: 100, v: 200, yaw: 0};
    assert.throws(() => buildYawApproachTarget({...source, yaw: Number.NaN}, {sx: 1, sy: 1}, "无效朝向"), /yaw/);
    assert.throws(() => buildYawApproachTarget(source, {sx: 0, sy: 1}, "无效比例"), /sx\/sy/);
});

test("AutoDelivery 路线同步刷新元数据并保留人工覆盖字段", () => {
    const synced = buildSyncedRouteConfig(
        {
            depots: [
                {
                    id: "depot-1",
                    name: {zh_cn: "仓储一"},
                },
                {
                    id: "depot-2",
                    name: {zh_cn: "仓储二"},
                },
            ],
            destinations: [
                {
                    id: "destination-1",
                    name: {zh_cn: "终点一"},
                    depot_id: "depot-1",
                },
            ],
        },
        {
            depots: [
                {
                    source_id: "depot-1",
                    name: "旧仓储名",
                    description: "人工说明",
                    walk_only: true,
                    retry_path: [
                        [
                            1,
                            2,
                        ],
                    ],
                },
            ],
            destinations: [
                {
                    source_id: "destination-1",
                    name: "旧终点名",
                    depot_id: "old-depot",
                    walk_only: true,
                    retry_path: [
                        [
                            3,
                            4,
                        ],
                    ],
                },
            ],
        },
    );

    assert.deepEqual(synced.depots, [
        {
            source_id: "depot-1",
            name: "仓储一",
            description: "人工说明",
            walk_only: true,
            retry_path: [
                [
                    1,
                    2,
                ],
            ],
        },
        {
            source_id: "depot-2",
            name: "仓储二",
        },
    ]);
    assert.deepEqual(synced.destinations, [
        {
            source_id: "destination-1",
            name: "终点一",
            depot_id: "depot-1",
            walk_only: true,
            retry_path: [
                [
                    3,
                    4,
                ],
            ],
        },
    ]);
});

test("AutoDelivery walk_only 路线在全局启用滑索时仍仅步行", () => {
    const rows = buildRows(
        "TestArea",
        "destination-1",
        "测试路线",
        [
            [
                1,
                2,
            ],
        ],
        "AutoDeliveryRouteDestinationTest",
        "AutoDeliveryRouteDestinationTestWithZipline",
        true,
    );

    assert.equal(rows.length, 2);
    assert.equal(rows[0].ActionParam.value.zip, false);
    assert.equal(rows[1].ActionParam.value.zip, false);
    assert.match(rows[1].Description, /仅允许步行/);

    const defaultRows = buildRows(
        "TestArea",
        "destination-2",
        "默认路线",
        [
            [
                3,
                4,
            ],
        ],
        "AutoDeliveryRouteDestinationDefault",
        "AutoDeliveryRouteDestinationDefaultWithZipline",
    );
    assert.equal(defaultRows[1].ActionParam.value.zip, true);
    assert.match(defaultRows[1].Description, /允许使用滑索/);
});

test("AutoDelivery routes.json 同步完整检索元数据并仅为必要项保留路线覆盖", () => {
    assert.deepEqual(
        routeSource.depots.map((item) => item.source_id),
        catalogSource.depots.map((item) => item.id).sort(),
    );
    assert.deepEqual(
        routeSource.destinations.map((item) => item.source_id),
        catalogSource.destinations.map((item) => item.id).sort(),
    );

    const depotSourceById = new Map(
        catalogSource.depots.map((item) => [
            item.id,
            item,
        ]),
    );
    for (const route of routeSource.depots) {
        assert.equal(route.name, depotSourceById.get(route.source_id).name.zh_cn);
    }
    const destinationSourceById = new Map(
        catalogSource.destinations.map((item) => [
            item.id,
            item,
        ]),
    );
    for (const route of routeSource.destinations) {
        const source = destinationSourceById.get(route.source_id);
        assert.equal(route.name, source.name.zh_cn);
        assert.equal(route.depot_id, source.depot_id);
    }

    assert.ok(routeSource.depots.some((item) => !item.path && !item.retry_path && !item.departure_path));
    assert.ok(routeSource.destinations.some((item) => !item.path));
});

test("AutoDelivery 路线为每个仓储和终点生成可独立执行的普通/滑索节点", () => {
    const retryCount = [
        ...depots,
        ...destinations,
    ].filter((item) => item.retryRouteNode).length;
    assert.equal(routeRows.length, depots.length * 2 + destinations.length * 2 + retryCount);
    assert.equal(new Set(routeRows.map((item) => item.Node)).size, routeRows.length);
    for (const row of routeRows) {
        assert.match(row.Node, /^AutoDeliveryRoute/);
        assert.ok(row.ActionParam.value.path.length > 0);
        assert.match(row.Description, /仓储节点|终点站位修正路线/);
    }
});

test("AutoDelivery 运行时目录只保留匹配文本和生成节点名", () => {
    assert.equal(runtimeCatalog.depots.length, depots.length);
    assert.equal(runtimeCatalog.destinations.length, destinations.length);
    const depotSourceById = new Map(
        catalogSource.depots.map((item) => [
            item.id,
            item,
        ]),
    );
    for (const depot of runtimeCatalog.depots) {
        assert.deepEqual(depot.name, depotSourceById.get(depot.id).name);
    }
    assert.equal(JSON.stringify(runtimeCatalog).includes('"path"'), false);
    assert.equal(JSON.stringify(runtimeCatalog).includes('"u"'), false);
    assert.equal(JSON.stringify(runtimeCatalog).includes('"v"'), false);
});

test("AutoDelivery 任务目标地图识别由公共节点复用", () => {
    const commonPipeline = parseJsonc(
        readFileSync(new URL("../../../assets/resource/pipeline/AutoDelivery/Common.json", import.meta.url), "utf8"),
    );
    const pickupPipeline = parseJsonc(
        readFileSync(new URL("../../../assets/resource/pipeline/AutoDelivery/Pickup.json", import.meta.url), "utf8"),
    );
    const areaPipeline = parseJsonc(
        readFileSync(
            new URL("../../../assets/resource/pipeline/AutoDelivery/RecycleBinAreas.json", import.meta.url),
            "utf8",
        ),
    );
    const recognitionNode = "AutoDeliveryInTaskDestinationMap";

    assert.deepEqual(commonPipeline[recognitionNode], {
        desc: "识别已打开的送货任务目标地图",
        recognition: "TemplateMatch",
        roi: [
            860,
            10,
            170,
            140,
        ],
        template: "AutoDelivery/TrackTaskSuccess.png",
    });
    assert.deepEqual(pickupPipeline.AutoDeliveryInDestinationMap.all_of, [recognitionNode]);
    for (const [
        node,
        body,
    ] of Object.entries(areaPipeline)) {
        if (node.endsWith("InDestinationMap")) {
            assert.deepEqual(body.all_of, [
                "CloseButtonType1",
                recognitionNode,
            ]);
            assert.equal(body.box_index, 0);
            assert.equal(body.action, "Click");
        }
    }
    const templateOccurrences = JSON.stringify([
        commonPipeline,
        pickupPipeline,
        areaPipeline,
    ]).match(/AutoDelivery\/TrackTaskSuccess\.png/g);
    assert.equal(templateOccurrences?.length, 1);
});

test("AutoDelivery 纯识别节点统一使用 Check 或 In 命名", () => {
    const pipeline = Object.assign(
        {},
        ...[
            "Common.json",
            "Pickup.json",
            "Delivery.json",
        ].map((file) =>
            parseJsonc(
                readFileSync(
                    new URL(`../../../assets/resource/pipeline/AutoDelivery/${file}`, import.meta.url),
                    "utf8",
                ),
            ),
        ),
    );
    const invalidNames = Object.entries(pipeline)
        .filter(
            ([
                name,
                node,
            ]) =>
                name !== "AutoDelivery" &&
                node.recognition &&
                node.recognition !== "DirectHit" &&
                !node.action &&
                !/^AutoDelivery(?:Check|In)/.test(name),
        )
        .map(([name]) => name);

    assert.deepEqual(invalidNames, []);
});

test("AutoDelivery 省略默认 DirectHit 识别和 DoNothing 动作", () => {
    const routeDirectory = new URL("../../../assets/resource/pipeline/AutoDelivery/Routes/", import.meta.url);
    const pipelineFiles = [
        new URL("./routes-template.json", import.meta.url),
        new URL("../../../assets/resource/pipeline/AutoDelivery/Common.json", import.meta.url),
        new URL("../../../assets/resource/pipeline/AutoDelivery/Pickup.json", import.meta.url),
        new URL("../../../assets/resource/pipeline/AutoDelivery/Delivery.json", import.meta.url),
        new URL("../../../assets/resource/pipeline/AutoDelivery/RecycleBinAreas.json", import.meta.url),
        new URL("../../../assets/resource/pipeline/AutoDelivery/RecycleBinCandidates.json", import.meta.url),
        ...readdirSync(routeDirectory)
            .filter((file) => file.endsWith(".json"))
            .map((file) => new URL(file, routeDirectory)),
    ];

    for (const file of pipelineFiles) {
        for (const [
            name,
            node,
        ] of Object.entries(readJsonc(file))) {
            assert.notEqual(node.recognition, "DirectHit", `${name} 不应显式设置默认 DirectHit 识别`);
            assert.notEqual(node.action, "DoNothing", `${name} 不应显式设置默认 DoNothing 动作`);
        }
    }
});

test("AutoDelivery 仅合并同一地图同一区域的多个资源回收站", () => {
    const groups = groupAmbiguousRecycleBins([
        {id: "map01-only", kind: "recycle_bin", map: "map01", areaId: "SharedArea"},
        {id: "map02-first", kind: "recycle_bin", map: "map02", areaId: "SharedArea"},
        {id: "map02-second", kind: "recycle_bin", map: "map02", areaId: "SharedArea"},
        {id: "map02-npc", kind: "npc", map: "map02", areaId: "SharedArea"},
    ]);

    assert.deepEqual(
        groups.map((group) => group.map(({id}) => id)),
        [
            [
                "map02-first",
                "map02-second",
            ],
        ],
    );
});

test("AutoDelivery 资源回收站保留游戏内区域序号", () => {
    assert.match(catalogSource.text.serial_id, /RecycleBinTable\.serialId/);
    const serialsByArea = new Map();
    const sources = catalogSource.destinations.filter((destination) => destination.kind === "recycle_bin");
    assert.equal(sources.length, recycleBins.length);

    for (const source of sources) {
        assert.ok(Number.isInteger(source.serial_id) && source.serial_id > 0, `${source.id} has invalid serial_id`);
        const area = source.area.en_us;
        const serials = serialsByArea.get(area) ?? new Set();
        assert.equal(serials.has(source.serial_id), false, `${area} has duplicate recycle bin #${source.serial_id}`);
        serials.add(source.serial_id);
        serialsByArea.set(area, serials);
    }
});

test("AutoDelivery 将 BaseNav 地区映射到 MapLocator 资源区", () => {
    assert.deepEqual(
        [...new Set(destinations.map((destination) => `${destination.map}:${destination.mapZone}`))].sort(),
        [
            "map01:ValleyIV",
            "map02:Wuling",
        ],
    );
});

test("AutoDelivery 为同地图同区域的多个资源回收站生成地图图标候选", () => {
    const areaPipeline = JSON.parse(
        readFileSync(
            new URL("../../../assets/resource/pipeline/AutoDelivery/RecycleBinAreas.json", import.meta.url),
            "utf8",
        ),
    );
    const candidatePipeline = JSON.parse(
        readFileSync(
            new URL("../../../assets/resource/pipeline/AutoDelivery/RecycleBinCandidates.json", import.meta.url),
            "utf8",
        ),
    );
    const pipeline = {...areaPipeline, ...candidatePipeline};
    assert.ok(recycleBins.length > 0);
    assert.equal(recycleBinResolveNodes.length, ambiguousRecycleBins.length);
    assert.equal(recycleBinCandidateRows.length, ambiguousRecycleBins.length);
    assert.equal(Object.keys(areaPipeline).length, recycleBinAreaRows.length * 3);
    assert.equal(Object.keys(candidatePipeline).length, recycleBinCandidateRows.length);
    assert.equal(
        Object.keys(pipeline).length,
        Object.keys(areaPipeline).length + Object.keys(candidatePipeline).length,
    );
    assert.equal(
        existsSync(new URL("../../../assets/resource/pipeline/AutoDelivery/RecycleBins.json", import.meta.url)),
        false,
    );
    assert.equal(JSON.stringify(pipeline).includes("四号谷地"), false);

    const sourceById = new Map(
        catalogSource.destinations.map((destination) => [
            destination.id,
            destination,
        ]),
    );
    const destinationById = new Map(
        destinations.map((destination) => [
            destination.id,
            destination,
        ]),
    );
    for (const {node, destinationId} of recycleBinResolveNodes) {
        const source = sourceById.get(destinationId);
        const destination = destinationById.get(destinationId);
        assert.ok(source);
        assert.ok(destination);
        assert.equal(pipeline[node].custom_recognition, "MapFind");
        assert.deepEqual(pipeline[node].custom_recognition_param, {
            zone: destination.mapZone,
            icon: "RecycleBin",
            at: [
                source.u,
                source.v,
            ],
        });
        assert.equal(pipeline[node].custom_action, "AutoDeliveryResolveDestinationAction");
        assert.deepEqual(pipeline[node].custom_action_param, {
            destination_id: destinationId,
        });
        assert.deepEqual(pipeline[node].next, [
            "AutoDeliveryPrepareNavigateDestination",
        ]);
    }

    assert.equal(recycleBinAreaRows.length, ambiguousRecycleBinGroups.length);
    for (const areaBins of ambiguousRecycleBinGroups) {
        assert.ok(areaBins.length > 1);
        assert.equal(new Set(areaBins.map(({map}) => map)).size, 1);
        const [{areaId}] = areaBins;
        const inMapNode = `AutoDeliveryRecycleBin${areaId}InDestinationMap`;
        assert.deepEqual(pipeline[`AutoDeliveryViewRecycleBin${areaId}Map`].next, [
            inMapNode,
        ]);
        assert.deepEqual(pipeline[`AutoDeliveryStartTrackingRecycleBin${areaId}`].next, [
            inMapNode,
        ]);
        assert.deepEqual(
            pipeline[inMapNode].next,
            areaBins.map(({id}) => {
                const {node} = recycleBinResolveNodes.find(({destinationId}) => destinationId === id);
                return node;
            }),
        );
    }
});

test("AutoDelivery 已生成 Pipeline 按仓储分组并与运行时目录覆盖全部路线", () => {
    const routesDir = new URL("../../../assets/resource/pipeline/AutoDelivery/Routes/", import.meta.url);
    const routeFiles = readdirSync(routesDir)
        .filter((file) => file.endsWith(".json"))
        .sort();
    const expectedRouteFiles = [...new Set(routeRows.map((row) => `${row.RouteFileId}.json`))].sort();
    assert.deepEqual(routeFiles, expectedRouteFiles);
    assert.equal(
        existsSync(new URL("../../../assets/resource/pipeline/AutoDelivery/Routes.json", import.meta.url)),
        false,
    );

    const pipelinesByFile = new Map(
        routeFiles.map((file) => [
            file,
            JSON.parse(readFileSync(new URL(file, routesDir), "utf8")),
        ]),
    );
    const pipeline = Object.assign({}, ...pipelinesByFile.values());
    const catalog = JSON.parse(
        readFileSync(new URL("../../../assets/data/AutoDelivery/catalog.json", import.meta.url), "utf8"),
    );
    assert.deepEqual(catalog, runtimeCatalog);
    assert.deepEqual(new Set(Object.keys(pipeline)), new Set(routeRows.map((item) => item.Node)));
    for (const row of routeRows) {
        const node = pipelinesByFile.get(`${row.RouteFileId}.json`)[row.Node];
        assert.equal(node.custom_action, "MapNavigateAction");
        assert.deepEqual(node.custom_action_param, row.ActionParam.value);
        assert.equal(node.next, undefined);
    }
});

test("AutoDelivery focus 文案统一使用完整 i18n 键", () => {
    const pipelineDir = new URL("../../../assets/resource/pipeline/AutoDelivery/", import.meta.url);
    const pipelineUrls = readdirSync(pipelineDir)
        .filter((file) => file.endsWith(".json"))
        .map((file) => new URL(file, pipelineDir));
    const routePipelineDir = new URL("Routes/", pipelineDir);
    pipelineUrls.push(
        ...readdirSync(routePipelineDir)
            .filter((file) => file.endsWith(".json"))
            .map((file) => new URL(file, routePipelineDir)),
    );
    const focusKeys = new Set();

    for (const url of pipelineUrls) {
        const pipeline = parseJsonc(readFileSync(url, "utf8"));
        for (const node of Object.values(pipeline)) {
            for (const value of Object.values(node.focus || {})) {
                if (typeof value !== "string") {
                    continue;
                }
                assert.match(value, /^\$task\.AutoDelivery\.focus\.[a-z0-9_]+$/, `${url.pathname}: ${value}`);
                focusKeys.add(value.slice(1));
            }
        }
    }

    const expectedKeys = [...focusKeys].sort();
    assert.ok(expectedKeys.length > 0);
    const dynamicFocusKeys = [
        "autodelivery.focus.depot_resolved",
        "autodelivery.focus.destination_resolved",
    ];
    const goServiceKeys = [
        ...dynamicFocusKeys,
        "autodelivery.destination.recycle_bin",
    ].sort();

    for (const lang of [
        "zh_cn",
        "zh_tw",
        "en_us",
        "ja_jp",
        "ko_kr",
    ]) {
        const locale = readJsonc(new URL(`../../../assets/locales/interface/${lang}.json`, import.meta.url));
        const localeKeys = Object.keys(locale)
            .filter((key) => key.startsWith("task.AutoDelivery.focus."))
            .sort();
        assert.deepEqual(localeKeys, expectedKeys, `${lang} AutoDelivery focus keys differ`);
        for (const key of expectedKeys) {
            assert.match(locale[key], /^🚛 /, `${lang} ${key} is missing the AutoDelivery emoji`);
            if (lang === "zh_cn" || lang === "zh_tw") {
                assert.equal(locale[key].includes("正在"), false, `${lang} ${key} should use a concise action phrase`);
            }
        }

        const goServiceLocale = readJsonc(new URL(`../../../assets/locales/go-service/${lang}.json`, import.meta.url));
        const goServiceLocaleKeys = Object.keys(goServiceLocale)
            .filter((key) => key.startsWith("autodelivery."))
            .sort();
        assert.deepEqual(goServiceLocaleKeys, goServiceKeys, `${lang} AutoDelivery go-service keys differ`);
        for (const key of dynamicFocusKeys) {
            assert.match(goServiceLocale[key], /^🚛 /, `${lang} ${key} is missing the AutoDelivery emoji`);
        }
        for (const key of goServiceKeys) {
            assert.equal((goServiceLocale[key].match(/%s/g) || []).length, 1, `${lang} ${key} must contain one %s`);
        }
        assert.equal(
            (goServiceLocale["autodelivery.destination.recycle_bin"].match(/%d/g) || []).length,
            1,
            `${lang} autodelivery.destination.recycle_bin must contain one %d`,
        );
    }
});
