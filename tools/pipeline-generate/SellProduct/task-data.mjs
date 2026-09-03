// SellProduct Task 模板数据

import {readJsonc} from "../jsonc.mjs";
import {sellProductLocations, toPascalCase} from "./model.mjs";
import {sellProductSelectableItems, sellProductSelectionData} from "./selection-data.mjs";

const zhCNLocale = readJsonc(new URL("../../../assets/locales/interface/zh_cn.json", import.meta.url));

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

// 复用运行时选品数据生成器提供的可选物品，确保 Task 与 Go 使用同一过滤结果。
const ITEMS = {};
for (const item of sellProductSelectableItems) {
    const localeKey = ITEM_LOCALE_KEY_BY_CN_NAME.get(item.name);
    const key = localeKey ?? toPascalCase(item.id.replace(/^item_/, ""));
    ITEMS[key] = {
        id: item.id,
        name: item.name,
        label: localeKey ? `$item.${localeKey}` : null,
    };
}

// RegionPrefix → 该区域下所有 `${RegionPrefix}${LocationId}` 的列表，
// 模板里 SellOptions 字段直接消费，让任意一个售卖点能枚举出同区域的全部目标。
const SETTLEMENT_REGION_MAP = sellProductLocations.reduce((acc, location) => {
    acc[location.RegionPrefix] = acc[location.RegionPrefix] || [];
    acc[location.RegionPrefix].push(`${location.RegionPrefix}${location.LocationId}`);
    return acc;
}, {});

// 每个地区的优先售卖选项只列出该地区至少一个据点可售的物品，并记录最高单价。
// 下拉选项与游戏货架一致按单价降序排列，同价保持 ITEMS 的稳定来源顺序。
const ITEM_PRICE_BY_ID = new Map();
const PRIORITY_ITEM_PRICE_BY_REGION = sellProductLocations.reduce((acc, location) => {
    acc[location.RegionPrefix] = acc[location.RegionPrefix] || new Map();
    for (const item of sellProductSelectionData.locations[location.LocationId]?.items || []) {
        const globalPrevious = ITEM_PRICE_BY_ID.get(item.item_id);
        if (globalPrevious === undefined || item.unit_price > globalPrevious) {
            ITEM_PRICE_BY_ID.set(item.item_id, item.unit_price);
        }
        const previous = acc[location.RegionPrefix].get(item.item_id);
        if (previous === undefined || item.unit_price > previous) {
            acc[location.RegionPrefix].set(item.item_id, item.unit_price);
        }
    }
    return acc;
}, {});

function compareItemsByUnitPrice(priceByItemID) {
    return (left, right) => priceByItemID.get(right.id) - priceByItemID.get(left.id);
}

const LOCATIONS = sellProductLocations;
const REGION_PREFIXES = Object.keys(SETTLEMENT_REGION_MAP);
const STOCK_MINIMUM_UNIT_PRICE_DEFAULT = 10;
const TASK_OPTIONS = [
    "SellProductSchedule",
    "SellProductOperatorAutoSwitch",
    "SellProductSelectionStrategy",
    "SellProductPriorityRules",
    "SellProductItemReserveRules",
    ...REGION_PREFIXES.map((regionPrefix) => `${regionPrefix}Sell`),
];

// 独立保留规则使用所有据点货品的并集，不提供 Auto，并按游戏货架单价顺序展示。
// 具体货品 case 只通过 attach 注入 itemId；子 input 独占 custom_action_param，
// 避免 MaaFramework 依次应用选项覆盖时完整替换同名字段。
function buildReserveItemCases(slot) {
    return [
        {
            name: "None",
            label: "$task.SellProduct.ReserveNone",
        },
        ...Object.values(ITEMS)
            .sort(compareItemsByUnitPrice(ITEM_PRICE_BY_ID))
            .map((item) => ({
                name: item.name,
                ...(item.label ? {label: item.label} : {}),
                option: [`SellProductReserveItem${slot}Mode`],
                pipeline_override: {
                    [`SellProductRegisterReserveRule${slot}`]: {
                        attach: {
                            item_id: item.id,
                        },
                    },
                },
            })),
    ];
}

// 用户通过显式模式选择保留数量或永不售卖；-1 仅作为内部配置哨兵，
// 不要求用户理解或手动输入特殊数值。
function buildReserveModeCases(slot) {
    return [
        {
            name: "Quantity",
            label: "$task.SellProduct.ReserveModeQuantity",
            option: [`SellProductReserveItem${slot}Value`],
        },
        {
            name: "NeverSell",
            label: "$task.SellProduct.ReserveModeNeverSell",
            pipeline_override: {
                [`SellProductRegisterReserveRule${slot}`]: {
                    custom_action_param: {
                        operation: "register",
                        quantity: -1,
                    },
                },
            },
        },
    ];
}

// 用户指定的六个槽位只调整对应地区的动态优先级。
function buildPriorityItemCases(regionPrefix, slot) {
    const priceByItemID = PRIORITY_ITEM_PRICE_BY_REGION[regionPrefix] || new Map();
    return [
        {
            name: "None",
            label: "$task.SellProduct.PriorityNone",
        },
        ...Object.values(ITEMS)
            .filter((item) => priceByItemID.has(item.id))
            .sort(compareItemsByUnitPrice(priceByItemID))
            .map((item) => ({
                name: item.name,
                ...(item.label ? {label: item.label} : {}),
                pipeline_override: {
                    [`SellProduct${regionPrefix}RegisterPriorityItem${slot}`]: {
                        custom_action_param: {
                            operation: "register",
                            item_id: item.id,
                        },
                    },
                },
            })),
    ];
}

