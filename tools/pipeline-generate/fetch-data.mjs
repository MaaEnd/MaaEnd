#!/usr/bin/env node
// 从 zmdmap API 获取最新版本，下载任务所需的精简游戏数据到 tools/pipeline-generate/data/ 目录。
// 若本地已是最新版本则跳过下载。
//
// 用法：node tools/pipeline-generate/fetch-data.mjs [--force] [--cache-bust]
//   --force  忽略本地版本缓存，强制重新下载
//   --cache-bust  等价于 --force，并在下载数据文件时追加时间戳参数，绕过远端缓存

import {mkdirSync, readFileSync, rmSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";
import {dataDir} from "./utils/paths.mjs";

const VERSION_API = "https://api.zmdmap.com/api/v1/endfield/version";
const DATA_BASE_URL = "https://assets.fz.wiki/output_maaend";

const DATA_FILES = [
    "delivery_jobs.json",
    "delivery_destinations.json",
    "environment_monitoring.json",
    "sell_product.json",
    "teleport_anchors.json",
    "collect_points.json",
];

const VERSION_FILE = "version.txt";
const isGitHubActions = process.env.GITHUB_ACTIONS === "true";
const shouldCacheBust = process.argv.includes("--cache-bust") || isGitHubActions;
const force = process.argv.includes("--force") || shouldCacheBust;
const cacheBustToken = shouldCacheBust ? Date.now().toString() : null;

function readCachedVersion() {
    try {
        return readFileSync(resolve(dataDir, VERSION_FILE), "utf8").trim();
    } catch {
        return null;
    }
}

function isDataCached() {
    if (!readCachedVersion()) {
        return false;
    }
    return DATA_FILES.every((file) => {
        try {
            JSON.parse(readFileSync(resolve(dataDir, file), "utf8"));
            return true;
        } catch {
            return false;
        }
    });
}

class CacheUpdateError extends Error {}

function writeCache(downloads, version) {
    let entries;
    try {
        mkdirSync(dataDir, {recursive: true});
        entries = [
            ...downloads,
            [
                VERSION_FILE,
                version,
            ],
        ].map(
            ([
                file,
                content,
            ]) => {
                const path = resolve(dataDir, file);
                let previous;
                try {
                    previous = readFileSync(path);
                } catch (error) {
                    if (error.code !== "ENOENT") {
                        throw error;
                    }
                }
                return {path, content, previous};
            },
        );
    } catch (error) {
        throw new CacheUpdateError(`缓存更新准备失败: ${error.message}`);
    }

    const touched = [];
    try {
        for (const entry of entries) {
            // 写入可能在截断文件后失败，必须先登记才能恢复该文件。
            touched.push(entry);
            writeFileSync(entry.path, entry.content, "utf8");
        }
    } catch (error) {
        const restoreErrors = [];
        for (const entry of touched.reverse()) {
            try {
                if (entry.previous === undefined) {
                    rmSync(entry.path, {force: true});
                } else {
                    writeFileSync(entry.path, entry.previous);
                }
            } catch (restoreError) {
                restoreErrors.push(`${entry.path}: ${restoreError.message}`);
            }
        }
        const restoration = restoreErrors.length ? `缓存恢复失败: ${restoreErrors.join("; ")}` : "已恢复更新前的缓存";
        throw new CacheUpdateError(`缓存写入失败: ${error.message}；${restoration}`);
    }
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
    const downloads = [];

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
        downloads.push([
            file,
            formattedData,
        ]);
    }

    // 六份数据全部下载并解析后才更新缓存，版本文件最后写入。
    // 回滚处理普通写入失败，不保证进程中断或并发更新时的整体原子性。
    writeCache(downloads, version);
    for (const [
        file,
        content,
    ] of downloads) {
        console.log(`[fetch-data] 已缓存 ${file} (${(Buffer.byteLength(content) / 1024).toFixed(0)} KB)`);
    }
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

main().catch((error) => {
    console.error(`[fetch-data] ${error.message}`);
    if (error instanceof CacheUpdateError) {
        process.exit(1);
    }
    if (!isDataCached()) {
        console.error("[fetch-data] 无可用本地缓存，请检查网络连接后重试。");
        process.exit(1);
    }
    console.warn("[fetch-data] 下载失败，将使用已有本地缓存继续。");
});
