import {isFieldMissing, sanitizeDisplayName} from "./common.mjs";

export const CAMERA_MAX_HIT_DEFAULT = 2;

export const ROUTE_CONFIG_FIELDS = [
    "EnterMap",
    "MapName",
    "MapAssert",
    "MapPath",
    "MapTarget",
    "MapTargetTier",
    "MapTargetDeckY",
    "MapGoal",
    "NavZoneId",
    "NavAssert",
    "NavPath",
    "CameraSwipeDirection",
    "CameraMaxHit",
    "Replace",
    "Heading",
    "NoEnsureInitialMovementState",
    "QuickTeleport",
];

// MapNavigator 路线只要求路径；普通传送还需要起点断言的分区与矩形。
const NAV_ROUTE_REQUIRED_FIELDS = [
    "NavPath",
];
const NAV_ASSERT_FIELDS = [
    "NavZoneId",
    "NavAssert",
];
const NAV_ROUTE_FIELDS = [
    ...NAV_ASSERT_FIELDS,
    "NavPath",
];

const ROUTE_RENDER_FIELDS = [
    "EnterMap",
    "MapName",
    "MapAssert",
    "CameraSwipeDirection",
];

export function collectNavRouteFields(route) {
    return NAV_ROUTE_FIELDS.filter((field) => !isFieldMissing(route?.[field]));
}

function hasCompleteNavRoute(route) {
    const hasRequiredFields = NAV_ROUTE_REQUIRED_FIELDS.every((field) => !isFieldMissing(route?.[field]));
    const assertFieldsPresent = NAV_ASSERT_FIELDS.filter((field) => !isFieldMissing(route?.[field])).length;
    const hasCompleteAssert = assertFieldsPresent === NAV_ASSERT_FIELDS.length;
    const canOmitAssert = route?.QuickTeleport === true && assertFieldsPresent === 0;
    return hasRequiredFields && (hasCompleteAssert || canOmitAssert);
}

export function collectMissingRouteFields(route) {
    if (route == null) {
        return ["route"];
    }

    const quickTeleport = route.QuickTeleport === true;
    const hasMapAssert = !isFieldMissing(route.MapAssert);
    const hasMapPath = !isFieldMissing(route.MapPath);
    const hasMapTarget = !isFieldMissing(route.MapTarget);
    const hasMapGoal = !isFieldMissing(route.MapGoal);
    const navFieldsPresent = collectNavRouteFields(route);
    const hasNavRoute = hasCompleteNavRoute(route);
    const navigationConfigCount = [
        hasMapPath,
        hasMapTarget,
        hasMapGoal,
    ].filter(Boolean).length;
    const isDirectPhoto = !hasMapAssert && navFieldsPresent.length === 0 && navigationConfigCount === 0;
    const canSkipMapAssert = quickTeleport && navigationConfigCount === 1;
    const missingFields = [];

    if (!quickTeleport && isFieldMissing(route.EnterMap)) {
        missingFields.push("EnterMap");
    }
    if (isFieldMissing(route.CameraSwipeDirection)) {
        missingFields.push("CameraSwipeDirection");
    }
    if (isDirectPhoto) {
        const unusedDirectPhotoFields = [
            "MapName",
            "MapTargetTier",
            "MapTargetDeckY",
            "NoEnsureInitialMovementState",
        ].filter((field) => !isFieldMissing(route[field]));
        if (unusedDirectPhotoFields.length > 0) {
            missingFields.push(`传送后直拍不应配置 ${unusedDirectPhotoFields.join("/")}`);
        }
    } else if (navFieldsPresent.length > 0) {
        if (!hasNavRoute) {
            const hasAnyAssertField = NAV_ASSERT_FIELDS.some((field) => !isFieldMissing(route[field]));
            const requiredFields = quickTeleport && !hasAnyAssertField ? NAV_ROUTE_REQUIRED_FIELDS : NAV_ROUTE_FIELDS;
            missingFields.push(`${requiredFields.join("/")} 必须同时配置`);
        } else {
            // Nav 路线自带路径，普通传送另带分区与断言；老的地图字段留着只会两份配置对不上。
            const unusedNavRouteFields = [
                "MapName",
                "MapAssert",
                "MapPath",
                "MapTarget",
                "MapTargetTier",
                "MapGoal",
                "NoEnsureInitialMovementState",
            ].filter((field) => !isFieldMissing(route[field]));
            if (unusedNavRouteFields.length > 0) {
                missingFields.push(`NavPath 路线不应配置 ${unusedNavRouteFields.join("/")}`);
            }
        }
    } else {
        if (isFieldMissing(route.MapName)) {
            missingFields.push("MapName");
        }
        if (!canSkipMapAssert && !hasMapAssert) {
            missingFields.push("MapAssert");
        }
        if (navigationConfigCount === 0) {
            missingFields.push("MapPath/MapTarget/MapGoal");
        } else if (navigationConfigCount > 1) {
            missingFields.push("MapPath/MapTarget/MapGoal 三选一");
        }
    }

    if (!isFieldMissing(route.MapTargetTier) && !hasMapTarget) {
        missingFields.push("MapTargetTier 仅可与 MapTarget 同时使用");
    }

    if (!isFieldMissing(route.MapTargetDeckY) && !hasMapTarget) {
        missingFields.push("MapTargetDeckY 仅可与 MapTarget 同时使用");
    }

    return missingFields;
}

