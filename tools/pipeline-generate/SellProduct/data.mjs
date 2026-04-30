// SellProduct 数据源

import {createRequire} from "module";
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
    return `(?i)^${escaped.replace(/\s+/g, "\\s*").replace(/-/g, "\\s*-\\s*")}$`;
}

function buildItemLocaleKeyByCNName() {
    const map = new Map();
    for (const [
        localeKey,
        localeValue,
    ] of Object.entries(zhCNLocale)) {
        if (!localeKey.startsWith("item.")) continue;
        const itemKey = localeKey.slice("item.".length);
        map.set(localeValue, itemKey);
    }
    return map;
}

const ITEM_LOCALE_KEY_BY_CN_NAME = buildItemLocaleKeyByCNName();

// ===== 单次遍历 settlements，同时构建：
//   - ITEMS：全局物品字典（key → {name, label, candidates}）
//     candidates 候选名称列表（CN/TC/JP/EN），供 Go 侧 SellProductNormalizedItemMatch
//     自定义识别做抗噪声匹配。不再带 `^...$` 锚定符，噪声剥离和严格相等由
//     Go 侧 stripSeparators / stripASCIIAlnum 两层保证，既能消化 "I紫晶质瓶"
//     这种 ASCII 前缀噪声，又不会把「柑实罐头」误匹配到「优质柑实罐头」
//     （见 MaaEnd issue #2344、PR #1790 / issue #1793）。
//   - SETTLEMENT_ITEM_STATS：每个 settlement 内 key → {rarity, unitPrice}（取 level 最高 unitPrice）
const ITEMS = {};
const ITEM_KEY_BY_ID = new Map();
const SETTLEMENT_ITEM_STATS = new Map();
for (const [
    settlementId,
    settlement,
] of Object.entries(settlementData.settlements)) {
    const stats = new Map();
    for (const level of Object.values(settlement.byProsperityLevel)) {
        for (const item of level.tradeItems) {
            let key = ITEM_KEY_BY_ID.get(item.itemId);
            if (!key) {
                const localeKey = ITEM_LOCALE_KEY_BY_CN_NAME.get(item.name.CN);
                key = localeKey ?? toPascalCase(item.itemId.replace(/^item_/, ""));
                ITEM_KEY_BY_ID.set(item.itemId, key);
                if (!ITEMS[key]) {
                    const enName = item.name.EN?.replace(/[\[\]|]+/g, "").trim() || "";
                    ITEMS[key] = {
                        name: item.name.CN,
                        label: localeKey ? `$item.${localeKey}` : null,
                        candidates: [
                            item.name.CN,
                            item.name.TC,
                            item.name.JP,
                            enName || null,
                        ]
                            .map((s) => (typeof s === "string" ? s.trim() : s))
                            .filter(Boolean),
                    };
                }
            }
            const prev = stats.get(key);
            if (!prev || item.unitPrice > prev.unitPrice) {
                stats.set(key, {rarity: item.rarity, unitPrice: item.unitPrice});
            }
        }
    }
    SETTLEMENT_ITEM_STATS.set(settlementId, stats);
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
        TextExpected: [
            "基建前站",
            "(?i)Infra\\s*-\\s*Station",
            "建設基地",
        ],
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
        TextExpected: [
            "天王坪",
            "天王坪援助",
            "天王坪援建",
            "Sky King",
            "天王原",
        ],
    },
};

const DOMAIN_REGION_PREFIX = {
    domain_1: "ValleyIV",
    domain_2: "Wuling",
};

const REGION_PRIORITY = {
    ValleyIV: 0,
    Wuling: 1,
};

function compareRegionPrefix(a, b) {
    const aOrder = REGION_PRIORITY[a] ?? Number.MAX_SAFE_INTEGER;
    const bOrder = REGION_PRIORITY[b] ?? Number.MAX_SAFE_INTEGER;
    if (aOrder !== bOrder) {
        return aOrder - bOrder;
    }
    return a.localeCompare(b);
}

function buildSettlementTextExpected(settlementId, settlement) {
    const override = SETTLEMENT_OVERRIDE[settlementId]?.TextExpected;
    if (override) {
        return override;
    }
    return uniqueArray([
        settlement.settlementName.CN,
        settlement.settlementName.TC,
        settlement.settlementName.JP,
        settlement.settlementName.EN ? toFlexibleEnglishRegex(settlement.settlementName.EN) : null,
    ]);
}

// ===== 从 settlement 数据生成 settlementId → 售卖点配置映射 =====
// 排序策略：先按 domainId（domain_1=ValleyIV=四号谷地 在前，domain_2=Wuling=武陵 在后），
// 同 domain 内再按 settlementId 字典序。直接按 settlementId 排序会让武陵（stm_hongs_*）
// 排在四号谷地（stm_tundra_*）前面，与游戏内区域解锁顺序和 UI 习惯不符。
const SETTLEMENT_MAP = Object.entries(settlementData.settlements)
    .sort(
        (
            [
                aId,
                aData,
            ],
            [
                bId,
                bData,
            ],
        ) => {
            const aDomain = aData.domainId || "";
            const bDomain = bData.domainId || "";
            if (aDomain !== bDomain) return aDomain.localeCompare(bDomain);
            return aId.localeCompare(bId);
        },
    )
    .reduce(
        (
            acc,
            [
                settlementId,
                settlement,
            ],
        ) => {
            const override = SETTLEMENT_OVERRIDE[settlementId] || {};
            const regionPrefix =
                override.RegionPrefix || DOMAIN_REGION_PREFIX[settlement.domainId] || toPascalCase(settlement.domainId);
            const locationId = override.LocationId || toPascalCase(settlement.settlementName.EN || settlementId);
            acc[settlementId] = {
                RegionPrefix: regionPrefix,
                LocationId: locationId,
                TextExpected: buildSettlementTextExpected(settlementId, settlement),
            };
            return acc;
        },
        {},
    );

