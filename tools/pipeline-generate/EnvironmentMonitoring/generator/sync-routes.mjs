import {existsSync, readFileSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";
import {pathToFileURL} from "node:url";
import {
    buildGeneratedIdIndex,
    collectMonitoringMissions,
    KITE_STATION_DATA_PATH,
    readJson,
    ROUTES_PATH,
} from "./common.mjs";

const ROUTE_METADATA_KEYS = new Set([
    "MissionId",
    "Name",
    "Id",
]);

function normalizeSearchText(text) {
    return String(text || "")
        .replace(/[\s"“”'‘’「」『』《》【】（）()，,。.!！？?：:；;\-_]/g, "")
        .toLowerCase();
}

function buildMissionSearchIndex(missions, idByMissionId) {
    const missionById = new Map();
    const missionsByName = new Map();

    for (const mission of missions) {
        missionById.set(mission.missionId, mission);

        for (const values of [
            Object.values(mission.name || {}),
            Object.values(mission.shotTargetName || {}),
            [
                idByMissionId.get(mission.missionId),
            ],
        ]) {
            for (const value of values) {
                const key = normalizeSearchText(value);
                if (!key) {
                    continue;
                }
                const matches = missionsByName.get(key) || [];
                matches.push(mission);
                missionsByName.set(key, matches);
            }
        }
    }

    return {
        missionById,
        missionsByName,
    };
}

function findMissionForRoute(route, index) {
    if (route.MissionId) {
        if (index.missionById.has(route.MissionId)) {
            return index.missionById.get(route.MissionId);
        }
        console.warn(
            `[EnvironmentMonitoring] routes.json 条目 ${route.Name || route.MissionId} 的 MissionId 无法匹配当前 zmdmap 任务，请手动修正。`,
        );
        return null;
    }

    for (const value of [
        route.Name,
        route.Id,
    ]) {
        const key = normalizeSearchText(value);
        if (!key) {
            continue;
        }
        const matches = [...new Set(index.missionsByName.get(key) || [])];
        if (matches.length === 1) {
            return matches[0];
        }
        if (matches.length > 1) {
            console.warn(
                `[EnvironmentMonitoring] routes.json 条目 ${route.Name || value} 缺少有效 MissionId，且名称匹配到多个任务；请手动补全 MissionId。`,
            );
            return null;
        }
    }

    if (!route.MissionId) {
        console.warn(
            `[EnvironmentMonitoring] routes.json 条目 ${route.Name || route.Id || "<unknown>"} 缺少 MissionId，且无法通过 Name/Id 自动匹配。`,
        );
    }
    return null;
}

function buildSyncedRoute(route, mission, idByMissionId) {
    if (!mission) {
        return route;
    }

    const synced = {
        MissionId: mission.missionId,
        Name: mission.name?.["zh-CN"] || route.Name || mission.missionId,
        Id: idByMissionId.get(mission.missionId) || route.Id || mission.missionId,
    };

    for (const [
        key,
        value,
    ] of Object.entries(route)) {
        if (!ROUTE_METADATA_KEYS.has(key)) {
            synced[key] = value;
        }
    }

    return synced;
}

function compareRoutes(a, b) {
    const aKey = a.MissionId || `~${a.Name || ""}`;
    const bKey = b.MissionId || `~${b.Name || ""}`;
    return String(aKey).localeCompare(String(bKey), undefined, {numeric: true});
}

function appendMissingMissionRoutes(routes, missions, idByMissionId) {
    const routeMissionIds = new Set(routes.map((route) => route.MissionId).filter(Boolean));
    const missingRoutes = [];

    for (const mission of missions) {
        if (routeMissionIds.has(mission.missionId)) {
            continue;
        }
        missingRoutes.push(
            buildSyncedRoute(
                {
                    MissionId: mission.missionId,
                },
                mission,
                idByMissionId,
            ),
        );
    }

    if (missingRoutes.length > 0) {
        console.warn(
            `[EnvironmentMonitoring] routes.json 缺少 ${missingRoutes.length} 个 zmdmap 任务条目，已追加仅含 MissionId/Name/Id 的未适配占位条目。`,
        );
    }

    return routes.concat(missingRoutes);
}

export function syncRouteConfig() {
    if (!existsSync(ROUTES_PATH) || !existsSync(KITE_STATION_DATA_PATH)) {
        return;
    }

    const originalText = readFileSync(ROUTES_PATH, "utf8");
    const routes = JSON.parse(originalText);
    const missions = collectMonitoringMissions(readJson(KITE_STATION_DATA_PATH));
    const idByMissionId = buildGeneratedIdIndex(missions);
    const index = buildMissionSearchIndex(missions, idByMissionId);
    const syncedRoutes = appendMissingMissionRoutes(
        routes.map((route) => buildSyncedRoute(route, findMissionForRoute(route, index), idByMissionId)),
        missions,
        idByMissionId,
    ).sort(compareRoutes);
    const syncedText = `${JSON.stringify(syncedRoutes, null, 4)}\n`;

    if (syncedText !== originalText.replace(/\r\n/g, "\n")) {
        writeFileSync(ROUTES_PATH, syncedText, "utf8");
        console.log("[EnvironmentMonitoring] 已同步 routes.json 的 MissionId/Name/Id 并按 MissionId 排序。");
    }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
    syncRouteConfig();
}
