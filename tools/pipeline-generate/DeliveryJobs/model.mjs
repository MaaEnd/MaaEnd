import {readJsonc} from "../jsonc.mjs";

const INTERFACE_LOCALES = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];

const deliveryJobsData = readJsonc(new URL("../data/delivery_jobs.json", import.meta.url));
const iconRecognitionItems = readJsonc(
    new URL("../../../assets/data/IconRecognition/recognition_items.json", import.meta.url),
);
// 装箱物品选项的默认物品（砂叶粉末），各地区均需可装箱
const DEFAULT_FILL_ITEM_ID = "item_plant_moss_powder_3";
export const DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT = 4;

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
    assertRecord(iconRecognitionItems, "IconRecognition recognition_items.json");
}

validateData();

function buildMaaEndId(names, label) {
    validateLocalizedNames(names, label);
    const id = names.en_us.replace(/[^A-Za-z0-9]+/g, "");
    if (!/^[A-Za-z][A-Za-z0-9]*$/.test(id)) {
        throw new Error(`[DeliveryJobs] ${label} 的英文名无法生成合法 MaaEnd ID：${names.en_us}`);
    }
    return id;
}

// 按分类分组、再按中文名排序，保证不同环境下生成结果一致
function compareFillItemIds(a, b) {
    const catalogA = assertRecord(iconRecognitionItems[a], `IconRecognition 物品目录 ${a}`);
    const catalogB = assertRecord(iconRecognitionItems[b], `IconRecognition 物品目录 ${b}`);
    const categoryA = `${catalogA.storageKind}:${catalogA.categoryType}`;
    const categoryB = `${catalogB.storageKind}:${catalogB.categoryType}`;
    if (categoryA !== categoryB) {
        return categoryA < categoryB ? -1 : 1;
    }
    const nameA = getFillItemName(a);
    const nameB = getFillItemName(b);
    if (nameA !== nameB) {
        return nameA < nameB ? -1 : 1;
    }
    if (a === b) {
        return 0;
    }
    return a < b ? -1 : 1;
}

function getFillItemName(gameID) {
    const item = assertRecord(deliveryJobsData.items[gameID], `delivery_jobs.json 物品 ${gameID}`);
    validateLocalizedNames(item.names, `物品 ${gameID}`);
    return item.names.zh_cn;
}

// 地区的可装箱物品取各仓储节点 fillable_items 的交集；
// IconRecognition 物品目录未收录的物品不提供选项
function listRegionFillItemIds(regionId, depots) {
    const depotItemSets = depots.map((depot) => new Set(depot.fillable_items));
    const commonIds = [...depotItemSets[0]].filter((id) => depotItemSets.every((set) => set.has(id)));
    const unsupportedIds = commonIds.filter((id) => !iconRecognitionItems[id]);
    if (unsupportedIds.length > 0) {
        console.warn(`[DeliveryJobs] 地区 ${regionId} 跳过 IconRecognition 未收录物品：${unsupportedIds.join(", ")}`);
    }
    return commonIds.filter((id) => iconRecognitionItems[id]).sort(compareFillItemIds);
}

function buildFillItem(gameID) {
    const item = assertRecord(deliveryJobsData.items[gameID], `delivery_jobs.json 物品 ${gameID}`);
    validateLocalizedNames(item.names, `物品 ${gameID}`);
    const catalogEntry = assertRecord(iconRecognitionItems[gameID], `IconRecognition 物品目录 ${gameID}`);
    return {
        Id: gameID,
        Names: item.names,
        Label: `$iconRecognition.name.${gameID}`,
        ItemId: gameID,
        RecheckFilter: `${catalogEntry.storageKind}:${catalogEntry.categoryType}`,
    };
}

function buildDepot(regionGameId, regionId, depotGameId) {
    const depot = assertRecord(deliveryJobsData.depots[depotGameId], `仓储节点 ${depotGameId}`);
    if (depot.region_id !== regionGameId) {
        throw new Error(
            `[DeliveryJobs] 仓储节点 ${depotGameId} 所属地区应为 ${regionGameId}，实际为 ${depot.region_id}`,
        );
    }
    const id = buildMaaEndId(depot.names, `仓储节点 ${depotGameId}`);
    return {
        Id: id,
        GameId: depotGameId,
        Name: depot.names.zh_cn,
        Names: depot.names,
        Expected: buildLocalizedExpected(depot.names, `仓储节点 ${depotGameId}`),
        RegionId: regionId,
        AutoDeliverySupported: true,
        RegionScene: `SceneEnterMenuRegionalDevelopment${regionId}`,
        DepotScene: `SceneEnterMenuRegionalDevelopment${regionId}DepotNode`,
    };
}

