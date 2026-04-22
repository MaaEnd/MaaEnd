import { createRequire } from "module";
import { ROUTE_CONFIG, ROUTE_DEFAULTS } from "./routes.mjs";

const require = createRequire(import.meta.url);
const kiteStationData = require("./kite_station.json");

// 监测终端 ID 列表直接从 kite_station.json 派生：凡是带有 entrustTasks 的条目都算。
// 上游游戏数据若新增监测终端会自动包含。新终端要真正可用，仍需手动补 Pipeline 侧的联动节点：
//   - assets/resource/pipeline/EnvironmentMonitoring/Locations.json：
//     新增 EnvironmentMonitoringGoTo{Station}MonitoringTerminal 与 EnvironmentMonitoringSelect{Station}MonitoringTerminal 节点
//   - assets/resource/pipeline/EnvironmentMonitoring.json 的 EnvironmentMonitoringLoop.next：
//     加入 [JumpBack]{Station}MonitoringTerminal
//   - 如有新文本识别节点（EnvironmentMonitoringCheck{Station}MonitoringTerminalText 等），手写补齐
// 上述节点缺失时，生成出来的 Pipeline 会引用未定义任务，MaaFramework 会在运行时报错——这是正确的失败模式。
export const MONITORING_TERMINAL_IDS = Object.keys(kiteStationData)
    .filter(
        (terminalId) =>
            Object.keys(kiteStationData[terminalId]?.entrustTasks?.list || {}).length > 0,
    )
    .sort();
const LOCALES = ["zh-CN", "zh-TW", "en-US", "ja-JP", "ko-KR"];

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

function normalizeMissionName(name) {
    return String(name || "")
        .replace(/[\s"“”'‘’「」『』《》【】（）()，,。.!！？?：:；;]/g, "")
        .toLowerCase();
}

function sanitizeDisplayName(name) {
    return String(name || "")
        .replace(/["“”'‘’「」『』《》【】（）()]/g, "")
        .trim();
}

function toPascalCase(str) {
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

function buildDefaultId(mission) {
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

function ensureUniqueId(baseId, usedIds, missionId) {
    let nextId = baseId;
    let seq = 2;
    while (usedIds.has(nextId)) {
        const suffix = missionId ? `_${missionId}` : `_${seq}`;
        nextId = `${baseId}${suffix}`;
        seq += 1;
    }
    usedIds.add(nextId);
    return nextId;
}

function collectMonitoringMissions() {
    const missions = [];

    for (const terminalId of MONITORING_TERMINAL_IDS) {
        const terminal = kiteStationData[terminalId];
        if (!terminal) {
            continue;
        }

        const missionList = terminal.entrustTasks?.list || {};
        for (const mission of Object.values(missionList)) {
            const nameZhCN = mission?.name?.["zh-CN"];
            if (!nameZhCN) {
                continue;
            }
            missions.push({
                ...mission,
                __terminalId: terminalId,
            });
        }
    }

    return missions.sort((a, b) => {
        if (a.__terminalId !== b.__terminalId) {
            return a.__terminalId.localeCompare(b.__terminalId);
        }
        return (a.entrustIdx || 0) - (b.entrustIdx || 0);
    });
}

const ROUTE_OVERRIDE_BY_NAME = new Map(
    ROUTE_CONFIG.map((item) => [normalizeMissionName(item.Name), item]),
);

function buildStationName(terminalId) {
    const stationEnglishName = kiteStationData?.[terminalId]?.level?.name?.["en-US"];
    return toPascalCase(stationEnglishName || terminalId) || terminalId;
}

function buildGoToMonitoringTerminal(station) {
    // Locations.json 中节点统一遵循 EnvironmentMonitoringGoTo{Station} 命名，
    // 所以这里直接拼，不维护硬编码白名单。新终端节点需要在 Locations.json 手写补齐。
    return `EnvironmentMonitoringGoTo${station}`;
}

function buildRow(mission, usedIds) {
    const missionName = mission?.name?.["zh-CN"] || mission?.missionId || "UnknownMission";
    const override = ROUTE_OVERRIDE_BY_NAME.get(normalizeMissionName(missionName));

    const missingFields = [];
    const EnterMap = override?.EnterMap ?? ROUTE_DEFAULTS.EnterMap;
    if (!override?.EnterMap) missingFields.push("EnterMap");
    const MapName = override?.MapName ?? ROUTE_DEFAULTS.MapName;
    if (!override?.MapName) missingFields.push("MapName");
    const MapTarget = override?.MapTarget ?? ROUTE_DEFAULTS.MapTarget;
    if (!override?.MapTarget) missingFields.push("MapTarget");
    const MapPath = override?.MapPath ?? ROUTE_DEFAULTS.MapPath;
    if (!override?.MapPath) missingFields.push("MapPath");
    const CameraSwipeDirection =
        override?.CameraSwipeDirection ?? ROUTE_DEFAULTS.CameraSwipeDirection;
    if (!override?.CameraSwipeDirection) missingFields.push("CameraSwipeDirection");
    const CameraMaxHit = override?.CameraMaxHit ?? ROUTE_DEFAULTS.CameraMaxHit;

    if (missingFields.length > 0) {
        console.warn(
            `[EnvironmentMonitoring] 任务 ${sanitizeDisplayName(missionName)} (${mission.missionId}) 缺少路线配置字段: ${missingFields.join(", ")}。已使用默认值，请补全 ROUTE_CONFIG。`,
        );
    }

    const baseId = override?.Id || buildDefaultId(mission);
    const Id = ensureUniqueId(baseId, usedIds, mission?.missionId);
    const Station = buildStationName(mission?.kiteStation || mission?.__terminalId);
    const GoToMonitoringTerminal = buildGoToMonitoringTerminal(Station);

    // 判断任务是否已适配路线：ROUTE_CONFIG 中无条目或使用了占位值的视为未适配
    const isAdapted =
        override != null &&
        EnterMap !== ROUTE_DEFAULTS.EnterMap &&
        MapName !== ROUTE_DEFAULTS.MapName &&
        JSON.stringify(MapTarget) !== JSON.stringify(ROUTE_DEFAULTS.MapTarget) &&
        JSON.stringify(MapPath) !== JSON.stringify(ROUTE_DEFAULTS.MapPath);

    if (!isAdapted) {
        console.warn(
            `[EnvironmentMonitoring] 任务 ${sanitizeDisplayName(missionName)} (${mission.missionId}) 尚未适配路线，仅接取并追踪。`,
        );
    }

    // 已适配：追踪后前往任务地点；未适配：仅接取并追踪，不前往
    const TrackOrGoToNext = isAdapted
        ? [`Track${Id}`, `GoTo${Id}`]
        : [`Track${Id}`];
    const TrackNext = isAdapted ? [`GoTo${Id}`] : [`${Id}NotAdapted`];

    return {
        Station,
        Id,
        Name: sanitizeDisplayName(missionName),
        GoToMonitoringTerminal,
        EnterMap,
        MapName,
        MapTarget,
        MapPath,
        CameraSwipeDirection,
        CameraMaxHit,
        ExpectedText: buildExpectedFromLocaleMap(mission.name),
        InExpectedText: buildExpectedFromLocaleMap(mission.shotTargetName),
        TrackOrGoToNext,
        TrackNext,
    };
}

const usedIds = new Set();
const rows = collectMonitoringMissions().map((mission) => buildRow(mission, usedIds));

export default rows;
