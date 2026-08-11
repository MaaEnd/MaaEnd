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

test("an EnterMap route without map or navigation fields takes photos directly", () => {
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

test("MapAssert without a navigation field is incomplete rather than direct-photo", () => {
    const missingFields = collectMissingRouteFields({
        EnterMap: "SceneEnterWorldTest",
        MapAssert: [
            0,
            0,
            10,
            10,
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    });

    assert.deepEqual(missingFields, [
        "MapName",
        "MapPath/MapTarget/MapGoal",
    ]);
});

test("direct-photo routes support Heading without map configuration", () => {
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
    assert.equal(result.RouteAction, "MapTrackerToward");
    assert.deepEqual(result.RouteActionParam, {angle: 90});
});

test("direct-photo routes reject unused map fields", () => {
    const missingFields = collectMissingRouteFields({
        EnterMap: "SceneEnterWorldTest",
        MapName: "map02_lv001",
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    });

    assert.deepEqual(missingFields, ["传送后直拍不应配置 MapName"]);
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

test("Nav route fields cannot be mixed with the map route fields", () => {
    const missingFields = collectMissingRouteFields({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        MapName: "map02_lv001",
        MapGoal: [
            5,
            5,
        ],
        NavZoneId: "Wuling_Base",
        NavAssert: [
            0,
            0,
            10,
            10,
        ],
        NavPath: [
            [
                5,
                5,
            ],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    });

    assert.deepEqual(missingFields, ["NavPath 路线不应配置 MapName/MapGoal"]);
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

test("existing navigation forms remain adapted", () => {
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        MapName: "map02_lv001",
        MapAssert: [
            0,
            0,
            10,
            10,
        ],
        MapPath: [
            [
                5,
                5,
            ],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    });

    assert.equal(result.isAdapted, true);
    assert.equal(result.IsDirectPhoto, false);
    assert.deepEqual(result.missingFields, []);
});

test("QuickTeleport MapPath routes can skip MapAssert", () => {
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        QuickTeleport: true,
        MapName: "map02_lv001",
        MapPath: [
            [
                5,
                5,
            ],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    });

    assert.equal(result.isAdapted, true);
    assert.equal(result.IsDirectPhoto, false);
    assert.equal(result.ShouldAssertAfterTeleport, false);
    assert.equal(result.RouteAction, "MapTrackerMove");
    assert.deepEqual(result.missingFields, []);
});

test("non-QuickTeleport MapPath routes still require MapAssert", () => {
    const missingFields = collectMissingRouteFields({
        EnterMap: "SceneEnterWorldTest",
        MapName: "map02_lv001",
        MapPath: [
            [
                5,
                5,
            ],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    });

    assert.deepEqual(missingFields, ["MapAssert"]);
});

test("non-QuickTeleport MapGoal routes still skip the post-teleport assertion", () => {
    const result = resolve({
        MissionId: mission.missionId,
        Name: "测试观察点",
        Id: "TestMission",
        EnterMap: "SceneEnterWorldTest",
        MapName: "map02_lv001",
        MapAssert: [
            0,
            0,
            10,
            10,
        ],
        MapGoal: [
            5,
            5,
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    });

    assert.equal(result.isAdapted, true);
    assert.equal(result.ShouldAssertAfterTeleport, false);
    assert.equal(result.RouteAction, "MapTrackerGoal");
    assert.deepEqual(result.missingFields, []);
});
