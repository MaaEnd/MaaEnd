#!/usr/bin/env node
// 扫描指定子目录下的所有 *config.json，依次调用 maa-pipeline-generate --config <file>。
// 用法：node tools/pipeline-generate/run-all.mjs <subdir>
// 例： node tools/pipeline-generate/run-all.mjs SellProduct

import {spawnSync} from "node:child_process";
import {existsSync, readdirSync, readFileSync, rmSync, statSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
// 仓库根目录：tools/pipeline-generate/ → ../../
const repoRoot = resolve(__dirname, "..", "..");

const subdir = process.argv[2];
if (!subdir) {
    console.error("Usage: node tools/pipeline-generate/run-all.mjs <subdir>");
    process.exit(1);
}

const targetDir = resolve(__dirname, subdir);
try {
    if (!statSync(targetDir).isDirectory()) throw new Error("not a directory");
} catch {
    console.error(`[run-all] target is not a directory: ${targetDir}`);
    process.exit(1);
}

const configs = readdirSync(targetDir)
    .filter((f) => /config\.json$/i.test(f))
    .sort();

if (configs.length === 0) {
    console.error(`[run-all] no *config.json found in ${targetDir}`);
    process.exit(1);
}

// 显式定位本地 bin，避免裸 `node tools/...` 调用时（无 pnpm/npm 注入 PATH）找不到命令。
const binBase = resolve(repoRoot, "node_modules", ".bin", "maa-pipeline-generate");
const bin = process.platform === "win32" ? `${binBase}.CMD` : binBase;
if (!existsSync(bin)) {
    console.error(`[run-all] maa-pipeline-generate not found at ${bin}; run pnpm install first`);
    process.exit(1);
}

function isInside(parent, child) {
    const rel = resolve(child).slice(resolve(parent).length);
    return rel === "" || rel.startsWith("/") || rel.startsWith("\\");
}

for (const config of configs) {
    console.log(`\n[run-all] ${subdir}/${config}`);
    // task / merged 模式默认都是「读旧文件 + 合并」，老 key 不会自动清理。
    // 生成前先把目标文件删掉，确保产物只反映当前数据源。
    // 仅删 outputFile 指向的单文件，避免把整个 outputDir 里其他人维护的文件误清。
    try {
        const cfg = JSON.parse(readFileSync(resolve(targetDir, config), "utf8"));
        if ((cfg.task || cfg.merged) && cfg.outputFile) {
            const outFile = resolve(targetDir, cfg.outputDir || ".", cfg.outputFile);
            // 防御性校验：outputFile 误写绝对路径或过多 ".." 时，可能解析到仓库外。
            if (!isInside(repoRoot, outFile)) {
                console.error(`[run-all] outputFile escapes repo root: ${outFile} (config: ${config})`);
                process.exit(1);
            }
            if (existsSync(outFile)) {
                rmSync(outFile);
                console.log(`[run-all] removed stale ${outFile}`);
            }
        }
    } catch (err) {
        console.error(`[run-all] failed to inspect ${config}: ${err.message}`);
        process.exit(1);
    }

    // 在 Windows 下 .CMD 必须经由 shell 启动，因此仍需启用 shell；但通过 args 数组传参，
    // 避免把 config 直接拼到命令字符串里（防空格/特殊字符 + 可能的 shell 元字符注入）。
    const result = spawnSync(bin, ["--config", config], {
        cwd: targetDir,
        stdio: "inherit",
        shell: process.platform === "win32",
    });
    if (result.status !== 0) {
        console.error(`[run-all] failed on ${config} (exit ${result.status})`);
        process.exit(result.status ?? 1);
    }
}
