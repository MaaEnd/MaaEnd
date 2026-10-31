/**
 * DeliveryJobs 任务定义生成模板（生成 assets/tasks/DeliveryJobs.json）。
 *
 * 运行：pnpm generate:DeliveryJobs
 *
 * option 的写入顺序（保持产物顺序稳定，便于 review 时对比 diff）：
 *   1. 地区开关：每个地区一个 switch，关闭后该地区下所有仓储节点禁用；
 *   2. 仓储节点动作：每个仓储节点一个 select，切换接单 / 装箱 / 报价 / 全自动送货等行为，
 *      实现方式是 pipeline_override 改写该仓储节点相关的几个 Pipeline 节点；
 *   3. 报价阈值与报价动作：每个仓储节点一个 input + 两个 select，
 *      分别定义「报价达到阈值」「报价未达到阈值」时怎么处理；
 *   4. 全局开关：全自动送货偏好滑索、已接委托失败兜底、装箱前是否先选品；
 *   5. 装箱物品优先级：每个地区一个 switch + 4 级 select（第 1 级默认该地区的默认物品）。
 *
 * 修改指引：
 *   - 地区、仓储节点、可装箱物品都来自 model.mjs（上游是 tools/zmdmap/data/*.json），
 *     本文件只负责展开，不要在模板里写死具体地区或节点；
 *   - Pipeline 节点 ID 统一由 pipelineNode 拼接，选项 key 统一由 optionKey 拼接，
 *     节点或选项改名时只改这两个助手，避免多处手写漏改；
 *   - 货物识别文本与报价默认阈值在 constants.mjs，depot 模板与任务模板共用，不要两边各写一份；
 *   - label / description 都是 locales 里的 i18n key（$task.… / $global.…）。
 */
import {existsSync} from "node:fs";

import {defaultQuoteThreshold, PACK_CARGO_EXPECTED} from "./constants.mjs";
import {DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT, deliveryJobDepots, deliveryJobRegions} from "./model.mjs";

const UI_ITEM_DIR = new URL("../../../assets/resource/image/UI/Item/", import.meta.url);

/** 物品图标存在时才写 icon 字段（部分物品没有 UI 图标）。 */
function buildItemIcon(itemId) {
    if (!itemId || !existsSync(new URL(`${itemId}.png`, UI_ITEM_DIR))) {
        return undefined;
    }
    return `resource/image/UI/Item/${itemId}.png`;
}

// 全自动送货的两个公共导航节点；「偏好滑索」开关通过 attach 给它们传 zip。
const AUTO_DELIVERY_NAVIGATE_NODES = [
    "AutoDeliveryNavigateDepot",
    "AutoDeliveryNavigateDestination",
];

// 仓储节点流程里复用的公共动作节点。
const actionNode = {
    decideQuickTransfer: "DeliveryJobsDecideQuickTransfer",
    redistributionBidNextStep: "DeliveryJobsRedistributionBidNextStep",
    closeBid: "DeliveryJobsCloseRedistributionBid",
    skipOngoingDelivery: "DeliveryJobsSkipOngoingDelivery",
    transferOngoingJob: "DeliveryJobsTransferOngoingJob",
    deliverByAutoDelivery: "DeliveryJobsDeliverByAutoDelivery",
};

/**
 * 生成 Pipeline 节点 ID。完整节点名仍以 assets/resource/pipeline/DeliveryJobs/ 下的定义为准，
 * 这里只是集中拼接：改节点名时只改这一处，不用在十几处手写字符串里逐个找。
 */
const pipelineNode = {
    region: (regionId) => `DeliveryJobs${regionId}`,
    enterDeliveryJob: (depot) => `DeliveryJobsEnter${depot.Id}DeliveryJob`,
    enterCargo: (depot) => `DeliveryJobsEnter${depot.Id}Cargo`,
    checkCargo: (depot) => `DeliveryJobsCheck${depot.Id}Cargo`,
    ongoingDelivery: (depot) => `DeliveryJobsOngoingDeliveryFor${depot.Id}`,
    waitMissionDetail: (depot) => `DeliveryJobsWait${depot.Id}DeliveryMissionDetail`,
    autoDelivery: (depot) => `DeliveryJobsAutoDelivery${depot.Id}`,
    returnAndTransfer: (depot) => `DeliveryJobsReturnAndTransfer${depot.Id}`,
    decideQuote: (depot) => `DeliveryJobsDecide${depot.Id}Quote`,
    quoteComparison: (depot, comparison) => `DeliveryJobs${depot.Id}Quote${comparison}`,
    quoteThresholdInput: (depot) => `DeliveryJobsQuoteThreshold${depot.Id}Value`,
    selectPriorityItems: (regionId) => `DeliveryJobsSelectPriorityItems${regionId}`,
    startFill: (regionId, priority) => `DeliveryJobsStartFill${regionId}Priority${priority}`,
    selectItemToFill: (regionId, priority) => `DeliveryJobsSelectItemToFill${regionId}Priority${priority}`,
};

