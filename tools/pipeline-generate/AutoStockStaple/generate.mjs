import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import {fileURLToPath, pathToFileURL} from "node:url";
import {format, resolveConfig} from "prettier";

import {readJsonc} from "../jsonc.mjs";
import {repoRoot} from "../utils/paths.mjs";

const directory = dirname(fileURLToPath(import.meta.url));
const locales = [
    "zh_cn",
    "zh_tw",
    "en_us",
    "ja_jp",
    "ko_kr",
];
// 地区界面和数量上限是任务策略；商品成员、名称和分类全部由 stock_staple 提供。
const regions = {domain_1: "ValleyIV", domain_2: "Wuling"};
const categories = {household: "DailyGoods", industry: "StockFactoryGoods", crafts: "CulturalGoods"};
const namePrefix = "task.AutoStockStaple.item.";
const categoryPrefix = "task.AutoStockStaple.category.";
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

export function buildArtifacts(data) {
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
    const task = readJsonc(resolve(directory, "task-template.json"));
    const template = readJsonc(resolve(directory, "pipeline-template.json"));
    const offsets = readJsonc(resolve(directory, "recognition-offsets.json"));
    const messages = readJsonc(resolve(directory, "messages.json"));
    const pipelines = {
        Goods: {},
        GoodsCountValidate: template.GoodsCountValidate,
        QuantityControl: template.QuantityControl,
    };
    const translations = Object.fromEntries(
        locales.map((locale) => [
            locale,
            {...messages[locale].ui},
        ]),
    );
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
    const usedCaseNames = new Set();
    for (const [
        itemId,
        goods,
    ] of ordered) {
        const names = namesOf(data.items[itemId], itemId);
        const suffix = nodeId(itemId);
        const name = caseName(names);
        if (!name || usedCaseNames.has(name)) throw new Error(`Invalid or duplicate item option: ${name}`);
        usedCaseNames.add(name);
        if (usedNodeIds.has(suffix)) throw new Error(`Node ID collision: ${suffix}`);
        usedNodeIds.add(suffix);
        const region = regions[goods.region_id];
        const category = goods.category_id;
        const option = task.option[`AutoStockBuy${categories[category]}${region}Items`];
        option.label = `$${categoryPrefix}${category}`;
        option.cases.push({
            name,
            label: `$${namePrefix}${itemId}`,
            icon: `resource/image/UI/Item/${itemId}.png`,
            pipeline_override: {
                AutoStockInStapleItemName: {attach: {[itemId]: locales.map((locale) => names[locale])}},
            },
        });
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
            limit.pipeline_override[`AutoStockStapleGoods${suffix}${ending}`] = {
                recognition: {
                    param: {
                        custom_recognition_param: {
                            expression: `{${limits.input}} ${comparison} {AutoStockStapleGoodsCountValidate}`,
                        },
                    },
                },
            };
        }
        const nodes = substitute(template.item, {NodeId: suffix, ItemId: itemId, Name: names.zh_cn});
        const recognition = nodes[`AutoStockStapleGoods${suffix}`].recognition.param;
        recognition.expected = expectedNames(names);
        if (offsets[itemId]) recognition.roi_offset = offsets[itemId];
        for (const [
            name,
            node,
        ] of Object.entries(nodes)) {
            const file = name.startsWith("AutoStockStapleQuantityControl")
                ? "QuantityControl"
                : name.endsWith("Validate")
                  ? "GoodsCountValidate"
                  : "Goods";
            pipelines[file][name] = node;
        }
        pipelines.QuantityControl.AutoStockStapleQuantityControl.next.push(`AutoStockStapleQuantityControl${suffix}`);
        for (const locale of locales) {
            translations[locale][`${namePrefix}${itemId}`] = names[locale];
            translations[locale][`${categoryPrefix}${category}`] = data.categories[category].names[locale];
            for (const event of events) {
                translations[locale][`${focusPrefix}${event}.${itemId}`] = substitute(messages[locale].events[event], {
                    Name: names[locale],
                });
            }
        }
    }
    return {task, pipelines, translations};
}

export async function generate({
    root = repoRoot,
    dataPath = resolve(root, "tools/zmdmap/data/stock_staple.json"),
} = {}) {
    const data = JSON.parse(readFileSync(dataPath, "utf8"));
    const {task, pipelines, translations} = buildArtifacts(data);
    const outputs = new Map([
        [
            resolve(root, "assets/tasks/AutoStockStaple.json"),
            task,
        ],
    ]);
    for (const [
        name,
        nodes,
    ] of Object.entries(pipelines)) {
        outputs.set(resolve(root, `assets/resource/pipeline/AutoStockStaple/General/${name}.json`), nodes);
    }
    for (const locale of locales) {
        const path = resolve(root, `assets/locales/interface/${locale}.json`);
        let messages = {};
        try {
            messages = readJsonc(path);
        } catch (error) {
            if (error.code !== "ENOENT") throw error;
        }
        // 只替换此生成器拥有的键，旧商品移除后不会留在名称或提示目录里。
        const remaining = Object.fromEntries(
            Object.entries(messages).filter(
                ([key]) =>
                    !key.startsWith(namePrefix) &&
                    !key.startsWith(categoryPrefix) &&
                    !events.some((event) => key.startsWith(`${focusPrefix}${event}.`)),
            ),
        );
        outputs.set(path, {...remaining, ...translations[locale]});
    }
    const config = await resolveConfig(resolve(repoRoot, ".prettierrc.mjs"));
    // 先构建、校验并格式化全部产物，任何数据错误都在写入之前暴露。
    const formatted = await Promise.all(
        [...outputs].map(
            async ([
                path,
                value,
            ]) => [
                path,
                await format(JSON.stringify(value, null, 4), {
                    ...config,
                    filepath: path,
                    parser: "json",
                    endOfLine: "lf",
                }),
            ],
        ),
    );
    for (const [
        path,
        text,
    ] of formatted) {
        mkdirSync(dirname(path), {recursive: true});
        writeFileSync(path, text, "utf8");
    }
    console.log(`[AutoStockStaple] Generated ${Object.keys(pipelines.Goods).length} items from stock_staple`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
    await generate();
}
