import {existsSync, readFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";

import {BASE_NAV_ZONE_IMAGE_PARTS} from "../../MapNavigator/web/static/js/model.js";
import {readJsonc} from "../jsonc.mjs";
import {readTeleportAnchors, registeredAnchors} from "../SceneTeleport/model.mjs";
import {dataDir, repoRoot} from "../utils/paths.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));

export const ENERGY_POINTS_PATH = resolve(dataDir, "energy_points.json");
export const ROUTES_PATH = resolve(__dirname, "routes.json");
const LOCATIONS_GO_PATH = resolve(repoRoot, "agent", "go-service", "autoessence", "locations.go");
const REGION_NODE_DIR = resolve(repoRoot, "assets", "resource", "pipeline", "AutoEssence", "RegionNodes");

const ROUTE_FIELDS = new Set(["level", "name", "manual", "anchor", "path"]);

// 激发点用异步交互：行进中认到交互提示就停车，确认后不按键，选哪一行交回外层流程
const TRIGGER_INTERACT = {
    interact_text: {node: "__AutoEssenceRecoApproachInteractText"},
    interact_scan: "__AutoEssenceRecoApproachInteractScan",
    interact_rec: true,
};

export function readEnergyPoints() {
    let data;
    try {
        data = JSON.parse(readFileSync(ENERGY_POINTS_PATH, "utf8"));
    } catch {
        throw new Error("[AutoEssence] 缺少激发点数据，请先运行 pnpm fetch:zmdmap --only energy_points");
    }
    if (!Array.isArray(data.points) || !data.maps) {
        throw new Error("[AutoEssence] energy_points.json 缺少 points 或 maps");
    }
    return data;
}

export function readRoutes() {
    const routes = readJsonc(ROUTES_PATH);
    if (!Array.isArray(routes.regions)) {
        throw new Error("[AutoEssence] routes.json 缺少 regions 数组");
    }
    for (const [index, route] of routes.regions.entries()) {
        const unknown = Object.keys(route).filter((field) => !ROUTE_FIELDS.has(field));
        if (typeof route.level !== "string" || unknown.length) {
            throw new Error(`[AutoEssence] routes.json regions[${index}] 需要 level，且只认 ${[...ROUTE_FIELDS].join(" / ")}`);
        }
    }
    return routes;
}

export function readRegionKeys() {
    const source = readFileSync(LOCATIONS_GO_PATH, "utf8");
    const block = source.match(/locationLevelToKey\s*=\s*map\[string\]string\{([^}]*)\}/);
    if (!block) {
        throw new Error("[AutoEssence] locations.go 里没有 locationLevelToKey");
    }
    return new Map([...block[1].matchAll(/"([^"]+)":\s*"([^"]+)"/g)].map((match) => [match[1], match[2]]));
}

export function levelName(teleport, levelId) {
    const name = teleport.levels?.[levelId]?.names?.zh_cn;
    if (!name) {
        throw new Error(`[AutoEssence] 营地数据里没有关卡 ${levelId} 的名字`);
    }
    return name;
}

function locatorZoneId(maps, mapId) {
    const [, zone, imageFile] = BASE_NAV_ZONE_IMAGE_PARTS[maps[mapId]?.zone] ?? [];
    if (imageFile !== "Base.png") {
        throw new Error(`[AutoEssence] 底图 ${mapId} 对应不到定位器的 Base 区域`);
    }
    return `${zone}_Base`;
}

function startAnchor(point, route, anchors) {
    const sameMap = anchors.filter((anchor) => anchor.map === point.map);
    if (route.anchor) {
        const chosen = sameMap.find((anchor) => anchor.entry === route.anchor);
        if (!chosen) {
            throw new Error(`[AutoEssence] ${point.level} 的 anchor ${route.anchor} 不是 ${point.map} 上已登记的营地入口`);
        }
        return chosen;
    }
    const distance = (anchor) => Math.hypot(anchor.u - point.u, anchor.v - point.v);
    const [nearest] = [...sameMap].sort((a, b) => distance(a) - distance(b) || a.entry.localeCompare(b.entry));
    if (!nearest) {
        throw new Error(`[AutoEssence] ${point.map} 上没有已登记的营地`);
    }
    return nearest;
}

function triggerPath(point, route, zoneId) {
    const target = [point.u, point.v];
    const approach = route.path ?? [{action: "NAVMESH", target, target_deck_y: point.y}];
    if (!Array.isArray(approach) || approach.some((step) => !Array.isArray(step) && step.action === "ZONE")) {
        throw new Error(`[AutoEssence] ${point.level} 的 path 须为路点数组，首点 ZONE 由生成器插入`);
    }
    return [{action: "ZONE", zone_id: zoneId}, ...approach, {action: "INTERACT", target, strict_arrival: true, ...TRIGGER_INTERACT}];
}

export function buildRegions() {
    const energy = readEnergyPoints();
    const teleport = readTeleportAnchors();
    const anchors = registeredAnchors();
    const keys = readRegionKeys();
    const routes = new Map(readRoutes().regions.map((route) => [route.level, route]));

    const unknownRoutes = [...routes.keys()].filter((level) => !energy.points.some((point) => point.level === level));
    if (unknownRoutes.length) {
        throw new Error(`[AutoEssence] routes.json 里的 ${unknownRoutes.join(", ")} 已不在激发点数据里，请先运行 sync-routes`);
    }

    return energy.points.flatMap((point) => {
        const route = routes.get(point.level);
        if (!route) {
            throw new Error(`[AutoEssence] routes.json 缺少 ${point.level}，请先运行 sync-routes`);
        }
        const key = keys.get(point.level);
        if (!key) {
            console.log(`[AutoEssence] ${point.level}（${levelName(teleport, point.level)}）尚未接入：在 locations.go 的 locationLevelToKey 登记区域 key 后生成`);
            return [];
        }
        // TODO: routes.json 里标 manual 的地区逐个实机确认后去掉 manual，改由生成器产出
        if (route.manual) {
            if (route.anchor || route.path) {
                throw new Error(`[AutoEssence] ${point.level} 标了 manual，不再生成，anchor / path 不会生效`);
            }
            if (!existsSync(resolve(REGION_NODE_DIR, `${key}.json`))) {
                throw new Error(`[AutoEssence] ${point.level} 标了 manual，但没有手写的 RegionNodes/${key}.json`);
            }
            return [];
        }
        const zoneId = locatorZoneId(energy.maps, point.map);
        return {
            key,
            level: point.level,
            name: levelName(teleport, point.level),
            entry: startAnchor(point, route, anchors).entry,
            path: triggerPath(point, route, zoneId),
        };
    });
}

export function rawJson(value) {
    return {value, raw: JSON.stringify(value, null, 4)};
}
