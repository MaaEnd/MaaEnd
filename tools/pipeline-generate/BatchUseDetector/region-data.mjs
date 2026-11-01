// 地区入口节点的数据：data.json 每个地区一行，再按 extra-item-data.json 补上该地区独有的道具，算出入口节点的 next。
import {readJsonc} from "../jsonc.mjs";

const {data: regions} = readJsonc(new URL("./data.json", import.meta.url));
const {data: extraItems} = readJsonc(new URL("./extra-item-data.json", import.meta.url));

export default regions.map((region) => {
    const findItems = [
        `BatchUseDetectorFind${region.Detector}`,
        `BatchUseDetectorFind${region.Compass}`,
        ...extraItems
            .filter((item) => item.LocationId === region.LocationId)
            .map((item) => `BatchUseDetectorFind${item.ItemId}`),
    ];
    return {
        ...region,
        ItemNext: [
            ...findItems,
            "BatchUseDetectorFindNoItem",
        ],
        // ADB 当前页找不到时先往下翻页再找
        ItemNextAdb: [
            ...findItems,
            "[JumpBack]BatchUseDetectorSwipeNextPage",
            "BatchUseDetectorFindNoItem",
        ],
    };
});
