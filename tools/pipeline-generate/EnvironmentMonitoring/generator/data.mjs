import {
    buildDefaultId,
    buildMonitoringTerminalIds,
    collectMonitoringMissions,
    ensureUniqueId,
    LOCALES,
    readKiteStationData,
    readJson,
    ROUTES_PATH,
    sanitizeDisplayName,
    toPascalCase,
} from "./common.mjs";
import {createRouteResolver} from "./route-resolver.mjs";

function loadKiteStationData() {
    try {
        return readKiteStationData();
    } catch {
        console.error(
            "[EnvironmentMonitoring] 数据文件缺失，请先运行 pnpm fetch:zmdmap 或 pnpm generate:EnvironmentMonitoring 以获取最新数据",
        );
        process.exit(1);
    }
}

export const kiteStationData = loadKiteStationData();

const ROUTE_CONFIG = readJson(ROUTES_PATH);

// 监测终端 ID 列表直接从 zmdmap 缓存数据派生：凡是带有 entrustTasks 的条目都算。
// 上游游戏数据若新增监测终端会自动包含；新终端要真正可用还需手动补 Pipeline 侧的联动节点
// （Locations.json / EnvironmentMonitoringLoop.next 等），详见 docs 维护手册。
export const MONITORING_TERMINAL_IDS = buildMonitoringTerminalIds(kiteStationData);

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

const routeResolver = createRouteResolver(ROUTE_CONFIG);

function buildStationName(terminalId) {
    const stationEnglishName = kiteStationData?.[terminalId]?.level?.name?.["en-US"];
    if (!stationEnglishName) {
        // 没匹配到游戏数据时通常意味着 mission.kiteStation 与 zmdmap 数据主键脱节，
        // 直接 PascalCase terminalId 容易得到中文/纯数字串这种诡异结果。打个 warn 让维护者尽早发现。
        console.warn(
            `[EnvironmentMonitoring] 找不到 ${terminalId} 对应的英文站点名，已退化使用 terminalId。请检查 zmdmap 缓存数据是否同步。`,
        );
    }
    return toPascalCase(stationEnglishName || terminalId) || terminalId;
}

function buildGoToMonitoringTerminal(station) {
    // Locations.json 中节点统一遵循 EnvironmentMonitoringGoTo{Station} 命名，新终端在 Locations.json 手写补齐。
    return `EnvironmentMonitoringGoTo${station}`;
}

function buildRow(mission, usedIds) {
    const missionName = mission?.name?.["zh-CN"] || mission?.missionId || "UnknownMission";
    const route = routeResolver.resolve(mission);

    const baseId = route.override?.Id || buildDefaultId(mission);
    const Id = ensureUniqueId(baseId, usedIds, mission?.missionId);
    const Station = buildStationName(mission?.kiteStation || mission?.__terminalId);
    const GoToMonitoringTerminal = buildGoToMonitoringTerminal(Station);

    // 游戏内未追踪时无法完成任务，已适配点也要先走追踪确认。
    const TrackOrGoToNext = [
        `Track${Id}`,
        `AlreadyTracked${Id}`,
    ];
    const AfterTrackedNext = route.isAdapted ? [`GoTo${Id}`] : [`${Id}NotAdapted`];

    return {
        Station,
        Id,
        MissionId: mission?.missionId,
        Name: sanitizeDisplayName(missionName),
		NameKey: `task.EnvironmentMonitoring.route.${Id}.label`,
        GoToMonitoringTerminal,
        EnterMap: route.EnterMap,
        MapName: route.MapName,
        MapAssert: route.MapAssert,
        MapPath: route.MapPath,
        MapTarget: route.MapTarget,
        MapTargetTier: route.MapTargetTier,
        MapGoal: route.MapGoal,
        MapAssertRecognition: route.MapAssertRecognition,
        MapAssertParam: rawJson(route.MapAssertParam),
        CameraSwipeDirection: route.CameraSwipeDirection,
        CameraMaxHit: route.CameraMaxHit,
        ExpectedText: buildExpectedFromLocaleMap(mission.name),
        InExpectedText: buildExpectedFromLocaleMap(mission.shotTargetName),
        OcrReplace: rawJson(route.Replace),
        TrackOrGoToNext: rawJson(TrackOrGoToNext),
        AfterTrackedNext: rawJson(AfterTrackedNext),
        MapNavigationAction: route.MapNavigationAction,
        MapNavigationParam: rawJson(route.MapNavigationParam),
    };
}

const usedIds = new Set();
const rows = collectMonitoringMissions(kiteStationData).map((mission) => buildRow(mission, usedIds));
routeResolver.warnUnusedRouteOverrides();

export default rows;
