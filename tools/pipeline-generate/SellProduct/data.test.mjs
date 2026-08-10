import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";

import {
    sellProductLocations,
    sellProductLocationsNewestFirst,
    sellProductRegions,
    sellProductRegionsNewestFirst,
    settlementData,
    toPascalCase,
} from "./model.mjs";
import sellProductSellRows from "./sell-data.mjs";
import {sellProductSelectionData} from "./selection-data.mjs";
import {sellProductTaskRows} from "./task-data.mjs";

const root = sellProductTaskRows[0];

function readPipeline(url) {
    return JSON.parse(readFileSync(url, "utf8").replace(/^\s*\/\/.*$/gm, ""));
}

test("SellProduct 保留按星期执行入口与任务选项", () => {
    const task = readPipeline(new URL("../../../assets/tasks/SellProduct.json", import.meta.url));
    const pipeline = readPipeline(new URL("../../../assets/resource/pipeline/SellProduct.json", import.meta.url));

    assert.equal(task.task[0].entry, "SellProductSchedule");
    assert.deepEqual(task.task[0].option, [
        "SellProductSchedule",
        "SellProductOperatorAutoSwitch",
        "SellProductSelectionStrategy",
        "SellProductPriorityRules",
        "SellProductItemReserveRules",
        ...sellProductRegions.map((region) => `${region.RegionPrefix}Sell`),
    ]);
    assert.equal(task.option.SellProductSchedule.type, "checkbox");
    assert.equal(task.option.SellProductSchedule.cases.length, 7);
    assert.equal(pipeline.SellProductScheduleEnabled.recognition.param.custom_recognition, "ScheduleRecognition");
    assert.deepEqual(Object.keys(pipeline.SellProductScheduleEnabled.attach), [
        "monday",
        "tuesday",
        "wednesday",
        "thursday",
        "friday",
        "saturday",
        "sunday",
    ]);
    // Reserve reset 会清空货品配置；先重置货品与干员会话，再应用优先售卖和选品策略。
    assert.deepEqual(pipeline.SellProductPrepareSession.custom_action_param.sub, [
        "SellProductInitializeReserveSession",
        "SellProductInitializeOperatorSession",
        "SellProductConfigurePrioritySession",
        "SellProductConfigureSelectionStrategy",
    ]);
});

test("SellProduct 选品策略支持稀有度、单价和库存优先", () => {
    const task = readPipeline(new URL("../../../assets/tasks/SellProduct.json", import.meta.url));
    const selectionStrategy = task.option.SellProductSelectionStrategy;
    assert.equal(selectionStrategy.type, "select");
    assert.equal(selectionStrategy.default_case, "Rarity");
    assert.deepEqual(
        selectionStrategy.cases.map((itemCase) => itemCase.name),
        [
            "Rarity",
            "Price",
            "Stock",
        ],
    );
    const rarity = selectionStrategy.cases.find((itemCase) => itemCase.name === "Rarity");
    assert.deepEqual(rarity.pipeline_override.SellProductConfigureSelectionStrategy.custom_action_param, {
        operation: "configure_strategy",
        strategy: "rarity",
    });
    const price = selectionStrategy.cases.find((itemCase) => itemCase.name === "Price");
    assert.deepEqual(price.pipeline_override.SellProductConfigureSelectionStrategy.custom_action_param, {
        operation: "configure_strategy",
        strategy: "price",
    });
    const stock = selectionStrategy.cases.find((itemCase) => itemCase.name === "Stock");
    assert.deepEqual(stock.option, ["SellProductStockMinimumUnitPrice"]);
    assert.deepEqual(stock.pipeline_override.SellProductConfigureSelectionStrategy.custom_action_param, {
        operation: "configure_strategy",
        strategy: "stock",
        minimum_unit_price: 10,
    });
    assert.equal(root.StockMinimumUnitPriceDefault, "10");
    assert.equal(task.option.SellProductStockMinimumUnitPrice.inputs[0].default, "10");
    assert.deepEqual(
        task.option.SellProductStockMinimumUnitPrice.pipeline_override.SellProductConfigureSelectionStrategy
            .custom_action_param,
        {
            operation: "configure_strategy",
            strategy: "stock",
            minimum_unit_price: "{SellProductStockMinimumUnitPrice}",
        },
    );
});

