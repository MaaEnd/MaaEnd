import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";

import {sellProductLocations} from "./model.mjs";
import {
    buildLocationOperatorOrder,
    buildSelectionItems,
    buildSellProductSelectionData,
    sellProductSelectableItems,
    sellProductSelectionData,
} from "./selection-data.mjs";

const generatedPath = new URL("../../../assets/data/SellProduct/selection_data.json", import.meta.url);

test("SellProduct selection data artifact matches the current source model", () => {
    const generated = JSON.parse(readFileSync(generatedPath, "utf8"));
    assert.deepEqual(generated, buildSellProductSelectionData());
    assert.deepEqual(generated, sellProductSelectionData);
});

test("SellProduct selection data contains only valid stable references", () => {
    const data = sellProductSelectionData;
    assert.deepEqual(
        data.location_order,
        sellProductLocations.map((location) => location.LocationId),
    );
    for (const item of Object.values(data.items)) {
        assert.deepEqual(Object.keys(item.names), [
            "zh_cn",
            "zh_tw",
            "en_us",
            "ja_jp",
            "ko_kr",
        ]);
    }
    for (const operator of Object.values(data.operators)) {
        assert.deepEqual(Object.keys(operator.names), [
            "zh_cn",
            "zh_tw",
            "en_us",
            "ja_jp",
            "ko_kr",
        ]);
    }
    for (const locationName of data.location_order) {
        const location = data.locations[locationName];
        assert.ok(location, `missing location ${locationName}`);
        assert.deepEqual(Object.keys(location.names), [
            "zh_cn",
            "zh_tw",
            "en_us",
            "ja_jp",
            "ko_kr",
        ]);
        for (const itemID of location.item_order) {
            assert.ok(data.items[itemID], `${locationName} references missing item ${itemID}`);
        }
        for (const operatorName of [
            ...location.target_operators.map((operator) => operator.name),
            ...location.restore_operators,
        ]) {
            assert.ok(data.operators[operatorName], `${locationName} references missing operator ${operatorName}`);
        }
    }
});

test("SellProduct temporary activity items stay recognizable but are not selectable or sellable", () => {
    const excluded = [
        "item_activity_xiranite_enr_hulu",
        "item_activity_xiranite_hulu",
    ];
    const selectableIDs = new Set(sellProductSelectableItems.map((item) => item.id));
    for (const itemID of excluded) {
        assert.ok(sellProductSelectionData.items[itemID]);
        assert.equal(selectableIDs.has(itemID), false);
        for (const location of Object.values(sellProductSelectionData.locations)) {
            assert.equal(location.item_order.includes(itemID), false);
        }
    }
});

test("SellProduct generated item order merges prosperity levels and sorts by rarity then price", () => {
    const data = {
        settlements: {
            test: {
                byProsperityLevel: {
                    1: {
                        tradeItems: [
                            {itemId: "low", rarity: 2, unitPrice: 100, name: {CN: "低级", EN: "Low"}},
                            {itemId: "high_cheap", rarity: 3, unitPrice: 80, name: {CN: "高级便宜"}},
                            {itemId: "event", rarity: 5, unitPrice: 999, name: {CN: "息壤玉葫芦"}},
                        ],
                    },
                    2: {
                        tradeItems: [
                            {itemId: "high_expensive", rarity: 3, unitPrice: 120, name: {CN: "高级昂贵"}},
                            {itemId: "low", rarity: 2, unitPrice: 110, name: {TC: "低級", KR: "낮음"}},
                        ],
                    },
                },
            },
        },
    };
    const result = buildSelectionItems(data, [{SettlementId: "test", LocationId: "Test"}]);
    assert.deepEqual(result.locationItemOrder.Test, [
        "high_expensive",
        "high_cheap",
        "low",
    ]);
    assert.deepEqual(result.items.low.names, {
        zh_cn: "低级",
        zh_tw: "低級",
        en_us: "Low",
        ja_jp: "低级",
        ko_kr: "낮음",
    });
    assert.ok(result.items.event);
});

test("SellProduct generated target operators prioritize combined profit bonuses", () => {
    const both = {charId: "both", name: {CN: "双加成", EN: "Both"}};
    const money = {charId: "money", name: {CN: "收益", EN: "Money"}};
    const exp = {charId: "exp", name: {CN: "经验", EN: "Exp"}};
    const settlement = {
        settlementFeatures: [
            {
                bonuses: [{type: "expProfit"}],
                matchingOperators: [
                    exp,
                    both,
                ],
            },
            {
                bonuses: [{type: "moneyProfit"}],
                matchingOperators: [
                    money,
                    both,
                ],
            },
        ],
    };
    const operators = {};
    const order = buildLocationOperatorOrder(
        settlement,
        [
            "expProfit",
            "moneyProfit",
        ],
        new Map([
            [
                "Exp",
                0,
            ],
            [
                "Money",
                1,
            ],
            [
                "Both",
                2,
            ],
        ]),
        operators,
        true,
    );
    assert.deepEqual(order, [
        {
            name: "Both",
            bonus_tier: 0,
        },
        {
            name: "Money",
            bonus_tier: 1,
        },
        {
            name: "Exp",
            bonus_tier: 2,
        },
    ]);
});

test("SellProduct generated target operators preserve equal bonus tiers", () => {
    const data = sellProductSelectionData.locations.XiranflowCloudseederStation.target_operators;
    const lifeng = data.find((operator) => operator.name === "Lifeng");
    const arcane = data.find((operator) => operator.name === "Arcane");

    assert.ok(lifeng);
    assert.ok(arcane);
    assert.equal(lifeng.bonus_tier, 0);
    assert.equal(arcane.bonus_tier, lifeng.bonus_tier);
});
