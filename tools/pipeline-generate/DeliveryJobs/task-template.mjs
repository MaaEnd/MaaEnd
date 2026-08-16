import {deliveryJobDepots, deliveryJobRegions} from "./model.mjs";

const ALL_CARGO_EXPECTED = [
    "查看报价",
    "查看報價",
    "(?i)Check\\s*Bid",
    "希望価格確認",
    "货物装箱",
    "貨物裝箱",
    "(?i)Pack\\s*Goods",
    "パッキング",
    "화물 포장",
    "입찰 보기",
];

const PACK_CARGO_EXPECTED = [
    "货物装箱",
    "貨物裝箱",
    "(?i)Pack\\s*Goods",
    "パッキング",
    "화물 포장",
];

function buildCargoAnchor(depot, bidAction, ongoingDeliveryAction = "DeliveryJobsStopForOngoingDelivery") {
    return {
        DeliveryJobsSelectItemToFill: `DeliveryJobsSelectItemToFill${depot.RegionId}`,
        DeliveryJobsRedistributionBidAction: bidAction,
        DeliveryJobsOngoingDeliveryAction: ongoingDeliveryAction,
        DeliveryJobsGoToDepot: depot.DepotScene,
    };
}

function buildModeOverride(depot, {deliveryEnabled, cargoEnabled, cargoExpected, bidAction, ongoingDeliveryAction}) {
    const deliveryNode = `DeliveryJobsEnter${depot.Id}DeliveryJob`;
    const cargoNode = `DeliveryJobsEnter${depot.Id}Cargo`;
    const cargoCheckNode = `DeliveryJobsCheck${depot.Id}Cargo`;
    const pipelineOverride = {
        [deliveryNode]: {
            enabled: deliveryEnabled,
        },
        [cargoNode]: {
            enabled: cargoEnabled,
        },
    };

    if (cargoEnabled) {
        pipelineOverride[cargoNode].anchor = buildCargoAnchor(depot, bidAction, ongoingDeliveryAction);
        pipelineOverride[cargoCheckNode] = {
            expected: cargoExpected,
        };
    }

    return pipelineOverride;
}

function buildDepotOption(depot) {
    return {
        type: "select",
        label: `$global.region.${depot.Id}`,
        cases: [
            {
                name: "Transfer",
                label: "$task.DeliveryJobs.DepotAction.Transfer",
                pipeline_override: buildModeOverride(depot, {
                    deliveryEnabled: true,
                    cargoEnabled: true,
                    cargoExpected: ALL_CARGO_EXPECTED,
                    bidAction: "DeliveryJobsRedistributionBidNextStep",
                }),
            },
            {
                name: "ByQuote",
                label: "$task.DeliveryJobs.DepotAction.ByQuote",
                option: [
                    `DeliveryJobsQuoteThreshold${depot.Id}`,
                    `DeliveryJobsAtLeastMinimumQuoteAction${depot.Id}`,
                    `DeliveryJobsBelowMinimumQuoteAction${depot.Id}`,
                ],
                pipeline_override: buildModeOverride(depot, {
                    deliveryEnabled: false,
                    cargoEnabled: true,
                    cargoExpected: ALL_CARGO_EXPECTED,
                    bidAction: `DeliveryJobsDecide${depot.Id}Quote`,
                }),
            },
            {
                name: "AcceptJobOnly",
                label: "$task.DeliveryJobs.DepotAction.AcceptJobOnly",
                pipeline_override: buildModeOverride(depot, {
                    deliveryEnabled: false,
                    cargoEnabled: true,
                    cargoExpected: ALL_CARGO_EXPECTED,
                    bidAction: "DeliveryJobsRedistributionBidNextStep",
                }),
            },
            {
                name: "PackCargoOnly",
                label: "$task.DeliveryJobs.DepotAction.PackCargoOnly",
                pipeline_override: buildModeOverride(depot, {
                    deliveryEnabled: false,
                    cargoEnabled: true,
                    cargoExpected: PACK_CARGO_EXPECTED,
                    bidAction: "DeliveryJobsBackToDepotFromBid",
                    ongoingDeliveryAction: "DeliveryJobsSkipOngoingDelivery",
                }),
            },
            {
                name: "Disabled",
                label: "$task.DeliveryJobs.DepotAction.Disabled",
                pipeline_override: buildModeOverride(depot, {
                    deliveryEnabled: false,
                    cargoEnabled: false,
                }),
            },
        ],
        default_case: "Transfer",
    };
}

function buildQuoteThresholdOption(depot) {
    const inputName = `DeliveryJobsQuoteThreshold${depot.Id}Value`;
    const comparisonExpressions = [
        [
            `DeliveryJobs${depot.Id}QuoteAtLeastMinimum`,
            `{DeliveryJobsSelectedBidPrice}>={${inputName}}`,
        ],
        [
            `DeliveryJobs${depot.Id}QuoteBelowMinimum`,
            `{DeliveryJobsSelectedBidPrice}<{${inputName}}`,
        ],
    ];

    return {
        type: "input",
        label: "$task.DeliveryJobs.QuoteThreshold.label",
        description: "$task.DeliveryJobs.QuoteThreshold.description",
        inputs: [
            {
                name: inputName,
                label: "$task.DeliveryJobs.QuoteThreshold.inputs.Value.label",
                description: "$task.DeliveryJobs.QuoteThreshold.inputs.Value.description",
                // The input is interpolated into an expression string; using "int" would parse the whole expression as an integer.
                pipeline_type: "string",
                verify: "^\\d+$",
                default: "119000",
                pattern_msg: "$task.DeliveryJobs.QuoteThreshold.inputs.Value.pattern_msg",
            },
        ],
        pipeline_override: Object.fromEntries(
            comparisonExpressions.map(
                ([
                    node,
                    expression,
                ]) => [
                    node,
                    {
                        custom_recognition_param: {
                            expression,
                        },
                    },
                ],
            ),
        ),
    };
}

