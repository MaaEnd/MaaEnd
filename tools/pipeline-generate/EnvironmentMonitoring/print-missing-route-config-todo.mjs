import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const DATA_MJS_PATH = path.join(__dirname, "data.mjs");
const KITE_STATION_JSON_PATH = path.join(__dirname, "kite_station.json");
const MONITORING_TERMINAL_IDS = ["kitestation_002_1", "kitestation_004_1"];
const PLACEHOLDER_DEFAULTS = {
    EnterMap: "SceneAnyEnterWorld",
    MapName: "^map\\d+_lv\\d+$",
    MapTarget: [0, 0, 1, 1],
    MapPath: [[0, 0]],
    CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
};

function normalizeMissionName(name) {
    return String(name || "")
        .replace(/[\s"“”'‘’「」『』《》【】（）()，,。.!！？?：:；;]/g, "")
        .toLowerCase();
}

function extractRouteConfigNames(source) {
    const blockMatch = source.match(/const ROUTE_CONFIG = \[(.|\r|\n)*?\n\];/);
    if (!blockMatch) {
        throw new Error("未找到 ROUTE_CONFIG 定义块");
    }

    return [...blockMatch[0].matchAll(/\bName:\s*"([^"]+)"/g)].map((m) => m[1]);
}

function extractRouteConfigItems(source) {
    const blockMatch = source.match(/const ROUTE_CONFIG = \[(.|\r|\n)*?\n\];/);
    if (!blockMatch) {
        throw new Error("未找到 ROUTE_CONFIG 定义块");
    }

    const arrayExpr = blockMatch[0]
        .replace(/^const ROUTE_CONFIG = /, "")
        .replace(/;\s*$/, "");

    return Function(`"use strict"; return (${arrayExpr});`)();
}

function deepEqualArray(a, b) {
    if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) {
        return false;
    }
    for (let i = 0; i < a.length; i += 1) {
        const x = a[i];
        const y = b[i];
        if (Array.isArray(x) || Array.isArray(y)) {
            if (!deepEqualArray(x, y)) {
                return false;
            }
            continue;
        }
        if (x !== y) {
            return false;
        }
    }
    return true;
}

function detectPlaceholderFields(routeItem) {
    const pendingFields = [];

    if (routeItem?.EnterMap === PLACEHOLDER_DEFAULTS.EnterMap) {
        pendingFields.push("EnterMap");
    }
    if (routeItem?.MapName === PLACEHOLDER_DEFAULTS.MapName) {
        pendingFields.push("MapName");
    }
    if (deepEqualArray(routeItem?.MapTarget, PLACEHOLDER_DEFAULTS.MapTarget)) {
        pendingFields.push("MapTarget");
    }
    if (deepEqualArray(routeItem?.MapPath, PLACEHOLDER_DEFAULTS.MapPath)) {
        pendingFields.push("MapPath");
    }

    const coreFieldsAllPlaceholder =
        pendingFields.includes("EnterMap") &&
        pendingFields.includes("MapName") &&
        pendingFields.includes("MapTarget") &&
        pendingFields.includes("MapPath");

    if (
        coreFieldsAllPlaceholder &&
        routeItem?.CameraSwipeDirection === PLACEHOLDER_DEFAULTS.CameraSwipeDirection
    ) {
        pendingFields.push("CameraSwipeDirection");
    }

    return pendingFields;
}

function collectMonitoringMissions(kiteStationData) {
    const missions = [];

    for (const terminalId of MONITORING_TERMINAL_IDS) {
        const terminal = kiteStationData[terminalId];
        if (!terminal) {
            continue;
        }

        const stationNameZhCN =
            terminal?.level?.name?.["zh-CN"] ||
            terminal?.level?.name?.["zh-TW"] ||
            terminalId;

        const missionList = terminal?.entrustTasks?.list || {};
        for (const mission of Object.values(missionList)) {
            const missionNameZhCN = mission?.name?.["zh-CN"];
            if (!missionNameZhCN) {
                continue;
            }

            missions.push({
                terminalId,
                stationNameZhCN,
                missionId: mission?.missionId || "",
                entrustIdx: mission?.entrustIdx || 0,
                missionNameZhCN,
            });
        }
    }

    return missions.sort((a, b) => {
        if (a.terminalId !== b.terminalId) {
            return a.terminalId.localeCompare(b.terminalId);
        }
        return a.entrustIdx - b.entrustIdx;
    });
}

function groupByStation(missions) {
    const groups = [];
    const byTerminalId = new Map();

    for (const mission of missions) {
        let group = byTerminalId.get(mission.terminalId);
        if (!group) {
            group = {
                terminalId: mission.terminalId,
                stationNameZhCN: mission.stationNameZhCN,
                missions: [],
            };
            byTerminalId.set(mission.terminalId, group);
            groups.push(group);
        }

        group.missions.push(mission);
    }

    return groups;
}

function renderMarkdown(missingMissions) {
    const groups = groupByStation(missingMissions);
    const lines = [];

    lines.push(`### EnvironmentMonitoring 路线配置待补全（${missingMissions.length}项）`);
    lines.push("");

    for (const group of groups) {
        lines.push(`#### ${group.stationNameZhCN}（${group.terminalId}）`);
        for (const mission of group.missions) {
            lines.push(
                `- [ ] ${mission.missionNameZhCN}（missionId: ${mission.missionId}, entrustIdx: ${mission.entrustIdx}）`,
            );
        }
        lines.push("");
    }

    return lines.join("\n").trimEnd() + "\n";
}

function renderPendingMarkdown(pendingMissions) {
    const groups = groupByStation(pendingMissions);
    const lines = [];

    lines.push(`### EnvironmentMonitoring 待补全路线配置（${pendingMissions.length}项）`);
    lines.push("");

    for (const group of groups) {
        lines.push(`#### ${group.stationNameZhCN}（${group.terminalId}）`);
        for (const mission of group.missions) {
            const fields = mission.pendingFields.join(", ");
            lines.push(
                `- [ ] ${mission.missionNameZhCN}（missionId: ${mission.missionId}, entrustIdx: ${mission.entrustIdx}, 待补字段: ${fields}）`,
            );
        }
        lines.push("");
    }

    return lines.join("\n").trimEnd() + "\n";
}

function renderFullReportMarkdown({ completed, placeholders, missing }) {
    const lines = [];

    lines.push("### EnvironmentMonitoring 路线配置状态总览");
    lines.push("");
    lines.push(`- 已补全: ${completed.length}`);
    lines.push(`- 占位待补全: ${placeholders.length}`);
    lines.push(`- 缺失配置: ${missing.length}`);
    lines.push("");

    lines.push(renderPendingMarkdown([...placeholders, ...missing]).trimEnd());
    lines.push("");

    if (completed.length > 0) {
        lines.push(`### 已补全任务（${completed.length}项）`);
        lines.push("");
        const groups = groupByStation(completed);
        for (const group of groups) {
            lines.push(`#### ${group.stationNameZhCN}（${group.terminalId}）`);
            for (const mission of group.missions) {
                lines.push(
                    `- ${mission.missionNameZhCN}（missionId: ${mission.missionId}, entrustIdx: ${mission.entrustIdx}）`,
                );
            }
            lines.push("");
        }
    }

    return lines.join("\n").trimEnd() + "\n";
}

function main() {
    const source = fs.readFileSync(DATA_MJS_PATH, "utf8");
    const routeItems = extractRouteConfigItems(source);
    const routeMapByName = new Map(
        routeItems.map((item) => [normalizeMissionName(item?.Name), item]),
    );

    const kiteStationData = JSON.parse(fs.readFileSync(KITE_STATION_JSON_PATH, "utf8"));
    const allMissions = collectMonitoringMissions(kiteStationData);
    const completed = [];
    const placeholders = [];
    const missing = [];

    for (const mission of allMissions) {
        const key = normalizeMissionName(mission.missionNameZhCN);
        const routeItem = routeMapByName.get(key);

        if (!routeItem) {
            missing.push({
                ...mission,
                pendingFields: [
                    "Id",
                    "EnterMap",
                    "MapName",
                    "MapTarget",
                    "MapPath",
                    "CameraSwipeDirection",
                ],
            });
            continue;
        }

        const pendingFields = detectPlaceholderFields(routeItem);
        if (pendingFields.length > 0) {
            placeholders.push({
                ...mission,
                pendingFields,
            });
            continue;
        }

        completed.push(mission);
    }

    const showAll = process.argv.includes("--all");
    if (showAll) {
        process.stdout.write(renderFullReportMarkdown({ completed, placeholders, missing }));
        return;
    }

    process.stdout.write(renderPendingMarkdown([...placeholders, ...missing]));
}

main();
