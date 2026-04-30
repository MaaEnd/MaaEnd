// 维护者手动编辑的路线配置：
//   - ROUTE_CONFIG：每个监测任务的路线/地图/朝向覆盖项（坐标数据维护在同目录的 routes.json）
//   - ROUTE_DEFAULTS：未提供 ROUTE_CONFIG 时的默认占位值
// 字段说明见 README.md。

import {createRequire} from "module";

const require = createRequire(import.meta.url);

export const ROUTE_CONFIG = require("./routes.json");

export const ROUTE_DEFAULTS = {
    EnterMap: "SceneAnyEnterWorld",
    MapName: "^map\\d+_lv\\d+$",
    MapTarget: [
        0,
        0,
        1,
        1,
    ],
    MapPath: [
        [
            0,
            0,
        ],
    ],
    CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
    CameraMaxHit: 2,
};
