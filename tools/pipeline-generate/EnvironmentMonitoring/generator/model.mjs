import {
    buildDefaultId,
    buildMonitoringTerminalIds,
    collectMonitoringMissions,
    ensureUniqueId,
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

// 监测终端 ID 列表直接从 zmdmap 缓存数据派生：entrustTasks.list 非空的条目都算。
// 上游游戏数据若新增监测终端会自动包含；新终端要真正可用还需手动补 Pipeline 侧的联动节点
// （Locations.json / EnvironmentMonitoringLoop.next 等），详见 docs 维护手册。
export const MONITORING_TERMINAL_IDS = buildMonitoringTerminalIds(kiteStationData);

const routeResolver = createRouteResolver(readJson(ROUTES_PATH));

function buildStationId(terminalId) {
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

function buildMissionModel(mission, usedIds) {
    const missionName = mission?.name?.["zh-CN"] || mission?.missionId || "UnknownMission";
    const route = routeResolver.resolve(mission);
    const baseId = route.override?.Id || buildDefaultId(mission);
    const Id = ensureUniqueId(baseId, usedIds, mission?.missionId);
    const Station = buildStationId(mission?.kiteStation || mission?.__terminalId);

    return {
        Station,
        Id,
        MissionId: mission?.missionId,
        Name: sanitizeDisplayName(missionName),
        LocalizedName: mission.name,
        ShotTargetName: mission.shotTargetName,
        route,
    };
}

const usedIds = new Set();

// 路线模板和终端模板共享这一份规范化任务模型，再各自投影为最小模板数据。
export const monitoringMissions = collectMonitoringMissions(kiteStationData).map((mission) =>
    buildMissionModel(mission, usedIds),
);

routeResolver.warnUnusedRouteOverrides();
