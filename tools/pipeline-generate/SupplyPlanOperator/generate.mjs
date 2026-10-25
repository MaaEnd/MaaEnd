import {readFileSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";

import {applyEdits, modify} from "jsonc-parser";
import {format, resolveConfig} from "prettier";

import {parseJsonc, readJsonc} from "../jsonc.mjs";
import {dataDir, repoRoot} from "../utils/paths.mjs";

const MAX_LIMIT = 3000000000;
const ADMIN_IDS = [
    "chr_0002_endminm",
    "chr_0003_endminf",
    "chr_9000_endmin",
];
// 路线顺序与 agent/go-service/supplyplan/expression.go 的路线表一致（不含武器路线）。
const ROUTES = [
    "T_CREDS",
    "PROTOHEDRON",
    "PROTOPRISM",
    "COGNITIVE_CARRIER_EXP",
    "COMBAT_RECORD_EXP",
    "PROTOSET",
    "PROTODISK",
    "WARP_PLASMA_COATED_SLIDE",
    "TRIPHASIC_NANOFLAKE",
    "QUADRANT_FITTING_FLUID",
    "TACHYON_SCREENING_LATTICE",
    "D96_STEEL_SAMPLE_4",
    "METADIASTIMA_PHOTOEMISSION_TUBE",
];
const ITEM_ROUTE = {
    item_gold: "T_CREDS",
    item_char_skill_level_7_12: "PROTOHEDRON",
    item_char_skill_level_1_6: "PROTOPRISM",
    item_char_break_stage_3_4: "PROTOSET",
    item_char_break_stage_1_2: "PROTODISK",
    item_char_skill_specialize_6: "WARP_PLASMA_COATED_SLIDE",
    item_char_skill_specialize_5: "TRIPHASIC_NANOFLAKE",
    item_char_skill_specialize_4: "QUADRANT_FITTING_FLUID",
    item_char_skill_specialize_3: "TACHYON_SCREENING_LATTICE",
    item_char_skill_specialize_2: "D96_STEEL_SAMPLE_4",
    item_char_skill_specialize_1: "METADIASTIMA_PHOTOEMISSION_TUBE",
};
// 经验曲线在 60 级断开：1-59 级用作战记录经验，60-89 级用认知载体经验。
const COGNITIVE_FROM_LEVEL = 60;
const MAX_LEVEL = 90;

const skipped = new Set();

function add(totals, route, amount) {
    if (!amount) return;
    totals[route] = (totals[route] ?? 0) + amount;
}

function addItems(totals, items) {
    for (const [
        item,
        amount,
    ] of Object.entries(items ?? {})) {
        const route = ITEM_ROUTE[item];
        if (route) add(totals, route, amount);
        else skipped.add(item);
    }
}

function growthTotals(id, operator, levelUp) {
    const totals = {};
    for (let level = 1; level < MAX_LEVEL; level++) {
        const row = levelUp[String(level)];
        if (!row) throw new Error(`[SupplyPlanOperator] level_up 缺少等级 ${level}`);
        add(totals, level < COGNITIVE_FROM_LEVEL ? "COMBAT_RECORD_EXP" : "COGNITIVE_CARRIER_EXP", row.exp);
        add(totals, "T_CREDS", row.gold);
    }
    for (const node of [
        ...(operator.break_nodes ?? []),
        ...(operator.equip_break_nodes ?? []),
        ...(operator.talent_nodes ?? []),
    ]) {
        addItems(totals, node.items);
    }
    for (const skill of operator.skills ?? []) {
        for (const row of skill.levels ?? []) {
            addItems(totals, row.items);
            if (!row.items?.item_gold) add(totals, "T_CREDS", row.gold);
        }
    }
    const ordered = {};
    for (const route of ROUTES) {
        const value = totals[route];
        if (value === undefined || value === 0) continue;
        if (!Number.isInteger(value) || value < 0 || value > MAX_LIMIT) {
            throw new Error(`[SupplyPlanOperator] ${id} 的 ${route} 超出范围: ${value}`);
        }
        ordered[route] = value;
    }
    return ordered;
}

function pascalName(operator) {
    return operator.names.en_us
        .split(/[^a-zA-Z0-9]+/)
        .filter(Boolean)
        .map((part) => part[0].toUpperCase() + part.slice(1))
        .join("");
}

const data = readJsonc(resolve(dataDir, "operator_growth.json"));
if (!data.operators || !data.level_up) {
    throw new Error(
        "[SupplyPlanOperator] operator_growth 缺少 operators 或 level_up，请先运行 pnpm fetch:zmdmap --only operator_growth",
    );
}
const zhCn = readJsonc(resolve(repoRoot, "assets/locales/interface/zh_cn.json"));

const targets = {};
for (const [
    id,
    operator,
] of Object.entries(data.operators).sort()) {
    if (!/^chr_\d+_[a-z0-9]+$/.test(id)) throw new Error(`[SupplyPlanOperator] 非法干员 ID: ${id}`);
    const name = pascalName(operator);
    if (!name) throw new Error(`[SupplyPlanOperator] ${id} 缺少英文名`);
    if (!Object.hasOwn(zhCn, `operator.${name}`)) {
        throw new Error(`[SupplyPlanOperator] ${id} 缺少 locale key operator.${name}`);
    }
    const totals = growthTotals(id, operator, data.level_up);
    if (ADMIN_IDS.includes(id)) {
        if (name !== "Endministrator") throw new Error(`[SupplyPlanOperator] ${id} 的名称不是 Endministrator: ${name}`);
        if (Object.hasOwn(targets, name)) {
            if (JSON.stringify(targets[name]) !== JSON.stringify(totals)) {
                throw new Error(`[SupplyPlanOperator] 管理员 ${id} 的总量与其他管理员不一致`);
            }
            continue;
        }
    } else if (Object.hasOwn(targets, name)) {
        throw new Error(`[SupplyPlanOperator] ${id} 的选项名冲突: ${name}`);
    }
    targets[name] = totals;
}
const names = Object.keys(targets).sort();
if (ADMIN_IDS.some((id) => Object.hasOwn(data.operators, id)) && !names.includes("Endministrator")) {
    throw new Error("[SupplyPlanOperator] 管理员未生成");
}

async function formatJson(path, value) {
    const text = typeof value === "string" ? value : `${JSON.stringify(value, null, 4)}\n`;
    return format(text, {...(await resolveConfig(path)), filepath: path});
}

// (a) 管线节点
const nodePath = resolve(repoRoot, "assets/resource/pipeline/SupplyPlan/SupplyPlanOperator.json");
const sortedTargets = Object.fromEntries(
    names.map((name) => [
        name,
        targets[name],
    ]),
);
const nodeText = await formatJson(nodePath, {
    SupplyPlanOperatorPlan: {
        desc: "按干员规划：把勾选干员从零养满的各路线总量相加并覆盖目标库存（由 tools/pipeline-generate/SupplyPlanOperator 生成）",
        recognition: "DirectHit",
        pre_delay: 0,
        action: {
            type: "Custom",
            param: {
                custom_action: "SupplyPlanOperatorPlan",
                custom_action_param: {targets: sortedTargets},
            },
        },
        post_delay: 0,
        rate_limit: 0,
        next: ["SupplyPlanDispatch"],
    },
});

// (b) 任务选项
const taskPath = resolve(repoRoot, "assets/tasks/ProtocolSpace.json");
const original = readFileSync(taskPath, "utf8");
let text = original;
let value = parseJsonc(text, taskPath);
function edit(path, newValue, options = {}) {
    text = applyEdits(
        text,
        modify(text, path, newValue, {
            formattingOptions: {insertSpaces: true, tabSize: 4},
            ...options,
        }),
    );
    value = parseJsonc(text, taskPath);
}

const modeCases = value.option.ProtocolSpaceMode.cases;
const caseIndex = modeCases.findIndex((item) => item.name === "TargetInventory");
if (caseIndex < 0) throw new Error("[SupplyPlanOperator] ProtocolSpaceMode 缺少 TargetInventory");
const caseOptions = modeCases[caseIndex].option;
if (!caseOptions.includes("SupplyPlanOperators")) {
    const at = caseOptions.indexOf("SupplyPlanLimits");
    if (at < 0) throw new Error("[SupplyPlanOperator] TargetInventory 缺少 SupplyPlanLimits");
    edit(
        [
            "option",
            "ProtocolSpaceMode",
            "cases",
            caseIndex,
            "option",
        ],
        [
            ...caseOptions.slice(0, at),
            "SupplyPlanOperators",
            ...caseOptions.slice(at),
        ],
    );
}

const operatorOption = {
    type: "checkbox",
    label: "$option.SupplyPlanOperators.label",
    cases: names.map((name) => ({
        name,
        label: `$operator.${name}`,
        pipeline_override: {
            SupplyPlanOperatorPlan: {attach: {[name]: true}},
            SupplyPlanMain: {
                next: [
                    "[JumpBack]SyncItemData",
                    "SupplyPlanOperatorPlan",
                ],
            },
        },
    })),
};
if (Object.hasOwn(value.option, "SupplyPlanOperators")) {
    edit(
        [
            "option",
            "SupplyPlanOperators",
        ],
        operatorOption,
    );
} else {
    // 放在 SupplyPlanLimits 之前：按键序位置插入。
    const keys = Object.keys(value.option);
    edit(
        [
            "option",
            "SupplyPlanOperators",
        ],
        operatorOption,
        {
            getInsertionIndex: () => keys.indexOf("SupplyPlanLimits"),
        },
    );
}
const taskText = await formatJson(taskPath, text);

const outputs = [
    [
        nodePath,
        nodeText,
    ],
    [
        taskPath,
        taskText,
    ],
];
for (const [
    path,
    content,
] of outputs)
    writeFileSync(path, content, "utf8");
console.log(`[SupplyPlanOperator] 生成 ${names.length} 名干员：${names.join(", ")}`);
console.log(`[SupplyPlanOperator] 跳过协议空间刷不到的物品：${[...skipped].sort().join(", ")}`);
