import {readFileSync} from "node:fs";
import {resolve} from "node:path";

import {dataDir, repoRoot} from "../utils/paths.mjs";

function loadUsableItems() {
    const sourcePath = resolve(dataDir, "usable_items.json");
    let data;
    try {
        data = JSON.parse(readFileSync(sourcePath, "utf8"));
    } catch (cause) {
        throw new Error(`[StashBackpack] 无法读取 ${sourcePath}，请先运行 uv run zmdmap generate --only usable_items`, {
            cause,
        });
    }
    const items = data?.items;
    if (!items || typeof items !== "object" || Array.isArray(items)) {
        throw new Error(`[StashBackpack] ${sourcePath} 的 items 必须是对象`);
    }
    if (Object.keys(items).length === 0) {
        throw new Error(`[StashBackpack] ${sourcePath} 中没有可供选择的主动持续增幅道具`);
    }
    return items;
}

function loadTacticalItemIds() {
    const sourcePath = resolve(dataDir, "item_mini_table.json");
    let data;
    try {
        data = JSON.parse(readFileSync(sourcePath, "utf8"));
    } catch (cause) {
        throw new Error(`[StashBackpack] 无法读取 ${sourcePath}，请先生成 item_mini_table.json`, {cause});
    }
    const records = Object.entries(data).filter(
        ([
            ,
            item,
        ]) => item && typeof item === "object",
    );
    if (
        records.length === 0 ||
        records.some(
            ([
                ,
                item,
            ]) => !("type" in item),
        )
    ) {
        throw new Error(`[StashBackpack] ${sourcePath} 缺少完整的 ItemTable.type 字段，请更新物品数据表`);
    }

    const ids = records
        .filter(
            ([
                ,
                item,
            ]) => item.storageKind === "Normal" && item.categoryType === "Usable" && item.type === "TacticalItem",
        )
        .map(([itemId]) => itemId);
    const catalogPath = resolve(repoRoot, "assets/data/IconRecognition/recognition_items.json");
    const catalog = JSON.parse(readFileSync(catalogPath, "utf8"));
    const missing = ids.filter((itemId) => !catalog[itemId]);
    if (missing.length > 0) {
        throw new Error(`[StashBackpack] 以下战术物品缺少 IconRecognition 数据：${missing.join(", ")}`);
    }
    if (ids.length === 0) {
        throw new Error(`[StashBackpack] ${sourcePath} 中没有 TacticalItem`);
    }
    return ids.sort((a, b) => {
        const left = catalog[a];
        const right = catalog[b];
        return (
            (right.sortId1 ?? 0) - (left.sortId1 ?? 0) ||
            (right.sortId2 ?? 0) - (left.sortId2 ?? 0) ||
            b.localeCompare(a)
        );
    });
}

const items = loadUsableItems();
const itemIds = Object.keys(items).sort();
const tacticalItemIds = loadTacticalItemIds();
const slots = [
    1,
    2,
    3,
    4,
];
const regions = [
    {id: "ValleyIV", scene: "SceneEnterWorldValleyIVTheHubHubBase"},
    {id: "Wuling", scene: "SceneEnterWorldWulingWulingCityFangxingAvenue"},
];

// 存放背包的三个选项被 AutoCollect、AutoEssence、AutoStockStaple、GiftOperator、ItemTransfer 等任务引用，
// 以 JSON 原文维护在 stash-backpack-options.json，生成时原样写入。
const stashBackpackOptions = JSON.parse(
    readFileSync(new URL("./stash-backpack-options.json", import.meta.url), "utf8"),
);

