import {existsSync, readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
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

const INTERFACE_LOCALES = {
    "zh-CN": "zh_cn",
    "zh-TW": "zh_tw",
    "en-US": "en_us",
    "ja-JP": "ja_jp",
    "ko-KR": "ko_kr",
};

const FAILURE_MESSAGE_SUFFIXES = {
    "zh-CN": "任务失败",
    "zh-TW": "任務失敗",
    "en-US": " task failed",
    "ja-JP": "：タスク失敗",
    "ko-KR": " 작업 실패",
};

function buildFailureMessage(name, locale) {
    return `<span style="color: red; font-weight: bold;">${name}${FAILURE_MESSAGE_SUFFIXES[locale]}</span>`;
}

function validateRouteLocaleCatalog(messages, failureMessages, missionCount, routeKeyPrefix, fileLocale) {
    const messageKeys = Object.keys(messages);
    const expectedFailureKeys = Object.keys(failureMessages);
    const actualRouteKeys = messageKeys.filter(
        (key) => key.startsWith(routeKeyPrefix) && (key.endsWith(".label") || key.endsWith(".failed")),
    );
    const routeStart = messageKeys.indexOf("task.EnvironmentMonitoring.label") + 1;
    const groupedFailureKeys = messageKeys.slice(routeStart, routeStart + expectedFailureKeys.length);

    if (
        routeStart === 0 ||
        expectedFailureKeys.length !== missionCount ||
        actualRouteKeys.join("\n") !== expectedFailureKeys.join("\n") ||
        groupedFailureKeys.join("\n") !== expectedFailureKeys.join("\n")
    ) {
        throw new Error(`[EnvironmentMonitoring] ${fileLocale}.json 的路线失败提示不完整或未集中排列。`);
    }
}

function syncRouteLocaleCatalogs(missions, idByMissionId) {
    const localeDir = resolve(dirname(ROUTES_PATH), "../../../assets/locales/interface");
    const routeKeyPrefix = "task.EnvironmentMonitoring.route.";
    for (const [
        sourceLocale,
        fileLocale,
    ] of Object.entries(INTERFACE_LOCALES)) {
        const localePath = resolve(localeDir, `${fileLocale}.json`);
        const originalText = readFileSync(localePath, "utf8");
        const messages = JSON.parse(originalText);
        const failureMessages = {};
        const sortedMissions = [...missions].sort((left, right) => {
            const leftId = idByMissionId.get(left.missionId);
            const rightId = idByMissionId.get(right.missionId);
            return leftId.localeCompare(rightId);
        });
        for (const mission of sortedMissions) {
            const id = idByMissionId.get(mission.missionId);
            const value = mission.name?.[sourceLocale] || mission.name?.["zh-CN"] || mission.missionId;
            const failureKey = `${routeKeyPrefix}${id}.failed`;
            failureMessages[failureKey] = messages[failureKey] ?? buildFailureMessage(value, sourceLocale);
        }

        const syncedMessages = {};
        let routesInserted = false;
        for (const [
            key,
            value,
        ] of Object.entries(messages)) {
            if (key.startsWith(routeKeyPrefix) && (key.endsWith(".label") || key.endsWith(".failed"))) {
                continue;
            }
            syncedMessages[key] = value;
            if (key === "task.EnvironmentMonitoring.label") {
                Object.assign(syncedMessages, failureMessages);
                routesInserted = true;
            }
        }
        if (!routesInserted) {
            Object.assign(syncedMessages, failureMessages);
        }
        validateRouteLocaleCatalog(syncedMessages, failureMessages, missions.length, routeKeyPrefix, fileLocale);
        const syncedText = `${JSON.stringify(syncedMessages, null, 4)}\n`;
        if (syncedText !== originalText.replace(/\r\n/g, "\n")) {
            writeFileSync(localePath, syncedText, "utf8");
            console.log(`[EnvironmentMonitoring] 已同步 ${fileLocale}.json 的路线失败提示。`);
        }
    }
}

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
    syncRouteLocaleCatalogs(missions, idByMissionId);
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
