// 把 data/raw/ 里的原始精简数据（只带世界坐标）投影成入库的 data/*.json。
//
// zone 参数（zone_id / size / sx / tx / sy / ty）取自 BaseNav 数据包，换算
// u = sx*x + tx, v = -sy*z + ty（原点左上、y 向下）。数据包缺省读
// assets/resource/model 子模块的 map/navmesh/base.nav.gz；本地没有时按该子模块当前
// 版本从远端取，也可以显式传 --nav。
//
// fetch-data.mjs 下载完发布数据后调用它；本地用 data/scripts/generate.py 重新生成
// data/raw/ 之后，也可以只跑投影（node tools/pipeline-generate/fetch-data.mjs --project-only）。

import {closeSync, existsSync, mkdirSync, openSync, readFileSync, readSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";
import {gunzipSync, constants as zlibConstants} from "node:zlib";

const VERSION_FILE = "version.txt";
const USER_AGENT = "MaaEnd-pipeline";

// 投影后的数据里 coord 的说明（原始数据的同名字段说明的是世界坐标，两者别混）。
export const PROJECTED_COORD_TEXT =
    "u/v = MapLocator 底图像素，原点左上、y 向下；BaseNav 顶点也是这个平面（u, v, height），" +
    "直接喂寻路即可。x/z 是投影前的世界坐标，maps 里的 zone / sx / tx / sy / ty 原样取自" +
    "BaseNav pack 的 zone 表，换算为 u = sx*x + tx, v = -sy*z + ty";
// 送货点数据的 coord 在通用说明后再补一句 y 的用途（与上游发布数据的文案呼应）。
const COORD_Y_NOTE = "y 是未换算的世界高度，直接作为 NAVMESH 终点的 target_deck_y 用来在重叠可走面中选中目标层";

export const DATA_FILES = [
    "delivery_jobs.json",
    "delivery_destinations.json",
    "environment_monitoring.json",
    "sell_product.json",
    "teleport_anchors.json",
    "collect_points.json",
];
// 带世界坐标的数据集 -> 条目数组所在的键；其余文件原样带到 data/。
export const COORDINATE_DATASETS = {
    "delivery_destinations.json": [
        "depots",
        "destinations",
    ],
    "teleport_anchors.json": ["anchors"],
    "collect_points.json": ["points"],
};

const NAVMESH_SUBPATH = [
    "assets",
    "resource",
    "model",
    "map",
    "navmesh",
];
const NAV_CANDIDATES = [
    "base.nav.gz",
    "base.nav",
];
const NAV_SUBMODULE_API = "https://api.github.com/repos/MaaEnd/MaaEnd/contents/assets/resource/model?ref=v2";
const NAV_REMOTE_TEMPLATE = "https://raw.githubusercontent.com/MaaEnd/MaaEnd-AI/{sha}/map/navmesh/base.nav.gz";
const NAV_PREFIX_BYTES = 64 * 1024;
const NAV_PREFIX_LIMIT = 8 * 1024 * 1024;
const NAV_COMPRESSED_START = 256 * 1024;
const NAV_MAGIC = Buffer.from("BNAV");
const NAV_HEADER_SIZE = 64;
const NAV_GEO_TAG = Buffer.from("BGEO");
const NAV_GEO_HEADER_SIZE = 72;

class NavPrefixTooShort extends Error {}

// ── nav 数据解析 ─────────────────────────────────────────────────────────

function findGeoZoneTable(raw, counts) {
    let at = raw.indexOf(NAV_GEO_TAG, NAV_HEADER_SIZE);
    while (at >= 0) {
        if (
            at + NAV_GEO_HEADER_SIZE <= raw.length &&
            raw.readUInt32LE(at + 8) === counts[0] &&
            raw.readUInt32LE(at + 12) === counts[1] &&
            raw.readUInt32LE(at + 16) === counts[2]
        ) {
            return at + Number(raw.readBigUInt64LE(at + 64));
        }
        at = raw.indexOf(NAV_GEO_TAG, at + 4);
    }
    throw new NavPrefixTooShort("nav 数据里找不到 BGEO zone 表");
}

export function parseNavZones(raw, source) {
    if (raw.length < NAV_HEADER_SIZE) {
        throw new NavPrefixTooShort("nav 数据头部没读全");
    }
    if (!raw.subarray(0, 4).equals(NAV_MAGIC)) {
        throw new Error(`nav 数据头不合法：${source}`);
    }
    const version = raw.readUInt16LE(4);
    const zoneCount = raw.readUInt32LE(8);
    const counts = [
        raw.readUInt32LE(12),
        raw.readUInt32LE(16),
        raw.readUInt32LE(20),
    ];
    const zoneSize = version >= 3 ? 48 : 44;
    let offset = version >= 5 ? findGeoZoneTable(raw, counts) : Number(raw.readBigUInt64LE(24));

    const zones = {};
    for (let index = 0; index < zoneCount; index += 1) {
        if (offset + zoneSize > raw.length) {
            throw new NavPrefixTooShort("nav 数据的 zone 表没读全");
        }
        const width = raw.readFloatLE(offset + 20);
        const height = raw.readFloatLE(offset + 24);
        const nameSize = raw.readUInt32LE(offset + 4);
        const zone = {
            zone_id: raw.readUInt16LE(offset),
            size: [
                Math.trunc(width),
                Math.trunc(height),
            ],
            sx: raw.readFloatLE(offset + 28),
            tx: raw.readFloatLE(offset + 32),
            sy: raw.readFloatLE(offset + 36),
            ty: raw.readFloatLE(offset + 40),
        };
        offset += zoneSize;
        if (offset + nameSize > raw.length) {
            throw new NavPrefixTooShort("nav 数据的 zone 名没读全");
        }
        const name = raw.subarray(offset, offset + nameSize).toString("utf8");
        offset += nameSize;
        if (name in zones) {
            throw new Error(`zone 名 ${name} 重复：${source}`);
        }
        zones[name] = zone;
    }
    if (Object.keys(zones).length === 0) {
        throw new Error(`没有解析到 zone：${source}`);
    }
    return zones;
}

function inflatePrefix(compressed) {
    if (compressed.length < 2 || compressed[0] !== 0x1f || compressed[1] !== 0x8b) {
        return compressed;
    }
    // Z_SYNC_FLUSH：拿到的是一段前缀，别因为还没读完就报错。
    return gunzipSync(compressed, {finishFlush: zlibConstants.Z_SYNC_FLUSH});
}

/** 反复多读一些压缩数据，直到解出来的长度够用。 */
async function readDecompressed(readCompressed, size) {
    let compressedSize = Math.max(size, NAV_COMPRESSED_START);
    for (let attempt = 0; attempt < 8; attempt += 1) {
        const data = inflatePrefix(await readCompressed(compressedSize));
        if (data.length >= size) {
            return data;
        }
        compressedSize *= 4;
    }
    throw new Error("没能读完 nav 数据的 zone 表");
}

function readFilePrefix(path, bytes) {
    const handle = openSync(path, "r");
    try {
        const buffer = Buffer.alloc(bytes);
        const read = readSync(handle, buffer, 0, bytes, 0);
        return buffer.subarray(0, read);
    } finally {
        closeSync(handle);
    }
}

async function readRemotePrefix(url, bytes) {
    const response = await fetch(url, {
        headers: {Range: `bytes=0-${bytes - 1}`, "User-Agent": USER_AGENT},
    });
    if (!response.ok) {
        throw new Error(`读取 nav 数据失败：${response.status} ${response.statusText}`);
    }
    return Buffer.from(await response.arrayBuffer());
}

async function resolveNavSubmoduleSha() {
    const response = await fetch(NAV_SUBMODULE_API, {
        headers: {"User-Agent": USER_AGENT, Accept: "application/vnd.github+json"},
    });
    if (!response.ok) {
        throw new Error(`查询 assets/resource/model 版本失败：${response.status}`);
    }
    const body = await response.json();
    if (typeof body?.sha !== "string" || !body.sha) {
        throw new Error("没能确定 assets/resource/model 的版本");
    }
    return body.sha;
}

async function resolveNavSource(nav, repoRoot) {
    if (nav) {
        if (/^https?:\/\//.test(nav)) {
            return {label: nav, read: (bytes) => readRemotePrefix(nav, bytes)};
        }
        const path = resolve(nav);
        if (!existsSync(path)) {
            throw new Error(`nav 数据不存在：${path}`);
        }
        return {label: path, read: async (bytes) => readFilePrefix(path, bytes)};
    }

    const navDir = resolve(repoRoot, ...NAVMESH_SUBPATH);
    for (const name of NAV_CANDIDATES) {
        const candidate = resolve(navDir, name);
        if (existsSync(candidate)) {
            return {label: candidate, read: async (bytes) => readFilePrefix(candidate, bytes)};
        }
    }
    const url = NAV_REMOTE_TEMPLATE.replace("{sha}", await resolveNavSubmoduleSha());
    return {label: url, read: (bytes) => readRemotePrefix(url, bytes)};
}

export async function loadNavZones(nav, repoRoot) {
    const {label, read} = await resolveNavSource(nav, repoRoot);
    console.log(`[project] nav 数据：${label}`);
    for (let size = NAV_PREFIX_BYTES; size <= NAV_PREFIX_LIMIT; size *= 2) {
        try {
            return parseNavZones(await readDecompressed(read, size), label);
        } catch (error) {
            if (error instanceof NavPrefixTooShort) {
                continue;
            }
            throw error;
        }
    }
    throw new Error(`没能读全 zone 表：${label}`);
}

// ── 投影 ────────────────────────────────────────────────────────────────

function resolveZone(zones, mapId) {
    for (const name of [
        mapId,
        `${mapId}base`,
    ]) {
        if (name in zones) {
            return {name, ...zones[name]};
        }
    }
    throw new Error(`没有 ${mapId} 对应的 zone`);
}

/** 与 Python 的 round(value, 3) 一致：按精确值取三位小数，逢半取偶。 */
function round3(value) {
    const [
        intPart,
        fractionPart = "",
    ] = Math.abs(value).toFixed(20).split(".");
    const kept = BigInt(`${intPart}${fractionPart.slice(0, 3).padEnd(3, "0")}`);
    const rest = fractionPart.slice(3);
    const firstRest = rest[0] ?? "0";
    const isHalf = firstRest === "5" && /^0*$/.test(rest.slice(1));
    const roundUp = firstRest > "5" || (firstRest === "5" && !isHalf) || (isHalf && kept % 2n === 1n);
    const result = Number(roundUp ? kept + 1n : kept) / 1000;
    return value < 0 ? -result : result;
}

function readAxes(entry, axes, label) {
    const values = [];
    for (const axis of axes) {
        const value = entry[axis];
        if (typeof value !== "number" || !Number.isFinite(value)) {
            throw new Error(`${label} 的 ${axis} 不是数值`);
        }
        values.push(value);
    }
    return values;
}

function projectToPixel(zone, x, z, label) {
    const u = zone.sx * x + zone.tx;
    const v = -zone.sy * z + zone.ty;
    const [
        width,
        height,
    ] = zone.size;
    if (!(u >= 0 && u < width && v >= 0 && v < height)) {
        throw new Error(
            `${label} 投影落在 ${zone.name} 底图外：u=${u.toFixed(1)} v=${v.toFixed(1)}（图 ${width}x${height}）；` +
                "请确认几份输入取自同一版本",
        );
    }
    return [
        round3(u),
        round3(v),
    ];
}

function projectEntries(entries, zones, usedZones, skippedMaps, label) {
    if (!Array.isArray(entries)) {
        throw new Error(`${label} 不是数组`);
    }
    const projected = [];
    for (const entry of entries) {
        if (typeof entry !== "object" || entry === null || Array.isArray(entry)) {
            throw new Error(`${label} 条目不是对象`);
        }
        const mapId = entry.map;
        if (typeof mapId !== "string" || !mapId) {
            throw new Error(`${label} 条目的 map 不是字符串`);
        }
        if (!("x" in entry) && "u" in entry) {
            throw new Error(
                `${label} 的原始数据只有 u/v、没有世界坐标 x/z：上游还是旧格式的发布数据，` +
                    "等 zmdmap 重新发布后再跑 fetch:zmdmap",
            );
        }

        let zone = usedZones.get(mapId);
        if (zone === undefined) {
            try {
                zone = resolveZone(zones, mapId);
            } catch {
                skippedMaps.add(mapId);
                continue;
            }
            usedZones.set(mapId, zone);
        }

        const entryId = entry.id ?? "";
        const [
            x,
            z,
        ] = readAxes(
            entry,
            [
                "x",
                "z",
            ],
            `${label} ${entryId}`,
        );
        const [
            u,
            v,
        ] = projectToPixel(zone, x, z, `${label} ${entryId}`);
        projected.push({...entry, u, v});
    }
    return projected;
}

function describeZones(usedZones) {
    const described = {};
    for (const mapId of [...usedZones.keys()].sort()) {
        const zone = usedZones.get(mapId);
        described[mapId] = {
            zone: zone.name,
            zone_id: zone.zone_id,
            size: [...zone.size],
            sx: zone.sx,
            tx: zone.tx,
            sy: zone.sy,
            ty: zone.ty,
        };
    }
    return described;
}

export function projectDataset(fileName, data, zones, skippedMaps) {
    const containers = COORDINATE_DATASETS[fileName];
    if (containers === undefined) {
        return data;
    }
    const usedZones = new Map();
    const projected = {...data};
    for (const container of containers) {
        projected[container] = projectEntries(
            data?.[container],
            zones,
            usedZones,
            skippedMaps,
            `${fileName}[${container}]`,
        );
    }
    const yNote = fileName === "delivery_destinations.json" ? `；${COORD_Y_NOTE}` : "";
    projected.coord = `${PROJECTED_COORD_TEXT}${yNote}`;
    projected.maps = describeZones(usedZones);
    return projected;
}

// ── 材料化 ──────────────────────────────────────────────────────────────

function selectFiles(only) {
    if (!only || only.length === 0) {
        return DATA_FILES;
    }
    const files = [];
    for (const keyword of only) {
        const fileName = keyword.endsWith(".json") ? keyword : `${keyword}.json`;
        if (!DATA_FILES.includes(fileName)) {
            throw new Error(
                `未知数据集 ${keyword}；可选：${DATA_FILES.map((name) => name.replace(/\.json$/, "")).join(", ")}`,
            );
        }
        if (!files.includes(fileName)) {
            files.push(fileName);
        }
    }
    return files;
}

function readDataset(path) {
    // 超出 Number 安全范围的整数保持原样，重新写出去时不会掉精度。
    return JSON.parse(readFileSync(path, "utf8"), (_key, value, context) =>
        typeof value === "number" && context.source.length >= 16 ? JSON.rawJSON(context.source) : value,
    );
}

function isUnchanged(outputPath, data) {
    if (!existsSync(outputPath)) {
        return false;
    }
    try {
        return readFileSync(outputPath, "utf8") === `${JSON.stringify(data, null, 4)}\n`;
    } catch {
        return false;
    }
}

function copyVersion(rawDir, outDir, force) {
    const source = resolve(rawDir, VERSION_FILE);
    if (!existsSync(source)) {
        console.warn(`[project] 没有 ${source}，保留 data/${VERSION_FILE} 不变`);
        return;
    }
    const version = readFileSync(source, "utf8").trim();
    const target = resolve(outDir, VERSION_FILE);
    if (!force && existsSync(target) && readFileSync(target, "utf8").trim() === version) {
        return;
    }
    writeFileSync(target, version, "utf8");
    console.log(`[project] ${VERSION_FILE} -> ${target}`);
}

/** 把 rawDir 里的原始数据投影/材料化到 outDir（缺省就是 data/）。 */
export async function projectData({rawDir, outDir, nav, repoRoot, only, force = false}) {
    const files = selectFiles(only);
    const zones = files.some((file) => file in COORDINATE_DATASETS) ? await loadNavZones(nav, repoRoot) : {};

    const skippedMaps = new Set();
    const pending = [];
    for (const fileName of files) {
        const sourcePath = resolve(rawDir, fileName);
        if (!existsSync(sourcePath)) {
            throw new Error(
                `缺少原始数据文件：${sourcePath}（先跑 pnpm fetch:zmdmap 下载，或先用 data/scripts/generate.py 本地生成）`,
            );
        }
        const result = projectDataset(fileName, readDataset(sourcePath), zones, skippedMaps);
        const outputPath = resolve(outDir, fileName);
        if (!force && isUnchanged(outputPath, result)) {
            console.log(`[project] ${fileName} 未变化，跳过写入`);
            continue;
        }
        pending.push({fileName, outputPath, result});
    }

    mkdirSync(outDir, {recursive: true});
    for (const {fileName, outputPath, result} of pending) {
        writeFileSync(outputPath, `${JSON.stringify(result, null, 4)}\n`, "utf8");
        console.log(`[project] ${fileName} -> ${outputPath}`);
    }
    if (files.length === DATA_FILES.length) {
        copyVersion(rawDir, outDir, force);
    }
    if (skippedMaps.size > 0) {
        console.warn(`[project] 以下底图没有对应的 BaseNav zone，其条目未纳入：${[...skippedMaps].sort().join(", ")}`);
    }
    return {written: pending.length, skippedMaps: [...skippedMaps].sort()};
}