// 未适配任务不会进入寻路/拍照分支；这些值只用于渲染模板中不可达的路线节点。
const UNREACHABLE_ROUTE_PLACEHOLDER = {
    EnterMap: "SceneAnyEnterWorld",
    MapName: "^map\\d+_lv\\d+$",
    MapAssert: [
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
    MapTarget: null,
    MapTargetTier: null,
    MapTargetDeckY: null,
    MapGoal: null,
    NavZoneId: "Wuling_Base",
    CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
};

function defaultWarn(message) {
    console.warn(message);
}

function buildRouteOverrideIndexes(routeConfig, warn) {
    const byMissionId = new Map();

    for (const item of routeConfig) {
        if (isFieldMissing(item.MissionId)) {
            warn(
                `[EnvironmentMonitoring] routes.json 条目 ${item.Name || "<unknown>"} 缺少必填 MissionId，不会参与匹配。`,
            );
            continue;
        }
        if (byMissionId.has(item.MissionId)) {
            warn(`[EnvironmentMonitoring] routes.json 中存在重复 MissionId: ${item.MissionId}，后者将覆盖前者。`);
        }
        byMissionId.set(item.MissionId, item);
    }

    return {
        byMissionId,
        used: new Set(),
    };
}

function getRouteOverride(mission, routeOverrides) {
    const missionId = mission?.missionId;
    if (missionId && routeOverrides.byMissionId.has(missionId)) {
        const override = routeOverrides.byMissionId.get(missionId);
        routeOverrides.used.add(override);
        return override;
    }
    return undefined;
}

function normalizeHeading(headingRaw, mission, missionName, warn) {
    const isHeadingNumber = typeof headingRaw === "number" && Number.isFinite(headingRaw);
    const isHeadingInRange = isHeadingNumber && headingRaw >= 0 && headingRaw < 360;

    if (isHeadingNumber && !isHeadingInRange) {
        warn(
            `[EnvironmentMonitoring] 任务 ${sanitizeDisplayName(missionName)} (${mission.missionId}) Heading 值 ${headingRaw} 超出合法范围 [0, 360)，已自动归一化为 ${((headingRaw % 360) + 360) % 360}。`,
        );
    }

    return {
        HasHeading: isHeadingNumber,
        Heading: isHeadingNumber ? ((headingRaw % 360) + 360) % 360 : undefined,
    };
}

function buildNavigationParams({
    MapName,
    MapAssert,
    MapPath,
    MapTarget,
    MapTargetTier,
    MapTargetDeckY,
    MapGoal,
    NavZoneId,
    NavAssert,
    NavPath,
    NoEnsureInitialMovementState,
    hasMapTarget,
    hasMapGoal,
    hasNavRoute,
    isDirectPhoto,
    heading,
}) {
    // 1. 构建位置断言识别节点
    const MapAssertRecognition = hasMapTarget || hasNavRoute ? "MapLocateAssertLocation" : "MapTrackerAssertLocation";
    const MapAssertParam =
        MapAssertRecognition === "MapLocateAssertLocation"
            ? {
                  // 使用 MapLocateAssertLocation
                  zone_id: hasNavRoute ? NavZoneId : MapName,
                  target: hasNavRoute ? NavAssert : MapAssert,
              }
            : {
                  // 使用 MapTrackerAssertLocation
                  expected: [
                      {
                          map_name: MapName,
                          target: MapAssert,
                      },
                  ],
              };

    // 2. 构建导航动作节点
    const shouldAdjustDirectPhotoHeading = isDirectPhoto && heading.HasHeading;
    const RouteAction = shouldAdjustDirectPhotoHeading
        ? "MapTrackerToward"
        : hasMapTarget || hasNavRoute
          ? "MapNavigateAction"
          : hasMapGoal
            ? "MapTrackerGoal"
            : "MapTrackerMove";
    const mapTrackerExtraParams = {
        ...(heading.HasHeading
            ? {
                  on_finish: {
                      action: "Custom",
                      custom_action: "MapTrackerToward",
                      custom_action_param: {
                          angle: heading.Heading,
                      },
                  },
              }
            : {}),
        ...(NoEnsureInitialMovementState ? {no_ensure_initial_movement_state: true} : {}),
    };
    const RouteActionParam = shouldAdjustDirectPhotoHeading
        ? {
              angle: heading.Heading,
          }
        : RouteAction === "MapNavigateAction"
          ? {
                // 使用 MapNavigateAction
                path: [
                    ...(hasNavRoute
                        ? NavPath
                        : [
                              {
                                  action: "NAVMESH",
                                  target: MapTarget,
                                  ...(!isFieldMissing(MapTargetTier) ? {target_tier: MapTargetTier} : {}),
                                  ...(!isFieldMissing(MapTargetDeckY) ? {target_deck_y: MapTargetDeckY} : {}),
                              },
                          ]),
                    ...(heading.HasHeading
                        ? [
                              {
                                  action: "HEADING",
                                  angle: heading.Heading,
                              },
                          ]
                        : []),
                ],
            }
          : RouteAction === "MapTrackerGoal"
            ? {
                  // 使用 MapTrackerGoal
                  map_name: MapName,
                  target: MapGoal,
                  ...mapTrackerExtraParams,
              }
            : {
                  // 使用 MapTrackerMove
                  map_name: MapName,
                  path: MapPath,
                  ...mapTrackerExtraParams,
              };

    return {
        MapAssertRecognition,
        MapAssertParam,
        RouteAction,
        RouteActionParam,
    };
}

export function createRouteResolver(routeConfig, options = {}) {
    const warn = options.warn || defaultWarn;
    const routeOverrides = buildRouteOverrideIndexes(routeConfig, warn);

    return {
        resolve(mission) {
            const missionName = mission?.name?.zh_cn || mission?.missionId || "UnknownMission";
            const override = getRouteOverride(mission, routeOverrides);
            const QuickTeleport = override?.QuickTeleport === true;
            const hasMapPath = !isFieldMissing(override?.MapPath);
            const hasMapTarget = !isFieldMissing(override?.MapTarget);
            const hasMapGoal = !isFieldMissing(override?.MapGoal);
            const navFieldsPresent = collectNavRouteFields(override);
            const hasNavRoute = hasCompleteNavRoute(override);
            const navigationConfigCount = [
                hasMapPath,
                hasMapTarget,
                hasMapGoal,
            ].filter(Boolean).length;
            const isDirectPhoto =
                isFieldMissing(override?.MapAssert) && navFieldsPresent.length === 0 && navigationConfigCount === 0;
            const canSkipMapAssert = QuickTeleport && navigationConfigCount === 1;

            const resolved = {};
            const missingFields = collectMissingRouteFields(override);
            for (const key of ROUTE_RENDER_FIELDS) {
                const overrideValue = override?.[key];
                if (key === "EnterMap" && QuickTeleport) {
                    resolved[key] = isFieldMissing(overrideValue) ? UNREACHABLE_ROUTE_PLACEHOLDER[key] : overrideValue;
                    continue;
                }
                if (key === "MapAssert" && (canSkipMapAssert || isDirectPhoto)) {
                    resolved[key] = isFieldMissing(overrideValue) ? UNREACHABLE_ROUTE_PLACEHOLDER[key] : overrideValue;
                    continue;
                }
                if (key === "MapName" && isDirectPhoto) {
                    resolved[key] = isFieldMissing(overrideValue) ? UNREACHABLE_ROUTE_PLACEHOLDER[key] : overrideValue;
                    continue;
                }
                if (isFieldMissing(overrideValue)) {
                    resolved[key] = UNREACHABLE_ROUTE_PLACEHOLDER[key];
                } else {
                    resolved[key] = overrideValue;
                }
            }

            const {EnterMap, MapName, MapAssert, CameraSwipeDirection} = resolved;
            const MapPath =
                navigationConfigCount === 1 && hasMapPath ? override.MapPath : UNREACHABLE_ROUTE_PLACEHOLDER.MapPath;
            const MapTarget =
                navigationConfigCount === 1 && hasMapTarget
                    ? override.MapTarget
                    : UNREACHABLE_ROUTE_PLACEHOLDER.MapTarget;
            const MapTargetTier =
                navigationConfigCount === 1 && hasMapTarget && !isFieldMissing(override?.MapTargetTier)
                    ? override.MapTargetTier
                    : UNREACHABLE_ROUTE_PLACEHOLDER.MapTargetTier;
            const MapTargetDeckY =
                navigationConfigCount === 1 && hasMapTarget && !isFieldMissing(override?.MapTargetDeckY)
                    ? override.MapTargetDeckY
                    : UNREACHABLE_ROUTE_PLACEHOLDER.MapTargetDeckY;
            const MapGoal =
                navigationConfigCount === 1 && hasMapGoal ? override.MapGoal : UNREACHABLE_ROUTE_PLACEHOLDER.MapGoal;
            const CameraMaxHit = override?.CameraMaxHit ?? CAMERA_MAX_HIT_DEFAULT;
            const Replace = override?.Replace ?? [];
            const NoEnsureInitialMovementState = override?.NoEnsureInitialMovementState ?? false;
            const heading = normalizeHeading(override?.Heading, mission, missionName, warn);
            const isAdapted = override != null && missingFields.length === 0;

            if (override != null && missingFields.length > 0) {
                warn(
                    `[EnvironmentMonitoring] 任务 ${sanitizeDisplayName(missionName)} (${mission.missionId}) 路线条目缺失字段: ${missingFields.join(", ")}。已使用默认值，请补全 routes.json。`,
                );
            }

            if (!isAdapted) {
                warn(
                    `[EnvironmentMonitoring] 任务 ${sanitizeDisplayName(missionName)} (${mission.missionId}) 尚未适配路线，仅接取并追踪。`,
                );
            }

            return {
                override,
                isAdapted,
                missingFields,
                EnterMap,
                MapName,
                MapAssert,
                MapPath,
                MapTarget,
                MapTargetTier,
                MapTargetDeckY,
                MapGoal,
                CameraSwipeDirection,
                CameraMaxHit,
                Replace,
                NoEnsureInitialMovementState,
                QuickTeleport,
                IsDirectPhoto: isAdapted && isDirectPhoto,
                // NavPath 路线传送后交给 MapNavigateAction 自行接管落点，不再复核起点
                ShouldAssertAfterTeleport:
                    !isDirectPhoto && !hasNavRoute && (navigationConfigCount !== 1 || (hasMapPath && !QuickTeleport)),
                ...heading,
                ...buildNavigationParams({
                    MapName,
                    MapAssert,
                    MapPath,
                    MapTarget,
                    MapTargetTier,
                    MapTargetDeckY,
                    MapGoal,
                    NavZoneId: isFieldMissing(override?.NavZoneId)
                        ? UNREACHABLE_ROUTE_PLACEHOLDER.NavZoneId
                        : override.NavZoneId,
                    NavAssert: isFieldMissing(override?.NavAssert)
                        ? UNREACHABLE_ROUTE_PLACEHOLDER.MapAssert
                        : override.NavAssert,
                    NavPath: override?.NavPath,
                    NoEnsureInitialMovementState,
                    hasMapTarget: navigationConfigCount === 1 && hasMapTarget,
                    hasMapGoal: navigationConfigCount === 1 && hasMapGoal,
                    hasNavRoute,
                    isDirectPhoto,
                    heading,
                }),
            };
        },

        warnUnusedRouteOverrides() {
            for (const item of routeConfig) {
                if (isFieldMissing(item.MissionId)) {
                    continue;
                }
                if (routeOverrides.used.has(item)) {
                    continue;
                }
                const label = item.MissionId || item.Name || "<unknown>";
                warn(
                    `[EnvironmentMonitoring] routes.json 条目 ${label} 未匹配到当前游戏数据，请检查 MissionId 是否仍然有效。`,
                );
            }
        },
    };
}
