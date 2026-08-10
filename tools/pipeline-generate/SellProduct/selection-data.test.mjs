import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";

import {sellProductLocationsNewestFirst} from "./model.mjs";
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
        sellProductLocationsNewestFirst.map((location) => location.LocationId),
    );
    for (const item of Object.values(data.items)) {
        assert.equal("unit_price" in item, false);
        assert.deepEqual(Object.keys(item.names), [
            "zh_cn",
            "zh_tw",
            "en_us",
            "ja_jp",
            "ko_kr",
        ]);
    }
    for (const operator of Object.values(data.operators)) {
        assert.equal("cache_name" in operator, false);
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
        for (const item of location.items) {
            assert.ok(data.items[item.item_id], `${locationName} references missing item ${item.item_id}`);
            assert.ok(item.rarity > 0, `${locationName} item ${item.item_id} has invalid rarity`);
            assert.ok(item.unit_price > 0, `${locationName} item ${item.item_id} has invalid unit price`);
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
            assert.equal(
                location.items.some((item) => item.item_id === itemID),
                false,
            );
        }
    }
});

test("SellProduct generated location items merge prosperity levels without applying a strategy order", () => {
    const data = {
        items: {
            low: {
                rarity: 2,
                names: {
                    zh_cn: "低级",
                    zh_tw: "低級",
                    en_us: "Low",
                    ja_jp: "低级",
                    ko_kr: "낮음",
                },
            },
            high_cheap: {rarity: 3, names: {zh_cn: "高级便宜"}},
            high_expensive: {rarity: 3, names: {zh_cn: "高级昂贵"}},
            event: {rarity: 5, names: {zh_cn: "息壤玉葫芦"}},
        },
        settlements: {
            test: {
                prosperity_levels: [
                    {
                        level: 1,
                        trade_items: [
                            {item_id: "low", unit_price: 100},
                            {item_id: "high_cheap", unit_price: 80},
                            {item_id: "event", unit_price: 999},
                        ],
                    },
                    {
                        level: 2,
                        trade_items: [
                            {item_id: "high_expensive", unit_price: 120},
                            {item_id: "low", unit_price: 110},
                        ],
                    },
                ],
            },
        },
    };
    const result = buildSelectionItems(data, [{SettlementId: "test", LocationId: "Test"}]);
    assert.deepEqual(result.locationItems.Test, [
        {item_id: "low", rarity: 2, unit_price: 110},
        {item_id: "high_cheap", rarity: 3, unit_price: 80},
        {item_id: "high_expensive", rarity: 3, unit_price: 120},
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

test("SellProduct generated target operators prioritize prosperity before trade profit", () => {
    const catalog = {
        chr_0003_both: {id: "chr_0003_both", names: {zh_cn: "双加成", en_us: "Both"}},
        chr_0002_money: {id: "chr_0002_money", names: {zh_cn: "收益", en_us: "Money"}},
        chr_0001_exp: {id: "chr_0001_exp", names: {zh_cn: "经验", en_us: "Exp"}},
    };
    const settlement = {
        features: [
            {
                bonus_types: ["expProfit"],
                operator_ids: [
                    "chr_0001_exp",
                    "chr_0003_both",
                ],
            },
            {
                bonus_types: ["moneyProfit"],
                operator_ids: [
                    "chr_0002_money",
                    "chr_0003_both",
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
        operators,
        true,
        catalog,
    );
    assert.deepEqual(order, [
        {
            name: "Both",
            bonus_tier: 0,
            outpost_prosperity_max_bonus_tier: 0,
        },
        {
            name: "Exp",
            bonus_tier: 1,
            outpost_prosperity_max_bonus_tier: 1,
        },
        {
            name: "Money",
            bonus_tier: 2,
            outpost_prosperity_max_bonus_tier: 0,
        },
    ]);
});

test("SellProduct generated operator order follows feature matches then descending character id", () => {
    const catalog = {
        chr_0016_most: {id: "chr_0016_most", names: {zh_cn: "三特性", en_us: "Most"}},
        chr_0033_higher: {id: "chr_0033_higher", names: {zh_cn: "二特性", en_us: "Higher"}},
        chr_0004_lower: {id: "chr_0004_lower", names: {zh_cn: "一特性", en_us: "Lower"}},
    };
    const settlement = {
        features: [
            {
                bonus_types: ["moneyProfit"],
                operator_ids: [
                    "chr_0004_lower",
                    "chr_0016_most",
                    "chr_0033_higher",
                ],
            },
            {
                bonus_types: ["moneyProduceSpeed"],
                operator_ids: [
                    "chr_0016_most",
                    "chr_0033_higher",
                ],
            },
            {
                bonus_types: ["expProfit"],
                operator_ids: ["chr_0016_most"],
            },
        ],
    };

    const order = buildLocationOperatorOrder(settlement, ["moneyProfit"], {}, true, catalog);
    assert.deepEqual(order, [
        {
            name: "Most",
            bonus_tier: 2,
            outpost_prosperity_max_bonus_tier: 0,
        },
        {
            name: "Higher",
            bonus_tier: 2,
            outpost_prosperity_max_bonus_tier: 0,
        },
        {
            name: "Lower",
            bonus_tier: 2,
            outpost_prosperity_max_bonus_tier: 0,
        },
    ]);
});

test("SellProduct generated operator order rejects missing or malformed character ids", () => {
    for (const operator of [
        {names: {zh_cn: "缺少编号", en_us: "Missing"}},
        {id: "invalid", names: {zh_cn: "非法编号", en_us: "Invalid"}},
    ]) {
        const settlement = {
            features: [
                {
                    bonus_types: ["moneyProfit"],
                    operator_ids: ["test"],
                },
            ],
        };
        assert.throws(
            () => buildLocationOperatorOrder(settlement, ["moneyProfit"], {}, true, {test: operator}),
            /has invalid charId/,
        );
    }
});

test("SellProduct generated Refugee Camp restore order matches the observed in-game list", () => {
    assert.deepEqual(sellProductSelectionData.locations.RefugeeCamp.restore_operators, [
        "Laevatain",
        "Camille",
        "Antal",
        "Rossi",
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
