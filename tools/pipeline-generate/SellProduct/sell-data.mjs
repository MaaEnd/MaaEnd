import {sellProductRegions} from "./model.mjs";

export const sellProductSellRows = sellProductRegions.map((region) => ({
    RegionPrefix: region.RegionPrefix,
    RegionDesc: region.RegionDesc,
    Next: region.LocationIds.map((locationId) => `[JumpBack]SellProduct${locationId}`).concat(
        "SellProductLoop",
        "[JumpBack]SceneEnterMenuRegionalDevelopment",
    ),
}));

export default sellProductSellRows;
