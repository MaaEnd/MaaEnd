// SellProduct 数据源
// 基于 settlement_trade_outposts.json 自动构建物品列表（取所有繁荣度等级的并集）

import { createRequire } from "module";
const require = createRequire(import.meta.url);
const settlementData = require("./settlement_trade_outposts.json");

// ===== itemId → 内部 key / label 映射 =====
const ITEM_META = {
    item_bottled_rec_hp_3: { key: "BuckCapsuleA", label: "$item.BuckCapsuleA" },
    item_proc_battery_3: { key: "HCValleyBattery", label: "$item.HCValleyBattery" },
    item_bottled_food_3: { key: "CannedCitromeA", label: "$item.CannedCitromeA" },
    item_proc_battery_2: { key: "SCValleyBattery", label: "$item.SCValleyBattery" },
    item_bottled_food_2: { key: "CannedCitromeB", label: "$item.CannedCitromeB" },
    item_bottled_rec_hp_2: { key: "BuckCapsuleB", label: "$item.BuckCapsuleB" },
    item_bottled_rec_hp_1: { key: "BuckCapsuleC", label: "$item.BuckCapsuleC" },
    item_bottled_food_1: { key: "CannedCitromeC", label: "$item.CannedCitromeC" },
    item_glass_bottle: { key: "AmethystBottle", label: "$item.AmethystBottle" },
    item_crystal_shell: { key: "Origocrust", label: "$item.Origocrust" },
    item_glass_cmpt: { key: "AmethystPart", label: "$item.AmethystPart" },
    item_proc_battery_1: { key: "LCValleyBattery", label: "$item.LCValleyBattery" },
    item_iron_cmpt: { key: "FerriumPart", label: "$item.FerriumPart" },
    item_iron_enr_cmpt: { key: "SteelPart", label: "$item.SteelPart" },
    item_proc_battery_5: { key: "SCWulingBattery", label: "$item.SCWulingBattery" },
    item_bottled_rec_hp_5: { key: "YazhenSyringeB", label: "$item.YazhenSyringeB" },
    item_proc_battery_4: { key: "LCWulingBattery", label: "$item.LCWulingBattery" },
    item_bottled_rec_hp_4: { key: "YazhenSyringe", label: "$item.YazhenSyringe" },
    item_bottled_food_4: { key: "JincaoDrink", label: "$item.JincaoDrink" },
    item_copper_cmpt: { key: "CuprumPart", label: "$item.CuprumPart" },
    item_xiranite_powder: { key: "Xiranite", label: "$item.Xiranite" },
};

// ===== 从 settlement 数据提取全局物品字典 =====
// expected 顺序: TC, CN, JP, EN
const ITEMS = {};
for (const settlement of Object.values(settlementData.settlements)) {
    for (const level of Object.values(settlement.byProsperityLevel)) {
        for (const item of level.tradeItems) {
            const meta = ITEM_META[item.itemId];
            if (!meta) continue;
            if (ITEMS[meta.key]) continue; // 已收集过
            ITEMS[meta.key] = {
                name: item.name.CN,
                label: meta.label,
                expected: [
                    `^${item.name.TC}$`,
                    `^${item.name.CN}$`,
                    `^${item.name.JP}$`,
                    `^${item.name.EN}$`,
                ],
            };
        }
    }
}

// ===== settlementId → 售卖点配置映射 =====
const SETTLEMENT_MAP = {
    stm_tundra_1: {
        RegionPrefix: "ValleyIV",
        LocationId: "RefugeeCamp",
        NodePrefix: "RefugeeCamp",
    },
    stm_tundra_2: {
        RegionPrefix: "ValleyIV",
        LocationId: "InfrastructureOutpost",
        NodePrefix: "InfrastructureOutpost",
    },
    stm_tundra_3: {
        RegionPrefix: "ValleyIV",
        LocationId: "ReconstructionCommand",
        NodePrefix: "ReconstructionCommand",
    },
    stm_hongs_1: {
        RegionPrefix: "Wuling",
        LocationId: "SkyKingFlats",
        NodePrefix: "SkyKingFlats",
    },
};

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
        return { ...config, items };
    },
);

// ===== 构建 cases 数组 =====
function buildItemCases(nodePrefix, itemNum, itemIds) {
    const selectKey = `SellProduct${nodePrefix}SelectItem${itemNum}`;
    const cases = [
        {
            name: "无",
            pipeline_override: {
                [selectKey]: { enabled: false },
            },
        },
    ];
    for (const id of itemIds) {
        const item = ITEMS[id];
        cases.push({
            name: item.name,
            pipeline_override: {
                [selectKey]: {
                    enabled: true,
                    expected: item.expected,
                },
            },
            label: item.label,
        });
    }
    return cases;
}

// ===== 导出数据 =====
export default LOCATIONS.map((loc) => ({
    RegionPrefix: loc.RegionPrefix,
    LocationId: loc.LocationId,
    NodePrefix: loc.NodePrefix,
    ItemCases1: buildItemCases(loc.NodePrefix, 1, loc.items),
    ItemCases2: buildItemCases(loc.NodePrefix, 2, loc.items),
    ItemCases3: buildItemCases(loc.NodePrefix, 3, loc.items),
    ItemCases4: buildItemCases(loc.NodePrefix, 4, loc.items),
}));
