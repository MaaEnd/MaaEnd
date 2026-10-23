import {readFileSync, readdirSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";
import {fileURLToPath} from "node:url";

import {applyEdits, createScanner, modify, parseTree, SyntaxKind} from "jsonc-parser";
import {format, resolveConfig} from "prettier";

import {parseJsonc, readJsonc} from "../jsonc.mjs";
import {dataDir, repoRoot} from "../utils/paths.mjs";

const sourceUrl = "https://voice.fz.wiki/output_maaend/region3d_data.json";
const directory = fileURLToPath(new URL(".", import.meta.url));
const clicksPath = resolve(directory, "clicks.json");
const outputPath = resolve(repoRoot, "assets/resource/pipeline/SceneManager/SceneMapOverview.json");
const nodePrefix = "__SceneMapOverviewClick";

export function nodeName(region, map) {
    return `${nodePrefix}_${region}_${map}`;
}

export function successName(region, map) {
    return `__SceneMapOverviewSuccess_${region}_${map}`;
}

function validateClicks(clicks) {
    if (!clicks || typeof clicks !== "object" || Array.isArray(clicks) || Object.keys(clicks).length === 0) {
        throw new Error("[MapOverview] 点击数据为空或不是地区字典");
    }
    for (const [
        region,
        maps,
    ] of Object.entries(clicks)) {
        if (!/^region\d+$/.test(region) || !maps || typeof maps !== "object" || Array.isArray(maps)) {
            throw new Error(`[MapOverview] 非法地区: ${region}`);
        }
        if (Object.keys(maps).length === 0) throw new Error(`[MapOverview] ${region} 没有地图`);
        for (const [
            map,
            point,
        ] of Object.entries(maps)) {
            // 总览点击坐标采用 1280×720 基准；拒绝缺失、越界和非整数数据，避免写入错误点击。
            if (
                !/^[a-z0-9_]+$/.test(map) ||
                !Array.isArray(point) ||
                point.length !== 2 ||
                !point.every(Number.isInteger) ||
                point[0] < 0 ||
                point[0] >= 1280 ||
                point[1] < 0 ||
                point[1] >= 720
            ) {
                throw new Error(`[MapOverview] ${region}/${map} 的点击坐标非法: ${JSON.stringify(point)}`);
            }
        }
    }
    return clicks;
}

export function extractClicks(source) {
    if (JSON.stringify(source._meta?.screen_size) !== "[1280,720]") {
        throw new Error("[MapOverview] 数据源必须使用 1280×720 基准");
    }
    const clicks = {};
    for (const [
        region,
        maps,
    ] of Object.entries(source)) {
        if (!/^region\d+$/.test(region)) continue;
        clicks[region] = {};
        for (const [
            map,
            entry,
        ] of Object.entries(maps)) {
            if (entry.status !== "ok") throw new Error(`[MapOverview] ${region}/${map} 状态异常: ${entry.status}`);
            // 当前发布结构以 center_px 提供点击点；不保存 area/polygon 等几何数据。
            clicks[region][map] = entry.center_px;
        }
    }
    return validateClicks(clicks);
}

export function buildPipeline(clicks, levels, source = {}) {
    validateClicks(clicks);
    const nodes = {};
    for (const [
        region,
        maps,
    ] of Object.entries(clicks).sort()) {
        for (const [
            map,
            point,
        ] of Object.entries(maps).sort()) {
            // 在线名称与点击点来自同一份数据；离线复用同步名称表，不维护地区名称映射。
            const remoteName = source[region]?.[map]?.name;
            const mapName = (typeof remoteName === "string" && remoteName.trim()) || levels[map]?.names?.zh_cn || map;
            nodes[nodeName(region, map)] = {
                desc: `在地图总览对应区域中，点击进入${mapName}地图`,
                recognition: "DirectHit",
                action: {type: "Click", param: {target: point}},
                next: [successName(region, map)],
            };
        }
    }
    return nodes;
}

// jsonc-parser 的属性删除会连同相邻注释一起移除；这里只删生成节点和紧随的逗号。
function removeClick(text, name) {
    const property = parseTree(text).children.find((entry) => entry.children[0].value === name);
    const scanner = createScanner(text, true);
    scanner.setPosition(property.offset + property.length);
    const edits = [{offset: property.offset, length: property.length, content: ""}];
    if (scanner.scan() === SyntaxKind.CommaToken) {
        edits.push({offset: scanner.getTokenOffset(), length: scanner.getTokenLength(), content: ""});
    }
    // 删除末项可能留下尾逗号，JSONC 解析和最终格式化均支持它。
    return applyEdits(text, edits);
}

export function updatePipeline(text, clicks, externalNames) {
    const existing = parseJsonc(text, outputPath);
    for (const name of Object.keys(existing)) {
        if (name.startsWith(`${nodePrefix}_`) && !Object.hasOwn(clicks, name)) text = removeClick(text, name);
    }
    const options = {formattingOptions: {insertSpaces: true, tabSize: 4}};
    for (const [
        name,
        node,
    ] of Object.entries(clicks)) {
        // 新点击节点插在手写节点之前，Success 始终留在文件尾部，内容和注释均不覆盖。
        text = applyEdits(
            text,
            modify(text, [name], node, {
                ...options,
                getInsertionIndex: (names) => {
                    const index = names.findIndex((entry) => !entry.startsWith(`${nodePrefix}_`));
                    return index === -1 ? names.length : index;
                },
            }),
        );
    }
    for (const node of Object.values(clicks)) {
        const [next] = node.next;
        if (externalNames.has(next) || Object.hasOwn(existing, next)) continue;
        text = applyEdits(
            text,
            modify(
                text,
                [next],
                {
                    desc: "待实现地图成功确认，实现后启用",
                    enabled: false,
                },
                options,
            ),
        );
    }
    return text;
}

function readOutput() {
    try {
        return readFileSync(outputPath, "utf8");
    } catch (error) {
        if (error.code !== "ENOENT") throw error;
        return "{}\n";
    }
}

function collectExternalNames() {
    const names = new Set();
    const pipelineDir = resolve(repoRoot, "assets/resource/pipeline");
    for (const entry of readdirSync(pipelineDir, {recursive: true, withFileTypes: true})) {
        if (!entry.isFile() || !/\.jsonc?$/.test(entry.name)) continue;
        const path = resolve(entry.parentPath, entry.name);
        if (path === outputPath) continue;
        for (const name of Object.keys(readJsonc(path))) names.add(name);
    }
    return names;
}

async function writeText(path, source) {
    const config = await resolveConfig(path);
    const text = await format(source, {...config, filepath: path});
    // 内容不变时不重写，便于重复生成和审查。
    let original;
    try {
        original = readFileSync(path, "utf8");
    } catch (error) {
        if (error.code !== "ENOENT") throw error;
    }
    if (text !== original) writeFileSync(path, text, "utf8");
}

async function writeJson(path, value) {
    await writeText(path, `${JSON.stringify(value, null, 4)}\n`);
}

async function main() {
    const args = process.argv.slice(2);
    if (args.some((arg) => arg !== "--no-download")) {
        throw new Error("用法: node tools/pipeline-generate/MapOverview/generate.mjs [--no-download]");
    }
    let clicks;
    let source;
    if (args.includes("--no-download")) {
        clicks = readJsonc(clicksPath);
    } else {
        const response = await fetch(sourceUrl, {signal: AbortSignal.timeout(60000)});
        if (!response.ok) throw new Error(`[MapOverview] 下载失败: HTTP ${response.status}`);
        source = await response.json();
        clicks = extractClicks(source);
    }
    const {levels} = readJsonc(resolve(dataDir, "teleport_anchors.json"));
    const nodes = buildPipeline(clicks, levels, source);
    const output = updatePipeline(readOutput(), nodes, collectExternalNames());
    await writeJson(clicksPath, clicks);
    await writeText(outputPath, output);
    console.log(`[MapOverview] 已生成 ${Object.keys(nodes).length} 个点击节点：${outputPath}`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
    await main();
}
