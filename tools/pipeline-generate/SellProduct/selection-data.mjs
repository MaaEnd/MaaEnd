import {mkdirSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath, pathToFileURL} from "node:url";

import {getOperatorCaseName, isAdminOperator, sellProductLocations, settlementData} from "./model.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const OUTPUT_PATH = resolve(__dirname, "../../../assets/data/SellProduct/selection_data.json");
const SUPPORTED_LANGUAGES = [
    "CN",
    "TC",
    "EN",
    "JP",
    "KR",
];
const LOCALE_BY_LANGUAGE = {
    CN: "zh_cn",
    TC: "zh_tw",
    EN: "en_us",
    JP: "ja_jp",
    KR: "ko_kr",
};
const OPERATOR_CHAR_ID_PATTERN = /^chr_(\d+)(?:_|$)/;

// 活动结束且 zmdmap 移除这些物品后，删除此临时过滤。
const TEMP_EXCLUDED_ITEM_CN_NAMES = new Set([
    "息壤玉葫芦",
    "息壤葫芦",
]);

function localizedNamesFromSource(names = {}) {
    return Object.fromEntries(
        SUPPORTED_LANGUAGES.map((language) => [
            LOCALE_BY_LANGUAGE[language],
            names[language]?.trim(),
        ]).filter(
            ([
                ,
                name,
            ]) => name,
        ),
    );
}

function completeLocalizedNames(names = {}) {
    const fallback = names.zh_cn || Object.values(names).find(Boolean);
    if (!fallback) return {};
    return Object.fromEntries(
        Object.values(LOCALE_BY_LANGUAGE).map((locale) => [
            locale,
            names[locale] || fallback,
        ]),
    );
}

export function buildLocalizedNames(names = {}) {
    return completeLocalizedNames(localizedNamesFromSource(names));
}

function parseOperatorCharId(operator) {
    const charId = operator?.charId?.trim() || "";
    const match = OPERATOR_CHAR_ID_PATTERN.exec(charId);
    if (!match) {
        const label = operator?.name?.EN || operator?.name?.CN || "<unknown>";
        throw new Error(`operator ${JSON.stringify(label)} has invalid charId ${JSON.stringify(charId)}`);
    }
    return {
        id: charId,
        number: Number.parseInt(match[1], 10),
    };
}

function buildOperatorFeatureCounts(settlement) {
    const counts = new Map();
    for (const feature of settlement.settlementFeatures || []) {
        const matchedInFeature = new Set();
        for (const operator of feature.matchingOperators || []) {
            const {id} = parseOperatorCharId(operator);
            if (matchedInFeature.has(id)) continue;
            matchedInFeature.add(id);
            counts.set(id, (counts.get(id) || 0) + 1);
        }
    }
    return counts;
}

function compareInGameOperatorOrder(left, right) {
    return (
        right.featureCount - left.featureCount ||
        right.characterNumber - left.characterNumber ||
        left.name.localeCompare(right.name)
    );
}

function registerOperator(operators, operator) {
    const {id: charId, number: characterNumber} = parseOperatorCharId(operator);
    if (isAdminOperator(operator)) return null;
    const name = getOperatorCaseName(operator);
    const names = buildLocalizedNames(operator.name);
    if (!name || Object.keys(names).length === 0) return null;

    const previous = operators[name];
    operators[name] = {
        names: {
            ...(previous?.names || {}),
            ...names,
        },
    };
    return {
        name,
        charId,
        characterNumber,
    };
}

function targetBonusTier(entry) {
    const hasExp = entry.bonusTypes.has("expProfit");
    const hasMoney = entry.bonusTypes.has("moneyProfit");
    if (hasExp && hasMoney) return 0;
    if (hasExp) return 1;
    if (hasMoney) return 2;
    return 3;
}

function outpostProsperityMaxBonusTier(entry) {
    return entry.bonusTypes.has("moneyProfit") ? 0 : 1;
}

export function buildLocationOperatorOrder(settlement, acceptedBonusTypes, operators, targetUsage) {
    const accepted = new Set(acceptedBonusTypes);
    const featureCounts = buildOperatorFeatureCounts(settlement);
    const entries = new Map();
    for (const feature of settlement.settlementFeatures || []) {
        const matchedTypes = (feature.bonuses || []).map((bonus) => bonus.type).filter((type) => accepted.has(type));
        if (matchedTypes.length === 0) continue;

        for (const operator of feature.matchingOperators || []) {
            const registered = registerOperator(operators, operator);
            if (!registered) continue;
            const {name, charId, characterNumber} = registered;
            const entry = entries.get(name) || {
                name,
                characterNumber,
                featureCount: featureCounts.get(charId) || 0,
                bonusTypes: new Set(),
            };
            for (const type of matchedTypes) {
                entry.bonusTypes.add(type);
            }
            entries.set(name, entry);
        }
    }

    const sorted = [...entries.values()].sort((left, right) => {
        const tierDifference = targetUsage ? targetBonusTier(left) - targetBonusTier(right) : 0;
        return tierDifference || compareInGameOperatorOrder(left, right);
    });
    if (targetUsage) {
        return sorted.map((entry) => ({
            name: entry.name,
            bonus_tier: targetBonusTier(entry),
            outpost_prosperity_max_bonus_tier: outpostProsperityMaxBonusTier(entry),
        }));
    }
    return sorted.map((entry) => entry.name);
}