/** 生成任务选项 key（对应 interface V2 的 option 名称）。 */
const optionKey = {
    quoteThreshold: (depot) => `DeliveryJobsQuoteThreshold${depot.Id}`,
    quoteAction: (depot, comparison) => `DeliveryJobs${comparison}QuoteAction${depot.Id}`,
    fillItemPriorities: (region) => `FillItemPriorities${region.Id}`,
    fillItemPriority: (region, priority) => `WhatToFill${region.Id}Priority${priority}`,
};

/**
 * 装箱节点的公共锚点，覆盖进入货物界面后依赖的几个跳转：
 * - DeliveryJobsSelectPriorityItems：装箱时从哪个地区的选品节点开始；
 * - DeliveryJobsRedistributionBidAction：调度申请界面上执行什么动作（bidAction）；
 * - DeliveryJobsGoToDepot：接单之后去哪个节点（goToDepot，默认回到该仓储节点）；
 * - DeliveryJobsReturnToDepotNode：流程结束后的回跳落点。
 */
function buildCargoAnchor(depot, bidAction, goToDepot = depot.DepotScene) {
    return {
        DeliveryJobsSelectPriorityItems: pipelineNode.selectPriorityItems(depot.RegionId),
        DeliveryJobsRedistributionBidAction: bidAction,
        DeliveryJobsGoToDepot: goToDepot,
        DeliveryJobsReturnToDepotNode: depot.DepotScene,
    };
}

/**
 * 「仓储节点动作」的公共覆盖：启用/禁用两个入口节点；
 * 货物入口启用时，再补上识别兜底、锚点，以及「已接但未转交」委托的下一步。
 *
 * - deliveryEnabled：是否启用「查看任务」入口（接取后由循环重入做转交的入口）；
 * - cargoEnabled：是否启用「查看报价 / 货物装箱」入口（装箱与调度申请流程）；
 * - cargoExpected：可选；只有识别文字需要与 Pipeline 默认值不同的方式（仅装箱货物）才传，
 *   其余方式不覆盖 DeliveryJobsCheck{DepotId}Cargo，直接沿用 depot 模板注入的默认值；
 * - bidAction：调度申请界面上执行的动作节点；
 * - ongoingDeliveryAction：处理已接未转交委托的下一步。
 */
function buildModeOverride(depot, {deliveryEnabled, cargoEnabled, cargoExpected, bidAction, ongoingDeliveryAction}) {
    const pipelineOverride = {
        [pipelineNode.enterDeliveryJob(depot)]: {
            enabled: deliveryEnabled,
        },
        [pipelineNode.enterCargo(depot)]: {
            enabled: cargoEnabled,
        },
    };

    if (cargoEnabled) {
        pipelineOverride[pipelineNode.enterCargo(depot)].anchor = buildCargoAnchor(depot, bidAction);
        if (cargoExpected) {
            pipelineOverride[pipelineNode.checkCargo(depot)] = {
                expected: cargoExpected,
            };
        }
        pipelineOverride[pipelineNode.ongoingDelivery(depot)] = {
            next: [
                ongoingDeliveryAction,
            ],
        };
    }

    return pipelineOverride;
}

