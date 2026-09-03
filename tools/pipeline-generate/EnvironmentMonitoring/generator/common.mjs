import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {readJsonc} from "../../jsonc.mjs";
import {dataDir} from "../../utils/paths.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));

export const ENVIRONMENT_MONITORING_DATA_PATH = resolve(dataDir, "environment_monitoring.json");
export const ROUTES_PATH = resolve(__dirname, "..", "routes.json");

// 与环境监测数据中的 names/shot_target_names locale 列表保持一致。
export const LOCALES = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];

export function readJson(path) {
    return readJsonc(path);
}

export function readEnvironmentMonitoringData() {
    return readJson(ENVIRONMENT_MONITORING_DATA_PATH);
}

export function buildMonitoringTerminalIds(environmentMonitoringData) {
    return Object.keys(environmentMonitoringData?.terminals || {})
        .filter((terminalId) => environmentMonitoringData.terminals[terminalId]?.missions?.length > 0)
        .sort();
}

export function collectMonitoringMissions(environmentMonitoringData) {
    const missions = [];
    const terminalIds = buildMonitoringTerminalIds(environmentMonitoringData);

    for (const terminalId of terminalIds) {
        const terminal = environmentMonitoringData.terminals[terminalId];
        for (const mission of terminal.missions || []) {
            if (mission?.mission_id && mission?.names?.zh_cn) {
                missions.push({
                    missionId: mission.mission_id,
                    entrustIdx: mission.entrust_index,
                    kiteStation: terminalId,
                    name: mission.names,
                    shotTargetName: mission.shot_target_names,
                    __terminalId: terminalId,
                });
            }
        }
    }

    return missions.sort((a, b) => {
        if (a.__terminalId !== b.__terminalId) {
            return a.__terminalId.localeCompare(b.__terminalId);
        }
        return (a.entrustIdx || 0) - (b.entrustIdx || 0);
    });
}

export function toPascalCase(str) {
    const cleaned = String(str || "")
        .replace(/[^a-zA-Z0-9]+/g, " ")
        .trim();
    if (!cleaned) {
        return "";
    }
    return cleaned
        .split(/\s+/)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join("");
}

export function sanitizeDisplayName(name) {
    return String(name || "")
        .replace(/["“”'‘’「」『』《》【】（）()]/g, "")
        .trim();
}

export function buildDefaultId(mission) {
    const fromEnglish = toPascalCase(mission?.name?.en_us);
    if (fromEnglish) {
        return fromEnglish;
    }
    const fromMissionId = toPascalCase(mission?.missionId);
    if (fromMissionId) {
        return `Mission${fromMissionId}`;
    }
    return `Mission${mission?.entrustIdx || "Unknown"}`;
}

export function ensureUniqueId(baseId, usedIds, missionId) {
    // 优先用 missionId 作为冲突后缀，保证 ID 在不同任务间稳定可读。
    // 若仍然撞名（极少见，例如 missionId 也重复），再退化到自增序号兜底。
    if (!usedIds.has(baseId)) {
        usedIds.add(baseId);
        return baseId;
    }
    if (missionId) {
        const withMissionId = `${baseId}_${missionId}`;
        if (!usedIds.has(withMissionId)) {
            usedIds.add(withMissionId);
            return withMissionId;
        }
    }
    let seq = 2;
    let nextId = `${baseId}_${seq}`;
    while (usedIds.has(nextId)) {
        seq += 1;
        nextId = `${baseId}_${seq}`;
    }
    usedIds.add(nextId);
    return nextId;
}

export function buildGeneratedIdIndex(missions) {
    const usedIds = new Set();
    const idByMissionId = new Map();

    for (const mission of missions) {
        idByMissionId.set(mission.missionId, ensureUniqueId(buildDefaultId(mission), usedIds, mission.missionId));
    }

    return idByMissionId;
}

export function buildStationId(environmentMonitoringData, terminalId) {
    const enName = environmentMonitoringData?.terminals?.[terminalId]?.names?.en_us;
    return toPascalCase(enName || terminalId) || terminalId;
}

export function buildStationDisplayName(environmentMonitoringData, terminalId) {
    return environmentMonitoringData?.terminals?.[terminalId]?.names?.zh_cn || terminalId;
}

export function isFieldMissing(value) {
    // null / undefined / 空字符串 / 空数组均视为缺失。
    if (value === undefined || value === null) {
        return true;
    }
    if (typeof value === "string" && value.trim() === "") {
        return true;
    }
    if (Array.isArray(value) && value.length === 0) {
        return true;
    }
    return false;
}
