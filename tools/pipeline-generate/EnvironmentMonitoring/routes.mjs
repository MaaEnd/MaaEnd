// 该文件仅保留维护者需要手动编辑的路线配置：
//   - ROUTE_CONFIG：每个监测任务的路线/地图/朝向覆盖项（坐标数据维护在同目录的 routes.json）
//   - ROUTE_DEFAULTS：未提供 ROUTE_CONFIG 时的默认占位值
// 其余生成逻辑（任务收集、ID 处理、终端归类、行装配等）见 data.mjs，
// 它会读取这里的 ROUTE_CONFIG / ROUTE_DEFAULTS 装配出最终行数据。
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
