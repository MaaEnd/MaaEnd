import assert from "node:assert/strict";
import test from "node:test";

import {readJson, ROUTES_PATH} from "./common.mjs";
import {collectMissingRouteFields, createRouteResolver} from "./route-resolver.mjs";

const mission = {
    missionId: "test-mission",
    name: {
        zh_cn: "测试观察点",
    },
};

function resolve(route) {
    return createRouteResolver([route], {warn() {}}).resolve(mission);
}

test("configured base paths expose the post-teleport map to WebUI", () => {
    const routes = readJson(ROUTES_PATH);

    for (const route of routes) {
        for (const field of [
            "NavPath",
            "FightAfterMove",
        ]) {
            const path = route[field];
            if (!Array.isArray(path)) {
                continue;
            }

            const targetTiers = new Set(path.map((node) => node?.target_tier).filter(Boolean));
            if (targetTiers.size > 0) {
                // target_tier describes the destination coordinate system, not the post-teleport map.
                continue;
            }

            assert.equal(path[0]?.action, "ZONE", `${route.MissionId}.${field} must start with post-teleport ZONE`);
            assert.equal(typeof path[0]?.zone_id, "string", `${route.MissionId}.${field} ZONE must provide zone_id`);
            assert.notEqual(path[0].zone_id, "", `${route.MissionId}.${field} ZONE must provide zone_id`);
        }
    }
});

test("metadata-only entries remain unadapted", () => {
    const route = {
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
    };
    const result = resolve(route);

    assert.equal(result.isAdapted, false);
    assert.equal(result.IsDirectPhoto, false);
    assert.deepEqual(result.missingFields, ["EnterMap"]);
});

test("an EnterMap route without navigation fields takes photos directly", () => {
    const route = {
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
    };
    const result = resolve(route);

    assert.equal(result.isAdapted, true);
    assert.equal(result.IsDirectPhoto, true);
    assert.deepEqual(result.missingFields, []);
});

test("a QuickTeleport route can take photos directly without EnterMap", () => {
    const route = {
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        QuickTeleport: true,
    };
    const result = resolve(route);

    assert.equal(result.isAdapted, true);
    assert.equal(result.IsDirectPhoto, true);
    assert.deepEqual(result.missingFields, []);
});

test("direct-photo routes support Heading without navigation configuration", () => {
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        Heading: 90,
    });

    assert.equal(result.isAdapted, true);
    assert.equal(result.IsDirectPhoto, true);
    assert.equal(result.RouteAction, "MapNavigateAction");
    assert.deepEqual(result.RouteActionParam, {
        path: [
            {
                action: "HEADING",
                angle: 90,
            },
        ],
    });
});

test("removed map shorthand fields no longer count as a route", () => {
    // MapTarget 简写已移除：只带旧字段的条目按直拍处理，旧字段被忽略。
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        MapName: "map02_lv001",
        MapTarget: [
            5,
            5,
        ],
    });

    assert.equal(result.isAdapted, true);
    assert.equal(result.IsDirectPhoto, true);
    assert.deepEqual(result.missingFields, []);
    assert.deepEqual(result.RouteActionParam, {
        path: [
            {
                action: "HEADING",
                angle: 0,
            },
        ],
    });
});

test("Nav route fields render MapLocateAssertLocation + MapNavigateAction", () => {
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        NavZoneId: "Wuling_Base",
        NavAssert: [
            0,
            0,
            10,
            10,
        ],
        NavPath: [
            {
                action: "ZONE",
                zone_id: "Wuling_Base",
            },
            [
                5,
                5,
            ],
        ],
        Heading: 90,
    });

    assert.deepEqual(result.missingFields, []);
    assert.equal(result.IsDirectPhoto, false);
    // 路线自己接管落点，传送后不再复核起点
    assert.equal(result.ShouldAssertAfterTeleport, false);
    assert.equal(result.MapAssertRecognition, "MapLocateAssertLocation");
    assert.deepEqual(result.MapAssertParam, {
        zone_id: "Wuling_Base",
        target: [
            0,
            0,
            10,
            10,
        ],
    });
    assert.equal(result.RouteAction, "MapNavigateAction");
    assert.deepEqual(result.RouteActionParam, {
        path: [
            {
                action: "ZONE",
                zone_id: "Wuling_Base",
            },
            [
                5,
                5,
            ],
            {
                action: "HEADING",
                angle: 90,
            },
        ],
    });
});

