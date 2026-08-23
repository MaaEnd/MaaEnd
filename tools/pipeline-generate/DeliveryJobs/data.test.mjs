import assert from "node:assert/strict";
import {existsSync, readdirSync, readFileSync} from "node:fs";
import {join, relative} from "node:path";
import test from "node:test";
import {fileURLToPath} from "node:url";

import {parse} from "jsonc-parser";

import {DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT, deliveryJobDepots, deliveryJobRegions} from "./model.mjs";

function readJsonc(path) {
    return parse(readFileSync(path, "utf8"));
}

function readGeneratedPipeline(...segments) {
    return readJsonc(new URL(`../../../assets/resource/pipeline/${segments.join("/")}`, import.meta.url));
}

function readAdbPipeline(...segments) {
    return readJsonc(new URL(`../../../assets/resource_adb/pipeline/${segments.join("/")}`, import.meta.url));
}

function readGeneratedTask() {
    return readJsonc(new URL("../../../assets/tasks/DeliveryJobs.json", import.meta.url));
}

function getDepotModeContext(task, depot) {
    const option = task.option[depot.Id];
    return {
        option,
        byName: Object.fromEntries(
            option.cases.map((mode) => [
                mode.name,
                mode,
            ]),
        ),
        deliveryNode: `DeliveryJobsEnter${depot.Id}DeliveryJob`,
        cargoNode: `DeliveryJobsEnter${depot.Id}Cargo`,
        cargoCheckNode: `DeliveryJobsCheck${depot.Id}Cargo`,
    };
}

test("DeliveryJobs model has unique regions and depots", () => {
    assert.equal(new Set(deliveryJobRegions.map((region) => region.Id)).size, deliveryJobRegions.length);
    assert.equal(new Set(deliveryJobDepots.map((depot) => depot.Id)).size, deliveryJobDepots.length);
    assert.deepEqual(
        deliveryJobRegions.flatMap((region) => region.Depots),
        deliveryJobDepots.map((depot) => depot.Id),
    );
    assert.deepEqual(
        deliveryJobRegions.map((region) => region.Id),
        [
            "ValleyIV",
            "Wuling",
        ],
    );
    assert.deepEqual(
        deliveryJobDepots.map((depot) => depot.Id),
        [
            "OriginiumSciencePark",
            "OriginLodespring",
            "PowerPlateau",
            "WulingCity",
            "TestArea",
        ],
    );
    const iconRecognitionItems = readJsonc(
        new URL("../../../assets/data/IconRecognition/recognition_items.json", import.meta.url),
    );
    for (const region of deliveryJobRegions) {
        assert.ok(region.Depots.length > 0, `${region.Id} must contain at least one depot`);
        assert.ok(region.FillItems.some((item) => item.Id === region.DefaultFillItem));
        assert.ok(region.FillItems.length > 0, `${region.Id} must contain at least one fill item`);
        for (const item of region.FillItems) {
            const catalogEntry = iconRecognitionItems[item.ItemId];
            assert.ok(catalogEntry, `${region.Id} item ${item.ItemId} missing from IconRecognition catalog`);
            assert.equal(item.RecheckFilter, `${catalogEntry.storageKind}:${catalogEntry.categoryType}`);
            assert.equal(item.Label, `$iconRecognition.name.${item.ItemId}`);
        }
    }
});

test("DeliveryJobs offers transferable equipment components in Valley IV", () => {
    const valleyIV = deliveryJobRegions.find((region) => region.Id === "ValleyIV");
    assert.ok(valleyIV);
    const itemIds = new Set(valleyIV.FillItems.map((item) => item.Id));
    for (const itemId of [
        "item_equip_script_4",
        "item_equip_script_4_1",
        "item_equip_script_4_2",
        "item_equip_script_4_3",
    ]) {
        assert.ok(itemIds.has(itemId), `${itemId} must be available after transfer to Valley IV`);
    }
    assert.equal(itemIds.has("item_fertilize_1"), false);
    assert.equal(itemIds.has("item_fertilize_2"), false);
});

