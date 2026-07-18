import {sellProductLocations} from "./model.mjs";

const operatorRegistrationNodes = [
    ...sellProductLocations.map((location) => `SellProductRegisterAuto${location.LocationId}`),
    "SellProductOperatorSessionReady",
];

export default sellProductLocations.map((location, index) => ({
    LocationId: location.LocationId,
    LocationDesc: location.LocationDesc,
    OperatorRegistrationNext: operatorRegistrationNodes[index + 1],
}));
