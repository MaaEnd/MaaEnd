import {existsSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";

import {applyEdits, modify} from "jsonc-parser";
import {format, resolveConfig} from "prettier";

import {parseJsonc, readJsonc} from "../jsonc.mjs";
import {dataDir, repoRoot} from "../utils/paths.mjs";

//数据依赖
const SOURCE_PATH = resolve(dataDir, "operator_growth.json"); //干员升级素材
const COLLECT_PATH = resolve(dataDir, "collect_points.json"); //采集物列表
const OUTPUT_PATH = resolve(repoRoot, "assets/data/ProtocolSpace/operatorgrowth_detail_data.json");

const SKILL_NAMES = [
    "NormalAttack",
    "NormalSkill",
    "ComboSkill",
    "UltimateSkill",
];

//非负整数检查
function requireCount(value, label) {
    if (!Number.isSafeInteger(value) || value < 0) {
        throw new Error(`[ProtocolSpace] ${label} 必须是非负安全整数`);
    }
    return value;
}

//排除采集物和存续的痕迹
function materialCosts(items, excludedItems, gold) {
    const result = {};
    for (const [
        id,
        count,
    ] of Object.entries(items)) {
        requireCount(count, id);
        if (!excludedItems.has(id)) result[id] = count;
    }
    //operator_growth.json有额外的gold数据，此处置换为统一ID item_gold
    if (gold !== undefined) {
        result.item_gold = requireCount((result.item_gold || 0) + requireCount(gold, "gold"), "item_gold");
    }
    return result;
}

// 精英突破使用奇数阶段，装备突破使用偶数阶段，按培养顺序合并为 1～7。
function breakCosts(nodes, equipNodes, excludedItems) {
    const result = {};
    for (const node of nodes) {
        const stage = requireCount(node.break_stage, "break_stage");
        result[stage * 2 - 1] = materialCosts(node.items, excludedItems);
    }
    for (const node of equipNodes) {
        const stage = requireCount(node.break_stage, "equip_break_stage");
        result[stage * 2] = materialCosts(node.items, excludedItems);
    }
    return result;
}

// 汇总所有天赋节点的材料，以 1 为索引。
function talentCosts(nodes, excludedItems) {
    const total = {};
    for (const node of nodes) {
        for (const [
            id,
            count,
        ] of Object.entries(materialCosts(node.items, excludedItems))) {
            total[id] = requireCount((total[id] ?? 0) + count, id);
        }
    }
    return {1: total};
}

// 以当前等级为键，生成升到下一级的经验与折金票消耗。
function buildLevelUpCosts(levelUpSource) {
    const levelUp = {90: {}};
    for (let level = 1; level < 90; level++) {
        const row = levelUpSource?.[level];
        if (!row) throw new Error(`[ProtocolSpace] 缺少等级 ${level} 的升级数据`);
        // 升到 60 级为止使用作战记录，60 → 61 起使用认知载体；单位均为经验值。
        const expType = level < 60 ? "COMBAT_RECORD_EXP" : "COGNITIVE_CARRIER_EXP";
        levelUp[level] = {
            [expType]: requireCount(row.exp, `level ${level} exp`),
            item_gold: requireCount(row.gold, `level ${level} gold`),
        };
    }
    return levelUp;
}

function buildExcludedItems(collectData) {
    if (!collectData?.types || Object.keys(collectData.types).length === 0) {
        throw new Error("[ProtocolSpace] 采集物类型表为空，无法过滤采集素材");
    }
    // 采集物随上游采集表更新；存续的痕迹不属于采集物，需要单独排除。
    const excludedItems = new Set(["item_char_skill_crown"]);
    for (const type of Object.values(collectData.types)) {
        if (type.action === "COLLECT" || type.action === "DIG") {
            if (!type.item) throw new Error("[ProtocolSpace] 采集物类型缺少产物 ID");
            excludedItems.add(type.item);
        }
    }
    return excludedItems;
}

// 动态补齐缺失干员，保留已有培养数据；技能等级统一以当前等级为键。
function buildOperators(operatorSource, excludedItems, existingOperators) {
    if (!operatorSource || Object.keys(operatorSource).length === 0) {
        throw new Error(
            "[ProtocolSpace] operator_growth.operators 为空，请先运行 pnpm fetch:zmdmap --only operator_growth",
        );
    }
    const operators = {};
    for (const [
        id,
        operator,
    ] of Object.entries(operatorSource).sort()) {
        if (id === "chr_0002_endminm" || id === "chr_0003_endminf") continue;
        if (!/^chr_\d+_[a-z0-9]+$/.test(id)) throw new Error(`[ProtocolSpace] 非法干员 ID: ${id}`);
        if (Object.hasOwn(existingOperators, id)) continue;
        const skills = {};
        for (const name of SKILL_NAMES) {
            const groups = operator.skills.filter((skill) => skill.group_id === `${id}_${name}`);
            if (groups.length !== 1) throw new Error(`[ProtocolSpace] ${id} 的 ${name} 技能缺失或重复`);
            const levels = {12: {}};
            for (const row of groups[0].levels) {
                const level = requireCount(row.level, `${id} ${name} level`);
                levels[level - 1] = materialCosts(
                    row.items,
                    excludedItems,
                    requireCount(row.gold, `${id} ${name} gold`),
                );
            }
            skills[name] = levels;
        }
        operators[id] = {
            break_nodes: breakCosts(operator.break_nodes, operator.equip_break_nodes, excludedItems), //精英与装备突破材料
            skills, //四个干员技能
            talent_nodes: talentCosts(operator.talent_nodes, excludedItems), //天赋直接取总和，赋值给字段 1
        };
    }
    return operators;
}

function buildOperatorGrowthDetailData(source, collectData, existingOperators) {
    const excludedItems = buildExcludedItems(collectData);
    return {
        level_up: buildLevelUpCosts(source.level_up),
        operators: buildOperators(source.operators, excludedItems, existingOperators),
    };
}

// 从末级向前累加，保存各等级自身及后续等级的材料总量。
function accumulateLevelCosts(levelCosts, maxLevel) {
    const result = {};
    const total = {};
    for (let level = maxLevel; level >= 1; level--) {
        const costs = levelCosts[level];
        if (!costs) throw new Error(`[ProtocolSpace] 缺少等级 ${level} 的材料数据`);
        for (const [
            id,
            count,
        ] of Object.entries(costs)) {
            total[id] = requireCount((total[id] ?? 0) + requireCount(count, id), id);
        }
        result[level] = {...total};
    }
    return result;
}

function accumulateOperatorGrowthDetailData(operatorGrowthDetailData) {
    operatorGrowthDetailData.level_up = accumulateLevelCosts(operatorGrowthDetailData.level_up, 90);
    for (const operator of Object.values(operatorGrowthDetailData.operators)) {
        operator.break_nodes = accumulateLevelCosts(operator.break_nodes, 7);
        for (const name of SKILL_NAMES) {
            operator.skills[name] = accumulateLevelCosts(operator.skills[name], 12);
        }
    }
}

async function generateOperatorGrowthDetailData() {
    const original = existsSync(OUTPUT_PATH) ? readFileSync(OUTPUT_PATH, "utf8") : "{}";
    const existing = parseJsonc(original, OUTPUT_PATH);
    const operatorGrowthDetailData = buildOperatorGrowthDetailData(
        readJsonc(SOURCE_PATH),
        readJsonc(COLLECT_PATH),
        existing.operators ?? {},
    );
    // 仅转换本次从源数据构建的逐级消耗，避免对已有累计数据重复求和。
    accumulateOperatorGrowthDetailData(operatorGrowthDetailData);
    let text = original;
    const formattingOptions = {insertSpaces: true, tabSize: 4};
    //增量更新检查
    if (JSON.stringify(existing.level_up) !== JSON.stringify(operatorGrowthDetailData.level_up)) {
        text = applyEdits(text, modify(text, ["level_up"], operatorGrowthDetailData.level_up, {formattingOptions}));
    }
    for (const [
        id,
        operator,
    ] of Object.entries(operatorGrowthDetailData.operators)) {
        text = applyEdits(
            text,
            modify(
                text,
                [
                    "operators",
                    id,
                ],
                operator,
                {formattingOptions},
            ),
        );
    }
    if (text !== original) {
        text = await format(text, {...(await resolveConfig(OUTPUT_PATH)), filepath: OUTPUT_PATH});
        mkdirSync(dirname(OUTPUT_PATH), {recursive: true});
        writeFileSync(OUTPUT_PATH, text, "utf8");
    }
    console.log(
        `[ProtocolSpace] 新增 ${Object.keys(operatorGrowthDetailData.operators).length} 名干员，培养数据：${OUTPUT_PATH}`,
    );
}

await generateOperatorGrowthDetailData();
