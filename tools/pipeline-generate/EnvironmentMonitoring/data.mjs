import {createRequire} from "module";
import {ROUTE_CONFIG, ROUTE_DEFAULTS} from "./routes.mjs";

const require = createRequire(import.meta.url);
const kiteStationData = require("./kite_station.json");

// 监测终端 ID 列表直接从 kite_station.json 派生：凡是带有 entrustTasks 的条目都算。
// 上游游戏数据若新增监测终端会自动包含；新终端要真正可用还需手动补 Pipeline 侧的联动节点
// （Locations.json / EnvironmentMonitoringLoop.next 等），详见 docs 维护手册。
export const MONITORING_TERMINAL_IDS = Object.keys(kiteStationData)
    .filter((terminalId) => Object.keys(kiteStationData[terminalId]?.entrustTasks?.list || {}).length > 0)
    .sort();
// 与 kite_station.json 中 name/shotTargetName 提供的 locale 列表保持一致；上游若新增语言需同步在这里补上。
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
    if (!stationEnglishName) {
        // 没匹配到游戏数据时通常意味着 mission.kiteStation 与 kite_station.json 主键脱节，
        // 直接 PascalCase terminalId 容易得到中文/纯数字串这种诡异结果。打个 warn 让维护者尽早发现。
        console.warn(
            `[EnvironmentMonitoring] 找不到 ${terminalId} 对应的英文站点名，已退化使用 terminalId。请检查 kite_station.json 是否同步。`,
        );
    }
    return toPascalCase(stationEnglishName || terminalId) || terminalId;
}

function buildGoToMonitoringTerminal(station) {
    // Locations.json 中节点统一遵循 EnvironmentMonitoringGoTo{Station} 命名，新终端在 Locations.json 手写补齐。
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

function isFieldMissing(value) {
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

function buildRow(mission, usedIds) {
    const missionName = mission?.name?.["zh-CN"] || mission?.missionId || "UnknownMission";
    const override = ROUTE_OVERRIDE_BY_NAME.get(normalizeMissionName(missionName));

    const resolved = {};
    const missingFields = [];
    for (const key of REQUIRED_ROUTE_FIELDS) {
        const overrideValue = override?.[key];
        if (isFieldMissing(overrideValue)) {
            missingFields.push(key);
            resolved[key] = ROUTE_DEFAULTS[key];
        } else {
            resolved[key] = overrideValue;
        }
    }
    const {EnterMap, MapName, MapTarget, MapPath, CameraSwipeDirection} = resolved;
    const CameraMaxHit = override?.CameraMaxHit ?? ROUTE_DEFAULTS.CameraMaxHit;

    if (override != null && missingFields.length > 0) {
        console.warn(
            `[EnvironmentMonitoring] 任务 ${sanitizeDisplayName(missionName)} (${mission.missionId}) 路线条目缺失字段: ${missingFields.join(", ")}。已使用默认值，请补全 routes.json。`,
        );
    }

    const baseId = override?.Id || buildDefaultId(mission);
    const Id = ensureUniqueId(baseId, usedIds, mission?.missionId);
    const Station = buildStationName(mission?.kiteStation || mission?.__terminalId);
    const GoToMonitoringTerminal = buildGoToMonitoringTerminal(Station);

    // routes.json 中无条目、或条目缺失任一必填字段，均视为未适配。
    const isAdapted = override != null && missingFields.length === 0;

    if (!isAdapted) {
        console.warn(
            `[EnvironmentMonitoring] 任务 ${sanitizeDisplayName(missionName)} (${mission.missionId}) 尚未适配路线，仅接取并追踪。`,
        );
    }

    // 游戏内未追踪时无法完成任务，已适配点也要先走追踪确认。
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
