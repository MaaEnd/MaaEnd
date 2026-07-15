import assert from "node:assert/strict";
import test from "node:test";

import {sellProductLocations, settlementData, toPascalCase} from "./model.mjs";
import sellProductAdbRows from "./pipeline-adb-data.mjs";
import sellProductPipelineRows from "./pipeline-data.mjs";
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
        "GlobalItemPriorityCases1",
        "GlobalItemPriorityCases2",
        "GlobalItemPriorityCases3",
        "GlobalItemPriorityCases4",
        "GlobalItemPrioritySwitchCases",
        "LocationId",
        "OperatorRefreshModeCases",
        "RegionPrefix",
        "SellOptions",
    ]);
});

test("SellProduct location IDs are derived from the current upstream English names", () => {
    for (const location of sellProductLocations) {
        const settlement = settlementData.settlements[location.SettlementId];
        assert.equal(location.LocationId, toPascalCase(settlement.settlementName.EN || location.SettlementId));
    }
});

test("SellProduct disabled global priority keeps two default sell attempts", () => {
    const disabledCase = root.GlobalItemPrioritySwitchCases.find((itemCase) => itemCase.name === "No");
    assert.ok(disabledCase);

    for (const row of sellProductTaskRows) {
        assert.equal(disabledCase.pipeline_override[`SellProduct${row.LocationId}SellAttempt1`].enabled, true);
        assert.equal(disabledCase.pipeline_override[`SellProduct${row.LocationId}SellAttempt2`].enabled, true);
    }
});

test("SellProduct enabled global priority expands four sibling priority options", () => {
    const enabledCase = root.GlobalItemPrioritySwitchCases.find((itemCase) => itemCase.name === "Yes");
    assert.deepEqual(enabledCase.option, [
        "SellProductPriorityItem1",
        "SellProductPriorityItem2",
        "SellProductPriorityItem3",
        "SellProductPriorityItem4",
    ]);
});

test("SellProduct automatic global priority enables every outpost with a concrete item", () => {
    const autoCase = root.GlobalItemPriorityCases1.find((itemCase) => itemCase.name === "Auto");
    assert.ok(autoCase);

    for (const row of sellProductTaskRows) {
        const attempt = autoCase.pipeline_override[`SellProduct${row.LocationId}SellAttempt1`];
        const select = autoCase.pipeline_override[`SellProduct${row.LocationId}SelectItem1`];
        assert.equal(attempt.enabled, true, `${row.LocationId} should enable its first automatic priority`);
        assert.equal(select.enabled, true, `${row.LocationId} should configure its first automatic priority`);
        assert.ok(select.custom_recognition_param.candidates.length > 0);
    }
});

test("SellProduct concrete global priority only enables outposts that sell the item", () => {
    const itemCase = root.GlobalItemPriorityCases1.find((entry) => entry.name === "精选荞愈胶囊");
    assert.ok(itemCase);

    for (const row of sellProductTaskRows) {
        const attempt = itemCase.pipeline_override[`SellProduct${row.LocationId}SellAttempt1`];
        assert.equal(Boolean(attempt?.enabled), row.RegionPrefix === "ValleyIV");
    }
});
