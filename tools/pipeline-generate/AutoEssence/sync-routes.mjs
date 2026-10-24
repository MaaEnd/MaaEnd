import {existsSync, readFileSync, writeFileSync} from "node:fs";

import {readTeleportAnchors} from "../SceneTeleport/model.mjs";
import {levelName, readEnergyPoints, readRoutes, ROUTES_PATH} from "./model.mjs";

// 按激发点数据刷新 routes.json：新地区补一条只有元数据的条目，地名跟着数据更新，人工覆盖原样保留。
const energy = readEnergyPoints();
const teleport = readTeleportAnchors();
const routes = existsSync(ROUTES_PATH) ? readRoutes() : {regions: []};
const existing = new Map(routes.regions.map((route) => [route.level, route]));

const stale = [...existing.keys()].filter((level) => !energy.points.some((point) => point.level === level));
if (stale.length) {
    throw new Error(`[AutoEssence] ${stale.join(", ")} 已不在激发点数据里，请从 routes.json 移除并处理对应的区域 key`);
}

const added = [];
const regions = energy.points.map((point) => {
    const route = existing.get(point.level);
    if (!route) {
        added.push(point.level);
    }
    const {level: _level, name: _name, ...overrides} = route ?? {};
    return {level: point.level, name: levelName(teleport, point.level), ...overrides};
});

const text = `${JSON.stringify({regions}, null, 4)}\n`;
if (existsSync(ROUTES_PATH) && readFileSync(ROUTES_PATH, "utf8") === text) {
    console.log("[AutoEssence] routes.json 无变化");
} else {
    writeFileSync(ROUTES_PATH, text, "utf8");
    console.log(`[AutoEssence] 已同步 routes.json${added.length ? `，新增 ${added.join(", ")}` : ""}，人工覆盖原样保留`);
}
