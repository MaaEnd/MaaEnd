import {sellProductRegions} from "./model.mjs";

export const sellProductSellRows = sellProductRegions.map((region) => {
    const outpostNext = region.LocationIds.map((locationId) => `[JumpBack]SellProduct${locationId}`).concat(
        "SellProductLoop",
        "[JumpBack]SceneEnterMenuRegionalDevelopment",
    );
    return {
        RegionPrefix: region.RegionPrefix,
        RegionDesc: region.RegionDesc,
        SellNext: [
            `[Anchor]SellProduct${region.RegionPrefix}PrepareOperatorCache`,
            ...outpostNext,
        ],
        PrepareNext: [
            "SellProductOutpostLocked",
            ...outpostNext,
        ],
    };
});

export default sellProductSellRows;
