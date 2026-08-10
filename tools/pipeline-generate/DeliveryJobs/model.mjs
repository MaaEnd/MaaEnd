import {readFileSync} from "node:fs";

const INTERFACE_LOCALES = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];

const deliveryJobsData = JSON.parse(readFileSync(new URL("../data/delivery_jobs.json", import.meta.url), "utf8"));

const FILL_ITEM_GAME_IDS = {
    SandleafPowder: "item_plant_moss_powder_3",
    BuckflowerPowder: "item_plant_moss_powder_1",
    CitromePowder: "item_plant_moss_powder_2",
    AketinePowder: "item_plant_bbflower_powder_1",
    YazhenPowder: "item_plant_grass_powder_2",
    JincaoPowder: "item_plant_grass_powder_1",
    Yazhen: "item_plant_grass_2",
    Jincao: "item_plant_grass_1",
    Xiranite: "item_xiranite_powder",
};

const DELIVERY_JOB_REGIONS = [
    {
        Id: "ValleyIV",
        GameId: "domain_1",
        RegionScene: "SceneEnterMenuRegionalDevelopmentValleyIV",
        DepotScene: "SceneEnterMenuRegionalDevelopmentValleyIVDepotNode",
        Depots: [
            {
                Id: "OriginiumSciencePark",
                GameId: "domain_1_lv005_depot_1",
            },
            {
                Id: "OriginLodespring",
                GameId: "domain_1_lv006_depot_1",
            },
            {
                Id: "PowerPlateau",
                GameId: "domain_1_lv007_depot_1",
            },
        ],
        FillItems: [
            "SandleafPowder",
            "BuckflowerPowder",
            "CitromePowder",
            "AketinePowder",
        ],
        DefaultFillItem: "SandleafPowder",
    },
    {
        Id: "Wuling",
        GameId: "domain_2",
        RegionScene: "SceneEnterMenuRegionalDevelopmentWuling",
        DepotScene: "SceneEnterMenuRegionalDevelopmentWulingDepotNode",
        Depots: [
            {
                Id: "WulingCity",
                GameId: "domain_2_lv002_depot_1",
            },
            {
                Id: "TestArea",
                GameId: "domain_2_lv005_depot_1",
            },
        ],
        FillItems: [
            "SandleafPowder",
            "BuckflowerPowder",
            "CitromePowder",
            "AketinePowder",
            "YazhenPowder",
            "JincaoPowder",
            "Yazhen",
            "Jincao",
            "Xiranite",
        ],
        DefaultFillItem: "SandleafPowder",
    },
];

function assertRecord(value, label) {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
        throw new Error(`[DeliveryJobs] ${label} 不是对象`);
    }
    return value;
}

function assertUnique(values, label) {
    if (new Set(values).size !== values.length) {
        throw new Error(`[DeliveryJobs] ${label} 存在重复项`);
    }
}

