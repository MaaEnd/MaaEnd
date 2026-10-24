# Development Manual - MapNavigator Path Navigation System

MapNavigator is MaaEnd's pathfinding component: given a target position, it plans a route automatically and drives the character there.

Path coordinates are obtained by clicking on the map in the companion tool `/tools/MapNavigator`; writing them by hand is not recommended.

- [Quick Start](#quick-start)
- [Companion Tool](#companion-tool)
- [Target-Based Pathfinding: `NAVMESH`](#target-based-pathfinding-navmesh)
- [Recording a Full `path`](#recording-a-full-path)
- [`path` Data Format](#path-data-format)
- [Node Parameters](#node-parameters)
- [How `NAVMESH` Pathfinding Works](#how-navmesh-pathfinding-works)
- [Collecting and Digging `COLLECT` / `DIG`](#collecting-and-digging-collect--dig)
- [Async Interaction `INTERACT`](#async-interaction-interact)
- [Practical Tips](#practical-tips)

## Quick Start

Getting the character to a given position takes five steps:

1. **Start the tool**: run `uv run map-navigator` from the project root. The browser opens automatically and enters `路径编辑` (Path Editing) mode by default.
2. **Select the tier**: click the button showing the current basemap / tier at the right end of the map's top toolbar, and in `选择底图与层级` (Select Basemap and Tier) choose the basemap / tier where the target is.
3. **Mark the target**: choose `添加/选择` (Add/Select) and click the target position on the map. Manually added waypoints default to `NAVMESH`; only points produced by recording a live trajectory default to `RUN`.
4. **Copy**: click `复制路径` (Copy Path).
5. **Paste**: paste it into the Pipeline node.

```json
{
    "GotoTarget": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapNavigateAction",
        "custom_action_param": {
            "path": [
                {
                    "action": "NAVMESH",
                    "target": [
                        720,
                        630
                    ]
                }
            ]
        }
    }
}
```

The node needs no start point, intermediate points, or `zone_id`; at runtime the route is planned automatically from the current localization. When the target is on a layered basemap, the tool also exports the `target_tier` field.

Exception: when the route contains mechanisms such as interactions, map transitions, or jump platforms, `NAVMESH` cannot infer these semantics; use a [recorded path](#recording-a-full-path) instead.

---

## Companion Tool

The tool produces the coordinates, and MapNavigator moves according to them. The tool is a local FastAPI backend (listening only on `127.0.0.1`) plus a browser frontend, and opens the page automatically on startup; see [`tools/MapNavigator/README.md`](../../../../tools/MapNavigator/README.md) for details.

To start it:

```powershell
uv run map-navigator
```

Use `--port PORT` to select the preferred port and `--no-browser` to suppress opening the browser:

```powershell
uv run map-navigator --port 9000 --no-browser
```

The service always listens only on `127.0.0.1`. Run `uv run map-navigator --help` for the complete startup options.

Tabs at the top of the workspace card in the left column switch between four workspaces: `路径编辑` (Path Editing), `断言` (Assert), the read-only `日志分析` (Log Analysis), and `禁区` (No-Go Zones):

| Workspace | Operation | Exported Content |
| ---------- | ------------------------------------------ | ------------------------------------------------ |
| `路径编辑` (Path Editing) | Add `NAVMESH` targets by hand, or connect to the game to record a `RUN` trajectory; edit actions and plan the current segment with runtime semantics | Copies the full `path` by default; in zipline mode, copies the full `custom_action_param` |
| `断言` (Assert) | Drag out a rectangular area, then move and resize it with the box and its edge and corner handles | Copies a `MapLocateAssertLocation` node, or only the Environment Monitoring `NavAssert` coordinates |
| `日志分析` (Log Analysis) | Import `cpp-algo/debug/maafw*.log` or a MaaEnd `-partNN.zip` locally and replay walking/zipline decisions on the matching basemap | No export; shows cost calculations, raw reasons, zipline snapshots, and actual landing records |
| `禁区` (No-Go Zones) | Use `圈禁区` (Draw No-Go Zone) to outline polygonal areas on the map that the planner must not enter | Nothing to copy; `保存并重载` (Save and Reload) writes them back to `assets/data/MapNavigator/nogo_zones.json` |

The connection settings and live test runs sit in the live card at the top of the left column, shown in every workspace except `日志分析` (Log Analysis); recording is only available in `路径编辑` (Path Editing), and a test run in `断言` (Assert) only checks the current assert box once with `MapLocateAssertLocation`. `日志分析` does not connect to the game: entering that workspace hides the live card and the live position and pauses connection status probing. Switching workspaces keeps the author route, but leaving `断言` clears the current assert box. All four modes share the `图层` (Layers) panel in the map's top toolbar: the `底图` (Basemap), `NAVMESH 网格` (NAVMESH Mesh), and `滑索架` (Zipline Pylons) toggles persist across modes and are saved in the browser, while the log-specific path, trajectory, and zipline decision annotations appear as an extra group in the same panel only in log mode.

`路径编辑` (Path Editing) covers both ways of using MapNavigator: clicking by hand produces `NAVMESH` targets that the runtime pathfinds to, and live recording produces `RUN` points that are executed along the trajectory. The two can be mixed within the same `path` array — use `NAVMESH` for long-distance movement, and recorded points and action points for the local segments that need precise semantics.

The `断言` (Assert) mode produces [MapLocator](./map-locator.md) area-check nodes, used to confirm that the character is at the expected position before navigating. Drag on empty space to redraw the rectangle, drag the existing box to move it, and drag the handles on its four edges or four corners to resize it.

`日志分析` (Log Analysis) extracts every `MapNavigateAction` call and distinguishes the author's path hints, the walking plan actually adopted, the walking baseline that lost to ziplines, and the zipline hops that were actually launched and confirmed to have landed. When a run fails, the page directly shows the termination reason, error description, and key values recorded by the runtime, and lists the most recent zipline abandonment record as a preceding recovery event; unknown reasons keep their raw identifier instead of being guessed by the frontend. The page also extracts the successful MapLocator coordinates (`status=0`) from the `PositionProvider` logs to draw the measured ground trajectory; zipline flights and localization gaps break the polyline instead of being filled with predicted positions. When all ZIP parts of the same log bundle are selected directly, the page decompresses the cpp-algo logs one by one and reads the `record/Ziplines.json` of the same group: zipline pylons that this run's logs can locate are shown as numbered dots, while same-map zipline pylons from the snapshot appear as purple diamonds with a dark outline as the candidate background. By default, clicking a point on the map shows the details of an author point, planned point, measured point, or zipline pylon; the floating detail card appears at the top right of the map only after something is hit. To measure distances, first click `滑索架测距` (Zipline Pylon Distance) in the map's top toolbar (it stays disabled until `滑索架` (Zipline Pylons) is checked in `图层` (Layers)); once the measuring card appears, select the two zipline pylons A / B in turn. Point details and measurement results are independent of each other, and dragging the map still pans; leaving the measuring tool only hides the result card and does not clear the existing A / B. Skland records a corner-cell anchor that changes with orientation, so the map and the floating card use the same `footprint` algorithm as the runtime to show the range of 3D world spans between the possible centers, the raw anchor distance, the horizontal distance range, the height difference, and the basemap pixel distance. The height difference is used as-is, without applying the footprint offset; when the two are of the same type, share the same `level_id`, and the lower bound of the range does not exceed `max_span`, the page marks them as geometrically connectable. This check does not cover power supply, reachability, disabled edges, or cost planning; the actual choice is still determined by the run log. The log does not record the full planned polylines for approaching the boarding point and leaving the last zipline of a zipline combination, so the page still uses clearly labeled dashed lines for the estimated endpoint connections and never passes them off as planned or measured trajectories. Files are read only locally in the browser; log mode cannot modify routes or start live test runs.

`禁区` (No-Go Zones) is for outlining virtual no-go zones that the planner must not enter: choose `圈禁区` (Draw No-Go Zone), click on the map to place the vertices one by one, and double-click or press `Enter` to close the ring; once an existing zone is selected, drag its vertices, edges, or interior to adjust its shape and position, and press `Delete` to delete the selected vertex or zone. A zone drawn on the base map applies to every layer; to block a single layer only, switch to that tier first and then draw. `保存并重载` (Save and Reload) writes them back to `assets/data/MapNavigator/nogo_zones.json` and reloads the navmesh used by the tool's preview, with the reload result shown in the status bar; previews already on screen were planned with the old table, so back in `路径编辑` (Path Editing) the route is re-planned automatically if `自动展开` (Auto Expand) is checked, otherwise click `重新展开` (Re-expand). The runtime `MapNavigateAction` reads the same table, but each agent process reads it only once, when it first loads a map's navmesh, and never re-reads it: a running MaaEnd needs a restart; during a test run, `F4` only stops the current round, and pressing `结束会话 (F4)` (End Session) while idle ends the session, after which a new test run picks up the change. The planner treats a zone as a wall, and when the start or the end lies inside a zone, planning fails outright instead of handing over to fallbacks such as blind walking.

Both `路径编辑` (Path Editing) and `断言` (Assert) can connect to the game directly and mark the current position as a reference point. In `路径编辑` the marker is read-only and does not add an author waypoint; when the existing path and the localization result are on different basemaps, the current segment is not forcibly switched. After recording starts in `路径编辑`, the bottom of the page keeps refreshing the coordinates, the area, and the orientation measured by MapLocator (`0°` is north, `90°` is east).

In map interaction, `Esc` always means "cancel the current operation or selection" and never deletes author content; when the `图层` (Layers) panel is open, it closes the panel first. The behavior in each view:

| View | `Esc` |
| --- | --- |
| `路径编辑` (Path Editing) 2D | Clears the quick test first; otherwise cancels the author waypoint or planning start selection, the temporary point-picking tool, zipline details, or the measurement selection |
| `路径编辑` 3D | Releases the pointer first when spectator mode has captured it; otherwise cancels the current 3D point selection |
| `断言` (Assert) | While dragging, reverts the current move, resize, or redraw; after dragging, deselects the assert box and hides its handles while keeping the rectangle; zipline details or measurements are also cleared, returning to the assert editing tool |
| `日志分析` (Log Analysis) | Cancels point details or zipline measurement, keeping the imported log data |
| `禁区` (No-Go Zones) | While drawing, discards the zone being drawn; otherwise cancels the vertex selection first, then the zone selection |

Only `Delete` deletes the selected author waypoint, planning start, or assert box; `清除断言` (Clear Assert) in the sidebar clears the rectangle directly without selecting it first. After the assert box is deselected, click the box to select it again and continue adjusting.

---

## Target-Based Pathfinding: `NAVMESH`

This is the node from [Quick Start](#quick-start): its point is placed by hand in the path editor and exported with `复制路径` (Copy Path).

Only one `target` is needed: no start point, intermediate points, or `zone_id`. At runtime, the character's area and position are determined from the current localization, and a feasible route is planned on the map's triangle-face data (BaseNav).

The character's position and orientation all come from [MapLocator](./map-locator.md)'s per-frame recognition; MapNavigator is only responsible for reaching the target.

The tool preview and the runtime read the same data and use the same algorithm, so in regular scenarios the previewed route matches the runtime planning result, and you can judge from the preview whether the target is reachable (for how the preview can still differ from actual execution, see [How `NAVMESH` Pathfinding Works](#how-navmesh-pathfinding-works)). This form is already used in production routes such as Auto Collect and Environment Monitoring.

### Tier Coordinate Frame: `target_tier`

For `NAVMESH`, omitting `target_tier` means that `target` is interpreted in **base-map coordinates**, i.e. the default behavior described above. For ordinary coordinate waypoints, omitting it preserves the historical coordinate interpretation without adding a conversion.

In a layered basemap (tier), each layer is an independent coordinate system: the same `[123, 456]` on the base and on a tier are two different locations. In this case, add a `target_tier` field to the node to declare which layer the `target` belongs to:

```json
{
    "action": "NAVMESH",
    "target": [
        81.77,
        108.72
    ],
    "target_tier": "ValleyIV_L1_171"
}
```

- `target`: the coordinate clicked directly in the tool after switching to that tier's basemap, without manually converting it to base coordinates.
- `target_tier`: the area name of that layer, i.e. the name after the colon in `id:name` in the tool's tier selection.
- At runtime, the affine transformation baked into the `.nav` projects it back to the base coordinate system, and the landing point is snapped according to that layer's floor height.

Ordinary coordinate actions can use the same object form to declare that point's coordinate frame directly:

```json
{
    "action": "RUN",
    "target": [
        243.49,
        177.53
    ],
    "target_tier": "Wuling_L4_328"
}
```

This form applies to `RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / JUMP_PAD / COLLECT / DIG / FIND / TRIGGER`, as well as `HEADING` with a `target`. `target_tier` **only interprets the coordinate of the current node**: it does not switch zones, does not change the context of following nodes, and cannot replace the `ZONE` / `PORTAL` required for an actual area transition.

The field also supports camelCase `targetTier`. An unknown tier on `NAVMESH` keeps the compatibility behavior of logging a warning and treating the target as base coordinates. An ordinary coordinate point that explicitly declares a nonexistent tier fails outright instead of silently moving toward the wrong location.

### Overlapping Deck Target: `target_deck_y`

The game is three-dimensional, the map is not: a walkway, a bridge and a rooftop can all sit on the same `target`. Without `target_deck_y` the planner treats every walkable surface in the target cell as a goal and **stops at whichever it reaches first**. Stacked surfaces usually belong to the same connected component so nothing fails, and the two-dimensional arrival check passes either way — which makes landing on the wrong deck completely silent.

`target_deck_y` declares which walkable surface the `target` sits on. The value is that surface's **world height**:

```json
{
    "action": "NAVMESH",
    "target": [
        358.8,
        238.8
    ],
    "target_deck_y": 265.37
}
```

- Read the number from the MapNavigator GUI: click the target and the `选中对象` (Selected Object) card in the top-right corner of the map lists the overlapping decks; selecting one highlights that deck on the canvas, and the `选择` (Select) button writes the value into the field. Do not estimate it by hand.
- A deck matches when it is the nearest surface and within 2px (measured spacing between adjacent decks is 8–9px). When nothing matches, the route fails with `目标面不可达` (target deck unreachable) — it does not fall back to the cell-level search and is not absorbed by the blind-walk fallback. Failing loudly beats silently walking onto another deck.
- Points with a single surface do not need it; the GUI hides the list when there is no overlap.
- The field also supports camelCase writing `targetDeckY`, and can be combined with `target_tier`.

A declaration applies to its own waypoint only: **it pins which deck that leg stops on, while which deck the start stands on is worked out by the planner from the start's own height**. A leg can therefore go from a rooftop down to a walkway or the other way round with no declaration on the start side — and re-planning starts from the live localization, which has no deck to begin with.

### Applicability

`NAVMESH` only handles movement and does not infer business semantics. When the route contains interactions, map transitions, jump platforms, or external teleports, they must be annotated explicitly in `path`; in that case, use the recording method in the next section. Rope lifts are the one exception: the planner hooks up rope lifts that start out lowered and inserts the interaction itself, see [Rope lifts](#rope-lifts).

---

## Recording a Full `path`

### Recording Preparation

1. The project development environment has been configured, especially that `install/agent/cpp-algo.exe` and `install/maafw` are usable.
2. uv is installed; from the project root, `uv run map-navigator` prepares Python and dependencies automatically from `pyproject.toml`.
3. **Windows** requires **running as administrator**; otherwise, the `G` / `X` hotkeys cannot be received when the game (an administrator process) is in the foreground. `main.py` will automatically detect this and prompt a UAC elevation request at startup.
4. **macOS**: On the first run, authorize the current terminal or the uv-managed Python interpreter in **System Settings → Privacy & Security → Input Monitoring**; otherwise, global hotkeys will not work.
5. If using `Win32` connection, the game is already started and the window is **not minimized**; if using `ADB` connection, `adb` is available and the device appears in the list after `检测并刷新设备` (Detect and Refresh Devices).
6. The current character is standing near the starting point of the route you want to record.

### Recording

In `路径编辑` (Path Editing) mode, choose the connection method, wait until the connection status shows success, then click `开始录制` (Start Recording), switch back to the game, and walk through once the way you want the route to be executed automatically. While the connection probe has not succeeded, the `开始录制`, `开始试跑 (F3)` (Start Test Run), and `标记当前位置` (Mark Current Position) buttons stay disabled.

There are two hotkeys during recording:

| Hotkey | Function |
| ---- | -------------------------------------------------------------- |
| `G` | Copies the current coordinates to the clipboard as `[x, y]`; does not affect the recorded data and can be pressed anytime |
| `X` | Forcibly inserts a **strict arrival point** at the current position |

`X` is used to mark key locations (interaction points, jump platform landing points, etc.) to ensure that the coordinate is recorded and marked as strict arrival. It only affects arrival precision and does not make the node required automatically; when the node really must not be skipped, also check `路径必经` (Required Waypoint) in the editor.

Points with stronger business semantics such as `FIGHT`, `TRANSFER`, and `HEADING` **are not determined automatically during recording**; change them manually on the page after stopping the recording.

After you click `停止录制` (Stop Recording), the tool organizes the raw trajectory: it normalizes it into the canonical format, marks the points on both sides of a cross-area boundary as `PORTAL`, and displays it segmented by area.

### Editing Path Points

- View: scroll to zoom, and drag the view with the `平移` (Pan) tool (or hold `Alt`).
- Mesh and reference point: the page shows the navmesh mesh for the current basemap / tier; you can connect to the game to mark the current position as a read-only reference point, which does not insert a waypoint. The basemap and mesh can be shown or hidden in the shared `图层` (Layers) panel in the map's top toolbar; the same state also applies to `断言` (Assert) and `日志分析` (Log Analysis).
- 2D / 3D view: switch views in the map's top toolbar. 2D is the authoritative view for route editing and planning preview; 3D shows the current mesh read-only at the real heights from the NMSH and follows the `NAVMESH 网格` (NAVMESH Mesh) toggle in `图层` (Layers). After switching to 3D, choose `3D 视角导航` (3D View Navigation) in the left sidebar: the default `观察者漫游` (Spectator Roam) mimics Minecraft's spectator mode — click the canvas to capture the pointer, after which the mouse turns the view directly, `W` / `A` / `S` / `D` move along the horizontal projection of the line of sight, `Space` rises and `Shift` descends, the scroll wheel adjusts the roaming speed in sync with the `漫游速度` (Roam Speed) slider on the left, a click reads the `[u, v]` and height under the crosshair, and `Esc` releases the pointer; a hint at the bottom of the canvas always shows the operations currently available, and keys have no effect while the pointer is not captured. `轨道环绕` (Orbit) rotates around the mesh point straight ahead of the line of sight with a left-button drag, pans with a right- or middle-button drag, zooms with the scroll wheel, and reads the coordinate and height with a left click; WASD and the rise/descend keys only take effect after spectator mode has captured the pointer. Both modes can fit the view. In 3D, trajectories from live test runs and real-time localization are lifted onto navmesh walkable surfaces: when several walkable surfaces lie under the starting point, the canvas pops up a height choice; after that, each point takes a surface from the connected neighborhood of the previous point's surface, takes the surface closest in height when none is connected, and keeps the previous point's height when off the mesh. 3D does not modify the author route; with `最终计划路径` (Final Planned Path) checked it also draws the planned path, using the per-point heights from the planning result and the mesh surface height where a point has none; leaving `路径编辑` (Path Editing) switches back to 2D automatically.
- Path points: first select the path's basemap and tier; `添加/选择` (Add/Select, key `1`) adds `NAVMESH` targets by hand and is also used to select and drag points; `框选` (Box Select, key `2`) selects multiple points with a box. Click `取消选择` (Deselect) in the details at the top right, press `Esc`, or click empty map space in box-select mode to clear the selection; only `Delete` deletes the selected objects. Trajectory points from live recording stay `RUN`.
- Properties: after selecting a point, set its `动作类型` (Action Type), `严格模式` (Strict Mode), and `路径必经` (Required Waypoint), then click `应用属性` (Apply Properties) to apply them.
- Coordinate tier: when a point's coordinate comes from a tier basemap, click `坐标层级` (Coordinate Tier) and choose that tier in the `选择路点坐标层级` (Select Waypoint Coordinate Tier) picker that opens; keeping `使用旧坐标` (Use Legacy Coordinates) keeps the legacy coordinate semantics.
- Overlapping decks: when a single `NAVMESH` waypoint is selected and its coordinate sits over several walkable surfaces, the `选中对象` (Selected Object) card in the top-right corner of the map lists each layer; click a height to preview that layer, and click `选择` (Select) to write `target_deck_y`. Moving the point, switching its coordinate tier, or changing it to another action clears the old value, so the floor declaration of the old position is not carried over to the new target.
- Cross-area routes are displayed segmented by area, making it easy to check whether the points before and after a map transition are reasonable.
- Route planning: `两点测线` (Two-Point Test) in the map's top toolbar (the quick test) verifies any two points directly. The first click sets a temporary `S`, the second sets a temporary `G` and plans automatically with runtime semantics, and a third click starts over from a new `S`; `Esc`, `Delete`, or `清除线路` (Clear Route) clears the temporary points and the result. An end point on a layered basemap automatically carries the current `target_tier`. The temporary S/G never enter the author `path`, the undo stack, the copied result, or live test runs. The author path preview starts from the first author waypoint of the current segment by default. To test the character starting from another position and then continuing through the author waypoints, click `地图点选` (Pick on Map) in the `NavMesh起点` (NavMesh Start) row of the sidebar and pick a point on the map in the same zone; once the point is picked, the previous editing tool is restored automatically. The cyan `S` serves only as the planning start of the current segment and is never written to or exported into the author `path`, so a single author waypoint can also be used to test "start to target" planning directly. With the `添加/选择` (Add/Select) or `框选` (Box Select) tool, `S` can be selected and dragged, and it can be removed with `Delete` or the toolbar's `删除` (Delete) button. Clicking an author waypoint or the planning start shows its action, coordinates, tier, target deck, and other details in the top-right corner of the map. With `自动展开` (Auto Expand) checked (the default), the current segment is planned automatically whenever a waypoint or the start changes, and `重新展开` (Re-expand) plans it immediately; in the result, the pink polyline shows the walking legs expanded by the runtime, and cyan directed edges show the selected zipline hops; expand `寻路诊断` (Pathfinding Diagnostics) in the `图层` (Layers) panel to show the `拓扑逐格路径` (Topology Cell Path), `几何紧绷折线` (Geometric Taut Polyline), `取直后折线` (Straightened Polyline), and `端点拼接与去冗` (Endpoint Splicing and Redundancy Removal) stages separately, while `最终计划路径` (Final Planned Path) in the same panel controls the final route. The diagnostics only cover the walking legs of the fully expanded, finally adopted plan, not failed global direct attempts or unselected walking baselines. On failure, the status bar names the failing author waypoint and shows the raw runtime reason, such as the start or end being off the mesh, the endpoints not being connected, the specified target deck being unreachable, or navmesh loading failing; if the start and end fall in two disconnected reachable regions, the map focuses automatically and connects the nearest boundary points on the two sides with a red dashed line, labeling the gap distance. Only when a precise gap cannot be obtained does it fall back to marking the whole failing leg; off-mesh endpoints still keep their amber distance markers.
- Quick test cleanup: actively switching to another editing tool, or to `断言` (Assert) / `日志分析` (Log Analysis), clears the temporary S/G and the planning result; switching to `禁区` (No-Go Zones) keeps both, and they show again back in `路径编辑` (Path Editing); holding `Alt` to pan temporarily does not count as leaving, and releasing it returns to the quick test.
- Live trajectory: during a test run, `实时实际路径` (Live Actual Path) keeps drawing MapLocator's real localization and the current position; a new test run clears the previous run's trajectory, and localization gaps are not filled with predicted points.
- Zipline toggle: after checking `滑索规划` (Zipline Planning) in the sidebar, preview, copy, and live test runs all carry the route-level `zip: true`. When there is no zipline data or the zipline cost has no advantage, it falls back to walking according to the runtime rules.
- Zipline pylon layer: `路径编辑` (Path Editing), `断言` (Assert), and `日志分析` (Log Analysis) share the `滑索架` (Zipline Pylons) toggle in `图层` (Layers). `路径编辑` and `断言` show the records of the current installation directory, while `日志分析` shows the zipline pylons in the current selection and in the ZIP snapshot of the same group; in all three 2D modes you can click a zipline pylon to see its basemap / world coordinates, navigation height, template, and Level, and they share the A / B selection, the possible-center span, and the geometric connectivity check. The layer is independent of the route-level zipline toggle and does not change preview parameters, copied content, or live test runs; recorded candidates may be unpowered or already excluded by the runtime, and being geometrically connectable does not mean being actually usable.
- Multi-account ziplines: official-map imports derive a pseudonymous account identity from the `/map/mark/list` `roleId`; game runs derive the same identity from the UID captured once during scene initialization. Runtime planning filters by account before map. Legacy records without an account field are not attributed at import time: when zipline navigation runs, the current account claims them — if that account has no records of its own yet, the legacy coordinates are attributed to it wholesale and persisted in place, so nothing has to be imported again after an upgrade; if the account already has records of its own, nothing is claimed, so two sets of coordinates never mix and then silently go wrong after an account switch. A successful claim is announced to the user; see `ZiplineStore::claimLegacyRecords`. Offline route preview cannot read the in-game UID, so you can choose an imported pseudonymous account in the account drop-down next to `滑索规划` (Zipline Planning); it defaults to the most recently imported one and the choice is remembered in the browser. The choice also filters the current installation's zipline pylon layer, but is never copied into route parameters and never overrides the account detected during a live run. `日志分析` (Log Analysis) still filters snapshots by the account identity recorded in the matching run. Windows imports run in the cpp-algo embedded browser; on Linux, `agent/go-service/ziplineimport` captures the same endpoint through a local restricted MITM proxy, derives the account identity from the request URL's `roleId`, and stores records under `(account_id, map_id)` as well; an import that sees more than one `roleId` is rejected as a whole. Windows always clears the embedded browser site session before each import; Linux uses a throwaway Firefox profile on every import, so the session is never kept either.

Usually, the only fine-tuning you really need to do is:

1. Change key interaction points to `INTERACT`; for interactions that must be triggered, also check `路径必经` (Required Waypoint). When the prompt text has to be confirmed before pressing, add `interact_text` to that point after exporting — see [Async Interaction](#async-interaction-interact).
2. Change points that require jumping, sprinting, waiting for a teleport, or map transitions to the corresponding action.
3. Check whether the two points before and after an area transition fall in reasonable locations.

### Exporting

By default, what `复制路径` (Copy Path) copies to the clipboard is **only the `path` body**, not the complete node JSON, so it can be pasted directly into `custom_action_param.path`:

```json
"custom_action_param": {
    "path": [
        ...
    ]
}
```

The exported content is already in the canonical format that MapNavigator can directly consume; it is recommended to finish all changes on the page before copying.

With zipline planning enabled, the button changes to `复制完整参数` (Copy Full Parameters), and the copied result is `{ "path": [...], "zip": true }`, which can be used directly as `MapNavigateAction.custom_action_param`. The approach points, dismount points, walking points, and `ZIPLINE` points generated by the runtime are only for preview and are never mixed into the author configuration.

### Importing Project Nodes

Both `路径编辑` (Path Editing) and `断言` (Assert) use `选择项目节点` (Select Project Node) as their primary import entry. The tool scans the JSON / JSONC under the current project's `assets` and shows only the nodes that apply to the current mode:

- `路径编辑` (Path Editing): select a `MapNavigateAction` with a valid `custom_action_param.path` to load the full path for further editing; if the node already has `zip: true`, the zipline toggle is checked as well;
- `断言` (Assert): selects `MapLocateAssertLocation` by default and loads its `zone_id` and `target` directly; after switching to `参考路线` (Reference Route), you can also select a `MapNavigateAction` to show its real path points on the assert basemap.

The selector searches by resource path, node name, or zone and shows each node's type, point count, and zone summary, so the wrong node is not picked automatically when a file contains several. When a navigation path lacks zone information, you still need to assign zones to its segments on the page; imports validate action semantics strictly — **an unknown action is rejected outright** and never silently downgraded.

Both modes provide a direct `读取剪贴板` (Read Clipboard) button. The tool automatically recognizes a complete Pipeline, a single `MapNavigateAction` / `MapLocateAssertLocation` node, a `{"path": [...]}` object, or a bare `path` array; when a navigation path lacks zone information, it still asks you to assign zones to its segments. Clipboard reading must be triggered by a button click, and the browser may ask for read permission the first time.

---

## `path` Data Format

This format is usually exported by the tool and does not need to be written by hand; refer to this section when you need to adjust it manually.

`path` is an array whose elements are listed in the order the author intends them to be executed. Whether intermediate points may be skipped is covered in [Execution Order and `required`](#execution-order-and-required).

**Coordinates only**: continue to the next point upon arrival:

```json
[
    688,
    350
]
```

**Coordinates plus action**: perform that action once upon arrival:

```json
[
    720,
    350,
    "SPRINT"
]
```

**Explicitly declaring the coordinate tier** uses the object form instead; legacy arrays without a declaration are unaffected:

```json
{
    "action": "RUN",
    "target": [
        243.49,
        177.53
    ],
    "target_tier": "Wuling_L4_328"
}
```

`target_tier` and `ZONE` mean different things: the former only states which basemap the current `target` was clicked on, while the latter declares the context for route execution and area verification. Filling in `target_tier` on an ordinary point does not trigger a tier switch.

**An `INTERACT` point declaring its interact prompt text** likewise uses the object form; `interact_text` is the prompt text that point has to recognize:

```json
{
    "action": "INTERACT",
    "target": [
        331,
        1578
    ],
    "interact_text": "登记"
}
```

The array form `[x, y, "INTERACT"]` has no slot for these fields, so async interaction requires the object form; an `INTERACT` without `interact_text` keeps its original meaning (press the interact key once upon arrival). See [Async Interaction](#async-interaction-interact).

Only `INTERACT` points can take these fields — on any other action they are ignored, with one `carries interact fields without an INTERACT action` line in the log.

`NAVMESH` is an exception: it is a semantic pathfinding node and must use an object with `target`; it cannot be written as `[x, y, "NAVMESH"]`:

```json
{
    "action": "NAVMESH",
    "target": [
        1242.04,
        773.41
    ]
}
```

### Available Actions

| Action | Behavior on Arrival |
| ---------- | --------------------------------------------------------------------------------- |
| `RUN` | No extra action; continues to the next point. The default when the action is omitted |
| `SPRINT` | Sprints once |
| `JUMP` | Jumps once |
| `FIGHT` | Attacks once |
| `INTERACT` | Interacts once; with `interact_text` it becomes an async interaction instead, see [Async Interaction](#async-interaction-interact) |
| `COLLECT` | Collect: stops moving, triggers OCR recognition and clicks to collect, see [Collecting and Digging](#collecting-and-digging-collect--dig) |
| `DIG` | Dig: stops moving and triggers the digging subtask, see [Collecting and Digging](#collecting-and-digging-collect--dig) |
| `FIND` | Finds and approaches a target that exists only as a recognition box: turns the camera to search and walks toward the box; done once `find_stop` hits or the character gets near `find_arrive`, see [Finding and Approaching a Target](#finding-and-approaching-a-target-find) |
| `TRIGGER` | Route end point: recognizes `trigger_node` while walking; navigation succeeds on a hit or on reaching `target`, see [Ending on a Recognition Hit](#ending-on-a-recognition-hit-trigger) |
| `NAVMESH` | Plans a route automatically from the current localization and moves to `target`; must use the object form above |
| `TRANSFER` | Waits in place for an external force (story, teleport, etc.) to move the character to the next segment, then continues from the following points |
| `JUMP_PAD` | Jump pad: stands on the pad center, jumps when needed, steps and retries if it did not launch, then continues from the following points after landing; see [Jump Pads](#jump-pads-jump_pad) |
| `PORTAL` | Map transition point: after triggering, walks blind for a short distance and waits for the area switch |
| `HEADING` | Turns the camera to the specified orientation, then presses `W` once to make the orientation take effect |

### Execution Order and `required`

Without ziplines enabled, the navigator executes strictly in `path` order. Ordinary points and actions are all kept, and `NAVMESH` is expanded point by point too.

Only with ziplines enabled does the navigator try to skip intermediate points, planning directly from the start of the current interval to its end. When the direct plan succeeds, the intermediate movement and reachability fallback points are skipped; when it fails, the author path is still executed point by point. Such optional points include `NAVMESH`, `RUN`, `SPRINT`, `JUMP`, `FIGHT`, `TRANSFER`, `JUMP_PAD`, `PORTAL`, and `INTERACT`.

The following nodes are always kept and need no `required`:

- `ZONE`: declares the area context;
- `HEADING`: adjusts the orientation;
- `COLLECT`, `DIG`: perform the collection or digging task.
- `FIND`: finds and approaches the target in a recognition box.
- `TRIGGER`: route end point; navigation ends on a trigger node hit or on reaching `target`.

With ziplines enabled, if another node must also be reached and executed, add `"required": true`. That node ends the current optimization interval, and the rest of the route starts again from there. The array form has no slot for this field, so required points must use the object form.

For example, when the route must go over a particular jump pad, write:

```json
{
    "action": "JUMP_PAD",
    "target": [
        1001.89,
        305.68
    ],
    "required": true
}
```

The position of the route's last point is always the final goal. When the last point carries an ordinary action that must be executed, it also needs `required`. Without ziplines enabled, all nodes are executed in order anyway, and `required` does not change the result.

### Strict Arrival

Adding `true` at the end of the array means the point must be reached strictly:

```json
[
    700,
    350,
    "INTERACT",
    true
]
```

By default, the arrival check keeps a certain radius; once marked strict, the radius is smaller and arrival is slower, but the landing point is more precise. Actions such as interactions, jump platforms, teleports, and map transitions already use stricter arrival semantics on their own.

Strict arrival only controls arrival precision and does not make a point required. With ziplines enabled, global planning can still skip strict arrival points; use `required` when a point must be executed.

### Zone Declaration: `ZONE`

Use `ZONE` to specify the map area of the following points:

```json
{
    "action": "ZONE",
    "zone_id": "Wuling_Base"
}
```

This node produces no movement and only provides area **verification** context for the following path points. A `path` exported from a recording usually contains it already.

### Adjusting Orientation: `HEADING`

Use `HEADING` to adjust the orientation on its own, without moving:

```json
{
    "action": "HEADING",
    "angle": 90
}
```

`HEADING` is always kept and does not need `required`. When a movement target precedes it, the navigator first reaches that target and then adjusts the orientation.

You can also give a coordinate to turn in the direction "current position → that coordinate":

```json
{
    "action": "HEADING",
    "target": [
        688,
        350
    ]
}
```

> [!NOTE]
>
> Point editing on the page covers path points with coordinates (`RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / JUMP_PAD / COLLECT / DIG / FIND / TRIGGER / NAVMESH`): it can edit `required` and `target_tier` per point, choose `target_deck_y` for a single `NAVMESH` target, and derive `ZONE` declarations from area information. `HEADING` is a non-coordinate control node outside this editing model; it is recommended to add and maintain it manually after exporting the `path`. Likewise, the `find_target` / `find_text` / `find_stop` / `find_arrive` fields of `FIND` and the `trigger_node` of `TRIGGER` are written by hand after exporting — the tool only preserves them as they are.

---

## Node Parameters

The interface is an Action based on MAA `Custom`: `MapNavigateAction`.

`path` is the only required parameter; the other optional parameters go in `custom_action_param`:

| Parameter | Default | Description |
| ------------------ | ------- | -------------------------------------------------------------------- |
| `map_name` | Empty | Initial area context. Usually unnecessary when `path` already contains a `ZONE` declaration |
| `heading_source` | `"character"` | Heading source for navigation and zipline pylon aiming throughout the call: `"character"` uses the character heading, `"camera"` uses the camera heading; neither falls back to the other |
| `arrival_timeout` | `60000` | Maximum time allowed to reach a single point, in milliseconds; exceeding it counts as failure |
| `sprint_threshold` | `16.0` | Threshold for automatic sprinting, based on **the length of the continuously runnable segment ahead** rather than the straight-line distance to the current point |
| `interact_text` | Empty | Route-wide default for the interact prompt text, see [Async Interaction](#async-interaction-interact) |
| `interact_scan` | Empty | Route-wide default for the walking pre-filter node, see [Replacing the Icon Pre-filter](#replacing-the-icon-pre-filter) |
| `interact_rec` | `false` | Whether the route's `INTERACT` points recognize the prompt without pressing anything, see [Recognize Only, Never Press](#recognize-only-never-press-rec-mode) |
| `find_target` | Empty | Route-wide default for the `FIND` target recognition node, see [Finding and Approaching a Target](#finding-and-approaching-a-target-find) |
| `find_text` | Empty | Route-wide default for the `FIND` inline OCR text table; use either this or `find_target`, not both |
| `find_stop` | Empty | Route-wide default for the `FIND` prompt criterion; a hit counts as arrived |
| `find_arrive` | Empty | Route-wide default for the `FIND` arrival coordinate; getting near it counts as arrived |
| `enable_bootstrap_navmesh` | `true` | Whether to plan a navmesh leg at startup to join the route first. Set it to `false` to skip this step and walk the points recorded in `path` directly — the fallback for stacked terrain (platforms/catwalks/rooftops) where the startup plan detours badly |

Unknown top-level fields are silently ignored without errors.

Navigation requires both the position and the selected heading to be available; an unavailable heading is treated as a localization failure. Character-heading mode keeps the locator's occlusion protection: when the character arrow is not visible, the frame is rejected before position matching and the tracking state update, and localization is let through only after the occlusion has lasted 5000ms in a row. Camera-heading mode skips this arrow-occlusion gate but still validates the localization result, the camera heading, and the confidence. `MapLocateRecognition` and `MapLocateAssertLocation` keep the default occlusion protection.

In camera-heading mode, navigation hands the camera azimuth actually recognized on the previous tick to the locator as a **prior**, used for disambiguation when the model output has two peaks (for decoding details and the secondary-peak criteria, see [MapLocator](map-locator.md)). The prior is an observation, not a command, and is passed only when all of the following hold: the navigator is in the `Navigate` phase, this is not a global re-localization, and **no camera-turn command has been issued since the last successful position fix**. This is tracked with a monotonic **heading epoch**: any yaw command (steering, semantic `HEADING`, camera alignment, unstick turns, turning around after falling into water) and any phase switch increments the epoch, and a successful position fix records the epoch at that moment; only when the recorded epoch still equals the current epoch is that tick's heading used as the prior. Therefore the startup fix, global re-localization, the first tick after a phase switch, and ticks while turning carry no prior; the prior is also never derived from the target angle or from a turn amount that has not landed yet, and the value takes no part in position matching. It is not exposed to the Pipeline: passing it to `MapLocateRecognition` has no effect.

`interact_text` accepts a string or an array of strings, or `{ "node": "SomeOcrNode" }` to take the text table from that node (see [Sharing One Text Table Across Routes](#sharing-one-text-table-across-routes)); camelCase `interactText` is accepted as well. `interact_scan` accepts a string, camelCase `interactScan`. For both, the value written on a waypoint wins, and the route-wide value only fills in the `INTERACT` points that carry none of their own.

`interact_rec` is a boolean, camelCase `interactRec`, and it does not follow "the waypoint wins": writing `true` at the route level turns on all of the route's `INTERACT` points at once, and writing `false` on a point cannot turn it back off — a boolean cannot tell "written `false`" from "not written", so this field only ever switches points on. To keep a few points pressing as usual, leave it off the route level and write it point by point.

The interaction points of one route usually belong to the same business, in which case writing the value at the route level saves repeating it on every point:

```json
"custom_action_param": {
    "interact_text": ["登记", "接取委托"],
    "path": [
        ...
    ]
}
```

An empty string or an empty array for `interact_text` is rejected outright (the whole node parameter parse fails) rather than treated as absent: on the recognition side, empty text means "match anything", so the key would be pressed at any prompt. An empty string for `interact_scan` counts as absent and falls back to the shipped one.

`find_target` / `find_text` / `find_stop` / `find_arrive` accept camelCase (`findTarget` / `findText` / `findStop` / `findArrive`), and fields written on the point win. Route-wide defaults are filled by group: the **target source** (`find_target` / `find_text`) is filled as a whole — if a point sets either field, neither route-wide field applies to it; only when neither is set are both filled in. The **completion condition** (`find_stop` / `find_arrive`) is filled field by field, supplying only the fields the point does not set. Parameter parsing fails outright when a `FIND` point lacks a target source or a completion condition, or when the route level sets both `find_target` and `find_text` while the route contains a `FIND` point.

### Execution Result

`MapNavigateAction` is an Action node; it has no structured output like a Recognition, and there are only two results:

- The whole route is completed → success.
- Continuous lack of progress midway, or continuous deviation from the route until timeout → failure.

In a Pipeline, orchestrate it as an atomic action: **either the whole route is completed, or the node fails**.

### Complete Example

Paste the `path` exported by the recording tool into it and you have a complete node:

```json
{
    "DebugNavi": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapNavigateAction",
        "custom_action_param": {
            "arrival_timeout": 45000,
            "path": [
                {
                    "action": "ZONE",
                    "zone_id": "Wuling_Base"
                },
                [
                    405,
                    1592
                ],
                [
                    400,
                    1583
                ],
                [
                    380,
                    1567,
                    "SPRINT"
                ],
                [
                    331,
                    1578,
                    "INTERACT",
                    true
                ]
            ]
        }
    }
}
```

> [!TIP]
>
> `MapNavigateAction` should come after a node that has confirmed the entry state: first confirm that the character is in the expected scene, area, and near the expected orientation, then start the whole navigation, which noticeably increases the success rate. The `MapLocateAssertLocation` node exported by the `断言` (Assert) mode is well suited as this entry check.

> [!WARNING]
>
> Adjacent path points should be reachable from one another in one continuous walk. The navigator cannot pass through models or understand business mechanisms; jump pads use `JUMP_PAD`, and segments such as map transitions, falls, and ascent mechanisms can be split into `PORTAL` / `TRANSFER` / business nodes as an author-path fallback. With ziplines enabled, nodes that must be passed or executed should be marked `required`.

---

## How `NAVMESH` Pathfinding Works

This section is for readers who need to understand the internal implementation; it can be skipped for everyday use.

1. Prioritize loading `assets/resource/model/map/navmesh/base.nav.gz`; fall back to `base.nav` if it doesn't exist.
2. Infer the BaseNav zone based on the current localization area.
3. When ziplines are enabled, read the pseudonymous account identity captured during this run's initialization and load only that account's zipline records; if the identity is unavailable, fall back to walking. Legacy records without an account field are claimed by the current account while that account still has no records of its own (see "Multi-account ziplines" above) and then take part in filtering exactly like account-scoped records.
4. Snap the landing point according to the current floor height, rasterize the triangle faces within the window into cells with height intervals, execute A\* on the cell graph, and then straighten the result under clearance constraints.
5. Expand the planning result into ordinary `RUN` waypoints, which are then handed over to the movement execution chain. When the target is unreachable, a separate blind-walk fallback takes over, and the route it walks does not come from the planner.

Without ziplines enabled, `NAVMESH` is expanded point by point in author order. With ziplines enabled, the expansion first splits the path into intervals at the [path boundaries](#execution-order-and-required), then compares the pure-walking and zipline routes of each interval; when no direct route is possible, it falls back to executing the author path point by point.

When pure walking is not connected, the whole interval is not judged unreachable right away: with ziplines enabled, the planner keeps looking for a bridging route of "walk to the boarding point → several consecutive zipline hops → walk from the dismount point to the end". As long as the boarding and dismount ends connect to the walkable surfaces of the start and the end respectively, this continuous chain takes priority over blind walking and falling back to author hints; when pure walking is itself possible, the comparison still uses the total cost and the `min_gain` threshold, so the planner never detours unconditionally just to ride a few more ziplines.

When one hop of the continuous chain fails, the runtime discards the remaining hops of that chain, waits for localization to stabilize again and snap back onto the navmesh, then rejoins at the first actually reachable point of the remaining route. If there is no reachable point to rejoin at, this navigation fails explicitly instead of continuing with the old post-dismount path generated from the expected dismount point.

When landing at the end of the chain (walking follows this hop), before dismounting the navigator first aligns the heading with the first planned walking point of the dismount leg, that is, the direction to walk next. The dismount itself carries momentum along the heading at the moment of the key press: however far the heading is off, the momentum is off by the same amount. A tangential deviation (about 90 degrees) is the worst case — all of that momentum is sideways to the walking direction, carrying the character sideways off the line it has to walk; a deviation along the walking direction (including 180 degrees, moving entirely backwards) only adds or removes a straight stretch, which the walking itself corrects. This step only affects the heading — if the turn fails, localization cannot be read, or it times out, the dismount proceeds as usual and the route is not interrupted.

Floor snapping is worth calling out separately: on multi-floor maps, triangle faces from several floors are stacked at the same planar coordinate. Without distinguishing height, the start or end point may be snapped to the wrong floor, which shows up as the character clipping through walls or the path being unreachable. BaseNav bakes the floor height for each zone into the data pack and filters candidate faces by height band during planning, so the landing point in multi-floor areas is deterministic.

> [!NOTE]
>
> The path editor's planning preview directly calls the C++ route expansion logic used by `MapNavigateAction` and reads the same `base.nav.gz`. The preview does not implement a separate routing algorithm in JavaScript; actual execution is still affected by localization, terrain, and in-game conditions, so the outer Pipeline still needs failure fallback.

### Rope lifts

`base.fields.nav.gz` contains the rope lifts whose two ends are both marked in the data as lowered from the start. Each one carries a bottom and a top foothold plus the seconds one ride takes (the rise divided by the lift speed, plus one second of animation). Rope lifts the player has to lower from the top first are left out: an account may never have opened them, and planning through one could mean finding out only at the bottom that it cannot be used.

Rope lifts are only ridden upwards; going down always jumps off a ledge. The planner tries them in two situations:

- The start and the end are on walkable surfaces that are not connected: when no ledge jump gets there, or the rope route is shorter than the route with the jump, the rope route is used;
- The start and the end are on the same connected surface: the ride time is converted into a distance at running speed, and the rope route is taken only when walking from the start to the bottom, riding, and walking from the top to the end is shorter than walking directly. A rope whose top is closer on foot from its bottom than the ride itself is never considered.

Rope lifts are hooked up only by the full expansion at the start of a route; local re-planning after drifting off the route mid-run does not use them. In the planned route the rope bottom becomes a strictly reached `INTERACT` point whose text comes from `MapNavigatorRopeUseText` in `MapNavigator/Interact.json`; when that node is missing or has no text, the full expansion uses no rope lifts. After the interaction is pressed, the navigator waits for the ride time, then for consecutive stable localizations, and continues from the top. When no interaction prompt shows up at the bottom (for example because this account has the rope raised), that rope is marked unusable for the rest of this navigation and the remaining route is expanded again from the current position.

---

## Collecting and Digging `COLLECT` / `DIG`

`COLLECT` and `DIG` are MapNavigator's built-in collection/digging semantic points. In `path`, write the corresponding action as the third element of the collection coordinate; after arriving precisely, the navigator automatically stops, synchronously triggers the corresponding Pipeline subtask, and continues to the next segment once it finishes, **without exiting NaviController throughout**:

```json
"path": [
    { "action": "ZONE", "zone_id": "Wuling_Base" },
    [707, 838],
    [720, 832],
    [741, 802, "COLLECT"],
    [744, 800, "COLLECT"],
    [739, 792, "COLLECT"]
]
```

- `[x, y, "COLLECT"]`: Triggers OCR recognition + automatic click collection (`AutoCollectClickStart`) upon reaching this point.
- `[x, y, "DIG"]`: Triggers unconditional click digging (`AutoCollectDigStart`) upon reaching this point.
- Any number of `COLLECT` and `DIG` points can be mixed within the same `MapNavigateAction` node.
- **No need** to write `anchor` on the node or point `next` to `AutoCollectClickStart`.

In the tool: walk up to the collectible normally while recording, then change that point's action to `COLLECT` or `DIG` after stopping the recording.

Compared with the old `anchor` chain-splitting form, this approach needs no re-establishing of the connection, re-bootstrapping, or resetting of the sprint start grace period for each collection; the entire segment near collection points automatically disables sprinting, preventing overshooting the target; and multiple collection points are merged into a single Pipeline node, without needing to split them into multiple `GotoFindN` nodes.

### Related Files

| File | Responsibility | When Changes Are Needed |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------ | ---------------------------------------------------------------- |
| `assets/resource/pipeline/AutoCollect/AutoCollectRoute*.json` | Path definitions, containing `MapNavigateAction` nodes and collection coordinates | Add new routes, adjust coordinates, add/remove collection points |
| `assets/resource/pipeline/AutoCollect/AutoCollectClick.json` | OCR and click subtask triggered by `COLLECT`, entry point is `AutoCollectClickStart` | Add or delete OCR-recognized collection object names |
| `assets/resource/pipeline/AutoCollect/AutoCollectDig.json` | Digging subtask triggered by `DIG`, entry point is `AutoCollectDigStart` | When digging interaction logic changes |
| `assets/resource/pipeline/AutoCollect.json` | Route iteration, failure collection, and backpack storage before/after the task | Add route entries or adjust the overall flow |

**In most cases, path authors only need to modify `AutoCollectRoute*.json`.**

The overall Auto Collect flow uses `AutoCollectLoop` to invoke route wrapper nodes in order. Each wrapper uses the generic `FailureCollectorRunTask` to execute an enabled route; if any node inside the route fails, the wrapper Action records that route's `{Route}Failed` Pipeline node and returns success so Pipeline continues with the next route. After all routes and the final backpack storage step finish, `AutoCollectFinish` calls those nodes in failure order to output the localized `$option.*.label`, then makes the Auto Collect task return failure.

### Parts Path Authors Do Not Need to Touch

The following files are maintained by cpp-algo developers; path authors do not need to modify them:

- `agent/cpp-algo/source/MapNavigator/navi_domain_types.h`: `ActionType` enum, `COLLECT`/`DIG` declared here.
- `agent/cpp-algo/source/MapNavigator/navi_config.h`: Subtask entry names, `pipeline_override`, wait time after collection, and other constants.
- `agent/cpp-algo/source/MapNavigator/semantic/semantic_nodes.cpp`: Execution logic after arriving at the collection point.

### Notes

**The old writing method is deprecated.** The old `anchor: { "AutoCollectClickAfter": "..." }` + `next: ["AutoCollectClickStart"]` chain-splitting writing method should no longer appear in new routes.

**`AutoCollectClickEnd`'s `next` must not be changed.** It points to `[Anchor]AutoCollectClickAfter` to maintain compatibility with old anchor chain calls. When called from a `MaaContextRunTask` subtask, cpp-algo temporarily nullifies this `next` via `pipeline_override`, allowing the subtask to exit cleanly. Changing it affects other routes still using the old writing method.

**Sprint control is runtime-managed.** For the entire segment before every `COLLECT`, `DIG`, and strict arrival point, automatic sprinting is hard-disabled by cpp-algo at the `NavigationStateMachine` level; path authors cannot and do not need to control it.

### Adding a Collection Route

1. Create a new `AutoCollectRouteN.json` under `assets/resource/pipeline/AutoCollect/`, referencing existing routes to write the basic skeleton of four nodes: `Start` → `AssertLocation` → `Goto` → `End`.
2. Record the path with the tool, change the action of the collection target points to `COLLECT` or `DIG`, copy the `path`, and paste it into the `custom_action_param.path` of the `Goto` node.
3. Register the new route entry in `interface.json` / the task entry JSON.
4. No changes are needed to `AutoCollectClick.json`, `AutoCollectDig.json`, or any cpp-algo source files.

---

## Async Interaction `INTERACT`

### Concept

`INTERACT` carries two meanings, decided by whether the waypoint has `interact_text`:

- **Without it**: when the point is selected for execution, press the interact key once upon arrival. This is the original point-action meaning.
- **With it**: when the point is selected for execution, it becomes an **async interaction**. On the last stretch toward that point, a background pre-filter watches for the interact prompt; when the prompt shows up the navigator stops and hands over to a Pipeline subtask, which OCRs the prompt and presses the interact key only when the text matches `interact_text`. The prompt showing up is the game's own confirmation that the character is in range, so that press completes the point — the little distance left is not walked.

The reason the route has to supply the text: there are far too many kinds of interactable, and the prompt wording differs per business, so no single shared table can enumerate them. Collectible names are a shared table, which is why `COLLECT` keeps its text in the Pipeline node; interact text has to be injected by whichever route uses it.

What a route may customize is those two recognitions — when to stop, and whether this is the one — plus whether a match presses anything:

| Field | Replaces | Left out |
| --------------- | ----------------------------------------------------------- | ----------------------------------------------- |
| `interact_scan` | The icon pre-filter that decides **when to stop** while walking | The shipped one, looking for the default icon |
| `interact_text` | The OCR text that decides **whether this is it** once stopped | The point is not async; it interacts on arrival |
| `interact_rec` | Whether a match **presses anything** | It presses |

What gets pressed is not customizable — it is always the interact key (F on Windows, a key code on macOS, a tap on the recognized prompt on touch controllers). To keep navigation from pressing at all, set `interact_rec`, see [Recognize Only, Never Press](#recognize-only-never-press-rec-mode). Everything past the UI the interaction opens belongs to the outer Pipeline; navigation returns once the point is done. See [Replacing the Icon Pre-filter](#replacing-the-icon-pre-filter).

### Writing Method

In `path`, write the interaction point in the object form and add `interact_text`:

```json
"path": [
    { "action": "ZONE", "zone_id": "Wuling_Base" },
    [405, 1592],
    { "action": "INTERACT", "target": [331, 1578], "interact_text": "登记", "required": true }
]
```

- `interact_text` accepts a string or an array of strings; any one of them matching is enough to press.
- When several routes recognize the same set of text, write `interact_text` as `{ "node": "..." }` to name an OCR node instead, see [Sharing One Text Table Across Routes](#sharing-one-text-table-across-routes).
- The text is matched against the OCR result as a **regular expression**, the same semantics as Pipeline's `expected`. Copy the in-game wording as-is; escape regex metacharacters yourself if the wording contains any.
- When a whole route belongs to one business, put `interact_text` at the top level of `custom_action_param` as the default, see [Node Parameters](#node-parameters).
- When navigation should only walk there and recognize the prompt, leaving the press to the outer Pipeline, add `interact_rec`, see [Recognize Only, Never Press](#recognize-only-never-press-rec-mode).
- A single route may mix interaction points from several businesses, and mix them with `COLLECT` / `DIG`, in any number.
- **No trailing `true` is needed**: `INTERACT` is already handled with strict arrival semantics.
- This example requires the interaction to happen, hence `required: true`; if the interaction only serves as a route fallback after zipline planning fails, it can be omitted. See [Execution Order and `required`](#execution-order-and-required).

In the GUI: walk up to the interactable normally while recording, change that point's action to `INTERACT` after stopping, then add `interact_text` to it by hand after exporting the `path` (the editor does not emit this field yet).

### Writing Text That Survives OCR

OCR is not always reliable, which is why this field is **a set of** regular expressions rather than one literal line. List every variant:

```json
{
    "action": "INTERACT",
    "target": [331, 1578],
    "interact_text": [
        "^登记$", // anchored short verb, so other words in the ROI cannot latch on
        "^登記$", // traditional
        "(?i)^Register$", // (?i) ignores case
        "(?i)^Sign\\s*In$" // \\s* absorbs spaces OCR splits in
    ]
}
```

Rules of thumb:

- **Anchor first.** Matching is "contains", not whole-string equality, so for a short verb prompt like `登记` (Register) write `^登记$`; an unanchored `登记` matches any text in the same ROI that contains those characters.
- **Prefer too narrow over too wide.** A miss only wastes one visit to the point (navigation does not fail, see below), whereas a false match presses the key at the wrong thing.
- **List variants one per line instead of expecting one pattern to cover every language.** Write simplified, traditional, English and Japanese separately; the syntax is Perl-flavoured, so inline flags such as `(?i)` are available. Remember that a backslash is doubled in JSON.
- **Leading and trailing spaces need no handling** — the recognized text is trimmed before matching; `\\s*` is only for spaces split into the middle of the text.
- **JSONC comments work.** Note which language or which UI each line covers, and keep the reason when a line is disabled.
- **A malformed regex does not fail silently.** The injected text is validity-checked first, and an invalid pattern makes the whole subtask refuse to dispatch, logging `regex invalid`, `failed to override_pipeline` and `Prompt subtask failed to dispatch` in sequence; the point simply does not press, and navigation still does not fail.
- A ready-made model is `AutoPickInteractive` in `assets/resource/pipeline/RealTimeTask/AutoPick.json`: it mixes item names with verbs like `^采集$` and `^打开$` in one table, and records why a few lines are left out.

Reuse boundary: within one route a shared table goes at the top level of `custom_action_param` (see [Node Parameters](#node-parameters)); to share a table across routes, put it in an OCR node, see the next section.

### Sharing One Text Table Across Routes

When several routes recognize the same set of prompt text (most commonly one business spread over several areas), write `interact_text` as an object naming an OCR node, and the table only has to live in that one place:

```json
"path": [
    { "action": "INTERACT", "target": [331, 1578], "interact_text": { "node": "MyBusinessInteractText" }, "required": true }
]
```

When the whole route shares it, it can likewise go at the top level:

```json
"custom_action_param": {
    "interact_text": { "node": "MyBusinessInteractText" },
    "path": [
        ...
    ]
}
```

The named node is just an ordinary OCR node, with the text written in its `expected`; such a node often exists already (the recognition node of the button to be clicked after stopping), in which case no new node is needed at all:

```json
"MyBusinessInteractText": {
    "recognition": {
        "type": "OCR",
        "param": {
            "roi": "MyBusinessInteractButton",
            "expected": ["^登记$", "^登記$", "(?i)^Register$"]
        }
    }
}
```

Some boundaries:

- **Only `expected` is read; the node itself is never dispatched.** Its `roi` and other fields belong to whoever actually runs it; navigation neither reads nor changes them.
- **It must be an OCR node, and `expected` must be a non-empty array without empty strings.** Otherwise the log carries one `Interact text node must recognize by OCR` or `has no usable expected list` line, the point falls back to the plain meaning (press the interact key once upon arrival), and **the route still runs**.
- **A malformed object is a hard error.** A misspelled key, or a `node` that is not a string or is an empty string, makes the whole node parameter parse fail — the same failure as an empty `interact_text` string.
- **A value written on the point overrides the top level**, the same rule as for literal text; text and a node never coexist on one point, because they are the same field.
- **The node is read before the run starts**; on success the log carries one `Interact text resolved from node` line.

### Replacing the Icon Pre-filter

On the last stretch toward an interaction point, a background pass looks for the prompt icon at a fixed cadence and stops only on a hit. This step only decides whether stopping is worth it — after the stop, `interact_text` has to confirm before anything is pressed — so it can afford to be loose.

The shipped one is an ordinary Pipeline node, `MapNavigatorInteractScan`, with its `roi` / `template` / `threshold` written right there. When the prompt icon looks different, or does not appear in the default region, copy it, adjust it, and name it with `interact_scan`:

```json
"MyBusinessInteractScan": {
    "recognition": {
        "type": "TemplateMatch",
        "param": {
            "roi": [755, 330, 297, 312],
            "template": "MyBusiness/InteractHint.png",
            "threshold": 0.75
        }
    }
}
```

```json
{
    "action": "INTERACT",
    "target": [331, 1578],
    "interact_text": "登记",
    "interact_scan": "MyBusinessInteractScan",
    "required": true
}
```

- This node is **never dispatched**; MapNavigator only reads those three parameters out of it. It therefore needs no `action`, no `next`, and does not have to hang off any chain.
- `recognition.type` must be `TemplateMatch`. `template` may list one or more images, and any one matching stops the walk; when one prompt changes its icon with state (an Energy Alluvium's "Activate" versus "Claim / Abandon"), list them all. A single `threshold` applies to every image, or give one per image (defaulting to `0.75`). `roi` must be absolute coordinates in the 1280×720 base frame — the offset-from-previous-node form is not supported, since the pre-filter runs independently per frame and has no "previous node".
- `template` resolves as a path under `image/`, the same as anywhere else in the Pipeline, and a platform overlay's same-named image wins as usual — so swapping the image for touch backends only means adding one under `resource_adb`.
- If any of the above does not hold, the log carries one `Prompt scan node ...` line and **the points naming it fall back to "recognize once upon precise arrival"**. It does not take the route down with it and it does not silently press the wrong thing.
- At most 4 pre-filters are armed per route (one background thread each). One node named by several points counts once. Anything beyond that reports `Prompt scan node dropped at the cap`, and those points fall back to arrival recognition too.
- **A pre-filter needs `interact_text` alongside it.** A point naming only `interact_scan` falls back to the plain meaning (press the interact key on arrival) with the pre-filter inert, and logs one `names a prompt scan node without any interact text` line. The text may live on the point or be inherited from the route root.
- A loose threshold cannot stall navigation: one pre-filter hit completes the point, so a pre-filter that fires on anything only spends its own points one by one and the route still drains forward. Those points do not actually interact, though, so set the threshold from the icon itself.

### Recognize Only, Never Press: rec Mode

An `INTERACT` point carrying `"interact_rec": true` keeps the whole sequence — the pre-filter while walking, the stop when the prompt appears, the OCR confirmation once stopped — and **drops only the final key press**. The arrival radius stays tightened to the async-interaction value, and a prompt hit still completes the point.

When this is what you want: the prompt is up but which entry to take is the business's decision, while the interact key can only ever take the default one. The typical shape is one interactable carrying several rows (Claim / Abandon and the like) — the key always takes the first row, so the business's own "which row" switch never gets to matter. Let navigation just get there and recognize the prompt, hand the choice back to the outer Pipeline, and that switch is in charge again.

```json
{
    "action": "INTERACT",
    "target": [331, 1578],
    "interact_text": "领取",
    "interact_rec": true,
    "required": true
}
```

- **It needs `interact_text` alongside it**, for the same reason `interact_scan` does: a point whose text never resolved cannot enter the async path at all and falls back to the plain meaning (press the interact key on arrival), which is exactly what rec mode exists to avoid. To keep that degradation from pressing anyway, the plain path honours this field too — it skips the press and logs one `INTERACT in rec mode, skipping the key press` line.
- **The recognition after stopping still runs**; only the recognition node's own key action is swapped for `DoNothing`. So `interact_text` is authored exactly as before, and whether it matched still shows up in the log.
- **Navigation emits no click at all.** Whether the interaction happened is entirely the outer Pipeline's to validate — that was already async interaction's contract; rec mode just follows it through.
- When a whole route belongs to one business, `interact_rec` can go at the top level of `custom_action_param`, see [Node Parameters](#node-parameters) — bearing in mind it only ever switches points on.

### Arrival Determination and Movement

Async interaction points use the same set of values as collection points, all runtime-managed, with nothing for the path author to control:

- The arrival radius is tightened to a smaller value, relaxing back to the ordinary radius only when the point stays out of reach for a long time.
- Automatic sprint is disabled for the whole segment approaching such a point, preventing overshoot.
- The last stretch automatically switches from running to walking, so the character is less likely to run past the prompt's trigger range.

The pre-filter only runs **while that point is the one being walked to and the last stretch has been entered**, at a fixed cadence, and a tick where the background reported nothing costs nothing; a route with no async interaction points is therefore unaffected. Passing other interactables further away cannot be mistaken for this point's prompt.

### Fallback and Failure Semantics

- If the prompt never shows up on the way, **the authoritative recognition still runs once upon precise arrival** — a missed pre-filter does not skip the point. Every async interaction point therefore gets exactly one authoritative recognition: a hit on the way completes it, and otherwise arrival makes it up.
- **Async interaction never fails navigation.** The key press only happens when OCR matches the injected text, and never at all in rec mode; navigation itself does not verify that the interaction actually took place, and does not error out because no prompt was recognized. Whether the interaction succeeded has to be validated by the outer Pipeline (for example, the UI that should appear afterwards).

When the interaction opens a UI (a registration desk, a commission board), **make that point the last one of the route and mark it `required: true`**: once the UI is up the character stops moving and the minimap is covered, so later points cannot be walked. This is the same author-side discipline as plain `INTERACT` without `interact_text` — async or not makes no difference here.

The `MapNavigatorInteract` node ships with a placeholder text that can never match, so a route that injects no `interact_text` recognizes nothing there and cannot press by mistake.

### Related Files

| File | Responsibility | When Changes Are Needed |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| The business's own route JSON | The `MapNavigateAction` node, interaction coordinates and `interact_text` | Add interaction points, change wording |
| `assets/resource/pipeline/MapNavigator/Interact.json` | Async interaction subtask, entry point `MapNavigatorInteractStart`, holds the prompt ROI and the key action; the shipped pre-filter `MapNavigatorInteractScan` lives here too | When the prompt region, the default interact key or the shipped pre-filter changes |
| `assets/tasks/setting/Keymap.json` | Interact key rebinding; `KeymapInteract` applies to this node as well | Add or remove nodes affected by rebinding |
| `assets/resource_macos/pipeline/MacOSKeyMap.json` | macOS key-code overrides, listed alongside the other interact-key nodes | Add or remove nodes that press the interact key |
| `assets/resource_adb/pipeline/MapNavigator/Interact.json` | On ADB / cloud / PlayCover, taps the recognized prompt box instead (a touchscreen has no F key) | When the touchscreen action changes |

**In most cases, path authors only need to modify their own route JSON.**

### Parts Path Authors Do Not Need to Touch

The following files are maintained by cpp-algo developers; path authors do not need to modify them:

- `agent/cpp-algo/source/MapNavigator/semantic/async_prompt_action.h` / `.cpp`: the shared implementation of prompt-driven actions, used by both `COLLECT` and async `INTERACT`.
- `agent/cpp-algo/source/MapNavigator/sensing/prompt_scan_profile.h` / `.cpp`: reading the pre-filter parameters out of a Pipeline node, and the validation chain around it.
- `agent/cpp-algo/source/MapNavigator/sensing/pipeline_node_reader.h` / `.cpp`: reading a Pipeline node's ROI, recognition type and interact text table.
- `agent/cpp-algo/source/MapNavigator/navi_config.h`: subtask node names, pre-filter cadence and last-resort constants, arrival values and others.
- `agent/cpp-algo/source/MapNavigator/navi_param_parser.cpp`: parsing of `interact_text` / `interact_scan` / `interact_rec` and propagation of the route-wide defaults, plus reading a named OCR node into a text table before the run starts.
- `agent/cpp-algo/source/MapNavigator/semantic/semantic_nodes.cpp`: the fallback execution logic upon arrival.

---

## Finding and Approaching a Target `FIND`

`FIND` handles targets that exist **only as a recognition box and have no coordinate**, such as NPC nameplates and prop labels. At runtime, the navigator steers the character by the recognition box, rotates the camera to search when the target is off screen, and completes the node once the configured completion condition is met.

Each `FIND` point is configured with two parameter groups: a **target source** (`find_target` or `find_text`; exactly one) and a **completion condition** (`find_stop` or `find_arrive`; at least one).

### Configuration

| Field | Type | Description |
| ---------------- | ------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `find_target` | string | Node name of the target recognition. Any box-producing node may be used, e.g. `OCR`, `NeuralNetworkDetect`, `TemplateMatch`, or an `And` combo. Exactly one of `find_target` / `find_text`. |
| `find_text` | string \| string[] \| object | Inline OCR text table: a string or an array of strings. `{ "node": "SomeOcrNode" }` is also accepted, and is equivalent to writing that node name into `find_target`. Exactly one of `find_target` / `find_text`. |
| `find_stop` | string | Node name of the completion criterion; a hit completes the point. At least one of `find_stop` / `find_arrive`. |
| `find_arrive` | [number, number] | Completion coordinate `[x, y]`; entering its 2-unit radius completes the point (the same radius as strict arrival, `kStrictArrivalLookaheadRadius`). At least one of `find_stop` / `find_arrive`. |

Additional rules:

- All four fields accept camel case (`findTarget`, `findText`, `findStop`, `findArrive`).
- Node-level fields win; route-wide defaults are filled by group: the **target source** (`find_target` / `find_text`) is filled as a whole — a point carrying either field receives neither route-wide value, and only a point carrying neither receives both. The **completion condition** (`find_stop` / `find_arrive`) is filled field by field, supplying only the fields the point does not set.
- When both `find_stop` and `find_arrive` are configured, whichever is satisfied first completes the point.
- The following configurations fail during parameter parsing and never reach runtime: on a `FIND` point, both or neither of `find_target` / `find_text`; neither of `find_stop` / `find_arrive`; a `find_arrive` that is not an array of exactly two numbers; a route that sets both `find_target` and `find_text` while containing a `FIND` point.

> **Note**: `find_stop` must be able to distinguish the target from the background. `FIND` completes on a recognition hit alone and does not verify what the box points at; an overly broad criterion (for example, "an interact prompt on any character") completes the point early when other interactables pass by.

### Node Forms

- **With a coordinate**: when `target` is configured, the node first walks to that coordinate (the anchor) as an ordinary movement point, then starts the search.
- **Control node**: without `target`, the search starts as soon as the character reaches the point.
- The two forms differ only in where the search starts.
- The array form `[182.52, 173.4, "FIND"]` carries a coordinate only; the target source and completion condition must come from route-level defaults.
- `FIND` is an intrinsic boundary node and is never skipped by zipline planning.

### Runtime Behavior

- **Target on screen**: corrects the heading by the box's horizontal offset and keeps walking. An offset within tolerance keeps the walk going; an offset beyond tolerance up to twice the tolerance is corrected while walking; an offset beyond twice the tolerance pauses the walk until the heading is aligned, after which the walk resumes.
- **Target passed**: when the box center drops below roughly two thirds of the screen height, the character is considered past the target; one short backward pulse is issued, followed by re-alignment.
- **Target off screen**: rotates the camera in fixed 30° steps. The search direction is the side on which the target was last seen and remains constant for the search; a target never seen uses a default side.
- **Target briefly lost**: after the target has been seen, 1-2 consecutive misses hold position; the search starts after three consecutive misses. A target never seen is searched for immediately, without waiting.
- **Completion checks while walking**: while the character is moving, `find_stop` is re-checked at fixed intervals within each walking cycle. When the criterion node resolves to a template (`TemplateMatch`; interact prompt icons usually do), a millisecond template pre-filter runs first, and a hit stops the walk for authoritative confirmation; a pre-filter false positive resumes the walk. When it does not (for example, an OCR node), the authoritative recognition runs directly at the same cadence.
- These cadences and thresholds are controlled by cpp-algo constants (`kFindSearchStepDeg`, `kFindMissGraceTicks`, `kFindAlignTolerancePx`, `kFindStopProbe*`, etc., in `navi_config.h`); route authors do not configure them.

### Completion and Failure

- **Completion**: `find_stop` hits, or the position enters the `find_arrive` radius (whichever is satisfied first); the node then completes and the flow advances to the next point.
- **Failure**:
    - a named node that does not exist, a recognition call that errors, or a hit without a valid recognition box (for example, using a `DirectHit` node as the target) fails the node immediately; this is not treated as "not recognized";
    - exhausting the budget (`kFindMaxSteps`, 48 ticks; or `kFindBudgetMs`, 60 seconds) without completion fails the node; `MapNavigateAction` returns false and the outer `on_error` or retry logic takes over.

### Relation to Adjacent Legs

The `FIND` search phase uses open-loop camera rotation and does not read the minimap (only the `find_arrive` distance test reads the locator). When the node ends, the character may stand outside the walkable mesh facing an arbitrary direction; the navigator resets this leg's steering and corridor bookkeeping on exit.

- Adding an ordinary movement point after a `FIND` point is recommended so that the next leg can re-localize;
- a `FIND` point as the final point of a route is also valid: the route finishes once it completes.

### Example

In the examples below, `GiftOperatorName` and `GiftOperatorApproachStop` are example node names; replace them with the recognition nodes of the business.

With a coordinate (walk to the anchor first, then search):

```json
{
    "action": "FIND",
    "target": [
        182.52,
        173.4
    ],
    "find_target": "GiftOperatorName",
    "find_stop": "GiftOperatorApproachStop"
}
```

Control node (search starts as soon as the point is reached):

```json
{
    "action": "FIND",
    "find_target": "GiftOperatorName",
    "find_stop": "GiftOperatorApproachStop"
}
```

### Related Files

| File | Responsibility | When Changes Are Needed |
| -------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ------------------------------ |
| The business's own route JSON | The `MapNavigateAction` `path`, the `FIND` point and its `find_*` fields | Add a search point, change the target |
| The business's own recognition nodes | The nodes named by `find_target` / `find_stop` | The ROI or the text of the target or prompt changes |
| `assets/resource/pipeline/MapNavigator/Find.json` | The built-in OCR node `MapNavigatorFind` used for inline text; called per frame by the navigator and never dispatched on its own | The default ROI for inline text |
| `agent/cpp-algo/source/MapNavigator/semantic/find_action.cpp` | The search and approach implementation and the bookkeeping it clears on the way out | Maintained by cpp-algo developers, tuned against a live client |

---

## Ending on a Recognition Hit `TRIGGER`

`TRIGGER` goes at the end of a route and means "arrived once a certain screen shows up": a story sequence starting, a dialog or menu popping up, a prompt appearing.

The navigator recognizes `trigger_node` while it walks. On the first hit it stops, the whole navigation succeeds, and the outer Pipeline's `next` takes over. A hit counts wherever the character is, even halfway along the route. If nothing hits, reaching `target` completes the navigation.

### Usage

Replace `MyTaskTriggerNode` with your own recognition node:

```json
[
    {
        "action": "NAVMESH",
        "target": [
            182.52,
            173.4
        ]
    },
    {
        "action": "TRIGGER",
        "target": [
            182.52,
            173.4
        ],
        "trigger_node": "MyTaskTriggerNode"
    }
]
```

- `trigger_node` (or `triggerNode`) names a recognition node. Only its recognition runs; its action and `next` never execute.
- Use the object form and make it the last point of the route; anything else fails at parse time.
- `TRIGGER` walks straight to `target`. For pathfinding, put a `NAVMESH` point with the same coordinate in front of it as in the example; a hit during that leg counts too.

### Choosing the Node

- Recognition starts the moment navigation starts, so pick a screen that only appears at the destination. A dialog or prompt that also shows up elsewhere on the route ends the navigation there.
- Pick something that stays on screen for a while. Recognition runs every 300 ms and can miss a brief flash.
- Reaching `target` completes the navigation even if the screen never appears. Put `target` where the character should stop.

The hit screen often covers the minimap. The navigator loses its position then, but recognition keeps running and no unstick keys are pressed, so nothing disturbs the screen that just appeared.

### Failure

`MapNavigateAction` returns false when:

- `trigger_node` does not exist or its recognition errors (fails immediately);
- nothing hits and localization stays lost for more than 30 seconds.

### Related Files

| File | Responsibility | When Changes Are Needed |
| ----------------------------------------------------- | ----------------------------------------------- | ---------------------------------------------- |
| The business's own route JSON | The `MapNavigateAction` `path`, the `TRIGGER` point and its `trigger_node` | Add a trigger end point, change the node |
| The business's own recognition nodes | The node named by `trigger_node` | The ROI or the text of the hit screen changes |
| `agent/cpp-algo/source/MapNavigator/semantic/trigger_action.cpp` | Recognition on the way and completion on arrival | Maintained by cpp-algo developers |

---

## Jump Pads `JUMP_PAD`

There are two kinds of jump pads: ones that launch the character as soon as it steps on, and ones that need a jump while standing on them. Both are written as the same point, with `target` at the pad center; the navigator decides whether to jump:

```json
[
    {
        "action": "NAVMESH",
        "target": [
            1001.4,
            302.0
        ]
    },
    {
        "action": "JUMP_PAD",
        "target": [
            1001.89,
            305.68
        ]
    },
    [
        995.79,
        320.78
    ],
    {
        "action": "NAVMESH",
        "target": [
            1021.31,
            368.82
        ]
    }
]
```

### Runtime Behavior

1. Walks in and stops once inside the arrival band;
2. Waits until the character stands still. If it is more than 0.5 from the center, turns the camera toward the center and holds forward for a short step until it is within that range; a pad that launches on contact launches during such a step;
3. Still on the pad once within range, it jumps. If the character is still on the pad 1.2 seconds after the jump, that jump did not launch: the range tightens to 0.25, it steps again and jumps again;
4. Moving more than 3 from the center counts as launched; two consecutive unchanged fixes after that count as landed, and the route continues with the following points from the landing spot. The landing spot is set by the pad and does not depend on the facing at the jump.

### Writing Tips

- `target` must be the pad center. A pad that needs a jump only reacts within about 0.8 of its center; off-center, the jump is just a regular jump in place.
- Stop the previous point 3–4 away from the pad on the walkable side. Pads often sit at cliff edges, so never place the approach point over the drop.
- Follow `JUMP_PAD` directly with the landing spot as a plain point, then continue with `NAVMESH` as usual. Each leg is planned from the point before it; without the landing point, the next leg would start from the pad side.

### Failure

`MapNavigateAction` returns false when:

| Reason | Meaning |
| -------------------------- | ------------------------------------------------------------ |
| `jump_pad_not_reached` | The step budget ran out before the character stood within range |
| `jump_pad_not_launched` | Three jumps did not launch, or no launch within 20 seconds of reaching the pad |
| `jump_pad_landing_timeout` | Did not stand still within 15 seconds after launch |

### Related Files

| File | Responsibility | When Changes Are Needed |
| ------------------------------------------------------ | -------------------------------------------- | --------------------------------- |
| The business's own route JSON | The `JUMP_PAD` point, its approach point and landing point | Add or adjust a jump pad segment |
| `agent/cpp-algo/source/MapNavigator/semantic/jump_pad_action.cpp` | Standing, jumping, launch and landing detection | Maintained by cpp-algo developers |

---

## Practical Tips

1. **Prefer `NAVMESH`; record only when semantics are needed.** For plain-movement routes, place the points by hand in the path editor, confirm them with planning, and copy; adjust a target the preview cannot reach on the spot instead of waiting for it to fail at runtime. Only routes with semantics such as interactions and map transitions need recording.
2. **Record rather than hand-write.** Walking through once in reality is usually more accurate than filling coordinates by feeling; if the precision of the recorded points is insufficient, move more slowly.
3. **Keep the starting state stable.** Before recording, tidy up the character's position and viewpoint, which significantly reduces later point correction.
4. **Keep special action points few and precise.** Add `INTERACT`, `JUMP_PAD`, `TRANSFER`, and `PORTAL` only where an interaction, a jump pad, or an area switch is needed; with ziplines enabled, points that must be executed should be marked `required`. `HEADING`, `COLLECT`, `DIG`, `FIND`, and `TRIGGER` are always kept.
5. **Always check map transition points on cross-area routes.** Automatic addition of `PORTAL` is only a semantic annotation and does not mean every cross-area boundary is inherently reasonable.
