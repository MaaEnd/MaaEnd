import assert from "node:assert/strict";
import {existsSync, readdirSync, readFileSync} from "node:fs";
import {join, relative} from "node:path";
import test from "node:test";
import {fileURLToPath} from "node:url";

import {parse} from "jsonc-parser";

import {deliveryJobDepots, deliveryJobRegions} from "./model.mjs";

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
    for (const region of deliveryJobRegions) {
        assert.ok(region.Depots.length > 0, `${region.Id} must contain at least one depot`);
        assert.ok(region.FillItems.some((item) => item.Id === region.DefaultFillItem));
        for (const item of region.FillItems) {
            assert.ok(
                existsSync(new URL(`../../../assets/resource/image/${item.Template}`, import.meta.url)),
                `${region.Id} missing template ${item.Template}`,
            );
        }
    }
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
            DeliveryJobsSelectItemToFill: `DeliveryJobsSelectItemToFill${depot.RegionId}`,
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
        assert.deepEqual(
            pipeline[`DeliveryJobs${depot.Id}QuoteAtLeastMinimum`].custom_recognition_param,
            {
                expression: "{DeliveryJobsSelectedBidPrice}>=119000",
            },
        );
        assert.deepEqual(
            pipeline[`DeliveryJobs${depot.Id}QuoteBelowMinimum`].custom_recognition_param,
            {
                expression: "{DeliveryJobsSelectedBidPrice}<119000",
            },
        );
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
    assert.deepEqual(pipeline.DeliveryJobsSelectedBidLocationIcon.roi, [
        970,
        132,
        223,
        106,
    ]);
    assert.equal(pipeline.DeliveryJobsSelectedBidLocationIcon.template, "DeliveryJobs/LocationIcon.png");
    assert.ok(existsSync(new URL("../../../assets/resource/image/DeliveryJobs/LocationIcon.png", import.meta.url)));
    assert.ok(existsSync(new URL("../../../assets/resource_adb/image/DeliveryJobs/LocationIcon.png", import.meta.url)));
    assert.equal(pipeline.DeliveryJobsSelectedBidPriceOCR.roi, "DeliveryJobsSelectedBidLocationIcon");
    assert.deepEqual(pipeline.DeliveryJobsSelectedBidPriceOCR.roi_offset, [
        137,
        16,
        28,
        -2,
    ]);
    assert.deepEqual(adbPipeline.DeliveryJobsSelectedBidPriceOCR.roi_offset, [
        170,
        20,
        35,
        -3,
    ]);
    assert.equal(pipeline.DeliveryJobsSelectedBidPriceOCR.only_rec, true);
    assert.deepEqual(pipeline.DeliveryJobsSelectedBidPriceOCR.expected, ["\\d+(?:[.,]\\d+)?"]);
    assert.deepEqual(pipeline.DeliveryJobsSelectedBidPriceOCR.replace, [["方", "万"]]);
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
