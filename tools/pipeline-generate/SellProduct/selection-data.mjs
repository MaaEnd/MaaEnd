import {mkdirSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath, pathToFileURL} from "node:url";

import {
    getOperatorCaseName,
    isAdminOperator,
    sellProductLocations,
    sellProductLocationsNewestFirst,
    settlementData,
    toPascalCase,
} from "./model.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const OUTPUT_PATH = resolve(__dirname, "../../../assets/data/SellProduct/selection_data.json");
const SUPPORTED_LOCALES = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];
const OPERATOR_CHAR_ID_PATTERN = /^chr_(\d+)(?:_|$)/;

// 活动结束且游戏数据移除这些物品后，删除此临时过滤。
const TEMP_EXCLUDED_ITEM_CN_NAMES = new Set([
    "息壤玉葫芦",
    "息壤葫芦",
]);

function completeLocalizedNames(names = {}) {
    const fallback = names.zh_cn || Object.values(names).find(Boolean);
    if (!fallback) return {};
    return Object.fromEntries(
        SUPPORTED_LOCALES.map((locale) => [
            locale,
            names[locale]?.trim() || fallback,
        ]),
    );
}

export function buildLocalizedNames(names = {}) {
    return completeLocalizedNames(names);
}

function parseOperatorCharId(operator) {
    const charId = operator?.id?.trim() || "";
    const match = OPERATOR_CHAR_ID_PATTERN.exec(charId);
    if (!match) {
        const label = operator?.names?.en_us || operator?.names?.zh_cn || "<unknown>";
        throw new Error(`operator ${JSON.stringify(label)} has invalid charId ${JSON.stringify(charId)}`);
    }
    return {
        id: charId,
        number: Number.parseInt(match[1], 10),
    };
}

function buildOperatorFeatureCounts(settlement, operatorCatalog) {
    const counts = new Map();
    for (const feature of settlement.features || []) {
        const matchedInFeature = new Set();
        for (const operatorID of feature.operator_ids || []) {
            const operator = operatorCatalog[operatorID];
            if (!operator) throw new Error(`feature references unknown operator ${JSON.stringify(operatorID)}`);
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
    const names = buildLocalizedNames(operator.names);
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

export function buildLocationOperatorOrder(
    settlement,
    acceptedBonusTypes,
    operators,
    targetUsage,
    operatorCatalog = settlementData.operators,
) {
    const accepted = new Set(acceptedBonusTypes);
    const featureCounts = buildOperatorFeatureCounts(settlement, operatorCatalog);
    const entries = new Map();
    for (const feature of settlement.features || []) {
        const matchedTypes = (feature.bonus_types || []).filter((type) => accepted.has(type));
        if (matchedTypes.length === 0) continue;

        for (const operatorID of feature.operator_ids || []) {
            const operator = operatorCatalog[operatorID];
            if (!operator) throw new Error(`feature references unknown operator ${JSON.stringify(operatorID)}`);
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
        const levels = [...(settlement.prosperity_levels || [])].sort((left, right) => left.level - right.level);
        for (const level of levels) {
            for (const tradeItem of level.trade_items || []) {
                const itemID = tradeItem.item_id?.trim();
                const item = data.items[itemID];
                const names = buildLocalizedNames(item?.names);
                if (!itemID || Object.keys(names).length === 0) continue;

                if (!items[itemID]) {
                    items[itemID] = {
                        names: {},
                    };
                }
                items[itemID].names = {
                    ...items[itemID].names,
                    ...names,
                };

                const excluded = TEMP_EXCLUDED_ITEM_CN_NAMES.has(item.names?.zh_cn);

                const previous = locationItems.get(itemID);
                if (!previous) {
                    locationItems.set(itemID, {
                        itemID,
                        rarity: item.rarity,
                        unitPrice: tradeItem.unit_price,
                        excluded,
                    });
                } else if (tradeItem.unit_price > previous.unitPrice) {
                    previous.rarity = item.rarity;
                    previous.unitPrice = tradeItem.unit_price;
                }
            }
        }

        locations[location.LocationId] = [...locationItems.values()]
            .filter((item) => !item.excluded)
            .map((item) => ({
                item_id: item.itemID,
                rarity: item.rarity,
                unit_price: item.unitPrice,
            }));
    }

    return {
        items,
        locationItems: locations,
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
            names: buildLocalizedNames(settlement.names),
            items: itemData.locationItems[location.LocationId],
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
        location_order: sellProductLocationsNewestFirst.map((location) => location.LocationId),
        locations,
    };
}

export const sellProductSelectionData = buildSellProductSelectionData();

// Task 选项按新地区优先、地区内据点稳定顺序收集物品，确保同价物品不受数据源对象键顺序影响。
// 运行时据点物品仍保留各自的稳定来源顺序和排序所需属性，具体选品顺序由 Go 策略决定。
function buildSelectableItems() {
    const items = [];
    const seen = new Set();
    for (const location of sellProductLocationsNewestFirst) {
        const settlement = settlementData.settlements[location.SettlementId];
        for (const level of settlement.prosperity_levels || []) {
            for (const tradeItem of level.trade_items || []) {
                const itemID = tradeItem.item_id?.trim();
                const item = settlementData.items[itemID];
                if (
                    !itemID ||
                    seen.has(itemID) ||
                    TEMP_EXCLUDED_ITEM_CN_NAMES.has(item?.names?.zh_cn) ||
                    !sellProductSelectionData.items[itemID]
                ) {
                    continue;
                }
                seen.add(itemID);
                items.push({
                    id: itemID,
                    name: item.names.zh_cn,
                });
            }
        }
    }
    return items;
}

export const sellProductSelectableItems = buildSelectableItems();

// 国际化同步器消费的物品视图。命名规则与 task-data.mjs 的反查兜底保持一致，
// 同步后的 item.* 键能被 Task 生成的 `$item.xxx` label 直接引用。
export const sellProductItemLocaleEntries = sellProductSelectableItems.map(({id}) => ({
    key: `item.${toPascalCase(id.replace(/^item_/, ""))}`,
    names: sellProductSelectionData.items[id]?.names || {},
}));

export function writeSellProductSelectionData() {
    mkdirSync(dirname(OUTPUT_PATH), {recursive: true});
    writeFileSync(OUTPUT_PATH, `${JSON.stringify(sellProductSelectionData, null, 4)}\n`, "utf8");
    console.log(`[SellProduct] 已生成运行时选品数据：${OUTPUT_PATH}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
    writeSellProductSelectionData();
}
