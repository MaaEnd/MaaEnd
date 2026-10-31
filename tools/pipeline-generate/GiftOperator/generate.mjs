import {spawnSync} from "node:child_process";
import {existsSync, readFileSync, writeFileSync} from "node:fs";
import {resolve} from "node:path";

import {applyEdits, modify} from "jsonc-parser";
import {format, resolveConfig} from "prettier";

import {parseJsonc, readJsonc} from "../jsonc.mjs";
import {dataDir, repoRoot} from "../utils/paths.mjs";

const locales = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];
const documents = [];

function escapeRegex(value) {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function openDocument(relativePath) {
    const path = resolve(repoRoot, relativePath);
    const original = readFileSync(path, "utf8");
    const document = {path, original, text: original, value: parseJsonc(original, path)};
    documents.push(document);
    return document;
}

// 只插入缺失项，保留既有配置、注释和排列顺序；整份基线缺失时从 Git 恢复。
function insert(document, path, value) {
    document.text = applyEdits(
        document.text,
        modify(document.text, path, value, {
            formattingOptions: {insertSpaces: true, tabSize: 4},
        }),
    );
    document.value = parseJsonc(document.text, document.path);
}

const data = readJsonc(resolve(dataDir, "sell_product.json"));
if (!data.operators || Object.keys(data.operators).length === 0) {
    throw new Error("[GiftOperator] sell_product.operators 缺失，请先运行 pnpm fetch:zmdmap --only sell_product");
}
const task = openDocument("assets/tasks/GiftOperator.json");
const nodes = openDocument("assets/resource/pipeline/GiftOperator/Operator/Operator.json");
const send = openDocument("assets/resource/pipeline/GiftOperator/Operator/GiftOperatorSend.json");
const contact = openDocument("assets/resource/pipeline/GiftOperator/GiftOperatorContact.json");
const messages = Object.fromEntries(
    locales.map((locale) => [
        locale,
        openDocument(`assets/locales/interface/${locale}.json`),
    ]),
);
const avatars = [];
const usedCases = new Set();
let added = 0;

for (const [
    id,
    operator,
] of Object.entries(data.operators).sort()) {
    if (id === "chr_0002_endminm" || id === "chr_0003_endminf") continue;
    if (!/^chr_\d+_[a-z0-9]+$/.test(id) || operator.id !== id) {
        throw new Error(`[GiftOperator] 非法干员 ID: ${id}`);
    }
    const names = locales.map((locale) => operator.names?.[locale]);
    if (names.some((name) => typeof name !== "string" || !name.trim())) {
        throw new Error(`[GiftOperator] ${id} 缺少五语名称`);
    }
    const expected = names.map(escapeRegex);
    const knownNames = new Set([
        ...names,
        ...expected,
    ]);

    // 通过现有名称找到基线，不维护另一份干员 ID 绑定表。
    const matches = task.value.option.SelectOperator.cases.filter(
        (item) =>
            item.name !== "Any" &&
            item.name !== "ALL" &&
            item.pipeline_override.GiftOperatorName.expected.some((name) => knownNames.has(name)),
    );
    if (matches.length > 1) throw new Error(`[GiftOperator] ${id} 对应多个现有选项`);
    let entry = matches[0];
    const name =
        entry?.name ||
        operator.names.en_us
            .split(/[^a-zA-Z0-9]+/)
            .filter(Boolean)
            .map((part) => part[0].toUpperCase() + part.slice(1))
            .join("");
    if (
        !name ||
        usedCases.has(name) ||
        (!entry && task.value.option.SelectOperator.cases.some((item) => item.name === name))
    ) {
        throw new Error(`[GiftOperator] ${id} 的选项名为空或冲突: ${name}`);
    }
    usedCases.add(name);
    if (!entry) {
        entry = {
            name,
            label: `$operator.${name}`,
            pipeline_override: {
                GiftOperatorContacSortOrder: {enabled: false},
                GiftOperatorContacSortBy: {enabled: false},
                GiftOperatorSelectOp1: {enabled: false},
                GiftOperatorSelectSpecifiedOp: {enabled: true, template: [`GiftOperator/Operators/${name}.png`]},
                GiftOperatorName: {expected},
            },
        };
        insert(
            task,
            [
                "option",
                "SelectOperator",
                "cases",
                -1,
            ],
            entry,
        );
        added++;
    }

    const template = entry.pipeline_override.GiftOperatorSelectSpecifiedOp.template;
    if (template.length !== 1 || !/^GiftOperator\/Operators\/[A-Za-z0-9]+\.png$/.test(template[0])) {
        throw new Error(`[GiftOperator] ${name} 的头像路径不符合约定`);
    }
    const missingResources = [
        "resource",
        "resource_adb",
    ].filter((resource) => !existsSync(resolve(repoRoot, "assets", resource, "image", template[0])));
    if (missingResources.length) avatars.push({id, template: template[0], resources: missingResources});

    const nodeName = `GiftOperatorSelect_${name}`;
    if (!Object.hasOwn(nodes.value, nodeName)) {
        insert(nodes, [nodeName], {
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
                    template,
                    green_mask: true,
                },
            },
            action: {
                type: "Custom",
                param: {
                    custom_action: "PipelineOverrideAction",
                    custom_action_param: {patch: {GiftOperatorName: entry.pipeline_override.GiftOperatorName}},
                },
            },
            next: ["GiftOperatorSelectSpecifiedOpSuccess"],
        });
    }
    if (!contact.value.GiftOperatorSelectGiftOp.next.includes(nodeName)) {
        insert(
            contact,
            [
                "GiftOperatorSelectGiftOp",
                "next",
                -1,
            ],
            nodeName,
        );
    }
    const sendNodeName = `GiftOperatorSendSelect_${name}`;
    if (!Object.hasOwn(send.value, sendNodeName)) {
        insert(send, [sendNodeName], {
            max_hit: 1,
            pre_delay: 0,
            action: {
                type: "Custom",
                param: {
                    custom_action: "PipelineOverrideAction",
                    custom_action_param: {
                        patch: {
                            GiftOperatorSelectSpecifiedOp: {template},
                            GiftOperatorName: entry.pipeline_override.GiftOperatorName,
                        },
                    },
                },
            },
            post_delay: 0,
            rate_limit: 0,
            next: ["GiftOperatorEntry"],
            focus: {
                "Node.Action.Starting": entry.label,
            },
        });
    }
    const sendReference = `[JumpBack]${sendNodeName}`;
    const sendNext = send.value.GiftOperatorSendAllMain.next;
    if (!sendNext.includes(sendReference)) {
        insert(
            send,
            [
                "GiftOperatorSendAllMain",
                "next",
            ],
            [
                sendReference,
                ...sendNext,
            ],
        );
    }
    const label = entry.label.slice(1);
    for (const locale of locales) {
        if (!Object.hasOwn(messages[locale].value, label)) {
            insert(messages[locale], [label], operator.names[locale]);
        }
    }
}

const changes = [];
for (const document of documents) {
    if (document.text === document.original) continue;
    const text = await format(document.text, {...(await resolveConfig(document.path)), filepath: document.path});
    changes.push({...document, text});
}
// 所有数据先校验；头像全部下载、解码成功后才落盘并发布配置，避免引用未生成的图片。
if (avatars.length) {
    const result = spawnSync(
        "uv",
        [
            "run",
            "tools/gift_operator/build_avatars.py",
        ],
        {
            cwd: repoRoot,
            input: JSON.stringify(avatars),
            stdio: [
                "pipe",
                "inherit",
                "inherit",
            ],
        },
    );
    if (result.error) throw result.error;
    if (result.status !== 0) throw new Error("[GiftOperator] 头像生成失败，未写入配置");
}
for (const {path, text} of changes) writeFileSync(path, text, "utf8");
console.log(`[GiftOperator] 新增 ${added} 名干员，补齐 ${avatars.length} 名干员的头像，更新 ${changes.length} 份配置`);
