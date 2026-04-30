import {createRequire} from "module";
import {ROUTE_CONFIG, ROUTE_DEFAULTS} from "./routes.mjs";

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
    .filter((terminalId) => Object.keys(kiteStationData[terminalId]?.entrustTasks?.list || {}).length > 0)
    .sort();
const LOCALES = [
    "zh-CN",
    "zh-TW",
    "en-US",
    "ja-JP",
    "ko-KR",
];

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
    // 优先用 missionId 作为冲突后缀，保证 ID 在不同任务间稳定可读；
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
    ROUTE_CONFIG.map((item) => [
        normalizeMissionName(item.Name),
        item,
    ]),
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

// 需要校验是否在 ROUTE_CONFIG 中显式配置的字段。CameraMaxHit 有合理默认值，未配置不视为缺失。
const REQUIRED_ROUTE_FIELDS = [
    "EnterMap",
    "MapName",
    "MapTarget",
    "MapPath",
    "CameraSwipeDirection",
];

function buildRow(mission, usedIds) {
    const missionName = mission?.name?.["zh-CN"] || mission?.missionId || "UnknownMission";
    const override = ROUTE_OVERRIDE_BY_NAME.get(normalizeMissionName(missionName));

    const resolved = {};
    const missingFields = [];
    for (const key of REQUIRED_ROUTE_FIELDS) {
        resolved[key] = override?.[key] ?? ROUTE_DEFAULTS[key];
        if (override?.[key] === undefined) {
            missingFields.push(key);
        }
    }
    const {EnterMap, MapName, MapTarget, MapPath, CameraSwipeDirection} = resolved;
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

    // 判断任务是否已适配路线：ROUTE_CONFIG 中无条目或缺少必填字段的视为未适配。
    // 直接看 override 是否显式提供字段，而不是和 ROUTE_DEFAULTS 字面量比较——
    // 后者在数组顺序/格式上稍有差异就会判错。
    const isAdapted = override != null && missingFields.length === 0;

    if (!isAdapted) {
        console.warn(
            `[EnvironmentMonitoring] 任务 ${sanitizeDisplayName(missionName)} (${mission.missionId}) 尚未适配路线，仅接取并追踪。`,
        );
    }

    // 先确认任务处于“开始追踪”或“已在追踪”状态，再决定后续是否前往。
    // 游戏内未追踪时无法完成任务，因此已适配点也不能直接跳过追踪确认。
    const TrackOrGoToNext = [
        `Track${Id}`,
        `AlreadyTracked${Id}`,
    ];
    const AfterTrackedNext = isAdapted ? [`GoTo${Id}`] : [`${Id}NotAdapted`];

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
        AfterTrackedNext,
    };
}

const usedIds = new Set();
const rows = collectMonitoringMissions().map((mission) => buildRow(mission, usedIds));

export default rows;