test("DeliveryJobs generated region loops cover every depot in stable order", () => {
    for (const region of deliveryJobRegions) {
        const pipeline = readGeneratedPipeline("DeliveryJobs", "Region", `${region.Id}.json`);
        assert.deepEqual(pipeline[`DeliveryJobs${region.Id}Loop`].next, [
            ...region.Depots.map((depotId) => `[JumpBack]DeliveryJobsEnter${depotId}DeliveryJob`),
            ...region.Depots.map((depotId) => `[JumpBack]DeliveryJobsEnter${depotId}Cargo`),
            "DeliveryJobsLoop",
            "[JumpBack]SceneEnterMenuRegionalDevelopment",
        ]);
        assert.deepEqual(pipeline[`DeliveryJobsIn${region.Id}LocalDepotNode`].all_of, [
            "InLocalDepotNode",
            `DeliveryJobsCheckLocalDepotNode${region.Depots[0]}Text`,
        ]);
        assert.deepEqual(pipeline[`DeliveryJobsSelectPriorityItems${region.Id}`].next, [
            "DeliveryJobsCargoFillToMax",
        ]);
    }
});

test("DeliveryJobs generated depot nodes enter the shared transfer and cargo flows", () => {
    for (const depot of deliveryJobDepots) {
        const pipeline = readGeneratedPipeline("DeliveryJobs", "Depot", depot.RegionId, `${depot.Id}.json`);
        assert.deepEqual(pipeline[`DeliveryJobsEnter${depot.Id}DeliveryJob`].next, [
            "DeliveryJobsClickTransferJob",
        ]);
        assert.equal(pipeline[`DeliveryJobsEnter${depot.Id}DeliveryJob`].max_hit, 1);
        assert.equal(pipeline[`DeliveryJobsEnter${depot.Id}Cargo`].max_hit, 1);
        assert.deepEqual(pipeline[`DeliveryJobsEnter${depot.Id}Cargo`].anchor, {
            DeliveryJobsSelectPriorityItems: `DeliveryJobsSelectPriorityItems${depot.RegionId}`,
            DeliveryJobsRedistributionBidAction: "DeliveryJobsRedistributionBidNextStep",
            DeliveryJobsOngoingDeliveryAction: "DeliveryJobsStopForOngoingDelivery",
            DeliveryJobsGoToDepot: depot.DepotScene,
        });
        assert.deepEqual(pipeline[`DeliveryJobsEnter${depot.Id}Cargo`].next, [
            "DeliveryJobsPackCargo",
        ]);
        assert.deepEqual(pipeline[`DeliveryJobsReturnAndTransfer${depot.Id}`].custom_action_param.sub, [
            depot.DepotScene,
        ]);
        assert.deepEqual(pipeline[`DeliveryJobsEnter${depot.Id}PriceDeliveryJob`].next, [
            "DeliveryJobsClickTransferJob",
        ]);
    }
});

test("DeliveryJobs generated quote nodes compare the threshold and route the selected action", () => {
    for (const depot of deliveryJobDepots) {
        const pipeline = readGeneratedPipeline("DeliveryJobs", "Depot", depot.RegionId, `${depot.Id}.json`);
        assert.deepEqual(pipeline[`DeliveryJobsDecide${depot.Id}Quote`].next, [
            `DeliveryJobs${depot.Id}QuoteAtLeastMinimum`,
            `DeliveryJobs${depot.Id}QuoteBelowMinimum`,
            "DeliveryJobsBidPriceRecognitionFailed",
        ]);
        assert.deepEqual(pipeline[`DeliveryJobs${depot.Id}QuoteAtLeastMinimum`].custom_recognition_param, {
            expression: "{DeliveryJobsSelectedBidPrice}>=119000",
        });
        assert.deepEqual(pipeline[`DeliveryJobs${depot.Id}QuoteBelowMinimum`].custom_recognition_param, {
            expression: "{DeliveryJobsSelectedBidPrice}<119000",
        });
        assert.deepEqual(pipeline[`DeliveryJobs${depot.Id}QuoteAtLeastMinimum`].anchor, {
            DeliveryJobsQuoteAction: "DeliveryJobsQuoteTransferJob",
            DeliveryJobsGoToDepot: `DeliveryJobsReturnAndTransfer${depot.Id}`,
        });
        assert.deepEqual(pipeline[`DeliveryJobs${depot.Id}QuoteBelowMinimum`].anchor, {
            DeliveryJobsQuoteAction: "DeliveryJobsQuoteAcceptJobOnly",
            DeliveryJobsGoToDepot: depot.DepotScene,
        });
        for (const comparison of [
            "AtLeastMinimum",
            "BelowMinimum",
        ]) {
            assert.deepEqual(pipeline[`DeliveryJobs${depot.Id}Quote${comparison}`].next, [
                "[Anchor]DeliveryJobsQuoteAction",
            ]);
        }
    }
});

