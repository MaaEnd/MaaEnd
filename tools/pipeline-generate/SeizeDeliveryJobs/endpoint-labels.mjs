import {existsSync, readFileSync, writeFileSync} from "node:fs";

import {parseJsonc} from "../jsonc.mjs";
import {commissionMaps} from "./commission-data.mjs";

export const ENDPOINT_LOCALES = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];

// 生成器为数据源里的每个终点补齐一条条目：五种语言的地点名（该终点 NPC 所在的地点；
// 未登记时为空字符串，此时该语言回退到数据源收货人名称）和 direction 方位代码（未填写时为 0，见 DIRECTION_TEXTS）。
// 地点名必须逐字抄自官方 i18n 表；查不到词条时沿用既有约定称呼
//（例如资源回收站的「猫头鹰」），不要自行翻译。

function validateEntry(id, entry, known) {
    if (!known.has(id)) {
        throw new Error(`[SeizeDeliveryJobs] endpoint-labels.json 的终点 ${id} 已不在数据源目录中，请删除`);
    }
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
        throw new Error(`[SeizeDeliveryJobs] 终点 ${id} 必须是对象`);
    }
    const locales = Object.keys(entry).filter((key) => key !== "direction");
    for (const key of locales) {
        if (!ENDPOINT_LOCALES.includes(key)) {
            throw new Error(`[SeizeDeliveryJobs] 终点 ${id} 只支持五种语言字段和 direction，收到未知字段 ${key}`);
        }
    }
    if (locales.length !== ENDPOINT_LOCALES.length) {
        const missing = ENDPOINT_LOCALES.filter((locale) => !locales.includes(locale));
        throw new Error(`[SeizeDeliveryJobs] 终点 ${id} 必须写全五种语言字段，缺少 ${missing.join("、")}`);
    }
    for (const locale of locales) {
        if (typeof entry[locale] !== "string") {
            throw new Error(`[SeizeDeliveryJobs] ${id}.${locale} 必须是字符串，未登记时留空`);
        }
    }
    const filled = locales.filter((locale) => entry[locale].trim());
    if (filled.length !== 0 && filled.length !== ENDPOINT_LOCALES.length) {
        const missing = ENDPOINT_LOCALES.filter((locale) => !entry[locale].trim());
        throw new Error(`[SeizeDeliveryJobs] ${id} 的地点名要么五种语言全填，要么都留空，缺少 ${missing.join("、")}`);
    }
    const direction = entry.direction;
    if (direction !== undefined) {
        if (!Number.isInteger(direction) || direction < 0 || direction > 8) {
            throw new Error(
                `[SeizeDeliveryJobs] ${id}.direction 必须是 0-8 的方位代码（0 表示不加方位：${directionHint()}），或直接省略该字段`,
            );
        }
    }
}

function directionHint() {
    return Object.entries(DIRECTION_TEXTS)
        .map(
            ([
                code,
                texts,
            ]) => `${code}=${texts?.zh_cn ?? "无方位"}`,
        )
        .join("、");
}

// 自动注释用数据源名称和登记后的展示名对照；每次重建注释，避免名称更新后注释过期。
function renderEndpointLabels(labels, destinations) {
    const mapLabels = new Map(
        commissionMaps.map(({MapId, AreaName}) => [
            MapId,
            AreaName,
        ]),
    );
    const blocks = [];
    let previousMap;
    let previousArea;
    for (const destination of destinations) {
        const entry = labels[destination.id];
        const lines = [];
        const comment = (text) => lines.push(`    // ${text.replace(/[\r\n\u2028\u2029]+/g, " ")}`);
        if (destination.map !== previousMap) {
            comment(mapLabels.get(destination.map) ?? destination.map);
        }
        if (destination.map !== previousMap || destination.areaId !== previousArea) {
            comment(destination.area.zh_cn);
        }
        comment(destination.name.zh_cn);
        previousMap = destination.map;
        previousArea = destination.areaId;
        // 条目正文交给 JSON 重新排版：字段值原样写出，字段集合与顺序是 sync 归一化后的
        //「五语言 + direction」，条目内部的手写注释不会被保留。
        lines.push(
            JSON.stringify({[destination.id]: entry}, null, 4)
                .split("\n")
                .slice(1, -1)
                .join("\n"),
        );
        blocks.push(lines.join("\n"));
    }
    return `{\n${blocks.join(",\n")}\n}\n`;
}

