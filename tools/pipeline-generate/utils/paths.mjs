import {resolve, dirname} from "node:path";
import {fileURLToPath} from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
export const repoRoot = resolve(__dirname, "..", "..", "..");
// zmdmap 的数据树：入库的 data/（投影后的六份）在这里。
export const dataDir = resolve(__dirname, "..", "..", "zmdmap", "data");
