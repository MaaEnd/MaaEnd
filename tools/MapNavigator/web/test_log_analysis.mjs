import assert from "node:assert/strict";
import test from "node:test";

import {
  logRunPoints,
  logZiplineGeometry,
  logZiplineTowers,
  parseMapNavigatorLog,
  ziplineReasonText,
} from "./static/js/log_analysis.js";

const request = {
  _CustomActionRequest: 1,
  custom_action_name: "MapNavigateAction",
  custom_action_param: JSON.stringify({
    path: [
      {action: "ZONE", zone_id: "Wuling_Base"},
      {action: "NAVMESH", target: [965.5, 1803.38]},
      {action: "NAVMESH", target: [724.98, 1596.8]},
    ],
    zip: true,
  }),
  node_name: "AutoDeliveryRouteDestinationWithZipline",
  task_id: 200000001,
};

function position(timestamp, x, y, {status = 0, held = false} = {}) {
  return `[${timestamp}][INF][position_provider.cpp][mapnavigator::PositionProvider::Capture] MapLocator [status=${status}] [position.zoneId=Wuling_Base] [position.x=${x}] [position.y=${y}] [position.isHeld=${held}]`;
}

const lines = [
  `[2026-08-26 11:42:20.323][INF][AgentServer.cpp] [req=${JSON.stringify(request)}] [ipc_addr_=ipc://test.sock]`,
  "[2026-08-26 11:42:50.039][INF][navmesh_path_expander.cpp] NAVMESH generated path. [state.navmesh_zone=map02base] [route_result.cost=467.555411] [navmesh_path_points=[[965.5,1803.38],[900,1700],[724.98,1596.8]]]",
  "[2026-08-26 11:42:53.239][INF][zipline_leg_planner.cpp] ZiplineRoute: picked [walking_baseline_available=true] [baseline_length=467.555411] [best->cost=355.899450] [best->towers.size()=4] [best->towers.front().x=955.5] [best->towers.front().y=1777.5] [best->towers.back().x=912] [best->towers.back().y=1584]",
  "[2026-08-26 11:42:53.239][INF][navmesh_path_expander.cpp] Expanded NAVMESH waypoint via zipline. [state.navmesh_zone=map02base] [walking_baseline_available=true] [route->cost=355.899450] [route->towers.size()=4] [mount.x=955.5] [mount.y=1777.5] [route->towers.back().x=912] [route->towers.back().y=1584]",
  "[2026-08-26 11:43:11.644][INF][zipline_action.cpp] Action: ZIPLINE launched toward the landing point. [landing.x=940.5] [landing.y=1699.5]",
  "[2026-08-26 11:43:18.812][INF][zipline_action.cpp] Action: ZIPLINE ride landed.",
  "[2026-08-26 11:43:20.128][INF][zipline_action.cpp] Action: ZIPLINE launched toward the landing point. [landing.x=957] [landing.y=1653]",
  "[2026-08-26 11:43:24.495][INF][zipline_action.cpp] Action: ZIPLINE ride landed.",
  "[2026-08-26 11:43:25.930][INF][zipline_action.cpp] Action: ZIPLINE launched toward the landing point. [landing.x=912] [landing.y=1584]",
  "[2026-08-26 11:43:33.696][INF][zipline_action.cpp] Action: ZIPLINE ride landed.",
  "[2026-08-26 11:43:34.000][INF][navmesh_path_expander.cpp] NAVMESH generated path. [state.navmesh_zone=map02base] [route_result.cost=22.381149] [navmesh_path_points=[[543.38,1270.17],[538.031,1250.27]]]",
  "[2026-08-26 11:43:34.010][INF][zipline_leg_planner.cpp] ZiplineRoute: walking this leg instead. [why=the walk is too short for any zipline to pay off] [navmesh_zone=map02base]",
  '[2026-08-26 11:45:38.041][INF][EventDispatcher.hpp] [msg=Node.Action.Succeeded] [details={"name":"AutoDeliveryRouteDestinationWithZipline"}]',
];

