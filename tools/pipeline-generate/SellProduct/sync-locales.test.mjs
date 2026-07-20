import assert from "node:assert/strict";
import test from "node:test";

import {sellProductLocaleEntries} from "./model.mjs";
import {rebuildSettlementMessages} from "./sync-locales.mjs";

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
