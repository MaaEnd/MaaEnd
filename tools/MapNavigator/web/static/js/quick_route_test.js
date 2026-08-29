/** Clone one temporary endpoint so UI updates cannot mutate a completed request. */
function cloneEndpoint(endpoint) {
    return {
        ...endpoint,
        position: endpoint.position.slice(0, 2),
    };
}

/** Whether an endpoint contains enough information for the runtime route-preview API. */
function isValidEndpoint(endpoint) {
    return (
        endpoint &&
        Array.isArray(endpoint.position) &&
        endpoint.position.length >= 2 &&
        endpoint.position.slice(0, 2).every(Number.isFinite) &&
        typeof endpoint.positionZone === "string" &&
        endpoint.positionZone.length > 0 &&
        Number.isFinite(endpoint.geometryZoneId)
    );
}

/**
 * Advance the two-click quick-test state. A completed S/G pair starts over on the
 * next click; the second endpoint must remain on the same navmesh geometry.
 *
 * @param {?{start:Object,goal:?Object}} current
 * @param {Object} endpoint
 * @returns {{ok:true,state:{start:Object,goal:?Object},shouldPlan:boolean}|{ok:false,error:string}}
 */
export function advanceQuickRouteTest(current, endpoint) {
    if (!isValidEndpoint(endpoint)) {
        return {ok: false, error: "测试点缺少有效坐标或区域。"};
    }
    if (!current?.start || current.goal) {
        return {
            ok: true,
            state: {start: cloneEndpoint(endpoint), goal: null},
            shouldPlan: false,
        };
    }
    if (current.start.geometryZoneId !== endpoint.geometryZoneId) {
        return {ok: false, error: "测试起点和终点必须位于同一张 navmesh 底图。"};
    }
    return {
        ok: true,
        state: {start: cloneEndpoint(current.start), goal: cloneEndpoint(endpoint)},
        shouldPlan: true,
    };
}

/**
 * Build the same request shape used by the authored-path preview, but with one
 * temporary NAVMESH target that never enters the editor's route state.
 *
 * @param {?{start:Object,goal:?Object}} state
 * @param {{zip?:boolean}} [options]
 * @returns {{ok:true,request:Object}|{ok:false,error:string}}
 */
export function buildQuickRouteTestRequest(state, {zip = false} = {}) {
    const start = state?.start;
    const goal = state?.goal;
    if (!isValidEndpoint(start) || !isValidEndpoint(goal)) {
        return {ok: false, error: "请先依次设置测试起点和终点。"};
    }
    if (start.geometryZoneId !== goal.geometryZoneId) {
        return {ok: false, error: "测试起点和终点必须位于同一张 navmesh 底图。"};
    }

    const target = {
        action: "NAVMESH",
        target: goal.position.slice(0, 2),
    };
    if (goal.targetTier) target.target_tier = goal.targetTier;

    const customActionParam = {path: [target]};
    if (zip) customActionParam.zip = true;
    return {
        ok: true,
        request: {
            position: start.position.slice(0, 2),
            position_zone: start.positionZone,
            custom_action_param: customActionParam,
        },
    };
}
