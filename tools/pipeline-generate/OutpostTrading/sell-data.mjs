import {outpostTradingRegions} from "./model.mjs";

export const outpostTradingSellRows = outpostTradingRegions.map((region) => {
    const outpostNext = region.LocationIds.map((locationId) => `[JumpBack]OutpostTrading${locationId}`).concat(
        "OutpostTradingLoop",
        "[JumpBack]SceneEnterMenuRegionalDevelopment",
    );
    return {
        RegionPrefix: region.RegionPrefix,
        RegionDesc: region.RegionDesc,
        // 地区据点锚点取该地区游戏内顺序第一个据点的标签页文本检查节点。
        AnchorLocationId: region.LocationIds[0],
        SellNext: [`OutpostTrading${region.RegionPrefix}InitializePrioritySession`],
        PrepareNext: outpostNext,
    };
});

export default outpostTradingSellRows;
