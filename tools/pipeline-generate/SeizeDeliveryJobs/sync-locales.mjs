import {readFileSync, writeFileSync} from "node:fs";

import {applyEdits, modify} from "jsonc-parser";

import {parseJsonc} from "../jsonc.mjs";
import {sortLocaleKeys} from "../utils/locales.mjs";
import {candidatesRows, commissionSourceOrder, endpointEntries} from "./endpoint-filter-data.mjs";

const LOCALE_TEXTS = {
    zh_cn: {points: (area) => `${area}送货点`, matched: (name) => `命中送货终点：${name}`},
    zh_tw: {points: (area) => `${area}送貨點`, matched: (name) => `命中送貨終點：${name}`},
    en_us: {points: (area) => `${area} delivery points`, matched: (name) => `Delivery destination matched: ${name}`},
    ja_jp: {points: (area) => `${area}の配達先`, matched: (name) => `配達先を検出：${name}`},
    ko_kr: {points: (area) => `${area} 배송지`, matched: (name) => `배송 목적지 확인: ${name}`},
};

// 自动生成的文案按字母序与既有键一起写回；键序由 prettier 的 maafw-sort（locale 文件按字母序）
// 决定，生成器不必再自己按业务分组挪位置。

export function syncSeizeDeliveryJobsLocales() {
    for (const [
        locale,
        texts,
    ] of Object.entries(LOCALE_TEXTS)) {
        const path = new URL(`../../../assets/locales/interface/${locale}.json`, import.meta.url);
        const original = readFileSync(path, "utf8");
        const messages = parseJsonc(original, String(path));
        const entries = {};
        const endpointKeys = new Set();
        // 只生成真实区域（仓储）的文案；「全部地区」与地区级 case 由人工维护，这里只固定它们的键序。
        for (const {CaseId} of commissionSourceOrder) {
            const row = candidatesRows.find(({AreaId}) => AreaId === CaseId);
            if (!row) continue;
            const area = row.AreaTexts[locale];
            entries[`task.SeizeDeliveryJobsCommissionSource.cases.${CaseId}.label`] = area;
            entries[`task.SeizeDeliveryJobsDeliveryPoint${CaseId}.label`] = texts.points(area);
        }
        for (const {AreaId, EndpointId, Names} of endpointEntries) {
            const labelKey = `task.SeizeDeliveryJobsDeliveryPoint${AreaId}.cases.${EndpointId}.label`;
            const focusKey = `task.SeizeDeliveryJobs.focus.endpoint${EndpointId}`;
            entries[labelKey] = Names[locale];
            entries[focusKey] = texts.matched(Names[locale]);
            endpointKeys.add(labelKey);
            endpointKeys.add(focusKey);
        }

        let content = original;
        for (const [
            key,
            value,
        ] of Object.entries(entries)) {
            if (typeof value !== "string" || value.trim() === "") {
                throw new Error(`[SeizeDeliveryJobs] ${locale} 缺少 ${key} 对应的 AutoDelivery 文案`);
            }
            // 终点名称每次同步，确保人工修改或清空后立即生效；区域文案仍只补缺失项。
            if (messages[key] === value) continue;
            if (!endpointKeys.has(key) && typeof messages[key] === "string" && messages[key].trim() !== "") continue;
            content = applyEdits(
                content,
                modify(content, [key], value, {
                    formattingOptions: {insertSpaces: true, tabSize: 4, eol: "\n"},
                }),
            );
        }
        // 键按字母序写回（与 prettier 的 maafw-sort 对 locale 文件的要求一致）。
        const synced = `${JSON.stringify(sortLocaleKeys(parseJsonc(content, String(path))), null, 4)}\n`;
        if (synced !== original) {
            writeFileSync(path, synced, "utf8");
            console.log(`[SeizeDeliveryJobs] 已同步 ${locale} 区域及终点文案`);
        }
    }
}
