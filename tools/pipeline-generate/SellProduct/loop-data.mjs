// SellProduct 主循环模板数据

import {sellProductRegionsNewestFirst} from "./model.mjs";

// 主循环是全局唯一节点，单行数据只把地区遍历列表注入模板的 next。
export const sellProductLoopRows = [
    {
        LoopNext: [
            ...sellProductRegionsNewestFirst.map((region) => `SellProduct${region.RegionPrefix}`),
            "SellProductTaskEnd",
        ],
    },
];

export default sellProductLoopRows;
