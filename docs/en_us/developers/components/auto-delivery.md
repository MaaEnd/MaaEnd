# Development Manual - AutoDelivery Delivery Component

AutoDelivery is a task-agnostic automatic delivery component. The caller only needs to enter the public node `AutoDelivery`; the component first makes sure the delivery job details are selected on the Missions screen, then decides on its own whether it currently needs to pick up or deliver, and completes the corresponding flow.

## Usage

### Prerequisites and Entry

The caller does not need to open the delivery job details itself. The component first checks whether the Missions screen already has the "Delivery Job" details selected and waits for the detail page to finish loading; if not, it enters the Missions screen through `SceneEnterMenuMission`, scrolls the mission list to find and select the delivery job, and fails if the list reaches the bottom without one.

```json
{
    "MyTaskOpenCurrentJobDetail": {
        "next": [
            "AutoDelivery"
        ]
    }
}
```

`AutoDelivery` is the only public execution entry. The other `AutoDelivery...` nodes belong to the component's implementation or its configuration contract and should not be written into the caller's `next` as standalone steps.

### Default Flow

```text
Any screen
  -> AutoDelivery
       -> Already picked up: recognize destination -> return to overworld -> go to destination -> submit cargo
       -> Not picked up yet: recognize depot -> quick teleport -> return to overworld
            -> go to depot -> pick up -> reopen delivery job details
            -> recognize destination -> return to overworld -> go to destination -> submit cargo
```

The component works out the stage on its own from the area and the mission conditions in the job details. On entry, and when the job details need to be reopened after pickup, the component runs `SceneEnterMenuMission` and then the delivery job selection flow through `SubTask` (going straight on if the delivery job details are already selected); only after confirming both the Missions screen and the "Delivery Job" details on the right does it go on to recognize the depot or the destination. The caller does not need to configure any extra entry or anchor for switching to the details.

When page information cannot be recognized, a route cannot be resolved, or an operation fails, the component does not forward any extra `on_error` and instead ends the task according to the Pipeline's default behavior. If the caller needs to return to its own flow at a normal stage, it should use the anchors in the next section instead of referencing internal nodes directly.

## Stage Exit Anchors

All four stage exits take the form "anchor first, default node as fallback". When unset, the default flow continues; the caller only configures them when it needs to cut the flow short or continue its own task after submission:

| Anchor | Trigger point | Default behavior when unset | Typical use |
| --- | --- | --- | --- |
| `AutoDeliveryAfterRecognizeDestination` | After the delivery destination has been recognized | Return to the overworld and go to the destination | When the cargo has already been picked up, stop modes such as teleport-only and walk-to-depot-only from moving on |
| `AutoDeliveryAfterQuickTeleport` | After quick-teleporting near the depot | Go to the depot | Quick teleport only |
| `AutoDeliveryAfterNavigateDepot` | After reaching the depot | Pick up | Walk to the depot node only |
| `AutoDeliveryAfterSubmitGoods` | After the delivery reward screen is closed | End the component normally | Return to the caller's main loop or completion node |

A full delivery usually only needs the exit after submission:

```json
{
    "MyTaskFullDelivery": {
        "recognition": "DirectHit",
        "action": "DoNothing",
        "anchor": {
            "AutoDeliveryAfterSubmitGoods": "MyTaskDeliveryDone"
        },
        "next": [
            "MyTaskOpenCurrentJobDetail"
        ]
    }
}
```

## Zipline Configuration

Depot and destination navigation both run with `zip: false` by default. To allow ziplines, have a task option override `attach.zip` of the two fixed navigation nodes, without modifying `AutoDelivery.next`:

```json
{
    "pipeline_override": {
        "AutoDeliveryNavigateDepot": {
            "attach": {
                "zip": true
            }
        },
        "AutoDeliveryNavigateDestination": {
            "attach": {
                "zip": true
            }
        }
    }
}
```

