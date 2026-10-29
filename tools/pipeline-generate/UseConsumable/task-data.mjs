import {readFileSync} from "node:fs";
import {resolve} from "node:path";

import {dataDir} from "../utils/paths.mjs";

function loadUsableItems() {
    const sourcePath = resolve(dataDir, "usable_items.json");
    let data;
    try {
        data = JSON.parse(readFileSync(sourcePath, "utf8"));
    } catch (cause) {
        throw new Error(`[UseConsumable] 无法读取 ${sourcePath}，请先运行 uv run zmdmap generate --only usable_items`, {
            cause,
        });
    }
    const items = data?.items;
    if (!items || typeof items !== "object" || Array.isArray(items)) {
        throw new Error(`[UseConsumable] ${sourcePath} 的 items 必须是对象`);
    }
    if (Object.keys(items).length === 0) {
        throw new Error(`[UseConsumable] ${sourcePath} 中没有可供选择的主动持续增幅道具`);
    }
    return items;
}

const items = loadUsableItems();
const itemIds = Object.keys(items).sort();
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

const LOCALE_TEXTS = {
    zh_cn: {
        suffix: "CN",
        label: "💊使用消耗品",
        description: "选择需要使用的消耗品，并自动喂给选定的干员",
        itemLabel: "选择使用的消耗品",
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
        label: "💊使用消耗品",
        description: "選擇需要使用的消耗品，並自動餵給選定的幹員",
        itemLabel: "選擇使用的消耗品",
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
        label: "💊 Use Consumable",
        description: "Choose the consumable to use and automatically give it to the selected operators.",
        itemLabel: "Consumable to Use",
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
        label: "💊消耗品を使用",
        description: "使用する消耗品を選択し、選んだオペレーターに自動で使用します。",
        itemLabel: "使用する消耗品",
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
        label: "💊 소모품 사용",
        description: "사용할 소모품을 선택하면 선택한 오퍼레이터에게 자동으로 사용합니다.",
        itemLabel: "사용할 소모품",
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

export const useConsumableLocaleMessages = Object.fromEntries(
    Object.entries(LOCALE_TEXTS).map(
        ([
            locale,
            texts,
        ]) => {
            const messages = {
                "task.UseConsumable.label": texts.label,
                "task.UseConsumable.description": texts.description,
                "task.UseConsumable.focus.usable_item_not_found": texts.notFound,
                "option.UseConsumableItem.label": texts.itemLabel,
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
        name: "UseConsumable",
        label: "$task.UseConsumable.label",
        entry: "UseConsumableFailureCollectorReset",
        description: "$task.UseConsumable.description",
        group: ["backpack"],
        controller: [
            "ADB",
            "CloudADB",
            "PlayCover",
            "Linux-Gamescope",
            "Linux-ScreenCast",
            "Linux-Wlroots",
            "Win32-Front",
        ],
        slots,
        defaultSlotCase: "Yes",
        itemIds,
        defaultItemId: itemIds[0],
        regions,
        defaultRegion: "ValleyIV",
    },
];