test("SellProduct 按固定地区顺序售卖且不再使用自动起始地区", () => {
    const loop = readPipeline(new URL("../../../assets/resource/pipeline/SellProduct/Loop.json", import.meta.url));
    const entry = readPipeline(new URL("../../../assets/resource/pipeline/SellProduct.json", import.meta.url));

    assert.deepEqual(loop.SellProductLoop.next, [
        ...sellProductRegionsNewestFirst.map((region) => `SellProduct${region.RegionPrefix}`),
        "SellProductTaskEnd",
    ]);
    assert.equal(entry.SellProductLoop, undefined);
    assert.deepEqual(
        Object.keys(entry).filter((nodeName) => nodeName.startsWith("SellProductAuto")),
        [],
    );
});

test("SellProduct 优先售卖新地区且地区内保持游戏顺序", () => {
    assert.deepEqual(sellProductRegionsNewestFirst, [...sellProductRegions].reverse());
    for (const region of sellProductRegionsNewestFirst) {
        assert.deepEqual(
            sellProductLocationsNewestFirst
                .filter((location) => location.RegionPrefix === region.RegionPrefix)
                .map((location) => location.LocationId),
            region.LocationIds,
        );
    }

    const regionOrder = sellProductRegionsNewestFirst.map((region) => region.RegionPrefix);
    assert.ok(regionOrder.indexOf("Wuling") < regionOrder.indexOf("ValleyIV"));
});

test("SellProduct 强制刷新选项为 PC 与 ADB 使用相同的当前干员 ROI", () => {
    const enabledCase = root.OperatorRefreshModeCases.find((itemCase) => itemCase.name === "Yes");
    assert.ok(enabledCase);
    for (const location of sellProductLocations) {
        assert.equal(
            enabledCase.pipeline_override[`SellProduct${location.LocationId}CurrentTargetOperator`],
            undefined,
        );
        assert.equal(
            enabledCase.pipeline_override[`SellProduct${location.LocationId}CurrentRestoreOperator`],
            undefined,
        );
    }
    const adbTemplate = readFileSync(new URL("./pipeline-adb-template.jsonc", import.meta.url), "utf8");
    assert.doesNotMatch(adbTemplate, /CurrentOperatorROI/);
});

test("SellProduct region entry rows contain every generated location", () => {
    assert.deepEqual(
        sellProductSellRows.map((row) => row.RegionPrefix),
        sellProductRegions.map((region) => region.RegionPrefix),
    );

    for (const row of sellProductSellRows) {
        const region = sellProductRegions.find((entry) => entry.RegionPrefix === row.RegionPrefix);
        const outpostNext = region.LocationIds.map((locationId) => `[JumpBack]SellProduct${locationId}`).concat(
            "SellProductLoop",
            "[JumpBack]SceneEnterMenuRegionalDevelopment",
        );
        assert.deepEqual(row.SellNext, [`SellProduct${region.RegionPrefix}InitializePrioritySession`]);
        assert.deepEqual(row.PrepareNext, outpostNext);
    }
});

test("SellProduct location IDs are derived from the current upstream English names", () => {
    for (const location of sellProductLocations) {
        const settlement = settlementData.settlements[location.SettlementId];
        assert.equal(location.LocationId, toPascalCase(settlement.names.en_us || location.SettlementId));
    }
});

test("SellProduct reserve rules only expand independent item slots", () => {
    const enabledCase = root.ReserveRuleSwitchCases.find((itemCase) => itemCase.name === "Yes");
    assert.deepEqual(enabledCase.option, [
        "SellProductReserveItem1",
        "SellProductReserveItem2",
        "SellProductReserveItem3",
        "SellProductReserveItem4",
        "SellProductReserveItem5",
        "SellProductReserveItem6",
    ]);
    assert.equal(enabledCase.pipeline_override, undefined);
});