const SETTLEMENT_REGION_MAP = Object.entries(SETTLEMENT_MAP).reduce(
    (
        acc,
        [
            ,
            config,
        ],
    ) => {
        acc[config.RegionPrefix] = acc[config.RegionPrefix] || [];
        acc[config.RegionPrefix].push(`${config.RegionPrefix}${config.LocationId}`);
        return acc;
    },
    {},
);

// ===== 从 settlement 数据构建 LOCATIONS（取所有繁荣度等级的物品并集） =====
// SETTLEMENT_ITEM_STATS 已在单遍遍历中按 settlement 聚合好 {rarity, unitPrice}（取最高 unitPrice）。
// 末尾 sort 为 SETTLEMENT_OVERRIDE.RegionPrefix 改写场景兜底：当前 SETTLEMENT_MAP 已按
// domainId 排好，且 DOMAIN_REGION_PREFIX 与 REGION_PRIORITY 一致，所以默认数据下这次 sort 是
// 稳定 no-op；但若未来某个 settlement 通过 override 把 RegionPrefix 改成跨 domain 的值，
// 必须在这里重新按 REGION_PRIORITY 落位，否则 UI 顺序会乱。
const LOCATIONS = Object.entries(SETTLEMENT_MAP)
    .map(
        ([
            settlementId,
            config,
        ]) => {
            const settlement = settlementData.settlements[settlementId];
            // 按 rarity 降序 → unitPrice 降序 排列
            const items = [...SETTLEMENT_ITEM_STATS.get(settlementId).entries()]
                .sort((a, b) => b[1].rarity - a[1].rarity || b[1].unitPrice - a[1].unitPrice)
                .map(([key]) => key);
            return {
                ...config,
                LocationDesc: settlement.settlementName.CN,
                items,
            };
        },
    )
    .sort((a, b) => compareRegionPrefix(a.RegionPrefix, b.RegionPrefix) || a.LocationId.localeCompare(b.LocationId));

// ===== 构建 cases 数组 =====
// 同一 location 的 4 个 itemNum 对应 cases 物品列表完全一致，仅 selectKey / missHandlerKey
// 后缀编号不同。先用 buildItemCaseEntries 抽出与 itemNum 无关的「物品 + 是否启用 + label」基础
// 数据，再由 buildItemCases 拼上 itemNum 相关的两个 key 名，避免重复构造 4×(N+1) 个相同物品对象。
function buildItemCaseEntries(itemIds) {
    const entries = [{name: "无", enabled: false}];
    for (const id of itemIds) {
        const item = ITEMS[id];
        const entry = {
            name: item.name,
            enabled: true,
            candidates: item.candidates,
        };
        if (item.label) entry.label = item.label;
        entries.push(entry);
    }
    return entries;
}

function buildItemCases(nodePrefix, itemNum, entries) {
    const selectKey = `SellProduct${nodePrefix}SelectItem${itemNum}`;
    const missHandlerKey = `SellProduct${nodePrefix}SellAttempt${itemNum}SetMissHandler`;
    return entries.map((entry) => {
        const newCase = {
            name: entry.name,
            pipeline_override: {
                [selectKey]: entry.enabled
                    ? {enabled: true, custom_recognition_param: {candidates: entry.candidates}}
                    : {enabled: false},
                [missHandlerKey]: {
                    anchor: {
                        SellProductPriorityGoodMissHandler: entry.enabled ? "SellProductPriorityGoodMissWarning" : "",
                    },
                },
            },
        };
        if (entry.label) newCase.label = entry.label;
        return newCase;
    });
}

// ===== BetterSliding Quantity.Box（Win 端 / ADB 端） =====
// 改这里就够了，模板里 4 个 BetterSliding 节点会自动同步
const QUANTITY_BOX = [
    1107,
    535,
    74,
    29,
];
const QUANTITY_BOX_ADB = [
    1065,
    499,
    78,
    36,
];
const MAX_QUANTITY_BOX = [
    1073,
    327,
    119,
    25,
];
const MAX_QUANTITY_BOX_ADB = [
    1041,
    239,
    131,
    32,
];

export const settlementFlatRows = LOCATIONS.map((loc) => {
    const entries = buildItemCaseEntries(loc.items);
    return {
        RegionPrefix: loc.RegionPrefix,
        SellOptions: SETTLEMENT_REGION_MAP[loc.RegionPrefix],
        LocationId: loc.LocationId,
        LocationDesc: loc.LocationDesc,
        TextExpected: loc.TextExpected,
        QuantityBox: QUANTITY_BOX,
        QuantityBoxAdb: QUANTITY_BOX_ADB,
        MaxTargetBox: MAX_QUANTITY_BOX,
        MaxTargetBoxAdb: MAX_QUANTITY_BOX_ADB,
        ItemCases1: buildItemCases(loc.LocationId, 1, entries),
        ItemCases2: buildItemCases(loc.LocationId, 2, entries),
        ItemCases3: buildItemCases(loc.LocationId, 3, entries),
        ItemCases4: buildItemCases(loc.LocationId, 4, entries),
    };
});

export default settlementFlatRows;
