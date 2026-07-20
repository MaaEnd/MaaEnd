import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";

import {sellProductLocations, sellProductRegions, settlementData, toPascalCase} from "./model.mjs";
import sellProductAdbRows from "./pipeline-adb-data.mjs";
import sellProductPipelineRows from "./pipeline-data.mjs";
import sellProductSellRows from "./sell-data.mjs";
import sellProductSessionRows from "./session-data.mjs";
import {sellProductTaskRows} from "./task-data.mjs";

const root = sellProductTaskRows[0];

function sortedKeys(value) {
    return Object.keys(value).sort();
}

function readPipeline(url) {
    return JSON.parse(readFileSync(url, "utf8").replace(/^\s*\/\/.*$/gm, ""));
}

test("SellProduct 保留按星期执行入口与任务选项", () => {
    const task = readPipeline(new URL("../../../assets/tasks/SellProduct.json", import.meta.url));
    const pipeline = readPipeline(new URL("../../../assets/resource/pipeline/SellProduct.json", import.meta.url));
    const taskTemplate = readFileSync(new URL("./task-template.jsonc", import.meta.url), "utf8");

    assert.equal(task.task[0].entry, "SellProductSchedule");
    assert.ok(task.task[0].option.includes("SellProductSchedule"));
    assert.equal(task.option.SellProductSchedule.type, "checkbox");
    assert.equal(task.option.SellProductSchedule.cases.length, 7);
    assert.match(taskTemplate, /"entry": "SellProductSchedule"/);
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
});

test("SellProduct 按固定地区顺序售卖且不再使用自动起始地区", () => {
    const pipeline = readPipeline(new URL("../../../assets/resource/pipeline/SellProduct.json", import.meta.url));

    assert.deepEqual(pipeline.SellProductLoop.next, [
        "SellProductValleyIV",
        "SellProductWuling",
        "SellProductTaskEnd",
    ]);
    assert.equal(pipeline.SellProductAuto, undefined);
    assert.equal(pipeline.SellProductAutoValleyIV, undefined);
    assert.equal(pipeline.SellProductAutoWuling, undefined);
});

test("SellProduct templates consume separate minimal projections of the shared location model", () => {
    const locationIds = sellProductLocations.map((location) => location.LocationId);
    for (const rows of [
        sellProductPipelineRows,
        sellProductAdbRows,
        sellProductSessionRows,
        sellProductTaskRows,
    ]) {
        assert.deepEqual(
            rows.map((row) => row.LocationId),
            locationIds,
        );
    }

    assert.deepEqual(sortedKeys(sellProductPipelineRows[0]), [
        "CurrentOperatorROI",
        "LocationDesc",
        "LocationId",
        "MaxTargetBox",
        "QuantityBox",
        "RegionPrefix",
        "TextExpected",
    ]);
    assert.deepEqual(sortedKeys(sellProductAdbRows[0]), [
        "LocationId",
        "MaxTargetBoxAdb",
        "QuantityBoxAdb",
    ]);
    assert.deepEqual(sortedKeys(sellProductSessionRows[0]), [
        "LocationDesc",
        "LocationId",
        "OperatorRegistrationNext",
    ]);
    assert.deepEqual(sortedKeys(root), [
        "LocationId",
        "OperatorRefreshModeCases",
        "PriorityItemCases1",
        "PriorityItemCases2",
        "PriorityItemCases3",
        "PriorityItemCases4",
        "PriorityItemCases5",
        "PriorityItemCases6",
        "PriorityRuleSwitchCases",
        "RegionPrefix",
        "ReserveItemCases1",
        "ReserveItemCases2",
        "ReserveItemCases3",
        "ReserveItemCases4",
        "ReserveItemCases5",
        "ReserveItemCases6",
        "ReserveRuleSwitchCases",
        "SellOptions",
    ]);
    assert.deepEqual(
        sellProductPipelineRows[0].CurrentOperatorROI,
        [
            260,
            568,
            280,
            35,
        ],
    );
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
        assert.deepEqual(row.SellNext, [
            `[Anchor]SellProduct${region.RegionPrefix}PrepareOperatorCache`,
            ...outpostNext,
        ]);
        assert.deepEqual(row.PrepareNext, [
            "SellProductOutpostLocked",
            ...outpostNext,
        ]);
    }
});

