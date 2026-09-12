# Developer Manual - SeizeDeliveryJobs Maintenance

This document describes the `SeizeDeliveryJobs` task: its runtime flow, the boundary between Pipeline and Go Service, generator inputs and outputs, and the maintenance procedure for adding maps, areas, and delivery destinations.

The task has two mostly independent paths:

- Without destination filtering, the Go Service scans jobs against the minimum reward threshold and clicks the first qualifying accept button.
- With destination filtering enabled, the Go Service caches all jobs that meet the reward threshold; the Pipeline opens `Check Location` for each job and uses an area-specific `MapFind` candidate set to inspect the destination map.

Both paths share the depot entry, job-list loading, reward OCR, and post-accept processing. Pipeline owns UI state and business routing; Go Service owns the complex job-card recognition and dynamic click coordinates.

> [!WARNING]
>
> `assets/tasks/SeizeDeliveryJobs.json`, `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsCommission.json`, `SeizeDeliveryJobsEndpointCandidates.json`, `SeizeDeliveryJobsEndpointDispatcher.json`, and `SeizeDeliveryJobsDestinations.json` are generated artifacts. Do not edit them directly; regeneration will overwrite manual changes.
>
> Area, destination, and match-message entries in `assets/locales/interface/*.json` are also maintained by the generator: display names and directions are registered in `endpoint-labels.json` (falling back to the source recipient name), so do not edit generated locale keys.

## At a Glance

