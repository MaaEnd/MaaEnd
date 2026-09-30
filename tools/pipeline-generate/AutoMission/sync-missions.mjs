import {mkdirSync, readdirSync, readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";

const currentDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(currentDir, "../../..");
const endfieldDataRoot = process.env.ENDFIELD_DATA ?? resolve(repoRoot, "../EndfieldData");
const missionDir = resolve(endfieldDataRoot, "Json/MissionRuntimeAsset");
const outputPath = resolve(repoRoot, "assets/data/AutoMission/missions.json");

const textTableRaw = readFileSync(resolve(endfieldDataRoot, "TableCfg/TextTable.json"), "utf8").replace(
    /"id":\s*(-?\d+)/g,
    '"id":"$1"',
);
const textTable = JSON.parse(textTableRaw);
const i18nCn = JSON.parse(readFileSync(resolve(endfieldDataRoot, "TableCfg/I18nTextTable_CN.json"), "utf8"));
const i18nTc = JSON.parse(readFileSync(resolve(endfieldDataRoot, "TableCfg/I18nTextTable_TC.json"), "utf8"));

function localizedText(key) {
    const node = key ? textTable[key] : undefined;
    const id = node && node.id != null ? String(node.id) : "";
    return {
        key: key ?? "",
        zh_cn: id ? (i18nCn[id] ?? "") : "",
        zh_tw: id ? (i18nTc[id] ?? "") : "",
    };
}

function emptyObjective(objective) {
    const description = objective?.description;
    const key = description && typeof description === "object" ? description.key : "";
    return {
        description: {
            key: key ?? "",
        },
        trackingInfoList: [],
        multiDescTrackingInfoList: [],
        condition: {},
    };
}

function extractQuest(quest) {
    return {
        questId: quest.questId ?? "",
        prevQuestIdList: Array.isArray(quest.prevQuestIdList) ? quest.prevQuestIdList : [],
        flowIndex: quest.flowIndex ?? 0,
        objectiveList: (quest.objectiveList ?? []).map(emptyObjective),
    };
}

const missions = {};
for (const name of readdirSync(missionDir)) {
    if (!name.endsWith(".json") || name.endsWith("_meta.json")) {
        continue;
    }
    const mission = JSON.parse(readFileSync(resolve(missionDir, name), "utf8"));
    const missionId = mission.missionId ?? name.slice(0, -".json".length);
    const questDic = {};
    for (const [questId, quest] of Object.entries(mission.questDic ?? {})) {
        questDic[questId] = extractQuest(quest);
    }
    missions[missionId] = {
        missionId,
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