test("parses one selected zipline chain and its actual launches", () => {
  const [run] = parseMapNavigatorLog(lines.join("\n"), "cpp-algo-maafw.log");

  assert.ok(run);
  assert.equal(run.nodeName, "AutoDeliveryRouteDestinationWithZipline");
  assert.equal(run.zone, "map02base");
  assert.equal(run.zipRequested, true);
  assert.equal(run.completed, true);
  assert.equal(run.failure, null);
  assert.deepEqual(run.authoredPoints, [
    [965.5, 1803.38],
    [724.98, 1596.8],
  ]);
  assert.equal(run.authoredPath[0].zone, "Wuling_Base");

  assert.equal(run.walks.length, 2);
  assert.equal(run.walks[0].decision, "baseline");
  assert.equal(run.walks[1].decision, "walk");

  assert.equal(run.ziplines.length, 1);
  assert.equal(run.ziplines[0].towerCount, 4);
  assert.equal(run.ziplines[0].landed, 3);
  assert.deepEqual(run.ziplines[0].launches, [
    [940.5, 1699.5],
    [957, 1653],
    [912, 1584],
  ]);
  assert.ok(Math.abs(run.decisions.find((decision) => decision.kind === "zipline").saving - 111.655961) < 1e-9);
  assert.ok(Math.abs(run.decisions.find((decision) => decision.kind === "zipline").savingPercent - 23.880798) < 1e-6);

  const geometry = logZiplineGeometry(run.ziplines[0]);
  assert.equal(geometry.actual.length, 3);
  assert.ok(geometry.actual.every((segment) => segment.landed));
  assert.deepEqual(geometry.estimated, [
    {from: [965.5, 1803.38], to: [955.5, 1777.5], kind: "approach"},
    {from: [912, 1584], to: [724.98, 1596.8], kind: "exit"},
  ]);

  assert.deepEqual(logZiplineTowers(run.ziplines[0]), [
    {index: 0, point: [955.5, 1777.5], height: null, confirmed: true},
    {index: 1, point: [940.5, 1699.5], height: null, confirmed: true},
    {index: 2, point: [957, 1653], height: null, confirmed: true},
    {index: 3, point: [912, 1584], height: null, confirmed: true},
  ]);
});

test("marks only reached towers as confirmed when a selected chain stops partway", () => {
  const partial = [
    lines[0],
    lines[1],
    lines[2],
    lines[3],
    lines[4],
    "[2026-08-26 11:43:16.000][INF][navigation_session.cpp] Phase transition. [from_phase_name=WaitZipline] [to_phase_name=Recover] [reason=zipline_ride_failed]",
    '[2026-08-26 11:43:17.000][INF][EventDispatcher.hpp] [msg=Node.Action.Failed] [details={"name":"AutoDeliveryRouteDestinationWithZipline"}]',
  ];
  const [run] = parseMapNavigatorLog(partial.join("\n"));

  assert.equal(run.ziplines[0].chainIndex, 0);
  assert.deepEqual(logZiplineTowers(run.ziplines[0]), [
    {index: 0, point: [955.5, 1777.5], height: null, confirmed: true},
    {index: 1, point: [940.5, 1699.5], height: null, confirmed: false},
    {index: 2, point: [912, 1584], height: null, confirmed: false},
  ]);
});