// 校验登记内容，并为每个终点补齐五语言字段与 direction，再重建注释与整体排版。
export function syncEndpointLabels(destinations, path = new URL("./endpoint-labels.json", import.meta.url)) {
    const original = existsSync(path) ? readFileSync(path, "utf8") : "{}\n";
    const labels = parseJsonc(original, String(path));
    if (!labels || typeof labels !== "object" || Array.isArray(labels)) {
        throw new Error("[SeizeDeliveryJobs] endpoint-labels.json 必须是以终点 ID 为键的对象");
    }
    const known = new Set(destinations.map(({id}) => id));
    for (const [
        id,
        entry,
    ] of Object.entries(labels)) {
        validateEntry(id, entry, known);
    }
    // 每个终点都保留一条条目：补齐五种语言字段（未登记时为空）和 direction（未填写时为 0）。
    const normalized = {};
    for (const {id} of destinations) {
        const entry = labels[id] ?? {};
        const filled = {};
        for (const locale of ENDPOINT_LOCALES) {
            filled[locale] = typeof entry[locale] === "string" ? entry[locale] : "";
        }
        filled.direction = entry.direction ?? 0;
        normalized[id] = filled;
    }
    const content = renderEndpointLabels(normalized, destinations);
    if (content !== original) writeFileSync(path, content, "utf8");
    return labels;
}

// 方位代码：1-8 对应八个方向，0 或不写该字段表示不加方位。
// 五种语言措辞在这里集中维护（与 sync-locales.mjs 的 LOCALE_TEXTS 同源），
// 避免同一条方位在每个终点重复填五遍。
export const DIRECTION_TEXTS = {
    1: {zh_cn: "上", zh_tw: "上", en_us: "Top", ja_jp: "上", ko_kr: "상"},
    2: {zh_cn: "下", zh_tw: "下", en_us: "Bottom", ja_jp: "下", ko_kr: "하"},
    3: {zh_cn: "左", zh_tw: "左", en_us: "Left", ja_jp: "左", ko_kr: "좌"},
    4: {zh_cn: "右", zh_tw: "右", en_us: "Right", ja_jp: "右", ko_kr: "우"},
    5: {zh_cn: "左上", zh_tw: "左上", en_us: "Top Left", ja_jp: "左上", ko_kr: "좌상"},
    6: {zh_cn: "左下", zh_tw: "左下", en_us: "Bottom Left", ja_jp: "左下", ko_kr: "좌하"},
    7: {zh_cn: "右上", zh_tw: "右上", en_us: "Top Right", ja_jp: "右上", ko_kr: "우상"},
    8: {zh_cn: "右下", zh_tw: "右下", en_us: "Bottom Right", ja_jp: "右下", ko_kr: "우하"},
};

function directionTexts(code) {
    return code === 0 ? null : DIRECTION_TEXTS[code];
}

// 方位括号风格：中日韩用全角，英文用半角并在前面留一个空格。
const DIRECTION_BRACKETS = {
    zh_cn: [
        "（",
        "）",
    ],
    zh_tw: [
        "（",
        "）",
    ],
    en_us: [
        " (",
        ")",
    ],
    ja_jp: [
        "（",
        "）",
    ],
    ko_kr: [
        "（",
        "）",
    ],
};

// 展示名称 = 本文件登记的地点名或数据源收货人名称，再按方位代码拼上五种语言措辞；
// 名称不写进方位字段，避免游戏改名后与本文件脱节，也避免从中文机械直译。
export function resolveEndpointNames(destination, labels) {
    const entry = labels[destination.id];
    const texts = directionTexts(entry?.direction ?? 0);
    return Object.fromEntries(
        ENDPOINT_LOCALES.map((locale) => {
            const placeName = entry?.[locale];
            const name = (
                typeof placeName === "string" && placeName.trim() ? placeName : destination.name[locale]
            )?.trim();
            if (!name) {
                throw new Error(`[SeizeDeliveryJobs] ${destination.id}.${locale} 缺少展示名称`);
            }
            const direction = texts?.[locale]?.trim();
            const brackets = DIRECTION_BRACKETS[locale];
            return [
                locale,
                direction ? `${name}${brackets[0]}${direction}${brackets[1]}` : name,
            ];
        }),
    );
}
