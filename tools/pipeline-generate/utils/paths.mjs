import {resolve, dirname} from "node:path";
import {fileURLToPath} from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
export const repoRoot = resolve(__dirname, "..", "..", "..");
export const dataDir = resolve(__dirname, "..", "data");
