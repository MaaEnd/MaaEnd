// DeliveryJobs 生成器的公共常量：Pipeline 模板与任务选项模板共用同一份定义。
//
// 货物界面可能停在「调度申请（查看报价）」或「货物装箱」两种页面：
// - ALL_CARGO_EXPECTED 是两种页面都认的默认识别文本，depot-template.jsonc 通过
//   depot-data.mjs 注入到 DeliveryJobsCheck{DepotId}Cargo.expected；
// - PACK_CARGO_EXPECTED 只认「货物装箱」，「仅装箱货物」处理方式用它覆盖节点默认值。
const BID_CARGO_EXPECTED = [
    "查看报价",
    "查看報價",
    "(?i)Check\\s*Bid",
    "希望価格確認",
    "입찰 보기",
];

export const PACK_CARGO_EXPECTED = [
    "货物装箱",
    "貨物裝箱",
    "(?i)Pack\\s*Goods",
    "パッキング",
    "화물 포장",
];

export const ALL_CARGO_EXPECTED = [
    ...BID_CARGO_EXPECTED,
    ...PACK_CARGO_EXPECTED,
];

// 「按报价处理」的默认阈值（调度券），同时用于 Pipeline 表达式与任务选项的默认值。
// 各地区的报价量级不同，目前四号谷地约 11.9 万、武陵约 19.4 万；新地区未配置时回退到 FALLBACK。
export const DEFAULT_QUOTE_THRESHOLD_BY_REGION = {
    ValleyIV: 119000,
    Wuling: 194000,
};
export const DEFAULT_QUOTE_THRESHOLD_FALLBACK = 119000;

/** 取某个仓储节点的默认报价阈值：按所属地区取；未配置地区回退到全局兜底。 */
export function defaultQuoteThreshold(depot) {
    return DEFAULT_QUOTE_THRESHOLD_BY_REGION[depot.RegionId] ?? DEFAULT_QUOTE_THRESHOLD_FALLBACK;
}