| Module | Path | Purpose |
| ------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Task definition (generated) | `assets/tasks/SeizeDeliveryJobs.json` | Task options for minimum reward, commission source, destination filtering, post-processing, and repeat count |
| Main flow (hand-maintained) | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobs.json` | Task entry, depot entry branches, job loop, reward-threshold recognition, and success/failure handling |
| Shared job-list nodes (hand-maintained) | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsCommon.json` | Job-list entry, list-loaded checks, and job-card OCR nodes |
| Area depot nodes (generated) | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsCommission.json` | Per-map dispatch-voucher recognition, depot entry, depot confirmation, and region filter nodes |
| Destination candidates (generated) | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsEndpointCandidates.json` | Area OCR gates and `MapFind` candidate lists |
| Destination leaves (generated) | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsDestinations.json` | Per-destination `enabled` switches, match messages, and the common successor |
| Destination dispatcher (generated) | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsEndpointDispatcher.json` | Confirms that the map is open and routes by the upper-left sub-area name |
| Destination loop (hand-maintained) | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsEndpointFilter.json` | Caching jobs, opening `Check Location`, destination matching, closing the map, accepting, and refreshing |
| Auto-delivery adapter (hand-maintained) | `assets/resource/pipeline/SeizeDeliveryJobs/AutoDeliveryAdapter.json` | Calls the shared `AutoDelivery` through continuation anchors instead of copying its flow |
| Destination names and directions (manual) | `tools/pipeline-generate/SeizeDeliveryJobs/endpoint-labels.json` | Per-destination five-language location name (where that destination's NPC stands) plus a `direction` code (1-8); an established nickname is kept when no official entry exists |
| Go Service | `agent/go-service/seizedeliveryjobs/` | Chained job-card OCR, reward parsing, dynamic click boxes, and per-scan session state |
| Shared delivery catalog | `tools/pipeline-generate/data/delivery_destinations.json` | zmdmap depot/destination data, map coordinates, areas, and source-language names read by `AutoDelivery` |

See the [`tools/pipeline-generate` overview](../../../../tools/pipeline-generate/README.md) for zmdmap data updates and the [AutoDelivery component documentation (Chinese)](../../../zh_cn/developers/components/auto-delivery.md) for route-generation rules.

## Runtime Flow

### Task Entry and Source Filtering

`SeizeDeliveryJobsMain` handles the risk-acknowledgement gate, then enters the depot management page for the selected map. Once the job list is open, `SeizeDeliveryJobsReadyToSeize` checks for an existing job and an exhausted daily quota, then applies the selected region filter.

The current source-to-map mapping is (in task-option order):

| Task option ID | Commission source | Depot entry map | Job-list filter | Destination options |
| ------------------------ | ----------------------------------------- | -------------------------- | ---------------------------- | ----------------------------------------------- |
| `AllUnlimited` | All areas in the catalog | `Wuling` | All | All destinations in the catalog |
| `Unlimited` | Wuling City + Test Area | `Wuling` | Wuling | Wuling City and Test Area destinations; legacy option name |
| `WulingCity` | Wuling City | `Wuling` | Wuling | Wuling City destinations |
| `TestArea` | Test Area | `Wuling` | Wuling | Test Area destinations |
| `ValleyIVUnlimited` | All three Valley IV areas | `ValleyIV` | Valley IV | All three Valley IV destination groups |
| `OriginiumSciencePark` | Originium Science Park | `ValleyIV` | Valley IV | Originium Science Park destinations |
| `OriginLodespring` | Origin Lodespring | `ValleyIV` | Valley IV | Origin Lodespring destinations |
| `PowerPlateau` | Power Plateau | `ValleyIV` | Valley IV | Power Plateau destinations |

`Unlimited` is a compatibility option case and must not be renamed or removed: saved user configurations may still reference it. `AllUnlimited` is the aggregate case for every area. A source case's override replaces `__SeizeDeliveryJobsRecoOrigin`, the depot entry, and the region-filter nodes together, so adding an area requires more than locale text.

`task-data.mjs` builds the aggregate destination sets from the map-filtered `candidatesRows`: `AllUnlimited` covers every area in the catalog, `Unlimited` covers every `map02` (Wuling) area, and `ValleyIVUnlimited` covers every `map01` (Valley IV) area. A new area joins `AllUnlimited` automatically; the other two aggregate cases change only when an area is added to the corresponding map. The in-game "all regions" filter already spans both maps, so `AllUnlimited` needs only one depot entry; `Unlimited` keeps its Wuling semantics for backward compatibility with saved user configs, and its key stays unchanged.

A source case's `pipeline_override` replaces fields rather than appending to them: `task-data.mjs` replaces `SeizeDeliveryJobsMain.next` and `SeizeDeliveryJobsReadyToSeize.next` wholesale. Both lists must therefore list every node that needs to survive — `SeizeDeliveryJobsGuard` (risk acknowledgement), `SeizeDeliveryJobsExistDueTask` (existing-job check), and `SeizeDeliveryJobsFailedChanceExhausted` (daily-quota check). Omitting any of them silently disables that check, because the override is what runs and the default list has already been replaced.

### Without Destination Filtering

When all `Specify Delivery Point` switches remain off, the task enables `SeizeDeliveryJobsFindTarget`:

```text
SeizeDeliveryJobsLoop
  └─ SeizeDeliveryJobsFindTarget
       └─ Go recognizes the whole column of dispatch-voucher boxes
            └─ For each box, chained OCR reads reward, origin, accept, and location
                 ├─ reward < threshold       → skip
                 ├─ incomplete/invalid OCR   → skip
                 └─ reward qualifies         → click the first qualifying accept button
                      ├─ accept succeeds → post-process or finish
                      └─ accept animation missing → refresh and scan again
