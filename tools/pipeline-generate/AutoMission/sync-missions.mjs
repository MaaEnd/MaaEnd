import {existsSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
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

const proxyTable = JSON.parse(
    readFileSync(resolve(endfieldDataRoot, "Json/GameplayConfig/NpcProxyTable.json"), "utf8"),
);
const proxyById = new Map();
for (const row of Object.values(proxyTable.dataTable ?? {})) {
    if (row && typeof row.proxyId === "string" && row.proxyId) {
        proxyById.set(row.proxyId, row);
    }
}
const proxyEx = JSON.parse(
    readFileSync(resolve(endfieldDataRoot, "Json/GameplayConfig/NpcProxyExDataTable.json"), "utf8"),
);
const proxyInfoData = proxyEx.proxyInfoData ?? {};
const npcTemplateGroup = JSON.parse(
    readFileSync(resolve(endfieldDataRoot, "TableCfg/NpcTemplateGroupTable.json"), "utf8"),
);

// 改写名优先；否则用模板组 name。简繁都空时不写 npcName。
function npcNameForProxy(proxyId) {
    const row = proxyById.get(proxyId);
    const overrideKey = row?.ifOverrideNpcName ? (row.overrideNpcNameId?.key ?? "") : "";
    const nameId = proxyInfoData[proxyId]?.npcNameId ?? "";
    const key = overrideKey || npcTemplateGroup[nameId]?.name || "";
    if (!key) {
        return null;
    }
    const text = localizedText(key);
    if (!text.zh_cn && !text.zh_tw) {
        return null;
    }
    return text;
}

function attachList(dstTracks, srcTracks, stats) {
    if (!Array.isArray(dstTracks) || !Array.isArray(srcTracks) || dstTracks.length !== srcTracks.length) {
        stats.mismatch += 1;
        return;
    }
    for (let i = 0; i < dstTracks.length; i++) {
        const dst = dstTracks[i];
        if (dst?.type !== "NpcProxyTrackingInfo") {
            continue;
        }
        stats.npc += 1;
        const name = npcNameForProxy(srcTracks[i]?.npcProxyId);
        if (!name) {
            delete dst.npcName;
            stats.empty += 1;
            continue;
        }
        dst.npcName = name;
        stats.written += 1;
    }
}

function attachObjective(dstObjective, srcObjective, stats) {
    attachList(dstObjective?.trackingInfoList, srcObjective?.trackingInfoList, stats);
    const dstMulti = dstObjective?.multiDescTrackingInfoList ?? [];
    const srcMulti = srcObjective?.multiDescTrackingInfoList ?? [];
    if (dstMulti.length !== srcMulti.length) {
        stats.mismatch += 1;
        return;
    }
    for (let i = 0; i < dstMulti.length; i++) {
        attachList(dstMulti[i]?.actualList, srcMulti[i]?.actualList, stats);
    }
}

// 坐标已在 missions.json 里，这里只按解包追踪下标补 NpcProxy 的名字。
function attachNpcNames(missions) {
    const stats = {npc: 0, written: 0, empty: 0, mismatch: 0, missingMission: 0};
    for (const [missionId, mission] of Object.entries(missions)) {
        const sourcePath = resolve(missionDir, `${mission.missionId || missionId}.json`);
        if (!existsSync(sourcePath)) {
            stats.missingMission += 1;
            continue;
        }
        const source = JSON.parse(readFileSync(sourcePath, "utf8"));
        for (const [questId, quest] of Object.entries(mission.questDic ?? {})) {
            const srcQuest = source.questDic?.[questId];
            if (!srcQuest) {
                continue;
            }
            const dstObjectives = quest.objectiveList ?? [];
            const srcObjectives = srcQuest.objectiveList ?? [];
            if (dstObjectives.length !== srcObjectives.length) {
                stats.mismatch += 1;
                continue;
            }
            for (let i = 0; i < dstObjectives.length; i++) {
                attachObjective(dstObjectives[i], srcObjectives[i], stats);
            }
        }
    }
    return stats;
}

if (!existsSync(outputPath)) {
    throw new Error(`[AutoMission] 缺少已有任务数据：${outputPath}`);
}
const missions = JSON.parse(readFileSync(outputPath, "utf8"));
const stats = attachNpcNames(missions);
mkdirSync(dirname(outputPath), {recursive: true});
writeFileSync(outputPath, `${JSON.stringify(missions, null, 4)}\n`, "utf8");
console.log(
    `[AutoMission] 已写入 npcName：${stats.written}/${stats.npc}，无名字 ${stats.empty}，下标不一致 ${stats.mismatch}`,
);