On the Go side, `AutoDeliveryResolveDepotAction` / `AutoDeliveryResolveDestinationAction` read the `attach.zip` of these two nodes respectively and pick the plain or the zipline-enabled generated route node accordingly. `AutoDeliveryNavigateDepot` and `AutoDeliveryNavigateDestination` are not execution entries, but their node names are part of the configuration contract used by task options. Allowing ziplines only means MapNavigator may choose a zipline when appropriate; it does not guarantee that the actual route will use one.

## Component Maintenance

This section is for maintainers of AutoDelivery's data and implementation; ordinary callers do not need to rely on the files, route fields, or recognition details in it.

### Data Locations

| Path | Contents |
| --- | --- |
| `tools/zmdmap/data/delivery_destinations.json` | Depots, destinations, five-language text, coordinates, and ownership relations, generated and published automatically by the zmdmap data CI |
| `tools/pipeline-generate/AutoDelivery/routes.json` | Depot, destination, and position-correction route overrides for when the automatic routes do not apply |
| `assets/resource/pipeline/AutoDelivery/Routes/{RouteFileId}.json` | Depot and destination navigation nodes generated from the data above, grouped by depot node, each of which can be test-run on its own |
| `assets/data/AutoDelivery/catalog.json` | Runtime OCR matching catalog, keeping only text, ownership relations, and generated node names |
| `assets/resource/pipeline/AutoDelivery/Common.json` | Public entry and job details recognition |
| `assets/resource/pipeline/AutoDelivery/Pickup.json` | Quick teleport, depot navigation, and pickup |
| `assets/resource/pipeline/AutoDelivery/Delivery.json` | Destination recognition, returning to the overworld, destination navigation, and cargo submission |
| `agent/go-service/autodelivery/` | OCR matching, runtime catalog validation, and dispatch to generated route nodes |

For ordinary depots and Resource Recycle Stations, `tools/zmdmap/datasets/delivery_destinations.py` provides the game-data coordinates and yaw. The route generator first generates a `NAVMESH` must-pass point with `required: true` 8 meters out along the target's yaw direction, then heads to the center of the interaction range 2 meters in front of the entity (the entity's origin sits inside the machine's footprint, which the navigation pack cuts out), which guarantees an approach from the front of the interaction (when the orientation is unsuitable, correct it with `yaw` in the override entry, see below); ordinary recipient NPCs still get only a single `NAVMESH` target at the original coordinate. Every generated point carries `target_deck_y`, whose value is the entity's world height in the source data; the approach point and the destination are on the same deck and share this height, which is used to pick the target deck among overlapping walkable surfaces. Deck declarations in manually overridden paths are kept exactly as measured. Only maintain overrides in `routes.json` for a broken mesh, layered terrain, or when a special standing position is needed.

The generator appends an `INTERACT` interaction point to the end of the hand-in main and retry routes: when the last point is a `NAVMESH`, it adds `required: true` to it and attaches the interaction point at the same coordinate; when the last point is a bare coordinate, the whole point is replaced by the interaction point (for recycle stations it lands on the center of the interaction range). The interaction point pre-filters the hand-in icon with `AutoDeliverySubmitGoodsInteractScan`, then confirms the text: Resource Recycle Stations are confirmed by `AutoDeliverySubmitGoodsInteractText` (`交货`, Deliver), recipient NPCs by the destination's five-language name. The interaction point sets `interact_rec`, so navigation ends as soon as the prompt is recognized, without pressing the interact key; the hand-in is still done by `AutoDeliveryCheckSubmitGoodsTarget` → `AutoDeliverySubmitGoods`, which recognize the hand-in icon and click it. If the prompt is still not recognized once the character reaches the point, navigation fails.

Two kinds of targets get no interaction point, and their routes end the same way as before: depot routes (callers that only walk to the depot node without picking up use this route too, and pickup happens on the business side) only move the end point to the center of the interaction range, and the bare-coordinate last point of a manual route is moved there as well; for an NPC with `verify_name` enabled, the business side still checks the name and then clicks.

