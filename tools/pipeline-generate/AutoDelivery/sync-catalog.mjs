import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";

import {runtimeCatalog} from "./model.mjs";

const currentDir = dirname(fileURLToPath(import.meta.url));
const outputPath = resolve(currentDir, "../../../assets/data/AutoDelivery/catalog.json");
const content = `${JSON.stringify(runtimeCatalog, null, 4)}\n`;

mkdirSync(dirname(outputPath), {recursive: true});
let current = "";
try {
    current = readFileSync(outputPath, "utf8");
} catch {}

if (current === content) {
    console.log("[AutoDelivery] 运行时匹配目录未变化");
} else {
    writeFileSync(outputPath, content, "utf8");
    console.log(`[AutoDelivery] 已生成 ${outputPath}`);
}
