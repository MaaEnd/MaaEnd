/**
 * 检测 zmdmap 缓存数据中缺失或未完整适配的环境监测观察点。
 * 运行方式（在仓库根目录）：
 *   node .agents/skills/environment-monitoring-add-route/check_missing.mjs
 */
import {
    buildGeneratedIdIndex,
    collectMonitoringMissions,
    KITE_STATION_DATA_PATH,
    readJson,
    ROUTES_PATH,
} from "../../../tools/pipeline-generate/EnvironmentMonitoring/generator/common.mjs";
import {collectMissingRouteFields} from "../../../tools/pipeline-generate/EnvironmentMonitoring/generator/route-resolver.mjs";

const kiteStationData = readJson(KITE_STATION_DATA_PATH);
const routes = readJson(ROUTES_PATH);
const missions = collectMonitoringMissions(kiteStationData);
const idByMissionId = buildGeneratedIdIndex(missions);
const routeByMissionId = new Map();

for (const route of routes) {
    if (!route.MissionId) {
        console.warn(
            `[EnvironmentMonitoring] routes.json 条目 ${route.Name || route.Id || "<unknown>"} 缺少 MissionId。`,
        );
        continue;
    }
    if (routeByMissionId.has(route.MissionId)) {
        console.warn(`[EnvironmentMonitoring] routes.json 中存在重复 MissionId: ${route.MissionId}。`);
    }
    routeByMissionId.set(route.MissionId, route);
}

const pending = missions
    .map((mission) => {
        const route = routeByMissionId.get(mission.missionId);
        const missingFields = collectMissingRouteFields(route);
        if (missingFields.length === 0) {
            return null;
        }

        return {
            Name: mission.name?.["zh-CN"] || route?.Name || mission.missionId,
            Id: route?.Id || idByMissionId.get(mission.missionId),
            MissionId: mission.missionId,
            MissingFields: missingFields,
        };
    })
    .filter(Boolean);

if (pending.length === 0) {
    console.log("✓ 所有观察点均已完整适配，无待适配条目。");
} else {
    console.log(`发现 ${pending.length} 个待适配条目：`);
    console.log(JSON.stringify(pending, null, 2));
}