/**
 * 仓储节点动作下拉：每个 case 只是换一组 buildModeOverride 参数。
 *
 * - Transfer：两个入口都启用；装箱后判断能否「快速转交」，不能则回退接取任务，由循环重入后转交；
 * - AutoDelivery：两个入口都启用；接单后交给全自动送货（只有 AutoDeliverySupported 的仓储节点提供）；
 * - ByQuote：只启用货物入口；先按报价阈值判定，再按「报价动作」选项决定接不接、接取后去哪；
 * - AcceptJobOnly：只启用货物入口；接单后回仓储节点，不转交；
 * - PackCargoOnly：只启用货物入口；只认「货物装箱」界面，装完关闭调度申请界面，不接单；
 * - Disabled：两个入口都停用，禁用该仓储节点。
 */
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
                    // 「接取并转交」先等调度申请界面的底部按钮稳定，再判断是否可以快速转交；
                    // 仓储节点未满 3 级（没有白底「快速转交」）时回退成接取任务。
                    bidAction: actionNode.decideQuickTransfer,
                    ongoingDeliveryAction: actionNode.transferOngoingJob,
                }),
            },
            ...(depot.AutoDeliverySupported
                ? [
                      {
                          name: "AutoDelivery",
                          label: "$task.DeliveryJobs.DepotAction.AutoDelivery",
                          pipeline_override: buildAutoDeliveryOverride(depot, {
                              bidAction: actionNode.redistributionBidNextStep,
                          }),
                      },
                  ]
                : []),
            {
                name: "ByQuote",
                label: "$task.DeliveryJobs.DepotAction.ByQuote",
                // 「按报价处理」会用到阈值和两个报价动作，所以把这三个选项一起挂出来。
                option: [
                    optionKey.quoteThreshold(depot),
                    optionKey.quoteAction(depot, "AtLeastMinimum"),
                    optionKey.quoteAction(depot, "BelowMinimum"),
                ],
                pipeline_override: buildModeOverride(depot, {
                    deliveryEnabled: false,
                    cargoEnabled: true,
                    bidAction: pipelineNode.decideQuote(depot),
                    // 报价只用于决定新委托怎么接；已接未转交的委托没有报价可判，跳过它会一直
                    // 占着调度申请界面，挡住后续仓储节点。这里交给全自动送货清掉，
                    // 没有归属终点（无处可送）的仓储节点才退回跳过。
                    ongoingDeliveryAction: depot.AutoDeliverySupported
                        ? pipelineNode.autoDelivery(depot)
                        : actionNode.skipOngoingDelivery,
                }),
            },
            {
                name: "AcceptJobOnly",
                label: "$task.DeliveryJobs.DepotAction.AcceptJobOnly",
                pipeline_override: buildModeOverride(depot, {
                    deliveryEnabled: false,
                    cargoEnabled: true,
                    bidAction: actionNode.redistributionBidNextStep,
                    ongoingDeliveryAction: actionNode.skipOngoingDelivery,
                }),
            },
            {
                name: "PackCargoOnly",
                label: "$task.DeliveryJobs.DepotAction.PackCargoOnly",
                pipeline_override: buildModeOverride(depot, {
                    deliveryEnabled: false,
                    cargoEnabled: true,
                    cargoExpected: PACK_CARGO_EXPECTED,
                    bidAction: actionNode.closeBid,
                    ongoingDeliveryAction: actionNode.skipOngoingDelivery,
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

/**
 * 报价阈值输入：把用户填的阈值拼进两个 ExpressionRecognition 节点的表达式里。
 * 「达到/未达到」的判定节点与报价动作选项共用同一组节点名。
 */
function buildQuoteThresholdOption(depot) {
    const inputName = pipelineNode.quoteThresholdInput(depot);

    return {
        type: "input",
        label: "$task.DeliveryJobs.QuoteThreshold.label",
        description: "$task.DeliveryJobs.QuoteThreshold.description",
        inputs: [
            {
                name: inputName,
                label: "$task.DeliveryJobs.QuoteThreshold.inputs.Value.label",
                description: "$task.DeliveryJobs.QuoteThreshold.inputs.Value.description",
                // 阈值会被插值进表达式字符串；用 "int" 会把整条表达式当整数解析，必须用 "string"。
                pipeline_type: "string",
                verify: "^\\d+$",
                // 默认值由节点基础报价计算（constants.mjs），修改后重新生成即可。
                default: String(defaultQuoteThreshold(depot)),
                pattern_msg: "$task.DeliveryJobs.QuoteThreshold.inputs.Value.pattern_msg",
            },
        ],
        pipeline_override: {
            [pipelineNode.quoteComparison(depot, "AtLeastMinimum")]: {
                custom_recognition_param: {
                    expression: `{DeliveryJobsSelectedBidPrice}>={${inputName}}`,
                },
            },
            [pipelineNode.quoteComparison(depot, "BelowMinimum")]: {
                custom_recognition_param: {
                    expression: `{DeliveryJobsSelectedBidPrice}<{${inputName}}`,
                },
            },
        },
    };
}

/**
 * 报价动作下拉：判定完报价后怎么处理。
 * comparison 同时决定要覆盖的判定节点（...QuoteAtLeastMinimum / ...QuoteBelowMinimum）。
 */
function buildQuoteActionOption(depot, {comparison, label, description, defaultCase}) {
    const comparisonNode = pipelineNode.quoteComparison(depot, comparison);
    const autoDelivery = pipelineNode.autoDelivery(depot);

    // 是否接取由 next 表达，接取后的去向只由 DeliveryJobsGoToDepot 表达；
    // 「不处理」直接关闭调度申请界面，不经过 DeliveryJobsBackToDepot。
    // 「接取并转交」先进判断节点 DeliveryJobsDecideQuickTransfer，由它决定立刻「快速转交」还是回退接取。
    const acceptThen = (goToDepot, bidAction = actionNode.redistributionBidNextStep) => ({
        [comparisonNode]: {
            next: [
                bidAction,
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
                pipeline_override: acceptThen(pipelineNode.returnAndTransfer(depot), actionNode.decideQuickTransfer),
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
                            actionNode.closeBid,
                        ],
                    },
                },
            },
        ],
        default_case: defaultCase,
    };
}

/** 两个报价动作选项只有判定条件、i18n key 和默认值不同。 */
const QUOTE_ACTIONS = [
    {
        comparison: "AtLeastMinimum",
        label: "$task.DeliveryJobs.AtLeastMinimumQuoteAction.label",
        description: "$task.DeliveryJobs.AtLeastMinimumQuoteAction.description",
        defaultCase: "Transfer",
    },
    {
        comparison: "BelowMinimum",
        label: "$task.DeliveryJobs.BelowMinimumQuoteAction.label",
        description: "$task.DeliveryJobs.BelowMinimumQuoteAction.description",
        defaultCase: "AcceptJobOnly",
    },
];

/**
 * 全自动送货的仓库入口：接单后先等任务详情界面稳定，再交给全自动送货。
 * 调度申请界面的动作与「已接未转交」委托也都指向全自动送货。
 */
function buildAutoDeliveryOverride(depot, {bidAction}) {
    return {
        [pipelineNode.enterDeliveryJob(depot)]: {
            enabled: true,
            next: [
                pipelineNode.waitMissionDetail(depot),
            ],
        },
        [pipelineNode.enterCargo(depot)]: {
            enabled: true,
            anchor: buildCargoAnchor(depot, bidAction, pipelineNode.autoDelivery(depot)),
        },
        [pipelineNode.ongoingDelivery(depot)]: {
            next: [
                pipelineNode.autoDelivery(depot),
            ],
        },
    };
}

/** 全自动送货导航时是否优先走滑索。只影响两个公共导航节点的 attach.zip。 */
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

/**
 * 全自动送货失败后的兜底：把已接委托转交出去。
 * 失败统一在公共节点 DeliveryJobsDeliverByAutoDelivery 上处理，所以只需覆盖一次 on_error，
 * 不必为每个仓储节点重复。
 */
function buildOngoingDeliveryFallbackOption() {
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
                    [actionNode.deliverByAutoDelivery]: {
                        on_error: [
                            actionNode.transferOngoingJob,
                        ],
                    },
                },
            },
        ],
    };
}