test("SellProduct 优先总开关展开地区配置且不耦合地区售卖开关", () => {
    const enabledCase = root.PriorityRuleSwitchCases.find((itemCase) => itemCase.name === "Yes");
    assert.deepEqual(enabledCase.option, [
        "SellProductOnlyPreferredItems",
        ...sellProductRegions.map((region) => `SellProduct${region.RegionPrefix}PriorityRules`),
    ]);
    assert.deepEqual(enabledCase.pipeline_override.SellProductConfigurePrioritySession.custom_action_param, {
        operation: "configure",
        enabled: true,
    });
    const disabledCase = root.PriorityRuleSwitchCases.find((itemCase) => itemCase.name === "No");
    assert.equal(disabledCase.option, undefined);

    const onlyPreferredCase = root.OnlyPreferredSwitchCases.find((itemCase) => itemCase.name === "Yes");
    assert.deepEqual(onlyPreferredCase.pipeline_override.SellProductConfigurePrioritySession.custom_action_param, {
        operation: "configure",
        enabled: true,
        only_preferred: true,
    });

    for (const region of sellProductRegions) {
        const regionPrefix = region.RegionPrefix;
        const regionRoot = sellProductTaskRows.find((row) => row.RegionPrefix === regionPrefix);
        assert.ok(regionRoot, `missing task row for region ${regionPrefix}`);
        const regionEnabledCase = regionRoot.RegionPriorityRuleSwitchCases.find((itemCase) => itemCase.name === "Yes");
        assert.deepEqual(
            regionEnabledCase.option,
            [
                1,
                2,
                3,
                4,
                5,
                6,
            ].map((slot) => `SellProduct${regionPrefix}PriorityItem${slot}`),
        );
        assert.deepEqual(
            regionEnabledCase.pipeline_override[`SellProduct${regionPrefix}InitializePrioritySession`]
                .custom_action_param,
            {
                operation: "reset_preferred",
                enabled: true,
            },
        );

        const regionItems = sellProductLocations
            .filter((location) => location.RegionPrefix === regionPrefix)
            .flatMap((location) => sellProductSelectionData.locations[location.LocationId].items);
        const regionItemIDs = new Set(regionItems.map((item) => item.item_id));
        const priceByItemID = new Map();
        for (const item of regionItems) {
            priceByItemID.set(item.item_id, Math.max(priceByItemID.get(item.item_id) ?? 0, item.unit_price));
        }
        for (const slot of [
            1,
            2,
            3,
            4,
            5,
            6,
        ]) {
            const cases = regionRoot[`PriorityItemCases${slot}`];
            const noneCase = cases.find((entry) => entry.name === "None");
            assert.ok(noneCase, `${regionPrefix} slot ${slot} missing None case`);
            assert.equal(noneCase.pipeline_override, undefined);

            const itemCases = cases.filter((entry) => entry.name !== "None");
            assert.ok(itemCases.length > 0, `${regionPrefix} slot ${slot} has no selectable items`);
            const itemPrices = itemCases.map((itemCase) => {
                const registration =
                    itemCase.pipeline_override[`SellProduct${regionPrefix}RegisterPriorityItem${slot}`];
                return priceByItemID.get(registration.custom_action_param.item_id);
            });
            assert.deepEqual(
                itemPrices,
                [...itemPrices].sort((left, right) => right - left),
                `${regionPrefix} slot ${slot} items are not sorted by unit price descending`,
            );
            for (const itemCase of itemCases) {
                const registration =
                    itemCase.pipeline_override[`SellProduct${regionPrefix}RegisterPriorityItem${slot}`];
                assert.equal(registration.enabled, undefined);
                assert.equal(registration.custom_action_param.operation, "register");
                assert.ok(registration.custom_action_param.item_id.startsWith("item_"));
                assert.ok(regionItemIDs.has(registration.custom_action_param.item_id));
            }
        }
    }
});

test("SellProduct 武陵优先物品顺序与游戏货架一致", () => {
    const regionPrefix = "Wuling";
    const regionRoot = sellProductTaskRows.find((row) => row.RegionPrefix === regionPrefix);
    const itemIDs = regionRoot.PriorityItemCases1.filter((entry) => entry.name !== "None").map(
        (entry) => entry.pipeline_override.SellProductWulingRegisterPriorityItem1.custom_action_param.item_id,
    );
    const observedSkyKingOrder = [
        "item_proc_battery_5",
        "item_copper_enr_cmpt",
        "item_xiranite_enr_powder",
        "item_proc_battery_4",
        "item_bottled_rec_hp_5",
        "item_bottled_food_5",
        "item_bottled_food_4",
    ];
    const observedItems = new Set(observedSkyKingOrder);
    assert.deepEqual(
        itemIDs.filter((itemID) => observedItems.has(itemID)),
        observedSkyKingOrder,
    );
});

test("SellProduct 四号谷地同价优先物品保留游戏货架顺序", () => {
    const regionRoot = sellProductTaskRows.find((row) => row.RegionPrefix === "ValleyIV");
    const itemIDs = regionRoot.PriorityItemCases1.filter((entry) => entry.name !== "None").map(
        (entry) => entry.pipeline_override.SellProductValleyIVRegisterPriorityItem1.custom_action_param.item_id,
    );
    const expectedOrder = [
        "item_bottled_rec_hp_1",
        "item_bottled_food_1",
        "item_crystal_shell",
        "item_glass_cmpt",
        "item_iron_cmpt",
    ];
    const expectedItems = new Set(expectedOrder);
    assert.deepEqual(
        itemIDs.filter((itemID) => expectedItems.has(itemID)),
        expectedOrder,
    );
});

