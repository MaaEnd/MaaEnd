import {existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {gzipSync} from "node:zlib";

const currentDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(currentDir, "../../..");
const endfieldDataRoot = process.env.ENDFIELD_DATA ?? resolve(repoRoot, "../EndfieldData");
const jsonRoot = resolve(endfieldDataRoot, "Json");
const missionDir = resolve(jsonRoot, "MissionRuntimeAsset");
const levelDataDir = resolve(jsonRoot, "LevelData");
const levelScriptDir = resolve(jsonRoot, "LevelScriptData");
// 运行时读压缩后的单行 JSON；加 --pretty 时另在脚本旁输出一份带缩进的 JSON 供本地对比，不提交。
const outputPath = resolve(repoRoot, "assets/data/AutoMission/missions.json.gz");
const legacyOutputPath = resolve(repoRoot, "assets/data/AutoMission/missions.json");
const prettyOutputPath = resolve(currentDir, "missions.pretty.json");
const pretty = process.argv.includes("--pretty");

// 世界坐标换算到 MapLocator 底图坐标：u = s * x + tx，v = -s * z + ty。
// 参数由 UILevelMapLoadConfig 中各关卡 worldRect 的并集和底图尺寸算出。
const MAP_PLANES = {
    map01: {zone: "ValleyIV_Base", s: 0.703125, tx: 720, ty: 630},
    map02: {zone: "Wuling_Base", s: 0.75, tx: 1536, ty: 1344},
    base01: {zone: "OMVBase01", s: 0.75, tx: 192, ty: 192},
    dung01_wrdg001: {zone: "Dung01Base", s: 0.75, tx: 384, ty: 480},
    indie_dg005: {zone: "IndieDg005Base", s: 0.75, tx: 1344, ty: -96},
    indie_dg007: {zone: "IndieDg007Base", s: 0.75, tx: 384, ty: 384},
    indie_dg016: {zone: "IndieDg016Base", s: 0.75, tx: 288, ty: 288},
};

// 交互物模板里的通用组件，不能区分交互物种类。
const GENERIC_COMPONENTS = new Set([
    "AttackTriggerComponentForIntData",
    "BaseControllerData",
    "ClickTriggerComponentForIntData",
    "HittableComponentForIntData",
    "Interactive3DUIComponentData",
    "InteractiveAssistedAimingComponentData",
    "InteractiveAudioData",
    "InteractiveBehitPerformComponentData",
    "InteractiveCommonPerformComponentData",
    "InteractiveHintPerformComponentData",
    "InteractiveModelComponentData",
    "InteractiveRootComponentData",
    "KeepRelativeOffsetComponentData",
    "PlayerInteractPerformComponentData",
    "ScanTriggerComponentForIntData",
    "ShowGuideComponentData",
    "ShowGuideWithConditionComponentData",
    "SimpleAnimatorComponentData",
    "TriggerObserverComponentData",
    "TriggerZoneComponentForIntData",
    "WaterAbsorbedImpactComponentData",
    "WaterProgressComponentData",
    "WaterProgressDriveCurveMovementComponentData",
    "WaterVolHeightMarkerComponentData",
]);

// 玩家进入触发区后会自动推进剧情的动作。

// 交互文字由客户端按组件写死、数据里查不到的交互物，按 entityDataIdKey 手工补文本 key。
const MANUAL_INTERACT_TEXTS = {
    int_fixable_robot: ["lang_int_fix_option"],
};

function readJson(path) {
    return JSON.parse(readFileSync(path, "utf8"));
}

function shortType(value) {
    if (typeof value !== "string") {
        return "";
    }
    return value.split(",")[0].split(".").pop();
}

const textTable = JSON.parse(
    readFileSync(resolve(endfieldDataRoot, "TableCfg/TextTable.json"), "utf8").replace(
        /"id":\s*(-?\d+)/g,
        '"id":"$1"',
    ),
);
const i18nCn = readJson(resolve(endfieldDataRoot, "TableCfg/I18nTextTable_CN.json"));
const i18nTc = readJson(resolve(endfieldDataRoot, "TableCfg/I18nTextTable_TC.json"));

function localizedText(key) {
    const node = key ? textTable[key] : undefined;
    const id = node && node.id != null ? String(node.id) : "";
    return {
        key: key ?? "",
        zh_cn: id ? (i18nCn[id] ?? "") : "",
        zh_tw: id ? (i18nTc[id] ?? "") : "",
    };
}

function objectiveText(key) {
    const text = localizedText(key);
    const plain = (value) => String(value ?? "").replace(/<@[^>]*>/g, "").replace(/<\/>/g, "");
    return {
        key: text.key,
        zh_cn: plain(text.zh_cn),
        zh_tw: plain(text.zh_tw),
    };
}

// 简繁都为空时返回 null，调用方据此不写字段。
function namedText(key) {
    if (!key) {
        return null;
    }
    const text = localizedText(key);
    return text.zh_cn || text.zh_tw ? text : null;
}

const proxyById = new Map();
for (const row of Object.values(readJson(resolve(jsonRoot, "GameplayConfig/NpcProxyTable.json")).dataTable ?? {})) {
    if (row && typeof row.proxyId === "string" && row.proxyId) {
        proxyById.set(row.proxyId, row);
    }
}
const proxyEx = readJson(resolve(jsonRoot, "GameplayConfig/NpcProxyExDataTable.json"));
const proxyInfoData = proxyEx.proxyInfoData ?? {};
const proxyDialogData = proxyEx.data ?? {};
const npcTemplateGroup = readJson(resolve(endfieldDataRoot, "TableCfg/NpcTemplateGroupTable.json"));
const dialogBrief = readJson(resolve(jsonRoot, "GameplayConfig/DialogIdTable.json")).dialogBriefInfo ?? {};
// m_areas 按 levelIdNum 分组，同一个 missionAreaId 可能出现在多个关卡。
const missionAreaGroups = readJson(resolve(jsonRoot, "GameplayConfig/MissionAreaTable.json")).m_areas ?? {};
const worldEntityBrief = readJson(resolve(jsonRoot, "GameplayConfig/WorldEntityRegistry.json")).worldEntityBriefInfos ?? {};
const interactiveTable = readJson(resolve(jsonRoot, "Interactive/InteractiveTable.json"));
const systemJumpTable = readJson(resolve(endfieldDataRoot, "TableCfg/SystemJumpTable.json"));

const dialogsByProxy = new Map();
for (const [dialogId, brief] of Object.entries(dialogBrief)) {
    for (const proxyId of brief?.npcProxyIds ?? []) {
        if (!dialogsByProxy.has(proxyId)) {
            dialogsByProxy.set(proxyId, []);
        }
        dialogsByProxy.get(proxyId).push(dialogId);
    }
}

// dlg_a1m14_3_001 -> dlg_a1m14_3，用于从对话行反查说话的 NPC。
const actorsByDialog = new Map();
for (const [lineKey, line] of Object.entries(readJson(resolve(endfieldDataRoot, "TableCfg/DialogTextTable.json")))) {
    const match = /^(.+)_\d{3}(?:_[a-z]+)?$/.exec(lineKey);
    if (!match || !line?.actorNameId) {
        continue;
    }
    if (!actorsByDialog.has(match[1])) {
        actorsByDialog.set(match[1], []);
    }
    actorsByDialog.get(match[1]).push(line.actorNameId);
}

const levelNumCache = new Map();
function levelNum(sceneId) {
    if (levelNumCache.has(sceneId)) {
        return levelNumCache.get(sceneId);
    }
    let num = null;
    const dir = resolve(levelDataDir, sceneId);
    if (sceneId && existsSync(dir)) {
        for (const name of readdirSync(dir)) {
            if (name.endsWith(".json")) {
                num = readJson(resolve(dir, name)).levelIdNum ?? null;
                break;
            }
        }
    }
    levelNumCache.set(sceneId, num);
    return num;
}

function entityValues(collection) {
    if (Array.isArray(collection)) {
        return collection;
    }
    return collection && typeof collection === "object" ? Object.values(collection) : [];
}

const levelEntityCache = new Map();
// 关卡里被锁住的交互物：全局 id -> 锁列表，与实体一起读。
const levelLockCache = new Map();
function levelEntities(sceneId) {
    if (levelEntityCache.has(sceneId)) {
        return levelEntityCache.get(sceneId);
    }
    const entities = new Map();
    const locks = new Map();
    const dir = resolve(levelDataDir, sceneId);
    if (existsSync(dir)) {
        for (const name of readdirSync(dir)) {
            if (!name.endsWith(".json")) {
                continue;
            }
            const data = readJson(resolve(dir, name));
            for (const key of ["interactives", "npcs", "enemies"]) {
                for (const entity of entityValues(data[key])) {
                    if (entity?.levelLogicId != null) {
                        entities.set(entity.levelLogicId, entity);
                    }
                }
            }
            for (const lockData of entityValues(data.interactiveLockData)) {
                if (lockData?.logicIdGlobal != null) {
                    locks.set(lockData.logicIdGlobal, entityValues(lockData.locks));
                }
            }
        }
    }
    levelEntityCache.set(sceneId, entities);
    levelLockCache.set(sceneId, locks);
    return entities;
}

// 交互物上锁时显示的解锁选项文字，如「解锁」；没上锁时为空。
function lockOptionKeys(sceneId, logicIdGlobal) {
    if (logicIdGlobal == null) {
        return [];
    }
    levelEntities(sceneId);
    return (levelLockCache.get(sceneId)?.get(logicIdGlobal) ?? [])
        .map((lock) => lock?.optionUnlock?.key)
        .filter(Boolean);
}

function scriptEntity(sceneId, scriptIdGlobal, slotId) {
    const path = resolve(levelScriptDir, sceneId, `${scriptIdGlobal}.json`);
    if (!existsSync(path)) {
        return null;
    }
    const data = readJson(path);
    for (const key of ["interactives", "npcs", "enemies"]) {
        const entity = data[key]?.[String(slotId)];
        if (entity) {
            return entity;
        }
    }
    return null;
}

function missionArea(sceneId, areaId) {
    const area = missionAreaGroups[String(levelNum(sceneId))]?.[areaId];
    if (area) {
        return area;
    }
    for (const group of Object.values(missionAreaGroups)) {
        if (group?.[areaId]) {
            return group[areaId];
        }
    }
    return null;
}

function resolveEntity(track) {
    const num = levelNum(track.sceneId);
    if (num == null) {
        return null;
    }
    if (track.trackScriptEntity) {
        return scriptEntity(track.sceneId, num * 1e8 + track.scriptId, track.entitySlotId);
    }
    // 副本里的 entityLogicId 已经是全局 id。
    const globalId = track.entityLogicId >= 1e8 ? track.entityLogicId : num * 1e8 + track.entityLogicId;
    const brief = worldEntityBrief[String(globalId)];
    // 简要信息只有模板和坐标，交互文字等实例属性要从关卡数据里取完整实体。
    const entity = levelEntities(track.sceneId).get(globalId);
    if (brief) {
        return {...entity, entityDataIdKey: brief.detailId, position: brief.position, logicIdGlobal: globalId};
    }
    return entity ? {...entity, logicIdGlobal: globalId} : null;
}

const templateCache = new Map();
function entityTemplate(dataKey) {
    if (!dataKey) {
        return null;
    }
    if (templateCache.has(dataKey)) {
        return templateCache.get(dataKey);
    }
    const templateId = interactiveTable.interactiveDataDict?.[dataKey]?.templateId ?? dataKey;
    const templatePath = interactiveTable.coreTemplatePathDict?.[templateId];
    let template = null;
    if (templatePath) {
        const path = resolve(jsonRoot, templatePath.replace(/^Data\/Json\//, ""));
        if (existsSync(path)) {
            template = readJson(path);
        }
    }
    templateCache.set(dataKey, template);
    return template;
}

function entityComponents(dataKey) {
    const components = (entityTemplate(dataKey)?.componentList ?? [])
        .map((component) => shortType(component?.$type))
        .filter((name) => name && !GENERIC_COMPONENTS.has(name));
    return [...new Set(components)];
}

function propertyStrings(properties, key) {
    return (properties ?? [])
        .filter((property) => property?.key === key)
        .flatMap((property) => property.value?.valueArray ?? [])
        .map((value) => value?.valueString)
        .filter((value) => typeof value === "string" && value);
}

function templateOptionKeys(node, keys = []) {
    if (Array.isArray(node)) {
        for (const item of node) {
            templateOptionKeys(item, keys);
        }
    } else if (node && typeof node === "object") {
        if (typeof node.optionName?.key === "string" && node.optionName.key) {
            keys.push(node.optionName.key);
        }
        for (const value of Object.values(node)) {
            templateOptionKeys(value, keys);
        }
    }
    return keys;
}

// 交互文字依次取：实例的点击选项、实例的 InteractText（任务信标）、模板选项、手工补充。
function interactTextKeys(entity) {
    const optionKeys = Object.values(entity?.componentProperties ?? {}).flatMap((properties) =>
        propertyStrings(properties, "click_option_name_list"),
    );
    if (optionKeys.length > 0) {
        return optionKeys;
    }
    const beaconKeys = propertyStrings(entity?.properties, "InteractText");
    if (beaconKeys.length > 0) {
        return beaconKeys;
    }
    const templateKeys = templateOptionKeys(entityTemplate(entity?.entityDataIdKey));
    if (templateKeys.length > 0) {
        return templateKeys;
    }
    return MANUAL_INTERACT_TEXTS[entity?.entityDataIdKey] ?? [];
}

// 交互物上锁时游戏只显示解锁选项，解锁后才出现原来的选项，所以解锁选项排在最前，原来的选项保留在后面。
function interactTextsOf(entity, sceneId) {
    const keys = [...lockOptionKeys(sceneId, entity?.logicIdGlobal), ...interactTextKeys(entity)];
    return [...new Set(keys)].map((key) => namedText(key)).filter(Boolean);
}

// 先按完整场景名查，再按场景前缀查；没有底图的副本两者都查不到。
function mapPlane(sceneId) {
    const scene = String(sceneId ?? "");
    return MAP_PLANES[scene] ?? MAP_PLANES[scene.split("_")[0]];
}

// 世界坐标换算到 MapLocator 底图坐标；只有 MAP_PLANES 里的地图有底图。
function placement(sceneId, position) {
    const plane = mapPlane(sceneId);
    if (!plane || !position || typeof position.x !== "number" || typeof position.z !== "number") {
        return {};
    }
    const round = (value) => Math.round(value * 1000) / 1000;
    return {
        zone_id: plane.zone,
        x: round(plane.s * position.x + plane.tx),
        y: round(-plane.s * position.z + plane.ty),
    };
}

// 坐标点、NPC、任务区域等带上世界高度，寻路时用来钉住目标所在的那层可走面；没有底图时不写。
function groundPlacement(sceneId, position) {
    const result = placement(sceneId, position);
    if (result.zone_id && typeof position.y === "number") {
        result.height = Math.round(position.y * 1000) / 1000;
    }
    return result;
}

// 关卡脚本运行时给代理改的名字，同一代理在不同进度可能显示不同名字。
function collectNameOverrides(dataMap, overrides) {
    for (const node of dataMap?.actionList ?? []) {
        if (shortType(node?.$type) !== "NpcProxyOverrideName") {
            continue;
        }
        const proxyId = node._proxyId?.constValue;
        const nameKey = node._nameId?.constValue?.key;
        if (typeof proxyId !== "string" || !proxyId || !nameKey) {
            continue;
        }
        if (!overrides.has(proxyId)) {
            overrides.set(proxyId, new Set());
        }
        overrides.get(proxyId).add(nameKey);
    }
}

// 关卡脚本主动开始的对话：走进触发区就会播放，不需要找 NPC 交互。
function collectStartedDialogs(dataMap, dialogs) {
    for (const node of dataMap?.actionList ?? []) {
        if (shortType(node?.$type) !== "StartDialogAction") {
            continue;
        }
        const dialogId = node._dialogId?.constValue;
        if (typeof dialogId === "string" && dialogId) {
            dialogs.add(dialogId);
        }
    }
}

// 各关卡的 NPC 巡逻路线，键为 `关卡:patrolId`，同一个 patrolId 在不同关卡是不同路线。
const npcPatrols = new Map();
if (existsSync(levelDataDir)) {
    for (const sceneId of readdirSync(levelDataDir)) {
        const dir = resolve(levelDataDir, sceneId);
        if (!statSync(dir).isDirectory()) {
            continue;
        }
        for (const name of readdirSync(dir)) {
            if (!name.endsWith(".json")) {
                continue;
            }
            for (const patrol of readJson(resolve(dir, name)).npcPatrol ?? []) {
                if (typeof patrol?.patrolId === "number" && Array.isArray(patrol.points) && patrol.points.length > 0) {
                    npcPatrols.set(`${sceneId}:${patrol.patrolId}`, patrol);
                }
            }
        }
    }
}

// 关卡脚本让 NPC 走的巡逻路线的终点：走完一遍就停在那里等玩家，按脚本所在关卡换算成底图坐标。
// 只收常量路线；循环巡逻（loop 不为 0）的终点不是停下的位置，不收。
function collectPatrolEnds(dataMap, sceneId, patrolEnds) {
    for (const node of dataMap?.actionList ?? []) {
        if (shortType(node?.$type) !== "NpcProxyPatrolStart") {
            continue;
        }
        const proxyId = node._targetProxy?.constValue;
        const patrolId = node._patrolId?.paramSource === 0 ? node._patrolId.constValue : null;
        if (typeof proxyId !== "string" || !proxyId || typeof patrolId !== "number") {
            continue;
        }
        const patrol = npcPatrols.get(`${sceneId}:${patrolId}`);
        if (!patrol || patrol.loop) {
            continue;
        }
        const point = groundPlacement(sceneId, patrol.points.at(-1)?.position);
        if (!point.zone_id) {
            continue;
        }
        if (!patrolEnds.has(proxyId)) {
            patrolEnds.set(proxyId, []);
        }
        patrolEnds.get(proxyId).push(point);
    }
}

// 动作节点的后继：_nextID 以及分支节点里各条分支的 id。
function nextActionIds(node) {
    const ids = [];
    for (const [key, value] of Object.entries(node ?? {})) {
        if (!/next/i.test(key)) {
            continue;
        }
        if (typeof value === "number") {
            ids.push(value);
        } else if (Array.isArray(value)) {
            for (const item of value) {
                if (typeof item === "number") {
                    ids.push(item);
                } else if (item && typeof item === "object") {
                    ids.push(...Object.values(item).filter((id) => typeof id === "number"));
                }
            }
        }
    }
    return ids.filter((id) => id >= 0);
}

// 关卡脚本里玩家走进去就推进任务的触发区：走进触发区后的动作链里有 CallServer。
// 记下所在关卡、脚本 id 和触发区中心的世界坐标。
function collectProgressTriggers(script, sceneId, scriptId, triggers) {
    const dataMap = script?.actionMap?.dataMap;
    const nodes = new Map();
    for (const node of [...(dataMap?.headerList ?? []), ...(dataMap?.actionList ?? [])]) {
        nodes.set(node?._ID, node);
    }
    for (const header of dataMap?.headerList ?? []) {
        if (shortType(header?.$type) !== "OnLeaderEnterTriggerVolume") {
            continue;
        }
        const slot = header._triggerSlotIdFilter?.constValue;
        const position = script.triggerVolumes?.[String(slot)]?.shapeList?.[0]?.position;
        if (!position) {
            continue;
        }
        const visited = new Set();
        const pending = [header._ID];
        let callsServer = false;
        while (pending.length > 0 && !callsServer) {
            const id = pending.pop();
            if (visited.has(id)) {
                continue;
            }
            visited.add(id);
            const node = nodes.get(id);
            callsServer = shortType(node?.$type) === "CallServer";
            pending.push(...nextActionIds(node));
        }
        if (callsServer) {
            triggers.push({sceneId, scriptId, position});
        }
    }
}

const proxyNameOverrides = new Map();
const scriptStartedDialogs = new Set();
const proxyPatrolEnds = new Map();
const progressTriggers = [];
if (existsSync(levelScriptDir)) {
    for (const sceneId of readdirSync(levelScriptDir)) {
        const dir = resolve(levelScriptDir, sceneId);
        for (const name of readdirSync(dir)) {
            if (!name.endsWith(".json")) {
                continue;
            }
            const script = readJson(resolve(dir, name));
            const dataMap = script.actionMap?.dataMap;
            collectNameOverrides(dataMap, proxyNameOverrides);
            collectStartedDialogs(dataMap, scriptStartedDialogs);
            collectPatrolEnds(dataMap, sceneId, proxyPatrolEnds);
            collectProgressTriggers(script, sceneId, Number(name.slice(0, -".json".length)), progressTriggers);
        }
    }
}

// 区域里离中心再近就没必要单独去的触发区距离，世界单位米；走到区域中心时基本已经顺路进去了。
const AREA_TRIGGER_MIN_OFFSET = 5;

// 任务区域里推进任务的触发区：同一任务（脚本 id 在区域的 subDataParentId 段内）、在区域范围内、
// 动作链里有 CallServer。只有一个且离中心够远时才返回，多个时分不清是哪一步的，不返回。
function areaTriggerPoint(sceneId, area) {
    const center = area?.shape?.position;
    const parent = area?.subDataParentId;
    if (!center || !parent) {
        return {};
    }
    const radius = area.shape.radius || Math.max(area.shape.size?.x ?? 0, area.shape.size?.z ?? 0) / 2;
    const inside = progressTriggers.filter(
        (trigger) =>
            trigger.sceneId === sceneId &&
            trigger.scriptId >= parent &&
            trigger.scriptId < parent + 10000 &&
            Math.hypot(trigger.position.x - center.x, trigger.position.z - center.z) <= radius,
    );
    if (inside.length !== 1) {
        return {};
    }
    const position = inside[0].position;
    if (Math.hypot(position.x - center.x, position.z - center.z) < AREA_TRIGGER_MIN_OFFSET) {
        return {};
    }
    return groundPlacement(sceneId, position);
}

// 任务区域追踪：区域中心，以及区域里推进任务的触发区（有的话），走完中心再去触发区。
function convertMissionArea(track) {
    const area = missionArea(track.sceneId, track.missionAreaId);
    const result = {type: "MissionAreaTrackingInfo", ...groundPlacement(track.sceneId, area?.shape?.position)};
    const triggerPoint = areaTriggerPoint(track.sceneId, area);
    if (triggerPoint.zone_id) {
        result.trigger_point = triggerPoint;
    }
    return result;
}

function proxyOwnDialogs(proxyId) {
    return (proxyDialogData[proxyId] ?? []).map((entry) => entry?.dialogId).filter(Boolean);
}

function proxyDialogs(proxyId) {
    return [...new Set([...proxyOwnDialogs(proxyId), ...(dialogsByProxy.get(proxyId) ?? [])])];
}

function npcNameOf(proxyId, dialogs) {
    const row = proxyById.get(proxyId);
    if (row?.ifOverrideNpcName) {
        const name = namedText(row.overrideNpcNameId?.key);
        if (name) {
            return name;
        }
    }
    const nameId = proxyInfoData[proxyId]?.npcNameId;
    const name = namedText(npcTemplateGroup[nameId]?.name);
    if (name) {
        return name;
    }
    for (const dialogId of dialogs) {
        for (const actorId of actorsByDialog.get(dialogId) ?? []) {
            const actorName = namedText(npcTemplateGroup[actorId]?.name);
            if (actorName) {
                return actorName;
            }
        }
    }
    return null;
}

function npcNameOverridesOf(proxyId, npcName) {
    const names = [];
    for (const key of [...(proxyNameOverrides.get(proxyId) ?? [])].sort()) {
        if (key === npcName?.key) {
            continue;
        }
        const name = namedText(key);
        if (name) {
            names.push(name);
        }
    }
    return names;
}

function interactTextOf(proxyId, dialogs) {
    const row = proxyById.get(proxyId);
    if (row?.overrideDefaultInteractText) {
        const text = namedText(row.defaultInteractText?.key);
        if (text) {
            return text;
        }
    }
    for (const dialogId of dialogs) {
        const text = namedText(dialogBrief[dialogId]?.interactText?.key);
        if (text) {
            return text;
        }
    }
    return null;
}

function pointDistance(a, b) {
    return a.zone_id === b.zone_id ? Math.hypot(a.x - b.x, a.y - b.y) : Infinity;
}

// NPC 可能在的位置，按这一步最可能的顺序排：关卡脚本让 NPC 走巡逻路线都发生在交互之后，
// 任务前面的步骤追踪过这个 NPC 时巡逻终点在前，否则初始位置在前；多个终点按离下一步追踪点由近到远。
// 没被脚本领着走过的 NPC 返回空，只用初始位置。离得很近的点不在这里去掉，交给运行时按寻路精度筛。
function npcProxyPoints(proxyId, origin, context) {
    const patrolEnds = proxyPatrolEnds.get(proxyId) ?? [];
    if (patrolEnds.length === 0 || !origin.zone_id) {
        return [];
    }
    const next = context?.next;
    const sorted = next
        ? patrolEnds
              .map((point, index) => ({point, index, distance: pointDistance(point, next)}))
              .sort((a, b) => a.distance - b.distance || a.index - b.index)
              .map((item) => item.point)
        : patrolEnds;
    const ordered = context?.seenProxies?.has(proxyId) ? [...sorted, origin] : [origin, ...sorted];
    const seen = new Set();
    const points = ordered.filter((point) => {
        const key = `${point.zone_id},${point.x},${point.y},${point.height}`;
        if (seen.has(key)) {
            return false;
        }
        seen.add(key);
        return true;
    });
    // 终点都和初始位置重合时只剩一个点，和不写一样。
    return points.length > 1 ? points : [];
}

function convertNpcProxy(track, context) {
    const proxyId = track.npcProxyId ?? "";
    const row = proxyById.get(proxyId);
    const dialogs = proxyDialogs(proxyId);
    const npcName = npcNameOf(proxyId, dialogs);
    const interactText = interactTextOf(proxyId, dialogs);
    const origin = groundPlacement(row?.levelId || track.sceneId, row?.position);
    const result = {
        type: "NpcProxyTrackingInfo",
        ...origin,
    };
    const points = npcProxyPoints(proxyId, origin, context);
    if (points.length > 0) {
        result.points = points;
    }
    if (npcName) {
        result.npcName = npcName;
    }
    const npcNameOverrides = npcNameOverridesOf(proxyId, npcName);
    if (npcNameOverrides.length > 0) {
        result.npcNameOverrides = npcNameOverrides;
    }
    if (interactText) {
        result.interactText = interactText;
    }
    return result;
}

// 对话 -> 挂着这段对话的 NPC 代理。
const proxiesByDialog = new Map();
for (const proxyId of new Set([...Object.keys(proxyDialogData), ...dialogsByProxy.keys()])) {
    for (const dialogId of proxyDialogs(proxyId)) {
        if (!proxiesByDialog.has(dialogId)) {
            proxiesByDialog.set(dialogId, new Set());
        }
        proxiesByDialog.get(dialogId).add(proxyId);
    }
}

// 坐标追踪的完成条件是和某个 NPC 对话、且这段对话不由关卡脚本开始时，带上这个 NPC 的名字。
// 能对应到多个名字不同的 NPC 时不写，避免找错人。
function convertPos(track, condition) {
    const result = {type: "PosTrackingInfo", ...groundPlacement(track.sceneId, track.trackingPos)};
    const dialogId = condition?._dialogId?.constValue;
    if (shortType(condition?.$type) !== "CheckTalkOptionFinish" || !dialogId || scriptStartedDialogs.has(dialogId)) {
        return result;
    }
    const named = [...(proxiesByDialog.get(dialogId) ?? [])]
        .sort()
        .map((proxyId) => ({proxyId, npcName: npcNameOf(proxyId, proxyDialogs(proxyId))}))
        .filter((entry) => entry.npcName);
    if (named.length === 0 || new Set(named.map((entry) => entry.npcName.key)).size !== 1) {
        return result;
    }
    const {proxyId, npcName} = named[0];
    result.npcName = npcName;
    const npcNameOverrides = npcNameOverridesOf(proxyId, npcName);
    if (npcNameOverrides.length > 0) {
        result.npcNameOverrides = npcNameOverrides;
    }
    return result;
}

function convertEntity(track) {
    const entity = resolveEntity(track);
    const components = entityComponents(entity?.entityDataIdKey);
    const result = {type: "EntityTrackingInfo"};
    if (entity?.entityDataIdKey) {
        result.entityDataIdKey = entity.entityDataIdKey;
    }
    if (components.length > 0) {
        result.components = components;
    }
    Object.assign(result, placement(track.sceneId, entity?.position));
    const interactTexts = interactTextsOf(entity, track.sceneId);
    if (interactTexts.length > 0) {
        result.interactTexts = interactTexts;
    }
    return result;
}

function convertTrack(track, condition, context) {
    const type = shortType(track?.$type);
    switch (type) {
        case "SnsTrackingInfo":
            return {type};
        case "JumpToUITrackingInfo": {
            const phaseId = systemJumpTable[track.jumpId]?.phaseId;
            return phaseId ? {type, phase_id: phaseId} : {type};
        }
        case "PosTrackingInfo":
            return convertPos(track, condition);
        case "MissionAreaTrackingInfo":
            return convertMissionArea(track);
        case "NpcProxyTrackingInfo":
            return convertNpcProxy(track, context);
        case "EntityTrackingInfo":
            return convertEntity(track);
        default:
            return null;
    }
}

// 这一步要不要用扫描：完成条件就是扫描，或说明里写了要扫描。
// 扫描的判定多数写在关卡脚本或服务器里，数据里只有说明能稳定看出来；多开一次扫描没有副作用。
function needsScan(objective) {
    if (shortType(objective?.condition?.$type) === "CheckScanInteractive") {
        return true;
    }
    const description = objectiveText(objective?.description?.key);
    return /[扫掃]描/.test(`${description.zh_cn}${description.zh_tw}`);
}

// 完成条件是拍照识别时，给追踪带上 snapshot，各类型走到位置后进拍照；要扫描时带上 scan，走到位置后打开扫描。
function convertTracks(tracks, condition, context, scan = false) {
    const snapshot = shortType(condition?.$type) === "CheckSnapshotIdentifySuccess";
    return (tracks ?? [])
        .map((track) => convertTrack(track, condition, context))
        .filter(Boolean)
        .map((track) => (snapshot ? {...track, snapshot: true} : track))
        .map((track) => (scan ? {...track, scan: true} : track));
}

function convertCondition(condition) {
    const type = shortType(condition?.$type);
    if (!type) {
        return {};
    }
    const result = {type};
    const dialogId = condition._dialogId?.constValue;
    if (type === "CheckTalkOptionFinish" && dialogId) {
        result.dialog_id = dialogId;
    }
    const enemies = conditionEnemies(type, condition);
    if (enemies.length > 0) {
        result.enemies = enemies;
    }
    const area = conditionArea(type, condition);
    if (area.zone_id) {
        result.area = area;
    }
    return result;
}

// 到达条件的区域中心坐标；找不到区域或没有底图时为空。
function conditionArea(type, condition) {
    const sceneId = condition?._mapId?.constValue;
    const areaId = condition?._areaId?.constValue;
    if (type !== "ReachDestination" || !sceneId || !areaId) {
        return {};
    }
    return groundPlacement(sceneId, missionArea(sceneId, areaId)?.shape?.position);
}

// 击杀条件里的怪物坐标，顺序同原始列表；找不到或没有底图的怪不写。
function conditionEnemies(type, condition) {
    const sceneId = condition?._sceneId?.constValue;
    if (!sceneId || (type !== "CheckScriptMonsterKilled" && type !== "CheckMonsterKilled")) {
        return [];
    }
    const num = levelNum(sceneId);
    let entities = [];
    if (type === "CheckScriptMonsterKilled") {
        const scriptId = condition._scriptId?.constValue?.scriptId;
        if (scriptId != null && num != null) {
            const globalId = scriptId >= 1e8 ? scriptId : num * 1e8 + scriptId;
            entities = (condition._slotIds?.constValue ?? []).map((slotId) => scriptEntity(sceneId, globalId, slotId));
        }
    } else if (type === "CheckMonsterKilled" && num != null) {
        entities = (condition._enemyIds?.constValue ?? [])
            .filter((enemy) => !enemy?.useSlotId)
            .map((enemy) => {
                const logicId = enemy?.logicId;
                return levelEntities(sceneId).get(logicId >= 1e8 ? logicId : num * 1e8 + logicId);
            });
    }
    return entities.map((entity) => placement(sceneId, entity?.position)).filter((pos) => pos.zone_id);
}

function convertObjective(objective, context) {
    const result = {
        description: objectiveText(objective?.description?.key),
    };
    if (objective?.useMultipleDescription) {
        result.useMultipleDescription = true;
        result.multipleDescription = (objective.multipleDescription ?? []).map((item) => objectiveText(item?.key));
    }
    const scan = needsScan(objective);
    result.trackingInfoList = convertTracks(objective?.trackingInfoList, objective?.condition, context, scan);
    result.multiDescTrackingInfoList = (objective?.multiDescTrackingInfoList ?? []).map((block) => ({
        actualList: convertTracks(block?.actualList, objective?.condition, context, scan),
    }));
    result.condition = convertCondition(objective?.condition);
    return result;
}

function convertQuest(quest, context) {
    return {
        questId: quest.questId ?? "",
        prevQuestIdList: Array.isArray(quest.prevQuestIdList) ? quest.prevQuestIdList : [],
        flowIndex: quest.flowIndex ?? 0,
        objectiveList: (quest.objectiveList ?? []).map((objective) => convertObjective(objective, context)),
    };
}

function questTracks(quest) {
    return (quest?.objectiveList ?? []).flatMap((objective) => [
        ...(objective?.trackingInfoList ?? []),
        ...(objective?.multiDescTrackingInfoList ?? []).flatMap((block) => block?.actualList ?? []),
    ]);
}

// 每一步的上下文：前面的步骤（沿 prevQuestIdList 追溯）追踪过的 NPC，以及下一步第一个有坐标的追踪点。
function questContexts(mission) {
    const quests = mission.questDic ?? {};
    const mainPath = Array.isArray(mission.mainPathQuests) ? mission.mainPathQuests : [];
    const proxiesOf = (quest) => questTracks(quest).map((track) => track?.npcProxyId).filter(Boolean);

    const ancestorCache = new Map();
    const ancestorProxies = (questId, visiting = new Set()) => {
        if (ancestorCache.has(questId)) {
            return ancestorCache.get(questId);
        }
        const result = new Set();
        if (visiting.has(questId)) {
            return result;
        }
        visiting.add(questId);
        for (const prevId of quests[questId]?.prevQuestIdList ?? []) {
            for (const proxyId of proxiesOf(quests[prevId])) {
                result.add(proxyId);
            }
            for (const proxyId of ancestorProxies(prevId, visiting)) {
                result.add(proxyId);
            }
        }
        ancestorCache.set(questId, result);
        return result;
    };

    const pathIndex = (questId) => {
        const index = mainPath.indexOf(questId);
        return index < 0 ? Infinity : index;
    };
    const nextPoint = (questId) => {
        const nextIds = Object.keys(quests)
            .filter((id) => (quests[id]?.prevQuestIdList ?? []).includes(questId))
            .sort((a, b) => pathIndex(a) - pathIndex(b));
        for (const id of nextIds) {
            for (const objective of quests[id]?.objectiveList ?? []) {
                const point = convertTracks(objective?.trackingInfoList, objective?.condition).find(
                    (track) => track.zone_id && typeof track.x === "number",
                );
                if (point) {
                    return point;
                }
            }
        }
        return null;
    };

    const contexts = new Map();
    for (const questId of Object.keys(quests)) {
        contexts.set(questId, {seenProxies: ancestorProxies(questId), next: nextPoint(questId)});
    }
    return contexts;
}

const missions = {};
for (const name of readdirSync(missionDir)) {
    if (!name.endsWith(".json") || name.endsWith("_meta.json")) {
        continue;
    }
    const mission = readJson(resolve(missionDir, name));
    const missionId = mission.missionId ?? name.slice(0, -".json".length);
    const contexts = questContexts(mission);
    const questDic = {};
    for (const [questId, quest] of Object.entries(mission.questDic ?? {})) {
        questDic[questId] = convertQuest(quest, contexts.get(questId));
    }
    missions[missionId] = {
        missionId,
        baseMissionImportance: mission.baseMissionImportance,
        missionName: localizedText(mission.missionName?.key),
        missionDescription: localizedText(mission.missionDescription?.key),
        mainPathQuests: Array.isArray(mission.mainPathQuests) ? mission.mainPathQuests : [],
        questDic,
    };
}

const ordered = {};
for (const missionId of Object.keys(missions).sort()) {
    ordered[missionId] = missions[missionId];
}

mkdirSync(dirname(outputPath), {recursive: true});
writeFileSync(outputPath, gzipSync(JSON.stringify(ordered), {level: 9}));
rmSync(legacyOutputPath, {force: true});
console.log(`[AutoMission] 已生成 ${outputPath}（${Object.keys(ordered).length} 个任务）`);
if (pretty) {
    writeFileSync(prettyOutputPath, `${JSON.stringify(ordered, null, 4)}\n`, "utf8");
    console.log(`[AutoMission] 已生成 ${prettyOutputPath}`);
}
