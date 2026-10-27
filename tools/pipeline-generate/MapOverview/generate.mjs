import {readFileSync, readdirSync, writeFileSync} from "node:fs";
import {relative, resolve} from "node:path";
import {fileURLToPath} from "node:url";

import {applyEdits, createScanner, modify, parseTree, SyntaxKind} from "jsonc-parser";
import {format, resolveConfig} from "prettier";

import {parseJsonc, readJsonc} from "../jsonc.mjs";
import {dataDir, repoRoot} from "../utils/paths.mjs";

const region3dPath = resolve(dataDir, "region3d.json");
const outputPath = resolve(repoRoot, "assets/resource/pipeline/SceneManager/SceneMapOverview.json");
const nodePrefix = "__ScenePrivateMapOverview";
const placeholderHint = "待实现地图成功确认，实现后启用";

export function nodeName(region, map) {
    return `${nodePrefix}_${region}_EnterMap_${map}`;
}

export function successName(region, map) {
    return `__ScenePrivateMapAnyEnterMap_${region}_${map}_Success`;
}

function validateClicks(clicks) {
    if (!clicks || typeof clicks !== "object" || Array.isArray(clicks)) {
        throw new Error("[MapOverview] 点击数据为空或不是地区字典");
    }
    // 顶层发布元数据不参与地区校验，也不写入点击缓存。
    clicks = Object.fromEntries(Object.entries(clicks).filter(([key]) => key !== "version" && key !== "sha256"));
    if (Object.keys(clicks).length === 0) throw new Error("[MapOverview] 点击数据为空或不是地区字典");
    for (const [
        region,
        data,
    ] of Object.entries(clicks)) {
        if (!/^region\d+$/.test(region) || !data || typeof data !== "object" || Array.isArray(data)) {
            throw new Error(`[MapOverview] 非法地区: ${region}`);
        }
        if (typeof data.name !== "string" || !data.name.trim()) {
            throw new Error(`[MapOverview] ${region} 缺少地区名称`);
        }
        const maps = data.area;
        if (!maps || typeof maps !== "object" || Array.isArray(maps)) {
            throw new Error(`[MapOverview] ${region} 缺少 area 地图字典`);
        }
        if (Object.keys(maps).length === 0) throw new Error(`[MapOverview] ${region} 没有地图`);
        for (const [
            map,
            entry,
        ] of Object.entries(maps)) {
            const point = entry?.click;
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
    source = validateClicks(source);
    const clicks = {};
    for (const [
        region,
        data,
    ] of Object.entries(source)) {
        clicks[region] = {name: data.name, area: {}};
        for (const [
            map,
            entry,
        ] of Object.entries(data.area)) {
            // 只缓存地区名称和点击点，不保留 polygon。
            clicks[region].area[map] = {click: entry.click};
        }
    }
    return clicks;
}

export function buildPipeline(clicks, levels) {
    clicks = validateClicks(clicks);
    const nodes = {};
    const successes = {};
    for (const [
        region,
        data,
    ] of Object.entries(clicks).sort()) {
        for (const [
            map,
            entry,
        ] of Object.entries(data.area).sort()) {
            const regionName = data.name.trim();
            // 地图名复用现有同步名称表；地区名取远端并随缓存更新。
            const mapName = levels[map]?.names?.zh_cn || map;
            nodes[nodeName(region, map)] = {
                desc: `在${regionName}总览界面，点击进入${mapName}地图`,
                recognition: "DirectHit",
                action: {type: "Click", param: {target: entry.click}},
                next: [successName(region, map)],
            };
            successes[successName(region, map)] = {
                desc: `在地图界面，成功进入${regionName}-${mapName}地图；${placeholderHint}`,
                enabled: false,
            };
        }
    }
    return {clicks: nodes, successes};
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

export function collectReferences(nodes, path) {
    const references = [];
    for (const [
        name,
        node,
    ] of Object.entries(nodes)) {
        for (const field of [
            "next",
            "on_error",
        ]) {
            const targets = node[field];
            for (const [
                index,
                target,
            ] of (Array.isArray(targets) ? targets : [targets]).entries()) {
                if (typeof target !== "string" || target.includes("[Anchor]")) continue;
                // Anchor 解析的是运行时锚点；JumpBack 则仍直接引用静态节点。
                references.push({
                    target: target.replace(/^(?:\[JumpBack\])+/, ""),
                    source: `${path}: ${name}.${field}[${index}]`,
                });
            }
        }
    }
    return references;
}

export function updatePipeline(text, {clicks, successes}, externalNames, externalReferences = []) {
    const existing = parseJsonc(text, outputPath);
    const removed = new Set(
        Object.keys(existing).filter((name) => name.startsWith(`${nodePrefix}_`) && !Object.hasOwn(clicks, name)),
    );
    // 生成点击节点会整体替换，只检查其他文件和本文件中仍保留的手写流程。
    const manual = Object.fromEntries(Object.entries(existing).filter(([name]) => !name.startsWith(`${nodePrefix}_`)));
    const references = [
        ...externalReferences,
        ...collectReferences(manual, relative(repoRoot, outputPath)),
    ];
    const blocked = references.filter(({target}) => removed.has(target));
    if (blocked.length > 0) {
        throw new Error(
            `[MapOverview] 拒绝删除仍被流程引用的点击节点，请先确认数据并调整引用：\n${blocked
                .map(({target, source}) => `${target} ← ${source}`)
                .join("\n")}`,
        );
    }
    for (const name of removed) text = removeClick(text, name);
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
    for (const [
        next,
        node,
    ] of Object.entries(successes)) {
        if (externalNames.has(next) || Object.hasOwn(existing, next)) continue;
        text = applyEdits(text, modify(text, [next], node, options));
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

function collectExternalNodes() {
    const names = new Set();
    const references = [];
    const pipelineDir = resolve(repoRoot, "assets/resource/pipeline");
    for (const entry of readdirSync(pipelineDir, {recursive: true, withFileTypes: true})) {
        if (!entry.isFile() || !/\.jsonc?$/.test(entry.name)) continue;
        const path = resolve(entry.parentPath, entry.name);
        if (path === outputPath) continue;
        const nodes = readJsonc(path);
        for (const name of Object.keys(nodes)) names.add(name);
        references.push(...collectReferences(nodes, relative(repoRoot, path)));
    }
    return {names, references};
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

async function main() {
    const args = process.argv.slice(2);
    if (args.length > 0) {
        throw new Error("用法: node tools/pipeline-generate/MapOverview/generate.mjs");
    }
    // 下载、版本和指纹校验由统一的 zmdmap fetch 命令负责，这里只读取入库后的地区总览数据。
    const clicks = extractClicks(readJsonc(region3dPath));
    const {levels} = readJsonc(resolve(dataDir, "teleport_anchors.json"));
    const nodes = buildPipeline(clicks, levels);
    const external = collectExternalNodes();
    const output = updatePipeline(readOutput(), nodes, external.names, external.references);
    await writeText(outputPath, output);
    console.log(`[MapOverview] 已生成 ${Object.keys(nodes.clicks).length} 个点击节点：${outputPath}`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
    await main();
}
