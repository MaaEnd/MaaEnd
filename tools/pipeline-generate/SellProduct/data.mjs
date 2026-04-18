// SellProduct 数据源

import { createRequire } from "module";
const require = createRequire(import.meta.url);
const settlementData = require("./settlement_trade.json");
const zhCNLocale = require("../../../assets/locales/interface/zh_cn.json");

function escapeRegex(str) {
    return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function toPascalCase(str) {
    return str
        .split(/[^a-zA-Z0-9]+/)
        .filter(Boolean)
        .map((part) => part[0].toUpperCase() + part.slice(1))
        .join("");
}

function uniqueArray(items) {
    return [...new Set(items.filter(Boolean))];
}

function toFlexibleEnglishRegex(text) {
    const escaped = escapeRegex(text.trim());
    return `(?i)^${escaped
        .replace(/\s+/g, "\\s*")
        .replace(/-/g, "\\s*-\\s*")}$`;
}

function collectTradeItems() {
    const items = new Map();
    for (const settlement of Object.values(settlementData.settlements)) {
        for (const level of Object.values(settlement.byProsperityLevel)) {
            for (const item of level.tradeItems) {
                if (!items.has(item.itemId)) {
                    items.set(item.itemId, item);
                }
            }
        }
    }
    return items;
}

function buildItemLocaleKeyByCNName() {
    const map = new Map();
    for (const [localeKey, localeValue] of Object.entries(zhCNLocale)) {
        if (!localeKey.startsWith("item.")) continue;
        const itemKey = localeKey.slice("item.".length);
        map.set(localeValue, itemKey);
    }
    return map;
}

const ITEM_LOCALE_KEY_BY_CN_NAME = buildItemLocaleKeyByCNName();

// ===== itemId 覆盖（可用于修正 locale 无法反查时的特殊键） =====
const ITEM_META_OVERRIDE = {};

// ===== 从 settlement 数据生成 itemId → 内部 key / label 映射 =====
const ITEM_META = [...collectTradeItems().entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .reduce((acc, [itemId, itemData]) => {
        const override = ITEM_META_OVERRIDE[itemId];
        const localeKey = ITEM_LOCALE_KEY_BY_CN_NAME.get(itemData.name.CN);
        const key =
            override?.key ??
            localeKey ??
            toPascalCase(itemId.replace(/^item_/, ""));
        acc[itemId] = {
            key,
            label: override?.label ?? (localeKey ? `$item.${localeKey}` : null),
        };
        return acc;
    }, {});

// ===== 从 settlement 数据提取全局物品字典 =====
// expected 顺序: TC, CN, JP, EN
const ITEMS = {};
for (const settlement of Object.values(settlementData.settlements)) {
    for (const level of Object.values(settlement.byProsperityLevel)) {
        for (const item of level.tradeItems) {
            const meta = ITEM_META[item.itemId];
            if (!meta) continue;
            if (ITEMS[meta.key]) continue; // 已收集过
            const enName = item.name.EN?.replace(/[\[\]|]+/g, "") || "";
            ITEMS[meta.key] = {
                name: item.name.CN,
                label: meta.label,
                expected: [
                    `^${escapeRegex(item.name.TC)}$`,
                    `^${escapeRegex(item.name.CN)}$`,
                    `^${escapeRegex(item.name.JP)}$`,
                    enName ? `^${escapeRegex(enName)}$` : null,
                ].filter(Boolean),
            };
        }
    }
}

// ===== settlementId 覆盖（命名 + TextExpected 特殊处理） =====
const SETTLEMENT_OVERRIDE = {
    stm_tundra_1: {
        LocationId: "RefugeeCamp",
        TextExpected: [
            "难民暂居处",
            "難民暫居處",
            "(?i)Refugee\\s*Camp",
            "仮設居住地",
        ],
    },
    stm_tundra_2: {
        LocationId: "InfrastructureOutpost",
        TextExpected: ["基建前站", "(?i)Infra\\s*-\\s*Station", "建設基地"],
    },
    stm_tundra_3: {
        LocationId: "ReconstructionCommand",
        TextExpected: [
            "重建指挥部",
            "重建指揮部",
            "(?i)Reconstruction\\s*HQ",
            "再建管理本部",
            "Reconstruction Hc",
        ],
    },
    stm_hongs_1: {
        LocationId: "SkyKingFlats",
        TextExpected: ["天王坪", "天王坪援助", "天王坪援建", "Sky King", "天王原"],
    },
};

const DOMAIN_REGION_PREFIX = {
    domain_1: "ValleyIV",
    domain_2: "Wuling",
};

function buildSettlementTextExpected(settlementId, settlement) {
    const override = SETTLEMENT_OVERRIDE[settlementId]?.TextExpected;
    if (override) {
        return override;
    }
    return uniqueArray([
        settlement.settlementName.CN,
        settlement.settlementName.TC,
        settlement.settlementName.JP,
        settlement.settlementName.EN
            ? toFlexibleEnglishRegex(settlement.settlementName.EN)
            : null,
    ]);
}

// ===== 从 settlement 数据生成 settlementId → 售卖点配置映射 =====
const SETTLEMENT_MAP = Object.entries(settlementData.settlements)
    .sort(([a], [b]) => a.localeCompare(b))
    .reduce((acc, [settlementId, settlement]) => {
        const override = SETTLEMENT_OVERRIDE[settlementId] || {};
        const regionPrefix =
            override.RegionPrefix ||
            DOMAIN_REGION_PREFIX[settlement.domainId] ||
            toPascalCase(settlement.domainId);
        const locationId =
            override.LocationId ||
            toPascalCase(settlement.settlementName.EN || settlementId);
        acc[settlementId] = {
            RegionPrefix: regionPrefix,
            LocationId: locationId,
            TextExpected: buildSettlementTextExpected(settlementId, settlement),
        };
        return acc;
    }, {});

const SETTLEMENT_REGION_MAP = Object.entries(SETTLEMENT_MAP).reduce((acc, [, config]) => {
    acc[config.RegionPrefix] = acc[config.RegionPrefix] || [];
    acc[config.RegionPrefix].push(`${config.RegionPrefix}${config.LocationId}`);
    return acc;
}, {});

// ===== 从 settlement 数据构建 LOCATIONS（取所有繁荣度等级的物品并集） =====
const LOCATIONS = Object.entries(SETTLEMENT_MAP).map(
    ([settlementId, config]) => {
        const settlement = settlementData.settlements[settlementId];
        // 取所有 level 的 tradeItems 并集（按 itemId 去重），记录 rarity 和最高 unitPrice
        const itemMap = new Map();
        for (const level of Object.values(settlement.byProsperityLevel)) {
            for (const item of level.tradeItems) {
                const meta = ITEM_META[item.itemId];
                if (!meta) continue;
                const prev = itemMap.get(meta.key);
                if (!prev || item.unitPrice > prev.unitPrice) {
                    itemMap.set(meta.key, {
                        rarity: item.rarity,
                        unitPrice: item.unitPrice,
                    });
                }
            }
        }
        // 按 rarity 降序 → unitPrice 降序 排列
        const items = [...itemMap.entries()]
            .sort(
                (a, b) =>
                    b[1].rarity - a[1].rarity ||
                    b[1].unitPrice - a[1].unitPrice,
            )
            .map(([key]) => key);
        return {
            ...config,
            LocationDesc: settlement.settlementName.CN,
            items,
        };
    },
);

// ===== 构建 cases 数组 =====
function buildItemCases(nodePrefix, itemNum, itemIds) {
    const selectKey = `SellProduct${nodePrefix}SelectItem${itemNum}`;
    const attemptKey = `SellProduct${nodePrefix}SellAttempt${itemNum}`;
    const cases = [
        {
            name: "无",
            pipeline_override: {
                [selectKey]: { enabled: false },
                [attemptKey]: {
                    anchor: {
                        SellProductSelectNewGood: selectKey,
                        SellProductPriorityGoodMissHandler: "",
                    },
                },
            },
        },
    ];
    for (const id of itemIds) {
        const item = ITEMS[id];
        const newCase = {
            name: item.name,
            pipeline_override: {
                [selectKey]: {
                    enabled: true,
                    expected: item.expected,
                },
                [attemptKey]: {
                    anchor: {
                        SellProductSelectNewGood: selectKey,
                        SellProductPriorityGoodMissHandler:
                            "SellProductPriorityGoodMissWarning",
                    },
                },
            },
        };
        if (item.label) {
            newCase.label = item.label;
        }
        cases.push(newCase);
    }
    return cases;
}

export const settlementFlatRows = LOCATIONS.map((loc) => ({
    RegionPrefix: loc.RegionPrefix,
    SellOptions: SETTLEMENT_REGION_MAP[loc.RegionPrefix],
    LocationId: loc.LocationId,
    LocationDesc: loc.LocationDesc,
    TextExpected: loc.TextExpected,
    ItemCases1: buildItemCases(loc.LocationId, 1, loc.items),
    ItemCases2: buildItemCases(loc.LocationId, 2, loc.items),
    ItemCases3: buildItemCases(loc.LocationId, 3, loc.items),
    ItemCases4: buildItemCases(loc.LocationId, 4, loc.items),
}));

export default settlementFlatRows;
