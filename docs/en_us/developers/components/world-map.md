# Developer Manual - WorldMap World Map Recognition

WorldMap is a native C++ coordinate recognition system for the open world map. A node only states "which zone, which coordinate on that zone's base map"; WorldMap works out where that coordinate currently sits on screen, pans the map over if it has to, confirms that the named icon really is there, and hands the screen position back to the node. **What happens with that position is the node's own `action`** — a teleport point gets clicked, another use may only want the coordinate.

It shares its template matching core, its local contrast normalisation and its base map images with [MapLocator](./map-locator.md). The difference: MapLocator reads the minimap and answers "where is the character", WorldMap reads the full screen map and answers "where is this coordinate drawn right now".

- [Developer Manual - WorldMap World Map Recognition](#developer-manual---worldmap-world-map-recognition)
    - [MapFind](#mapfind)
        - [Node parameters](#node-parameters)
        - [Judging a set of candidates in one call](#judging-a-set-of-candidates-in-one-call)
            - [Turning one candidate off](#turning-one-candidate-off)
        - [Success and failure](#success-and-failure)
        - [Examples](#examples)
    - [Icon table](#icon-table)
    - [Wiring up teleport points](#wiring-up-teleport-points)
    - [How recognition works](#how-recognition-works)
        - [Features and thresholds](#features-and-thresholds)
        - [Tier layers](#tier-layers)

WorldMap is a recognition layer. It only resolves a coordinate into a screen position on an already open map. Opening the map and switching layers are the job of the [SceneManager](../scene-manager.md) universal jump; the confirmation dialog afterwards is handled by the Pipeline. The zoom it sets itself, see below. A map sitting on a tier layer is solved too, see [Tier layers](#tier-layers).

---

## MapFind

Find the given coordinate on the current map, and confirm the icon sitting there if an icon name was given.

The node zooms the map all the way out first, then captures the screen itself, solves the viewport, and pans the map when needed, until the target is inside the usable area. **Once `icon` is given, no confirmation means no coordinate** — being able to compute a position does not mean anything is there. It would rather fail and let the caller retry or take another candidate than hand back a computed empty spot. Without an `icon` it just solves the coordinate and hands it back, making no such promise.

The zoom level is the unknown the viewport solve has to sweep its scale band for, so pinning it down is the first thing the node does: it runs the Pipeline's `SceneMapZoomOutWithoutReco` once; when that subtask returns, the map should already be at minimum zoom. The button sits at different coordinates on each client, so it follows the resource layers; the node itself carries no coordinates. Writing `[JumpBack]__ScenePrivateMapZoomOut` into the Pipeline is therefore optional: with it the zoom is already done, without it `MapFind` still calls `SceneMapZoomOutWithoutReco`.

### Node parameters

Required (`custom_recognition_param`):

| Parameter | Description |
| ------------------- | ------------------------------------------------------------------------------------------ |
| `zone` | Zone name, i.e. the directory name under `assets/resource/image/MapLocator/`, e.g. `Wuling` |
| `at` / `candidates` | One of the two. `at` judges one point, `candidates` judges a set, see [Judging a set of candidates in one call](#judging-a-set-of-candidates-in-one-call) |

`at` is two numbers `[x, y]`. The base map is one large image of the whole zone, with each sub-area occupying its own non-overlapping patch of it. `at` uses that image's frame — so the sub-area is not a parameter, and does not need to be one.

Optional (`custom_recognition_param`):

| Parameter | Default | Description |
| -------------- | ------------ | ----------------------------------------------------------------------------------- |
| `icon` | none | An icon name from the [icon table](#icon-table). Omit it to only solve the coordinate, confirming no icon at all |
| `state` | `"unlocked"` | Which unlock state counts as a hit. Requires an icon carrying `gold_ratio` in the table, since nothing else tells the two states apart |
| `tier` | none | The tier layer the map has already been switched to. When given, the viewport is solved on that tier only, see [Tier layers](#tier-layers) |
| `max_attempts` | `4` | Recognition attempts. Panning the map is not charged against it |

Thresholds, templates and calibrated scales never appear in the node: they belong to the icon and live in the icon table. A new kind of icon is one table entry, not a fistful of node parameters.

### Judging a set of candidates in one call

When several coordinates on the same map have to be tried one after another and only one of them will hit, write them as a set of `candidates`. Each candidate is one entry:

| Field | Description |
| ------ | ------------------------------------------------------------------------------------------- |
| `at` | Two numbers `[x, y]`, the same frame as above |
| `next` | The node the framework goes to when this candidate hits; it is also the candidate's on/off switch |

A set of candidates shares one zoom-out, and, as long as the screen has not moved, one viewport solve: they all look at the same screen, and those two steps give every one of them the same answer. What each candidate pays on its own is the windowed confirmation, plus a pan when its target is off screen; when its icon does not confirm, it recaptures the screen and re-solves the viewport before retrying, up to `max_attempts` times, and the next candidate carries on from that recaptured screen (only an unlock-state mismatch fails on the spot and leaves the screen as it was). One node per point is exactly what repeats those first two steps, and every repetition computes the same answer.

Candidates are judged in writing order, and **the first one whose icon confirms wins; the ones behind it are never looked at**. The winner hands its own `next` back to the framework, overriding whatever the node's own `next` says.

#### Turning one candidate off

When the `next` node is turned off with `enabled: false`, that candidate is **skipped without being looked at at all**. Turning one destination off from a task option therefore means turning off its `next` node — the Pipeline's own switch, with nothing extra stuffed into `custom_recognition_param`:

```json
{
    "option": {
        "DeliveryDestination": {
            "type": "select",
            "default_case": "Both",
            "cases": [
                {
                    "name": "NorthOnly",
                    "pipeline_override": {
                        "MyDeliverToSouthBin": {
                            "enabled": false
                        }
                    }
                }
            ]
        }
    }
}
```

The switch has to sit ahead of recognition, and that part matters: candidates stop at the first hit, so confirming one and then handing back a node that will not run takes every candidate behind it down as well — they never even get judged. That is exactly the case when all the icons are present and an option decides which one to go to, because the first candidate is then bound to confirm. A node whose data cannot be read counts as on.

`candidates` requires an `icon`: coordinates alone cannot tell candidates apart, so without one every candidate would confirm and the first one would always come back. They also have to be far enough apart — the confirmation gate is the icon table's `gate` (10 base map pixels by default), and two candidates closer than that can confirm each other's icon.

### Success and failure

| Result | When |
| ------ | ------------------------------------------------------------------------------------ |
| Hit | The icon is confirmed and `box` is where it sits; or the player marker covers it (below) |
| Miss | The viewport cannot be solved, the icon cannot be confirmed, the map hits its edge while still out of reach, or the unlock state does not match |

With `candidates` this table applies per candidate: any candidate hitting makes the node hit, and the node misses only when the whole set misses.

The player's own marker is drawn on top of the icon, which is what stops the icon from being recognised — and the reason it is covered is precisely that the character is already standing there. In that case the node reports a **hit** at the expected position, because a marker landing there is itself evidence that the viewport was solved correctly. This branch only applies to icons flagged `occluded_by_player` in the table, and only when the requested state is unlocked (`state` is not `"locked"`).

A mismatched unlock state makes that candidate **fail immediately without retrying** — that is a rule, not a recognition failure, and retrying changes nothing.

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

Try the Resource Recycle Stations of one sub-area in turn, and let the one that hits decide which delivery route comes next:

```json
{
    "MyPickRecycleBin": {
        "recognition": "Custom",
        "custom_recognition": "MapFind",
        "custom_recognition_param": {
            "zone": "Wuling",
            "icon": "RecycleBin",
            "candidates": [
                {
                    "at": [
                        636.2,
                        1319.2
                    ],
                    "next": "MyDeliverToNorthBin"
                },
                {
                    "at": [
                        712.0,
                        1402.5
                    ],
                    "next": "MyDeliverToSouthBin"
                }
            ]
        },
        "action": "Click"
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
| `gate` | `10.0` | Maximum offset between the match and the expected position, in base map pixels. Ignored when `radius` is above zero |
| `radius` | `0` | Above zero, the icon floats within this radius (base map pixels) and is searched across that range instead of confirmed in a small fixed window |
| `gold_ratio` | `0` | Gold pixel fraction required to read as unlocked. Zero skips the check entirely, which also leaves `state` with nothing to judge |
| `occluded_by_player` | `false` | This kind of icon gets covered by the player marker, so the marker fallback is allowed when it cannot be recognised |

The table sits beside the templates it names and resolves through the same resource layers: a client whose icons are drawn differently ships its own templates and its own thresholds in its own layer.

Four entries exist today:

**`TeleportAnchor`**, the ordinary teleport anchor. Its position is fixed, `at` is the icon itself, and a match offset beyond `gate` is treated as the wrong icon. The two closest teleport points in a zone are 23.5 base map pixels apart, so a 10 pixel gate leaves twice the margin needed.

**`Core`**, the base teleport point. Its icon is drawn on a structure the player placed themselves, so it floats with that placement: `at` bounds the range but not the point, which is why it carries a `radius` and is searched across that range. Its templates are much larger than the anchor's, which lifts the background peak on plain map texture to 0.654, so its threshold had to be calibrated separately at 0.82. A zone holds at most one base teleport point, its icon comes in two styles, and both are tried.

> [!WARNING]
>
> The unlock threshold for `Core` was only ever calibrated against unlocked captures. The author has no locked account and could not capture one, so **the locked side has never been verified**. It does not affect the normal flow for unlocked points — that branch is only reached once an icon is confirmed and its gold ratio falls below the gate. `TeleportAnchor` carries no `gold_ratio` at all, so asking for `state` on it raises an error rather than returning an uncalibrated verdict.

**`RecycleBin`**, the Resource Recycle Station. A sub-area holds several of them, a delivery job names exactly one, and the map draws **an icon on every one of them**: the one the job names is blue, the rest are white, and the shape is identical. Normalised correlation is insensitive to overall brightness, so the blue template still scores 0.65 against a white icon and clears the 0.55 threshold — on score alone both candidates confirm, which is no discrimination at all. Discrimination therefore goes to `gold_ratio`: measured 0.92 on the named one against 0.05 on the others, so a gate at 0.5 leaves over 0.4 of margin on either side.

> [!NOTE]
>
> `RecycleBin` borrows the `gold_ratio` and `state` fields, but what it judges is not lock state — it is "is this the one the current delivery job names". The mechanism is exactly the same, saturation measured over the pixels the template marks out; only the name does not fit. Nodes need not write `state`: the default already asks for the blue one, and a white one fails on the spot as a state mismatch and yields to the next candidate.
>
> This threshold was calibrated from one blue and one white icon in a single capture. The margin on both sides is wide, but that one pair is the whole sample. Blue is also not exclusive to this icon — the same map carries other blue icons, they simply sit far enough from the Recycle Station coordinates never to fall inside that small fixed window.

**`DeliveryPoint`**, the delivery destination marker. It borrows `../SeizeDeliveryJobs/DeliveryPoint.png` with its own threshold of 0.8; the Accept Commission & Delivery task uses it with `candidates` to judge every enabled delivery destination of an area in one call. It carries no `gold_ratio`, so `state` cannot be used on it.

---

## Wiring up teleport points

Regular teleport anchors are generated from the camp data by `pnpm generate:SceneTeleport`: the `MapFind` nodes go to `assets/resource/pipeline/SceneManager/SceneTeleportAnchor.json` and the entry nodes to `Interface/SceneTeleportAnchor.json`. Coordinates and descs come from the camp data; sub-areas and entry names are registered in `anchors.json` under [`tools/pipeline-generate/SceneTeleport`](../../../../tools/pipeline-generate/SceneTeleport/README.md). Other icons such as base teleports are still hand-written in `SceneManager/SceneTeleport<Zone>.json` and `Interface/Scene<Zone>.json`, wired the same way.

Teleport nodes fill the `__ScenePrivateMapTeleportPickAnchor` slot; entry nodes bind that slot and route through `__ScenePrivateMap<SubArea>EnterWorldAnchorWithPick`, which switches the map to its main layer, uses `all_of` to confirm that this really is that sub-area's map screen, and gives the zoom a head start on the way.

The `[JumpBack]__ScenePrivateMapZoomOut` in `...EnterWorldAnchorWithPick` is a head start, not a requirement: `MapFind` calls `SceneMapZoomOutWithoutReco` on its own. When wiring up a new sub-area, copy an existing `...EnterWorldAnchorWithPick`; leaving it out costs nothing in recognition.

The sub-area check lives on `...EnterWorldAnchorWithPick`; the `MapFind` node no longer repeats it — the `recognition` slot went to `MapFind`, and solving the viewport is itself the stronger test of "are we on this map at all".

After hand-writing points, two things are worth re-checking: every `MapFind` node is bound by exactly one entry node, and its `at` lies inside that zone's base map.

Binding the wrong entry matters most, because the entry decides which sub-area the map gets switched to. Get it wrong and the viewport still solves (both sub-areas are on the same base map); the target simply lands far off screen, and the node pans all the way to the map edge before failing — nothing is misclicked, but a dozen seconds are burnt.

---

## How recognition works

This section is for readers who need the internals; day-to-day use does not require it.

Recognition runs in two stages that answer different questions, and only the second one decides where the coordinate lands:

1. **Viewport solve — where is the camera pointed**: take a fixed patch of the screen centre (clear of the layer list on the left, the detail panel on the right, and the top and bottom bars) and template match it against the zone's base map across scales, solving the similarity transform `base = (screen - roiOrigin) * scale + baseOrigin`. This decides **where we are looking**, not which coordinate to hand back. The scale sweep runs coarse then fine: the coarse pass walks a geometric ladder across the whole band on a downscaled image, the fine pass returns to full resolution and only sweeps the neighbourhood of the winning coarse rung.
2. **Icon confirmation — is anything actually there**: project `at` through that transform onto the screen, match the icon templates in a small window at that position, and hand back the thickest point of the matched icon's body (the point farthest from the edge in the largest blob of the template's alpha mask), not the centre of the template box.

Map icons are drawn at a fixed screen size and do not scale with the map; 5% off the best scale already drops the score below 0.6. Icon templates therefore ship at their displayed size under the capture resolution, and the table's scale ladder has to land squarely on it.

The framework normalises every capture to one base resolution, but the same icon is not drawn at the same size on every client — mobile runs about a quarter larger than desktop. Template paths are therefore resolved through the resource layers the controller loaded, the controller's own layer ahead of the base layer, the same hierarchy `TemplateMatch` uses in the Pipeline. Base maps exist only in the base layer and land there naturally. The log records each template's name and pixel size and the icon table's actual path, and prints the whole layer chain when a file cannot be found, so a wrong layer is visible at a glance.

The template's alpha channel doubles as a weight mask: the soft glow around an icon shows the terrain through it in a real capture, and letting it take part in the correlation only drags the score down; the level text on a base card is masked out too, since it differs per player.

When the target falls outside the usable area the node pans the map instead of answering anyway. Panning is accounted separately and is not charged against `max_attempts` — bringing the target into view is this step's own job, not a recognition failure. But a pan has to actually move the map: only when an axis stays put through 3 consecutive pans that issued enough distance is the map taken to be against its edge (a single stall is usually the touch being swallowed by an icon or line under the start point, and the next pan takes another path); the node then confirms the target where it stands if it is still inside the usable area, and misses otherwise, rather than spinning.

The unlock check runs on its own track. Locked and unlocked icons differ only in colour and are identical in shape, and normalised correlation on greyscale is insensitive to overall brightness — which is exactly why it recognises icons so reliably, and exactly why it cannot tell the two apart. Unlock is therefore judged on colour instead: whichever pixels are gold in the template are measured for saturation at the same pixels of the live capture. This only judges unlock and never judges authenticity — map texture alone can reach a high gold ratio, so it is no evidence that an icon is there.

> [!IMPORTANT]
>
> Once `icon` is given, icon confirmation is the sole basis for acting. A solved viewport is **not** enough to click on — however accurate the transform, it only says "if an icon is there, it should be at this position", not that one is. Missing an icon costs a retry; hitting the wrong one costs a click on another teleport point or on bare terrain, and the two are not equivalent. Without `icon` the node hands back a coordinate and makes no promise about it; wiring that to `Click` is clicking blind, so know what you are doing.

### Features and thresholds

Both sides of the viewport solve go through local contrast normalisation before the normalised correlation, the same feature MapLocator uses on the minimap: each pixel becomes how much brighter it is than its neighbourhood of a few pixels, in units of that neighbourhood's variation. The overall brightness of the frame and its large light and dark areas are flattened out, what is compared is edges and ground texture, and dark areas count as much as bright ones. The screen is scaled to each rung first and normalised afterwards, so both sides measure their neighbourhoods in base-map pixels.

The zone base map is transparent outside the map and reads as black, while the game draws a grey backdrop there that is brighter than dark terrain. The light and dark sides of the coastline are therefore swapped against the screenshot, that edge correlates negatively once normalised, and when the outside covers a large part of the frame it is enough to sink the correct position's score. So the transparent area is filled with grey 34 when the base map is loaded, close to the backdrop in screenshots.

The thresholds are a score of 0.30 and a delta of 0.02, where delta is the solved score minus the best coarse rung more than 6% away in scale. On a set of users' world-map failure screenshots, wrong positions scored at most 0.17 and correct ones at least 0.43, and 0.30 sits between them. When the whole window cannot be solved, the node nudges the map, captures again and solves again, rather than forcing another method onto the same frame.

### Tier layers

When the map sits on a tier layer, that layer is lit, everything else is dimmed to about a third, and place names are hidden. Such a frame does not match the zone base map, so the whole base map cannot solve the viewport.

So when `at` lies inside a tier, the viewport is first solved as if the map sat on that tier: the original tier image is pasted onto the base map where it belongs, with 300 base pixels around it, giving what the world map looks like with that layer selected, and matched with the same thresholds as the base map. If the score falls short, the frame is showing the main layer, and the base map is solved as described above. When `at` lies inside several overlapping tiers they are tried one by one, and if two of their solutions lie more than 10 base pixels apart the frame is rejected. When `at` lies in no tier the base map is used directly.

The tier images are the same ones MapLocator uses (`MapLocator/<zone>/LvNNNTierMMM.png` and the like); where each sits on the base map is read from the zone table of the navigation pack `base.nav.gz`, once per zone the first time it is needed. `viewport_tier` in the recognition detail is the tier the viewport was solved on, and empty when the base map solved it.

When the caller has already switched the map to a tier, name it with `tier`: the viewport is then solved on that tier only, neither picked from `at` nor falling back to the base map. This is how to write it when several tiers are stacked on the same spot (such as `IndieDg007Tier336` to `IndieDg007Tier338` in 首墩 (Shoudun)). The names are the same as `viewport_tier`: the tier image's file name without `.png`, with `LvNNNTierMMM` written as `<zone>_L<N>_<M>`. If the zone has no such tier, the node fails outright.

With `tier` given, `at` and each candidate's `at` are in that tier image's own frame, i.e. the coordinates picked in the MapNavigator tool with that layer selected, the same convention as `target_tier` in `MapNavigateAction`; at runtime they are mapped back to the base map with the affine transform in the navigation pack. Without `tier`, coordinates stay in the zone's base map frame.

When the map is on the main layer, the content of a tier (a cave under the tree canopy, say) is not on screen, so only the base map applies then.