```

`__SeizeDeliveryJobsRecoCommissionToken` is the **row anchor** of the grab flow: it uses `TemplateMatch` to locate each row's **dispatch voucher icon** (the Wuling or Valley IV dispatch voucher), and Go then derives the reward amount, origin, accept button, and view-location ROIs from that box via fixed offsets.

The node is a **single `TemplateMatch` with multiple templates**: `template` lists one voucher image per map (`WulingToken.png` = Wuling dispatch voucher, `ValleyIVToken.png` = Valley IV dispatch voucher), so one recognition returns the whole column of boxes, including the mixed multi-map rows shown by "all regions"; `order_by: Vertical` keeps them top-to-bottom.

Do not replace it with an `Or` combination of per-map nodes: `Or` returns only the first hitting child's result, so a mixed list would silently drop the other map's jobs. A new map must also ship its own voucher image; cropping requirements are in step 4 of "Adding a Map".

### With Destination Filtering

When any `Specify Delivery Point` switch is set to Yes, the task's overrides disable `SeizeDeliveryJobsFindTarget` and enable `SeizeDeliveryJobsScanTarget` plus `SeizeDeliveryJobsEndpointFilter`. The flow is:

```text
SeizeDeliveryJobsScanTarget
  └─ First Go call: cache all jobs meeting the reward threshold and their dynamic boxes
       └─ SeizeDeliveryJobsFoundTargetViewLocationClick
            └─ Open the current job's Check Location map
                 └─ SeizeDeliveryJobsEndpointFilter
                      ├─ map open → OCR the upper-left sub-area name
                      │    └─ route to that area's MapFind candidates
                      │         ├─ enabled destination hit → close map → accept
                      │         └─ no hit → close map → inspect the next cached job
                      └─ all cached jobs checked → clear state → refresh the list
```

The area-gate ROI `[16, 14, 214, 41]` reads the upper-left map-area name; it prevents an all-source run from testing the destination candidates for another map. Each area's `MapFind` node uses exactly one `zone`; the candidate set's `at` values come from the delivery catalog's map coordinates (world/map coordinates). Once the gate hits, that area's candidate node decides the destination; when nothing matches (for example the current commission goes to an unchecked destination), the trailing `SeizeDeliveryJobsEndpointNotMatched` in its `next` closes the map and scans the next commission.

The dispatcher node `SeizeDeliveryJobsEndpointFilter` uses `__SeizeDeliveryJobsRecoAnyDepotNode` (a wrapper around `InLocalDepotNode`, the feature marker of any map's depot management page) in inverse mode to confirm that the map view is open: the marker is still present while the depot page is showing, so the node does not hit. It is not tied to a specific map, so adding an area only requires appending the new region gate to the dispatcher's `next` list.

Each generated `SeizeDeliveryJobsEndpointFilter<EndpointId>` leaf is disabled by default. Task options enable only the checked leaves through Pipeline overrides. The leaves do not perform icon recognition again; a hit routes to `SeizeDeliveryJobsEndpointMatched`, so one `MapFind` call can inspect all enabled destinations in an area.

### Post-Accept Processing

`SeizeDeliveryJobsPostProcessing` controls what happens after a successful accept or when the task starts with an existing job:

| Case | Behavior |
| ---------------------- | ------------------------------------------------------------------------ |
| `Disable` | Finish after accepting; an existing job produces the one-job-only message and finishes |
| `Tele` | Call `AutoDelivery`; if pickup is needed, use quick teleport to the depot area |
| `TeleWalk` | Quick-teleport and walk to the depot; optionally prefer ziplines |
| `TeleWalkFetchDeliver` | Call full `AutoDelivery` to decide pickup/delivery and complete the job; configure risk acknowledgement, ziplines, and repeat count |

`TeleWalkFetchDeliver` sets `SeizeDeliveryJobsMain.max_hit` to 1, 2, or 3 through the repeat-count option. Risk acknowledgement defaults to No, so `SeizeDeliveryJobsGuard` blocks the automatic walking flow; only an explicit Yes lets it through. `AutoDeliveryAdapter.json` only supplies entry points and continuation anchors. Shared `AutoDelivery` owns navigation, routes, and job completion.

## Data and Generation

### Input Data Flow

Both the area depot nodes and the destination nodes use the same catalog model as `AutoDelivery`:

```text
zmdmap
  └─ tools/pipeline-generate/data/delivery_destinations.json
       └─ AutoDelivery/model.mjs
            ├─ destinations (map, area, MapFind zone, destination u/v coordinates, recipient names)
            │    └─ endpoint-filter-data.mjs → destination rows, area candidate rows, and dispatcher rows
            │         ├─ endpoint-labels.mjs → validates the location names and directions in endpoint-labels.json
            │         ├─ endpoint-candidates-data.mjs / endpoint-dispatcher-data.mjs → re-export those rows
            │         └─ task-data.mjs → task cases and destination switches, then calls sync-locales.mjs for locale text
            └─ depots (depot and its owning map)
                 └─ commission-data.mjs → per-map depot entry, depot confirmation, and region filter rows
