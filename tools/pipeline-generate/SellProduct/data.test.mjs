import assert from "node:assert/strict";
import test from "node:test";

import sellProductSellRows from "./sell-data.mjs";
import {buildOperatorCaseEntry, settlementFlatRows} from "./data.mjs";

test("SellProduct region entry rows contain every generated location", () => {
    for (const row of sellProductSellRows) {
        assert.deepEqual(
            row.Next,
            settlementFlatRows
                .filter((location) => location.RegionPrefix === row.RegionPrefix)
                .map((location) => `[JumpBack]SellProduct${location.LocationId}`)
                .concat("SellProductLoop", "[JumpBack]SceneEnterMenuRegionalDevelopment"),
        );
    }
});

function collectOperatorCases() {
    return settlementFlatRows.flatMap((row) => [
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
