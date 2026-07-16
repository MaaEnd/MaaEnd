// SellProduct Task 模板数据

import {createRequire} from "node:module";
import {
    sellProductLocations,
    settlementData,
    toPascalCase,
} from "./model.mjs";

const require = createRequire(import.meta.url);
const zhCNLocale = require("../../../assets/locales/interface/zh_cn.json");

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
// 构造自动干员 CustomRecognition 的完整参数，供默认节点和强制刷新覆盖复用。
function buildOperatorRecognitionParam(usage, location, mode = "cache", result = undefined) {
    return {
        mode,
        usage,
        location,
        ...(result ? {result} : {}),
        roi: [
            164,
            121,
            700,
            430,
        ],
    };
}

// TODO(SellProduct): 活动结束后，临时排除以下活动物品，避免继续生成到可售卖列表。
// 当 settlement_trade.json 数据更新并确认活动物品已移除后，删除该常量与下方过滤判断。
const TEMP_EXCLUDED_ITEM_CN_NAMES = new Set([
    "息壤玉葫芦",
    "息壤葫芦",
]);

// 单次遍历 settlements，同时构建：
//   - ITEMS：物品字典（key → {id, name, label, candidates}）。candidates 是 CN/TC/EN/JP/KR 候选名，
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
                        id: item.itemId,
                        name: item.name.CN,
                        label: localeKey ? `$item.${localeKey}` : null,
                        candidates: [
                            item.name.CN,
                            item.name.TC,
                            enName || null,
                            item.name.JP,
                            item.name.KR,
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
    const items = [...SETTLEMENT_ITEM_STATS.get(location.SettlementId).entries()]
        .sort((a, b) => b[1].rarity - a[1].rarity || b[1].unitPrice - a[1].unitPrice)
        .map(([key]) => key);
    return {
        ...location,
        items,
    };
});

// 为一个全局优先级顺位生成全部据点的覆盖。
// 指定货品只会启用实际出售该货品的据点；Auto 则按各据点自己的价值顺序取同一顺位。
function buildGlobalItemPriorityOverride(locations, itemNum, selectedItemKey) {
    const pipelineOverride = {};
    for (const loc of locations) {
        const itemKey = selectedItemKey === "Auto" ? loc.items[itemNum - 1] : selectedItemKey;
        const enabled = Boolean(itemKey && loc.items.includes(itemKey));
        if (!enabled) {
            continue;
        }
        const item = ITEMS[itemKey];
        pipelineOverride[`SellProduct${loc.LocationId}SellAttempt${itemNum}`] = {enabled: true};
        pipelineOverride[`SellProduct${loc.LocationId}SelectItem${itemNum}`] = {
            enabled: true,
            custom_recognition_param: {
                item_id: item.id,
                candidates: item.candidates,
            },
        };
        pipelineOverride[`SellProduct${loc.LocationId}RecordItem${itemNum}`] = {
            custom_action_param: {
                operation: "select",
                item_id: item.id,
            },
        };
        pipelineOverride[`SellProduct${loc.LocationId}SellAttempt${itemNum}SetMissHandler`] = {
            anchor: {
                SellProductPriorityGoodMissHandler: "SellProductPriorityGoodMissWarning",
            },
        };
    }
    return pipelineOverride;
}

// 独立保留规则使用所有据点货品的并集，不提供 Auto，且不依赖任何优先级顺位。
// 每个具体货品 case 只负责启用并注册 itemId；数量由其子 input 注入同一注册节点。
function buildReserveItemCases(slot) {
    return [
        {
            name: "None",
            label: "$task.SellProduct.ReserveNone",
        },
        ...Object.values(ITEMS).map((item) => ({
            name: item.name,
            ...(item.label ? {label: item.label} : {}),
            option: [`SellProductReserveItem${slot}Value`],
            pipeline_override: {
                [`SellProductRegisterReserveRule${slot}`]: {
                    enabled: true,
                    custom_action_param: {
                        operation: "register",
                        item_id: item.id,
                        quantity: `{SellProductReserveItem${slot}Value}`,
                    },
                },
            },
        })),
    ];
}

