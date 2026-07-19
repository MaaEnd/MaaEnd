import assert from "node:assert/strict";
import test from "node:test";

import {buildRow} from "./data.mjs";

function buildDirectPhotoRow(hasHeading) {
    return buildRow({
        Station: "TestMonitoringTerminal",
        Id: "TestMission",
        Name: "测试观察点",
        LocalizedName: {
            "zh-CN": "测试观察点",
        },
        ShotTargetName: {
            "zh-CN": "测试目标",
        },
        route: {
            isAdapted: true,
            QuickTeleport: false,
            IsDirectPhoto: true,
            HasHeading: hasHeading,
            ShouldAssertAfterTeleport: false,
            EnterMap: "SceneEnterWorldTest",
            MapAssertRecognition: "MapTrackerAssertLocation",
            MapAssertParam: {},
            CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
            CameraMaxHit: 2,
            Replace: [],
            RouteAction: "MapTrackerToward",
            RouteActionParam: hasHeading ? {angle: 90} : {},
        },
    });
}

test("direct-photo routes always teleport instead of checking MapAssert", () => {
    const row = buildDirectPhotoRow(false);

    assert.deepEqual(row.GoToNext.value, ["GoToTestMissionNotAtStartPos"]);
    assert.deepEqual(row.AfterTeleportNext.value, ["TestMissionTakePhoto"]);
});

test("direct-photo routes apply optional Heading before taking a photo", () => {
    const row = buildDirectPhotoRow(true);

    assert.deepEqual(row.AfterTeleportNext.value, ["GoToTestMissionMove"]);
    assert.equal(row.RouteAction, "MapTrackerToward");
    assert.deepEqual(row.RouteActionParam.value, {angle: 90});
    assert.equal(row.MoveDescription, "在测试观察点传送点调整拍照朝向");
});
