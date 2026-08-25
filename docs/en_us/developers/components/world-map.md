# Developer Manual - WorldMap

WorldMap is a native C++ coordinate recognition system for the open world map. A node only states "which zone, which coordinate on that zone's base map"; WorldMap works out where that coordinate currently sits on screen, pans the map over if it has to, confirms that the named icon really is there, and hands the screen position back to the node. **What happens with that position is the node's own `action`** — a teleport point gets clicked, another use may only want the coordinate.

It shares its template matching core and its base map images with [MapLocator](./map-locator.md). The difference: MapLocator reads the minimap and answers "where is the character", WorldMap reads the full screen map and answers "where is this coordinate drawn right now".

- [MapFind](#mapfind)
- [Icon table](#icon-table)
- [Wiring up teleport points](#wiring-up-teleport-points)
- [How recognition works](#how-recognition-works)

WorldMap is a recognition layer. It only resolves a coordinate into a screen position on an already open map. Opening the map, switching layers and setting the zoom are the job of [SceneManager](../scene-manager.md); the confirmation dialog afterwards is handled by the Pipeline.

---

## MapFind

Find the given coordinate on the current map, and confirm the icon sitting there if an icon name was given.

The node captures the screen itself, solves the viewport, and pans the map when needed, until the target is inside the usable area. **Once `icon` is given, no confirmation means no coordinate** — being able to compute a position does not mean anything is there. It would rather fail and let the caller retry or take another candidate than hand back a computed empty spot. Without an `icon` it just solves the coordinate and hands it back, making no such promise.

### Node parameters

Required (`custom_recognition_param`):

| Parameter | Description |
| --------- | -------------------------------------------------------------------------------------- |
| `zone` | Zone name, i.e. the directory name under `assets/resource/image/MapLocator/`, e.g. `Wuling` |
| `at` | Two numbers `[x, y]`, the position in that zone's base map pixel frame |

The base map is one large image of the whole zone, with each sub-area occupying its own non-overlapping patch of it. `at` uses that image's frame — so the sub-area is not a parameter, and does not need to be one.

Optional (`custom_recognition_param`):

| Parameter | Default | Description |
| -------------- | ------------ | ----------------------------------------------------------------------------------- |
| `icon` | none | An icon name from the [icon table](#icon-table). Omit it to only solve the coordinate, confirming no icon at all |
| `state` | `"unlocked"` | Which unlock state counts as a hit. Requires an icon carrying `gold_ratio` in the table, since nothing else tells the two states apart |
| `max_attempts` | `4` | Recognition attempts. Panning the map is not charged against it |

Thresholds, templates and calibrated scales never appear in the node: they belong to the icon and live in the icon table. A new kind of icon is one table entry, not a fistful of node parameters.

### Success and failure

| Result | When |
| ------ | ------------------------------------------------------------------------------------ |
| Hit | The icon is confirmed and `box` is where it sits; or the player marker covers it (below) |
| Miss | The viewport cannot be solved, the icon cannot be confirmed, the map hits its edge while still out of reach, or the unlock state does not match |

The player's own marker is drawn on top of the icon, which is what stops the icon from being recognised — and the reason it is covered is precisely that the character is already standing there. In that case the node reports a **hit** at the expected position, because a marker landing there is itself evidence that the viewport was solved correctly. This branch only applies to icons flagged `occluded_by_player` in the table.

A mismatched unlock state **fails immediately and does not retry** — that is a rule, not a recognition failure, and retrying changes nothing.

### Examples

Confirm the teleport anchor at a coordinate and click it:

```json
{
    "MyTeleportTask": {
        "recognition": "Custom",
        "custom_recognition": "MapFind",
        "custom_recognition_param": {
            "zone": "ValleyIV",
            "icon": "TeleportAnchor",
            "at": [
                452.3,
                910.8
            ]
        },
        "action": "Click"
    }
}
```

Take another branch when the point at that coordinate is still locked:

```json
{
    "MyTeleportLocked": {
        "recognition": "Custom",
        "custom_recognition": "MapFind",
        "custom_recognition_param": {
            "zone": "Wuling",
            "icon": "Core",
            "state": "locked",
            "at": [
                636.2,
                1319.2
            ]
        },
        "action": "DoNothing",
        "next": [
            "SceneAnyEnterWorld"
        ]
    }
}
```

---

## Icon table

The icon table is `assets/resource/image/SceneManager/MapIcons.json`, one entry per icon name:

| Field | Default | Description |
| -------------------- | -------------- | ---------------------------------------------------------------------------- |
| `templates` | required | Template file names, relative to `assets/resource/image/SceneManager/`. With several, the highest scoring one wins |
| `scale` | `[0.90, 1.35]` | Bounds of the scale sweep |
| `scale_step` | `0.025` | Step of the scale ladder |
| `threshold` | `0.55` | Match score floor |
| `gate` | `10.0` | Maximum offset between the match and the expected position, in base map pixels. Ignored when `radius` is set |
| `radius` | `0` | Above zero, the icon floats within this radius (base map pixels) and is searched across that range instead of confirmed in a small fixed window |
| `gold_ratio` | `0` | Gold pixel fraction required to read as unlocked. Zero skips the check entirely, which also leaves `state` with nothing to judge |
| `occluded_by_player` | `false` | This kind of icon gets covered by the player marker, so the marker fallback is allowed when it cannot be recognised |

The table sits beside the templates it names and resolves through the same resource layers: a client whose icons are drawn differently ships its own templates and its own thresholds in its own layer.

Two entries exist today:

**`TeleportAnchor`**, the ordinary teleport anchor. Its position is fixed, `at` is the icon itself, and a match offset beyond `gate` is treated as the wrong icon. The two closest teleport points in a zone are 23.5 base map pixels apart, so a 10 pixel gate leaves twice the margin needed.

**`Core`**, the base teleport point. Its icon is drawn on a structure the player placed themselves, so it floats with that placement: `at` bounds the range but not the point, which is why it carries a `radius` and is searched across that range. Its templates are much larger than the anchor's, which lifts the background peak on plain map texture to 0.654, so its threshold had to be calibrated separately at 0.82. A zone holds at most one base teleport point, its icon comes in two styles, and both are tried.

> [!WARNING]
>
> The unlock threshold for `Core` was only ever calibrated against unlocked captures. The author has no locked account and could not capture one, so **the locked side has never been verified**. It does not affect the normal flow for unlocked points — that branch is only reached once an icon is confirmed and its gold ratio falls below the gate. `TeleportAnchor` carries no `gold_ratio` at all, so asking for `state` on it raises an error rather than returning an uncalibrated verdict.

---

## Wiring up teleport points

Teleport nodes live in `assets/resource/pipeline/SceneManager/SceneTeleport<Zone>.json` and fill the `__ScenePrivateMapTeleportPickAnchor` slot; entry nodes live in `Interface/Scene<Zone>.json`, bind that slot and route through `__ScenePrivateMap<SubArea>EnterWorldAnchorWithPick`, which switches the map to its main layer, sets a non-extreme zoom, and uses `all_of` to confirm that this really is that sub-area's map screen.

The zoom step cannot be skipped: pushed to either end of its range, the patch of screen fed to the viewport solver holds too little terrain to resolve a unique viewport.

The sub-area check lives on `...EnterWorldAnchorWithPick`; the `MapFind` node no longer repeats it — the `recognition` slot went to `MapFind`, and solving the viewport is itself the stronger test of "are we on this map at all".

After adding points, two things are worth re-checking: every `MapFind` node is bound by exactly one entry node, and its `at` lies inside that zone's base map.

Binding the wrong entry matters most, because the entry decides which sub-area the map gets switched to. Get it wrong and the viewport still solves (both sub-areas are on the same base map); the target simply lands far off screen, and the node pans all the way to the map edge before failing — nothing is misclicked, but a dozen seconds are burnt.

---

## How recognition works

This section is for readers who need the internals; day-to-day use does not require it.

Recognition runs in two stages that answer different questions, and only the second one decides where the coordinate lands:

1. **Viewport solve — where is the camera pointed**: take a fixed patch of the screen centre (clear of the layer list on the left, the detail panel on the right, and the top and bottom bars) and template match it against the zone's base map across scales, solving the similarity transform `base = (screen - roiOrigin) * scale + baseOrigin`. This decides **where we are looking**, not which coordinate to hand back. The scale sweep runs coarse then fine: the coarse pass walks a geometric ladder across the whole band on a downscaled image, the fine pass returns to full resolution and only sweeps the neighbourhood of the winning coarse rung.
2. **Icon confirmation — is anything actually there**: project `at` through that transform onto the screen, match the icon templates in a small window at that position, and hand back the centre of what matched.

Map icons are drawn at a fixed screen size and do not scale with the map; 5% off the best scale already drops the score below 0.6. Icon templates therefore ship at their displayed size under the capture resolution, and the table's scale ladder has to land squarely on it.

The framework normalises every capture to one base resolution, but the same icon is not drawn at the same size on every client — mobile runs about a quarter larger than desktop. Template paths are therefore resolved through the resource layers the controller loaded, the controller's own layer ahead of the base layer, the same hierarchy `TemplateMatch` uses in the Pipeline. Base maps exist only in the base layer and land there naturally. The log records which file was actually read, so a wrong layer is visible at a glance.

The template's alpha channel doubles as a weight mask: the soft glow around an icon shows the terrain through it in a real capture, and letting it take part in the correlation only drags the score down; the level text on a base card is masked out too, since it differs per player.

When the target falls outside the usable area the node pans the map instead of answering anyway. Panning is accounted separately and is not charged against `max_attempts` — bringing the target into view is this step's own job, not a recognition failure. But every pan has to genuinely shorten the remaining distance; otherwise the map is already against its edge and will not move, and the node stops there rather than spinning.

The unlock check runs on its own track. Locked and unlocked icons differ only in colour and are identical in shape, and normalised correlation on greyscale is insensitive to overall brightness — which is exactly why it recognises icons so reliably, and exactly why it cannot tell the two apart. Unlock is therefore judged on colour instead: whichever pixels are gold in the template are measured for saturation at the same pixels of the live capture. This only judges unlock and never judges authenticity — map texture alone can reach a high gold ratio, so it is no evidence that an icon is there.

> [!IMPORTANT]
>
> Once `icon` is given, icon confirmation is the sole basis for acting. A solved viewport is **not** enough to click on — however accurate the transform, it only says "if an icon is there, it should be at this position", not that one is. Missing an icon costs a retry; hitting the wrong one costs a click on another teleport point or on bare terrain, and the two are not equivalent. Without `icon` the node hands back a coordinate and makes no promise about it; wiring that to `Click` is clicking blind, so know what you are doing.
