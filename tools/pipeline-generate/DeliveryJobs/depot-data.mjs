import {deliveryJobDepots, rawJson} from "./model.mjs";

export default deliveryJobDepots.map((depot) => ({
    DepotId: depot.Id,
    DepotName: depot.Name,
    RegionId: depot.RegionId,
    DepotScene: depot.DepotScene,
    DepotExpected: rawJson(depot.Expected),
}));