const configuredRegions = Object.keys(deliveryJobsData.regions)
    .sort()
    .map((regionGameId) => {
        const region = assertRecord(deliveryJobsData.regions[regionGameId], `地区 ${regionGameId}`);
        const id = buildMaaEndId(region.names, `地区 ${regionGameId}`);
        const depots = Object.keys(deliveryJobsData.depots)
            .sort()
            .filter((depotGameId) => deliveryJobsData.depots[depotGameId].region_id === regionGameId)
            .map((depotGameId) => buildDepot(regionGameId, id, depotGameId));
        if (depots.length === 0) {
            throw new Error(`[DeliveryJobs] 地区 ${regionGameId} 没有仓储节点`);
        }
        const fillItems = listRegionFillItemIds(
            id,
            depots.map((depot) => deliveryJobsData.depots[depot.GameId]),
        ).map(buildFillItem);
        assertUnique(
            fillItems.map((item) => item.Id),
            `地区 ${id} 装箱物品 ID`,
        );
        if (!fillItems.some((item) => item.Id === DEFAULT_FILL_ITEM_ID)) {
            throw new Error(`[DeliveryJobs] 地区 ${id} 不能装箱默认物品 ${DEFAULT_FILL_ITEM_ID}`);
        }
        return {
            id,
            source: region,
            fillItems,
            depots,
        };
    });

assertUnique(
    configuredRegions.map((region) => region.id),
    "地区 ID",
);
assertUnique(
    configuredRegions.flatMap((region) => region.depots.map((depot) => depot.Id)),
    "仓储节点 ID",
);
assertUnique(
    configuredRegions.flatMap((region) => [
        region.id,
        ...region.depots.map((depot) => depot.Id),
    ]),
    "地区与仓储节点 ID",
);

export const deliveryJobRegions = configuredRegions.map(({id, source, fillItems, depots}) => ({
    Id: id,
    Name: source.names.zh_cn,
    Names: source.names,
    RegionScene: `SceneEnterMenuRegionalDevelopment${id}`,
    DepotScene: `SceneEnterMenuRegionalDevelopment${id}DepotNode`,
    Depots: depots.map((depot) => depot.Id),
    FillItems: fillItems,
    DefaultFillItem: DEFAULT_FILL_ITEM_ID,
}));

export const deliveryJobDepots = configuredRegions.flatMap(({source, depots}) =>
    depots.map((depot) => ({
        ...depot,
        RegionName: source.names.zh_cn,
    })),
);

export const deliveryJobLocaleEntries = {
    regions: configuredRegions.flatMap(({id, source, depots}) => [
        {
            key: `global.region.${id}`,
            names: source.names,
        },
        ...depots.map((depot) => ({
            key: `global.region.${depot.Id}`,
            names: depot.Names,
        })),
    ]),
    items: [
        ...new Map(
            configuredRegions
                .flatMap(({fillItems}) => fillItems)
                .map((item) => [
                    item.Id,
                    {
                        key: `iconRecognition.name.${item.Id}`,
                        names: item.Names,
                    },
                ]),
        ).values(),
    ],
    fillItemPriorities: configuredRegions.flatMap(({id, source}) =>
        Array.from({length: DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT}, (_, index) => {
            const priority = index + 1;
            return {
                key: `task.DeliveryJobs.WhatToFill${id}Priority${priority}`,
                names: {
                    zh_cn: `${source.names.zh_cn} · 优先级 ${priority}`,
                    zh_tw: `${source.names.zh_tw} · 優先級 ${priority}`,
                    en_us: `${source.names.en_us} · Priority ${priority}`,
                    ja_jp: `${source.names.ja_jp} · 優先度 ${priority}`,
                    ko_kr: `${source.names.ko_kr} · 우선순위 ${priority}`,
                },
            };
        }),
    ),
};

export function rawJson(value) {
    return {
        value,
        raw: JSON.stringify(value, null, 4),
    };
}