test("SellProduct 保留物品按游戏货架单价降序排列", () => {
    const priceByItemID = new Map();
    for (const location of Object.values(sellProductSelectionData.locations)) {
        for (const item of location.items) {
            priceByItemID.set(item.item_id, Math.max(priceByItemID.get(item.item_id) ?? 0, item.unit_price));
        }
    }
    for (const slot of [
        1,
        2,
        3,
        4,
        5,
        6,
    ]) {
        const itemCases = root[`ReserveItemCases${slot}`].filter((entry) => entry.name !== "None");
        const itemPrices = itemCases.map((itemCase) => {
            const registration = itemCase.pipeline_override[`SellProductRegisterReserveRule${slot}`];
            return priceByItemID.get(registration.attach.item_id);
        });
        assert.deepEqual(
            itemPrices,
            [...itemPrices].sort((left, right) => right - left),
            `reserve slot ${slot} items are not sorted by unit price descending`,
        );
    }

    const itemIDs = root.ReserveItemCases1.filter((entry) => entry.name !== "None").map(
        (entry) => entry.pipeline_override.SellProductRegisterReserveRule1.attach.item_id,
    );
    for (const expectedOrder of [
        [
            "item_copper_enr2_cmpt",
            "item_bottled_rec_hp_3",
            "item_proc_battery_3",
            "item_bottled_food_3",
        ],
        [
            "item_bottled_rec_hp_1",
            "item_bottled_food_1",
        ],
        [
            "item_filter_core",
            "item_crystal_shell",
            "item_glass_cmpt",
            "item_iron_cmpt",
        ],
    ]) {
        const expectedItems = new Set(expectedOrder);
        assert.deepEqual(
            itemIDs.filter((itemID) => expectedItems.has(itemID)),
            expectedOrder,
        );
    }
});

test("SellProduct concrete reserve rule separates itemId attach from handling mode", () => {
    const itemCase = root.ReserveItemCases1.find((entry) => entry.name !== "None");
    assert.ok(itemCase);
    assert.deepEqual(itemCase.option, ["SellProductReserveItem1Mode"]);
    const registration = itemCase.pipeline_override.SellProductRegisterReserveRule1;
    assert.equal(registration.enabled, undefined);
    assert.ok(registration.attach.item_id.startsWith("item_"));
    assert.equal(registration.custom_action_param, undefined);

    const quantityCase = root.ReserveModeCases1.find((entry) => entry.name === "Quantity");
    assert.deepEqual(quantityCase.option, ["SellProductReserveItem1Value"]);
    assert.equal(quantityCase.pipeline_override, undefined);
    const neverSellCase = root.ReserveModeCases1.find((entry) => entry.name === "NeverSell");
    assert.deepEqual(neverSellCase.pipeline_override.SellProductRegisterReserveRule1.custom_action_param, {
        operation: "register",
        quantity: -1,
    });
});

test("SellProduct reserve None case does not register a rule", () => {
    const noneCase = root.ReserveItemCases1.find((entry) => entry.name === "None");
    assert.ok(noneCase);
    assert.equal(noneCase.pipeline_override, undefined);
});

test("SellProduct registration slots form an always-enabled no-op chain", () => {
    const pipeline = readPipeline(
        new URL("../../../assets/resource/pipeline/SellProduct/ReserveSession.json", import.meta.url),
    );
    const chain = [
        "SellProductInitializeReserveSession",
        "SellProductRegisterReserveRule1",
        "SellProductRegisterReserveRule2",
        "SellProductRegisterReserveRule3",
        "SellProductRegisterReserveRule4",
        "SellProductRegisterReserveRule5",
        "SellProductRegisterReserveRule6",
    ];

    for (let index = 0; index < chain.length - 1; index += 1) {
        const node = pipeline[chain[index]];
        assert.ok(node, `missing registration node ${chain[index]}`);
        assert.equal(node.enabled, undefined);
        assert.deepEqual(node.next, [chain[index + 1]]);
    }

    // 保留规则链终止于 Rule6；其余初始化阶段由 SellProductPrepareSession 按依赖顺序调度。
    assert.equal(pipeline.SellProductRegisterReserveRule6.next, undefined);
    assert.equal(pipeline.SellProductConfigurePrioritySession.enabled, undefined);
    assert.equal(pipeline.SellProductConfigurePrioritySession.next, undefined);

    const entry = readPipeline(new URL("../../../assets/resource/pipeline/SellProduct.json", import.meta.url));
    assert.deepEqual(entry.SellProductPrepareSession.custom_action_param.sub, [
        "SellProductInitializeReserveSession",
        "SellProductInitializeOperatorSession",
        "SellProductConfigurePrioritySession",
        "SellProductConfigureSelectionStrategy",
    ]);
});

