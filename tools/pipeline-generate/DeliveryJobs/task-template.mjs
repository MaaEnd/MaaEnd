import {DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT, deliveryJobDepots, deliveryJobRegions} from "./model.mjs";

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

const AUTO_DELIVERY_NAVIGATE_NODES = [
    "AutoDeliveryNavigateDepot",
    "AutoDeliveryNavigateDestination",
];

function buildCargoAnchor(
    depot,
    bidAction,
    goToDepot = depot.DepotScene,
    afterAcceptJob = "DeliveryJobsDeliverQuickly",
) {
    return {
        DeliveryJobsSelectPriorityItems: `DeliveryJobsSelectPriorityItems${depot.RegionId}`,
        DeliveryJobsRedistributionBidAction: bidAction,
        DeliveryJobsAfterAcceptJob: afterAcceptJob,
        DeliveryJobsGoToDepot: goToDepot,
        DeliveryJobsReturnToDepotNode: depot.DepotScene,
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
        pipelineOverride[cargoNode].anchor = buildCargoAnchor(depot, bidAction);
        pipelineOverride[cargoCheckNode] = {
            expected: cargoExpected,
        };
        pipelineOverride[`DeliveryJobsOngoingDeliveryFor${depot.Id}`] = {
            next: [
                ongoingDeliveryAction,
            ],
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
                    ongoingDeliveryAction: "DeliveryJobsTransferOngoingJob",
                }),
            },
            ...(depot.AutoDeliverySupported
                ? [
                      {
                          name: "AutoDelivery",
                          label: "$task.DeliveryJobs.DepotAction.AutoDelivery",
                          pipeline_override: buildAutoDeliveryOverride(depot, {
                              bidAction: "DeliveryJobsRedistributionBidNextStep",
                          }),
                      },
                  ]
                : []),
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
                    ongoingDeliveryAction: "DeliveryJobsSkipOngoingDelivery",
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
                    ongoingDeliveryAction: "DeliveryJobsSkipOngoingDelivery",
                }),
            },
            {
                name: "PackCargoOnly",
                label: "$task.DeliveryJobs.DepotAction.PackCargoOnly",
                pipeline_override: buildModeOverride(depot, {
                    deliveryEnabled: false,
                    cargoEnabled: true,
                    cargoExpected: PACK_CARGO_EXPECTED,
                    bidAction: "DeliveryJobsCloseRedistributionBid",
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
    const autoDelivery = `DeliveryJobsAutoDelivery${depot.Id}`;
    // 是否接取由 next 表达，接取后的去向只由 DeliveryJobsGoToDepot 表达；
    // 「不处理」直接关闭调度申请界面，不经过 DeliveryJobsBackToDepot。
    const acceptThen = (goToDepot) => ({
        [comparisonNode]: {
            next: [
                "DeliveryJobsRedistributionBidNextStep",
            ],
            anchor: {
                DeliveryJobsGoToDepot: goToDepot,
            },
        },
    });
    return {
        type: "select",
        label,
        description,
        cases: [
            {
                name: "Transfer",
                label: "$task.DeliveryJobs.QuoteAction.Transfer",
                pipeline_override: acceptThen(`DeliveryJobsReturnAndTransfer${depot.Id}`),
            },
            ...(depot.AutoDeliverySupported
                ? [
                      {
                          name: "AutoDelivery",
                          label: "$task.DeliveryJobs.QuoteAction.AutoDelivery",
                          pipeline_override: acceptThen(autoDelivery),
                      },
                  ]
                : []),
            {
                name: "AcceptJobOnly",
                label: "$task.DeliveryJobs.QuoteAction.AcceptJobOnly",
                pipeline_override: acceptThen(depot.DepotScene),
            },
            {
                name: "DoNotAccept",
                label: "$task.DeliveryJobs.QuoteAction.DoNotAccept",
                pipeline_override: {
                    [comparisonNode]: {
                        next: [
                            "DeliveryJobsCloseRedistributionBid",
                        ],
                    },
                },
            },
        ],
        default_case: defaultCase,
    };
}

function buildAutoDeliveryOverride(depot, {bidAction}) {
    const deliveryNode = `DeliveryJobsEnter${depot.Id}DeliveryJob`;
    const cargoNode = `DeliveryJobsEnter${depot.Id}Cargo`;
    const autoDelivery = `DeliveryJobsAutoDelivery${depot.Id}`;
    // 入口 A 点完「查看任务」后先经门节点等任务详情界面稳定，再交给全自动送货
    const waitDetail = `DeliveryJobsWait${depot.Id}DeliveryMissionDetail`;
    return {
        [deliveryNode]: {
            enabled: true,
            next: [waitDetail],
        },
        [cargoNode]: {
            enabled: true,
            anchor: buildCargoAnchor(depot, bidAction, autoDelivery),
        },
        [`DeliveryJobsOngoingDeliveryFor${depot.Id}`]: {
            next: [
                autoDelivery,
            ],
        },
    };
}

function buildOngoingDeliveryFallbackOption() {
    // 自动送货失败统一在公共调用节点 DeliveryJobsDeliverByAutoDelivery 上处理，
    // 因此开关只需要覆盖一次 on_error，无需为每个仓储节点重复。
    return {
        type: "switch",
        label: "$task.DeliveryJobs.OngoingDeliveryFallback.label",
        description: "$task.DeliveryJobs.OngoingDeliveryFallback.description",
        default_case: "No",
        cases: [
            {
                name: "No",
            },
            {
                name: "Yes",
                pipeline_override: {
                    DeliveryJobsDeliverByAutoDelivery: {
                        on_error: [
                            "DeliveryJobsTransferOngoingJob",
                        ],
                    },
                },
            },
        ],
    };
}

function buildAutoDeliveryPreferZiplineOption() {
    const buildCase = (name, zip) => ({
        name,
        pipeline_override: Object.fromEntries(
            AUTO_DELIVERY_NAVIGATE_NODES.map((node) => [
                node,
                {
                    attach: {
                        zip,
                    },
                },
            ]),
        ),
    });

    return {
        type: "switch",
        label: "$task.AutoDeliveryPreferZipline.label",
        description: "$task.AutoDeliveryPreferZipline.description",
        default_case: "No",
        cases: [
            buildCase("No", false),
            buildCase("Yes", true),
        ],
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

function priorityOptionName(regionId, priority) {
    return `WhatToFill${regionId}Priority${priority}`;
}

function priorityOptionNames(regionId) {
    return Array.from({length: DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT}, (_, index) =>
        priorityOptionName(regionId, index + 1),
    );
}

function buildFillItemPriorityRegionOption(region) {
    return {
        type: "switch",
        label: `$global.region.${region.Id}`,
        description: "$task.DeliveryJobs.FillItemPriorityRegionDescription",
        cases: [
            {
                name: "Yes",
                option: priorityOptionNames(region.Id),
                pipeline_override: {
                    [`DeliveryJobsSelectPriorityItems${region.Id}`]: {
                        next: [
                            `DeliveryJobsStartFill${region.Id}Priority1`,
                        ],
                    },
                },
            },
            {
                name: "No",
            },
        ],
        default_case: "Yes",
    };
}

function buildFillItemCases(region, priority) {
    const cases = region.FillItems.map((item) => ({
        name: item.Id,
        label: item.Label,
        pipeline_override: {
            [`DeliveryJobsStartFill${region.Id}Priority${priority}`]: {
                enabled: true,
            },
            [`DeliveryJobsSelectItemToFill${region.Id}Priority${priority}`]: {
                enabled: true,
                custom_recognition_param: {
                    grid_type: "shipment",
                    item_ids: [
                        item.ItemId,
                    ],
                    item_recheck_filters: [
                        item.RecheckFilter,
                    ],
                    deduplicate: true,
                },
            },
        },
    }));
    if (priority > 1) {
        cases.unshift({
            name: "None",
            label: "$task.DeliveryJobs.FillItemPriorityNone",
        });
    }
    return cases;
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

    options.DeliveryJobsAutoDeliveryPreferZipline = buildAutoDeliveryPreferZiplineOption();
    options.DeliveryJobsOngoingDeliveryFallback = buildOngoingDeliveryFallbackOption();
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
                            "[Anchor]DeliveryJobsSelectPriorityItems",
                        ],
                    },
                },
                option: deliveryJobRegions.map((region) => `FillItemPriorities${region.Id}`),
            },
            {
                name: "No",
            },
        ],
        default_case: "No",
    };

    for (const region of deliveryJobRegions) {
        options[`FillItemPriorities${region.Id}`] = buildFillItemPriorityRegionOption(region);
        for (let priority = 1; priority <= DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT; priority += 1) {
            options[priorityOptionName(region.Id, priority)] = {
                type: "select",
                label: `$task.DeliveryJobs.WhatToFill${region.Id}Priority${priority}`,
                cases: buildFillItemCases(region, priority),
                default_case: priority === 1 ? region.DefaultFillItem : "None",
            };
        }
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
                    "DeliveryJobsAutoDeliveryPreferZipline",
                    "DeliveryJobsOngoingDeliveryFallback",
                ],
                controller: [
                    "ADB",
                    "CloudADB",
                    "Linux-Gamescope",
                    "Linux-ScreenCast",
                    "Linux-Wlroots",
                    "MacOS-Background",
                    "MacOS-Front",
                    "PlayCover",
                    "Win32-Front",
                ],
                group: [
                    "regional_development",
                ],
            },
        ],
        option: buildTaskOptions(),
    };
}
