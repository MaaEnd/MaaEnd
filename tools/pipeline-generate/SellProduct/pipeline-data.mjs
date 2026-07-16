import {sellProductLocations} from "./model.mjs";

// Win32 BetterSliding 识别框，以 1280x720 为基准。
const QUANTITY_BOX = [
    1107,
    535,
    74,
    29,
];
const MAX_QUANTITY_BOX = [
    1073,
    327,
    119,
    25,
];

export default sellProductLocations.map((location) => ({
    RegionPrefix: location.RegionPrefix,
    LocationId: location.LocationId,
    LocationDesc: location.LocationDesc,
    TextExpected: location.TextExpected,
    QuantityBox: QUANTITY_BOX,
    MaxTargetBox: MAX_QUANTITY_BOX,
}));
