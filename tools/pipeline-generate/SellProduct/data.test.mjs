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
        "PriorityRuleSwitchCases",
        "RegionPrefix",
        "ReserveItemCases1",
        "ReserveItemCases2",
        "ReserveItemCases3",
        "ReserveItemCases4",
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

test("SellProduct 强制刷新选项保留 PC 与 ADB 共用的当前干员 ROI", () => {
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
    ]);
    assert.equal(enabledCase.pipeline_override, undefined);
});

test("SellProduct priority switch expands four direct priority slots", () => {
    const enabledCase = root.PriorityRuleSwitchCases.find((itemCase) => itemCase.name === "Yes");
    assert.deepEqual(enabledCase.option, [
        "SellProductPriorityItem1",
        "SellProductPriorityItem2",
        "SellProductPriorityItem3",
        "SellProductPriorityItem4",
    ]);
    assert.equal(enabledCase.pipeline_override, undefined);
    const disabledCase = root.PriorityRuleSwitchCases.find((itemCase) => itemCase.name === "No");
    assert.equal(disabledCase.option, undefined);

    for (const slot of [
        1,
        2,
        3,
        4,
    ]) {
        const cases = root[`PriorityItemCases${slot}`];
        const noneCase = cases.find((entry) => entry.name === "None");
        assert.ok(noneCase);
        assert.equal(noneCase.pipeline_override, undefined);

        const itemCase = cases.find((entry) => entry.name === "精选荞愈胶囊");
        assert.ok(itemCase);
        const registration = itemCase.pipeline_override[`SellProductRegisterPriorityItem${slot}`];
        assert.equal(registration.enabled, true);
        assert.equal(registration.custom_action_param.operation, "register");
        assert.ok(registration.custom_action_param.item_id.startsWith("item_"));
    }
});

test("SellProduct concrete reserve rule separates itemId attach from quantity input", () => {
    const itemCase = root.ReserveItemCases1.find((entry) => entry.name === "精选荞愈胶囊");
    assert.ok(itemCase);
    assert.deepEqual(itemCase.option, ["SellProductReserveItem1Value"]);
    const registration = itemCase.pipeline_override.SellProductRegisterReserveRule1;
    assert.equal(registration.enabled, true);
    assert.ok(registration.attach.item_id.startsWith("item_"));
    assert.equal(registration.custom_action_param, undefined);

    const taskTemplate = readFileSync(new URL("./task-template.jsonc", import.meta.url), "utf8");
    assert.equal((taskTemplate.match(/"quantity": "\{SellProductReserveItem[1-4]Value\}"/g) || []).length, 4);
});

test("SellProduct reserve None case does not register a rule", () => {
    const noneCase = root.ReserveItemCases1.find((entry) => entry.name === "None");
    assert.ok(noneCase);
    assert.equal(noneCase.pipeline_override, undefined);
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

test("SellProduct 已派驻干员会被临时排除并从列表顶部重新选择", () => {
    const pipelineTemplate = readFileSync(new URL("./pipeline-template.jsonc", import.meta.url), "utf8");

    for (const usage of [
        "Target",
        "Restore",
    ]) {
        assert.match(pipelineTemplate, new RegExp(`SellProduct\\$\\{LocationId\\}${usage}OperatorAlreadyAssigned`));
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
    assert.match(pipelineTemplate, /"YellowConfirmButtonType1",\s*"GrayCancelButton"/);
});