function buildPriorityRuleSwitchCases(regionPrefixes) {
    return [
        {
            name: "Yes",
            option: [
                "SellProductOnlyPreferredItems",
                ...regionPrefixes.map((regionPrefix) => `SellProduct${regionPrefix}PriorityRules`),
            ],
            pipeline_override: {
                SellProductConfigurePrioritySession: {
                    custom_action_param: {
                        operation: "configure",
                        enabled: true,
                    },
                },
            },
        },
        {name: "No"},
    ];
}

function buildSelectionStrategyCases() {
    return [
        {
            name: "Rarity",
            label: "$task.SellProduct.SelectionStrategyRarity",
            pipeline_override: {
                SellProductConfigureSelectionStrategy: {
                    custom_action_param: {
                        operation: "configure_strategy",
                        strategy: "rarity",
                    },
                },
            },
        },
        {
            name: "Price",
            label: "$task.SellProduct.SelectionStrategyPrice",
            pipeline_override: {
                SellProductConfigureSelectionStrategy: {
                    custom_action_param: {
                        operation: "configure_strategy",
                        strategy: "price",
                    },
                },
            },
        },
        {
            name: "Stock",
            label: "$task.SellProduct.SelectionStrategyStock",
            option: ["SellProductStockMinimumUnitPrice"],
            pipeline_override: {
                SellProductConfigureSelectionStrategy: {
                    custom_action_param: {
                        operation: "configure_strategy",
                        strategy: "stock",
                        minimum_unit_price: STOCK_MINIMUM_UNIT_PRICE_DEFAULT,
                    },
                },
            },
        },
    ];
}

function buildOnlyPreferredSwitchCases() {
    return [
        {
            name: "Yes",
            pipeline_override: {
                SellProductConfigurePrioritySession: {
                    custom_action_param: {
                        operation: "configure",
                        enabled: true,
                        only_preferred: true,
                    },
                },
            },
        },
        {name: "No"},
    ];
}

function buildRegionPriorityRuleSwitchCases(regionPrefix) {
    return [
        {
            name: "Yes",
            option: [
                `SellProduct${regionPrefix}PriorityItem1`,
                `SellProduct${regionPrefix}PriorityItem2`,
                `SellProduct${regionPrefix}PriorityItem3`,
                `SellProduct${regionPrefix}PriorityItem4`,
                `SellProduct${regionPrefix}PriorityItem5`,
                `SellProduct${regionPrefix}PriorityItem6`,
            ],
            pipeline_override: {
                [`SellProduct${regionPrefix}InitializePrioritySession`]: {
                    custom_action_param: {
                        operation: "reset_preferred",
                        enabled: true,
                    },
                },
            },
        },
        {name: "No"},
    ];
}

function buildReserveRuleSwitchCases() {
    return [
        {
            name: "Yes",
            option: [
                "SellProductReserveItem1",
                "SellProductReserveItem2",
                "SellProductReserveItem3",
                "SellProductReserveItem4",
                "SellProductReserveItem5",
                "SellProductReserveItem6",
            ],
        },
        {name: "No"},
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
        // 当前干员检查只消费已生成的候选与拥有缓存，不依赖 mode。
        // 不覆盖这两个节点，确保 Win32 / ADB 资源包各自提供的 ROI 始终生效。
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
const SELECTION_STRATEGY_CASES = buildSelectionStrategyCases();
const PRIORITY_RULE_SWITCH_CASES = buildPriorityRuleSwitchCases(REGION_PREFIXES);
const ONLY_PREFERRED_SWITCH_CASES = buildOnlyPreferredSwitchCases();
const RESERVE_RULE_SWITCH_CASES = buildReserveRuleSwitchCases();

const SLOT_NUMBERS = [
    1,
    2,
    3,
    4,
    5,
    6,
];

// 槽位选项按编号展开为模板占位字段；未启用行提供空数组，由任务合并去重保留首个非空版本。
function spreadSlotCases(prefix, enabled, build) {
    return Object.fromEntries(
        SLOT_NUMBERS.map((slot) => [
            `${prefix}${slot}`,
            enabled ? build(slot) : [],
        ]),
    );
}

export const sellProductTaskRows = LOCATIONS.map((loc, index) => {
    const firstInRegion = LOCATIONS.findIndex((entry) => entry.RegionPrefix === loc.RegionPrefix) === index;
    return {
        RegionPrefix: loc.RegionPrefix,
        SellOptions: SETTLEMENT_REGION_MAP[loc.RegionPrefix],
        TaskOptions: index === 0 ? TASK_OPTIONS : [],
        LocationId: loc.LocationId,
        OnlyPreferredSwitchCases: index === 0 ? ONLY_PREFERRED_SWITCH_CASES : [],
        OperatorRefreshModeCases: index === 0 ? OPERATOR_REFRESH_MODE_CASES : [],
        StockMinimumUnitPriceDefault: String(STOCK_MINIMUM_UNIT_PRICE_DEFAULT),
        SelectionStrategyCases: index === 0 ? SELECTION_STRATEGY_CASES : [],
        ...spreadSlotCases("PriorityItemCases", firstInRegion, (slot) =>
            buildPriorityItemCases(loc.RegionPrefix, slot),
        ),
        PriorityRuleSwitchCases: index === 0 ? PRIORITY_RULE_SWITCH_CASES : [],
        RegionPriorityRuleSwitchCases: firstInRegion ? buildRegionPriorityRuleSwitchCases(loc.RegionPrefix) : [],
        ReserveRuleSwitchCases: index === 0 ? RESERVE_RULE_SWITCH_CASES : [],
        ...spreadSlotCases("ReserveItemCases", index === 0, buildReserveItemCases),
        ...spreadSlotCases("ReserveModeCases", index === 0, buildReserveModeCases),
    };
});

export default sellProductTaskRows;
