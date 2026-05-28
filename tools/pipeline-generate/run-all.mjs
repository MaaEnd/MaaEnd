#!/usr/bin/env node
// 扫描指定子目录下的所有 *config.json，依次调用 maa-pipeline-generate --config <file>。
// 用法：node tools/pipeline-generate/run-all.mjs <subdir>
// 例： node tools/pipeline-generate/run-all.mjs SellProduct

import {spawnSync} from "node:child_process";
import {existsSync, readdirSync, readFileSync, rmSync, statSync} from "node:fs";
import {basename, dirname, isAbsolute, relative, resolve} from "node:path";
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

function collectConfigs(dir) {
    const configs = [];

    for (const entry of readdirSync(dir, {withFileTypes: true})) {
        const path = resolve(dir, entry.name);
        if (entry.isDirectory()) {
            configs.push(...collectConfigs(path));
        } else if (/config\.json$/i.test(entry.name)) {
            configs.push(path);
        }
    }

    return configs.sort((a, b) => relative(targetDir, a).localeCompare(relative(targetDir, b)));
}

const configs = collectConfigs(targetDir);

if (configs.length === 0) {
    console.error(`[run-all] no *config.json found in ${targetDir}`);
    process.exit(1);
}

// 显式定位包内 bin 入口，避免裸 `node tools/...` 调用时（无 pnpm/npm 注入 PATH）找不到命令。
const packagePath = resolve(repoRoot, "node_modules", "@joebao", "maa-pipeline-generate", "package.json");
if (!existsSync(packagePath)) {
    console.error(`[run-all] maa-pipeline-generate package not found at ${packagePath}; run pnpm install first`);
    process.exit(1);
}
const packageRoot = dirname(packagePath);
const packageJson = JSON.parse(readFileSync(packagePath, "utf8"));
const binEntry = typeof packageJson.bin === "string" ? packageJson.bin : packageJson.bin?.["maa-pipeline-generate"];
if (!binEntry) {
    console.error(`[run-all] maa-pipeline-generate bin entry is missing in ${packagePath}`);
    process.exit(1);
}
const bin = resolve(packageRoot, binEntry);
if (!existsSync(bin)) {
    console.error(`[run-all] maa-pipeline-generate bin not found at ${bin}; run pnpm install first`);
    process.exit(1);
}

function isInside(parent, child) {
    const rel = relative(resolve(parent), resolve(child));
    return rel === "" || (!rel.startsWith("..") && !isAbsolute(rel));
}

for (const config of configs) {
    const configDir = dirname(config);
    const configName = basename(config);
    const configLabel = `${subdir}/${relative(targetDir, config).replaceAll("\\", "/")}`;

    console.log(`\n[run-all] ${configLabel}`);
    // task / merged 模式默认都是「读旧文件 + 合并」，老 key 不会自动清理。
    // 生成前先把目标文件删掉，确保产物只反映当前数据源。
    // 仅删 outputFile 指向的单文件，避免把整个 outputDir 里其他人维护的文件误清。
    try {
        const cfg = JSON.parse(readFileSync(config, "utf8"));
        if ((cfg.task || cfg.merged) && cfg.outputFile) {
            const outFile = resolve(configDir, cfg.outputDir || ".", cfg.outputFile);
            // 防御性校验：outputFile 误写绝对路径或过多 ".." 时，可能解析到仓库外。
            if (!isInside(repoRoot, outFile)) {
                console.error(`[run-all] outputFile escapes repo root: ${outFile} (config: ${configLabel})`);
                process.exit(1);
            }
            if (existsSync(outFile)) {
                rmSync(outFile);
                console.log(`[run-all] removed stale ${outFile}`);
            }
        }
    } catch (err) {
        console.error(`[run-all] failed to inspect ${configLabel}: ${err.message}`);
        process.exit(1);
    }

    const result = spawnSync(process.execPath, [bin, "--config", configName], {
        cwd: configDir,
        stdio: "inherit",
    });
    if (result.status !== 0) {
        console.error(`[run-all] failed on ${configLabel} (exit ${result.status})`);
        process.exit(result.status ?? 1);
    }
}
