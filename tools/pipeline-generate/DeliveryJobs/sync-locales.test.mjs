import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import test from "node:test";

import {deliveryJobLocaleEntries} from "./model.mjs";
import {fillMissingLocaleEntries} from "./sync-locales.mjs";

test("DeliveryJobs locale sync fills missing and empty entries without replacing existing text", () => {
    const entries = [
        {key: "iconRecognition.name.item_a", names: {zh_cn: "物品甲", en_us: "Item A"}},
        {key: "iconRecognition.name.item_b", names: {zh_cn: "物品乙", en_us: "Item B"}},
        {key: "iconRecognition.name.item_c", names: {zh_cn: "物品丙", en_us: "Item C"}},
    ];
    const messages = {
        "iconRecognition.name.existing": "Existing",
        "iconRecognition.name.item_a": "Hand-authored disambiguation",
        "iconRecognition.name.item_b": "",
        "iconRecognition.name.item_d": null,
        "task.Other.label": "Other task",
    };
    entries.push({key: "iconRecognition.name.item_d", names: {zh_cn: "物品丁", en_us: "Item D"}});

    const result = fillMissingLocaleEntries(messages, entries, "en_us", "iconRecognition.name.");

    assert.deepEqual(Object.keys(result.messages), [
        "iconRecognition.name.existing",
        "iconRecognition.name.item_a",
        "iconRecognition.name.item_b",
        "iconRecognition.name.item_d",
        "iconRecognition.name.item_c",
        "task.Other.label",
    ]);
    assert.equal(result.messages["iconRecognition.name.item_a"], "Hand-authored disambiguation");
    assert.equal(result.messages["iconRecognition.name.item_b"], "Item B");
    assert.equal(result.messages["iconRecognition.name.item_d"], "Item D");
    assert.equal(result.messages["iconRecognition.name.item_c"], "Item C");
    assert.equal(result.filled, 3);
});

test("DeliveryJobs locale sync requires an existing key group", () => {
    assert.throws(
        () =>
            fillMissingLocaleEntries(
                {"task.Other.label": "Other task"},
                [{key: "global.region.NewRegion", names: {en_us: "New Region"}}],
                "en_us",
                "global.region.",
            ),
        /global\.region\./,
    );
});

test("DeliveryJobs generated locale entries exist in all interface locales", () => {
    const expectedKeys = [
        ...deliveryJobLocaleEntries.regions,
        ...deliveryJobLocaleEntries.items,
    ].map(({key}) => key);
    for (const fileLocale of [
        "zh_cn",
        "zh_tw",
        "en_us",
        "ja_jp",
        "ko_kr",
    ]) {
        const messages = JSON.parse(
            readFileSync(new URL(`../../../assets/locales/interface/${fileLocale}.json`, import.meta.url), "utf8"),
        );
        for (const key of expectedKeys) {
            assert.equal(typeof messages[key], "string", `${fileLocale} missing ${key}`);
            assert.notEqual(messages[key].trim(), "", `${fileLocale} has empty ${key}`);
        }
    }
});
