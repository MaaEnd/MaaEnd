import {readFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {dataDir} from "../../utils/paths.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));

export const KITE_STATION_DATA_PATH = resolve(dataDir, "kite_station_i18n.json");
export const ROUTES_PATH = resolve(__dirname, "..", "routes.json");

// 与 zmdmap 数据中 name/shotTargetName 提供的 locale 列表保持一致；上游若新增语言需同步在这里补上。
export const LOCALES = [
    "zh-CN",
    "zh-TW",
    "en-US",
    "ja-JP",
    "ko-KR",
];

export function readJson(path) {
    return JSON.parse(readFileSync(path, "utf8"));
}

export function readKiteStationData() {
    return readJson(KITE_STATION_DATA_PATH);
}

export function buildMonitoringTerminalIds(kiteStationData) {
    return Object.keys(kiteStationData)
        .filter((terminalId) => Object.keys(kiteStationData[terminalId]?.entrustTasks?.list || {}).length > 0)
        .sort();
}

export function collectMonitoringMissions(kiteStationData) {
    const missions = [];
    const terminalIds = buildMonitoringTerminalIds(kiteStationData);

    for (const terminalId of terminalIds) {
        const terminal = kiteStationData[terminalId];
        for (const mission of Object.values(terminal?.entrustTasks?.list || {})) {
            if (mission?.missionId && mission?.name?.["zh-CN"]) {
                missions.push({
                    ...mission,
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
    const fromEnglish = toPascalCase(mission?.name?.["en-US"]);
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

export function buildStationId(kiteStationData, terminalId) {
    const enName = kiteStationData?.[terminalId]?.level?.name?.["en-US"];
    return toPascalCase(enName || terminalId) || terminalId;
}

export function buildStationDisplayName(kiteStationData, terminalId) {
    return kiteStationData?.[terminalId]?.level?.name?.["zh-CN"] || terminalId;
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
