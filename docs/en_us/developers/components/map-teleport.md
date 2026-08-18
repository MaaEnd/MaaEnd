# Developer Manual - MapTeleport World Map Teleport

MapTeleport is a native C++ system for clicking teleport points on the world map. A node only has to say "which zone, and which coordinate in that zone's base map"; MapTeleport works out where that coordinate currently sits on screen, verifies that a teleport icon is really there, and clicks it.

It shares its template matching kernel and its base map images with [MapLocator](./map-locator.md). The difference is what they look at: MapLocator reads the minimap and answers "where is the character", MapTeleport reads the full-screen world map and answers "where is this coordinate drawn right now".

- [MapTeleportSelect](#mapteleportselect)
- [Two Kinds of Teleport Point](#two-kinds-of-teleport-point)
- [Adding a Teleport Point](#adding-a-teleport-point)
- [How It Works](#how-it-works)

MapTeleport is an Action. It only clicks a teleport icon on an already-open world map. Opening the map, switching layers, and setting the zoom are handled by [SceneManager](../scene-manager.md); the confirmation dialog after the click is handled by the Pipeline.

---

## MapTeleportSelect

Finds the given teleport point on the current world map and clicks it.

The node takes its own screenshots, solves the viewport, and pans the map when needed, until the target is inside the clickable area and an icon has been confirmed there. **If no icon is confirmed, nothing is clicked** — being able to compute a coordinate does not mean anything is there. It returns failure so the caller can retry or take `on_error`, rather than clicking a computed coordinate blind.

### Node Parameters

Required (`custom_action_param`):

| Parameter | Description |
| --------- | ------------------------------------------------------------------------------------- |
| `zone` | Zone name, i.e. the directory name under `assets/resource/image/MapLocator/`, e.g. `Wuling` |
| `target` | Array of 2 numbers `[x, y]`, the teleport point in that zone's base map pixel frame |

A zone's base map is one stitched image of the whole zone, and the zone's sub-regions each occupy a distinct, non-overlapping area on it. `target` is expressed in that stitched frame — so the sub-region is not a parameter, and does not need to be one. Switching the map to the sub-region that holds the point is the job of the `all_of` recognition gate on the entry chain, which this Action takes as a precondition: if the gate does not pass, the node does not run.

Optional (`custom_action_param`):

| Parameter | Default | Description |
| -------------- | ------- | ------------------------------------------------------------------------------------------ |
| `core` | `false` | This is the zone's base teleport point. Switches to the base icon templates and treats `target` as the centre of a float region rather than the icon itself |
| `max_attempts` | `4` | Recognition attempts. Panning is not charged against it |
| `gate_base` | `10.0` | Maximum offset between the match and the expected position, in base map pixels. Ordinary anchors only |

### Success and Failure

| Result | When |
| ------- | -------------------------------------------------------------------------------- |
| Success | An icon was confirmed and clicked; or the character is already standing on the point (see below) |
| Failure | The viewport could not be solved, no icon was confirmed, panning hit the map boundary while still out of reach, or the teleport point is locked |

The player's own marker is drawn on top of teleport icons, which makes the icon unrecognizable — and the reason it is covered is that the character is already standing there. In that case the node leaves the map and returns **success**, letting [MapNavigator](./map-navigator.md) cover the remaining distance on foot.

A locked teleport point returns **failure** and is not retried — this is a game rule, and retrying cannot change it. The log says "locked" rather than "not recognized", so the Pipeline side can route this case to a notification node through `on_error`.

### Examples

An ordinary teleport anchor:

```json
{
    "MyTeleportTask": {
        "action": "Custom",
        "custom_action": "MapTeleportSelect",
        "custom_action_param": {
            "zone": "ValleyIV",
            "target": [
                452.3,
                910.8
            ]
        }
    }
}
```

A base teleport point:

```json
{
    "MyBaseTeleportTask": {
        "action": "Custom",
        "custom_action": "MapTeleportSelect",
        "custom_action_param": {
            "zone": "Wuling",
            "target": [
                636.2,
                1319.2
            ],
            "core": true
        }
    }
}
```

---

## Two Kinds of Teleport Point

**Ordinary anchors** sit at a fixed position, so `target` is the icon's own coordinate. Confirmation matches the anchor template inside a small window at the expected position; if the match lands more than `gate_base` base pixels away it is treated as a mis-identification and nothing is clicked. The closest pair of teleport points in one zone is 23.5 base pixels apart, so the default 10-pixel gate leaves better than 2x margin.

**Base teleport points** in the Pipeline still go through SceneManager's traditional template matching today; this section describes `core`, a capability that is ready but not yet wired up.

The icon is drawn on the base the player placed themselves, so it floats with that placement and `target` can only bound a region rather than name a pixel. Here `target` is the centre of the float region, the node searches for the icon inside it, and the match's own position wins — which makes `gate_base` meaningless for this case. A zone has at most one base teleport point, its icon comes in exactly two styles, both templates live under `assets/resource/image/SceneManager/`, and both are tried with the higher score winning.

Base teleport points also carry an unlock test. A locked base does not accept teleports, so the node stops it after the icon is confirmed and before the click.

> [!WARNING]
>
> The unlock thresholds were calibrated on unlocked captures only. The author has no account with a locked base and could not capture one, so **the locked side has never been verified**. It does not affect the normal path for unlocked teleport points — that branch is only reached once an icon has been confirmed and its gold ratio falls below the floor. If you can test it, please do, and remove this warning along with the `TODO` in the code.

---

## Adding a Teleport Point

Teleport nodes live in `assets/resource/pipeline/SceneManager/SceneTeleport<Zone>.json` and fill the `__ScenePrivateMapTeleportPickAnchor` slot. The entry node lives in `Interface/Scene<Zone>.json`, binds that slot, and routes through `__ScenePrivateMap<SubRegion>EnterWorldAnchorWithPick`, which normalizes the map to the main layer and a non-extreme zoom.

The zoom step is not optional: at either end of the zoom slider there is too little terrain in the crop for the viewport solve to be unique.

After adding points, three things are worth re-checking: every `MapTeleportSelect` node is bound by exactly one entry node, its `all_of` recognition matches the `...EnterWorldAnchorWithPick` node it routes through, and its `target` lies inside that zone's base map.

The second one matters most: `all_of` is the only thing that pins the sub-region. Name the wrong one and the viewport still solves — both sub-regions are on the same base map — but the target lands far off-screen, and the node pans until it hits the map boundary before failing. It will not click the wrong thing; it will just burn a dozen seconds first.

---

## How It Works

This section is for readers who want the internals; day-to-day use does not need it.

There are two stages. They answer different questions, and only the second one decides where the click lands:

1. **Viewport solve — where is the camera looking?** A fixed central crop of the screen (clearing the layer list on the left, the detail panel on the right, and the top and bottom bars) is matched against the zone's base map across a scale ladder, producing the similarity transform `base = (screen - roiOrigin) * scale + baseOrigin`. This decides where to **look**, not where to click. The scan runs in two passes: a coarse geometric ladder over the whole band on downscaled images, then a fine pass at full resolution over the neighbourhood of the winning rung.
2. **Icon confirm — is an icon actually there?** `target` is reprojected to the screen through that transform, the icon template is matched inside a small window at that position, and the click uses the matched icon's centre.

Map icons are drawn at a fixed screen size and do not scale with map zoom — their best match scale is exactly 1.000, and 5% off it the score falls below 0.6. Icon templates are therefore cut at their display size for the capture resolution, and the scale ladder must land exactly on 1.000.

The framework scales every capture to one common base resolution, but the same icon is not drawn at the same size on every platform — mobile draws it about a quarter larger than desktop. Template paths are therefore resolved through the resource layers the controller has loaded, the controller's own layer ahead of the base one — the same layering the Pipeline's `TemplateMatch` uses. Each platform's template then sits near 1.000 on its own, so the scale ladder does not have to be widened for either. Zone base maps exist only in the base layer and fall through to it. The log records which file was actually read, so a wrong layer is visible at a glance.

A template's alpha channel is used as a weight mask: an icon's soft outer halo shows terrain through it in a real capture, so letting it correlate only drags the score down. The level text on the base card is masked out too, since it differs per player.

When the target falls outside the clickable area the node drags the map rather than clicking anyway. Panning has its own budget and is not charged against `max_attempts` — moving the target into view is this step's job, not a recognition failure. But each drag must genuinely shorten the remaining distance; once the map has hit its scroll boundary further drags do nothing, and the node stops instead of spinning.

The unlock test is separate. Locked and unlocked icons differ in colour, not in shape, and normalized correlation over grayscale is insensitive to overall brightness — which is exactly why it recognizes icons so reliably, and exactly why it cannot judge unlock state. So unlock state is read from colour instead: whichever template pixels are gold are sampled again on the capture to see whether they are still gold. This test only decides locked-or-not and never contributes to real-or-false — map terrain can reach a high gold ratio on its own, so it is not evidence that an icon is there.

> [!IMPORTANT]
>
> The icon confirm is the only basis for a click. A successful viewport solve is **not** enough — however accurate the transform, it only says "if an icon is there, it should be at this position", not that one is. Missing a teleport costs one retry; clicking the wrong thing costs a jump to another teleport point or to empty terrain. The two are not equivalent.
