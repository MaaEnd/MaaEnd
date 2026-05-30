// SellProduct 数据源

import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {createRequire} from "node:module";
import {repoRoot, dataDir} from "../utils/paths.mjs";

let settlementData;
try {
    settlementData = JSON.parse(readFileSync(resolve(dataDir, "settlement_trade.json"), "utf8"));
} catch {
    console.error("[SellProduct] 数据文件缺失，请先运行 pnpm fetch:zmdmap 或 pnpm generate:SellProduct");
    process.exit(1);
}

const require = createRequire(import.meta.url);
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

// 中文物品名 → locales/interface/zh_cn.json 中 `item.*` 的后缀 key。
// 用于反查物品的 i18n key，进而生成 `$item.xxx` 形式的可翻译 label。
const ITEM_LOCALE_KEY_BY_CN_NAME = buildItemLocaleKeyByCNName();

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

// ===== settlementId 覆盖（命名 + TextExpected 特殊处理） =====
// 当原始数据里的 EN 名称生成的 LocationId 不符合习惯（如音译/缩写），或者 OCR
// 经常误识为某些固定文本（如 "Reconstruction Hc"），需要在这里手动指定。
// LocationId    覆盖 toPascalCase(EN) 默认值，决定生成出的 pipeline 节点前缀。
// TextExpected  完全替换默认的 CN/TC/JP/EN 候选，需要自行覆盖所有语言变体 + OCR 噪声。
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

// domainId → RegionPrefix 默认映射。新 domain 接入时若沿用「英文区域名」命名约定，加一行即可；
// 不在表中的 domain 会回退到 toPascalCase(domainId)。
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
        settlement.settlementName.EN ? toFlexibleEnglishRegex(settlement.settlementName.EN) : null,
    ]);
}

// settlementId → {RegionPrefix, LocationId, TextExpected}
// 排序：先按 domainId（与游戏内解锁顺序一致：domain_1=ValleyIV 在前，domain_2=Wuling 在后），
// 同 domain 内再按 settlementId 字典序。
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

// RegionPrefix → 该区域下所有 `${RegionPrefix}${LocationId}` 的列表，
// 模板里 SellOptions 字段直接消费，让任意一个售卖点能枚举出同区域的全部目标。
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

// LOCATIONS：模板最终消费形态，items 按 rarity → unitPrice 降序排列。顺序继承 SETTLEMENT_MAP。
const LOCATIONS = Object.entries(SETTLEMENT_MAP).map(
    ([
        settlementId,
        config,
    ]) => {
        const settlement = settlementData.settlements[settlementId];
        const items = [...SETTLEMENT_ITEM_STATS.get(settlementId).entries()]
            .sort((a, b) => b[1].rarity - a[1].rarity || b[1].unitPrice - a[1].unitPrice)
            .map(([key]) => key);
        return {
            ...config,
            LocationDesc: settlement.settlementName.CN,
            items,
        };
    },
);

// 同一 location 的 4 个 itemNum 的物品列表完全一致，仅 selectKey/missHandlerKey 后缀编号不同。
// 先抽出与 itemNum 无关的基础数据（buildItemCaseEntries），再由 buildItemCases 拼上 itemNum 相关的 key。
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