function buildReserveRuleSwitchCases(locations) {
    const pipelineOverride = {
        SellProductSelectFirstGood: {
            next: ["SellProductRecordFirstGood"],
        },
        SellProductSelectNextGood: {
            next: ["SellProductRecordNextGood"],
        },
    };
    for (const loc of locations) {
        for (let slot = 1; slot <= 4; slot += 1) {
            pipelineOverride[`SellProduct${loc.LocationId}SellAttempt${slot}`] = {
                anchor: {
                    SellProductBetterSliding: `SellProduct${loc.LocationId}ApplyReserve${slot}`,
                },
            };
            pipelineOverride[`SellProduct${loc.LocationId}SelectItem${slot}`] = {
                next: [`SellProduct${loc.LocationId}RecordItem${slot}`],
            };
        }
    }
    return [
        {
            name: "Yes",
            option: [
                "SellProductReserveItem1",
                "SellProductReserveItem2",
                "SellProductReserveItem3",
                "SellProductReserveItem4",
            ],
            pipeline_override: pipelineOverride,
        },
        {name: "No"},
    ];
}

// 全局优先级使用所有据点货品的并集。四个顺位由总开关作为同级子选项展开。
function buildGlobalItemPriorityCases(locations, itemNum) {
    const makeCase = (name, label, selectedItemKey) => {
        const itemCase = {
            name,
            pipeline_override: buildGlobalItemPriorityOverride(locations, itemNum, selectedItemKey),
        };
        if (label) {
            itemCase.label = label;
        }
        return itemCase;
    };
    return [
        makeCase("Auto", "$task.SellProduct.GlobalPriorityAuto", "Auto"),
        makeCase("None", "$task.SellProduct.GlobalPriorityNone", null),
        ...Object.entries(ITEMS).map(
            ([
                itemKey,
                item,
            ]) => makeCase(item.name, item.label, itemKey),
        ),
    ];
}

// 未启用优先物品时仍执行前两次默认售卖，只跳过自定义货品识别。
// 启用后才展开四档全局优先级，由各顺位分别决定哪些据点执行。
function buildGlobalItemPrioritySwitchCases(locations) {
    const defaultSellOverride = {};
    for (const loc of locations) {
        defaultSellOverride[`SellProduct${loc.LocationId}SellAttempt1`] = {enabled: true};
        defaultSellOverride[`SellProduct${loc.LocationId}SellAttempt2`] = {enabled: true};
    }
    return [
        {
            name: "Yes",
            option: [
                "SellProductPriorityItem1",
                "SellProductPriorityItem2",
                "SellProductPriorityItem3",
                "SellProductPriorityItem4",
            ],
        },
        {
            name: "No",
            pipeline_override: defaultSellOverride,
        },
    ];
}

