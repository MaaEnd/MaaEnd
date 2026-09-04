import {outpostTradingLocations} from "./model.mjs";

const operatorRegistrationNodes = [
    ...outpostTradingLocations.map((location) => `OutpostTradingRegisterLocation${location.LocationId}`),
    "OutpostTradingOperatorSessionReady",
];

export default outpostTradingLocations.map((location, index) => ({
    LocationId: location.LocationId,
    LocationDesc: location.LocationDesc,
    OperatorRegistrationNext: operatorRegistrationNodes[index + 1],
}));
