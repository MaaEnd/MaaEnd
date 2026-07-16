// SellProduct Task 模板数据

import {createRequire} from "node:module";
import {
    escapeRegex,
    getOperatorCaseName,
    isAdminOperator,
    sellProductLocations,
    settlementData,
    toPascalCase,
    uniqueArray,
} from "./model.mjs";

const require = createRequire(import.meta.url);
const zhCNLocale = require("../../../assets/locales/interface/zh_cn.json");

function buildOperatorExpected(operator) {
    return uniqueArray([
        operator.name?.CN,
        operator.name?.TC,
        operator.name?.EN,
        operator.name?.JP,
        operator.name?.KR,
    ]).map(escapeRegex);
}

// 建立中文物品名到 interface locale key 的反查表。
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

// 中文物品名 → locales/interface/zh_cn.json 中 `item.*` 的后缀 key。
// 用于反查物品的 i18n key，进而生成 `$item.xxx` 形式的可翻译 label。
const ITEM_LOCALE_KEY_BY_CN_NAME = buildItemLocaleKeyByCNName();
const OPERATOR_LOCALE_ORDER = new Map(
    Object.keys(zhCNLocale)
        .filter((key) => key.startsWith("operator."))
        .map((key, index) => [
            key.slice("operator.".length),
            index,
        ]),
);
const TARGET_OPERATOR_BONUS_TYPES = new Set([
    "expProfit",
    "moneyProfit",
]);
const RESTORE_OPERATOR_BONUS_TYPES = new Set([
    "moneyProduceSpeed",
]);

export function buildOperatorCaseEntry(operator) {
    const name = getOperatorCaseName(operator);
    if (!OPERATOR_LOCALE_ORDER.has(name)) {
        console.warn(`[SellProduct] 缺少干员本地化条目 operator.${name}，将按名称排序回退处理`);
    }

    return {
        name,
        label: `$operator.${name}`,
        expected: buildOperatorExpected(operator),
    };
}

// 从 settlement_trade.operators 生成全局干员 case 基表，并保持现有 i18n 顺序。
function buildOperatorCaseEntries() {
    return Object.values(settlementData.operators || {})
        .filter((operator) => !isAdminOperator(operator))
        .map(buildOperatorCaseEntry)
        .sort((a, b) => {
            return (
                (OPERATOR_LOCALE_ORDER.get(a.name) ?? Number.MAX_SAFE_INTEGER) -
                    (OPERATOR_LOCALE_ORDER.get(b.name) ?? Number.MAX_SAFE_INTEGER) || a.name.localeCompare(b.name)
            );
        })
        .filter((entry) => entry.expected.length > 0);
}

const OPERATOR_CASE_ENTRIES = buildOperatorCaseEntries();

// 按据点特性 bonus 类型收集该据点可用于对应用途的干员名。
function buildOperatorNameSetByBonusTypes(settlement, bonusTypes) {
    const names = new Set();
    for (const feature of settlement.settlementFeatures || []) {
        const hasMatchingBonus = (feature.bonuses || []).some((bonus) => bonusTypes.has(bonus.type));
        if (!hasMatchingBonus) continue;

        for (const operator of feature.matchingOperators || []) {
            if (isAdminOperator(operator)) continue;
            names.add(getOperatorCaseName(operator));
        }
    }
    return names;
}

// 用据点级干员名集合筛选全局干员 case，同时保留全局排序。
function filterOperatorCaseEntries(operatorNames) {
    return OPERATOR_CASE_ENTRIES.filter((entry) => operatorNames.has(entry.name));
}

// TODO(SellProduct): 活动结束后，临时排除以下活动物品，避免继续生成到可售卖列表。
// 当 settlement_trade.json 数据更新并确认活动物品已移除后，删除该常量与下方过滤判断。
const TEMP_EXCLUDED_ITEM_CN_NAMES = new Set([
    "息壤玉葫芦",
    "息壤葫芦",
]);

// 单次遍历 settlements，同时构建：
//   - ITEMS：物品字典（key → {name, label, candidates}）。candidates 是 CN/TC/JP/EN 候选名，
//     由 Go 侧 SellProductNormalizedItemMatch 做抗噪声匹配（不含 `^...$` 锚定符）。
//   - ITEM_KEY_BY_ID：itemId → ITEMS key 反查表，去重。
//   - SETTLEMENT_ITEM_STATS：settlementId → (key → {rarity, unitPrice})，
//     同 key 在多个 prosperityLevel 出现时取 unitPrice 最高的一条，供 LOCATIONS 排序。
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
            if (TEMP_EXCLUDED_ITEM_CN_NAMES.has(item.name.CN)) {
                continue;
            }
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

