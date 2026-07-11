import {readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import rows, {kiteStationData, MONITORING_TERMINAL_IDS} from "./data.mjs";
import {buildStationId} from "./common.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const localeDir = resolve(__dirname, "../../../../assets/locales/interface");
const localeFiles = {
    "zh-CN": "zh_cn.json",
    "zh-TW": "zh_tw.json",
    "en-US": "en_us.json",
    "ja-JP": "ja_jp.json",
    "ko-KR": "ko_kr.json",
};
const keyPrefix = "task.EnvironmentMonitoring.option.";

function buildLabels(locale) {
    const labels = {};
    for (const terminalId of MONITORING_TERMINAL_IDS) {
        const station = buildStationId(kiteStationData, terminalId);
        labels[`${keyPrefix}${station}`] =
            kiteStationData[terminalId]?.level?.name?.[locale] ||
            kiteStationData[terminalId]?.level?.name?.["zh-CN"] ||
            station;
    }
    for (const row of rows) {
        labels[`${keyPrefix}${row.Id}`] = row.NameLocales[locale] || row.Name;
    }
    return labels;
}

for (const [
    locale,
    fileName,
] of Object.entries(localeFiles)) {
    const path = resolve(localeDir, fileName);
    const current = JSON.parse(readFileSync(path, "utf8"));
    const next = Object.fromEntries(Object.entries(current).filter(([key]) => !key.startsWith(keyPrefix)));
    Object.assign(next, buildLabels(locale));
    writeFileSync(path, `${JSON.stringify(next, null, 4)}\n`, "utf8");
}

console.log("[EnvironmentMonitoring] 已同步任务选项本地化文本。");
