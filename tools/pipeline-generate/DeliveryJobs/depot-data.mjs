import {ALL_CARGO_EXPECTED, defaultQuoteThreshold} from "./constants.mjs";
import {deliveryJobDepots, rawJson} from "./model.mjs";

export default deliveryJobDepots.map((depot) => ({
    DepotId: depot.Id,
    DepotName: depot.Name,
    RegionId: depot.RegionId,
    DepotScene: depot.DepotScene,
    DepotExpected: rawJson(depot.Expected),
    CargoExpected: rawJson(ALL_CARGO_EXPECTED),
    QuoteThreshold: String(defaultQuoteThreshold(depot)),
}));
