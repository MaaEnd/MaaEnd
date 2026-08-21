import {readFileSync} from "node:fs";

const INTERFACE_LOCALES = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];

const deliveryJobsData = JSON.parse(readFileSync(new URL("../data/delivery_jobs.json", import.meta.url), "utf8"));
const iconRecognitionItems = JSON.parse(
    readFileSync(new URL("../../../assets/data/IconRecognition/recognition_items.json", import.meta.url), "utf8"),
);
const interfaceLocaleZhCn = JSON.parse(
    readFileSync(new URL("../../../assets/locales/interface/zh_cn.json", import.meta.url), "utf8"),
);

// 装箱物品选项的默认物品（砂叶粉末），各地区均需可装箱
const DEFAULT_FILL_ITEM_ID = "item_plant_moss_powder_3";

// 地区与仓储节点的 MaaEnd 标识通过 global.region.* 文案与游戏数据名称匹配得到，
// 场景节点名按 SceneEnterMenuRegionalDevelopment{Id}[DepotNode] 约定生成
const REGION_LOCALE_PREFIX = "global.region.";

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

function buildRegionIdByName() {
    const idByName = new Map();
    for (const [
        key,
        value,
    ] of Object.entries(interfaceLocaleZhCn)) {
        if (!key.startsWith(REGION_LOCALE_PREFIX) || typeof value !== "string") {
            continue;
        }
        if (idByName.has(value)) {
            throw new Error(`[DeliveryJobs] global.region.* 中存在重名文案：${value}`);
        }
        idByName.set(value, key.slice(REGION_LOCALE_PREFIX.length));
    }
    return idByName;
}

const regionIdByName = buildRegionIdByName();

function matchRegionId(names, label) {
    validateLocalizedNames(names, label);
    const id = regionIdByName.get(names.zh_cn);
    if (!id) {
        throw new Error(
            `[DeliveryJobs] ${label}（${names.zh_cn}）在 global.region.* 中没有对应文案，请先在 assets/locales/interface/*.json 中登记`,
        );
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
    const name = interfaceLocaleZhCn[`iconRecognition.name.${gameID}`];
    if (typeof name !== "string" || name.length === 0) {
        throw new Error(`[DeliveryJobs] 物品 ${gameID} 缺少 iconRecognition.name 中文名称`);
    }
    return name;
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
        Name: getFillItemName(gameID),
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
    const id = matchRegionId(depot.names, `仓储节点 ${depotGameId}`);
    return {
        Id: id,
        GameId: depotGameId,
        Name: depot.names.zh_cn,
        Expected: buildLocalizedExpected(depot.names, `仓储节点 ${depotGameId}`),
        RegionId: regionId,
        RegionScene: `SceneEnterMenuRegionalDevelopment${regionId}`,
        DepotScene: `SceneEnterMenuRegionalDevelopment${regionId}DepotNode`,
    };
}

const configuredRegions = Object.keys(deliveryJobsData.regions)
    .sort()
    .map((regionGameId) => {
        const region = assertRecord(deliveryJobsData.regions[regionGameId], `地区 ${regionGameId}`);
        const id = matchRegionId(region.names, `地区 ${regionGameId}`);
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
            fillItems.map((item) => item.Name),
            `地区 ${id} 装箱物品名称`,
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

export const deliveryJobRegions = configuredRegions.map(({id, source, fillItems, depots}) => ({
    Id: id,
    Name: source.names.zh_cn,
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

export function rawJson(value) {
    return {
        value,
        raw: JSON.stringify(value, null, 4),
    };
}