test("DeliveryJobs task registers region switches and the shared packing option", () => {
    const task = readGeneratedTask();
    assert.deepEqual(task.task[0].option, [
        ...deliveryJobRegions.map((region) => region.Id),
        "PackCargoSelectItem",
    ]);
    assert.equal(task.option.DeliveryJobsAcceptJobOnly, undefined);
    assert.equal(task.option.DeliveryJobsPackCargoOnly, undefined);
});

test("DeliveryJobs packing item options inject IconRecognition item ids", () => {
    const task = readGeneratedTask();
    const enabledCase = task.option.PackCargoSelectItem.cases.find((itemCase) => itemCase.name === "Yes");
    assert.deepEqual(
        enabledCase.option,
        deliveryJobRegions.map((region) => `FillItemPriorities${region.Id}`),
    );
    assert.deepEqual(enabledCase.pipeline_override.DeliveryJobsSelectTypeOfGoodsToPackNextStep.next, [
        "[Anchor]DeliveryJobsSelectPriorityItems",
    ]);

    for (const region of deliveryJobRegions) {
        const regionOption = task.option[`FillItemPriorities${region.Id}`];
        assert.equal(regionOption.type, "switch");
        assert.equal(regionOption.label, `$global.region.${region.Id}`);
        assert.equal(regionOption.description, "$task.DeliveryJobs.FillItemPriorityRegionDescription");
        assert.equal(regionOption.default_case, "Yes");
        const regionEnabledCase = regionOption.cases.find((itemCase) => itemCase.name === "Yes");
        assert.deepEqual(
            regionEnabledCase.option,
            Array.from(
                {length: DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT},
                (_, index) => `WhatToFill${region.Id}Priority${index + 1}`,
            ),
        );
        assert.deepEqual(regionEnabledCase.pipeline_override[`DeliveryJobsSelectPriorityItems${region.Id}`].next, [
            `DeliveryJobsStartFill${region.Id}Priority1`,
        ]);
        for (let priority = 1; priority <= DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT; priority += 1) {
            const optionName = `WhatToFill${region.Id}Priority${priority}`;
            const option = task.option[optionName];
            assert.equal(option.type, "select");
            assert.equal(option.label, `$task.DeliveryJobs.WhatToFill${region.Id}Priority${priority}`);
            assert.equal(option.cases.length, region.FillItems.length + (priority === 1 ? 0 : 1));
            assert.equal(option.default_case, priority === 1 ? region.DefaultFillItem : "None");
            if (priority > 1) {
                assert.deepEqual(option.cases[0], {
                    name: "None",
                    label: "$task.DeliveryJobs.FillItemPriorityNone",
                });
            }
            const itemCases = priority === 1 ? option.cases : option.cases.slice(1);
            for (const [
                index,
                item,
            ] of region.FillItems.entries()) {
                const optionCase = itemCases[index];
                assert.equal(optionCase.name, item.Id);
                assert.equal(optionCase.label, item.Label);
                assert.equal(
                    optionCase.pipeline_override[`DeliveryJobsStartFill${region.Id}Priority${priority}`].enabled,
                    true,
                );
                const selectionOverride =
                    optionCase.pipeline_override[`DeliveryJobsSelectItemToFill${region.Id}Priority${priority}`];
                assert.equal(selectionOverride.enabled, true);
                assert.deepEqual(selectionOverride.custom_recognition_param, {
                    grid_type: "shipment",
                    item_ids: [
                        item.ItemId,
                    ],
                    item_recheck_filters: [
                        item.RecheckFilter,
                    ],
                    deduplicate: true,
                });
            }
        }
    }
});

test("DeliveryJobs task orders five independent modes for every depot", () => {
    const task = readGeneratedTask();
    for (const depot of deliveryJobDepots) {
        const {option} = getDepotModeContext(task, depot);
        assert.equal(option.type, "select");
        assert.equal(option.default_case, "Transfer");
        assert.deepEqual(
            option.cases.map((mode) => mode.name),
            [
                "Transfer",
                "ByQuote",
                "AcceptJobOnly",
                "PackCargoOnly",
                "Disabled",
            ],
        );
    }
});

