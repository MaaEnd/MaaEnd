/**
 * Parse cpp-algo MaaFramework logs into MapNavigator runs that can be inspected on
 * the existing basemap. The parser is deliberately independent from DOM APIs so a
 * real log fragment can be covered by Node tests.
 *
 * @module log_analysis
 */

const REQUEST_PREFIX = "[req=";
const REQUEST_SUFFIX = "] [ipc_addr_=";
const OBSERVED_MIN_STEP = 1.0;

const REASON_TEXT = Object.freeze({
  "the walk is too short for any zipline to pay off": "路程太短，上索固定开销无法回本",
  "no reachable pair of ziplines leads anywhere useful": "没有可达且方向有用的滑索组合",
  "no zipline route beats walking": "滑索总代价不低于步行",
  "no feasible zipline bridge to target": "没有滑索链能完整桥接不连通路段",
  "no powered ziplines recorded in this zone": "当前区域没有已记录且供电的滑索",
  "not one zipline here can be walked up to": "没有滑索端点可从导航网格走到",
  "no zipline calibration on disk": "缺少滑索坐标标定",
  "this zone is not calibrated": "当前区域没有滑索标定",
});

const NAVIGATION_REASON_TEXT = Object.freeze({
  route_tail_without_final_success: "路线已经耗尽，但没有确认抵达最终目标",
  localization_lost_timeout: "定位持续丢失，重新定位超时",
  localization_thrash: "定位反复丢失与恢复，路线始终没有推进",
  zipline_recovery_localization_timeout: "滑索恢复时无法获得稳定的可走面位置",
  zipline_recovery_route_unavailable: "滑索恢复后找不到可接回的剩余路线",
  crosstier_escape_stalled: "跨层脱困路线持续没有进展",
  river_fall_recovery_timeout: "落水恢复持续没有进展并超时",
  offroute_wedge_timeout: "偏离路线后持续没有路线进展，解卡超时",
  dynamic_recovery_timeout: "动态解卡持续没有进展并超时",
  no_progress_timeout: "导航持续没有进展并超时",
  portal_transit_timeout: "等待区域切换超时",
  transfer_wait_timeout: "等待传送完成超时",
  heading_turn_failed: "调整朝向失败",
  dig_context_missing: "缺少执行挖掘所需的 Pipeline 上下文",
  dig_dispatch_failed: "挖掘子任务派发失败",
});

const ZIPLINE_INCIDENT_TEXT = Object.freeze({
  zipline_target_missing: "滑索节点缺少落点",
  zipline_no_context: "缺少识别上索提示所需的 Pipeline 上下文",
  zipline_prompt_missing: "滑索架旁没有识别到上索提示",
  zipline_mount_failed: "按下交互键后仍未成功登上滑索架",
  zipline_aim_failed: "滑索瞄准落点失败",
  zipline_ride_timeout: "滑索飞行定位超时",
  zipline_launch_exhausted: "多次尝试后仍未从滑索架发射",
  zipline_landed_off_target: "滑索停在了目标落点之外",
  zipline_rode_back: "滑索误乘回上索点",
  zipline_unreachable: "多次重规划后仍无法抵达上索点",
  zipline_recovery_timeout: "卡在滑索架旁，解卡超时后放弃滑索链",
});

function timestampOf(line) {
  const match = /^\[([^\]]+)\]/.exec(line);
  return match ? match[1] : "";
}