// 生成全局“强制刷新干员缓存”开关；Yes 覆盖完整参数，避免浅合并丢失候选列表。
function buildOperatorRefreshModeCases(locations) {
    const refreshOverride = {
        SellProductInitializeOperatorSession: {
            custom_action_param: {
                operation: "reset",
                mode: "refresh",
            },
        },
        SellProductOperatorCacheReady: {
            custom_recognition_param: buildOperatorRecognitionParam("all", "global", "refresh"),
        },
        SellProductOperatorListScanDone: {
            custom_recognition_param: buildOperatorRecognitionParam("all", "global", "refresh", "scan_done"),
        },
        SellProductOperatorListScanFailed: {
            custom_recognition_param: buildOperatorRecognitionParam("all", "global", "refresh", "error"),
        },
    };
    for (const loc of locations) {
        refreshOverride[`SellProduct${loc.LocationId}CurrentTargetOperator`] = {
            custom_recognition_param: {
                ...buildOperatorRecognitionParam("target", loc.LocationId, "refresh"),
                roi: [
                    268,
                    568,
                    190,
                    35,
                ],
            },
        };
        refreshOverride[`SellProduct${loc.LocationId}SelectTargetOperator`] = {
            custom_recognition_param: buildOperatorRecognitionParam("target", loc.LocationId, "refresh"),
        };
        refreshOverride[`SellProduct${loc.LocationId}RetryTargetOperatorAfterScan`] = {
            custom_recognition_param: buildOperatorRecognitionParam("target", loc.LocationId, "refresh", "retry"),
        };
        refreshOverride[`SellProduct${loc.LocationId}TargetOperatorNotFound`] = {
            custom_recognition_param: buildOperatorRecognitionParam("target", loc.LocationId, "refresh", "not_found"),
        };
        refreshOverride[`SellProduct${loc.LocationId}TargetOperatorScanFailed`] = {
            custom_recognition_param: buildOperatorRecognitionParam("target", loc.LocationId, "refresh", "error"),
        };
        refreshOverride[`SellProduct${loc.LocationId}CurrentRestoreOperator`] = {
            custom_recognition_param: {
                ...buildOperatorRecognitionParam("restore", loc.LocationId, "refresh"),
                roi: [
                    268,
                    568,
                    190,
                    35,
                ],
            },
        };
        refreshOverride[`SellProduct${loc.LocationId}SelectRestoreOperator`] = {
            custom_recognition_param: buildOperatorRecognitionParam("restore", loc.LocationId, "refresh"),
        };
        refreshOverride[`SellProduct${loc.LocationId}RetryRestoreOperatorAfterScan`] = {
            custom_recognition_param: buildOperatorRecognitionParam("restore", loc.LocationId, "refresh", "retry"),
        };
        refreshOverride[`SellProduct${loc.LocationId}RestoreOperatorNotFoundAtBottom`] = {
            custom_recognition_param: buildOperatorRecognitionParam("restore", loc.LocationId, "refresh", "not_found"),
        };
        refreshOverride[`SellProduct${loc.LocationId}RestoreOperatorScanFailed`] = {
            custom_recognition_param: buildOperatorRecognitionParam("restore", loc.LocationId, "refresh", "error"),
        };
    }
    return [
        {
            name: "No",
        },
        {
            name: "Yes",
            pipeline_override: refreshOverride,
        },
    ];
}

const OPERATOR_REFRESH_MODE_CASES = buildOperatorRefreshModeCases(LOCATIONS);
const GLOBAL_ITEM_PRIORITY_SWITCH_CASES = buildGlobalItemPrioritySwitchCases(LOCATIONS);
const RESERVE_RULE_SWITCH_CASES = buildReserveRuleSwitchCases(LOCATIONS);

export const sellProductTaskRows = LOCATIONS.map((loc, index) => {
    return {
        RegionPrefix: loc.RegionPrefix,
        SellOptions: SETTLEMENT_REGION_MAP[loc.RegionPrefix],
        LocationId: loc.LocationId,
        OperatorRefreshModeCases: index === 0 ? OPERATOR_REFRESH_MODE_CASES : [],
        GlobalItemPrioritySwitchCases: index === 0 ? GLOBAL_ITEM_PRIORITY_SWITCH_CASES : [],
        ReserveRuleSwitchCases: index === 0 ? RESERVE_RULE_SWITCH_CASES : [],
        ReserveItemCases1: index === 0 ? buildReserveItemCases(1) : [],
        ReserveItemCases2: index === 0 ? buildReserveItemCases(2) : [],
        ReserveItemCases3: index === 0 ? buildReserveItemCases(3) : [],
        ReserveItemCases4: index === 0 ? buildReserveItemCases(4) : [],
        GlobalItemPriorityCases1: index === 0 ? buildGlobalItemPriorityCases(LOCATIONS, 1) : [],
        GlobalItemPriorityCases2: index === 0 ? buildGlobalItemPriorityCases(LOCATIONS, 2) : [],
        GlobalItemPriorityCases3: index === 0 ? buildGlobalItemPriorityCases(LOCATIONS, 3) : [],
        GlobalItemPriorityCases4: index === 0 ? buildGlobalItemPriorityCases(LOCATIONS, 4) : [],
    };
});

export default sellProductTaskRows;