test("DeliveryJobs ordinary depot modes override delivery and cargo behavior", () => {
    const task = readGeneratedTask();
    for (const depot of deliveryJobDepots) {
        const {byName, deliveryNode, cargoNode, cargoCheckNode} = getDepotModeContext(task, depot);
        assert.equal(byName.Transfer.pipeline_override[deliveryNode].enabled, true);
        assert.equal(
            byName.Transfer.pipeline_override[cargoNode].anchor.DeliveryJobsRedistributionBidAction,
            "DeliveryJobsRedistributionBidNextStep",
        );
        assert.equal(
            byName.Transfer.pipeline_override[cargoNode].anchor.DeliveryJobsOngoingDeliveryAction,
            "DeliveryJobsStopForOngoingDelivery",
        );
        assert.equal(byName.Transfer.pipeline_override[cargoNode].anchor.DeliveryJobsGoToDepot, depot.DepotScene);

        assert.equal(byName.PackCargoOnly.pipeline_override[deliveryNode].enabled, false);
        assert.equal(
            byName.PackCargoOnly.pipeline_override[cargoNode].anchor.DeliveryJobsRedistributionBidAction,
            "DeliveryJobsBackToDepotFromBid",
        );
        assert.equal(
            byName.PackCargoOnly.pipeline_override[cargoNode].anchor.DeliveryJobsOngoingDeliveryAction,
            "DeliveryJobsSkipOngoingDelivery",
        );
        assert.equal(byName.PackCargoOnly.pipeline_override[cargoNode].anchor.DeliveryJobsGoToDepot, depot.DepotScene);
        assert.equal(byName.PackCargoOnly.pipeline_override[cargoCheckNode].expected.includes("查看报价"), false);

        assert.equal(byName.AcceptJobOnly.pipeline_override[deliveryNode].enabled, false);
        assert.equal(
            byName.AcceptJobOnly.pipeline_override[cargoNode].anchor.DeliveryJobsOngoingDeliveryAction,
            "DeliveryJobsStopForOngoingDelivery",
        );
        assert.equal(byName.AcceptJobOnly.pipeline_override[cargoNode].anchor.DeliveryJobsGoToDepot, depot.DepotScene);

        assert.deepEqual(byName.Disabled.pipeline_override, {
            [deliveryNode]: {
                enabled: false,
            },
            [cargoNode]: {
                enabled: false,
            },
        });
    }
});

test("DeliveryJobs quote mode registers threshold and branch action options", () => {
    const task = readGeneratedTask();
    for (const depot of deliveryJobDepots) {
        const {byName, deliveryNode, cargoNode} = getDepotModeContext(task, depot);
        assert.deepEqual(byName.ByQuote.option, [
            `DeliveryJobsQuoteThreshold${depot.Id}`,
            `DeliveryJobsAtLeastMinimumQuoteAction${depot.Id}`,
            `DeliveryJobsBelowMinimumQuoteAction${depot.Id}`,
        ]);
        assert.equal(byName.ByQuote.pipeline_override[deliveryNode].enabled, false);
        assert.equal(byName.ByQuote.pipeline_override[cargoNode].enabled, true);
        assert.equal(
            byName.ByQuote.pipeline_override[cargoNode].anchor.DeliveryJobsRedistributionBidAction,
            `DeliveryJobsDecide${depot.Id}Quote`,
        );
        assert.equal(
            byName.ByQuote.pipeline_override[cargoNode].anchor.DeliveryJobsOngoingDeliveryAction,
            "DeliveryJobsStopForOngoingDelivery",
        );
        assert.equal(byName.ByQuote.pipeline_override[cargoNode].anchor.DeliveryJobsGoToDepot, depot.DepotScene);
    }
});

test("DeliveryJobs quote threshold configures both comparison nodes", () => {
    const task = readGeneratedTask();
    for (const depot of deliveryJobDepots) {
        const thresholdOption = task.option[`DeliveryJobsQuoteThreshold${depot.Id}`];
        const thresholdInput = thresholdOption.inputs[0];
        assert.equal(thresholdOption.type, "input");
        assert.equal(thresholdInput.default, "119000");
        assert.equal(thresholdInput.pipeline_type, "string");
        assert.deepEqual(
            thresholdOption.pipeline_override[`DeliveryJobs${depot.Id}QuoteAtLeastMinimum`].custom_recognition_param,
            {
                expression: `{DeliveryJobsSelectedBidPrice}>={${thresholdInput.name}}`,
            },
        );
        assert.deepEqual(
            thresholdOption.pipeline_override[`DeliveryJobs${depot.Id}QuoteBelowMinimum`].custom_recognition_param,
            {
                expression: `{DeliveryJobsSelectedBidPrice}<{${thresholdInput.name}}`,
            },
        );
    }
});

