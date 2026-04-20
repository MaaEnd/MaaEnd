import { createRequire } from "module";

const require = createRequire(import.meta.url);
const kiteStationData = require("./kite_station.json");

export const MONITORING_TERMINAL_IDS = ["kitestation_002_1", "kitestation_004_1"];
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

const ALL_MONITORING_MISSIONS = collectMonitoringMissions();

const ROUTE_CONFIG = [
    {
        Id: "AncientTree",
        Name: "古树",
        EnterMap: "SceneEnterWorldWulingJingyuValley7",
        MapName: "map02_lv001",
        MapTarget: [280, 580, 15, 15],
        MapPath: [
            [288, 587],
            [296, 577],
            [301, 572],
            [303, 565],
            [303, 552],
            [306, 545],
            [315, 542],
            [329, 540],
            [339, 541],
            [345, 536],
            [356, 534],
            [368, 531],
            [374, 539],
            [369, 547],
            [368, 556],
            [363, 564],
            [355, 570],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "BeaconDamagedInBlightTide",
        Name: "侵蚀潮中损坏的信标",
        EnterMap: "SceneEnterWorldWulingWulingCity0",
        MapName: "map02_lv002",
        MapTarget: [640, 255, 15, 15],
        MapPath: [
            [641, 272],
            [639, 299],
            [628, 301],
            [623, 302],
            [641, 317],
            [648, 317],
            [685, 318],
            [686, 329],
            [680, 333],
            [691, 344],
            [701, 345],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenDown",
    },
    {
        Id: "CisternOriginiumSlugs",
        Name: "蓄水源石虫",
        EnterMap: "SceneEnterWorldWulingJingyuValley7",
        MapName: "map02_lv001",
        MapTarget: [280, 580, 15, 15],
        MapPath: [
            [291, 582],
            [298, 576],
            [306, 578],
            [309, 593],
            [318, 593],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenRight",
    },
    {
        Id: "CleansingJade",
        Name: "漱玉",
        EnterMap: "SceneEnterWorldWulingJingyuValley4",
        MapName: "map02_lv001_tier_277",
        MapTarget: [255, 395, 15, 15],
        MapPath: [[246, 394]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "CollapsedTianshiPillar",
        Name: "倒塌的天师桩",
        EnterMap: "SceneEnterWorldWulingJingyuValley8",
        MapName: "map02_lv001",
        MapTarget: [210, 635, 15, 15],
        MapPath: [
            [213, 632],
            [191, 616],
            [186, 621],
            [180, 618],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "EternalSunset",
        Name: "栖霞驻影",
        EnterMap: "SceneEnterWorldWulingJingyuValley2",
        MapName: "map02_lv001",
        MapTarget: [315, 305, 15, 15],
        MapPath: [
            [332, 303],
            [346, 296],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "IndoorCrops",
        Name: "室内作物",
        EnterMap: "SceneEnterWorldWulingWulingCity4",
        MapName: "map02_lv002",
        MapTarget: [240, 695, 15, 15],
        MapPath: [
            [237, 703],
            [219, 705],
            [214, 716],
            [211, 727],
            [198, 729],
            [195, 721],
            [187, 721],
            [190, 729],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "RainbowFin",
        Name: "七彩鳞",
        EnterMap: "SceneEnterWorldWulingJingyuValley10",
        MapName: "map02_lv001",
        MapTarget: [125, 815, 15, 15],
        MapPath: [
            [131, 820],
            [142, 818],
            [151, 813],
            [163, 808],
            [172, 799],
            [188, 789],
            [194, 765],
            [205, 755],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenDown",
    },
    {
        Id: "EcologyNearTheFieldLogisticsDepot",
        Name: "储备站周围的生态环境",
        EnterMap: "SceneEnterWorldWulingWulingCity8",
        MapName: "map02_lv002",
        MapTarget: [689.6, 917.3, 6.0, 5.5],
        MapPath: [
            [691.3, 922.7],
            [678.3, 925.9],
            [672.5, 918.3],
            [648.4, 916.3],
            [648.7, 928.2],
            [653.1, 931.8],
            [666.8, 907.8],
            [664.6, 897.7],
            [659.9, 896.1],
            [651.9, 894.2],
            [627.7, 898.2],
            [617.8, 901.6],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenLeft",
    },
    {
        Id: "InflatedAndGlowingBugspittingLankybeast",
        Name: "肚子鼓气后发光的吐虫长股兽",
        EnterMap: "SceneEnterWorldWulingWulingCity7",
        MapName: "map02_lv002",
        MapTarget: [416.7, 844.3, 11.4, 10.6],
        MapPath: [
            [425.0, 851.2],
            [436.3, 842.5],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "ObservantSableChestedFowlbeast",
        Name: "东张西望的黑腹雉羽兽",
        EnterMap: "SceneEnterWorldWulingJingyuValley0",
        MapName: "map02_lv001",
        MapTarget: [342.2, 124.2, 4, 4],
        MapPath: [
            [349.9, 128.0],
            [373.3, 135.6],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenDown",
    },
    {
        Id: "VigorousCisternOriginiumSlug",
        Name: "充满活力的蓄水源石虫",
        EnterMap: "SceneEnterWorldWulingJingyuValley8",
        MapName: "map02_lv001",
        MapTarget: [210, 635, 15, 15],
        MapPath: [
            [213.0, 632.0],
            [216.0, 630.4],
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenRight",
    },
    {
        Id: "Waterlamp",
        Name: "水灯虫",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "XiraniteNexus",
        Name: "枢壤仪",
        EnterMap: "SceneEnterWorldWulingWulingCity4",
        MapName: "map02_lv002",
        MapTarget: [240, 695, 15, 15],
        MapPath: [
            [241.3, 703.6],
            [215.2, 703.7],
            [213.6, 693.7],
            [195.1, 680.2],
            [183.7, 680.2],
            [177.6, 675.2],
            [170.8, 674.6]
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "PierWaterwheel",
        Name: "码头水车",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "MiniShellbeast",
        Name: "小壳兽",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "AnnularWaterfall",
        Name: "环形瀑布",
        EnterMap: "SceneEnterWorldWulingQingboStockade3",
        MapName: "map02_lv003",
        MapTarget: [502.2, 360.2, 13.2, 13.9],
        MapPath: [
            [498.8, 360.5],
            [501.1, 347.9],
            [506.4, 334.0],
            [507.4, 330.5]
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "BambooWaterChimes",
        Name: "水竹铃",
        EnterMap: "SceneEnterWorldWulingQingboStockade1",
        MapName: "map02_lv003",
        MapTarget: [300, 460, 15, 15],
        MapPath: [
            [291.7, 473.5],
            [269.9, 474.2],
            [269.6, 464.1],
            [268.8, 458.1]
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "WitheredBranches",
        Name: "枯枝",
        EnterMap: "SceneEnterWorldWulingQingboStockade3",
        MapName: "map02_lv003",
        MapTarget: [502.2, 360.2, 13.2, 13.9],
        MapPath: [
            [506.3, 378.0],
            [507.5, 379.2]
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "MysteriousCryptidGraffiti",
        Name: "谜之生物的涂鸦",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "TreeOfTheOldCourtyard",
        Name: "“旧庭树”",
        EnterMap: "SceneEnterWorldWulingMarkerStone1",
        MapName: "map02_lv004",
        MapTarget: [491.2, 197.9, 11.7, 10.7],
        MapPath: [
            [498.6,210.6],
            [519.5,210.5],
            [517.6,192.0],
            [535.2,191.6],
            [536.0,165.7],
            [541.2,188.0]
        ],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "HangingVines",
        Name: "垂藤",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "FloraObservationPoint",
        Name: "植物观察点",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "CloudrestEcoSite",
        Name: "栖云生态点",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "Rainbow",
        Name: "彩虹",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "PeachTreeAtBabblesSHome",
        Name: "念念家的桃树",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "WaterTemperatureController",
        Name: "净水温控装置",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
    {
        Id: "SerenityGardenTianshiPillar",
        Name: "安思园的天师桩",
        EnterMap: "SceneAnyEnterWorld",
        MapName: "^map\\d+_lv\\d+$",
        MapTarget: [0, 0, 1, 1],
        MapPath: [[0, 0]],
        CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    },
];

const ROUTE_OVERRIDE_BY_NAME = new Map(
    ROUTE_CONFIG.map((item) => [normalizeMissionName(item.Name), item]),
);

const ROUTE_DEFAULTS = {
    EnterMap: "SceneAnyEnterWorld",
    MapName: "^map\\d+_lv\\d+$",
    MapTarget: [0, 0, 1, 1],
    MapPath: [[0, 0]],
    CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
};

function buildStationName(terminalId) {
    const stationEnglishName = kiteStationData?.[terminalId]?.level?.name?.["en-US"];
    return toPascalCase(stationEnglishName || terminalId) || terminalId;
}

function buildGoToMonitoringTerminal(station) {
    if (station === "OutskirtsMonitoringTerminal") {
        return "EnvironmentMonitoringGoToOutskirtsMonitoringTerminal";
    }
    if (station === "MarkerStoneMonitoringTerminal") {
        return "EnvironmentMonitoringGoToMarkerStoneMonitoringTerminal";
    }
    console.warn(
        `[EnvironmentMonitoring] 未识别的监测终端分组: ${station}，已回退到城郊监测终端符号。`,
    );
    return "EnvironmentMonitoringGoToOutskirtsMonitoringTerminal";
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

    if (missingFields.length > 0) {
        console.warn(
            `[EnvironmentMonitoring] 任务 ${sanitizeDisplayName(missionName)} (${mission.missionId}) 缺少路线配置字段: ${missingFields.join(", ")}。已使用默认值，请补全 STATIC_ROUTE_CONFIG。`,
        );
    }

    const baseId = override?.Id || buildDefaultId(mission);
    const Id = ensureUniqueId(baseId, usedIds, mission?.missionId);
    const Station = buildStationName(mission?.kiteStation || mission?.__terminalId);
    const GoToMonitoringTerminal = buildGoToMonitoringTerminal(Station);

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
        ExpectedText: buildExpectedFromLocaleMap(mission.name),
        InExpectedText: buildExpectedFromLocaleMap(mission.shotTargetName),
    };
}

const usedIds = new Set();
const rows = ALL_MONITORING_MISSIONS.map((mission) => buildRow(mission, usedIds));

export default rows;
