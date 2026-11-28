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
// 节点最高基础报价由 zmdmap 的 max_base_quote 提供；这里只选择常见买家倍率并按界面精度截位。
const COMMON_QUOTE_MULTIPLIER = 1.1;
const QUOTE_DISPLAY_UNIT = 1000;

/** 取节点的常见高档报价，并按界面显示精度向下截到千位。 */
export function defaultQuoteThreshold(depot) {
    return Math.floor((depot.MaxBaseQuote * COMMON_QUOTE_MULTIPLIER) / QUOTE_DISPLAY_UNIT) * QUOTE_DISPLAY_UNIT;
}
