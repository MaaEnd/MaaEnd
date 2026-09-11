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

// 自动注释始终使用当前数据源名称，方便对照人工填写的地标名称。
function renderEndpointLabels(labels, destinations) {
    const destinationById = new Map(
        destinations.map((destination) => [
            destination.id,
            destination,
        ]),
    );
    const mapLabels = new Map(
        commissionMaps.map(({MapId, AreaName}) => [
            MapId,
            AreaName,
        ]),
    );
    const blocks = [];
    let previousMap;
    let previousArea;
    for (const [
        id,
        entry,
    ] of Object.entries(labels)) {
        const destination = destinationById.get(id);
        const lines = [];
        const comment = (text) => lines.push(`    // ${text.replace(/[\r\n\u2028\u2029]+/g, " ")}`);
        if (destination) {
            if (destination.map !== previousMap) {
                comment(mapLabels.get(destination.map) ?? destination.map);
            }
            if (destination.map !== previousMap || destination.areaId !== previousArea) {
                comment(destination.area.zh_cn);
            }
            comment(destination.name.zh_cn);
        }
        previousMap = destination?.map;
        previousArea = destination?.areaId;
        // 保留原有条目顺序和所有已填写字段；每次重建注释，避免名称更新后注释过期。
        lines.push(
            JSON.stringify({[id]: entry}, null, 4)
                .split("\n")
                .slice(1, -1)
                .join("\n"),
        );
        blocks.push(lines.join("\n"));
    }
    return `{\n${blocks.join(",\n")}\n}\n`;
}

// 只补齐缺少的终点和语言字段，不覆盖人工名称，也不把回退名称写入配置。
export function syncEndpointLabels(destinations, path = new URL("./endpoint-labels.json", import.meta.url)) {
    const original = existsSync(path) ? readFileSync(path, "utf8") : "{}\n";
    const labels = parseJsonc(original, String(path));
    if (!labels || typeof labels !== "object" || Array.isArray(labels)) {
        throw new Error("[SeizeDeliveryJobs] endpoint-labels.json 必须是以终点 ID 为键的对象");
    }
    for (const {id} of destinations) {
        const entry = labels[id] ?? {};
        if (typeof entry !== "object" || Array.isArray(entry)) {
            throw new Error(`[SeizeDeliveryJobs] 终点 ${id} 的展示名称必须是语言映射对象`);
        }
        for (const locale of ENDPOINT_LOCALES) {
            if (entry[locale] !== undefined && typeof entry[locale] !== "string") {
                throw new Error(`[SeizeDeliveryJobs] ${id}.${locale} 必须是字符串，未编辑时请留空`);
            }
            if (entry[locale] !== undefined) continue;
            entry[locale] = "";
        }
        labels[id] = entry;
    }
    const content = renderEndpointLabels(labels, destinations);
    if (content !== original) writeFileSync(path, content, "utf8");
    return labels;
}

// 各语言独立回退到当前游戏数据中的收货人名称，避免生成空白选项或提示。
export function resolveEndpointNames(destination, labels) {
    return Object.fromEntries(
        ENDPOINT_LOCALES.map((locale) => {
            const name = labels[destination.id]?.[locale]?.trim() || destination.name[locale]?.trim();
            if (!name) {
                throw new Error(`[SeizeDeliveryJobs] ${destination.id}.${locale} 缺少展示名称和数据源回退名称`);
            }
            return [
                locale,
                name,
            ];
        }),
    );
}
