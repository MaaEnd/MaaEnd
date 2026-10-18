import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {readJsonc} from "../jsonc.mjs";
import {dataDir} from "../utils/paths.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));

export const TELEPORT_ANCHORS_PATH = resolve(dataDir, "teleport_anchors.json");
export const ANCHORS_CONFIG_PATH = resolve(__dirname, "anchors.json");

const ZH_DIRECTIONS = {east: "东侧", west: "西侧", south: "南侧", north: "北侧"};
const EN_DIRECTIONS = {east: "East", west: "West", south: "South", north: "North"};

export function readTeleportAnchors() {
    let data;
    try {
        data = readJsonc(TELEPORT_ANCHORS_PATH);
    } catch {
        throw new Error("[SceneTeleport] 缺少营地数据，请先运行 pnpm fetch:zmdmap --only teleport_anchors");
    }
    if (!data.levels) {
        throw new Error("[SceneTeleport] 营地数据缺少区域名（levels / area_names），请等 zmdmap 用当前代码重新发布后再生成");
    }
    return data;
}

export function readAnchorsConfig() {
    return readJsonc(ANCHORS_CONFIG_PATH);
}

export function toPascalCase(text) {
    return text
        .replace(/['’]s\b/g, "")
        .split(/[^A-Za-z0-9]+/)
        .filter(Boolean)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join("");
}

function directionFrom(center, anchor) {
    const dx = anchor.u - center.u;
    const dy = anchor.v - center.v;
    if (Math.abs(dx) >= Math.abs(dy)) {
        return dx >= 0 ? "east" : "west";
    }
    return dy >= 0 ? "south" : "north";
}

/** 同一关卡里名字相同的营地，按各自相对这几个点中心的方位加后缀；方位还撞就再按营地 ID 编号。 */
export function disambiguatedNames(levelAnchors, nameOf, directions) {
    const groups = new Map();
    for (const anchor of levelAnchors) {
        const name = nameOf(anchor);
        groups.set(name, [...(groups.get(name) ?? []), anchor]);
    }

    const names = new Map();
    for (const [name, group] of groups) {
        if (group.length === 1) {
            names.set(group[0].id, name);
            continue;
        }
        const center = {
            u: group.reduce((sum, anchor) => sum + anchor.u, 0) / group.length,
            v: group.reduce((sum, anchor) => sum + anchor.v, 0) / group.length,
        };
        const byDirection = new Map();
        for (const anchor of [...group].sort((a, b) => Number(a.id) - Number(b.id))) {
            const direction = directionFrom(center, anchor);
            byDirection.set(direction, [...(byDirection.get(direction) ?? []), anchor]);
        }
        for (const [direction, members] of byDirection) {
            members.forEach((anchor, offset) => {
                names.set(anchor.id, `${name}${directions[direction]}${members.length > 1 ? offset + 1 : ""}`);
            });
        }
    }
    return names;
}

export function englishNames(levelAnchors) {
    return disambiguatedNames(levelAnchors, (anchor) => toPascalCase(anchor.area_names.en_us), EN_DIRECTIONS);
}

const round1 = (value) => Math.round(value * 10) / 10;

export function buildTeleportAnchors() {
    const data = readTeleportAnchors();
    const config = readAnchorsConfig();
    const anchorsById = new Map(data.anchors.map((anchor) => [anchor.id, anchor]));

    const entries = [];
    for (const [levelId, levelConfig] of Object.entries(config.levels)) {
        const region = config.regions[levelConfig.region];
        const levelAnchors = data.anchors.filter((anchor) => anchor.level === levelId);
        const missingArea = levelAnchors.find((anchor) => !anchor.area_names?.zh_cn);
        if (missingArea) {
            throw new Error(`[SceneTeleport] 营地 ${missingArea.id}（${levelId}）没有区域名`);
        }
        const areaNames = disambiguatedNames(levelAnchors, (anchor) => anchor.area_names.zh_cn, ZH_DIRECTIONS);
        const levelName = data.levels[levelId].names.zh_cn;
        const prefix = `${levelConfig.region}${levelConfig.sub_region}`;

        for (const [name, id] of Object.entries(levelConfig.anchors)) {
            const anchor = anchorsById.get(id);
            if (!anchor) {
                throw new Error(`[SceneTeleport] 营地 ${id}（${levelId}）已不在数据里，请从 anchors.json 移除并处理引用它的节点`);
            }
            const areaName = areaNames.get(id);
            entries.push({
                Node: `__ScenePrivateMap${prefix}EnterWorld${prefix}${name}`,
                Entry: `SceneEnterWorld${prefix}${name}`,
                EnterMap: `SceneEnterMap${prefix}`,
                PickAnchor: `__ScenePrivateMap${prefix}EnterWorldAnchorWithPick`,
                Zone: region.zone,
                At: [round1(anchor.u), round1(anchor.v)],
                Desc: `在地图界面找锚点 (${levelName}-${areaName})`,
                EntryDesc: `从任意界面进入${region.name}-${levelName}-${areaName}大世界`,
            });
        }
    }
    return entries;
}
