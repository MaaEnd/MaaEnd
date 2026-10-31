import {readFileSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";
import {pathToFileURL} from "node:url";

import {parseJsonc} from "../jsonc.mjs";
import {sortLocaleKeys} from "../utils/locales.mjs";
import {useConsumableLocaleMessages} from "./task-data.mjs";

export function fillMissingLocaleMessages(messages, entries, locale) {
    if (!messages || typeof messages !== "object" || Array.isArray(messages)) {
        throw new Error(`[UseConsumable] ${locale}.json 顶层必须是对象`);
    }
    const synced = {...messages};
    let filled = 0;
    for (const [
        key,
        value,
    ] of Object.entries(entries)) {
        if (typeof value !== "string" || value.trim() === "") {
            throw new Error(`[UseConsumable] ${locale} 缺少 ${key} 的多语言文本`);
        }
        // 物品名称与其他任务共享，只补缺失或空值，保留已有译文和其他物品的键。
        if (typeof messages[key] === "string" && messages[key].trim() !== "") continue;
        synced[key] = value;
        filled += 1;
    }
    return {messages: synced, filled};
}

export function syncUseConsumableLocaleCatalogs() {
    const pending = [];
    for (const [
        locale,
        entries,
    ] of Object.entries(useConsumableLocaleMessages)) {
        const path = new URL(`../../../assets/locales/interface/${locale}.json`, import.meta.url);
        const original = readFileSync(path, "utf8");
        const messages = parseJsonc(original, String(path));
        const result = fillMissingLocaleMessages(messages, entries, locale);
        if (result.filled === 0) continue;
        pending.push({
            path,
            locale,
            filled: result.filled,
            content: `${JSON.stringify(sortLocaleKeys(result.messages), null, 4)}\n`,
        });
    }
    // 五种语言均检查通过后再写入，避免缺少某种语言时只更新了部分文件。
    for (const {path, locale, filled, content} of pending) {
        writeFileSync(path, content, "utf8");
        console.log(`[UseConsumable] 已为 ${locale}.json 补齐 ${filled} 项文案`);
    }
    if (pending.length === 0) {
        console.log("[UseConsumable] 五种语言文案已齐全，无需修改");
    }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
    syncUseConsumableLocaleCatalogs();
}
