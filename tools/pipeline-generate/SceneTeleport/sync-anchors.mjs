import {writeFileSync} from "node:fs";
import {ANCHORS_CONFIG_PATH, readAnchorsConfig, readTeleportAnchors, usedIndices} from "./model.mjs";

const data = readTeleportAnchors();
const config = readAnchorsConfig();
const regionMaps = new Set(Object.values(config.regions).map((region) => region.map));

const added = [];
const unbound = new Map();
for (const anchor of [...data.anchors].sort((a, b) => Number(a.id) - Number(b.id))) {
    if (!regionMaps.has(anchor.map)) {
        continue;
    }
    const levelConfig = config.levels[anchor.level];
    if (!levelConfig) {
        unbound.set(anchor.level, (unbound.get(anchor.level) ?? 0) + 1);
        continue;
    }
    const known = [...Object.values(levelConfig.anchors), ...Object.values(levelConfig.manual ?? {})];
    if (known.includes(anchor.id)) {
        continue;
    }
    const index = Math.max(-1, ...usedIndices(levelConfig)) + 1;
    levelConfig.anchors[index] = anchor.id;
    added.push(`${anchor.level} ${levelConfig.sub_region}${index} <- ${anchor.id}`);
}

for (const [levelId, count] of unbound) {
    const levelName = data.levels[levelId]?.names?.zh_cn ?? levelId;
    console.warn(
        `[SceneTeleport] ${levelId}（${levelName}）有 ${count} 个营地，但 anchors.json 没登记它的子区域；` +
            "补上子区域的地图节点后在 levels 里登记，营地才会生成",
    );
}

if (added.length > 0) {
    writeFileSync(ANCHORS_CONFIG_PATH, `${JSON.stringify(config, null, 4)}\n`, "utf8");
    console.log(`[SceneTeleport] 新增 ${added.length} 个营地：\n  ${added.join("\n  ")}`);
} else {
    console.log("[SceneTeleport] anchors.json 无变化");
}