test("extracts the terminal navigation reason and nearest zipline recovery event", () => {
  const failed = [
    lines[0],
    "[2026-08-27 14:57:10.818][WRN][zipline_action.cpp][mapnavigator::semantic_nodes::AbandonZipline] Action: ZIPLINE given up, recovering from a fresh position. [reason=zipline_aim_failed] [detail=could not aim the view at the landing point] [dropped=16] [ctx.position->x=964.970000] [ctx.position->y=1827.190000]",
    "[2026-08-27 14:57:42.247][INF][navigation_session.cpp][mapnavigator::NavigationSession::UpdatePhase] Phase transition. [from_phase_name=Navigate] [to_phase_name=Failed] [reason=offroute_wedge_timeout] [current_node_idx_=0] [path_origin_index_=13]",
    "[2026-08-27 14:57:42.247][ERR][navigation_state_machine.cpp][mapnavigator::NavigationStateMachine::FailNavigation] Off-route with no route progress past the wedge timeout; terminating so the pipeline can retry. [current_distance=3.932283] [yaw_error=-130.386859] [stalled_ms=0]",
    '[2026-08-27 14:57:42.258][INF][EventDispatcher.hpp] [msg=Node.Action.Failed] [details={"name":"AutoDeliveryRouteDestinationWithZipline"}]',
  ];
  const [run] = parseMapNavigatorLog(failed.join("\n"));

  assert.equal(run.completed, false);
  assert.deepEqual(run.failure, {
    timestamp: "2026-08-27 14:57:42.247",
    reason: "offroute_wedge_timeout",
    text: "偏离路线后持续没有路线进展，解卡超时",
    fromPhase: "Navigate",
    currentNodeIndex: 0,
    pathOriginIndex: 13,
    message: "Off-route with no route progress past the wedge timeout; terminating so the pipeline can retry.",
    metrics: {
      current_distance: 3.932283,
      yaw_error: -130.386859,
      stalled_ms: 0,
    },
  });
  assert.deepEqual(run.incidents, [
    {
      kind: "zipline-abandoned",
      timestamp: "2026-08-27 14:57:10.818",
      reason: "zipline_aim_failed",
      text: "滑索瞄准落点失败",
      detail: "could not aim the view at the landing point",
      dropped: 16,
      position: [964.97, 1827.19],
    },
  ]);
});

test("does not treat a disconnected walking baseline as a zero-cost comparison", () => {
  const disconnected = [
    lines[0],
    lines[1].replace("route_result.cost=467.555411", "route_result.cost=0"),
    lines[2]
      .replace("walking_baseline_available=true", "walking_baseline_available=false")
      .replace("baseline_length=467.555411", "baseline_length=0"),
    lines[3].replace("walking_baseline_available=true", "walking_baseline_available=false"),
    lines[lines.length - 1],
  ];
  const [run] = parseMapNavigatorLog(disconnected.join("\n"));
  const decision = run.decisions.find((entry) => entry.kind === "zipline");

  assert.equal(run.ziplines[0].walkingBaselineAvailable, false);
  assert.equal(run.ziplines[0].baselineLength, null);
  assert.equal(decision.walkingBaselineAvailable, false);
  assert.equal(decision.saving, null);
  assert.equal(decision.savingPercent, null);
});

test("frames author, walk, and zipline coordinates", () => {
  const [run] = parseMapNavigatorLog(lines.join("\n"));
  const points = logRunPoints(run);
  assert.ok(points.some(([x, y]) => x === 955.5 && y === 1777.5));
  assert.ok(points.some(([x, y]) => x === 538.031 && y === 1250.27));
});

