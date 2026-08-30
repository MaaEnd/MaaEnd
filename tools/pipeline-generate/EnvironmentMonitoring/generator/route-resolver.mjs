import {isFieldMissing, sanitizeDisplayName} from "./common.mjs";

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
    const navFieldsPresent = collectNavRouteFields(route);
    const hasNavRoute = hasCompleteNavRoute(route);
    const expectsNavRoute = navFieldsPresent.length > 0 || !isFieldMissing(route.FightAfterMove);
    const missingFields = [];

    if (!quickTeleport && isFieldMissing(route.EnterMap)) {
        missingFields.push("EnterMap");
    }
    if (expectsNavRoute && !hasNavRoute) {
        const hasAnyAssertField = NAV_ASSERT_FIELDS.some((field) => !isFieldMissing(route[field]));
        const requiredFields = quickTeleport && !hasAnyAssertField ? NAV_ROUTE_REQUIRED_FIELDS : NAV_ROUTE_FIELDS;
        missingFields.push(`${requiredFields.join("/")} 必须同时配置`);
    }

    return missingFields;
}

// 未适配任务不会进入寻路/拍照分支；这些值只用于渲染模板中不可达的路线节点。
// 断言矩形取 1×1 像素，角色永远落不进去，识别必然失败并转去传送分支。
const UNREACHABLE_ROUTE_PLACEHOLDER = {
    EnterMap: "SceneAnyEnterWorld",
    NavAssert: [
        0,
        0,
        1,
        1,
    ],
    NavZoneId: "Wuling_Base",
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

function buildRouteActionParam(routeNodes, heading) {
    const headingNodes = heading.HasHeading
        ? [
              {
                  action: "HEADING",
                  angle: heading.Heading,
              },
          ]
        : [];
    const path = [
        ...routeNodes,
        ...headingNodes,
    ];

    return {
        path:
            path.length > 0
                ? path
                : [
                      {
                          action: "HEADING",
                          angle: 0,
                      },
                  ],
    };
}

function buildNavigationParams({NavZoneId, NavAssert, NavPath, FightAfterMove, hasNavRoute, heading}) {
    // 1. 构建位置断言识别节点
    // 没有路线时用占位值，渲染出的节点本来就走不到。
    const MapAssertRecognition = "MapLocateAssertLocation";
    const MapAssertParam = {
        zone_id: NavZoneId,
        target: NavAssert,
    };

    // 2. 构建导航动作节点
    const RouteAction = "MapNavigateAction";
    const routeNodes = hasNavRoute ? NavPath : [];
    // 传送后直拍只有朝向节点；一个节点都没有时补零度朝向，让不可达节点的参数保持合法。
    const RouteActionParam = buildRouteActionParam(routeNodes, heading);
    const FightAfterMoveRouteActionParam = isFieldMissing(FightAfterMove)
        ? RouteActionParam
        : buildRouteActionParam(FightAfterMove, heading);

    return {
        MapAssertRecognition,
        MapAssertParam,
        RouteAction,
        RouteActionParam,
        FightAfterMoveRouteActionParam,
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
            const navFieldsPresent = collectNavRouteFields(override);
            const hasNavRoute = hasCompleteNavRoute(override);
            const isDirectPhoto = navFieldsPresent.length === 0;

            const resolved = {};
            const missingFields = collectMissingRouteFields(override);
            for (const key of ROUTE_RENDER_FIELDS) {
                const overrideValue = override?.[key];
                if (key === "EnterMap" && QuickTeleport) {
                    resolved[key] = isFieldMissing(overrideValue) ? UNREACHABLE_ROUTE_PLACEHOLDER[key] : overrideValue;
                    continue;
                }
                if (isFieldMissing(overrideValue)) {
                    resolved[key] = UNREACHABLE_ROUTE_PLACEHOLDER[key];
                } else {
                    resolved[key] = overrideValue;
                }
            }

            const {EnterMap} = resolved;
            const Replace = override?.Replace ?? [];
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
                Replace,
                QuickTeleport,
                IsDirectPhoto: isAdapted && isDirectPhoto,
                FightAfterMove: hasNavRoute && !isFieldMissing(override?.FightAfterMove),
                // NavPath 路线传送后交给 MapNavigateAction 自行接管落点，不再复核起点
                ShouldAssertAfterTeleport: !isDirectPhoto && !hasNavRoute,
                ...heading,
                ...buildNavigationParams({
                    NavZoneId: isFieldMissing(override?.NavZoneId)
                        ? UNREACHABLE_ROUTE_PLACEHOLDER.NavZoneId
                        : override.NavZoneId,
                    NavAssert: isFieldMissing(override?.NavAssert)
                        ? UNREACHABLE_ROUTE_PLACEHOLDER.NavAssert
                        : override.NavAssert,
                    NavPath: override?.NavPath,
                    FightAfterMove: override?.FightAfterMove,
                    hasNavRoute,
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