test("SellProduct 每次进入地区都会切换对应的优先售卖表", () => {
    for (const region of sellProductRegions) {
        const pipeline = readPipeline(
            new URL(
                `../../../assets/resource/pipeline/SellProduct/${region.RegionPrefix}/SellProduct${region.RegionPrefix}.json`,
                import.meta.url,
            ),
        );
        const chain = [
            `SellProduct${region.RegionPrefix}InitializePrioritySession`,
            ...[
                1,
                2,
                3,
                4,
                5,
                6,
            ].map((slot) => `SellProduct${region.RegionPrefix}RegisterPriorityItem${slot}`),
            `SellProduct${region.RegionPrefix}PrepareOperatorCache`,
        ];
        assert.deepEqual(pipeline[`SellProduct${region.RegionPrefix}Sell`].next, [chain[0]]);
        assert.equal(pipeline[chain[0]].custom_action_param.operation, "reset_preferred");
        assert.equal(pipeline[chain[0]].custom_action_param.enabled, false);
        for (let index = 0; index < chain.length - 1; index += 1) {
            assert.deepEqual(pipeline[chain[index]].next, [chain[index + 1]]);
        }
    }
});

test("SellProduct operator locations form an always-enabled active-flag chain", () => {
    const scan = readPipeline(
        new URL("../../../assets/resource/pipeline/SellProduct/OperatorScan.json", import.meta.url),
    );
    const session = readPipeline(
        new URL("../../../assets/resource/pipeline/SellProduct/OperatorSession.json", import.meta.url),
    );
    const pipeline = {...scan, ...session};
    const registrationNodes = sellProductLocations.map(
        (location) => `SellProductRegisterLocation${location.LocationId}`,
    );
    const chain = [
        "SellProductInitializeOperatorSession",
        ...registrationNodes,
        "SellProductOperatorSessionReady",
    ];

    for (let index = 0; index < chain.length - 1; index += 1) {
        const node = pipeline[chain[index]];
        assert.ok(node, `missing operator registration node ${chain[index]}`);
        assert.equal(node.enabled, undefined);
        assert.deepEqual(node.next, [chain[index + 1]]);
    }
    for (const nodeName of registrationNodes) {
        assert.equal(pipeline[nodeName].custom_action_param.active, false);
    }

    const task = readPipeline(new URL("../../../assets/tasks/SellProduct.json", import.meta.url));
    for (const location of sellProductLocations) {
        const option = task.option[`${location.RegionPrefix}${location.LocationId}`];
        const enabledCase = option.cases.find((itemCase) => itemCase.name === "Yes");
        const disabledCase = option.cases.find((itemCase) => itemCase.name === "No");
        const nodeName = `SellProductRegisterLocation${location.LocationId}`;
        assert.deepEqual(enabledCase.pipeline_override[nodeName].custom_action_param, {
            operation: "register",
            location: location.LocationId,
            active: true,
        });
        assert.equal(disabledCase.pipeline_override[nodeName], undefined);
    }
});

