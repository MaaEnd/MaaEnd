#!/usr/bin/env node
// 从 zmdmap API 获取最新版本，把任务所需的精简游戏数据下载到
// tools/pipeline-generate/data/raw/（原始形态，只带世界坐标，不入库），再按 BaseNav 的
// zone 表投影/材料化到入库的 data/*.json —— 各任务生成器只读这一份。
// 若本地已是最新版本则跳过下载，但仍然会跑一次投影。
//
// 用法：node tools/pipeline-generate/fetch-data.mjs [--force] [--cache-bust] [--project-only] [--nav <路径|URL>] [--only <数据集>...]
//   --force        忽略本地版本缓存，强制重新下载
//   --cache-bust   等价于 --force，并在下载数据文件时追加时间戳参数，绕过远端缓存
//   --project-only 不下载，只用现有的 data/raw/ 重新投影（本地重算后常用）
//   --nav          nav 数据的本地路径或 URL（缺省读 assets/resource/model 子模块的 navmesh）
//   --only         只投影列出的数据集（如 collect_points），用于本地迭代

import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";
import {DATA_FILES, projectData} from "./project.mjs";
import {dataDir, repoRoot} from "./utils/paths.mjs";

const VERSION_API = "https://api.zmdmap.com/api/v1/endfield/version";
const DATA_BASE_URL = "https://assets.fz.wiki/output_maaend";
const RAW_DIR = resolve(dataDir, "raw");
const VERSION_FILE = "version.txt";

const isGitHubActions = process.env.GITHUB_ACTIONS === "true";
const shouldCacheBust = process.argv.includes("--cache-bust") || isGitHubActions;
const force = process.argv.includes("--force") || shouldCacheBust;
const projectOnly = process.argv.includes("--project-only");
const cacheBustToken = shouldCacheBust ? Date.now().toString() : null;

function readArgument(name) {
    const index = process.argv.indexOf(name);
    return index >= 0 ? process.argv[index + 1] : undefined;
}

function readArguments(name) {
    const values = [];
    for (let index = 0; index < process.argv.length; index += 1) {
        const value = process.argv[index];
        if (value === name) {
            for (let next = index + 1; next < process.argv.length && !process.argv[next].startsWith("--"); next += 1) {
                values.push(process.argv[next]);
            }
        } else if (value.startsWith(`${name}=`)) {
            values.push(value.slice(name.length + 1));
        }
    }
    return values;
}

const nav = readArgument("--nav");
const only = readArguments("--only");

function readCachedVersion() {
    try {
        return readFileSync(resolve(RAW_DIR, VERSION_FILE), "utf8").trim();
    } catch {
        return null;
    }
}

function isDataCached() {
    return DATA_FILES.every((file) => {
        try {
            readFileSync(resolve(RAW_DIR, file), "utf8");
            return true;
        } catch {
            return false;
        }
    });
}

async function fetchLatestVersion() {
    const res = await fetch(VERSION_API);
    if (!res.ok) {
        throw new Error(`版本查询失败: ${res.status} ${res.statusText}`);
    }
    const body = await res.json();
    const version = body?.data?.list?.[0]?.version;
    if (!version) {
        throw new Error("无法解析版本号，API 返回结构可能已变更");
    }
    return version;
}

async function fetchAndCache(version) {
    mkdirSync(RAW_DIR, {recursive: true});

    for (const file of DATA_FILES) {
        const url = new URL(`${DATA_BASE_URL}/${file}`);
        url.searchParams.set("ver", version);
        if (cacheBustToken) {
            url.searchParams.set("t", cacheBustToken);
        }
        console.log(`[fetch-data] 下载 ${url}`);
        const res = await fetch(url);
        if (!res.ok) {
            throw new Error(`下载 ${file} 失败: ${res.status} ${res.statusText}`);
        }
        const text = await res.text();
        // 保留超出 Number 安全范围的 ID 原始字面量，避免格式化时发生精度丢失。
        const data = JSON.parse(text, (_key, value, context) =>
            typeof value === "number" ? JSON.rawJSON(context.source) : value,
        );
        const formattedData = `${JSON.stringify(data, null, 4)}\n`;
        writeFileSync(resolve(RAW_DIR, file), formattedData, "utf8");
        console.log(`[fetch-data] 已缓存 ${file} (${(Buffer.byteLength(formattedData) / 1024).toFixed(0)} KB)`);
    }

    writeFileSync(resolve(RAW_DIR, VERSION_FILE), version, "utf8");
}

async function download() {
    const cachedVersion = readCachedVersion();
    const dataCached = isDataCached();
    let latestVersion;

    if (cachedVersion && dataCached && !force) {
        try {
            latestVersion = await fetchLatestVersion();
            if (latestVersion === cachedVersion) {
                console.log(`[fetch-data] 本地已是最新版本 (v${cachedVersion})，跳过下载`);
                return;
            }
            console.log(`[fetch-data] 发现新版本: v${cachedVersion} → v${latestVersion}`);
        } catch (error) {
            console.warn(`[fetch-data] 版本检查失败 (${error.message})，使用本地缓存 v${cachedVersion}`);
            return;
        }
    } else if (!dataCached) {
        console.log("[fetch-data] 本地缓存不完整，开始下载...");
    } else if (force) {
        console.log("[fetch-data] 强制模式，重新下载...");
    } else {
        console.log("[fetch-data] 本地无缓存，开始下载...");
    }

    const version = latestVersion ?? (await fetchLatestVersion());
    await fetchAndCache(version);
    console.log(`[fetch-data] 完成，当前版本: v${version}`);
}

async function main() {
    if (projectOnly) {
        console.log("[fetch-data] --project-only：跳过下载，只用 data/raw/ 重新投影");
    } else {
        try {
            await download();
        } catch (error) {
            if (!isDataCached()) {
                throw error;
            }
            console.warn(`[fetch-data] ${error.message}`);
            console.warn("[fetch-data] 下载失败，改用已有本地缓存继续。");
        }
    }

    await projectData({rawDir: RAW_DIR, outDir: dataDir, nav, repoRoot, only, force});
    console.log("[fetch-data] data/*.json 已按 BaseNav zone 表投影完成");
}

main().catch((error) => {
    console.error(`[fetch-data] ${error.message}`);
    process.exit(1);
});