test("FightAfterMove uses an independent route and shares the photo heading", () => {
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        NavZoneId: "Wuling_Base",
        NavAssert: [
            0,
            0,
            10,
            10,
        ],
        NavPath: [
            {
                action: "NAVMESH",
                target: [
                    10,
                    10,
                ],
            },
        ],
        Heading: 90,
        FightAfterMove: [
            {
                action: "NAVMESH",
                target: [
                    20,
                    20,
                ],
            },
            {
                action: "NAVMESH",
                target: [
                    10,
                    10,
                ],
            },
        ],
    });

    assert.equal(result.FightAfterMove, true);
    assert.deepEqual(result.RouteActionParam.path, [
        {
            action: "NAVMESH",
            target: [
                10,
                10,
            ],
        },
        {
            action: "HEADING",
            angle: 90,
        },
    ]);
    assert.deepEqual(result.FightAfterMoveRouteActionParam.path, [
        {
            action: "NAVMESH",
            target: [
                20,
                20,
            ],
        },
        {
            action: "NAVMESH",
            target: [
                10,
                10,
            ],
        },
        {
            action: "HEADING",
            angle: 90,
        },
    ]);
});

test("direct-photo routes reject FightAfterMove", () => {
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        FightAfterMove: [
            {
                action: "ZONE",
                zone_id: "Wuling_Base",
            },
            [
                5,
                5,
            ],
        ],
    });

    assert.equal(result.isAdapted, false);
    assert.equal(result.IsDirectPhoto, false);
    assert.equal(result.FightAfterMove, false);
    assert.deepEqual(result.missingFields, ["NavZoneId/NavAssert/NavPath 必须同时配置"]);
});

test("QuickTeleport routes reject FightAfterMove without NavPath", () => {
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        QuickTeleport: true,
        FightAfterMove: [
            {
                action: "ZONE",
                zone_id: "Wuling_Base",
            },
            [
                5,
                5,
            ],
        ],
    });

    assert.equal(result.isAdapted, false);
    assert.equal(result.IsDirectPhoto, false);
    assert.equal(result.FightAfterMove, false);
    assert.deepEqual(result.missingFields, ["NavPath 必须同时配置"]);
});

test("QuickTeleport Nav routes only require NavPath", () => {
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        QuickTeleport: true,
        NavPath: [
            {
                action: "NAVMESH",
                target: [
                    5,
                    5,
                ],
            },
        ],
    });

    assert.equal(result.isAdapted, true);
    assert.equal(result.IsDirectPhoto, false);
    assert.equal(result.ShouldAssertAfterTeleport, false);
    assert.equal(result.MapAssertRecognition, "MapLocateAssertLocation");
    assert.deepEqual(result.MapAssertParam, {
        zone_id: "Wuling_Base",
        target: [
            0,
            0,
            1,
            1,
        ],
    });
    assert.equal(result.RouteAction, "MapNavigateAction");
    assert.deepEqual(result.RouteActionParam, {
        path: [
            {
                action: "NAVMESH",
                target: [
                    5,
                    5,
                ],
            },
        ],
    });
    assert.deepEqual(result.missingFields, []);
});

test("incomplete Nav route fields are reported", () => {
    const missingFields = collectMissingRouteFields({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        NavZoneId: "Wuling_Base",
        NavPath: [
            [
                5,
                5,
            ],
        ],
    });

    assert.deepEqual(missingFields, ["NavZoneId/NavAssert/NavPath 必须同时配置"]);
});

test("QuickTeleport Nav routes with a partial assert still require the full set", () => {
    const missingFields = collectMissingRouteFields({
        QuickTeleport: true,
        NavZoneId: "Wuling_Base",
        NavPath: [
            [
                5,
                5,
            ],
        ],
    });

    assert.deepEqual(missingFields, ["NavZoneId/NavAssert/NavPath 必须同时配置"]);
});