```

`delivery_destinations.json` is compact zmdmap data and must not be edited manually. `pnpm fetch:zmdmap` updates it along with the other task data files. `AutoDelivery/model.mjs` also reads `tools/pipeline-generate/AutoDelivery/routes.json` to validate route overrides. A new destination that should work with full automatic delivery must therefore be generated through `AutoDelivery` as well. The generator only sees depots listed in `depots`, so a map without a depot entry never appears in `SeizeDeliveryJobsCommission.json`.

### Generator Configurations and Outputs

`tools/pipeline-generate/run-all.mjs SeizeDeliveryJobs` scans all `*config.json` files in filename order and produces:

| Config | Template | Output | Hand-edit? |
| ----------------------------- | ------------------------------------ | ------------------------------------------------------------ | --------- |
| `commission-config.json` | `commission-template.jsonc` | `SeizeDeliveryJobsCommission.json` | No |
| `endpoint-candidates-config.json` | `endpoint-candidates-template.jsonc` | `SeizeDeliveryJobsEndpointCandidates.json` | No |
| `endpoint-dispatcher-config.json` | `endpoint-dispatcher-template.jsonc` | `SeizeDeliveryJobsEndpointDispatcher.json` | No |
| `endpoint-filter-config.json` | `endpoint-filter-template.json` | `SeizeDeliveryJobsDestinations.json` | No |
| `task-config.json` | `task-template.jsonc` | `assets/tasks/SeizeDeliveryJobs.json` | No |

During task rendering, `task-data.mjs` calls `syncSeizeDeliveryJobsLocales()`, which writes area labels, destination labels, and match messages to the five `assets/locales/interface/*.json` files. Area labels only fill missing values; destination labels and match messages are rewritten on every run from `endpoint-labels.json` (location name or source recipient name, plus the direction).

Before rendering a task or `merged` output, `run-all.mjs` removes the old target file. This prevents deleted areas, destinations, or cases from remaining in generated output. Make sure there is no unsaved manual content in those files before regenerating.

### Commands

Run these from the repository root:

```bash
# Update zmdmap data and regenerate all SeizeDeliveryJobs artifacts
pnpm generate:SeizeDeliveryJobs

# Render from existing local data without accessing zmdmap
node tools/pipeline-generate/run-all.mjs SeizeDeliveryJobs
```

If the change adds a depot or destination and `TeleWalkFetchDeliver` must support it, generate shared auto-delivery resources first:

```bash
pnpm generate:AutoDelivery
pnpm generate:SeizeDeliveryJobs
```

`generate:SeizeDeliveryJobs` does not generate `assets/resource/pipeline/AutoDelivery/` or `assets/data/AutoDelivery/catalog.json`. Changing a direction, or renaming a destination in the source data, only requires SeizeDeliveryJobs regeneration, not AutoDelivery regeneration.

## Maintaining Destination Names and Directions

`endpoint-labels.json` is the only manual entry point for destination display names. What is registered is the **name of the location where that destination's NPC stands** (a recipient name is often just a person, while a location name is easier to find on the map); an unregistered destination uses the source recipient name. Display names are resolved in this order:

1. a destination with a registered location name uses those five strings ;
2. every other destination uses the recipient name from `delivery_destinations.json`;
3. the direction suffix for that `direction` code is appended last.

### `endpoint-labels.json` Rules

Keys are the raw destination IDs from `delivery_destinations.json`. The generator keeps one entry per destination and fills in the five locale fields plus `direction`: registered destinations carry the location name, unregistered ones stay empty (that locale falls back to the recipient name), and a hint means changing `direction` from `0` to a code:

```jsonc
{
    // 武陵
    // 武陵城
    // 苏白易
    "deliver_target_map02_lv002_01": {
        "zh_cn": "技术生产办公室",
        "zh_tw": "技術生產辦公室",
        "en_us": "Technological Production Office",
        "ja_jp": "技術生産室",
        "ko_kr": "기술 생산 사무소",
        "direction": 5
    },
    // Origin Lodespring
    // Molly
    "deliver_target_map01_lv006_03": {
        "zh_cn": "",
        "zh_tw": "",
        "en_us": "",
        "ja_jp": "",
        "ko_kr": "",
        "direction": 0
    }
}
```

The rules are:

- The five locale fields are the location's official in-game name, copied verbatim from the official i18n tables; all five locale keys must be filled together.
- When the official tables have no entry, keep the established nickname, such as Owl.
- `direction` is a **direction code** written as a single number, where `0` means no suffix:

  | Code | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
  | --- | --- | --- | --- | --- | --- | --- | --- | --- |
  | Direction | Top | Bottom | Left | Right | Top Left | Bottom Left | Top Right | Bottom Right |

  The five-language wording lives once in `DIRECTION_TEXTS` in `endpoint-labels.mjs`.
- Generation fails when both the registered text and the source recipient name are empty.
- The `// map / area / recipient` comments above each entry are rebuilt on every run, but registered text and directions are never cleared. Comments are not program input: do not edit them to change a display name.

### ID Compatibility and Ordering

Existing destinations retain their internal `EndpointId` through the `LEGACY_ENDPOINTS` table in `endpoint-filter-data.mjs`. The current compatibility IDs are `Owl`, `MaterialResearchInstitute`, `Observatory`, `TechProductionOffice`, `No1TypeCAnchorArea`, `No3TypeCAnchorArea`, and `JingweiFieldArea`. Do not rename them to match a newer display name: doing so changes saved task cases, Pipeline node names, locale keys, and `MapFind` candidate references.

Destinations not in the compatibility table derive a PascalCase ID from the raw source ID. For example:

```text
deliver_target_map01_lv006_03
  → DeliverTargetMap01Lv00603
  → SeizeDeliveryJobsEndpointFilterDeliverTargetMap01Lv00603
```

Legacy destinations keep their existing order; new destinations are appended in the source-ID order emitted by `AutoDelivery/model.mjs`. When a destination disappears from the source data, regeneration removes its generated options and nodes instead of leaving orphaned entries.

Area IDs have no compatibility table and are derived from the data: `area.en_us` with every non-alphanumeric character removed (`Origin Lodespring` → `OriginLodespring`, `Test District` → `TestDistrict`). The same ID determines the commission-source case name, the `SeizeDeliveryJobsDeliveryPoint<AreaId>` option name, and the area gate node names, so renaming an English area name in the source data renames all of them.

### Changing the Direction of an Existing Destination

1. Change `direction` for the raw destination ID in `endpoint-labels.json` (0-8; `0` means no suffix).
2. Run `pnpm generate:SeizeDeliveryJobs`, or run `node tools/pipeline-generate/run-all.mjs SeizeDeliveryJobs` when local source data is already current.
3. Check the destination cases in `assets/tasks/SeizeDeliveryJobs.json`, the `desc` in `SeizeDeliveryJobsDestinations.json`, and the corresponding option/focus messages in all five locale files.

Do not edit generated task, Pipeline, or locale files directly. The next generation must be able to reproduce the result from `endpoint-labels.json` and zmdmap data.

### Registering or Changing a Location Name

1. Use your editor's global search to look up the location's Simplified Chinese name in the official i18n tables, take the entry ID it belongs to, then search that ID for the other four languages.
2. Add the five locale fields for that destination in `endpoint-labels.json` and copy the official name verbatim into all five; keep the established nickname when the official tables have no entry.
3. Run `pnpm generate:SeizeDeliveryJobs` and confirm the destination text in all five locale files matches the official name.

## Adding Destinations, Areas, or Maps

### Adding a Destination to an Existing Area

If zmdmap already exposes the destination and it belongs to an existing area with the same `MapFind zone`:

1. Run `pnpm fetch:zmdmap` and confirm that `delivery_destinations.json` contains the new destination and its `u`/`v` coordinates.
2. Run `pnpm generate:AutoDelivery` so AutoDelivery syncs route metadata, generates navigation nodes, and updates `catalog.json`.
3. The generator adds the empty entry (five locales + `direction: 0`) for the new ID; change `direction` for a hint, fill the five fields through "Registering or Changing a Location Name" for a location name, or leave them empty to use the recipient name.
4. Run `pnpm generate:SeizeDeliveryJobs`.
5. Verify that the destination appears in that area's task checkbox, `SeizeDeliveryJobsDestinations.json`, that area's `MapFind` candidates, and all five locale files.
6. Run the node test or a real-device check to confirm that `DeliveryPoint.png` matches at the new `at` coordinate after map zooming, and that a match returns to the job list so the task can continue accepting jobs.

Destination matching uses the `DeliveryPoint` icon registered in `assets/resource/image/SceneManager/MapIcons.json`. All destinations share `assets/resource/image/SeizeDeliveryJobs/DeliveryPoint.png`; update this template and its test set only when the game's visual resource changes.

### Adding an Area

First decide which case applies:

**A. The area belongs to an existing map**

Nothing needs manual editing beyond the upstream data. If the new area also brings new destinations, run `pnpm generate:AutoDelivery` first, then `pnpm generate:SeizeDeliveryJobs` (see "Commands"). The following are generated automatically:

- the source case in the commission-source option, ordered entirely from the source data (newest map first, then source order inside a map); locale key order shares that source, so there is no manual order table;
- the area's delivery-point option, one case per destination in that area, and the specify-delivery-point toggles;
- the area gate's OCR `expected` values (complete area names) and the `MapFind` `at` coordinates;
- the `expected` values of `Wuling - All`, `Valley IV - All`, and `All Regions`, which include the new area automatically;
- the area labels in the five `assets/locales/interface/*.json` files (missing values only).

**No new images are needed**: the area gate uses OCR, destinations share `DeliveryPoint.png`, and the filter buttons are per map (`Filter${MapName}.png`). The only manual decisions left are the direction in `endpoint-labels.json` and the established nickname used when a name has no official text, as described in "Maintaining Destination Names and Directions".

**B. The area belongs to a new map**

A new map needs explicit wiring. Check the following in order:

1. Confirm that zmdmap provides the area name, depot relation, map `u`/`v` coordinates, and five-language source text.
2. Add the new `map` mapping to `mapNames`, `mapLabels`, and `depotTextNodes` in `tools/pipeline-generate/SeizeDeliveryJobs/commission-data.mjs`; each generated row exports `MapId`, `MapName`, `AreaName`, `DepotTextNode`, and `Labels`.
3. Add a region-level specify-delivery-point option block for the new map in `task-template.jsonc`, following the existing ones and naming it `<MapName>Unlimited` (for example `ValleyIVUnlimited`), then add the placeholder it references to `taskRows` in `task-data.mjs` (`<MapName>DeliveryPointOptions: deliveryPointOptionsOfMap("<MapId>")`); a placeholder nobody provides is emitted verbatim. Source cases, region-level case order, and locale key order pick up the new map from the source data automatically, and the map name is read from `commission-data.mjs`. The case label `task.SeizeDeliveryJobsCommissionSource.cases.<MapName>Unlimited.label` is **not generated** and must be added by hand to the five locale files.
4. Confirm that `Filter${MapName}.png` exists and **capture a dedicated voucher image for the new map** at `assets/resource/image/SeizeDeliveryJobs/DepotNodePage/${MapName}Token.png` (then append it to `template` as in step 5). Match the size of the two existing images (47×33, cropped from the 720p screenshot as-is), and note two rules:
   - **Align the crop's anchor with the existing templates**: Go derives the following ROIs from the voucher box via fixed offsets, so the new template's match y must land at the same row-relative position. Calibrate against the centre of the yellow line at the bottom edge of the voucher card — both existing images sit 41.5~42.5 px above it in their matched rows, and a few pixels off shifts that row's reward ROI.
   - **Keep the reward digits and that yellow line out of the crop**: including the digits drops the cross-row score from 0.997 to 0.93 (the amounts differ per row), and adding the line drops it further.
5. Wire the new map into the hand-maintained Pipeline:
   - append the new map's voucher image to `__SeizeDeliveryJobsRecoCommissionToken.template` in `SeizeDeliveryJobsCommon.json` (a shared node, not generated per map);
   - also add the new map entry/filter nodes to the default `Main.next` and `ReadyToSeize.next` in `SeizeDeliveryJobs.json` so the default lists stay complete; the lists that actually run come from the per-case overrides in `task-data.mjs`;
   - confirm that the corresponding SceneManager depot entry and `DeliveryJobsCheckLocalDepotNode${DepotTextNode}` (for example `DeliveryJobsCheckLocalDepotNodeWulingCityText`) exist; the `DepotTextNode` value already has the `Text` suffix.
6. Run `pnpm generate:AutoDelivery`, then `pnpm generate:SeizeDeliveryJobs`.
7. Inspect the generated area gate, `MapFind zone`, and candidates. If destinations in one area span multiple `MapFind` zones, the generator intentionally fails; extend the area-grouping model and template first instead of putting different zones into one candidates node.

Area-gate `expected` values should use the complete area names and match the actual upper-left map OCR. The gate routes by area name, not by destination recipient name.

### Adding or Changing Routes

SeizeDeliveryJobs destination matching only uses `delivery_destinations.json` map coordinates. Pickup and delivery routes belong to shared `AutoDelivery`:

- Store measured route overrides in `tools/pipeline-generate/AutoDelivery/routes.json`; do not put full navigation paths into SeizeDeliveryJobs `MapFind` candidates.
- Run `pnpm generate:AutoDelivery` and verify the relevant `AutoDeliveryRoute...` nodes, retry routes, and `assets/data/AutoDelivery/catalog.json`.
- Then regenerate SeizeDeliveryJobs and confirm that destination task options and map candidates still use the same source IDs.

## Go Service Maintenance

### Registered Components

`agent/go-service/seizedeliveryjobs/register.go` registers four components:

| Registered name | Type | Purpose |
| ------------------------------------------------ | -------- | ------------------------------------------------------------------- |
| `SeizeDeliveryJobsFindTargetRecognition` | Custom Recognition | Without destination filtering, find the first reward-qualified job and return its accept-button box |
| `SeizeDeliveryJobsScanTargetRecognition` | Custom Recognition | With destination filtering, perform the first scan and cache reward-qualified jobs |
| `SeizeDeliveryJobsScanTargetAction` | Custom Action | Use the current cached job's dynamic location/accept boxes and advance the index |
| `SeizeDeliveryJobsResetScanStateAction` | Custom Action | Clear the cache and index after a match or scan exhaustion |

If a component is renamed, added, or removed, update:

- `agent/go-service/seizedeliveryjobs/register.go`;
- `agent/go-service/register.go`'s `seizedeliveryjobs.Register()` call (only when adding/removing the package itself);
- `tools/schema/custom.recognition.schema.json` or `tools/schema/custom.action.schema.json`;
- every Pipeline `custom_recognition` / `custom_action` reference.

### Reward Parsing and Dynamic Boxes

Go reads the task input from `__SeizeDeliveryJobsMinReward.expected[0]` and normalizes rewards to units of ten thousand:

- `万` / `萬`: use the number directly;
- `K`: divide by 10, for example `119K = 11.9` ten-thousands;
- `M`: multiply by 100, for example `1.2M = 120` ten-thousands;
- no suffix: assume the value is already in ten-thousands.

The later OCR ROIs are derived from the preceding recognition box: dispatch voucher, reward, origin, accept, and location. Do not hard-code an absolute screen coordinate for each job card in Go. When the layout changes, first inspect the Pipeline base ROIs, OCR hit boxes, and the offset relationships.

### Scan-State Boundaries

In destination-filter mode, `scannedJobItems` and `currentIndex` are temporary per-process scan state and are not persisted:

- The first `ScanTargetRecognition` call scans and caches all reward-qualified jobs; later recognition calls reuse that cache instead of rescanning the column.
- After a destination match, `ResetScanStateAction` clears the state before the current cached job is accepted.
- On a destination miss, the map closes and the next cached item is checked; after all items are checked, the state is cleared before the list refreshes.
- A refreshed job list must be scanned again; old dynamic boxes must never be reused.

If a new branch leaves the job list or refreshes job data, connect it to an explicit reset node. Otherwise the next iteration may use stale dynamic click coordinates.

## Common Problems and Triage

| Symptom | Check first |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Generator cannot find the data file | Run `pnpm fetch:zmdmap`; do not create an incomplete local `delivery_destinations.json` as a substitute |
| Destination option is empty or generation fails | Check the raw ID, that the five location-name fields or the source recipient name are non-empty, and that `direction` is an integer in 0-8 |
| New destination has an option but `MapFind` misses | Check `mapAt`/`u`/`v`, the `MapFind zone`, the `DeliveryPoint` registration, and the post-zoom location; `at` is not a screen ROI |
| Destination filtering routes to the wrong area | Check the area-gate ROI `[16, 14, 214, 41]`, five-language `expected`, and whether all destinations of that area share one `MapFind zone` |
| Destination-filter mode never checks the next job | Check the miss path `ESC → SeizeDeliveryJobsScanTarget` and the exhaustion path `reset → refresh` |
| Unfiltered mode cannot recognize any job | Check that `__SeizeDeliveryJobsRecoCommissionToken.template` contains the current map's voucher image, then inspect the voucher ROI, reward OCR, and source `expected`; `filtered_results_=[]` with scores below `param_.thresholds` in `maafw.log` only means the threshold was not met, so also check the ROI and the captured frame |
| A newly added voucher image scores too low | Give the node a `threshold` array to set a per-template threshold (its length must match `template`, for example `[0.7, 0.6]`); if lowering it makes two images cross-match, a row yields two boxes and the Go side needs to deduplicate by y |
| Jobs are recognized but the reward is unreadable or accepting fails | The map's voucher image is most likely misaligned: the reward/origin/accept/view-location ROIs derived from its box shift as a block. Recalibrate against the yellow row line as in step 4 of "Adding a Map" |
| Auto-delivery cannot find the accepted destination | Run `pnpm generate:AutoDelivery` and inspect the corresponding `AutoDelivery/catalog.json` and route nodes; SeizeDeliveryJobs generation does not create them |
| Changing display text breaks old configurations | Check whether a `LEGACY_ENDPOINTS` internal ID was changed; both directions and display names live in `endpoint-labels.json`, so neither should change the case or node ID |

Start with `maafw.log`, `go-service.log`, and node focus messages to identify whether the failure is in list entry, job OCR, destination routing, or AutoDelivery. Fix that layer instead of masking the issue with extra retries or fixed delays.

## Pre-Submission Checks

For an existing destination-direction change, run at least:

```bash
node tools/pipeline-generate/run-all.mjs SeizeDeliveryJobs
node --test tools/pipeline-generate/SeizeDeliveryJobs/task-data.test.mjs tools/pipeline-generate/DeliveryJobs/data.test.mjs
pnpm check
pnpm test
pnpm format:md:check
git diff --check
```

`SeizeDeliveryJobs/task-data.test.mjs` covers source order, locale key order, entry structure, and display-name resolution. `DeliveryJobs/data.test.mjs` is an upstream file; its `DeliveryJobs packing item options inject IconRecognition item ids` case is a pre-existing failure unrelated to this task.

For a new destination, area, or route, also run:

```bash
pnpm generate:AutoDelivery
pnpm generate:SeizeDeliveryJobs
```

Review the generated diff and confirm it contains only the expected task, Pipeline, locale, and AutoDelivery artifacts. Do not leave direct edits in generated files or add resource directories whose names begin with `_`. When code or JSON is changed as part of the work, also run the project-standard `pnpm format`, `pnpm format:go`, `pnpm check`, and `pnpm test` commands.