test("SellProduct operator switching uses shared core dispatch nodes", () => {
    const core = readPipeline(new URL("../../../assets/resource/pipeline/SellProduct/SellCore.json", import.meta.url));
    assert.deepEqual(core.SellProductSellMain.next, ["SellProductBeforeSellOperator"]);
    assert.deepEqual(core.SellProductBeforeSellOperator.next, [
        "[Anchor]SellProductBeforeSellOperatorTarget",
    ]);
    assert.deepEqual(core.SellProductSellLoopEnd.next, ["SellProductAfterSellOperator"]);
    assert.deepEqual(core.SellProductAfterSellOperator.next, [
        "[Anchor]SellProductAfterSellOperatorTarget",
    ]);

    for (const location of sellProductLocations) {
        const pipeline = readPipeline(
            new URL(
                `../../../assets/resource/pipeline/SellProduct/${location.RegionPrefix}/${location.LocationId}.json`,
                import.meta.url,
            ),
        );
        const prefix = `SellProduct${location.LocationId}`;
        assert.deepEqual(pipeline[`${prefix}Sell`].next, [
            `${prefix}OutpostProsperityAvailable`,
            `${prefix}OutpostProsperityMax`,
        ]);
        assert.deepEqual(pipeline[`${prefix}OutpostProsperityAvailable`].next, [
            `${prefix}ReportLocationPlan`,
        ]);
        assert.deepEqual(pipeline[`${prefix}OutpostProsperityMax`].next, [
            `${prefix}ReportLocationPlan`,
        ]);
        assert.deepEqual(pipeline[`${prefix}ReportLocationPlan`].next, [
            `${prefix}SetOperatorAnchors`,
        ]);
        assert.deepEqual(pipeline[`${prefix}SelectPriorityItem`].next, [
            "SellProductSelectNewGoodConfirm",
        ]);
        assert.deepEqual(pipeline[`${prefix}PriorityItemsExhausted`].next, [
            "SellProductCloseGoodsAfterExhausted",
        ]);
        assert.deepEqual(pipeline[`${prefix}SetOperatorAnchors`].anchor, {
            SellProductBeforeSellOperatorTarget: `${prefix}BeforeSellOperator`,
            SellProductAfterSellOperatorTarget: `${prefix}AfterSellOperator`,
        });
        assert.deepEqual(pipeline[`${prefix}SetOperatorAnchors`].next, [
            "SellProductSellMain",
        ]);
        assert.equal(pipeline[`${prefix}SetBeforeSellOperatorAnchor`], undefined);
        assert.equal(pipeline[`${prefix}SetAfterSellOperatorAnchor`], undefined);
    }

    const task = readPipeline(new URL("../../../assets/tasks/SellProduct.json", import.meta.url));
    const disabledCase = task.option.SellProductOperatorAutoSwitch.cases.find((itemCase) => itemCase.name === "No");
    assert.deepEqual(disabledCase.pipeline_override.SellProductSellMain, {
        next: ["SellProductSellLoop"],
    });
    assert.deepEqual(disabledCase.pipeline_override.SellProductSellLoopEnd, {next: []});
    assert.deepEqual(disabledCase.pipeline_override.SellProductScanOperatorList, {next: []});
    assert.deepEqual(Object.keys(disabledCase.pipeline_override).sort(), [
        "SellProductScanOperatorList",
        "SellProductSellLoopEnd",
        "SellProductSellMain",
    ]);
});

test("SellProduct 选品库存识别只扫描第一页且不滑动货品列表", () => {
    const changeGoods = readPipeline(
        new URL("../../../assets/resource/pipeline/SellProduct/ChangeGoods.json", import.meta.url),
    );

    assert.deepEqual(changeGoods.SellProductChangeGoodsRelay.next, [
        "[Anchor]SellProductSelectPriorityItem",
        "[Anchor]SellProductPriorityItemsExhausted",
    ]);
    assert.equal(changeGoods.SellProductCheckGoodsCellAnchor.recognition, "TemplateMatch");
});

test("SellProduct 各平台通过 Pipeline 配置货品名称、库存和点击区域", () => {
    const win32 = readPipeline(
        new URL("../../../assets/resource/pipeline/SellProduct/ValleyIV/RefugeeCamp.json", import.meta.url),
    ).SellProductRefugeeCampSelectPriorityItem.custom_recognition_param;
    const adb = readPipeline(
        new URL("../../../assets/resource_adb/pipeline/SellProduct/ValleyIV/RefugeeCamp.json", import.meta.url),
    ).SellProductRefugeeCampSelectPriorityItem.custom_recognition_param;

    for (const [
        platform,
        params,
    ] of [
        [
            "Win32",
            win32,
        ],
        [
            "ADB",
            adb,
        ],
    ]) {
        assert.equal(params.location, "RefugeeCamp");
        assert.equal(params.result, "select");

        for (const name of [
            "stock_name_offset",
            "stock_quantity_offset",
            "stock_click_offset",
        ]) {
            const offset = params[name];
            assert.ok(Array.isArray(offset), `${platform} ${name} 应为数组`);
            assert.equal(offset.length, 4, `${platform} ${name} 应包含 4 个坐标值`);
            assert.ok(offset.every(Number.isInteger), `${platform} ${name} 应只包含整数`);
            assert.ok(offset[2] > 0, `${platform} ${name} 宽度应大于 0`);
            assert.ok(offset[3] > 0, `${platform} ${name} 高度应大于 0`);
        }
    }
});

test("SellProduct 每轮选货及保留交易后优先检查调度券不足", () => {
    const pipeline = readPipeline(
        new URL("../../../assets/resource/pipeline/SellProduct/SellCore.json", import.meta.url),
    );

    assert.deepEqual(pipeline.SellProductSellLoop.next, [
        "[Anchor]SellProductZeroMoneyHandler",
        "SellProductChangeGoods",
    ]);
    assert.deepEqual(pipeline.SellProductAtSell.next.slice(0, 2), [
        "[Anchor]SellProductZeroMoneyHandler",
        "SellProductZeroProductAfterChangeStillEmpty",
    ]);
    assert.equal(pipeline.SellProductSellCheckThenLoop.anchor.SellProductZeroMoneyHandler, "SellProductZeroMoney");
    assert.deepEqual(pipeline.SellProductSellCheckThenLoop.next, [
        "[Anchor]SellProductZeroMoneyHandler",
        "SellProductReserveQuantityReached",
        "[Anchor]SellProductBetterSliding",
    ]);
    assert.deepEqual(pipeline.SellProductZeroProductAfterChangeStillEmpty.next, [
        "[Anchor]SellProductMarkOutOfStock",
    ]);
});

