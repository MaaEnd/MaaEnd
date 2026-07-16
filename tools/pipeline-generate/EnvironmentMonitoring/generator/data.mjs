import {LOCALES} from "./common.mjs";
import {monitoringMissions} from "./model.mjs";

export {kiteStationData, MONITORING_TERMINAL_IDS} from "./model.mjs";

function escapeRegex(str) {
    return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function toFlexibleEnglishRegex(text) {
    const escaped = escapeRegex(text.trim());
    return `(?i)${escaped.replace(/\s+/g, "\\s*").replace(/-/g, "\\s*-\\s*")}`;
}

function buildExpectedFromLocaleMap(localeMap) {
    return LOCALES.map((locale) => {
        const value = localeMap?.[locale];
        if (!value) {
            return null;
        }
        if (locale === "en-US") {
            return toFlexibleEnglishRegex(value);
        }
        return value;
    }).filter(Boolean);
}

function rawJson(value) {
    return {
        value,
        raw: JSON.stringify(value, null, 4),
    };
}

function buildGoToMonitoringTerminal(station) {
    // Locations.json 中节点统一遵循 EnvironmentMonitoringGoTo{Station} 命名，新终端在 Locations.json 手写补齐。
    return `EnvironmentMonitoringGoTo${station}`;
}

function buildRow(mission) {
    const {Station, Id, Name, LocalizedName, ShotTargetName, route} = mission;
    const GoToMonitoringTerminal = buildGoToMonitoringTerminal(Station);

    // 游戏内未追踪时无法完成任务，已适配点也要先走追踪确认。
    const TrackOrGoToNext = [
        `Track${Id}`,
        `AlreadyTracked${Id}`,
    ];
    const AfterTrackedNext = route.isAdapted ? [`GoTo${Id}`] : [`${Id}NotAdapted`];
    // NavMesh 可自行处理传送点附近的落点；手录路径仍需确认固定起点。
    const AfterTeleportDescription = route.ShouldAssertAfterTeleport ? "先传送再检查落点" : "先传送再继续寻路";
    const AfterTeleportNext = route.ShouldAssertAfterTeleport ? [`GoTo${Id}StartPos`] : [`GoTo${Id}Move`];

    return {
        Station,
        Id,
        Name,
        FailureKey: `task.EnvironmentMonitoring.route.${Id}.failed`,
        GoToMonitoringTerminal,
        EnterMap: route.EnterMap,
        MapAssertRecognition: route.MapAssertRecognition,
        MapAssertParam: rawJson(route.MapAssertParam),
        CameraSwipeDirection: route.CameraSwipeDirection,
        CameraMaxHit: route.CameraMaxHit,
        ExpectedText: buildExpectedFromLocaleMap(LocalizedName),
        InExpectedText: buildExpectedFromLocaleMap(ShotTargetName),
        OcrReplace: rawJson(route.Replace),
        TrackOrGoToNext: rawJson(TrackOrGoToNext),
        AfterTrackedNext: rawJson(AfterTrackedNext),
        AfterTeleportDescription,
        AfterTeleportNext: rawJson(AfterTeleportNext),
        MapNavigationAction: route.MapNavigationAction,
        MapNavigationParam: rawJson(route.MapNavigationParam),
    };
}

export default monitoringMissions.map(buildRow);
