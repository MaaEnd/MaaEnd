import assert from "node:assert/strict";
import test from "node:test";

import {sellProductLocaleEntries} from "./model.mjs";
import {insertMissingItemMessages, rebuildSettlementMessages} from "./sync-locales.mjs";

test("SellProduct locale settlement keys are rebuilt in game order", () => {
    const expectedKeys = sellProductLocaleEntries.settlements.map(({key}) => key);
    const messages = {
        "task.SellProduct.label": "一键售卖产品",
        [expectedKeys[0]]: "人工修改的据点名",
        [expectedKeys[4]]: "原有据点名",
        "task.SellProduct.ValleyIVRemovedOutpost": "已删除据点",
        [expectedKeys[2]]: "原有据点名",
        "task.VisitFriends.label": "拜访好友",
    };

    const result = rebuildSettlementMessages(messages, "CN", "task.VisitFriends.label");
    const actualKeys = Object.keys(result.messages).filter((key) => expectedKeys.includes(key));

    assert.deepEqual(actualKeys, expectedKeys);
    assert.equal(result.messages[expectedKeys[0]], sellProductLocaleEntries.settlements[0].names.CN);
    assert.equal(Object.hasOwn(result.messages, "task.SellProduct.ValleyIVRemovedOutpost"), false);
    assert.equal(result.removed, 1);
    assert.equal(result.inserted, expectedKeys.length - 3);
    assert.equal(result.updated, 3);
});

test("SellProduct locale item keys are inserted after the last item key", () => {
    const entries = [
        {key: "item.NewItemA", names: {zh_cn: "新物品甲", en_us: "New Item A"}},
        // 中文名已被既有 item.* 键覆盖，不再重复创建。
        {key: "item.NewItemB", names: {zh_cn: "共享物品", en_us: "Shared Item"}},
    ];
    const messages = {
        "item.Existing": "既有物品",
        "item.Shared": "共享物品",
        "task.Other.label": "其他任务",
    };

    const result = insertMissingItemMessages(messages, entries, "en_us", new Set(["共享物品"]));

    assert.deepEqual(Object.keys(result.messages), [
        "item.Existing",
        "item.Shared",
        "item.NewItemA",
        "task.Other.label",
    ]);
    assert.equal(result.messages["item.NewItemA"], "New Item A");
    assert.equal(result.inserted, 1);
});

test("SellProduct locale item sync keeps existing keys and falls back to Chinese names", () => {
    const entries = [
        {key: "item.Existing", names: {zh_cn: "既有物品"}},
        {key: "item.NewItem", names: {zh_cn: "新物品"}},
    ];
    const messages = {
        "item.Existing": "人工维护的物品名",
        "task.Other.label": "其他任务",
    };

    const result = insertMissingItemMessages(messages, entries, "ja_jp", new Set());

    assert.equal(result.messages["item.Existing"], "人工维护的物品名");
    assert.equal(result.messages["item.NewItem"], "新物品");
    assert.equal(result.inserted, 1);
});

test("SellProduct locale item sync requires an existing item group", () => {
    const entries = [{key: "item.NewItem", names: {zh_cn: "新物品"}}];
    assert.throws(
        () => insertMissingItemMessages({"task.Other.label": "其他任务"}, entries, "zh_cn", new Set()),
        /item\.\*/,
    );
});