function buildQuoteActionOption(depot, {comparison, label, description, defaultCase}) {
    const comparisonNode = `DeliveryJobs${depot.Id}Quote${comparison}`;
    return {
        type: "select",
        label,
        description,
        cases: [
            {
                name: "Transfer",
                label: "$task.DeliveryJobs.QuoteAction.Transfer",
                pipeline_override: {
                    [comparisonNode]: {
                        anchor: {
                            DeliveryJobsQuoteAction: "DeliveryJobsQuoteTransferJob",
                            DeliveryJobsGoToDepot: `DeliveryJobsReturnAndTransfer${depot.Id}`,
                        },
                    },
                },
            },
            {
                name: "AcceptJobOnly",
                label: "$task.DeliveryJobs.QuoteAction.AcceptJobOnly",
                pipeline_override: {
                    [comparisonNode]: {
                        anchor: {
                            DeliveryJobsQuoteAction: "DeliveryJobsQuoteAcceptJobOnly",
                            DeliveryJobsGoToDepot: depot.DepotScene,
                        },
                    },
                },
            },
            {
                name: "DoNotAccept",
                label: "$task.DeliveryJobs.QuoteAction.DoNotAccept",
                pipeline_override: {
                    [comparisonNode]: {
                        anchor: {
                            DeliveryJobsQuoteAction: "DeliveryJobsQuoteDoNotAccept",
                            DeliveryJobsGoToDepot: depot.DepotScene,
                        },
                    },
                },
            },
        ],
        default_case: defaultCase,
    };
}

function buildRegionOption(region) {
    return {
        type: "switch",
        label: `$global.region.${region.Id}`,
        default_case: "Yes",
        cases: [
            {
                name: "Yes",
                pipeline_override: {
                    [`DeliveryJobs${region.Id}`]: {
                        enabled: true,
                    },
                },
                option: region.Depots,
            },
            {
                name: "No",
                pipeline_override: {
                    [`DeliveryJobs${region.Id}`]: {
                        enabled: false,
                    },
                },
            },
        ],
    };
}

function buildFillItemCases(region) {
    return region.FillItems.map((item) => ({
        name: item.Name,
        label: item.Label,
        pipeline_override: {
            [`DeliveryJobsSelectItemToFill${region.Id}`]: {
                template: item.Template,
            },
        },
    }));
}

function buildTaskOptions() {
    const options = {};
    for (const region of deliveryJobRegions) {
        options[region.Id] = buildRegionOption(region);
        for (const depotId of region.Depots) {
            const depot = deliveryJobDepots.find((item) => item.Id === depotId);
            options[depot.Id] = buildDepotOption(depot);
            options[`DeliveryJobsQuoteThreshold${depot.Id}`] = buildQuoteThresholdOption(depot);
            options[`DeliveryJobsAtLeastMinimumQuoteAction${depot.Id}`] = buildQuoteActionOption(depot, {
                comparison: "AtLeastMinimum",
                label: "$task.DeliveryJobs.AtLeastMinimumQuoteAction.label",
                description: "$task.DeliveryJobs.AtLeastMinimumQuoteAction.description",
                defaultCase: "Transfer",
            });
            options[`DeliveryJobsBelowMinimumQuoteAction${depot.Id}`] = buildQuoteActionOption(depot, {
                comparison: "BelowMinimum",
                label: "$task.DeliveryJobs.BelowMinimumQuoteAction.label",
                description: "$task.DeliveryJobs.BelowMinimumQuoteAction.description",
                defaultCase: "AcceptJobOnly",
            });
        }
    }

    options.PackCargoSelectItem = {
        type: "switch",
        label: "$task.DeliveryJobs.PackCargoSelectItem.label",
        description: "$task.DeliveryJobs.PackCargoSelectItem.description",
        cases: [
            {
                name: "Yes",
                pipeline_override: {
                    DeliveryJobsSelectTypeOfGoodsToPackNextStep: {
                        next: [
                            "DeliveryJobsSelectItemLoop",
                        ],
                    },
                },
                option: deliveryJobRegions.map((region) => `WhatToFill${region.Id}`),
            },
            {
                name: "No",
            },
        ],
        default_case: "No",
    };

    for (const region of deliveryJobRegions) {
        options[`WhatToFill${region.Id}`] = {
            type: "select",
            label: `$task.DeliveryJobs.WhatToFill${region.Id}`,
            cases: buildFillItemCases(region),
            default_case: region.FillItems.find((item) => item.Id === region.DefaultFillItem).Name,
        };
    }
    return options;
}

export default function buildDeliveryJobsTask() {
    return {
        task: [
            {
                name: "DeliveryJobs",
                label: "$task.DeliveryJobs.label",
                entry: "DeliveryJobsMain",
                description: "$task.DeliveryJobs.description",
                option: [
                    ...deliveryJobRegions.map((region) => region.Id),
                    "PackCargoSelectItem",
                ],
                controller: [
                    "ADB",
                    "CloudADB",
                    "MacOS-Background",
                    "MacOS-Front",
                    "PlayCover",
                    "Win32-Front",
                    "Wlroots",
                ],
                group: [
                    "regional_development",
                ],
            },
        ],
        option: buildTaskOptions(),
    };
}
