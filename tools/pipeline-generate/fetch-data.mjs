#!/usr/bin/env node
// 从 zmdmap API 获取最新版本，下载数据文件到 tools/pipeline-generate/data/ 目录。
// 若本地已是最新版本则跳过下载。
//
// 用法：node tools/pipeline-generate/fetch-data.mjs [--force]
//   --force  忽略本地版本缓存，强制重新下载

import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";
import {dataDir} from "./utils/paths.mjs";

const VERSION_API = "https://api.zmdmap.com/api/v1/endfield/version";
const DATA_BASE_URL = "https://assets.zmdmap.com/data/entity";

const DATA_FILES = [
    "settlement_trade.json",
    "kite_station_i18n.json",
];

const VERSION_FILE = "version.txt";
const force = process.argv.includes("--force");

function readCachedVersion() {
    try {
        return readFileSync(resolve(dataDir, VERSION_FILE), "utf8").trim();
    } catch {
        return null;
    }
}

function isDataCached() {
    return DATA_FILES.every((file) => {
        try {
            readFileSync(resolve(dataDir, file), "utf8");
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
    mkdirSync(dataDir, {recursive: true});

    for (const file of DATA_FILES) {
        const url = `${DATA_BASE_URL}/${version}/${file}`;
        console.log(`[fetch-data] 下载 ${url}`);
        const res = await fetch(url);
        if (!res.ok) {
            throw new Error(`下载 ${file} 失败: ${res.status} ${res.statusText}`);
        }
        const text = await res.text();
        writeFileSync(resolve(dataDir, file), text, "utf8");
        console.log(`[fetch-data] 已缓存 ${file} (${(Buffer.byteLength(text) / 1024).toFixed(0)} KB)`);
    }

    writeFileSync(resolve(dataDir, VERSION_FILE), version, "utf8");
}

async function main() {
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
        } catch (e) {
            console.warn(`[fetch-data] 版本检查失败 (${e.message})，使用本地缓存 v${cachedVersion}`);
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

main().catch((e) => {
    console.error(`[fetch-data] ${e.message}`);
    if (!isDataCached()) {
        console.error("[fetch-data] 无可用本地缓存，请检查网络连接后重试。");
        process.exit(1);
    }
    console.warn("[fetch-data] 下载失败，将使用已有本地缓存继续。");
});