// RegionPrefix → 该区域下所有 `${RegionPrefix}${LocationId}` 的列表，
// 模板里 SellOptions 字段直接消费，让任意一个售卖点能枚举出同区域的全部目标。
const SETTLEMENT_REGION_MAP = sellProductLocations.reduce((acc, location) => {
    acc[location.RegionPrefix] = acc[location.RegionPrefix] || [];
    acc[location.RegionPrefix].push(`${location.RegionPrefix}${location.LocationId}`);
    return acc;
}, {});

// Task 模板最终消费形态，items 按 rarity → unitPrice 降序排列。
const LOCATIONS = sellProductLocations.map((location) => {
    const settlement = settlementData.settlements[location.SettlementId];
    const items = [...SETTLEMENT_ITEM_STATS.get(location.SettlementId).entries()]
        .sort((a, b) => b[1].rarity - a[1].rarity || b[1].unitPrice - a[1].unitPrice)
        .map(([key]) => key);
    const targetOperatorNames = buildOperatorNameSetByBonusTypes(settlement, TARGET_OPERATOR_BONUS_TYPES);
    const restoreOperatorNames = buildOperatorNameSetByBonusTypes(settlement, RESTORE_OPERATOR_BONUS_TYPES);
    return {
        ...location,
        TargetOperatorNames: targetOperatorNames,
        RestoreOperatorNames: restoreOperatorNames,
        items,
    };
});

// 同一 location 的 4 个 itemNum 的物品列表完全一致，仅 selectKey/missHandlerKey 后缀编号不同。
// 先抽出与 itemNum 无关的基础数据（buildItemCaseEntries），再由 buildItemCases 拼上 itemNum 相关的 key。
// 将据点可售物品转换成四个售卖尝试共用的选项基表。
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

// 为指定售卖尝试编号生成物品选择 case，并绑定对应 miss handler。
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

// 生成售卖前切换干员选项，只包含建设效率和调度券收益相关干员。
function buildTargetOperatorCases(nodePrefix, operatorNames) {
    const currentKey = `SellProduct${nodePrefix}CurrentTargetOperator`;
    const selectKey = `SellProduct${nodePrefix}SelectTargetOperator`;
    return filterOperatorCaseEntries(operatorNames).map((entry) => ({
        name: entry.name,
        label: entry.label,
        pipeline_override: {
            [currentKey]: {
                expected: entry.expected,
            },
            [selectKey]: {
                expected: entry.expected,
            },
        },
    }));
}

// 生成售卖后恢复干员选项，只包含调度券生产速度相关干员。
function buildRestoreOperatorCases(nodePrefix, operatorNames) {
    const currentKey = `SellProduct${nodePrefix}CurrentRestoreOperator`;
    const selectKey = `SellProduct${nodePrefix}SelectRestoreOperator`;
    return [
        {
            name: "DoNotRestore",
            label: "$task.SellProduct.OperatorRestoreDoNotRestore",
            pipeline_override: {
                [`SellProduct${nodePrefix}SetAfterSellOperatorAnchor`]: {
                    anchor: {
                        SellProductAfterSellOperator: "SellProductAfterSellOperator",
                    },
                },
            },
        },
        ...filterOperatorCaseEntries(operatorNames).map((entry) => ({
            name: entry.name,
            label: entry.label,
            pipeline_override: {
                [`SellProduct${nodePrefix}SetAfterSellOperatorAnchor`]: {
                    anchor: {
                        SellProductAfterSellOperator: `SellProduct${nodePrefix}AfterSellOperator`,
                    },
                },
                [currentKey]: {
                    expected: entry.expected,
                },
                [selectKey]: {
                    expected: entry.expected,
                },
            },
        })),
    ];
}

export const sellProductTaskRows = LOCATIONS.map((loc) => {
    const entries = buildItemCaseEntries(loc.items);
    const targetOperatorCases = buildTargetOperatorCases(loc.LocationId, loc.TargetOperatorNames);
    const restoreOperatorCases = buildRestoreOperatorCases(loc.LocationId, loc.RestoreOperatorNames);
    return {
        RegionPrefix: loc.RegionPrefix,
        SellOptions: SETTLEMENT_REGION_MAP[loc.RegionPrefix],
        LocationId: loc.LocationId,
        ItemCases1: buildItemCases(loc.LocationId, 1, entries),
        ItemCases2: buildItemCases(loc.LocationId, 2, entries),
        ItemCases3: buildItemCases(loc.LocationId, 3, entries),
        ItemCases4: buildItemCases(loc.LocationId, 4, entries),
        TargetOperatorDefaultCase: targetOperatorCases[0]?.name,
        TargetOperatorCases: targetOperatorCases,
        RestoreOperatorCases: restoreOperatorCases,
    };
});

export default sellProductTaskRows;