const LOCALE_TEXTS = {
    zh_cn: {
        suffix: "CN",
        label: "🎒背包存取与补给",
        description: "存放背包物资，并可按需补充战术物品、为干员使用消耗品",
        stashSubTaskLabel: "存放背包",
        useConsumableSubTaskLabel: "取用可用道具",
        itemLabel: "选择使用的消耗品",
        tacticalItemsLabel: "补充战术物品",
        tacticalItemLabel: "选择需要补充的战术物品",
        enabledLabel: "使用消耗品",
        slotLabels: [
            "一号位",
            "二号位",
            "三号位",
            "四号位",
        ],
        regionLabel: "选择使用的地区",
        regionNames: {ValleyIV: "四号谷地", Wuling: "武陵"},
        notFound: "未找到对应消耗品，继续使用流程",
    },
    zh_tw: {
        suffix: "TC",
        label: "🎒背包存取與補給",
        description: "存放背包物資，並可按需補充戰術物品、為幹員使用消耗品",
        stashSubTaskLabel: "存放背包",
        useConsumableSubTaskLabel: "取用可用道具",
        itemLabel: "選擇使用的消耗品",
        tacticalItemsLabel: "補充戰術物品",
        tacticalItemLabel: "選擇需要補充的戰術物品",
        enabledLabel: "使用消耗品",
        slotLabels: [
            "一號位",
            "二號位",
            "三號位",
            "四號位",
        ],
        regionLabel: "選擇使用的地區",
        regionNames: {ValleyIV: "四號谷地", Wuling: "武陵"},
        notFound: "未找到對應消耗品，繼續使用流程",
    },
    en_us: {
        suffix: "EN",
        label: "🎒Stash & Resupply",
        description: "Stash backpack items, and optionally restock tactical items and use consumables on operators.",
        stashSubTaskLabel: "Stash backpack",
        useConsumableSubTaskLabel: "Retrieve usable items",
        itemLabel: "Consumable to Use",
        tacticalItemsLabel: "Restock Tactical Items",
        tacticalItemLabel: "Tactical Item to Restock",
        enabledLabel: "Use Consumables",
        slotLabels: [
            "Slot 1",
            "Slot 2",
            "Slot 3",
            "Slot 4",
        ],
        regionLabel: "Region to Use In",
        regionNames: {ValleyIV: "Valley IV", Wuling: "Wuling"},
        notFound: "Consumable not found in storage; continuing to the use flow.",
    },
    ja_jp: {
        suffix: "JP",
        label: "🎒バックパック収納と補給",
        description:
            "バックパックのアイテムを収納し、必要に応じて戦術アイテムの補充やオペレーターへの消耗品使用も行います。",
        stashSubTaskLabel: "バックパックを収納",
        useConsumableSubTaskLabel: "使用可能アイテムを取得",
        itemLabel: "使用する消耗品",
        tacticalItemsLabel: "戦術アイテムを補充",
        tacticalItemLabel: "補充する戦術アイテム",
        enabledLabel: "消耗品を使用",
        slotLabels: [
            "1番目の枠",
            "2番目の枠",
            "3番目の枠",
            "4番目の枠",
        ],
        regionLabel: "使用する地域",
        regionNames: {ValleyIV: "谷地IV", Wuling: "武陵"},
        notFound: "該当する消耗品が見つからないため、使用フローを続行します。",
    },
    ko_kr: {
        suffix: "KR",
        label: "🎒배낭 보관 및 보급",
        description: "배낭 물품을 보관하고, 필요에 따라 전술 아이템을 보충하거나 오퍼레이터에게 소모품을 사용합니다.",
        stashSubTaskLabel: "배낭 보관",
        useConsumableSubTaskLabel: "사용 가능 아이템 가져오기",
        itemLabel: "사용할 소모품",
        tacticalItemsLabel: "전술 아이템 보충",
        tacticalItemLabel: "보충할 전술 아이템",
        enabledLabel: "소모품 사용",
        slotLabels: [
            "1번 슬롯",
            "2번 슬롯",
            "3번 슬롯",
            "4번 슬롯",
        ],
        regionLabel: "사용할 지역",
        regionNames: {ValleyIV: "밸리 IV", Wuling: "무릉"},
        notFound: "해당 소모품을 찾지 못해 사용 절차를 계속 진행합니다.",
    },
};

export const stashBackpackLocaleMessages = Object.fromEntries(
    Object.entries(LOCALE_TEXTS).map(
        ([
            locale,
            texts,
        ]) => {
            const messages = {
                "task.StashBackpack.label": texts.label,
                "task.StashBackpack.description": texts.description,
                "task.StashBackpack.option.StashBackpackTaskSubTask.label": texts.stashSubTaskLabel,
                "task.StashBackpack.option.UseConsumableSubTask.label": texts.useConsumableSubTaskLabel,
                "task.UseConsumable.focus.usable_item_not_found": texts.notFound,
                "option.UseConsumableItem.label": texts.itemLabel,
                "option.UseConsumableTacticalItems.label": texts.tacticalItemsLabel,
                "option.UseConsumableTacticalItem.label": texts.tacticalItemLabel,
                "option.UseConsumableEnabled.label": texts.enabledLabel,
                "option.UseConsumableRegion.label": texts.regionLabel,
            };
            for (const slot of slots) {
                messages[`option.UseConsumableOperators.cases.OperatorSlot${slot}.label`] = texts.slotLabels[slot - 1];
            }
            for (const region of regions) {
                messages[`option.UseConsumableRegion.cases.${region.id}.label`] = texts.regionNames[region.id];
            }
            for (const itemId of itemIds) {
                messages[`iconRecognition.name.${itemId}`] = items[itemId]?.names?.[texts.suffix];
            }
            return [
                locale,
                messages,
            ];
        },
    ),
);

export default [
    {
        name: "StashBackpack",
        label: "$task.StashBackpack.label",
        entry: "StashBackpackEntry",
        description: "$task.StashBackpack.description",
        group: ["backpack"],
        controller: [
            "ADB",
            "CloudADB",
            "PlayCover",
            "Linux-Gamescope",
            "Linux-ScreenCast",
            "Linux-Wlroots",
            "MacOS-Background",
            "MacOS-Front",
            "Win32-Front",
        ],
        slots,
        defaultSlotCase: "Yes",
        itemIds,
        defaultItemId: itemIds[0],
        tacticalItemIds,
        regions,
        defaultRegion: "ValleyIV",
        stashBackpackOptions,
    },
];
