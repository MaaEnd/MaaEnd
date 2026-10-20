import {normalizeZoneId} from "./model.js";

/**
 * Select the offline preview start and the authored targets sent to MapNavigator.
 * A manual start is preview-only, so every authored point remains a target. Without
 * one, the first authored point represents the already-reached start for compatibility.
 *
 * @param {Object[]} points current authored segment
 * @param {?{position:number[],positionZone:string}} explicitStart preview start in its original zone frame
 * @returns {{ok:true,position:number[],positionZone:string,startDeckY:?number,targets:Object[],explicit:boolean}|{ok:false,error:string}}
 */
export function buildEditPreviewPlan(points, explicitStart = null) {
  const authored = Array.isArray(points) ? points : [];
  if (explicitStart) {
    if (authored.length < 1) {
      return {ok: false, error: "请先添加至少一个目标路点再规划。"};
    }
    const positionZone = normalizeZoneId(explicitStart.positionZone);
    const position = Array.isArray(explicitStart.position) ? explicitStart.position.slice(0, 2) : [];
    if (position.length < 2 || !position.every(Number.isFinite) || !positionZone) {
      return {ok: false, error: "手动规划起点缺少有效坐标或区域。"};
    }
    return {
      ok: true,
      position,
      positionZone,
      startDeckY: null,
      targets: authored.slice(),
      explicit: true,
    };
  }

  if (authored.length < 2) {
    return {ok: false, error: "当前片段至少需要两个路点，或先在地图上设置规划起点。"};
  }
  const start = authored[0];
  const positionZone = normalizeZoneId(start.target_tier || start.zone);
  if (!positionZone) {
    return {ok: false, error: "当前片段缺少起点区域，无法交给运行时规划。"};
  }
  return {
    ok: true,
    position: [start.x, start.y],
    positionZone,
    // The first authored point is already reached in the compatibility preview mode,
    // so its chosen deck belongs to the preview start rather than a removed target.
    startDeckY: Number.isFinite(start.target_deck_y) ? start.target_deck_y : null,
    targets: authored.slice(1),
    explicit: false,
  };
}

/**
 * Normalize a successful runtime response into the overlay's route shape.
 * @param {Object} result `/api/route-preview` response
 * @returns {{points:Array, walk_segments:Array, zipline_segments:Array, diagnostics:Array, zipline:Object}}
 */
export function routePreviewData(result) {
  return {
    points: result.points || [],
    walk_segments: result.walk_segments || [],
    zipline_segments: result.zipline_segments || [],
    diagnostics: result.diagnostics || [],
    zipline: result.zipline || {},
  };
}

/**
 * User-facing summary shared by every runtime route preview.
 * @param {ReturnType<typeof routePreviewData>} route
 * @param {Object} result the raw response (`expanded_waypoints`)
 * @param {string} subject what was planned, leading the sentence
 * @returns {[string, string]} status text and color
 */
export function routePreviewStatus(route, result, subject) {
  const hops = route.zipline_segments.length;
  const expanded = result.expanded_waypoints || route.points.length;
  if (hops > 0) {
    return [`${subject}：采用 ${hops} 跳滑索，展开为 ${expanded} 个运行时路点。`, "#10b981"];
  }
  if (route.zipline.account_unknown) {
    return [`${subject}：没有可用于规划的账号记录，已回退为步行路线。`, "#f59e0b"];
  }
  if (route.zipline.no_data) {
    return [`${subject}：当前区域没有可用的滑索记录，已回退为步行路线。`, "#f59e0b"];
  }
  if (route.zipline.not_chosen) {
    return [`${subject}：滑索没有显著优于步行，运行时会采用当前步行路线。`, "#10b981"];
  }
  return [`${subject}：展开为 ${expanded} 个运行时路点。`, "#10b981"];
}

/**
 * A runtime preview (base px) as the overlay's `editPreview` view model.
 * @param {?ReturnType<typeof routePreviewData>} route
 * @param {?Object} failure runtime failure with its segment / gap endpoints
 * @param {(point:number[]) => number[]} project base px → display frame
 * @param {{diagnostics?:Array, debugOptions?:Object}} [extras] diagnostics already in the display
 *   frame, and which diagnostic layers to draw
 * @returns {?Object} null when there is neither a route nor a failure
 */
export function routePreviewForDisplay(route, failure, project, {diagnostics = [], debugOptions = {}} = {}) {
  if (!route && !failure) return null;
  const previewRoute = route || {};
  return {
    previewPoints: (previewRoute.points || []).map(project),
    segmentBreaks: [],
    hasRoute: !!route,
    waypoints: [],
    blindWalks: [],
    diagnostics,
    debugOptions,
    showPlannedPath: debugOptions.planned !== false,
    walkSegments: (previewRoute.walk_segments || []).map((segment) => segment.map(project)),
    ziplineSegments: (previewRoute.zipline_segments || [])
      .filter((segment) => Array.isArray(segment?.from) && Array.isArray(segment?.to))
      .map((segment) => ({
        ...segment,
        from: project(segment.from),
        to: project(segment.to),
        mount_spots: Array.isArray(segment.mount_spots) ? segment.mount_spots.filter(Array.isArray).map(project) : [],
      })),
    failure: failure
      ? {
          ...failure,
          segment_start: failure.segment_start ? project(failure.segment_start) : null,
          segment_goal: failure.segment_goal ? project(failure.segment_goal) : null,
          gap_start: failure.gap_start ? project(failure.gap_start) : null,
          gap_goal: failure.gap_goal ? project(failure.gap_goal) : null,
        }
      : null,
  };
}
