import assert from "node:assert/strict";
import test from "node:test";

import {collectMissingRouteFields, createRouteResolver} from "./route-resolver.mjs";

const mission = {
    missionId: "test-mission",
    name: {
        "zh-CN": "测试观察点",
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