test("extracts measured ground tracks without connecting zipline rides or held positions", () => {
  const traceLines = [
    lines[0],
    position("2026-08-26 11:42:21.000", 965.5, 1803.38),
    position("2026-08-26 11:42:22.000", 955.8, 1777.7),
    "[2026-08-26 11:43:11.644][INF][zipline_action.cpp] Action: ZIPLINE launched toward the landing point. [landing.x=940.5] [landing.y=1699.5]",
    position("2026-08-26 11:43:15.000", 950, 1750),
    position("2026-08-26 11:43:18.800", 940.8, 1699.8),
    "[2026-08-26 11:43:18.812][INF][zipline_action.cpp] Action: ZIPLINE ride landed.",
    position("2026-08-26 11:43:19.000", 942, 1698),
    "[2026-08-26 11:43:20.128][INF][zipline_action.cpp] Action: ZIPLINE launched toward the landing point. [landing.x=912] [landing.y=1584]",
    position("2026-08-26 11:43:30.000", 912.2, 1584.1),
    "[2026-08-26 11:43:33.696][INF][zipline_action.cpp] Action: ZIPLINE ride landed.",
    position("2026-08-26 11:43:34.000", 900, 1585),
    position("2026-08-26 11:43:35.000", 850, 1590, {held: true}),
    position("2026-08-26 11:43:36.000", 800, 1594),
    position("2026-08-26 11:43:37.000", 724.98, 1596.8),
    lines[lines.length - 1],
  ];
  const [run] = parseMapNavigatorLog(traceLines.join("\n"));

  assert.deepEqual(run.observedWalks, [
    [
      [965.5, 1803.38],
      [955.8, 1777.7],
    ],
    [
      [940.8, 1699.8],
      [942, 1698],
    ],
    [
      [912.2, 1584.1],
      [900, 1585],
    ],
    [
      [800, 1594],
      [724.98, 1596.8],
    ],
  ]);
  assert.ok(!run.observedWalks.flat().some(([x, y]) => x === 950 && y === 1750));
  assert.ok(!run.observedWalks.flat().some(([x, y]) => x === 850 && y === 1590));

  const points = logRunPoints(run);
  assert.ok(points.some(([x, y]) => x === 940.8 && y === 1699.8));
  assert.ok(points.some(([x, y]) => x === 724.98 && y === 1596.8));
});

test("resumes measured tracking after a zipline phase ends without a confirmed landing", () => {
  const traceLines = [
    lines[0],
    position("2026-08-26 11:42:21.000", 965.5, 1803.38),
    position("2026-08-26 11:42:22.000", 955.8, 1777.7),
    "[2026-08-26 11:43:11.644][INF][zipline_action.cpp] Action: ZIPLINE launched toward the landing point. [landing.x=940.5] [landing.y=1699.5]",
    position("2026-08-26 11:43:15.000", 950, 1750),
    "[2026-08-26 11:43:16.000][INF][navigation_session.cpp] Phase transition. [from_phase_name=WaitZipline] [to_phase_name=Recover] [reason=zipline_ride_failed]",
    position("2026-08-26 11:43:17.000", 800, 1594),
    position("2026-08-26 11:43:18.000", 724.98, 1596.8),
    lines[lines.length - 1],
  ];
  const [run] = parseMapNavigatorLog(traceLines.join("\n"));

  assert.deepEqual(run.observedWalks, [
    [
      [965.5, 1803.38],
      [955.8, 1777.7],
    ],
    [
      [800, 1594],
      [724.98, 1596.8],
    ],
  ]);
  assert.ok(!run.observedWalks.flat().some(([x, y]) => x === 950 && y === 1750));
});

test("keeps searchable runtime reasons while presenting Chinese text", () => {
  assert.equal(ziplineReasonText("no zipline route beats walking"), "滑索总代价不低于步行");
  assert.equal(ziplineReasonText("future raw reason"), "future raw reason");
});

test("keeps authored tier metadata for display-frame conversion", () => {
  const tieredRequest = {
    ...request,
    custom_action_param: JSON.stringify({
      path: [
        {action: "ZONE", zone_id: "Wuling_Base"},
        {action: "NAVMESH", target: [81.77, 108.72], target_tier: "Wuling_L4_328"},
      ],
    }),
  };
  const [run] = parseMapNavigatorLog(
    `[2026-08-26 12:00:00.000][INF][AgentServer.cpp] [req=${JSON.stringify(tieredRequest)}] [ipc_addr_=ipc://test.sock]`,
  );
  assert.equal(run.authoredPath[0].targetTier, "Wuling_L4_328");
  assert.equal(run.zone, "Wuling_Base");
  assert.equal(run.completed, null);
});
