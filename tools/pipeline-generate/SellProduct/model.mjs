import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {dataDir} from "../utils/paths.mjs";

let loadedSettlementData;
try {
    loadedSettlementData = JSON.parse(readFileSync(resolve(dataDir, "settlement_trade.json"), "utf8"));
} catch {
    console.error("[SellProduct] 数据文件缺失，请先运行 pnpm fetch:zmdmap 或 pnpm generate:SellProduct");
    process.exit(1);
}

export const settlementData = loadedSettlementData;

// 转义文本中的正则元字符，用于把数据源名称安全地放进 OCR expected。
export function escapeRegex(str) {
    return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// 将数据源里的 id 或英文名称转换成稳定的选项/节点后缀。
export function toPascalCase(str) {
    return str
        .split(/[^a-zA-Z0-9]+/)
        .filter(Boolean)
        .map((part) => part[0].toUpperCase() + part.slice(1))
        .join("");
}

// 在保留原始顺序的前提下去掉空值和重复候选。
export function uniqueArray(items) {
    return [...new Set(items.filter(Boolean))];
}

export function isAdminOperator(operator) {
    return operator.name?.EN === "Endministrator" || operator.name?.CN === "管理员";
}

export function getOperatorCaseName(operator) {
    return toPascalCase(operator.name?.EN || operator.charId);
}

// 英文地名匹配允许空格和连字符有轻微 OCR 差异。
function toFlexibleEnglishRegex(text) {
    const escaped = escapeRegex(text.trim());
    return `(?i)^${escaped.replace(/\s+/g, "\\s*").replace(/-/g, "\\s*-\\s*")}$`;
}

// 仅保留有实际识别证据的 OCR 兼容值。官方多语言全文统一从 settlementName 提取，
// LocationId 也统一由英文名称派生，不再维护手写 ID 覆盖。
const SETTLEMENT_OCR_ALIASES = {
    // OCR 会将 Reconstruction HQ 末尾误识为 Hc。
    stm_tundra_3: ["Reconstruction Hc"],
    // 天王坪页签在不同语言/显示宽度下会出现截断或简称。
    stm_hongs_1: [
        "天王坪",
        "天王坪援助",
        "天王坪援建",
        "Sky King",
        "天王原",
    ],
};

// domainId → RegionPrefix 默认映射。新 domain 接入时若沿用「英文区域名」命名约定，加一行即可；
// 不在表中的 domain 会回退到 toPascalCase(domainId)。
const DOMAIN_REGION_PREFIX = {
    domain_1: "ValleyIV",
    domain_2: "Wuling",
};

// 生成据点入口 OCR 候选；有手动覆盖时优先使用覆盖值。
function buildSettlementTextExpected(settlementId, settlement) {
    return uniqueArray([
        settlement.settlementName.CN,
        settlement.settlementName.TC,
        settlement.settlementName.JP,
        settlement.settlementName.KR,
        settlement.settlementName.EN ? toFlexibleEnglishRegex(settlement.settlementName.EN) : null,
        ...(SETTLEMENT_OCR_ALIASES[settlementId] || []),
    ]);
}

// 所有模板共享的据点模型。排序先按 domainId，同 domain 内再按 settlementId。
export const sellProductLocations = Object.entries(settlementData.settlements)
    .sort(
        (
            [
                aId,
                aData,
            ],
            [
                bId,
                bData,
            ],
        ) => {
            const aDomain = aData.domainId || "";
            const bDomain = bData.domainId || "";
            if (aDomain !== bDomain) return aDomain.localeCompare(bDomain);
            return aId.localeCompare(bId);
        },
    )
    .map(
        ([
            SettlementId,
            settlement,
        ]) => {
            const RegionPrefix = DOMAIN_REGION_PREFIX[settlement.domainId] || toPascalCase(settlement.domainId);
            const LocationId = toPascalCase(settlement.settlementName.EN || SettlementId);

            return {
                SettlementId,
                RegionPrefix,
                LocationId,
                LocationDesc: settlement.settlementName.CN,
                TextExpected: buildSettlementTextExpected(SettlementId, settlement),
            };
        },
    );

// 国际化同步器消费的最小数据视图。命名规则与所有模板共享同一模型。
export const sellProductLocaleEntries = {
    operators: Object.values(settlementData.operators || {})
        .filter((operator) => !isAdminOperator(operator))
        .map((operator) => ({
            key: `operator.${getOperatorCaseName(operator)}`,
            names: operator.name || {},
        })),
    settlements: sellProductLocations.map((location) => ({
        key: `task.SellProduct.${location.RegionPrefix}${location.LocationId}`,
        names: settlementData.settlements[location.SettlementId].settlementName || {},
    })),
};