test("SellProduct location IDs are derived from the current upstream English names", () => {
    for (const location of sellProductLocations) {
        const settlement = settlementData.settlements[location.SettlementId];
        assert.equal(location.LocationId, toPascalCase(settlement.settlementName.EN || location.SettlementId));
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

test("SellProduct priority switch expands six direct priority slots", () => {
    const enabledCase = root.PriorityRuleSwitchCases.find((itemCase) => itemCase.name === "Yes");
    assert.deepEqual(enabledCase.option, [
        "SellProductPriorityItem1",
        "SellProductPriorityItem2",
        "SellProductPriorityItem3",
        "SellProductPriorityItem4",
        "SellProductPriorityItem5",
        "SellProductPriorityItem6",
    ]);
    assert.equal(enabledCase.pipeline_override, undefined);
    const disabledCase = root.PriorityRuleSwitchCases.find((itemCase) => itemCase.name === "No");
    assert.equal(disabledCase.option, undefined);

    for (const slot of [
        1,
        2,
        3,
        4,
        5,
        6,
    ]) {
        const cases = root[`PriorityItemCases${slot}`];
        const noneCase = cases.find((entry) => entry.name === "None");
        assert.ok(noneCase);
        assert.equal(noneCase.pipeline_override, undefined);

        const itemCase = cases.find((entry) => entry.name === "精选荞愈胶囊");
        assert.ok(itemCase);
        const registration = itemCase.pipeline_override[`SellProductRegisterPriorityItem${slot}`];
        assert.equal(registration.enabled, undefined);
        assert.equal(registration.custom_action_param.operation, "register");
        assert.ok(registration.custom_action_param.item_id.startsWith("item_"));
    }
});

test("SellProduct concrete reserve rule separates itemId attach from quantity input", () => {
    const itemCase = root.ReserveItemCases1.find((entry) => entry.name === "精选荞愈胶囊");
    assert.ok(itemCase);
    assert.deepEqual(itemCase.option, ["SellProductReserveItem1Value"]);
    const registration = itemCase.pipeline_override.SellProductRegisterReserveRule1;
    assert.equal(registration.enabled, undefined);
    assert.ok(registration.attach.item_id.startsWith("item_"));
    assert.equal(registration.custom_action_param, undefined);

    const taskTemplate = readFileSync(new URL("./task-template.jsonc", import.meta.url), "utf8");
    assert.equal((taskTemplate.match(/"quantity": "\{SellProductReserveItem[1-6]Value\}"/g) || []).length, 6);
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
        "SellProductRegisterPriorityItem1",
        "SellProductRegisterPriorityItem2",
        "SellProductRegisterPriorityItem3",
        "SellProductRegisterPriorityItem4",
        "SellProductRegisterPriorityItem5",
        "SellProductRegisterPriorityItem6",
        "SellProductInitializeOperatorSession",
    ];

    for (let index = 0; index < chain.length - 1; index += 1) {
        const node = pipeline[chain[index]];
        assert.ok(node, `missing registration node ${chain[index]}`);
        assert.equal(node.enabled, undefined);
        assert.deepEqual(node.next, [chain[index + 1]]);
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
                `../../../assets/resource/pipeline/SellProduct/Outposts/${location.LocationId}.json`,
                import.meta.url,
            ),
        );
        const prefix = `SellProduct${location.LocationId}`;
        assert.deepEqual(pipeline[`${prefix}Sell`].next, [
            `${prefix}OutpostProsperityAvailable`,
            `${prefix}OutpostProsperityMax`,
        ]);
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

test("SellProduct pipeline templates use one dynamic priority loop instead of fixed attempts", () => {
    const pipelineTemplate = readFileSync(new URL("./pipeline-template.jsonc", import.meta.url), "utf8");
    const adbTemplate = readFileSync(new URL("./pipeline-adb-template.jsonc", import.meta.url), "utf8");

    assert.doesNotMatch(pipelineTemplate, /SellAttempt[1-4]/);
    assert.match(pipelineTemplate, /SellProduct\$\{LocationId\}SelectPriorityItem/);
    assert.match(pipelineTemplate, /SellProduct\$\{LocationId\}PriorityItemsExhausted/);
    assert.equal((pipelineTemplate.match(/"SellProduct\$\{LocationId\}BetterSliding": \{/g) || []).length, 1);
    assert.doesNotMatch(adbTemplate, /BetterSliding[1-4]/);
    assert.match(adbTemplate, /SellProduct\$\{LocationId\}BetterSliding/);
});

test("SellProduct 每轮换货前优先检查调度券不足", () => {
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
    assert.deepEqual(pipeline.SellProductZeroProductAfterChangeStillEmpty.next, [
        "[Anchor]SellProductMarkOutOfStock",
    ]);
});

test("SellProduct 缺货物品通过据点锚点标记并在本次任务内共享", () => {
    for (const location of sellProductLocations) {
        const pipeline = readPipeline(
            new URL(
                `../../../assets/resource/pipeline/SellProduct/Outposts/${location.LocationId}.json`,
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

test("SellProduct 按启用据点边界处理已派驻干员冲突", () => {
    const pipelineTemplate = readFileSync(new URL("./pipeline-template.jsonc", import.meta.url), "utf8");

    for (const usage of [
        "Target",
        "Restore",
    ]) {
        assert.match(pipelineTemplate, new RegExp(`SellProduct\\$\\{LocationId\\}${usage}OperatorManagedConflict`));
        assert.match(pipelineTemplate, new RegExp(`SellProduct\\$\\{LocationId\\}${usage}OperatorProtectedConflict`));
        assert.match(
            pipelineTemplate,
            new RegExp(`SellProduct\\$\\{LocationId\\}Cancel${usage}OperatorAlreadyAssigned`),
        );
        assert.match(
            pipelineTemplate,
            new RegExp(`SellProduct\\$\\{LocationId\\}Close${usage}OperatorLiaisonAfterAlreadyAssigned`),
        );
    }

    assert.equal((pipelineTemplate.match(/"operation": "exclude_selected"/g) || []).length, 2);
    assert.equal((pipelineTemplate.match(/"custom_recognition": "SellProductOperatorConflict"/g) || []).length, 4);
    assert.match(pipelineTemplate, /"result": "managed"/);
    assert.match(pipelineTemplate, /"result": "protected"/);
    assert.match(pipelineTemplate, /"CancelButton"/);
    assert.doesNotMatch(pipelineTemplate, /"GrayCancelButton"/);

    for (const location of sellProductLocations) {
        const pipeline = readPipeline(
            new URL(
                `../../../assets/resource/pipeline/SellProduct/Outposts/${location.LocationId}.json`,
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

test("SellProduct generated outpost nodes report confirmed runtime state changes", () => {
    for (const location of sellProductLocations) {
        const pipeline = readPipeline(
            new URL(
                `../../../assets/resource/pipeline/SellProduct/Outposts/${location.LocationId}.json`,
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

test("SellProduct UI focus messages use complete interface i18n keys", () => {
    const pipelineUrls = [
        new URL("../../../assets/resource/pipeline/SellProduct.json", import.meta.url),
        new URL("../../../assets/resource/pipeline/SellProduct/SellCore.json", import.meta.url),
        new URL("../../../assets/resource/pipeline/SellProduct/OperatorScan.json", import.meta.url),
        ...sellProductLocations.map(
            (location) =>
                new URL(
                    `../../../assets/resource/pipeline/SellProduct/Outposts/${location.LocationId}.json`,
                    import.meta.url,
                ),
        ),
    ];
    const focusKeys = new Set();
    for (const url of pipelineUrls) {
        for (const node of Object.values(readPipeline(url))) {
            for (const value of Object.values(node.focus || {})) {
                assert.match(value, /^\$task\.SellProduct\./, `${url.pathname}: ${value}`);
                focusKeys.add(value.slice(1));
            }
        }
    }

    for (const lang of [
        "zh_cn",
        "zh_tw",
        "en_us",
        "ja_jp",
        "ko_kr",
    ]) {
        const locale = JSON.parse(
            readFileSync(new URL(`../../../assets/locales/interface/${lang}.json`, import.meta.url), "utf8"),
        );
        for (const key of focusKeys) {
            assert.equal(typeof locale[key], "string", `${lang} missing ${key}`);
            assert.notEqual(locale[key].trim(), "", `${lang} has empty ${key}`);
        }
    }
});

test("SellProduct Go UI messages have matching keys in all locales", () => {
    const localeEntries = [
        "zh_cn",
        "zh_tw",
        "en_us",
        "ja_jp",
        "ko_kr",
    ].map((lang) => [
        lang,
        JSON.parse(readFileSync(new URL(`../../../assets/locales/go-service/${lang}.json`, import.meta.url), "utf8")),
    ]);
    const expectedKeys = Object.keys(localeEntries[0][1])
        .filter((key) => key.startsWith("sellproduct."))
        .sort();

    for (const [
        lang,
        locale,
    ] of localeEntries) {
        const keys = Object.keys(locale)
            .filter((key) => key.startsWith("sellproduct."))
            .sort();
        assert.deepEqual(keys, expectedKeys, `${lang} SellProduct keys differ`);
        for (const key of keys) {
            assert.notEqual(locale[key].trim(), "", `${lang} has empty ${key}`);
        }
    }
});