After `pnpm generate:AutoDelivery` runs, each main route produces two `AutoDeliveryRoute...` nodes: a plain one and one that allows ziplines. Every depot and delivery destination also gets a retry node without ziplines, built from the same two-point approach route; an explicit `retry_path` can override that route. The fixed `AutoDeliveryNavigateDepot`, `AutoDeliveryRetryNavigateDepot`, `AutoDeliveryNavigateDestination`, and `AutoDeliveryRetryNavigateDestination` are `SubTask` dispatchers: the Go Service only picks a generated node name based on the OCR result and no longer injects coordinates or a full `path` into the Pipeline. Generated nodes are public single-route test entries, and their `desc` notes which depot node the route belongs to; they do not replace `AutoDelivery`, the only entry for the full delivery business.

The generator uniformly inserts a `ZONE` declaration as the first point of every generated route (`ValleyIV_Base` / `Wuling_Base`, taken from the BaseNav region of the depot's `map`) as the expected zone for the locator to start from; `path` / `retry_path` / `departure_path` therefore never need a first-point zone declaration, while `ZONE` points kept mid-path for cross-zone routes stay as usual.

Currently only **start points on the base map** are supported: `maps[map].zone` in `delivery_destinations.json` only has `map01base` / `map02base`. If a depot inside a tier or indoors appears in the future, tier start zones and tier coordinates (`target_tier`) must be supported first before a usable route can be generated; otherwise the first-point declaration generated from the base region makes the locator fail immediately inside the tier because the zone does not match.

The override file only has the top-level `depots` and `destinations`:

| Array | Field | Meaning |
| --- | --- | --- |
| `depots` | `path` | Full MapNavigator route from the quick teleport landing point to the depot; when not configured, the automatically generated depot coordinates are used |
| `depots` | `retry_path` | Overrides the automatic two-point position-correction route run when the pickup button is still not recognized after the circle search |
| `depots` | `departure_path` | Shared departure route prepended to the routes of the destinations that belong to this depot |
| `depots` | `yaw` | Overrides the facing angle used by the main route's approach point and the automatic retry route; use it when the depot faces a wall |
| `depots` | `offset` | Fine-tunes the automatically generated depot navigation landing point (base map pixel offset) |
| `depots` | `walk_only` / `zipline_only` | Overrides the zipline policy of this depot's main route, see below; the two are mutually exclusive |
| `destinations` | `path` | Full destination route including the final waypoint; when not configured, the automatically generated destination coordinates are used |
| `destinations` | `retry_path` | Overrides the automatic two-point position-correction route run when the submit button is still not recognized after the circle search |
| `destinations` | `yaw` | Overrides the facing angle used by the recycle station main route's approach point and the automatic retry route; the NPC main route remains a single destination point |
| `destinations` | `offset` | Fine-tunes the automatically generated destination navigation landing point (base map pixel offset) |
| `destinations` | `walk_only` / `zipline_only` | Overrides the zipline policy of this destination's main route, see below; the two are mutually exclusive |
| `destinations` | `verify_name` | After arriving at the destination, uses OCR to verify the character name in the world interact prompt, see below; only supported for NPC destinations |

The `area` in the destination catalog comes from `LevelDescTable.showName`, which is the level name actually shown on the job detail page, not the depot node name in Regional Development. Ordinary recipient jobs match the destination by `buyerName`; recycle station jobs, whose `kind` is `recycle_bin`, show no buyer name and match the full `mission` instead. When several identical recycle station texts exist in the same area, the text does not pick the destination; instead the job's map is opened and the world map icon decides which recycle station it is.

### `retry_path`

`retry_path` uses the same format as `MapNavigateAction.custom_action_param.path`. It runs from the actual position where the main route ended, and only the waypoints need maintaining: the generator injects the first-point zone declaration uniformly from the depot's `map`, a same-zone declaration is normalized and regenerated, and a cross-zone declaration is an error. When not configured, every depot and delivery destination uses the automatically generated "8-meter must-pass approach point → end point" route (for depots and recycle stations the end point is the interaction range center 2 meters in front of the entity, for NPCs the original coordinate), so no coordinates need copying by hand; the automatic route comes from the same source as the main route, and every point from the approach point to the destination carries `target_deck_y`, sharing the entity's world height. When the approach point's direction is unsuitable, prefer correcting it with `yaw` in the next section over rewriting the whole `retry_path`. This does not change the rule that the NPC main route remains a single destination point.

Configure `retry_path` only once you have confirmed that the automatic two-point correction does not apply, for example with a broken mesh, layered terrain, or a detour needed near the target. Do not use it to mask unstable templates, pages that have not loaded, or a wrong main route.

The execution boundaries are as follows:

```text
Go to the depot or destination
  -> Interaction button recognized on the first try: continue with pickup or submission
  -> Not recognized on the first try: circle-search first; if still not found and the catalog has a retry node, correct the position once -> recognize once more
  -> No retry node, or still not recognized after correction: end the task
```

The retry node does not inherit the main route's `zip`, and it forms neither an anchor nor a retry loop. A single pickup or delivery flow performs at most one position correction. On a hand-in route that ends with an interaction point, if the prompt is still not recognized at the point, the navigation itself fails and ends the task without the circle search or the position correction.

### `yaw`

`yaw` overrides the facing angle (in degrees) used to generate the automatic approach point. It is meant for cases where the recipient NPC actually faces a wall, or the source data lacks an orientation (`yaw` defaults to 0), so that the 8-meter approach point lands somewhere unreachable and the retry position correction is bound to fail. It only changes the direction of the approach point, not the route structure:

- Depots: applies to both the main route's approach point and the automatic retry route;
- Destinations: applies to the approach point of the Resource Recycle Station main route and to the automatic retry route. By the established rule, the NPC main route still generates only a single `NAVMESH` point at the original coordinate, so `yaw` only affects its retry route.

The value means the same as the source data's `yaw`: the approach point lies 8 meters out in the direction the entity faces, 0 degrees points to the top of the base map, and larger values rotate clockwise (90 degrees right, 180 degrees down, 270 degrees left); negative values or values above 360 are first normalized to `[0, 360)`.

`yaw` only takes effect when an automatic approach point exists: if an entry overrides both `path` and `retry_path`, the generator reports an error right away, so the setting cannot silently do nothing. When the approach leg has already been overridden with a measured path, or the approach distance needs adjusting, still use `retry_path`.

### `offset`

`offset` fine-tunes the automatically generated navigation landing point with a base map pixel offset `[du, dv]` (right is positive `u`, down is positive `v`). It is for cases where the coordinate projected from the source data is a few meters away from the actual interactable position: the landing point is blocked by an upper structure such as an elevated walkway, stops on another walkable surface, or the interaction area is a few meters away, so the character reaches the coordinate but cannot trigger the interaction button. Compared with rewriting the whole `path` / `retry_path`, it only changes the coordinate; the route segments and zone declarations are still maintained by the generator.

- Scope: the main route end points of depots and destinations, the automatic approach point, and the automatic retry route all shift together; the approach point is still recomputed from 8 meters and `yaw`, and `target_deck_y` stays unchanged.
- It does not affect the coordinates of the recycle station icon check on the world map: the `at` that `AutoDeliveryFindRecycleBin...` uses in `assets/resource/pipeline/AutoDelivery/RecycleBinCandidates.json` still comes from the source data.
- The shifted landing point must stay within the base map; an out-of-bounds point, anything other than two numbers, or an all-zero offset makes the generator report an error right away; so does an entry that overrides both `path` and `retry_path`, so the setting cannot silently do nothing.
- It can only correct the horizontal landing point. When the landing point itself is correct but stops on the wrong walkable deck (a height difference), neither `offset` nor `yaw` helps; override `path` / `retry_path` with a measured path instead.

### `walk_only` / `zipline_only`

These two fields override the zipline policy of one main route. They are mutually exclusive, and declaring both makes the generator report an error right away. The policy does not rewrite the `zip` of the generated nodes: plain nodes are always `zip: false` and `WithZipline` nodes are always `zip: true`; the two flags reach Go through the runtime catalog `assets/data/AutoDelivery/catalog.json` and take effect when route nodes are dispatched. Node names and parameters therefore always agree, and while troubleshooting you will never see a `WithZipline` node saying `zip: false`:

- `walk_only: true`: keeps the recorded path in full and forbids global zipline planning from skipping the author's waypoints. `AutoDeliveryResolveDepotAction` / `AutoDeliveryResolveDestinationAction` dispatch only the plain node whether or not the user enables ziplines, so even with ziplines enabled the route is walked strictly along the author's path; the zipline node this entry keeps in the mapping is only for single-route test runs. The action log includes `walkOnly=true`.
- `zipline_only: true`: the target can only be reached by zipline (such as the destination Pei Lingrong), and there is no usable walking route. If at runtime the user chooses to deliver on foot ("Prefer Ziplines for Delivery" is off), the Go side prints a red message explaining why and fails the action before `AutoDeliveryResolveDepotAction` / `AutoDeliveryResolveDestinationAction` dispatches the route, instead of silently degrading to walking to an unreachable spot and timing out.

Note that `zip: true` only means MapNavigator is allowed to use a zipline when appropriate; when no zipline coordinates have been imported or the zipline is not the cheaper option, navigation may still choose to walk. What `zipline_only` blocks is the configuration error "the user explicitly chose walking"; it does not guarantee that navigation planning will use a zipline.

### `verify_name`

The hand-in icon template is only a dozen or so pixels, so when other characters are around the target, template matching may hit someone else's interact prompt. `verify_name: true` makes the "already standing in front of the delivery destination" check verify the text in the interact prompt once more on top of the icon:

- The world interact prompt renders the target name in the prompt area next to the icon (NPCs show the character name, such as Zhao Zhao), and this prompt area shares the same ROI as the hand-in icon, so name recognition follows controller overrides along with it;
- After the Go side resolves the destination, it injects that destination's five-language name into the `expected` of `AutoDeliveryCheckSubmitGoodsName` and extends the `all_of` of `AutoDeliveryCheckSubmitGoodsTarget` to "hand-in icon + name check"; for destinations without it enabled, `all_of` keeps only the hand-in icon and behaves as before;
- A name miss is handled as "not in position": `CharacterSearchAction` first circles around searching, then the position-correction route runs once, and finally it fails, rather than handing the cargo to another character in the prompt.

Enable it only on NPC destinations confirmed to be disturbed by surrounding characters. The prompt text of Resource Recycle Stations and depot nodes does not show the destination name, so both the generator and the runtime refuse to enable this option for such destinations (`kind` must be `npc`).

### Verification

After changing data, recognition, or flow, regenerate the routes and the runtime catalog:

```powershell
pnpm generate:AutoDelivery
```

Run `pnpm check` / `pnpm test` as needed: run `pnpm test` locally when the change includes `tests/**`, and if you also changed `assets/**` or `tools/schema/**`, run `pnpm check` as well; otherwise there is no need to run them locally, just follow the relevant CI after opening a PR. See the [coding standards](../coding-standards.md#pre-submission-check) for details.

Static checks and node tests cannot replace in-game verification. After adding a region or changing an interaction screen, the paths for recovery before pickup, recovery after pickup, pickup position correction, NPC hand-in, and non-NPC hand-in still need to be verified one by one.
