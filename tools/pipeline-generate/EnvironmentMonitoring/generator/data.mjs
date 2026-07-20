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

export function buildRow(mission) {
    const {Station, Id, Name, LocalizedName, ShotTargetName, route} = mission;
    const GoToMonitoringTerminal = buildGoToMonitoringTerminal(Station);

    // 游戏内未追踪时无法完成任务，已适配点也要先走追踪确认。
    const TrackOrGoToNext = [
        `Track${Id}`,
        `AlreadyTracked${Id}`,
    ];
    const defaultAfterTrackedNext = route.isAdapted ? [`GoTo${Id}`] : [`${Id}NotAdapted`];
    const afterTrackNext =
        route.isAdapted && route.QuickTeleport ? [`${Id}InQuickTeleportMap`] : defaultAfterTrackedNext;
    const afterAlreadyTrackedNext =
        route.isAdapted && route.QuickTeleport ? [`${Id}OpenTrackedMissionMap`] : defaultAfterTrackedNext;
    // 传送点可直接拍照时不需要位置断言：统一先进入大世界，再无条件执行真实传送。
    const GoToNext =
        route.IsDirectPhoto && !route.QuickTeleport
            ? [`GoTo${Id}NotAtStartPos`]
            : [
                  `GoTo${Id}StartPos`,
                  `GoTo${Id}NotAtStartPos`,
              ];
    // NavMesh 可自行处理传送点附近的落点；手录路径仍需确认固定起点。
    const AfterTeleportDescription = route.IsDirectPhoto
        ? route.HasHeading
            ? "传送后调整朝向并拍照"
            : "传送后直接拍照"
        : route.ShouldAssertAfterTeleport
          ? "先传送再检查落点"
          : "先传送再继续寻路";
    const AfterTeleportNext = route.IsDirectPhoto
        ? [route.HasHeading ? `GoTo${Id}Move` : `${Id}TakePhoto`]
        : route.ShouldAssertAfterTeleport
          ? [`GoTo${Id}StartPos`]
          : [`GoTo${Id}Move`];
    const GoToNotAtStartPosDescription = route.IsDirectPhoto
        ? `前往${Name}传送点，${AfterTeleportDescription}`
        : `不在${Name}任务开始位置附近，${AfterTeleportDescription}`;
    const MoveDescription = route.IsDirectPhoto ? `在${Name}传送点调整拍照朝向` : `自动寻路前往${Name}`;

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
        AfterTrackNext: rawJson(afterTrackNext),
        AfterAlreadyTrackedNext: rawJson(afterAlreadyTrackedNext),
        GoToNext: rawJson(GoToNext),
        GoToNotAtStartPosDescription,
        MoveDescription,
        AfterTeleportDescription,
        AfterTeleportNext: rawJson(AfterTeleportNext),
        RouteAction: route.RouteAction,
        RouteActionParam: rawJson(route.RouteActionParam),
    };
}

export default monitoringMissions.map(buildRow);
