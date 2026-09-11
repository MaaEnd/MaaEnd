import assert from "node:assert/strict";
import test from "node:test";

import {readJsonc} from "../jsonc.mjs";
import {destinations} from "../AutoDelivery/model.mjs";
import {ENDPOINT_LOCALES, DIRECTION_TEXTS} from "./endpoint-labels.mjs";
import {commissionSourceOrder, endpointEntries} from "./endpoint-filter-data.mjs";

const LEGACY_ENDPOINTS = [
    "Owl",
    "MaterialResearchInstitute",
    "Observatory",
    "TechProductionOffice",
    "No1TypeCAnchorArea",
    "No3TypeCAnchorArea",
    "JingweiFieldArea",
];

// 终点展示名与方位登记：地点名（终点 NPC 所在的地点）手工抄自官方 i18n 表。
const endpointLabels = readJsonc(new URL("./endpoint-labels.json", import.meta.url));

const readTask = () => readJsonc(new URL("../../../assets/tasks/SeizeDeliveryJobs.json", import.meta.url));

const readLocale = (locale) => readJsonc(new URL(`../../../assets/locales/interface/${locale}.json`, import.meta.url));

test("SeizeDeliveryJobs 委托来源顺序与 task 选项顺序同源", () => {
    const cases = readTask().option.SeizeDeliveryJobsCommissionSource.cases.map(({name}) => name);
    assert.deepEqual(
        commissionSourceOrder.map(({CaseId}) => CaseId),
        cases,
    );
});

test("SeizeDeliveryJobs 文案键序与 task 选项顺序一致", () => {
    const cases = readTask().option.SeizeDeliveryJobsCommissionSource.cases.map(({name}) => name);
    const expected = cases.map((name) => `task.SeizeDeliveryJobsCommissionSource.cases.${name}.label`);
    for (const locale of ENDPOINT_LOCALES) {
        const messages = readLocale(locale);
        assert.deepEqual(
            Object.keys(messages).filter((key) => key.startsWith("task.SeizeDeliveryJobsCommissionSource.cases.")),
            expected,
            `${locale} 委托来源键序与 task 选项不一致`,
        );
    }
});

test("SeizeDeliveryJobs 每个终点都有地点名登记条目", () => {
    for (const {id} of destinations) {
        const entry = endpointLabels[id];
        assert.ok(entry, `数据源终点 ${id} 缺少登记条目`);
        assert.deepEqual(
            Object.keys(entry).filter((key) => key !== "direction"),
            ENDPOINT_LOCALES,
            `${id} 的字段应是五种语言，未登记时留空字符串`,
        );
        const code = entry.direction;
        assert.ok(
            Number.isInteger(code) && code >= 0 && code <= 8 && (code === 0 || DIRECTION_TEXTS[code]),
            `${id}.direction 必须是生成器补好的 0-8 方位代码：${code}`,
        );
        const filled = ENDPOINT_LOCALES.filter((locale) => typeof entry[locale] === "string" && entry[locale].trim());
        assert.ok(
            filled.length === 0 || filled.length === ENDPOINT_LOCALES.length,
            `${id} 的地点名要么五种语言全填，要么都留空，当前填了 ${filled.join("、")}`,
        );
    }
});

test("SeizeDeliveryJobs 终点展示名取地点名或数据源名称", () => {
    for (const entry of endpointEntries) {
        const destination = destinations.find(({id}) => id === entry.DestinationId);
        assert.ok(destination, `终点 ${entry.EndpointId} 未匹配到数据源`);
        for (const locale of ENDPOINT_LOCALES) {
            const placeName = endpointLabels[entry.DestinationId]?.[locale];
            // 与生成器 resolveEndpointNames 一致：留空（含纯空白）视为未登记，回退数据源收货人名称。
            const name = (
                typeof placeName === "string" && placeName.trim() ? placeName : destination.name[locale]
            )?.trim();
            assert.ok(
                entry.Names[locale].startsWith(name),
                `${entry.EndpointId}.${locale} 未使用地点名或数据源名称：${entry.Names[locale]}`,
            );
        }
    }
});

// 只检查生成文案与「方位代码 → 方位词」表自洽：漏填方位、产物被手改、生成器取错语言或括号规则坏掉时会红。
// 方位值本身填得对不对（例如把 5 填成 7）无法机器校验，靠 review 与游戏内验证。
test("SeizeDeliveryJobs 老终点保留手工维护的方位", () => {
    for (const endpointId of LEGACY_ENDPOINTS) {
        const entry = endpointEntries.find(({EndpointId}) => EndpointId === endpointId);
        assert.ok(entry, `缺少终点 ${endpointId}`);
        const code = endpointLabels[entry.DestinationId]?.direction ?? 0;
        assert.notEqual(code, 0, `${endpointId} 缺少方位代码`);
        for (const locale of ENDPOINT_LOCALES) {
            assert.match(entry.Names[locale], /[（(][^（()）]+[）)]$/, `${endpointId}.${locale} 缺少方位`);
            const direction = DIRECTION_TEXTS[code][locale];
            assert.ok(
                entry.Names[locale].endsWith(`（${direction}）`) || entry.Names[locale].endsWith(` (${direction})`),
                `${endpointId}.${locale} 的方位与代码 ${code} 不一致：${entry.Names[locale]}`,
            );
        }
    }
});