export function buildSelectionItems(data = settlementData, sourceLocations = sellProductLocations) {
    const items = {};
    const locations = {};

    for (const location of sourceLocations) {
        const settlement = data.settlements[location.SettlementId];
        const locationItems = new Map();
        const levels = Object.keys(settlement.byProsperityLevel || {}).sort();
        for (const level of levels) {
            for (const item of settlement.byProsperityLevel[level].tradeItems || []) {
                const itemID = item.itemId?.trim();
                const names = localizedNamesFromSource(item.name);
                if (!itemID || Object.keys(names).length === 0) continue;

                if (!items[itemID]) {
                    items[itemID] = {names: {}};
                }
                items[itemID].names = {
                    ...items[itemID].names,
                    ...names,
                };

                const excluded = TEMP_EXCLUDED_ITEM_CN_NAMES.has(item.name?.CN);

                const previous = locationItems.get(itemID);
                if (!previous) {
                    locationItems.set(itemID, {
                        itemID,
                        rarity: item.rarity,
                        unitPrice: item.unitPrice,
                        excluded,
                    });
                } else if (item.unitPrice > previous.unitPrice) {
                    previous.rarity = item.rarity;
                    previous.unitPrice = item.unitPrice;
                }
            }
        }

        locations[location.LocationId] = [...locationItems.values()]
            .filter((item) => !item.excluded)
            .sort((left, right) => right.rarity - left.rarity || right.unitPrice - left.unitPrice)
            .map((item) => item.itemID);
    }

    for (const item of Object.values(items)) {
        item.names = completeLocalizedNames(item.names);
    }

    return {
        items,
        locationItemOrder: locations,
    };
}

export function buildSellProductSelectionData() {
    const operators = {};
    for (const [
        ,
        operator,
    ] of Object.entries(settlementData.operators || {}).sort(([left], [right]) => left.localeCompare(right))) {
        registerOperator(operators, operator);
    }

    const itemData = buildSelectionItems();
    const locations = {};
    for (const location of sellProductLocations) {
        const settlement = settlementData.settlements[location.SettlementId];
        locations[location.LocationId] = {
            names: buildLocalizedNames(settlement.settlementName),
            item_order: itemData.locationItemOrder[location.LocationId],
            target_operators: buildLocationOperatorOrder(
                settlement,
                [
                    "expProfit",
                    "moneyProfit",
                ],
                operators,
                true,
            ),
            restore_operators: buildLocationOperatorOrder(settlement, ["moneyProduceSpeed"], operators, false),
        };
    }

    return {
        items: itemData.items,
        operators,
        location_order: sellProductLocations.map((location) => location.LocationId),
        locations,
    };
}

export const sellProductSelectionData = buildSellProductSelectionData();

// Task 选项使用上游展示顺序；运行时 item_order 使用稳定的据点排序。
// 两者共享同一物品字典和临时过滤规则，但不把 UI 顺序耦合到运行时识别顺序。
function buildSelectableItems() {
    const items = [];
    const seen = new Set();
    for (const settlement of Object.values(settlementData.settlements || {})) {
        for (const level of Object.values(settlement.byProsperityLevel || {})) {
            for (const item of level.tradeItems || []) {
                const itemID = item.itemId?.trim();
                if (
                    !itemID ||
                    seen.has(itemID) ||
                    TEMP_EXCLUDED_ITEM_CN_NAMES.has(item.name?.CN) ||
                    !sellProductSelectionData.items[itemID]
                ) {
                    continue;
                }
                seen.add(itemID);
                items.push({
                    id: itemID,
                    name: item.name.CN,
                });
            }
        }
    }
    return items;
}

export const sellProductSelectableItems = buildSelectableItems();

export function writeSellProductSelectionData() {
    mkdirSync(dirname(OUTPUT_PATH), {recursive: true});
    writeFileSync(OUTPUT_PATH, `${JSON.stringify(sellProductSelectionData, null, 4)}\n`, "utf8");
    console.log(`[SellProduct] 已生成运行时选品数据：${OUTPUT_PATH}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
    writeSellProductSelectionData();
}
