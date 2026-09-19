// 任务前端「物品选项」的展示顺序与图标。
//
// 排序规则（MaaEnd 前端约定）：
// 1. 一级：物品大类（`category`），顺序取游戏仓库顺序，即目录里每个大类最小的
//    `sortId1`/`sortId2`；`sortId` 缺失的大类（如“独立资源”货币）排在最后；
// 2. 二级：稀有度从低到高；
// 3. 三级：游戏仓库顺序（`sortId1`/`sortId2`），最后按 item id 保证确定性。
//
// 图标统一使用 `resource/image/icon/<iconId>.png`（16×16、带品质遮罩的展示图标）。

import {readJsonc} from "../jsonc.mjs";

const UNRANKED = 9999;

export const iconRecognitionCatalog = readJsonc(
    new URL("../../../assets/data/IconRecognition/recognition_items.json", import.meta.url),
);

function storageKey(entry) {
    return [
        entry.sortId1 ?? UNRANKED,
        entry.sortId2 ?? UNRANKED,
    ];
}

const CATEGORY_RANK = (() => {
    const minByCategory = new Map();
    for (const entry of Object.values(iconRecognitionCatalog)) {
        const category = entry.category ?? "";
        const key = storageKey(entry);
        const current = minByCategory.get(category);
        if (!current || key[0] < current[0] || (key[0] === current[0] && key[1] < current[1])) {
            minByCategory.set(category, key);
        }
    }
    return new Map(
        [
            ...minByCategory.entries(),
        ]
            .sort((left, right) => left[1][0] - right[1][0] || left[1][1] - right[1][1])
            .map(([category], index) => [
                category,
                index,
            ]),
    );
})();

/** 物品在任务前端使用的 16×16 图标路径；目录未收录时返回 undefined。 */
export function itemIconPath(itemId) {
    const entry = iconRecognitionCatalog[itemId];
    if (!entry || !entry.iconId) return undefined;
    return `resource/image/icon/${entry.iconId}.png`;
}

/** 物品的展示排序 key；目录未收录的物品排到最后。 */
export function itemDisplayRank(itemId) {
    const entry = iconRecognitionCatalog[itemId];
    if (!entry)
        return [
            Number.MAX_SAFE_INTEGER,
            0,
            UNRANKED,
            UNRANKED,
        ];
    const [
        sortId1,
        sortId2,
    ] = storageKey(entry);
    return [
        CATEGORY_RANK.get(entry.category ?? "") ?? CATEGORY_RANK.size,
        entry.rarity ?? 0,
        sortId1,
        sortId2,
    ];
}

/** 任务前端物品选项的比较器：大类 → 稀有度升序 → 游戏仓库顺序 → item id。 */
export function compareItemDisplayOrder(left, right) {
    const rankLeft = itemDisplayRank(left);
    const rankRight = itemDisplayRank(right);
    for (let index = 0; index < rankLeft.length; index += 1) {
        if (rankLeft[index] !== rankRight[index]) return rankLeft[index] - rankRight[index];
    }
    if (left === right) return 0;
    return left < right ? -1 : 1;
}
