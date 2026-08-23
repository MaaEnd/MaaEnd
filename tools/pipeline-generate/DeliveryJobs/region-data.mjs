import {deliveryJobRegions, rawJson} from "./model.mjs";

export default deliveryJobRegions.map((region) => ({
    RegionId: region.Id,
    RegionName: region.Name,
    RegionScene: region.RegionScene,
    DepotScene: region.DepotScene,
    FirstDepotId: region.Depots[0],
    LoopNext: rawJson([
        ...region.Depots.map((depotId) => `[JumpBack]DeliveryJobsEnter${depotId}DeliveryJob`),
        ...region.Depots.map((depotId) => `[JumpBack]DeliveryJobsEnter${depotId}Cargo`),
        "DeliveryJobsLoop",
        "[JumpBack]SceneEnterMenuRegionalDevelopment",
    ]),
}));
