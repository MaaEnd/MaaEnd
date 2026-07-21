// SellProduct Task 模板数据

import {createRequire} from "node:module";
import {sellProductLocations, toPascalCase} from "./model.mjs";
import {sellProductSelectableItems} from "./selection-data.mjs";

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

const LOCATIONS = sellProductLocations;

// 独立保留规则使用所有据点货品的并集，不提供 Auto，且不依赖任何优先级顺位。
// 具体货品 case 只通过 attach 注入 itemId；子 input 独占 custom_action_param，
// 避免 MaaFramework 依次应用选项覆盖时完整替换同名字段。
function buildReserveItemCases(slot) {
    return [
        {
            name: "None",
            label: "$task.SellProduct.ReserveNone",
        },
        ...Object.values(ITEMS).map((item) => ({
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

// 用户指定的六个槽位直接调整生成数据中的动态优先级。
function buildPriorityItemCases(slot) {
    return [
        {
            name: "None",
            label: "$task.SellProduct.PriorityNone",
        },
        ...Object.values(ITEMS).map((item) => ({
            name: item.name,
            ...(item.label ? {label: item.label} : {}),
            pipeline_override: {
                [`SellProductRegisterPriorityItem${slot}`]: {
                    custom_action_param: {
                        operation: "register",
                        item_id: item.id,
                    },
                },
            },
        })),
    ];
}

function buildPriorityRuleSwitchCases() {
    return [
        {
            name: "Yes",
            option: [
                "SellProductPriorityItem1",
                "SellProductPriorityItem2",
                "SellProductPriorityItem3",
                "SellProductPriorityItem4",
                "SellProductPriorityItem5",
                "SellProductPriorityItem6",
            ],
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
const PRIORITY_RULE_SWITCH_CASES = buildPriorityRuleSwitchCases();
const RESERVE_RULE_SWITCH_CASES = buildReserveRuleSwitchCases();

export const sellProductTaskRows = LOCATIONS.map((loc, index) => {
    return {
        RegionPrefix: loc.RegionPrefix,
        SellOptions: SETTLEMENT_REGION_MAP[loc.RegionPrefix],
        LocationId: loc.LocationId,
        OperatorRefreshModeCases: index === 0 ? OPERATOR_REFRESH_MODE_CASES : [],
        PriorityItemCases1: index === 0 ? buildPriorityItemCases(1) : [],
        PriorityItemCases2: index === 0 ? buildPriorityItemCases(2) : [],
        PriorityItemCases3: index === 0 ? buildPriorityItemCases(3) : [],
        PriorityItemCases4: index === 0 ? buildPriorityItemCases(4) : [],
        PriorityItemCases5: index === 0 ? buildPriorityItemCases(5) : [],
        PriorityItemCases6: index === 0 ? buildPriorityItemCases(6) : [],
        PriorityRuleSwitchCases: index === 0 ? PRIORITY_RULE_SWITCH_CASES : [],
        ReserveRuleSwitchCases: index === 0 ? RESERVE_RULE_SWITCH_CASES : [],
        ReserveItemCases1: index === 0 ? buildReserveItemCases(1) : [],
        ReserveItemCases2: index === 0 ? buildReserveItemCases(2) : [],
        ReserveItemCases3: index === 0 ? buildReserveItemCases(3) : [],
        ReserveItemCases4: index === 0 ? buildReserveItemCases(4) : [],
        ReserveItemCases5: index === 0 ? buildReserveItemCases(5) : [],
        ReserveItemCases6: index === 0 ? buildReserveItemCases(6) : [],
        ReserveModeCases1: index === 0 ? buildReserveModeCases(1) : [],
        ReserveModeCases2: index === 0 ? buildReserveModeCases(2) : [],
        ReserveModeCases3: index === 0 ? buildReserveModeCases(3) : [],
        ReserveModeCases4: index === 0 ? buildReserveModeCases(4) : [],
        ReserveModeCases5: index === 0 ? buildReserveModeCases(5) : [],
        ReserveModeCases6: index === 0 ? buildReserveModeCases(6) : [],
    };
});

export default sellProductTaskRows;