function escapeRegex(text) {
    return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function toFlexibleEnglishRegex(text) {
    return `(?i)${escapeRegex(text.trim()).replace(/\s+/g, "\\s*")}`;
}

function validateLocalizedNames(names, label) {
    for (const locale of INTERFACE_LOCALES) {
        if (typeof names?.[locale] !== "string" || names[locale].length === 0) {
            throw new Error(`[DeliveryJobs] ${label} 缺少 ${locale} 名称`);
        }
    }
    return names;
}

function buildLocalizedExpected(names, label) {
    validateLocalizedNames(names, label);
    const values = INTERFACE_LOCALES.map((locale) =>
        locale === "en_us" ? toFlexibleEnglishRegex(names[locale]) : names[locale],
    );
    return [...new Set(values)];
}

function validateData() {
    assertRecord(deliveryJobsData.regions, "delivery_jobs.json regions");
    assertRecord(deliveryJobsData.depots, "delivery_jobs.json depots");
    assertRecord(deliveryJobsData.items, "delivery_jobs.json items");
    assertUnique(
        DELIVERY_JOB_REGIONS.map((region) => region.Id),
        "地区 ID",
    );
    assertUnique(
        DELIVERY_JOB_REGIONS.flatMap((region) => region.Depots.map((depot) => depot.Id)),
        "仓储节点 ID",
    );
}

validateData();

function getConfiguredItem(id) {
    const gameID = FILL_ITEM_GAME_IDS[id];
    if (!gameID) throw new Error(`[DeliveryJobs] 未配置物品 ${id} 的游戏 ID`);
    const item = deliveryJobsData.items[gameID];
    if (!item) throw new Error(`[DeliveryJobs] delivery_jobs.json 缺少物品 ${gameID}`);
    validateLocalizedNames(item.names, `物品 ${gameID}`);
    return {
        id,
        gameID,
        item,
    };
}

function buildFillItem(id) {
    const {item} = getConfiguredItem(id);
    return {
        Id: id,
        Name: item.names.zh_cn,
        Label: `$item.${id}`,
        Template: `DeliveryJobs/${id}.png`,
    };
}

function buildDepot(regionSettings, depotSettings, configuredItems) {
    const depot = deliveryJobsData.depots[depotSettings.GameId];
    if (!depot) throw new Error(`[DeliveryJobs] delivery_jobs.json 缺少仓储节点 ${depotSettings.GameId}`);
    if (depot.region_id !== regionSettings.GameId) {
        throw new Error(
            `[DeliveryJobs] 仓储节点 ${depotSettings.GameId} 所属地区应为 ${regionSettings.GameId}，实际为 ${depot.region_id}`,
        );
    }
    validateLocalizedNames(depot.names, `仓储节点 ${depotSettings.GameId}`);
    for (const item of configuredItems) {
        if (!depot.fillable_items.includes(item.gameID)) {
            throw new Error(`[DeliveryJobs] 仓储节点 ${depotSettings.GameId} 不能装箱物品 ${item.gameID}`);
        }
    }
    return {
        Id: depotSettings.Id,
        Name: depot.names.zh_cn,
        Expected: buildLocalizedExpected(depot.names, `仓储节点 ${depotSettings.GameId}`),
        RegionId: regionSettings.Id,
        RegionScene: regionSettings.RegionScene,
        DepotScene: regionSettings.DepotScene,
    };
}

const configuredRegions = DELIVERY_JOB_REGIONS.map((settings) => {
    const region = deliveryJobsData.regions[settings.GameId];
    if (!region) throw new Error(`[DeliveryJobs] delivery_jobs.json 缺少地区 ${settings.GameId}`);
    validateLocalizedNames(region.names, `地区 ${settings.GameId}`);
    const configuredItems = settings.FillItems.map(getConfiguredItem);
    if (!settings.FillItems.includes(settings.DefaultFillItem)) {
        throw new Error(`[DeliveryJobs] 地区 ${settings.Id} 的默认装箱物品不在 FillItems 中`);
    }
    return {
        settings,
        source: region,
        depots: settings.Depots.map((depotSettings) => buildDepot(settings, depotSettings, configuredItems)),
    };
});

export const deliveryJobRegions = configuredRegions.map(({settings, source, depots}) => ({
    Id: settings.Id,
    Name: source.names.zh_cn,
    RegionScene: settings.RegionScene,
    DepotScene: settings.DepotScene,
    Depots: depots.map((depot) => depot.Id),
    FillItems: settings.FillItems.map(buildFillItem),
    DefaultFillItem: settings.DefaultFillItem,
}));

export const deliveryJobDepots = configuredRegions.flatMap(({source, depots}) =>
    depots.map((depot) => ({
        ...depot,
        RegionName: source.names.zh_cn,
    })),
);

export function rawJson(value) {
    return {
        value,
        raw: JSON.stringify(value, null, 4),
    };
}
