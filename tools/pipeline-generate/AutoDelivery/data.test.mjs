import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";

import routeRows from "./routes-data.mjs";
import {depots, destinations, runtimeCatalog} from "./model.mjs";

test("AutoDelivery 路线为每个仓储和终点生成可独立执行的普通/滑索节点", () => {
    const retryCount = depots.filter((item) => item.retryRouteNode).length;
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
