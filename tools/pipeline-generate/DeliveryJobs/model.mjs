import {readFileSync} from "node:fs";
import {resolve} from "node:path";

import {repoRoot} from "../utils/paths.mjs";

const INTERFACE_LOCALES = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];

const localeCatalogs = Object.fromEntries(
    INTERFACE_LOCALES.map((locale) => [
        locale,
        JSON.parse(readFileSync(resolve(repoRoot, `assets/locales/interface/${locale}.json`), "utf8")),
    ]),
);

function escapeRegex(text) {
    return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function toFlexibleEnglishRegex(text) {
    return `(?i)${escapeRegex(text.trim()).replace(/\s+/g, "\\s*")}`;
}

function buildLocalizedExpected(localeKey) {
    const values = INTERFACE_LOCALES.map((locale) => {
        const value = localeCatalogs[locale][localeKey];
        if (typeof value !== "string" || value.length === 0) {
            throw new Error(`[DeliveryJobs] ${localeKey} 缺少 ${locale} 文案`);
        }
        return locale === "en_us" ? toFlexibleEnglishRegex(value) : value;
    });
    return [...new Set(values)];
}

function getChineseName(localeKey) {
    const value = localeCatalogs.zh_cn[localeKey];
    if (typeof value !== "string" || value.length === 0) {
        throw new Error(`[DeliveryJobs] ${localeKey} 缺少 zh_cn 文案`);
    }
    return value;
}

const FILL_ITEMS = {
    SandleafPowder: "item.SandleafPowder",
    BuckflowerPowder: "item.BuckflowerPowder",
    CitromePowder: "item.CitromePowder",
    AketinePowder: "item.AketinePowder",
    YazhenPowder: "item.YazhenPowder",
    JincaoPowder: "item.JincaoPowder",
    Yazhen: "item.Yazhen",
    Jincao: "item.Jincao",
    Xiranite: "item.Xiranite",
};

function buildFillItem(id) {
    const localeKey = FILL_ITEMS[id];
    return {
        Id: id,
        Name: getChineseName(localeKey),
        Label: `$${localeKey}`,
        Template: `DeliveryJobs/${id}.png`,
    };
}

export const deliveryJobRegions = [
    {
        Id: "ValleyIV",
        Name: getChineseName("global.region.ValleyIV"),
        RegionScene: "SceneEnterMenuRegionalDevelopmentValleyIV",
        DepotScene: "SceneEnterMenuRegionalDevelopmentValleyIVDepotNode",
        Depots: [
            "OriginiumSciencePark",
            "OriginLodespring",
            "PowerPlateau",
        ],
        FillItems: [
            "SandleafPowder",
            "BuckflowerPowder",
            "CitromePowder",
            "AketinePowder",
        ].map(buildFillItem),
        DefaultFillItem: "SandleafPowder",
    },
    {
        Id: "Wuling",
        Name: getChineseName("global.region.Wuling"),
        RegionScene: "SceneEnterMenuRegionalDevelopmentWuling",
        DepotScene: "SceneEnterMenuRegionalDevelopmentWulingDepotNode",
        Depots: [
            "WulingCity",
            "TestArea",
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
        ].map(buildFillItem),
        DefaultFillItem: "SandleafPowder",
    },
];

export const deliveryJobDepots = deliveryJobRegions.flatMap((region) =>
    region.Depots.map((id) => ({
        Id: id,
        Name: getChineseName(`global.region.${id}`),
        Expected: buildLocalizedExpected(`global.region.${id}`),
        RegionId: region.Id,
        RegionName: region.Name,
        RegionScene: region.RegionScene,
        DepotScene: region.DepotScene,
    })),
);

export function rawJson(value) {
    return {
        value,
        raw: JSON.stringify(value, null, 4),
    };
}
