import {sellProductLocations} from "./model.mjs";

// ADB BetterSliding 识别框，以 1280x720 为基准。
const QUANTITY_BOX_ADB = [
    1065,
    499,
    78,
    36,
];
const MAX_QUANTITY_BOX_ADB = [
    1041,
    239,
    131,
    32,
];

export default sellProductLocations.map((location) => ({
    LocationId: location.LocationId,
    QuantityBoxAdb: QUANTITY_BOX_ADB,
    MaxTargetBoxAdb: MAX_QUANTITY_BOX_ADB,
}));
