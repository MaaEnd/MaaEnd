# Development Guide - MapTracker Advanced Reference Document

## Introduction

This document describes **advanced content** related to MapTracker. It is intended for readers who:

- Want to call the MapTracker library at the code level to implement more complex features.
- Are maintainers of MapTracker and want to learn the daily maintenance workflow.

> [!WARNING]
>
> If you only want to use MapTracker nodes in the pipeline with low-code, you do not need to read this advanced document. Please read [this document](./map-tracker.md) instead.

## Programming Node Descriptions

The following describes programming-only nodes in MapTracker. These nodes are designed for code-level usage and are not suitable for pipeline use.

### Recognition: MapTrackerInfer

📍Gets the player's current map name, position coordinates, and orientation.

> [!TIP]
>
> MapTracker uses an integer between $[0, 360)$ to represent the player's **orientation**, in degrees. 0° indicates facing due north, with clockwise rotation as the increasing direction.

#### Node Parameters

Required parameters: None

Optional parameters:

- `map_name_regex`: A [regular expression](https://regexr.com/) used to filter map names. Only maps matching this regular expression will participate in recognition. For example:
    - `^map\\d+_lv\\d+$`: Default value. Matches all regular maps.
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: Matches all regular maps and tiered maps (Tier).
    - `^map01_lv001$`: Only matches "map01_lv001" (Fourth Valley - Hub Area).
    - `^map01_lv\\d+$`: Matches all sub-regions of "map01" (Fourth Valley).

- `precision`: Real number between $(0, 1]$, default `0.5`. Controls the accuracy of matching. A larger value will match map features more strictly but may result in slow matching speed; a smaller value will greatly improve matching speed but may lead to incorrect results. When the number of maps to be matched is small (e.g., only one map), it is recommended to use a larger value to obtain more accurate results.

- `threshold`: Real number between $(0, 1]$, default `0.4`. Controls the confidence threshold for matching. Matching results below this value will not hit the recognition.

### Recognition: MapTrackerBigMapInfer

🗺️ Infers the map coordinate of the current viewport region on the big map and the current map scale.

> [!TIP]
>
> For the exact cropping rule of the "current viewport region", refer to the implementation details in code.

#### Node Parameters

Please refer to the `MapTrackerBigMapInferParam` type definition in code.

## Maintenance Guide

MapTracker maintenance is mainly about **updating map images**. When the game ships a new version, you need to sync the latest maps into the MapTracker image library.

Currently, the map data and map images are sourced from zmdmap. You can update the map image library by running the **map fetch and generation scripts** below.

### Steps

> [!TIP]
>
> Running these scripts requires Python and the `opencv-python` and `PyMaxflow` dependencies.
>
> ```bash
> pip install opencv-python PyMaxflow
> ```

The complete steps for using the tool scripts are as follows:

1. Pull the latest map data from zmdmap:

    ```bash
    python tools/map_tracker/map_fetcher.py json -o tools/map_tracker/data
    ```

2. Pull the latest Region map raw images from zmdmap (and slice them into Level images), and pull the latest Tier map raw images:

    ```bash
    python tools/map_tracker/map_fetcher.py image -i tools/map_tracker/data -o tools/map_tracker/images
    ```

3. Re-assign overlapping regions for all Level images:

    ```bash
    python tools/map_tracker/map_generator.py distinguish_levels -i tools/map_tracker/images -o tools/map_tracker/final --layout-dir tools/map_tracker/data
    ```

4. Expand the canvas and overlay backgrounds for all Tier images:

    ```bash
    python tools/map_tracker/map_generator.py tidy_tiers -i tools/map_tracker/images -o tools/map_tracker/final
    ```

5. Generate BBox data for the final map images:

    ```bash
    python tools/map_tracker/map_generator.py bbox -i tools/map_tracker/final -o tools/map_tracker/final
    ```

6. The images and BBox data under `tools/map_tracker/final` are the latest map image library.

### Glossary

- Region map: A large map of a region in the game (merged from multiple Level maps).

- Level map: A sub-region map within a region in the game.

- Tier map: A layered map used in the game.

- Overlap reassignment: To ensure the same location does not appear in two Level maps simultaneously, a max-flow/min-cut algorithm is used to allocate overlapping areas to the proper Level map.

- Canvas expansion: To ease coordinate computation, a Tier map canvas is expanded to the same size as its corresponding Level map.

- Background overlay: Since a Tier map is displayed on top of its corresponding Level map in the game, the Level map image is also overlaid onto the Tier map as a background to improve recognition accuracy.

- BBox data: The bounding-box coordinates for each map image, used to reduce matching computation.

### Alternative Plan

If zmdmap becomes unavailable, map image updates are still possible as long as you have the following data:

1. Map data: Names and geometry data for all Regions and Levels.

2. Region map unpacked images: The game stores maps using a 600\*600 tile grid (original size). You may need to stitch these tiles to obtain a full Region map image.

    > [!TIP]
    >
    > In the 720P PC game, the minimap scale is 0.1625 of the original map size.

3. Tier map unpacked images and Tier ownership metadata.