test("DeliveryJobs quote branches share actions with branch-specific defaults", () => {
    const task = readGeneratedTask();
    for (const depot of deliveryJobDepots) {
        const quoteActionOptions = [
            {
                option: task.option[`DeliveryJobsAtLeastMinimumQuoteAction${depot.Id}`],
                comparison: "AtLeastMinimum",
                defaultCase: "Transfer",
            },
            {
                option: task.option[`DeliveryJobsBelowMinimumQuoteAction${depot.Id}`],
                comparison: "BelowMinimum",
                defaultCase: "AcceptJobOnly",
            },
        ];
        for (const {option: quoteAction, comparison, defaultCase} of quoteActionOptions) {
            assert.equal(quoteAction.type, "select");
            assert.equal(quoteAction.default_case, defaultCase);
            assert.deepEqual(
                quoteAction.cases.map((mode) => mode.name),
                [
                    "Transfer",
                    "AcceptJobOnly",
                    "DoNotAccept",
                ],
            );
            const comparisonNode = `DeliveryJobs${depot.Id}Quote${comparison}`;
            assert.deepEqual(
                quoteAction.cases.map((mode) => mode.pipeline_override[comparisonNode].anchor),
                [
                    {
                        DeliveryJobsQuoteAction: "DeliveryJobsQuoteTransferJob",
                        DeliveryJobsGoToDepot: `DeliveryJobsReturnAndTransfer${depot.Id}`,
                    },
                    {
                        DeliveryJobsQuoteAction: "DeliveryJobsQuoteAcceptJobOnly",
                        DeliveryJobsGoToDepot: depot.DepotScene,
                    },
                    {
                        DeliveryJobsQuoteAction: "DeliveryJobsQuoteDoNotAccept",
                        DeliveryJobsGoToDepot: depot.DepotScene,
                    },
                ],
            );
        }
    }
});

test("DeliveryJobs shared bid page locates the selected quote", () => {
    const pipeline = readGeneratedPipeline("DeliveryJobs", "PackCargo.json");
    const adbPipeline = readAdbPipeline("DeliveryJobs", "PackCargo.json");
    assert.deepEqual(
        pipeline.DeliveryJobsSelectedBidLocationIcon.roi,
        [
            970,
            132,
            223,
            106,
        ],
    );
    assert.equal(pipeline.DeliveryJobsSelectedBidLocationIcon.template, "DeliveryJobs/LocationIcon.png");
    assert.ok(existsSync(new URL("../../../assets/resource/image/DeliveryJobs/LocationIcon.png", import.meta.url)));
    assert.ok(existsSync(new URL("../../../assets/resource_adb/image/DeliveryJobs/LocationIcon.png", import.meta.url)));
    assert.equal(pipeline.DeliveryJobsSelectedBidPriceOCR.roi, "DeliveryJobsSelectedBidLocationIcon");
    assert.deepEqual(
        pipeline.DeliveryJobsSelectedBidPriceOCR.roi_offset,
        [
            137,
            16,
            28,
            -2,
        ],
    );
    assert.deepEqual(
        adbPipeline.DeliveryJobsSelectedBidPriceOCR.roi_offset,
        [
            170,
            20,
            35,
            -3,
        ],
    );
    assert.equal(pipeline.DeliveryJobsSelectedBidPriceOCR.only_rec, true);
    assert.deepEqual(pipeline.DeliveryJobsSelectedBidPriceOCR.expected, ["\\d+(?:[.,]\\d+)?"]);
    assert.deepEqual(pipeline.DeliveryJobsSelectedBidPriceOCR.replace, [
        [
            "方",
            "万",
        ],
    ]);
});

