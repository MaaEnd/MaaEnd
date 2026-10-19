import {writeFileSync} from "node:fs";
import {ANCHORS_CONFIG_PATH, englishNames, readAnchorsConfig, readTeleportAnchors} from "./model.mjs";

const data = readTeleportAnchors();
const config = readAnchorsConfig();
const regionMaps = new Set(Object.values(config.regions).map((region) => region.map));

const added = [];
const unbound = new Map();
for (const [levelId, levelAnchors] of Map.groupBy(
    data.anchors.filter((anchor) => regionMaps.has(anchor.map)),
    (anchor) => anchor.level,
)) {
    const levelConfig = config.levels[levelId];
    if (!levelConfig) {
        unbound.set(levelId, levelAnchors.length);
        continue;
    }
    const known = {...levelConfig.anchors, ...levelConfig.manual};
    const knownIds = new Set(Object.values(known));
    const names = englishNames(levelAnchors);
    for (const anchor of [...levelAnchors].sort((a, b) => Number(a.id) - Number(b.id))) {
        if (knownIds.has(anchor.id)) {
            continue;
        }
        const base = names.get(anchor.id);
        let name = base;
        for (let ordinal = 2; name in known; ordinal++) {
            name = `${base}${ordinal}`;
        }
        levelConfig.anchors[name] = anchor.id;
        known[name] = anchor.id;
        added.push(`${levelId} ${levelConfig.sub_region}${name} <- ${anchor.id}`);
    }
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
