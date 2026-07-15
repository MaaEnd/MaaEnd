# Developer Manual - MapTracker Advanced Reference Documentation

## Introduction

This document introduces the **advanced content** related to **MapTracker** components. It is suitable for the following readers:

- You want to invoke the MapTracker library at the code level to implement more complex functionalities;
- You are a maintainer of MapTracker and want to learn its day-to-day maintenance methods.

> [!WARNING]
>
> If you only want to invoke MapTracker's relevant nodes in the pipeline with low-code methods, you do not need to read this advanced document. Please directly read [this document](./map-tracker.md).

## Programming Node Descriptions

The following sections detail the programming nodes in MapTracker that cannot be used for low-code invocation. These nodes are only suitable for code-level invocation and should not be used in pipelines.

### Recognition: MapTrackerInfer

📍Obtains the player's current map name, position coordinates, and facing direction.

> [!TIP]
>
> MapTracker uses an integer in the range $[0, 360)$ to represent the player's **facing direction**, in degrees. 0° indicates facing north, with clockwise rotation as the increasing direction.

#### Node Parameters

Required Parameters: None

Optional Parameters:

- `map_name_regex`: A [regular expression](https://regexr.com/) used to filter map names. Only maps matching this regular expression will participate in recognition. For example:
    - `^map\\d+_lv\\d+$`: Default value. Matches all regular maps.
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: Matches all regular maps and tiered maps (Tier).
    - `^map01_lv001$`: Only matches "map01_lv001" (Valley IV - Hub Zone).
    - `^map01_lv\\d+$`: Matches all sub-regions of "map01" (Valley IV).

- `precision`: A real number in the range $(0, 1]$, default `0.5`. Controls the matching precision. Larger values are stricter about matching map features, but may slow down matching; smaller values greatly improve matching speed, but may lead to incorrect results. When the number of maps to match is small (for example, matching only one map), a larger value is recommended for more accurate results.

- `threshold`: A real number in the range $(0, 1]$, default `0.4`. Controls the confidence threshold for matching. Match results below this value will not hit the recognition.

- `allowed_modes`: Integer, default `3`. Advanced parameter that controls the allowed positioning inference modes. The value is the bitwise OR result of `INFER_MODE_FULL_SEARCH = 1` and `INFER_MODE_FAST_SEARCH = 2`. This parameter must include `INFER_MODE_FULL_SEARCH`.

### Recognition: MapTrackerBigMapInfer

🗺️ Infers the coordinates and map zoom level of the current viewport area in the big map interface.

> [!TIP]
>
> For the cropping rules of the "current viewport area", please refer to the specific code definitions.

#### Node Parameters

Please refer to the type definition of `MapTrackerBigMapInferParam` in the specific code. The parameters include `map_name_regex` and `threshold`. These parameters are also embedded in the `MapTrackerBigMapFindImageParam` of the `MapTrackerBigMapFindImage` node to control its internal big-map inference behavior.

## Algorithm Explanation

### Point Density-Deflection Trade-off Algorithm

> [!TIP]
>
> This algorithm is only used in the road network recording tool and is not used in the main Go business logic.

Given three points $p1$, $p2$, and $p3$, we want to determine whether $p3$ should be added to the path, with the following requirements:

- If the distance $d$ between $p3$ and $p2$ is too small, we tend not to add $p3$ to avoid overly dense point distribution;
- If the directional angle $\theta_1$ from $p2$ to $p3$ and the directional angle $\theta_0$ from $p1$ to $p2$ deviate too much, we tend to add $p3$ to avoid losing deflection information.

To solve this "point density-deflection" trade-off problem, a simple heuristic is to consider the trigonometric characteristics between them.

If the difference between $\theta_1$ and $\theta_0$ is $\Delta\theta$, then the function $f(d, \Delta\theta) = (d + 1) \cdot |\sin\Delta\theta|$ has the property that "when $d$ is large and $\Delta\theta$ is large, $f(d, \Delta\theta)$ is large," which meets our needs.

A threshold $k$ can be set. When $f(d, \Delta\theta) < k$, we consider that $p3$ should not be added to the path; otherwise, it should be added.

## Other Settings

### Zipline Related Constants

`MapTrackerGoal` will parse `zipline_policy` into an internal zipline strategy. The weight coefficients for the three types of runtime edges are as follows (distance multipliers):

| Strategy     | Zipline Enabled | Approaching Zipline Point | Leaving Zipline Point | Between Zipline Points |
| ------------ | --------------- | ------------------------: | --------------------: | ---------------------: |
| `Never`      | No              |                      `64` |                  `16` |                  `2.0` |
| `Lazy`       | Yes             |                      `64` |                  `16` |                  `2.0` |
| `Active`     | Yes             |                       `8` |                   `4` |                  `0.5` |
| `Aggressive` | Yes             |                       `1` |                   `1` |                 `0.25` |

## Testing Methods

### Unit Tests

Some MapTracker components, as well as the core library [minicv](/agent/go-service/pkg/minicv/) that MapTracker depends on, have unit tests. You can run them with Go's testing command.

### Integration Tests

Integration tests mainly verify whether MapTracker's recognition results meet expectations by creating an offline MaaFW controller and feeding it fixed image files.

You can run the following script to perform batch testing:

```bash
python tools/map_tracker/map_tracker_tester.py batch_test -i tests/MaaEndTestset/Win32/Official_CN/map_tracker
```

> [!NOTE]
>
> Before running this test script, you need to install Python and the `opencv-python` and `maafw` libraries, and you must have already configured the development environment for this project.

> [!TIP]
>
> As you can see, the test set is located in the `Win32/Official_CN/map_tracker` directory of the Git Submodule `tests/MaaEndTestset`. Make sure this Submodule has been pulled correctly to your local machine.

If you need to collect new test sample images, you can run the following script to record them live from the game:

```bash
python tools/map_tracker/map_tracker_tester.py collect_data -o your_output_dir
```

## Maintenance Methods

Daily maintenance of MapTracker mainly involves **updating map images**. When the game releases a new version, the latest maps need to be synchronized into MapTracker's map image library.

Currently, the source of map data and map images is zmdmap. You can easily complete the update of map images by running the **map fetching and generation script**.

### Operation Steps

> [!NOTE]
>
> Before running the following scripts, you need to install Python and the `opencv-python` and `PyMaxflow` dependency libraries.

The complete operation steps for this tool script are as follows:

1. Pull the latest map data from zmdmap into the production data directory:

    ```bash
    python tools/map_tracker/map_fetcher.py json -o assets/data/ZmdMap
    ```

2. Pull the latest original images of the Region maps from zmdmap (and cut them into several Level map images), while also pulling the latest original images of the Tier maps. These source images are not runtime resources and are temporarily stored in `.cache/map_tracker/images`:

    ```bash
    python tools/map_tracker/map_fetcher.py image -i assets/data/ZmdMap -o .cache/map_tracker/images
    ```

3. Redistribute the overlapping areas of all Level map images:

    ```bash
    python tools/map_tracker/map_generator.py distinguish_levels -i .cache/map_tracker/images -o .cache/map_tracker/images_staged --data-dir assets/data/ZmdMap
    ```

4. Attach fixed icons to all non-Tier map images and deploy them to the production directory:

    ```bash
    python tools/map_tracker/map_generator.py attach_icons -i .cache/map_tracker/images_staged -o assets/resource/image/MapTracker/map/ --data-dir assets/data/ZmdMap
    ```

5. Perform canvas expansion and background overlay for all Tier map images and deploy them to the production directory:

    ```bash
    python tools/map_tracker/map_generator.py tidy_tiers -i .cache/map_tracker/images -o assets/resource/image/MapTracker/map/
    ```

6. Generate BBox data from the final map images in the production resource directory:

    ```bash
    python tools/map_tracker/map_generator.py bbox -i assets/resource/image/MapTracker/map -o assets/data/MapTracker
    ```

The production resources used by MapTracker are the map images in `assets/resource/image/MapTracker/map` and `assets/data/MapTracker/map_bbox_data.json`. `assets/data/ZmdMap` stores the zmdmap data used by the map generation tools; `.cache/map_tracker` only contains temporary download and generation artifacts and should not be committed as production resources.

### Term Definitions

- Region map: Refers to a large map of an area in the game (a map formed by merging multiple Levels);

- Level map: Refers to a sub-region map of an area in the game;

- Tier map: Refers to a layered map in the game;

- Overlapping area redistribution: To ensure that the same location does not appear in two Level maps simultaneously, an algorithm based on max-flow min-cut is used to assign the overlapping areas of multiple Levels to the appropriate Level.

- Icon attachment: Overlays fixed icons on all non-Tier maps based on map entity data to add distinctive map features.

- Canvas expansion: For easier coordinate calculation, the canvas of the Tier map is expanded to the same size as the corresponding Level map;

- Background overlay: Because Tier maps in the game are displayed over the corresponding Level maps, when generating Tier maps, the image content of the corresponding Level map is also overlaid onto the Tier map as a background to improve recognition accuracy;

- BBox data: Records the bounding-box coordinate data of each map image, used to reduce computation during matching.

### Alternative Solutions

If zmdmap stops providing services due to force majeure, the map images can still be updated as long as the following data is available:

1. Map data: The names and geometric coordinate data of all Regions and Levels.

2. Unpacked images of Region maps: The game actually uses a 600\*600 tile grid to store map images (original size). You may need to stitch these images together to obtain a complete Region map image.

    > [!TIP]
    >
    > In 720P PC games, the minimap scaling ratio is 0.1625 times the original map size.

3. Unpacked images of Tier maps and Tier attribution information.