test("DeliveryJobs generated packing priorities configure ordered IconRecognition entries", () => {
    const pipeline = readGeneratedPipeline("DeliveryJobs", "PriorityItems.json");
    const adbPipeline = readAdbPipeline("DeliveryJobs", "PriorityItems.json");
    for (const region of deliveryJobRegions) {
        const defaultItem = region.FillItems.find((item) => item.Id === region.DefaultFillItem);
        for (let priority = 1; priority <= DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT; priority += 1) {
            const startNode = pipeline[`DeliveryJobsStartFill${region.Id}Priority${priority}`];
            const selectNode = pipeline[`DeliveryJobsSelectItemToFill${region.Id}Priority${priority}`];
            const continueNode = pipeline[`DeliveryJobsContinueFill${region.Id}AfterPriority${priority}`];
            assert.equal(startNode.enabled, priority === 1);
            assert.equal(selectNode.enabled, priority === 1);
            assert.deepEqual(startNode.anchor, {
                DeliveryJobsCurrentPriorityItem: `DeliveryJobsSelectItemToFill${region.Id}Priority${priority}`,
                DeliveryJobsNextPriority: `DeliveryJobsContinueFill${region.Id}AfterPriority${priority}`,
            });
            assert.deepEqual(startNode.custom_action_param.patch, {
                DeliveryJobsItemListAtTop: {attach: {ready: false}},
                DeliveryJobsItemListAtBottom: {attach: {ready: false}},
            });
            assert.equal(selectNode.recognition, "Custom");
            assert.equal(selectNode.custom_recognition, "IconRecognition");
            assert.deepEqual(selectNode.custom_recognition_param, {
                grid_type: "shipment",
                item_ids: [
                    region.DefaultFillItem,
                ],
                item_recheck_filters: [
                    defaultItem.RecheckFilter,
                ],
                deduplicate: true,
            });
            assert.equal(selectNode.action, "Click");
            assert.deepEqual(
                adbPipeline[`DeliveryJobsSelectItemToFill${region.Id}Priority${priority}`].roi,
                [
                    43,
                    169,
                    480,
                    408,
                ],
            );
            assert.deepEqual(continueNode.next, [
                ...Array.from(
                    {length: DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT - priority},
                    (_, index) => `DeliveryJobsStartFill${region.Id}Priority${priority + index + 1}`,
                ),
                "DeliveryJobsConfiguredFillItemsInsufficient",
            ]);
        }
    }
});

test("DeliveryJobs packing flow resets, scans, and falls back between configured priorities", () => {
    const pipeline = readGeneratedPipeline("DeliveryJobs", "PackCargo.json");
    const adbPipeline = readAdbPipeline("DeliveryJobs", "PackCargo.json");
    assert.deepEqual(pipeline.DeliveryJobsResetItemListLoop.next, [
        "DeliveryJobsItemListAtTop",
        "[JumpBack]DeliveryJobsSelectItemScrollDown",
    ]);
    assert.deepEqual(pipeline.DeliveryJobsSelectPriorityItemLoop.next, [
        "[Anchor]DeliveryJobsCurrentPriorityItem",
        "DeliveryJobsItemListAtBottom",
        "[JumpBack]DeliveryJobsSelectItemScrollUp",
    ]);
    for (const boundary of [
        "Top",
        "Bottom",
    ]) {
        const nodeName = `DeliveryJobsItemListAt${boundary}`;
        assert.equal(pipeline[nodeName].custom_recognition, "ListCompleteRecognition");
        assert.deepEqual(pipeline[nodeName].attach, {ready: false});
        assert.deepEqual(
            adbPipeline[nodeName].roi,
            [
                43,
                169,
                480,
                408,
            ],
        );
    }
    assert.deepEqual(pipeline.DeliveryJobsItemListAtBottom.next, [
        "[Anchor]DeliveryJobsNextPriority",
    ]);
    assert.deepEqual(pipeline.DeliveryJobsSelectItemScrollUp.next, [
        "MouseMoveReset",
    ]);
    assert.equal(pipeline.DeliveryJobsSelectItemScrollUp.duration, 200);
    assert.equal(pipeline.DeliveryJobsSelectItemScrollUp.end_hold, 400);
    assert.equal(pipeline.DeliveryJobsSelectItemScrollUp.max_hit, undefined);
    assert.deepEqual(pipeline.DeliveryJobsSelectItemScrollDown.next, [
        "MouseMoveReset",
    ]);
    assert.equal(pipeline.DeliveryJobsSelectItemScrollDown.duration, 200);
    assert.equal(pipeline.DeliveryJobsSelectItemScrollDown.end_hold, 400);
    assert.deepEqual(pipeline.DeliveryJobsFillCorrespondingGoods.next, [
        "DeliveryJobsFillToMaxNextStep",
        "[Anchor]DeliveryJobsNextPriority",
    ]);
    assert.equal(pipeline.DeliveryJobsConfiguredFillItemsInsufficient.custom_action, "FalseAction");
    assert.equal(
        pipeline.DeliveryJobsConfiguredFillItemsInsufficient.focus["Node.Recognition.Succeeded"],
        "$task.DeliveryJobs.ConfiguredFillItemsInsufficient",
    );
});