test("SellProduct 调度券不足使用完整多语言文案并保留关键词兜底", () => {
    const pipeline = readPipeline(
        new URL("../../../assets/resource/pipeline/SellProduct/SellCore.json", import.meta.url),
    );

    assert.deepEqual(pipeline.SellProductZeroMoney.expected, [
        "当前据点调度券储量不足",
        "目前據點調度券存量不足",
        "Insufficient stock bills in current outpost",
        "(?i)Insufficient",
        "拠点取引券が不足しています",
        "거점 관리권 보유량 부족",
        "不足",
    ]);
});

test("SellProduct 交易完成后重复点击直到获得物品界面消失", () => {
    const pipeline = readPipeline(
        new URL("../../../assets/resource/pipeline/SellProduct/SellCore.json", import.meta.url),
    );

    assert.equal(pipeline.SellProductCheckHeader.recognition, "TemplateMatch");
    assert.deepEqual(pipeline.SellProductCheckHeader.template, [
        "SellProduct/SellProductCheckHeader.png",
    ]);
    assert.deepEqual(
        pipeline.SellProductCheckHeader.roi,
        [
            577,
            10,
            138,
            479,
        ],
    );

    for (const nodeName of [
        "SellProductSellCheck",
        "SellProductSellCheckThenLoop",
    ]) {
        const node = pipeline[nodeName];
        assert.deepEqual(node.all_of, ["SellProductCheckHeader"]);
        assert.equal(node.pre_wait_freezes, undefined);
        assert.equal(node.custom_action, "RepeatUntilNotFoundAction");
        assert.deepEqual(node.custom_action_param, {
            action: "Click",
            wait_node: "SellProductCheckHeader",
            repeat_count: 10,
            interval_ms: 200,
        });
        assert.deepEqual(
            node.target,
            [
                35,
                611,
                58,
                57,
            ],
        );
    }
});

test("SellProduct 缺货物品通过据点锚点标记并在本次任务内共享", () => {
    for (const location of sellProductLocations) {
        const pipeline = readPipeline(
            new URL(
                `../../../assets/resource/pipeline/SellProduct/${location.RegionPrefix}/${location.LocationId}.json`,
                import.meta.url,
            ),
        );
        const prefix = `SellProduct${location.LocationId}`;
        assert.equal(pipeline[`${prefix}Sell`].anchor.SellProductMarkOutOfStock, `${prefix}MarkOutOfStock`);
        assert.deepEqual(pipeline[`${prefix}MarkOutOfStock`].custom_action_param, {
            operation: "out_of_stock",
            location: location.LocationId,
        });
        assert.deepEqual(pipeline[`${prefix}MarkOutOfStock`].next, ["SellProductSellLoop"]);
    }
});

test("SellProduct 持续售卖到保留量后再进入下一轮选货", () => {
    const pipeline = readPipeline(
        new URL("../../../assets/resource/pipeline/SellProduct/SellCore.json", import.meta.url),
    );

    assert.deepEqual(pipeline.SellProductSellCheckThenLoop.next, [
        "[Anchor]SellProductZeroMoneyHandler",
        "SellProductReserveQuantityReached",
        "[Anchor]SellProductBetterSliding",
    ]);
    assert.equal(pipeline.SellProductReserveQuantityReached.enabled, false);
    assert.equal(pipeline.SellProductReserveQuantityReached.custom_action, "SellProductReserveSession");
    assert.deepEqual(pipeline.SellProductReserveQuantityReached.custom_action_param, {
        operation: "satisfy",
    });
    assert.deepEqual(pipeline.SellProductReserveQuantityReached.next, ["SellProductSellLoop"]);
    assert.equal(pipeline.SellProductReserveAlreadySatisfied.recognition, "DirectHit");
    assert.equal(pipeline.SellProductReserveAlreadySatisfied.custom_action, "SellProductReserveSession");
    assert.deepEqual(pipeline.SellProductReserveAlreadySatisfied.custom_action_param, {
        operation: "satisfy",
    });
    assert.deepEqual(pipeline.SellProductReserveAlreadySatisfied.next, ["SellProductSellLoop"]);

    for (const location of sellProductLocations) {
        const outpost = readPipeline(
            new URL(
                `../../../assets/resource/pipeline/SellProduct/${location.RegionPrefix}/${location.LocationId}.json`,
                import.meta.url,
            ),
        );
        assert.equal(
            outpost[`SellProduct${location.LocationId}BetterSliding`].custom_action_param.TargetReachableOverrideEnable,
            "SellProductReserveQuantityReached",
        );
    }
});

