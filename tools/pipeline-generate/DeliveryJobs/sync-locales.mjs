import {readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath, pathToFileURL} from "node:url";

import {parseJsonc} from "../jsonc.mjs";
import {deliveryJobLocaleEntries} from "./model.mjs";

const INTERFACE_LOCALES = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];

const __dirname = dirname(fileURLToPath(import.meta.url));
const LOCALE_DIR = resolve(__dirname, "../../../assets/locales/interface");

function localizedName(entry, fileLocale) {
    return entry.names[fileLocale] || entry.names.zh_cn || entry.names.en_us || entry.key;
}

export function fillMissingLocaleEntries(messages, entries, fileLocale, keyPrefix) {
    const missingEntries = entries.filter(({key}) => !Object.hasOwn(messages, key));
    const emptyEntries = new Map(
        entries
            .filter(
                ({key}) =>
                    Object.hasOwn(messages, key) && (typeof messages[key] !== "string" || messages[key].trim() === ""),
            )
            .map((entry) => [
                entry.key,
                localizedName(entry, fileLocale),
            ]),
    );
    if (missingEntries.length === 0 && emptyEntries.size === 0) {
        return {
            messages,
            filled: 0,
        };
    }

    const groupKeys = Object.keys(messages).filter((key) => key.startsWith(keyPrefix));
    if (missingEntries.length > 0 && groupKeys.length === 0) {
        throw new Error(`[DeliveryJobs] 未找到 ${keyPrefix} 分组，无法确定国际化键插入位置`);
    }
    const lastGroupKey = groupKeys.at(-1);
    const syncedMessages = {};
    for (const [
        key,
        value,
    ] of Object.entries(messages)) {
        syncedMessages[key] = emptyEntries.get(key) ?? value;
        if (key === lastGroupKey) {
            for (const entry of missingEntries) {
                syncedMessages[entry.key] = localizedName(entry, fileLocale);
            }
        }
    }

    return {
        messages: syncedMessages,
        filled: missingEntries.length + emptyEntries.size,
    };
}

export function removeStaleLocaleEntries(messages, entries, keyPattern) {
    const expectedKeys = new Set(entries.map(({key}) => key));
    const staleKeys = new Set(Object.keys(messages).filter((key) => keyPattern.test(key) && !expectedKeys.has(key)));
    if (staleKeys.size === 0) {
        return {
            messages,
            removed: 0,
        };
    }

    return {
        messages: Object.fromEntries(Object.entries(messages).filter(([key]) => !staleKeys.has(key))),
        removed: staleKeys.size,
    };
}

function validateLocaleCatalog(messages, fileLocale) {
    const missingKeys = [
        ...deliveryJobLocaleEntries.regions,
        ...deliveryJobLocaleEntries.fillItemPriorities,
        ...deliveryJobLocaleEntries.items,
    ]
        .map(({key}) => key)
        .filter((key) => typeof messages[key] !== "string" || messages[key].trim() === "");
    if (missingKeys.length > 0) {
        throw new Error(`[DeliveryJobs] ${fileLocale}.json 仍缺少国际化键：${missingKeys.join(", ")}`);
    }
}

export function syncDeliveryJobsLocaleCatalogs() {
    for (const fileLocale of INTERFACE_LOCALES) {
        const localePath = resolve(LOCALE_DIR, `${fileLocale}.json`);
        const originalText = readFileSync(localePath, "utf8");
        const originalMessages = parseJsonc(originalText, localePath);
        const regionResult = fillMissingLocaleEntries(
            originalMessages,
            deliveryJobLocaleEntries.regions,
            fileLocale,
            "global.region.",
        );
        const priorityCleanupResult = removeStaleLocaleEntries(
            regionResult.messages,
            deliveryJobLocaleEntries.fillItemPriorities,
            /^task\.DeliveryJobs\.WhatToFill[A-Za-z0-9]+Priority\d+$/,
        );
        const priorityResult = fillMissingLocaleEntries(
            priorityCleanupResult.messages,
            deliveryJobLocaleEntries.fillItemPriorities,
            fileLocale,
            "task.DeliveryJobs.WhatToFill",
        );
        const itemResult = fillMissingLocaleEntries(
            priorityResult.messages,
            deliveryJobLocaleEntries.items,
            fileLocale,
            "iconRecognition.name.",
        );
        validateLocaleCatalog(itemResult.messages, fileLocale);

        const changed = regionResult.filled + priorityCleanupResult.removed + priorityResult.filled + itemResult.filled;
        if (changed > 0) {
            const syncedText = `${JSON.stringify(itemResult.messages, null, 4)}\n`;
            writeFileSync(localePath, syncedText, "utf8");
            console.log(
                `[DeliveryJobs] 已为 ${fileLocale}.json 补齐 ${regionResult.filled} 个地区/仓储节点键、补齐 ${priorityResult.filled} 个并清理 ${priorityCleanupResult.removed} 个装箱优先级键，以及补齐 ${itemResult.filled} 个物品键。`,
            );
        }
    }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
    syncDeliveryJobsLocaleCatalogs();
}
