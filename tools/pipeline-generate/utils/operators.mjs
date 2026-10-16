import {resolve} from "node:path";

import {readJsonc} from "../jsonc.mjs";
import {dataDir} from "./paths.mjs";

export const OPERATOR_LOCALES = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];

// 与据点交易原有规则一致，选项/节点后缀由英文名称派生。
export function toPascalCase(str) {
    return str
        .split(/[^a-zA-Z0-9]+/)
        .filter(Boolean)
        .map((part) => part[0].toUpperCase() + part.slice(1))
        .join("");
}

export function isAdminOperator(operator) {
    return operator.id === "chr_0002_endminm" || operator.id === "chr_0003_endminf";
}

export function getOperatorCaseName(operator) {
    return toPascalCase(operator.names.en_us);
}

// fetch / generate 入库时从 sell_product 自动拆出，不维护第二份名单。
function readOperatorCatalog() {
    const path = resolve(dataDir, "operators.json");
    const {operators} = readJsonc(path);
    if (
        !operators ||
        typeof operators !== "object" ||
        Array.isArray(operators) ||
        Object.keys(operators).length === 0
    ) {
        throw new Error(`[operators] ${path} 缺少干员目录，请先运行 pnpm fetch:zmdmap --only sell_product`);
    }
    for (const [
        id,
        operator,
    ] of Object.entries(operators)) {
        if (operator?.id !== id) throw new Error(`[operators] 干员 ID 不一致：${id}`);
        for (const locale of OPERATOR_LOCALES) {
            if (typeof operator.names?.[locale] !== "string" || !operator.names[locale].trim()) {
                throw new Error(`[operators] ${id} 缺少 ${locale} 名称`);
            }
        }
    }
    return operators;
}

export const operatorCatalog = readOperatorCatalog();