/** 装箱前是否先进入选品流程；选品的具体优先级由 FillItemPriorities* 控制。 */
function buildPackCargoSelectItemOption() {
    return {
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
                option: deliveryJobRegions.map((region) => optionKey.fillItemPriorities(region)),
            },
            {
                name: "No",
            },
        ],
        default_case: "No",
    };
}

/**
 * 地区开关：关闭时禁用该地区入口节点 DeliveryJobs<Region>。
 * option 字段挂上该地区下的仓储节点选项，保证地区关闭时这些选项也不再生效。
 */
function buildRegionOption(region) {
    return {
        type: "switch",
        label: `$global.region.${region.Id}`,
        default_case: "Yes",
        cases: [
            {
                name: "Yes",
                pipeline_override: {
                    [pipelineNode.region(region.Id)]: {
                        enabled: true,
                    },
                },
                option: region.Depots,
            },
            {
                name: "No",
                pipeline_override: {
                    [pipelineNode.region(region.Id)]: {
                        enabled: false,
                    },
                },
            },
        ],
    };
}

/**
 * 某地区的装箱优先级链开关：打开后从 Priority1 开始逐级选品，
 * 每一级对应下面的一个 WhatToFill<Region>Priority<N> 下拉。
 */
function buildFillItemPriorityRegionOption(region) {
    return {
        type: "switch",
        label: `$global.region.${region.Id}`,
        description: "$task.DeliveryJobs.FillItemPriorityRegionDescription",
        cases: [
            {
                name: "Yes",
                option: priorityOptionNames(region),
                pipeline_override: {
                    [pipelineNode.selectPriorityItems(region.Id)]: {
                        next: [
                            pipelineNode.startFill(region.Id, 1),
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

/** 某一地区 4 级优先级对应的选项 key。 */
function priorityOptionNames(region) {
    return Array.from({length: DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT}, (_, index) =>
        optionKey.fillItemPriority(region, index + 1),
    );
}

/**
 * 某一级优先级的可选物品。
 * priority > 1 时追加「None」，允许这一级不选物品；第 1 级必须选，
 * 默认值由调用方设为该地区的 DefaultFillItem。
 */
function buildFillItemCases(region, priority) {
    const cases = region.FillItems.map((item) => {
        const icon = buildItemIcon(item.ItemId);
        return {
            name: item.Id,
            label: item.Label,
            ...(icon ? {icon} : {}),
            pipeline_override: {
                [pipelineNode.startFill(region.Id, priority)]: {
                    enabled: true,
                },
                [pipelineNode.selectItemToFill(region.Id, priority)]: {
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
        };
    });
    if (priority > 1) {
        cases.unshift({
            name: "None",
            label: "$task.DeliveryJobs.FillItemPriorityNone",
        });
    }
    return cases;
}

/** 某地区的全部装箱优先级选项：1 个地区开关 + 4 级物品下拉。 */
function buildFillPriorityOptions(region) {
    const options = {
        [optionKey.fillItemPriorities(region)]: buildFillItemPriorityRegionOption(region),
    };

    for (let priority = 1; priority <= DELIVERY_JOB_FILL_ITEM_PRIORITY_COUNT; priority += 1) {
        const key = optionKey.fillItemPriority(region, priority);
        options[key] = {
            type: "select",
            label: `$task.DeliveryJobs.${key}`,
            cases: buildFillItemCases(region, priority),
            default_case: priority === 1 ? region.DefaultFillItem : "None",
        };
    }

    return options;
}

/** 地区开关 + 该地区下所有仓储节点的动作/报价选项（保持这个顺序写入）。 */
function buildRegionAndDepotOptions(region) {
    const options = {
        [region.Id]: buildRegionOption(region),
    };

    for (const depotId of region.Depots) {
        const depot = deliveryJobDepots.find((item) => item.Id === depotId);
        options[depot.Id] = buildDepotOption(depot);
        options[optionKey.quoteThreshold(depot)] = buildQuoteThresholdOption(depot);
        for (const action of QUOTE_ACTIONS) {
            options[optionKey.quoteAction(depot, action.comparison)] = buildQuoteActionOption(depot, action);
        }
    }

    return options;
}

/** 不随地区变化的三个全局开关。 */
function buildGlobalOptions() {
    return {
        DeliveryJobsAutoDeliveryPreferZipline: buildAutoDeliveryPreferZiplineOption(),
        DeliveryJobsOngoingDeliveryFallback: buildOngoingDeliveryFallbackOption(),
        PackCargoSelectItem: buildPackCargoSelectItemOption(),
    };
}

/** 所有 option，按「地区 → 仓储节点 → 全局开关 → 装箱优先级」的顺序写入。 */
function buildTaskOptions() {
    const options = {};

    for (const region of deliveryJobRegions) {
        Object.assign(options, buildRegionAndDepotOptions(region));
    }
    Object.assign(options, buildGlobalOptions());
    for (const region of deliveryJobRegions) {
        Object.assign(options, buildFillPriorityOptions(region));
    }

    return options;
}

/** 任务入口：task 数组只有一条，所有的选项都挂在它下面。 */
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
