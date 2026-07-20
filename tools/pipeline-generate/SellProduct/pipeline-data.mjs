import {sellProductLocations} from "./model.mjs";

// Win32 / ADB 共用的当前联络干员识别框，以 1280x720 为基准。
// 大范围同时覆盖两种控制器的名称位置；右侧界面文本由 Go 的已知名称前缀匹配消歧。
const CURRENT_OPERATOR_ROI = [
    260,
    568,
    280,
    35,
];

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
    CurrentOperatorROI: CURRENT_OPERATOR_ROI,
    QuantityBox: QUANTITY_BOX,
    MaxTargetBox: MAX_QUANTITY_BOX,
}));
