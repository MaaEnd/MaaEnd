import assert from "node:assert/strict";
import test from "node:test";

import {sellProductLocations, sellProductRegions, settlementData, toPascalCase} from "./model.mjs";
import sellProductAdbRows from "./pipeline-adb-data.mjs";
import sellProductPipelineRows from "./pipeline-data.mjs";
import sellProductSellRows from "./sell-data.mjs";
import {buildOperatorCaseEntry, sellProductTaskRows} from "./task-data.mjs";

function sortedKeys(value) {
    return Object.keys(value).sort();
}

test("SellProduct templates consume separate minimal projections of the shared location model", () => {
    const locationIds = sellProductLocations.map((location) => location.LocationId);
    assert.deepEqual(
        sellProductPipelineRows.map((row) => row.LocationId),
        locationIds,
    );
    assert.deepEqual(
        sellProductAdbRows.map((row) => row.LocationId),
        locationIds,
    );
    assert.deepEqual(
        sellProductTaskRows.map((row) => row.LocationId),
        locationIds,
    );

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
    assert.deepEqual(sortedKeys(sellProductTaskRows[0]), [
        "ItemCases1",
        "ItemCases2",
        "ItemCases3",
        "ItemCases4",
        "LocationId",
        "RegionPrefix",
        "RestoreOperatorCases",
        "SellOptions",
        "TargetOperatorCases",
        "TargetOperatorDefaultCase",
    ]);
});

test("SellProduct region entry rows contain every generated location", () => {
    assert.deepEqual(
        sellProductSellRows.map((row) => row.RegionPrefix),
        sellProductRegions.map((region) => region.RegionPrefix),
    );

    for (const row of sellProductSellRows) {
        const region = sellProductRegions.find((entry) => entry.RegionPrefix === row.RegionPrefix);
        assert.deepEqual(
            row.Next,
            region.LocationIds.map((locationId) => `[JumpBack]SellProduct${locationId}`).concat(
                "SellProductLoop",
                "[JumpBack]SceneEnterMenuRegionalDevelopment",
            ),
        );
    }
});

test("SellProduct location IDs are derived from the current upstream English names", () => {
    for (const location of sellProductLocations) {
        const settlement = settlementData.settlements[location.SettlementId];
        assert.equal(location.LocationId, toPascalCase(settlement.settlementName.EN || location.SettlementId));
    }
});

function collectOperatorCases() {
    return sellProductTaskRows.flatMap((row) => [
        ...row.TargetOperatorCases,
        ...row.RestoreOperatorCases.filter((entry) => entry.name !== "DoNotRestore"),
    ]);
}

test("SellProduct operator OCR expected candidates are deduplicated", () => {
    for (const entry of collectOperatorCases()) {
        const expected =
            entry.pipeline_override[
                Object.keys(entry.pipeline_override).find(
                    (key) => key.endsWith("CurrentTargetOperator") || key.endsWith("CurrentRestoreOperator"),
                )
            ]?.expected;

        assert.deepEqual(
            expected,
            [...new Set(expected)],
            `${entry.name} should not contain duplicate OCR expected candidates`,
        );
    }
});

test("SellProduct operator case entry escapes regex characters and reports missing locale", () => {
    const warnings = [];
    const originalWarn = console.warn;
    console.warn = (message) => warnings.push(message);

    try {
        const entry = buildOperatorCaseEntry({
            charId: "chr_test_regex",
            name: {
                CN: "A+B",
                TC: "A+B",
                EN: "Regex (Test)",
                JP: "A+B",
                KR: "테스트",
            },
        });

        assert.equal(entry.name, "RegexTest");
        assert.equal(entry.label, "$operator.RegexTest");
        assert.deepEqual(entry.expected, [
            "A\\+B",
            "Regex \\(Test\\)",
            "테스트",
        ]);
        assert.match(warnings[0], /operator\.RegexTest/);
    } finally {
        console.warn = originalWarn;
    }
});
