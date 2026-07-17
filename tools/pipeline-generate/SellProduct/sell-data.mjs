import settlementFlatRows from "./data.mjs";

const REGION_DESCRIPTIONS = {
    ValleyIV: "四号谷地",
    Wuling: "武陵",
};

export const sellProductSellRows = Object.values(
    settlementFlatRows.reduce((regions, location) => {
        if (!regions[location.RegionPrefix]) {
            regions[location.RegionPrefix] = {
                RegionPrefix: location.RegionPrefix,
                RegionDesc: REGION_DESCRIPTIONS[location.RegionPrefix] || location.RegionPrefix,
                Next: [],
            };
        }
        regions[location.RegionPrefix].Next.push(`[JumpBack]SellProduct${location.LocationId}`);
        return regions;
    }, {}),
).map((region) => ({
    ...region,
    Next: region.Next.concat("SellProductLoop", "[JumpBack]SceneEnterMenuRegionalDevelopment"),
}));

export default sellProductSellRows;
