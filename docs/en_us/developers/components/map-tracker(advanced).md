# Developer Manual - MapTracker Advanced Reference Documentation

## Introduction

This document introduces the **advanced content** related to **MapTracker** components. It is suitable for the following types of readers:

- You want to invoke the MapTracker library at the code level to implement more complex functionalities;
- You are a maintainer of MapTracker and wish to learn about its daily maintenance methods.

> [!WARNING]
>
> If you only wish to invoke MapTracker's relevant nodes with low code in the pipeline, you do not need to read this advanced document. Please directly read [this document](./map-tracker.md).

## Programming Node Descriptions

Below, we will detail the programming nodes in MapTracker that cannot be used for low-code invocation. These nodes are only suitable for code-level invocation and should not be used in pipelines.

### Recognition: MapTrackerInfer

📍Obtains the player's current map name, position coordinates, and facing direction.

> [!TIP]
>
> MapTracker uses an integer in the range $[0, 360)$ to represent the player's **facing direction**, in degrees. 0° indicates facing due north, with the direction increasing clockwise.

#### Node Parameters

Required Parameters: None

Optional Parameters:

- `map_name_regex`: A [regular expression](https://regexr.com/) used to filter map names. Only maps matching this regular expression will participate in recognition. For example:
    - `^map\\d+_lv\\d+$`: Default value. Matches all standard maps.
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: Matches all standard maps and tiered maps (Tier).
    - `^map01_lv001$`: Only matches "map01_lv001" (Valley No. 4 - Hub Area).
    - `^map01_lv\\d+$`: Matches all sub-regions of "map01" (Valley No. 4).

- `precision`: A real number in the range $(0, 1]$, default `0.5`. Controls the matching precision. A higher value will match map features more strictly, but may result in slower matching speed; a lower value will greatly improve matching speed, but may lead to incorrect results. When the number of maps to match is small (e.g., matching only one map), a higher value is recommended for more accurate results.

- `threshold`: A real number in the range $(0, 1]$, default `0.4`. Controls the confidence threshold for matching. Match results below this value will not hit the recognition.

- `allowed_modes`: Integer, default `3`. Advanced parameter, controls the allowed positioning inference modes. The value is the bitwise OR result of `INFER_MODE_FULL_SEARCH = 1` and `INFER_MODE_FAST_SEARCH = 2`. This parameter must include `INFER_MODE_FULL_SEARCH`.

### Recognition: MapTrackerBigMapInfer

🗺️ Infers the coordinates and map zoom level of the current viewport area in the large map interface within the map.

> [!TIP]
>
> For the cropping rules of the "current viewport area", please refer to the specific code definitions.

#### Node Parameters

Please refer to the type definition of `MapTrackerBigMapInferParam` in the specific code. Parameters include `map_name_regex` and `threshold`. These parameters are also embedded into the `MapTrackerBigMapFindImageParam` of the `MapTrackerBigMapFindImage` node to control its internal large map inference behavior.

## Algorithm Explanation

### Point Density-Deflection Trade-off Algorithm

> [!TIP]
>
> This algorithm is only used in the road network recording tool and is not used in the main Go business logic.

Given three points $p1$, $p2$, $p3$, we want to determine whether $p3$ should be added to the path, with the following requirements:

- If the distance $d$ between $p3$ and $p2$ is too close, we tend not to add $p3$ to avoid overly dense point locations;
- If the directional angle $\theta_1$ from $p2$ to $p3$ and the directional angle $\theta_0$ from $p1$ to $p2$ have a large deviation, we tend to add $p3$ to avoid losing deflection information.

To solve this "point density-deflection" trade-off problem, a simple heuristic is to consider the trigonometric characteristics between them.

If the difference between $\theta_1$ and $\theta_0$ is $\Delta\theta$, then the function $f(d, \Delta\theta) = (d + 1) \cdot |\sin\Delta\theta|$ has the property "when $d$ is large and $\Delta\theta$ is large, $f(d, \Delta\theta)$ is large," which meets our needs.

A threshold $k$ can be set. When $f(d, \Delta\theta) < k$, we consider that $p3$ should not be added to the path; otherwise, it should be added to the path.

## Other Settings

### Zipline Related Constants

`MapTrackerGoal` will parse `zipline_policy` into an internal zipline strategy. The weight coefficients for three types of runtime edges are as follows (distance multiplier):

| Strategy     | Zipline Enabled | Approaching Zipline Point | Leaving Zipline Point | Between Zipline Points |
| ------------ | --------------- | ------------------------: | --------------------: | ---------------------: |
| `Never`      | No              |                      `64` |                  `16` |                  `2.0` |
| `Lazy`       | Yes             |                      `64` |                  `16` |                  `2.0` |
| `Active`     | Yes             |                       `8` |                   `4` |                  `0.5` |
| `Aggressive` | Yes             |                       `1` |                   `1` |                 `0.25` |

## Maintenance Methods

Daily maintenance of MapTracker mainly involves the **updating of map images**. When a new version of the game is released, the latest maps need to be synchronized into MapTracker's map image library.

Currently, the source for map data and map images is zmdmap. You can easily complete the update of map images by running the **map fetching and generation script**.

### Operation Steps

> [!TIP]
>
> Running the script requires the installation of Python and the `opencv-python` and `PyMaxflow` dependency libraries.
>
> ```bash
> pip install opencv-python PyMaxflow
> ```

The complete operation steps for this tool script are as follows:

1. Pull the latest map data from zmdmap:

    ```bash
    python tools/map_tracker/map_fetcher.py json -o tools/map_tracker/data
    ```

2. Pull the latest original images of the Region maps from zmdmap (and cut them into several Level map images), and also pull the latest original images of the Tier maps:

    ```bash
    python tools/map_tracker/map_fetcher.py image -i tools/map_tracker/data -o tools/map_tracker/images
    ```

3. Redistribute the overlapping areas of all Level map images:

    ```bash
    python tools/map_tracker/map_generator.py distinguish_levels -i tools/map_tracker/images -o tools/map_tracker/final --layout-dir tools/map_tracker/data
    ```

4. Perform canvas expansion and background overlay for all Tier map images:

    ```bash
    python tools/map_tracker/map_generator.py tidy_tiers -i tools/map_tracker/images -o tools/map_tracker/final
    ```

5. Generate the BBox data for the final map images:

    ```bash
    python tools/map_tracker/map_generator.py bbox -i tools/map_tracker/final -o tools/map_tracker/final
    ```

6. The images and BBox data obtained in the `tools/map_tracker/final` directory constitute the latest map image library.

### Term Definitions

- Region map: Refers to a large map of an area in the game (a map formed by merging multiple Levels);

- Level map: Refers to a sub-region map of an area in the game;

- Tier map: Refers to a layered map in the game;

- Overlapping area redistribution: To ensure that the same location does not appear in two Level maps simultaneously, an algorithm based on max-flow min-cut is used to assign the overlapping areas of multiple Levels to the appropriate Level.

- Canvas expansion: For the convenience of coordinate calculation, the canvas of the Tier map is expanded to the same size as the corresponding Level map.

- Background overlay: Since Tier maps in the game are displayed overlaid on the corresponding Level maps, when generating Tier maps, the image content of the corresponding Level map is also overlaid onto the Tier map as a background to improve recognition accuracy.

- BBox data: Records the bounding box coordinate data of each map image, used to reduce computation during matching.

### Alternative Solutions

If zmdmap stops providing services due to force majeure reasons, as long as the following data is available, the update of map images can be achieved:

1. Map data: The names and geometric coordinate data of all Regions and Levels.

2. Unpacked images of Region maps: The game actually uses a 600\*600 grid network to store map images (original size). You may need to stitch these images together to obtain a complete Region map image.

    > [!TIP]
    >
    > In 720P PC games, the minimap scaling ratio is 0.1625 times the original map size.

3. Unpacked images of Tier maps and Tier attribution information.