test("DeliveryJobs shared bid page dispatches through common quote actions", () => {
    const pipeline = readGeneratedPipeline("DeliveryJobs", "PackCargo.json");
    assert.deepEqual(pipeline.DeliveryJobsInCargoRedistributionBid.next, [
        "DeliveryJobsOngoingDelivery",
        "[Anchor]DeliveryJobsRedistributionBidAction",
    ]);
    assert.deepEqual(pipeline.DeliveryJobsQuoteTransferJob.next, [
        "DeliveryJobsRedistributionBidNextStep",
    ]);
    assert.deepEqual(pipeline.DeliveryJobsQuoteAcceptJobOnly.next, [
        "DeliveryJobsRedistributionBidNextStep",
    ]);
    assert.equal(pipeline.DeliveryJobsQuoteAcceptJobOnly.anchor, undefined);
    assert.deepEqual(pipeline.DeliveryJobsQuoteDoNotAccept.next, [
        "InLocalDepotNode",
    ]);
    assert.equal(pipeline.DeliveryJobsQuoteDoNotAcceptStopped, undefined);
    assert.deepEqual(pipeline.DeliveryJobsOngoingDelivery.next, [
        "[Anchor]DeliveryJobsOngoingDeliveryAction",
    ]);
    assert.equal(pipeline.DeliveryJobsOngoingDelivery.action, undefined);
    assert.equal(pipeline.DeliveryJobsStopForOngoingDelivery.action, "StopTask");
    assert.deepEqual(pipeline.DeliveryJobsSkipOngoingDelivery.next, [
        "DeliveryJobsBackToDepotFromBid",
    ]);
});

test("DeliveryJobs stops only for an ongoing delivery or quote recognition failure", () => {
    const pipelineRoot = new URL("../../../assets/resource/pipeline/DeliveryJobs/", import.meta.url);
    const stopNodes = readdirSync(pipelineRoot, {recursive: true, withFileTypes: true})
        .filter((entry) => entry.isFile() && entry.name.endsWith(".json"))
        .flatMap((entry) => {
            const pipeline = readJsonc(join(entry.parentPath, entry.name));
            return Object.entries(pipeline)
                .filter(
                    ([
                        ,
                        node,
                    ]) => {
                        const action = typeof node.action === "string" ? node.action : node.action?.type;
                        return action === "StopTask";
                    },
                )
                .map(([nodeName]) => nodeName);
        })
        .sort();
    assert.deepEqual(stopNodes, [
        "DeliveryJobsBidPriceRecognitionFailed",
        "DeliveryJobsStopForOngoingDelivery",
    ]);
});

test("DeliveryJobs no longer keeps stale handwritten region pipelines", () => {
    for (const region of deliveryJobRegions) {
        assert.equal(
            existsSync(new URL(`../../../assets/resource/pipeline/DeliveryJobs/${region.Id}.json`, import.meta.url)),
            false,
        );
    }
});

test("DeliveryJobs generated directories exactly match the current model", () => {
    const pipelineRoot = new URL("../../../assets/resource/pipeline/DeliveryJobs/", import.meta.url);
    const actualRegionFiles = readdirSync(new URL("Region/", pipelineRoot))
        .filter((name) => name.endsWith(".json"))
        .sort();
    assert.deepEqual(actualRegionFiles, deliveryJobRegions.map((region) => `${region.Id}.json`).sort());

    const depotRoot = new URL("Depot/", pipelineRoot);
    const depotRootPath = fileURLToPath(depotRoot);
    const actualDepotFiles = readdirSync(depotRoot, {recursive: true, withFileTypes: true})
        .filter((entry) => entry.isFile() && entry.name.endsWith(".json"))
        .map((entry) => relative(depotRootPath, join(entry.parentPath, entry.name)).replaceAll("\\", "/"))
        .sort();
    assert.deepEqual(actualDepotFiles, deliveryJobDepots.map((depot) => `${depot.RegionId}/${depot.Id}.json`).sort());
});