test("SellProduct 按启用据点边界处理已派驻干员冲突", () => {
    for (const location of sellProductLocations) {
        const pipeline = readPipeline(
            new URL(
                `../../../assets/resource/pipeline/SellProduct/${location.RegionPrefix}/${location.LocationId}.json`,
                import.meta.url,
            ),
        );
        const prefix = `SellProduct${location.LocationId}`;
        for (const [
            usageName,
            usage,
        ] of [
            [
                "Target",
                "target",
            ],
            [
                "Restore",
                "restore",
            ],
        ]) {
            const managed = pipeline[`${prefix}${usageName}OperatorManagedConflict`];
            const protectedConflict = pipeline[`${prefix}${usageName}OperatorProtectedConflict`];
            const expectedParam = {
                usage,
                location: location.LocationId,
            };

            assert.equal(managed.recognition, "And");
            assert.equal(managed.all_of[0], "SellProductOperatorAlreadyAssignedPrompt");
            assert.deepEqual(managed.all_of[1], {
                recognition: "Custom",
                roi: [
                    240,
                    260,
                    800,
                    90,
                ],
                custom_recognition: "SellProductOperatorConflict",
                custom_recognition_param: {result: "managed", ...expectedParam},
            });
            assert.equal(managed.all_of[2], "YellowConfirmButtonType1");
            assert.equal(managed.box_index, 2);

            assert.equal(protectedConflict.recognition, "And");
            assert.equal(protectedConflict.all_of[0], "SellProductOperatorAlreadyAssignedPrompt");
            assert.deepEqual(protectedConflict.all_of[1], {
                recognition: "Custom",
                roi: [
                    240,
                    260,
                    800,
                    90,
                ],
                custom_recognition: "SellProductOperatorConflict",
                custom_recognition_param: {result: "protected", ...expectedParam},
            });
            assert.equal(protectedConflict.all_of[2], "CancelButton");

            const close = pipeline[`${prefix}Close${usageName}OperatorLiaison`];
            assert.deepEqual(close.all_of, [
                "SellProductInOperatorLiaison",
                "SellProductCheckWithdrawText",
                "CloseButtonType1",
            ]);
            assert.equal(close.box_index, 2);

            const confirm = pipeline[`${prefix}Confirm${usageName}Operator`];
            assert.equal(confirm.recognition, "And");
            assert.equal(confirm.all_of[0], "SellProductCheckAssignText");
            assert.equal(confirm.all_of[1], "WhiteConfirmButtonType1");
            assert.equal(confirm.box_index, 1);
            assert.equal(confirm.target_offset, undefined);
        }
    }
});

test("SellProduct generated outpost nodes report task-level runtime state changes", () => {
    for (const location of sellProductLocations) {
        const pipeline = readPipeline(
            new URL(
                `../../../assets/resource/pipeline/SellProduct/${location.RegionPrefix}/${location.LocationId}.json`,
                import.meta.url,
            ),
        );
        const prefix = `SellProduct${location.LocationId}`;

        assert.deepEqual(pipeline[`${prefix}OutpostProsperityAvailable`].custom_action_param, {
            operation: "enter_location",
            location: location.LocationId,
            outpost_prosperity_max: false,
        });
        assert.deepEqual(pipeline[`${prefix}OutpostProsperityMax`].custom_action_param, {
            operation: "enter_location",
            location: location.LocationId,
            outpost_prosperity_max: true,
        });
        assert.deepEqual(pipeline[`${prefix}ReportLocationPlan`].custom_action_param, {
            location: location.LocationId,
        });
        assert.deepEqual(pipeline[`${prefix}CurrentTargetOperator`].custom_action_param, {
            operation: "complete_target",
            location: location.LocationId,
            changed: false,
        });
        assert.deepEqual(pipeline[`${prefix}TargetOperatorDone`].custom_action_param, {
            operation: "complete_target",
            location: location.LocationId,
            changed: true,
        });
        assert.deepEqual(pipeline[`${prefix}CurrentRestoreOperator`].custom_action_param, {
            operation: "complete_restore",
            location: location.LocationId,
            changed: false,
        });
        assert.deepEqual(pipeline[`${prefix}RestoreOperatorDone`].custom_action_param, {
            operation: "complete_restore",
            location: location.LocationId,
            changed: true,
        });
    }
});
