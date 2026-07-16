import {readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath, pathToFileURL} from "node:url";

import {sellProductLocaleEntries} from "./data.mjs";

// 根据 zmdmap 的 settlement_trade.json 自动维护 SellProduct 使用的国际化键。
//
// 这个脚本只负责补齐「数据源已存在、locale 尚未登记」的据点和干员：
// 1. 稳定 key 由 data.mjs 统一生成，避免同步脚本和 Pipeline 生成器采用不同命名规则；
// 2. 已有 locale 文案始终保留，避免覆盖维护者手工润色或修正过的译文；
// 3. 新键插入对应的业务分组，不追加到整个 JSON 的末尾；
// 4. 写入前检查五语言完整性，重复执行时不产生新的文件变更。

// settlement_trade.json 使用 CN/TC/EN/JP/KR，项目 locale 文件使用小写地区名。
// 显式列出映射，既限制本脚本维护的语言范围，也避免依赖对象字段的偶然命名。
const INTERFACE_LOCALES = {
    CN: "zh_cn",
    TC: "zh_tw",
    EN: "en_us",
    JP: "ja_jp",
    KR: "ko_kr",
};

const __dirname = dirname(fileURLToPath(import.meta.url));
const LOCALE_DIR = resolve(__dirname, "../../../assets/locales/interface");

/**
 * 把缺失的国际化条目插入指定锚点之前。
 *
 * messages 保持原 JSON 的插入顺序；entries 来自 data.mjs，包含稳定 key 和数据源五语言名称。
 * 只重建内存对象，不在此函数中写文件，方便据点与干员两组数据串联处理。
 */
function insertMissingMessages(messages, entries, sourceLocale, insertBeforeKey) {
    // Object.hasOwn 可正确处理值为空字符串的已有键；只要 key 存在，就视为人工维护内容并保留。
    const missingEntries = entries.filter(({key}) => !Object.hasOwn(messages, key));
    if (missingEntries.length === 0) {
        return {
            messages,
            inserted: 0,
        };
    }

    const syncedMessages = {};
    let inserted = false;
    for (const [
        key,
        value,
    ] of Object.entries(messages)) {
        if (key === insertBeforeKey) {
            for (const entry of missingEntries) {
                // 优先使用当前 locale 的官方名称；极端情况下该语言缺值时依次回退简中、英文，
                // 最后才显示 key，确保生成的 JSON 始终有合法字符串且缺失来源容易定位。
                syncedMessages[entry.key] = entry.names[sourceLocale] || entry.names.CN || entry.names.EN || entry.key;
            }
            inserted = true;
        }
        syncedMessages[key] = value;
    }

    // 锚点缺失通常意味着 locale 结构被重命名。此时宁可中止，也不要静默追加到文件末尾。
    if (!inserted) {
        throw new Error(`[SellProduct] 未找到国际化插入锚点 ${insertBeforeKey}`);
    }

    return {
        messages: syncedMessages,
        inserted: missingEntries.length,
    };
}

// 写入前验证所有当前据点和可选干员均有 key。
// 此检查同时覆盖「插入算法遗漏」和「未来新增数据结构未接入同步器」两类回归。
function validateLocaleCatalog(messages, fileLocale) {
    const missingKeys = [
        ...sellProductLocaleEntries.settlements,
        ...sellProductLocaleEntries.operators,
    ]
        .map(({key}) => key)
        .filter((key) => !Object.hasOwn(messages, key));
    if (missingKeys.length > 0) {
        throw new Error(`[SellProduct] ${fileLocale}.json 仍缺少国际化键：${missingKeys.join(", ")}`);
    }
}

export function syncSellProductLocaleCatalogs() {
    // 每种语言独立读取和写回，避免某一语言的人工文案被错误复制到其他语言。
    for (const [
        sourceLocale,
        fileLocale,
    ] of Object.entries(INTERFACE_LOCALES)) {
        const localePath = resolve(LOCALE_DIR, `${fileLocale}.json`);
        const originalText = readFileSync(localePath, "utf8");
        const originalMessages = JSON.parse(originalText);

        // 据点开关位于 SellProduct 固定设置之后、SellAttempt1 之前，因此以 SellAttempt1 为插入锚点。
        const stationResult = insertMissingMessages(
            originalMessages,
            sellProductLocaleEntries.settlements,
            sourceLocale,
            "task.SellProduct.SellAttempt1",
        );

        // Endministrator 是干员列表的固定末项；新干员插在它之前，保持选项和 locale 的既有分组顺序。
        const operatorResult = insertMissingMessages(
            stationResult.messages,
            sellProductLocaleEntries.operators,
            sourceLocale,
            "operator.Endministrator",
        );
        validateLocaleCatalog(operatorResult.messages, fileLocale);

        // 与项目 JSON 约定保持一致：4 空格缩进、文件末尾一个换行。
        // 比较时统一原文件的 CRLF，保证 Windows 下无内容变化时也不会反复重写。
        const syncedText = `${JSON.stringify(operatorResult.messages, null, 4)}\n`;
        if (syncedText !== originalText.replace(/\r\n/g, "\n")) {
            writeFileSync(localePath, syncedText, "utf8");
            console.log(
                `[SellProduct] 已为 ${fileLocale}.json 补齐 ${stationResult.inserted} 个据点键和 ${operatorResult.inserted} 个干员键。`,
            );
        }
    }
}

// 既支持 package.json 直接执行，也允许测试或其他脚本 import 后显式调用而不触发写文件。
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
    syncSellProductLocaleCatalogs();
}
