# Developer Manual - MapTracker Advanced Reference Document

## Introduction

This document introduces **advanced content** related to **MapTracker** components. It is suitable for the following types of readers:

- You want to call the MapTracker library at the code level to implement more complex functions;
- You are a maintainer of MapTracker and wish to learn about its daily maintenance methods.

> [!WARNING]
>
> If you only wish to call the relevant nodes of MapTracker in a low-code manner within a pipeline, you do not need to read this advanced document. Please directly read [this document](./map-tracker.md).

## Programming Nodes Explanation

The following will detail the programming nodes in MapTracker that cannot be used for low-code calls. These nodes are only suitable for code-level calls and should not be used in pipelines.

### Recognition: MapTrackerInfer

📍 Obtain the player's current map name, position coordinates, and orientation.

> [!TIP]
>
> MapTracker uses an integer between $[0, 360)$ to represent the player's **orientation**, in degrees. 0° indicates facing north, with clockwise rotation as the increasing direction.

#### Node Parameters

Required parameters: None

Optional parameters:

- `map_name_regex`: A [regular expression](https://regexr.com/) used to filter map names. Only maps matching this regular expression will participate in recognition. For example:
    - `^map\\d+_lv\\d+$`: Default value. Matches all regular maps.
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: Matches all regular maps and tier maps (Tier).
    - `^map01_lv001$`: Matches only "map01_lv001" (Valley 4 - Hub Area).
    - `^map01_lv\\d+$`: Matches all sub-areas of "map01" (Valley 4).

- `precision`: A real number between $(0, 1]$, default `0.5`. Controls the matching precision. Larger values will match map features more strictly, but may lead to slower matching; smaller values will greatly improve matching speed, but may result in incorrect results. When the number of maps to match is small (e.g., matching only one map), it is recommended to use a larger value for more accurate results.

- `threshold`: A real number between $(0, 1]$, default `0.4`. Controls the confidence threshold for matching. Matching results below this value will not be recognized.

- `allowed_modes`: Integer, default `3`. Advanced parameter that controls which location inference modes are allowed. The value is a bitwise OR of `INFER_MODE_FULL_SEARCH = 1` and `INFER_MODE_FAST_SEARCH = 2`. This parameter must include `INFER_MODE_FULL_SEARCH`.

### Recognition: MapTrackerBigMapInfer

🗺️ Infer the coordinates and map zoom of the current view area in the map within the large map interface.

> [!TIP]
>
> For the cropping rules of the 'current view area', please refer to the definition in the specific code.

#### Node Parameters

Please refer to the type definition of `MapTrackerBigMapInferParam` in the specific code.

## Maintenance Methods

The daily maintenance of MapTracker mainly involves **updating map images**. When a new version of the game is released, the latest maps need to be synchronized to MapTracker's map image library.

Currently, the source of map data and map images is zmdmap. You can easily complete the map image update by running the **map fetching and generation script**.

### Operation Steps

> [!TIP]
>
> Running the script requires installing Python and the `opencv-python`, `PyMaxflow` dependency libraries.
>
> ```bash
> pip install opencv-python PyMaxflow
> ```

The complete operation steps of this tool script are as follows:

1. Fetch the latest map data from zmdmap:

    ```bash
    python tools/map_tracker/map_fetcher.py json -o tools/map_tracker/data
    ```

2. Fetch the latest original images of Region maps (and cut them into several Level map images), while also fetching the latest original images of Tier maps:

    ```bash
    python tools/map_tracker/map_fetcher.py image -i tools/map_tracker/data -o tools/map_tracker/images
    ```

3. Perform overlap area redistribution for all Level map images:

    ```bash
    python tools/map_tracker/map_generator.py distinguish_levels -i tools/map_tracker/images -o tools/map_tracker/final --layout-dir tools/map_tracker/data
    ```

4. Perform canvas extension and background overlay for all Tier map images:

    ```bash
    python tools/map_tracker/map_generator.py tidy_tiers -i tools/map_tracker/images -o tools/map_tracker/final
    ```

5. Generate BBox data for the final map images:

    ```bash
    python tools/map_tracker/map_generator.py bbox -i tools/map_tracker/final -o tools/map_tracker/final
    ```

6. The images and BBox data in the resulting `tools/map_tracker/final` directory are the latest map image library.

### Nomenclature

- Region map: Refers to the large map of an area in the game (a map formed by merging multiple Levels);

- Level map: Refers to the sub-area map of an area in the game;

- Tier map: Refers to the tiered map in the game;

- Overlap area redistribution: To ensure that the same location does not appear in two Level maps simultaneously, an algorithm based on max-flow cut is used to assign the overlapping areas of multiple Levels to the appropriate Levels.

- Canvas extension: For ease of coordinate calculation, the canvas of the Tier map is extended to the same size as the corresponding Level map.

- Background overlay: Since Tier maps in the game are displayed overlaid on the corresponding Level maps, when generating Tier maps, the image content of the corresponding Level map is also overlaid onto the Tier map as a background to improve recognition accuracy.

- BBox data: Records the bounding box coordinate data of each map image, used to reduce computational load during matching.

### Alternative Solutions

If zmdmap stops providing services due to force majeure, as long as the following data is available, the map image update can be achieved:

1. Map data: Names and geometric coordinate data of all Regions and Levels.

2. Unpacked images of Region maps: In fact, the game uses a 600\*600 grid to store map images (original size), and it may be necessary to stitch these images yourself to obtain the complete Region map images.

    > [!TIP]
    >
    > In 720P PC games, the scaling factor of the mini-map is 0.1625 times the original map size.

3. Unpacked images of Tier maps and Tier attribution information.
