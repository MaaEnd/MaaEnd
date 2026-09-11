import {readFileSync, writeFileSync} from "node:fs";

import {applyEdits, modify} from "jsonc-parser";

import {parseJsonc} from "../jsonc.mjs";
import {candidatesRows, endpointEntries} from "./endpoint-filter-data.mjs";

const LOCALE_TEXTS = {
    zh_cn: {points: (area) => `${area}送货点`, matched: (name) => `命中送货终点：${name}`},
    zh_tw: {points: (area) => `${area}送貨點`, matched: (name) => `命中送貨終點：${name}`},
    en_us: {points: (area) => `${area} delivery points`, matched: (name) => `Delivery destination matched: ${name}`},
    ja_jp: {points: (area) => `${area}の配達先`, matched: (name) => `配達先を検出：${name}`},
    ko_kr: {points: (area) => `${area} 배송지`, matched: (name) => `배송 목적지 확인: ${name}`},
};

// 将自动生成的文案移回已有同类键旁；送货点按区域归组，保留各组首次出现的顺序。
function groupGeneratedLocaleKeys(content) {
    for (const prefix of [
        "task.SeizeDeliveryJobsCommissionSource.",
        "task.SeizeDeliveryJobs.focus.endpoint",
        "task.SeizeDeliveryJobsDeliveryPoint",
    ]) {
        const messages = parseJsonc(content);
        const keys = Object.keys(messages);
        const groups = new Map();
        for (const key of keys.filter((key) => key.startsWith(prefix))) {
            const group = key.split(".")[1];
            if (!groups.has(group)) groups.set(group, []);
            groups.get(group).push(key);
        }
        const orderedKeys = [...groups.values()].flat();
        if (orderedKeys.length === 0) continue;
        const firstIndex = keys.indexOf(orderedKeys[0]);
        if (orderedKeys.every((key, index) => keys[firstIndex + index] === key)) continue;

        const formattingOptions = {insertSpaces: true, tabSize: 4, eol: "\n"};
        for (const key of orderedKeys) {
            content = applyEdits(content, modify(content, [key], undefined, {formattingOptions}));
        }
        let insertionIndex = firstIndex;
        for (const key of orderedKeys) {
            content = applyEdits(
                content,
                modify(content, [key], messages[key], {
                    formattingOptions,
                    getInsertionIndex: () => insertionIndex,
                }),
            );
            insertionIndex += 1;
        }
    }
    return content;
}

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
        for (const {AreaId} of candidatesRows) {
            const area = endpointEntries.find((entry) => entry.AreaId === AreaId).AreaTexts[locale];
            entries[`task.SeizeDeliveryJobsCommissionSource.cases.${AreaId}.label`] = area;
            entries[`task.SeizeDeliveryJobsDeliveryPoint${AreaId}.label`] = texts.points(area);
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
        content = groupGeneratedLocaleKeys(content);
        if (content !== original) {
            writeFileSync(path, content, "utf8");
            console.log(`[SeizeDeliveryJobs] 已同步 ${locale} 区域及终点文案`);
        }
    }
}
