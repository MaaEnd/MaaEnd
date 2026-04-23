/**
 * 检测 kite_station.json 中尚未在 routes.mjs ROUTE_CONFIG 中配置的观察点。
 * 运行方式（在 tools/pipeline-generate/EnvironmentMonitoring/ 目录下）：
 *   node .claude/skills/em-add-route/check_missing.mjs
 * 或直接复制到该目录后运行：
 *   node check_missing.mjs
 */
import {readFileSync} from "fs";
import {fileURLToPath} from "url";
import {resolve, dirname} from "path";

const __dir = dirname(fileURLToPath(import.meta.url));
const emDir = resolve(__dir, "../../../tools/pipeline-generate/EnvironmentMonitoring");

const kite = JSON.parse(readFileSync(resolve(emDir, "kite_station.json"), "utf8"));
const routes = readFileSync(resolve(emDir, "routes.mjs"), "utf8");

const existingNames = Array.from(routes.matchAll(/Name:\s*"([^"]+)"/g)).map((m) =>
    m[1].replace(/[^\p{L}\p{N}]/gu, "").toLowerCase(),
);
const normalize = (s) => s.replace(/[^\p{L}\p{N}]/gu, "").toLowerCase();

const missing = [];
for (const [
    ,
    station,
] of Object.entries(kite)) {
    const tasks = station.entrustTasks?.list ?? {};
    for (const [
        ,
        task,
    ] of Object.entries(tasks)) {
        const zhName = task.name?.["zh-CN"] ?? "";
        const enName = task.name?.["en-US"] ?? "";
        if (zhName && !existingNames.includes(normalize(zhName))) missing.push({zhName, enName});
    }
}

if (missing.length === 0) {
    console.log("✓ 所有观察点均已配置，无缺失条目。");
} else {
    console.log(`发现 ${missing.length} 个缺失条目：`);
    console.log(JSON.stringify(missing, null, 2));
}
