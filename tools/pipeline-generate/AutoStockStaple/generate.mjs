import {readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {format, resolveConfig} from "prettier";
import {applyEdits, modify} from "jsonc-parser";

import {parseJsonc, readJsonc} from "../jsonc.mjs";
import {repoRoot} from "../utils/paths.mjs";

const directory = dirname(fileURLToPath(import.meta.url));
const locales = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];
// 地区界面和数量上限是任务策略；新增商品及其名称和分类由 stock_staple 提供。
const regions = {domain_1: "ValleyIV", domain_2: "Wuling"};
const categories = {household: "DailyGoods", industry: "StockFactoryGoods", crafts: "CulturalGoods"};
const namePrefix = "task.AutoStockStaple.item.";
const focusPrefix = "task.AutoStockStaple.focus.quantity_control.";
const events = [
    "buy",
    "reached",
    "stock_bill_insufficient",
];

function nodeId(id) {
    if (!/^[a-z][a-z0-9_]*$/.test(id)) throw new Error(`Invalid item ID: ${id}`);
    return id
        .split("_")
        .map((part) => part[0].toUpperCase() + part.slice(1))
        .join("");
}

function caseName(names) {
    return names.en_us
        .replace(/['’]/g, "")
        .split(/[^a-zA-Z0-9]+/)
        .filter(Boolean)
        .map((part) => part[0].toUpperCase() + part.slice(1))
        .join("");
}

function namesOf(entry, id) {
    for (const locale of locales) {
        if (typeof entry?.names?.[locale] !== "string" || !entry.names[locale].trim()) {
            throw new Error(`${id}: missing ${locale} name`);
        }
    }
    return entry.names;
}

function substitute(value, variables) {
    if (typeof value === "string")
        return value.replace(/\$\{(\w+)\}/g, (_, key) => {
            if (!Object.hasOwn(variables, key)) throw new Error(`Unknown template variable: ${key}`);
            return variables[key];
        });
    if (Array.isArray(value)) return value.map((entry) => substitute(entry, variables));
    if (value && typeof value === "object")
        return Object.fromEntries(
            Object.entries(value).map(
                ([
                    key,
                    entry,
                ]) => [
                    substitute(key, variables),
                    substitute(entry, variables),
                ],
            ),
        );
    return value;
}

function expectedNames(names) {
    return [
        ...new Set(
            locales.map((locale) => {
                const escaped = names[locale].replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
                // 与原节点一致：英文大小写不敏感，允许 OCR 丢失词间空格。
                return locale === "en_us" ? `(?i)${escaped.replace(/\s+/g, "\\s*")}` : escaped;
            }),
        ),
    ];
}

function limitOption(region, category, itemId) {
    const shortRegion = region === "ValleyIV" ? "Valley" : region;
    let suffix;
    // 两种独立数量上限沿用现有 UI；规则覆盖整个游戏物品 ID 家族，不列商品白名单。
    if (category === "household") {
        suffix = itemId.startsWith("item_gem_stable_energy_point_") ? "EngravingPermit" : "FoodAndBuff";
    } else if (category === "industry") {
        suffix = itemId.startsWith("item_equip_enhance_") ? "ArtificingCatalyst" : "DetectorCompass";
    } else {
        suffix = "Gift";
    }
    return {
        option: `AutoStockBuy${categories[category]}${region}${suffix}Limit`,
        input: `${shortRegion}${suffix}Limit`,
    };
}

function buildAdditions(data, task, pipelines, translations) {
    for (const key of [
        "regions",
        "categories",
        "items",
        "goods",
    ]) {
        if (!data?.[key] || typeof data[key] !== "object" || Array.isArray(data[key])) {
            throw new Error(`stock_staple: missing ${key}`);
        }
    }
    const template = readJsonc(resolve(directory, "pipeline-template.json"));
    const messages = readJsonc(resolve(directory, "messages.json"));
    // 用现有选项的图标路径识别 item_id，不另存商品绑定表。
    const existing = new Map();
    const usedCaseNames = new Set();
    for (const [
        name,
        option,
    ] of Object.entries(task.option)) {
        if (!name.endsWith("Items")) continue;
        for (const entry of option.cases) {
            const itemId = entry.icon?.match(/\/Item\/([^/]+)\.png$/)?.[1];
            if (!itemId || existing.has(itemId)) throw new Error(`${name}: missing or duplicate item icon ID`);
            existing.set(itemId, entry);
            usedCaseNames.add(entry.name);
        }
    }
    const selected = new Map();
    const reachedGoods = new Set();

    for (const [
        regionId,
        region,
    ] of Object.entries(data.regions)) {
        const regionSuffix = regions[regionId];
        if (!regionSuffix) throw new Error(`Region ${regionId} needs an adapted navigation pipeline`);
        namesOf(region, regionId);
        if (!Array.isArray(region.goods_ids)) throw new Error(`${regionId}: missing goods_ids`);
        for (const goodsId of region.goods_ids) {
            const goods = data.goods[goodsId];
            if (!goods || goods.region_id !== regionId) throw new Error(`${goodsId}: invalid region reference`);
            if (reachedGoods.has(goodsId)) throw new Error(`Duplicate goods reference: ${goodsId}`);
            reachedGoods.add(goodsId);
            if (!Number.isFinite(goods.price) || goods.price < 0 || !(goods.discount > 0 && goods.discount <= 1)) {
                throw new Error(`${goodsId}: invalid price or discount`);
            }
            const category = data.categories[goods.category_id];
            if (!categories[goods.category_id]) throw new Error(`Unsupported shop category: ${goods.category_id}`);
            namesOf(category, goods.category_id);
            namesOf(data.items[goods.item_id], goods.item_id);
            const previous = selected.get(goods.item_id);
            if (previous && (previous.region_id !== regionId || previous.category_id !== goods.category_id)) {
                throw new Error(`${goods.item_id}: conflicting shop categories or regions`);
            }
            if (!previous || goods.sort_id < previous.sort_id) selected.set(goods.item_id, goods);
        }
    }
    if (selected.size === 0 || reachedGoods.size !== Object.keys(data.goods).length) {
        throw new Error("stock_staple: empty selection or unreferenced goods");
    }
    const ordered = [...selected.entries()].sort(
        (
            [
                aId,
                a,
            ],
            [
                bId,
                b,
            ],
        ) =>
            data.regions[a.region_id].sort_id - data.regions[b.region_id].sort_id ||
            data.categories[a.category_id].sort_id - data.categories[b.category_id].sort_id ||
            a.sort_id - b.sort_id ||
            aId.localeCompare(bId, "en"),
    );
    const usedNodeIds = new Set();
    let added = 0;
    for (const [
        itemId,
        goods,
    ] of ordered) {
        const names = namesOf(data.items[itemId], itemId);
        const previous = existing.get(itemId);
        // 已有手写配置原样保留。生成项也只补缺失节点，不改已存在的配置。
        if (previous && !Object.hasOwn(previous.pipeline_override.AutoStockInStapleItemName.attach, itemId)) continue;
        const suffix = nodeId(itemId);
        const name = previous?.name ?? caseName(names);
        if (!name || (!previous && usedCaseNames.has(name)))
            throw new Error(`Invalid or duplicate item option: ${name}`);
        usedCaseNames.add(name);
        if (usedNodeIds.has(suffix)) throw new Error(`Node ID collision: ${suffix}`);
        usedNodeIds.add(suffix);
        const region = regions[goods.region_id];
        const category = goods.category_id;
        const option = task.option[`AutoStockBuy${categories[category]}${region}Items`];
        if (!option) throw new Error(`Missing shop option for ${region}/${category}`);
        if (!previous) {
            option.cases.push({
                name,
                label: `$${namePrefix}${itemId}`,
                icon: `resource/image/UI/Item/${itemId}.png`,
                pipeline_override: {
                    AutoStockInStapleItemName: {attach: {[itemId]: locales.map((locale) => names[locale])}},
                },
            });
            added++;
        }
        const limits = limitOption(region, category, itemId);
        const limit = task.option[limits.option];
        if (!limit) throw new Error(`Missing quantity policy: ${limits.option}`);
        for (const [
            ending,
            comparison,
        ] of [
            [
                "Validate",
                ">",
            ],
            [
                "ExcludeValidate",
                "<=",
            ],
        ]) {
            limit.pipeline_override[`AutoStockStapleGoods${suffix}${ending}`] ??= {
                recognition: {
                    param: {
                        custom_recognition_param: {
                            expression: `{${limits.input}} ${comparison} {AutoStockStapleGoodsCountValidate}`,
                        },
                    },
                },
            };
        }
        const nodes = substitute(template, {NodeId: suffix, ItemId: itemId, Name: names.zh_cn});
        const recognition = nodes[`AutoStockStapleGoods${suffix}`].recognition.param;
        recognition.expected = expectedNames(names);
        for (const [
            name,
            node,
        ] of Object.entries(nodes)) {
            const file = name.startsWith("AutoStockStapleQuantityControl")
                ? "QuantityControl"
                : name.endsWith("Validate")
                  ? "GoodsCountValidate"
                  : "Goods";
            pipelines[file][name] ??= node;
        }
        const branches = pipelines.QuantityControl.AutoStockStapleQuantityControl.next;
        const branch = `AutoStockStapleQuantityControl${suffix}`;
        if (!branches.includes(branch)) branches.push(branch);
        for (const locale of locales) {
            translations[locale][`${namePrefix}${itemId}`] ??= names[locale];
            for (const event of events) {
                translations[locale][`${focusPrefix}${event}.${itemId}`] ??= substitute(messages[locale][event], {
                    Name: names[locale],
                });
            }
        }
    }
    return added;
}

// 仅插入新增键和数组尾部，保留原 JSONC 注释、OCR 特例和已有成员顺序。
function appendChanges(text, before, after, path = []) {
    if (JSON.stringify(before) === JSON.stringify(after)) return text;
    const edit = (target, value) => {
        text = applyEdits(
            text,
            modify(text, target, value, {formattingOptions: {insertSpaces: true, tabSize: 4, eol: "\n"}}),
        );
    };
    if (before === undefined) {
        edit(path, after);
    } else if (Array.isArray(before) && Array.isArray(after)) {
        if (JSON.stringify(before) !== JSON.stringify(after.slice(0, before.length)))
            throw new Error(`Existing array changed: ${path.join(".")}`);
        for (const value of after.slice(before.length))
            edit(
                [
                    ...path,
                    -1,
                ],
                value,
            );
    } else if (before && after && typeof before === "object" && typeof after === "object") {
        for (const key of new Set([
            ...Object.keys(before),
            ...Object.keys(after),
        ])) {
            text = appendChanges(text, before[key], after[key], [
                ...path,
                key,
            ]);
        }
    } else {
        throw new Error(`Existing value changed: ${path.join(".")}`);
    }
    return text;
}

async function generate() {
    const data = readJsonc(resolve(repoRoot, "tools/zmdmap/data/stock_staple.json"));
    const documents = [];
    const load = (relative) => {
        const path = resolve(repoRoot, relative);
        const text = readFileSync(path, "utf8");
        const before = parseJsonc(text, path);
        const value = structuredClone(before);
        documents.push({path, text, before, value});
        return value;
    };
    const task = load("assets/tasks/AutoStockStaple.json");
    const pipelines = Object.fromEntries(
        [
            "Goods",
            "GoodsCountValidate",
            "QuantityControl",
        ].map((name) => [
            name,
            load(`assets/resource/pipeline/AutoStockStaple/General/${name}.json`),
        ]),
    );
    const translations = Object.fromEntries(
        locales.map((locale) => [
            locale,
            load(`assets/locales/interface/${locale}.json`),
        ]),
    );
    const added = buildAdditions(data, task, pipelines, translations);
    const config = await resolveConfig(resolve(repoRoot, ".prettierrc.mjs"));
    // 全部构建、校验成功后再写入；没有增量时连格式和文件时间也不改。
    const formatted = await Promise.all(
        documents
            .filter(({before, value}) => JSON.stringify(before) !== JSON.stringify(value))
            .map(async ({path, text, before, value}) => [
                path,
                await format(appendChanges(text, before, value), {
                    ...config,
                    filepath: path,
                    parser: "json",
                    endOfLine: "lf",
                }),
            ]),
    );
    for (const [
        path,
        text,
    ] of formatted) {
        writeFileSync(path, text, "utf8");
    }
    console.log(`[AutoStockStaple] Added ${added} items; updated ${formatted.length} files`);
}

await generate();
