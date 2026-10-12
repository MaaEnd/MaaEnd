import {existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";

const currentDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(currentDir, "../../..");
const endfieldDataRoot = process.env.ENDFIELD_DATA ?? resolve(repoRoot, "../EndfieldData");
const jsonRoot = resolve(endfieldDataRoot, "Json");
const missionDir = resolve(jsonRoot, "MissionRuntimeAsset");
const levelDataDir = resolve(jsonRoot, "LevelData");
const levelScriptDir = resolve(jsonRoot, "LevelScriptData");
const outputPath = resolve(repoRoot, "assets/data/AutoMission/missions.json");

// 与 BaseNav zone 表一致：u = sx * x + tx，v = -sy * z + ty。
const MAP_PLANES = {
    map01: {zone: "ValleyIV_Base", sx: 0.703125, tx: 720, sy: 0.703125, ty: 630},
    map02: {zone: "Wuling_Base", sx: 0.75, tx: 1536, sy: 0.75, ty: 1344},
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
const AUTO_TRIGGER_ACTIONS = new Set(["StartDialogAction", "StartLevelCustomPerformance", "NpcProxyPatrolStart"]);

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
function levelEntities(sceneId) {
    if (levelEntityCache.has(sceneId)) {
        return levelEntityCache.get(sceneId);
    }
    const entities = new Map();
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
        }
    }
    levelEntityCache.set(sceneId, entities);
    return entities;
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
    if (brief) {
        return {entityDataIdKey: brief.detailId, position: brief.position};
    }
    return levelEntities(track.sceneId).get(globalId) ?? null;
}

