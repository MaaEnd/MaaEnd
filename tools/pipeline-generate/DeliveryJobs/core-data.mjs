import {deliveryJobRegions, rawJson} from "./model.mjs";

export default [
    {
        LoopNext: rawJson([
            "DeliveryJobsAuto",
            ...deliveryJobRegions.map((region) => `DeliveryJobs${region.Id}`),
            "DeliveryJobsFinished",
        ]),
        AutoNext: rawJson([
            ...deliveryJobRegions.map((region) => `DeliveryJobsAuto${region.Id}`),
            "DeliveryJobsLoop",
        ]),
    },
];
