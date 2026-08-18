import assert from "node:assert/strict";
import test from "node:test";

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

test("metadata-only entries remain unadapted", () => {
    const route = {
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
    };
    const result = resolve(route);

    assert.equal(result.isAdapted, false);
    assert.equal(result.IsDirectPhoto, false);
    assert.deepEqual(result.missingFields, [
        "EnterMap",
        "CameraSwipeDirection",
    ]);
});

test("an EnterMap route without navigation fields takes photos directly", () => {
    const route = {
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
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
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenLeft",
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
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
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
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
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
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
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
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
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
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
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
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    });

    assert.deepEqual(missingFields, ["NavZoneId/NavAssert/NavPath 必须同时配置"]);
});