const componentCache = new Map();
function entityComponents(dataKey) {
    if (!dataKey) {
        return [];
    }
    if (componentCache.has(dataKey)) {
        return componentCache.get(dataKey);
    }
    const templateId = interactiveTable.interactiveDataDict?.[dataKey]?.templateId ?? dataKey;
    const templatePath = interactiveTable.coreTemplatePathDict?.[templateId];
    let components = [];
    if (templatePath) {
        const path = resolve(jsonRoot, templatePath.replace(/^Data\/Json\//, ""));
        if (existsSync(path)) {
            components = (readJson(path).componentList ?? [])
                .map((component) => shortType(component?.$type))
                .filter((name) => name && !GENERIC_COMPONENTS.has(name));
            components = [...new Set(components)];
        }
    }
    componentCache.set(dataKey, components);
    return components;
}

// 世界坐标换算到 MapLocator 底图坐标；只有四号谷地和武陵大地图有底图。
function placement(sceneId, position) {
    const plane = MAP_PLANES[String(sceneId ?? "").split("_")[0]];
    if (!plane || !position || typeof position.x !== "number" || typeof position.z !== "number") {
        return {};
    }
    const round = (value) => Math.round(value * 1000) / 1000;
    return {
        zone_id: plane.zone,
        x: round(plane.sx * position.x + plane.tx),
        y: round(-plane.sy * position.z + plane.ty),
    };
}

function scriptNodeMap(dataMap) {
    const nodes = new Map();
    for (const key of ["headerList", "actionList"]) {
        for (const node of dataMap?.[key] ?? []) {
            if (node?._ID != null) {
                nodes.set(node._ID, node);
            }
        }
    }
    return nodes;
}

// 从玩家进入触发区事件出发，沿 _nextID / Split._idList 查找会自动推进剧情的动作。
function hasAutoTrigger(dataMap) {
    const nodes = scriptNodeMap(dataMap);
    const queue = [];
    for (const header of dataMap?.headerList ?? []) {
        if (shortType(header?.$type) === "OnLeaderEnterTriggerVolume") {
            queue.push(header._nextID);
        }
    }
    const visited = new Set();
    while (queue.length > 0) {
        const id = queue.shift();
        if (id == null || id < 0 || visited.has(id)) {
            continue;
        }
        visited.add(id);
        const node = nodes.get(id);
        if (!node) {
            continue;
        }
        if (AUTO_TRIGGER_ACTIONS.has(shortType(node.$type))) {
            return true;
        }
        queue.push(node._nextID, ...(Array.isArray(node._idList) ? node._idList : []));
    }
    return false;
}

function referencedProxies(dataMap) {
    const proxies = new Set();
    for (const key of ["getterList", "actionList", "headerList"]) {
        for (const node of dataMap?.[key] ?? []) {
            const proxyId = node?._targetProxy?.constValue;
            if (typeof proxyId === "string" && proxyId) {
                proxies.add(proxyId);
            }
        }
    }
    return proxies;
}

const autoTriggerProxies = new Set();
if (existsSync(levelScriptDir)) {
    for (const sceneId of readdirSync(levelScriptDir)) {
        const dir = resolve(levelScriptDir, sceneId);
        for (const name of readdirSync(dir)) {
            if (!name.endsWith(".json")) {
                continue;
            }
            const dataMap = readJson(resolve(dir, name)).actionMap?.dataMap;
            const proxies = referencedProxies(dataMap);
            if (proxies.size === 0 || !hasAutoTrigger(dataMap)) {
                continue;
            }
            for (const proxyId of proxies) {
                autoTriggerProxies.add(proxyId);
            }
        }
    }
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

// 代理自身挂了对话：靠近后按键交互。否则看关卡脚本是否在进入触发区时自动推进。
function proxyAction(proxyId, npcName, interactText) {
    if (proxyOwnDialogs(proxyId).length > 0) {
        return "interact";
    }
    if (autoTriggerProxies.has(proxyId)) {
        return "wait";
    }
    if (npcName || interactText) {
        return "interact";
    }
    return "unknown";
}

const actionStats = {interact: 0, wait: 0, unknown: 0};
const unknownSamples = [];

function convertNpcProxy(track, missionId) {
    const proxyId = track.npcProxyId ?? "";
    const row = proxyById.get(proxyId);
    const dialogs = proxyDialogs(proxyId);
    const npcName = npcNameOf(proxyId, dialogs);
    const interactText = interactTextOf(proxyId, dialogs);
    const action = proxyAction(proxyId, npcName, interactText);
    actionStats[action] += 1;
    if (action === "unknown" && unknownSamples.length < 10) {
        unknownSamples.push(`${missionId} ${proxyId}`);
    }
    const result = {
        type: "NpcProxyTrackingInfo",
        ...placement(row?.levelId || track.sceneId, row?.position),
        action,
    };
    if (npcName) {
        result.npcName = npcName;
    }
    if (interactText) {
        result.interactText = interactText;
    }
    return result;
}

function convertEntity(track) {
    const entity = resolveEntity(track);
    const components = entityComponents(entity?.entityDataIdKey);
    const result = {type: "EntityTrackingInfo"};
    if (components.length > 0) {
        result.components = components;
    }
    return {...result, ...placement(track.sceneId, entity?.position)};
}

function convertTrack(track, missionId) {
    const type = shortType(track?.$type);
    switch (type) {
        case "SnsTrackingInfo":
            return {type};
        case "JumpToUITrackingInfo": {
            const phaseId = systemJumpTable[track.jumpId]?.phaseId;
            return phaseId ? {type, phase_id: phaseId} : {type};
        }
        case "PosTrackingInfo":
            return {type, ...placement(track.sceneId, track.trackingPos)};
        case "MissionAreaTrackingInfo":
            return {type, ...placement(track.sceneId, missionArea(track.sceneId, track.missionAreaId)?.shape?.position)};
        case "NpcProxyTrackingInfo":
            return convertNpcProxy(track, missionId);
        case "EntityTrackingInfo":
            return convertEntity(track);
        default:
            return null;
    }
}

function convertTracks(tracks, missionId) {
    return (tracks ?? []).map((track) => convertTrack(track, missionId)).filter(Boolean);
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
    return result;
}

function convertObjective(objective, missionId) {
    const result = {
        description: objectiveText(objective?.description?.key),
    };
    if (objective?.useMultipleDescription) {
        result.useMultipleDescription = true;
        result.multipleDescription = (objective.multipleDescription ?? []).map((item) => objectiveText(item?.key));
    }
    result.trackingInfoList = convertTracks(objective?.trackingInfoList, missionId);
    result.multiDescTrackingInfoList = (objective?.multiDescTrackingInfoList ?? []).map((block) => ({
        actualList: convertTracks(block?.actualList, missionId),
    }));
    result.condition = convertCondition(objective?.condition);
    return result;
}

function convertQuest(quest, missionId) {
    return {
        questId: quest.questId ?? "",
        prevQuestIdList: Array.isArray(quest.prevQuestIdList) ? quest.prevQuestIdList : [],
        flowIndex: quest.flowIndex ?? 0,
        objectiveList: (quest.objectiveList ?? []).map((objective) => convertObjective(objective, missionId)),
    };
}

const missions = {};
for (const name of readdirSync(missionDir)) {
    if (!name.endsWith(".json") || name.endsWith("_meta.json")) {
        continue;
    }
    const mission = readJson(resolve(missionDir, name));
    const missionId = mission.missionId ?? name.slice(0, -".json".length);
    const questDic = {};
    for (const [questId, quest] of Object.entries(mission.questDic ?? {})) {
        questDic[questId] = convertQuest(quest, missionId);
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
writeFileSync(outputPath, `${JSON.stringify(ordered, null, 4)}\n`, "utf8");
console.log(`[AutoMission] 已生成 ${outputPath}（${Object.keys(ordered).length} 个任务）`);
console.log(
    `[AutoMission] NpcProxy action：interact ${actionStats.interact}，wait ${actionStats.wait}，unknown ${actionStats.unknown}`,
);
if (unknownSamples.length > 0) {
    console.log(`[AutoMission] unknown 样例：${unknownSamples.join("；")}`);
}
