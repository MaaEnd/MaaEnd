import {existsSync, readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";

import {applyEdits, modify} from "jsonc-parser";
import {format, resolveConfig} from "prettier";

import {parseJsonc} from "../jsonc.mjs";
import {getOperatorCaseName, isAdminOperator, OPERATOR_LOCALES, operatorCatalog} from "../utils/operators.mjs";

const GENERATOR_DIR = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(GENERATOR_DIR, "../../..");
const PIPELINE_DIR = resolve(REPO_ROOT, "assets/resource/pipeline/GiftOperator");
// 全量目录仅排除男女管理员，不以现有模板或旧选项作为支持名单。
function buildGiftOperators() {
    const usedNames = new Set();
    const operators = Object.values(operatorCatalog)
        .filter((operator) => !isAdminOperator(operator))
        .map((operator) => {
            const name = getOperatorCaseName(operator);
            if (!/^[A-Za-z][A-Za-z0-9]*$/.test(name) || name === "Any") {
                throw new Error(`[GiftOperator] 非法选项名：${name}`);
            }
            if (usedNames.has(name)) throw new Error(`[GiftOperator] 重复选项名：${name}`);
            usedNames.add(name);
            const template = `GiftOperator/Operators/${name}.png`;
            return {
                name,
                template,
                names: operator.names,
                expected: [...new Set(OPERATOR_LOCALES.map((locale) => operator.names[locale]))],
            };
        });
    if (operators.length === 0) throw new Error("[GiftOperator] 支持的干员列表不能为空");
    return operators;
}

function buildOperatorCase(operator) {
    return {
        name: operator.name,
        label: `$operator.${operator.name}`,
        pipeline_override: {
            GiftOperatorContacSortOrder: {enabled: false},
            GiftOperatorContacSortBy: {enabled: false},
            GiftOperatorSelectOp1: {enabled: false},
            GiftOperatorSelectSpecifiedOp: {enabled: true, template: [operator.template]},
            GiftOperatorName: {expected: operator.expected},
        },
    };
}

function buildOperatorNode(operator) {
    return {
        recognition: {
            type: "TemplateMatch",
            param: {
                roi: "GiftOperatorSelectGiftOp",
                roi_offset: [
                    -48,
                    -36,
                    165,
                    128,
                ],
                template: [operator.template],
                green_mask: true,
            },
        },
        action: {
            type: "Custom",
            param: {
                custom_action: "PipelineOverrideAction",
                custom_action_param: {patch: {GiftOperatorName: {expected: operator.expected}}},
            },
        },
        next: ["GiftOperatorSelectSpecifiedOpSuccess"],
    };
}

// 只替换生成器拥有的字段，保留其余流程以及 @i18n-skip 等 JSONC 注释。
function updateJsonc(text, path, value) {
    const current = path.reduce((entry, key) => entry?.[key], parseJsonc(text));
    if (JSON.stringify(current) === JSON.stringify(value)) return text;
    return applyEdits(
        text,
        modify(text, path, value, {
            formattingOptions: {insertSpaces: true, tabSize: 4, eol: text.includes("\r\n") ? "\r\n" : "\n"},
        }),
    );
}

async function generate() {
    const operators = buildGiftOperators();
    const outputs = new Map();
    const taskPath = resolve(REPO_ROOT, "assets/tasks/GiftOperator.json");
    const taskText = readFileSync(taskPath, "utf8");
    const anyCase = parseJsonc(taskText, taskPath).option.SelectOperator.cases.find(({name}) => name === "Any");
    if (!anyCase) throw new Error("[GiftOperator] 缺少 Any 选项，停止生成");
    outputs.set(
        taskPath,
        updateJsonc(
            taskText,
            [
                "option",
                "SelectOperator",
                "cases",
            ],
            [
                anyCase,
                ...operators.map(buildOperatorCase),
            ],
        ),
    );

    const nodes = Object.fromEntries(
        operators.map((operator) => [
            `GiftOperatorSelect_${operator.name}`,
            buildOperatorNode(operator),
        ]),
    );
    outputs.set(
        resolve(PIPELINE_DIR, "Operator/Operator.json"),
        "// 由 tools/pipeline-generate/GiftOperator/generate.mjs 生成。\n" + JSON.stringify(nodes, null, 4) + "\n",
    );

    const contactPath = resolve(PIPELINE_DIR, "GiftOperatorContact.json");
    const contactText = readFileSync(contactPath, "utf8");
    if (!parseJsonc(contactText, contactPath).GiftOperatorSelectGiftOp) {
        throw new Error("[GiftOperator] 缺少收礼干员分派节点，停止生成");
    }
    outputs.set(
        contactPath,
        updateJsonc(
            contactText,
            [
                "GiftOperatorSelectGiftOp",
                "next",
            ],
            Object.keys(nodes).sort(),
        ),
    );

    for (const [
        path,
        text,
    ] of outputs) {
        if (existsSync(path) && readFileSync(path, "utf8") === text) continue;
        outputs.set(path, await format(text, {...(await resolveConfig(path)), filepath: path}));
    }

    // 语言文件只更新干员名称，不整文件格式化，避免重排其他任务的键。
    for (const locale of OPERATOR_LOCALES) {
        const path = resolve(REPO_ROOT, `assets/locales/interface/${locale}.json`);
        let text = readFileSync(path, "utf8");
        for (const operator of operators) {
            text = updateJsonc(text, [`operator.${operator.name}`], operator.names[locale]);
        }
        outputs.set(path, text);
    }

    // 全部解析、校验和格式化完成后才写文件，避免输入错误留下半套产物。
    let changed = 0;
    for (const [
        path,
        text,
    ] of outputs) {
        if (existsSync(path) && readFileSync(path, "utf8") === text) continue;
        writeFileSync(path, text);
        changed += 1;
    }
    console.log(`[GiftOperator] 已同步 ${operators.length} 名干员，更新 ${changed} 个文件`);
}

await generate();