function escapedRegex(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function valueOf(line, name) {
  const match = new RegExp(`\\[${escapedRegex(name)}=([^\\]]*)\\]`).exec(line);
  return match ? match[1] : null;
}

function messageAfter(line, marker) {
  const markerAt = line.indexOf(marker);
  if (markerAt < 0) return "";
  const tail = line.slice(markerAt + marker.length).trim();
  const fieldAt = tail.search(/\s\[[^=\]]+=/);
  return (fieldAt < 0 ? tail : tail.slice(0, fieldAt)).trim();
}

function selectedValues(line, names) {
  const result = {};
  for (const name of names) {
    const raw = valueOf(line, name);
    if (raw === null) continue;
    const number = Number(raw);
    result[name] = Number.isFinite(number) ? number : raw;
  }
  return result;
}

function numberValue(line, name) {
  const raw = valueOf(line, name);
  if (raw === null) return null;
  const value = Number(raw);
  return Number.isFinite(value) ? value : null;
}

function boolValue(line, name) {
  const raw = valueOf(line, name);
  if (raw === "true") return true;
  if (raw === "false") return false;
  return null;
}

function pointDistance(lhs, rhs) {
  return Math.hypot(lhs[0] - rhs[0], lhs[1] - rhs[1]);
}

function normalizedTowerPoints(value) {
  if (!Array.isArray(value)) return [];
  return value
    .filter((point) => Array.isArray(point) && point.length >= 2)
    .map((point) => point.slice(0, 3).map(Number))
    .filter((point) => point.length >= 2 && point.every(Number.isFinite));
}

function arrayValue(line, name) {
  const marker = `[${name}=`;
  const markerAt = line.indexOf(marker);
  if (markerAt < 0) return null;
  const start = markerAt + marker.length;
  if (line[start] !== "[") return null;

  let depth = 0;
  for (let i = start; i < line.length; i += 1) {
    if (line[i] === "[") depth += 1;
    else if (line[i] === "]") {
      depth -= 1;
      if (depth === 0) {
        try {
          return JSON.parse(line.slice(start, i + 1));
        } catch {
          return null;
        }
      }
    }
  }
  return null;
}

function parseRequest(line) {
  if (!line.includes('"custom_action_name":"MapNavigateAction"')) {
    return null;
  }
  const begin = line.indexOf(REQUEST_PREFIX);
  const end = line.indexOf(REQUEST_SUFFIX, begin + REQUEST_PREFIX.length);
  if (begin < 0 || end < 0) return null;
  try {
    const request = JSON.parse(line.slice(begin + REQUEST_PREFIX.length, end));
    const param = JSON.parse(request.custom_action_param || "{}");
    return {request, param};
  } catch {
    return null;
  }
}

function authoredPath(path) {
  if (!Array.isArray(path)) return [];
  const result = [];
  let zone = "";
  for (const point of path) {
    if (point && typeof point === "object" && !Array.isArray(point) && point.action === "ZONE") {
      zone = String(point.zone_id || point.zoneId || "");
      continue;
    }
    let target = null;
    if (Array.isArray(point)) target = point;
    else if (point && typeof point === "object" && Array.isArray(point.target)) target = point.target;
    if (!target || target.length < 2) continue;
    const x = Number(target[0]);
    const y = Number(target[1]);
    if (!Number.isFinite(x) || !Number.isFinite(y)) continue;
    result.push({
      point: [x, y],
      zone,
      targetTier:
        point && typeof point === "object" && !Array.isArray(point)
          ? String(point.target_tier || point.targetTier || "")
          : "",
      action:
        point && typeof point === "object" && !Array.isArray(point)
          ? String(point.action || "RUN")
          : String(target[2] || "RUN"),
    });
  }
  return result;
}

function newRun(parsed, line, sourceName, index) {
  const {request, param} = parsed;
  const path = authoredPath(param.path);
  return {
    id: `${sourceName}:${index}`,
    sourceName,
    timestamp: timestampOf(line),
    endTimestamp: "",
    nodeName: String(request.node_name || "MapNavigateAction"),
    taskId: request.task_id ?? null,
    zone: "",
    _fallbackZone: String(param.map_name || (path.find((entry) => entry.zone) || {}).zone || ""),
    zipRequested: param.zip === true,
    authoredPath: path,
    authoredPoints: path.map((entry) => entry.point),
    walks: [],
    observedWalks: [],
    ziplines: [],
    decisions: [],
    incidents: [],
    failure: null,
    completed: null,
    _pendingWalk: null,
    _pendingPick: null,
    _observedWalk: [],
    _observedTail: null,
    _observedZone: "",
    _ziplineInFlight: false,
    _ziplineLastPosition: null,
  };
}

function flushObservedWalk(run) {
  const points = run._observedWalk;
  const tail = run._observedTail;
  if (points.length && tail) {
    const tailDistance = pointDistance(points[points.length - 1], tail.point);
    if (tailDistance >= OBSERVED_MIN_STEP || (points.length >= 2 && tailDistance > 1e-6)) {
      points.push(tail.point);
    }
  }
  if (points.length >= 2) run.observedWalks.push(points);
  run._observedWalk = [];
  run._observedTail = null;
  run._observedZone = "";
}

function appendObservedPosition(run, sample) {
  if (run._observedZone && sample.zone && sample.zone !== run._observedZone) {
    flushObservedWalk(run);
  }
  run._observedZone ||= sample.zone;
  run._observedTail = sample;
  const points = run._observedWalk;
  if (!points.length || pointDistance(points[points.length - 1], sample.point) >= OBSERVED_MIN_STEP) {
    points.push(sample.point);
  }
}

function addObservedPosition(run, line) {
  const point = [
    numberValue(line, "position.x"),
    numberValue(line, "position.y"),
  ];
  const valid =
    numberValue(line, "status") === 0 && boolValue(line, "position.isHeld") !== true && point.every(Number.isFinite);
  if (!valid) {
    if (run._ziplineInFlight) run._ziplineLastPosition = null;
    else flushObservedWalk(run);
    return;
  }

  const sample = {
    point,
    zone: valueOf(line, "position.zoneId") || "",
  };
  if (run._ziplineInFlight) {
    run._ziplineLastPosition = sample;
    return;
  }
  appendObservedPosition(run, sample);
}

function startZiplineRide(run) {
  flushObservedWalk(run);
  run._ziplineInFlight = true;
  run._ziplineLastPosition = null;
}

function finishZiplineRide(run, resumeAtLastPosition = true) {
  if (!run._ziplineInFlight) return;
  run._ziplineInFlight = false;
  const landing = run._ziplineLastPosition;
  run._ziplineLastPosition = null;
  if (resumeAtLastPosition && landing) appendObservedPosition(run, landing);
}

function addWalk(run, line) {
  const points = arrayValue(line, "navmesh_path_points");
  const cost = numberValue(line, "route_result.cost");
  if (!Array.isArray(points) || points.length < 2 || cost === null) return;
  const normalized = points
    .filter((point) => Array.isArray(point) && point.length >= 2)
    .map((point) => [Number(point[0]), Number(point[1])])
    .filter((point) => point.every(Number.isFinite));
  if (normalized.length < 2) return;

  const walk = {
    timestamp: timestampOf(line),
    points: normalized,
    cost,
    decision: "pending",
    reason: "",
  };
  run.walks.push(walk);
  run._pendingWalk = walk;
  run.zone ||= valueOf(line, "state.navmesh_zone") || "";
}

function addDecision(run, kind, line, detail = {}) {
  run.decisions.push({kind, timestamp: timestampOf(line), ...detail});
}

function addZiplineIncident(run, line) {
  const reason = valueOf(line, "reason") || "";
  const position = [numberValue(line, "ctx.position->x"), numberValue(line, "ctx.position->y")];
  run.incidents.push({
    kind: "zipline-abandoned",
    timestamp: timestampOf(line),
    reason,
    text: ZIPLINE_INCIDENT_TEXT[reason] || reason || "运行时放弃了滑索链",
    detail: valueOf(line, "detail") || "",
    dropped: numberValue(line, "dropped"),
    position: position.every(Number.isFinite) ? position : null,
  });
}

function addFailureTransition(run, line) {
  const reason = valueOf(line, "reason") || "";
  const previous = run.failure || {};
  run.failure = {
    ...previous,
    timestamp: timestampOf(line),
    reason,
    text: NAVIGATION_REASON_TEXT[reason] || reason || "导航运行失败",
    fromPhase: valueOf(line, "from_phase_name") || "",
    currentNodeIndex: numberValue(line, "current_node_idx_"),
    pathOriginIndex: numberValue(line, "path_origin_index_"),
    message: previous.message || "",
    metrics: previous.metrics || {},
  };
}

function addFailureDetail(run, line) {
  const previous = run.failure || {};
  run.failure = {
    timestamp: previous.timestamp || timestampOf(line),
    reason: previous.reason || "",
    text: previous.text || "导航运行失败",
    fromPhase: previous.fromPhase || "",
    currentNodeIndex: previous.currentNodeIndex ?? null,
    pathOriginIndex: previous.pathOriginIndex ?? null,
    message: messageAfter(line, "FailNavigation]"),
    metrics: selectedValues(line, ["current_distance", "yaw_error", "stalled_ms"]),
  };
}

function rejectOrChooseWalk(run, line) {
  const reason = valueOf(line, "why") || "";
  const walk = run._pendingWalk;
  if (walk && walk.decision === "pending") {
    walk.decision = "walk";
    walk.reason = reason;
    run._pendingWalk = null;
  }
  addDecision(run, "walk", line, {
    reason,
    text: REASON_TEXT[reason] || reason || "本段选择步行",
    cost: walk ? walk.cost : null,
  });
  run.zone ||= valueOf(line, "navmesh_zone") || "";
}

function rememberZiplinePick(run, line) {
  run._pendingPick = {
    timestamp: timestampOf(line),
    walkingBaselineAvailable: boolValue(line, "walking_baseline_available") === true,
    baselineLength: numberValue(line, "baseline_length"),
    cost: numberValue(line, "best->cost"),
    towerCount: numberValue(line, "best->towers.size()"),
    first: [numberValue(line, "best->towers.front().x"), numberValue(line, "best->towers.front().y")],
    last: [numberValue(line, "best->towers.back().x"), numberValue(line, "best->towers.back().y")],
  };
}

function addZipline(run, line) {
  const picked = run._pendingPick || {};
  const lineBaselineAvailable = boolValue(line, "walking_baseline_available");
  const baselineAvailable =
    lineBaselineAvailable === null ? picked.walkingBaselineAvailable === true : lineBaselineAvailable;
  let baselineWalk = null;
  if (baselineAvailable && run._pendingWalk && run._pendingWalk.decision === "pending") {
    baselineWalk = run._pendingWalk;
    baselineWalk.decision = "baseline";
    baselineWalk.reason = "zipline selected";
    run._pendingWalk = null;
  }

  const mount = [numberValue(line, "mount.x"), numberValue(line, "mount.y")];
  const last = [numberValue(line, "route->towers.back().x"), numberValue(line, "route->towers.back().y")];
  const chain = {
    chainIndex: run.ziplines.length,
    timestamp: timestampOf(line),
    walkingBaselineAvailable: baselineAvailable,
    baselineLength: baselineAvailable ? (picked.baselineLength ?? (baselineWalk ? baselineWalk.cost : null)) : null,
    cost: numberValue(line, "route->cost") ?? picked.cost ?? null,
    towerCount: numberValue(line, "route->towers.size()") ?? picked.towerCount ?? null,
    mount: mount.every(Number.isFinite) ? mount : picked.first,
    last: last.every(Number.isFinite) ? last : picked.last,
    baselineWalk,
    launches: [],
    landings: [],
    landed: 0,
  };
  run.ziplines.push(chain);
  run._pendingPick = null;
  addDecision(run, "zipline", line, {
    chainIndex: chain.chainIndex,
    walkingBaselineAvailable: chain.walkingBaselineAvailable,
    baselineLength: chain.baselineLength,
    cost: chain.cost,
    towerCount: chain.towerCount,
    saving:
      Number.isFinite(chain.baselineLength) && Number.isFinite(chain.cost) ? chain.baselineLength - chain.cost : null,
    savingPercent:
      Number.isFinite(chain.baselineLength) && chain.baselineLength > 0 && Number.isFinite(chain.cost)
        ? ((chain.baselineLength - chain.cost) / chain.baselineLength) * 100
        : null,
  });
  run.zone ||= valueOf(line, "state.navmesh_zone") || "";
}

function pendingExecutionChain(run) {
  return run.ziplines.find((chain) => {
    const expected = Number.isFinite(chain.towerCount) ? Math.max(1, chain.towerCount - 1) : Infinity;
    return chain.launches.length < expected;
  });
}

function addLaunch(run, line) {
  startZiplineRide(run);
  const landing = [numberValue(line, "landing.x"), numberValue(line, "landing.y")];
  if (!landing.every(Number.isFinite)) return;
  const chain = pendingExecutionChain(run);
  if (!chain) return;
  chain.launches.push(landing);
}

function addLanding(run, line) {
  const chain = run.ziplines.find((candidate) => candidate.landed < candidate.launches.length);
  if (chain) {
    const landing = [numberValue(line, "landing.x"), numberValue(line, "landing.y")];
    chain.landings.push(landing.every(Number.isFinite) ? landing : chain.launches[chain.landed]);
    chain.landed += 1;
  }
  finishZiplineRide(run);
}

function closeRun(run, line, succeeded) {
  flushObservedWalk(run);
  for (const walk of run.walks) {
    if (walk.decision === "pending") walk.decision = "walk";
  }
  run._pendingWalk = null;
  run._pendingPick = null;
  run.zone ||= run._fallbackZone;
  run.endTimestamp = timestampOf(line);
  run.completed = succeeded === null && run.failure ? false : succeeded;
  delete run._pendingWalk;
  delete run._pendingPick;
  delete run._fallbackZone;
  delete run._observedWalk;
  delete run._observedTail;
  delete run._observedZone;
  delete run._ziplineInFlight;
  delete run._ziplineLastPosition;
}

function lineBelongsToRunEnd(run, line) {
  if (!line.includes(run.nodeName)) return null;
  if (line.includes("[msg=Node.Action.Succeeded]")) return true;
  if (line.includes("[msg=Node.Action.Failed]")) return false;
  return null;
}

/**
 * Parse one MaaFramework/cpp-algo log file.
 * @param {string} text
 * @param {string} [sourceName='maafw.log']
 * @returns {Array<Object>}
 */
export function parseMapNavigatorLog(text, sourceName = "maafw.log") {
  const runs = [];
  let current = null;
  for (const line of String(text || "").split(/\r?\n/)) {
    const request = parseRequest(line);
    if (request) {
      if (current) closeRun(current, line, null);
      current = newRun(request, line, sourceName, runs.length);
      runs.push(current);
      continue;
    }
    if (!current) continue;

    if (line.includes("PositionProvider::Capture") && line.includes("MapLocator")) {
      addObservedPosition(current, line);
    } else if (line.includes("NAVMESH generated path.")) addWalk(current, line);
    else if (line.includes("ZiplineRoute: picked")) rememberZiplinePick(current, line);
    else if (line.includes("Expanded NAVMESH waypoint via zipline.")) addZipline(current, line);
    else if (line.includes("ZiplineRoute: walking this leg instead.")) rejectOrChooseWalk(current, line);
    else if (line.includes("ZiplineRoute: no bridge for the disconnected walking leg.")) {
      const reason = valueOf(line, "why") || "";
      addDecision(current, "bridge-failed", line, {reason, text: REASON_TEXT[reason] || reason});
      current.zone ||= valueOf(line, "navmesh_zone") || "";
    } else if (line.includes("Global authored route unavailable; replaying authored hints.")) {
      addDecision(current, "authored-replay", line, {text: "整段直达失败，回放作者路径并逐段规划"});
    } else if (line.includes("Action: ZIPLINE launched toward the landing point.")) addLaunch(current, line);
    else if (line.includes("Action: ZIPLINE ride landed.")) addLanding(current, line);
    else if (line.includes("Action: ZIPLINE given up, recovering from a fresh position.")) {
      addZiplineIncident(current, line);
    }

    if (line.includes("Phase transition.")) {
      if (line.includes("[from_phase_name=WaitZipline]")) finishZiplineRide(current, false);
      if (line.includes("[to_phase_name=Failed]")) addFailureTransition(current, line);
    }
    if (line.includes("FailNavigation]")) addFailureDetail(current, line);

    const succeeded = lineBelongsToRunEnd(current, line);
    if (succeeded !== null) {
      closeRun(current, line, succeeded);
      current = null;
    }
  }
  if (current) closeRun(current, "", null);
  return runs.filter(
    (run) => run.authoredPoints.length || run.walks.length || run.observedWalks.length || run.ziplines.length,
  );
}

/** All base-map points needed to frame one parsed run. @param {Object} run @returns {number[][]} */
export function logRunPoints(run) {
  if (!run) return [];
  const points = [...(run.authoredPoints || [])];
  for (const walk of run.walks || []) points.push(...walk.points);
  for (const walk of run.observedWalks || []) points.push(...walk);
  for (const chain of run.ziplines || []) {
    if (Array.isArray(chain.mount)) points.push(chain.mount);
    if (Array.isArray(chain.last)) points.push(chain.last);
    points.push(...(chain.launches || []));
    points.push(...logZiplineTowers(chain).map((tower) => tower.point));
  }
  return points.filter((point) => Array.isArray(point) && point.length >= 2 && point.every(Number.isFinite));
}

/**
 * Tower positions observable in the existing runtime log: the mount, launch
 * targets, and final tower. A tower is confirmed only after execution reached it.
 * @param {Object} chain
 * @returns {Array<{index:number,point:number[],height:?number,confirmed:boolean}>}
 */
export function logZiplineTowers(chain) {
  if (!chain) return [];
  const points = normalizedTowerPoints([chain.mount, ...(chain.launches || []), chain.last]).filter(
    (point, index, all) => index === 0 || pointDistance(point, all[index - 1]) > 1e-6,
  );

  const launches = Array.isArray(chain.launches) ? chain.launches.length : 0;
  const landed = Number.isFinite(chain.landed) ? Math.max(0, chain.landed) : 0;
  const furthestConfirmed = launches ? Math.max(launches - 1, landed) : -1;
  return points.map((point, index) => ({
    index,
    point: point.slice(0, 2),
    height: point.length >= 3 ? point[2] : null,
    confirmed: index <= furthestConfirmed,
  }));
}

/**
 * Zipline segments that the runtime actually launched, plus the two straight endpoint
 * connections that can only be estimated from the selected walking baseline.
 * @param {Object} chain
 * @returns {{actual:Array<{from:number[],to:number[],landed:boolean}>, estimated:Array<{from:number[],to:number[],kind:string}>}}
 */
export function logZiplineGeometry(chain) {
  if (!chain) return {actual: [], estimated: []};
  const actual = [];
  let from = Array.isArray(chain.mount) ? chain.mount : null;
  for (let i = 0; from && i < (chain.launches || []).length; i += 1) {
    const to = chain.launches[i];
    if (!Array.isArray(to)) continue;
    actual.push({from, to, landed: i < (chain.landed || 0)});
    from = to;
  }

  const estimated = [];
  const baseline = chain.baselineWalk && chain.baselineWalk.points;
  if (Array.isArray(baseline) && baseline.length >= 2 && Array.isArray(chain.mount)) {
    estimated.push({from: baseline[0], to: chain.mount, kind: "approach"});
    const last = actual.length ? actual[actual.length - 1].to : chain.last;
    if (Array.isArray(last)) {
      estimated.push({from: last, to: baseline[baseline.length - 1], kind: "exit"});
    }
  }
  return {actual, estimated};
}

/** Human-readable reason from the runtime's stable raw identifier. @param {string} reason @returns {string} */
export function ziplineReasonText(reason) {
  return REASON_TEXT[reason] || reason || "本段选择步行";
}
