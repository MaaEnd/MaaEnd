import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";

import routeRows from "./routes-data.mjs";
import {depots, destinations, runtimeCatalog} from "./model.mjs";
import {buildSyncedRouteConfig} from "./sync-routes.mjs";

const catalogSource = JSON.parse(readFileSync(new URL("../data/delivery_destinations.json", import.meta.url), "utf8"));
const routeSource = JSON.parse(readFileSync(new URL("./routes.json", import.meta.url), "utf8"));

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
            retry_path: [
                [
                    3,
                    4,
                ],
            ],
        },
    ]);
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
        assert.match(row.Description, /仓储节点/);
    }
});

test("AutoDelivery 运行时目录只保留匹配文本和生成节点名", () => {
    assert.equal(runtimeCatalog.depots.length, depots.length);
    assert.equal(runtimeCatalog.destinations.length, destinations.length);
    assert.equal(JSON.stringify(runtimeCatalog).includes('"path"'), false);
    assert.equal(JSON.stringify(runtimeCatalog).includes('"u"'), false);
    assert.equal(JSON.stringify(runtimeCatalog).includes('"v"'), false);
});

test("AutoDelivery 已生成 Pipeline 与运行时目录覆盖全部路线", () => {
    const pipeline = JSON.parse(
        readFileSync(new URL("../../../assets/resource/pipeline/AutoDelivery/Routes.json", import.meta.url), "utf8"),
    );
    const catalog = JSON.parse(
        readFileSync(new URL("../../../assets/data/AutoDelivery/catalog.json", import.meta.url), "utf8"),
    );
    assert.deepEqual(catalog, runtimeCatalog);
    assert.deepEqual(new Set(Object.keys(pipeline)), new Set(routeRows.map((item) => item.Node)));
    for (const row of routeRows) {
        assert.equal(pipeline[row.Node].custom_action, "MapNavigateAction");
        assert.deepEqual(pipeline[row.Node].custom_action_param, row.ActionParam.value);
        assert.equal(pipeline[row.Node].next, undefined);
    }
});
