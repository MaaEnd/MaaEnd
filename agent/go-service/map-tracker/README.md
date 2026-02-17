# MapTracker Reference

This document shows how to use MapTracker series nodes.

## Action: MapTrackerMove

🚶Moves the player to one or more **target locations** on the map.

### Definition

- `type`: Custom
- `custom_action`: MapTrackerMove
- `custom_action_param`: (required)
    - `map_name`: string
    - `targets`: list of int[2]

### Parameters

- `map_name`: The exact name of the map. For example, "map001_lv001".
- `targets`: A list of target locations on the map, each represented as a pair of integers [x, y].

### Example

Here is a pipeline example.

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerMove",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "targets": [
                [
                    679,
                    341
                ],
                [
                    688,
                    350
                ],
                [
                    679,
                    358
                ],
                [
                    670,
                    350
                ]

            ]
        }
    }
}
```

### FAQ

- **Where can I find the map names?**  
   Please refer to `/assets/resource/image/MapTracker/map`. Note that the name suffix "\_merged.png" is not part of the map name.
- **How are the coordinates measured?**  
   The coordinates are measured in mini-map image pixels, where (0, 0) is the top-left corner.
- **How to get target coordinates?**  
   _We provide a MapTracker tool_ to easily generate location targets via GUI. Please install Python and `opencv-python`, then run the script at `/tools/map_tracker_tool.py`.

> **Note**: The MapTracker tool can also open and edit an existing `targets` list from a given pipeline file.

## Recognition: MapTrackerInfer

📍Gets the player's current **location and rotation** on the map by analyzing the mini-map in the game screen.

### Definition

- `type`: Custom
- `custom_recognition`: MapTrackerInfer
- `custom_recognition_param`: (optional)
    - `map_name_regex`: string
    - `precision`: float
    - `threshold`: float

### Parameters

- `map_name_regex`: A [regular expression](https://regexr.com/) that filters map names. Only maps whose names match this regex will be used for matching. For example:
    - `^map\\d+_lv\\d+$`: Matches all normal maps. (Default)
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: Matches all normal maps and tier maps.
    - `^map001_lv001$`: Matches only "map001_lv001".
    - `^map001_lv\\d+$`: Matches all levels of "map001".
- `precision`: Range \(0.0, 1.0\]. Default 0.4. Controls the precision of matching. Higher values yield more accurate results but increase inference time.
- `threshold`: Range \[0.0, 1.0). Default 0.5. Controls the confidence threshold for a success recognition.

> **Note**: Typically, the default `precision` and `threshold` work well for most cases. Only adjust them if you have specific needs.

### Result

Please refer to the type `maptracker.InferResult` in `/agent/go-service`:

```go
type InferResult struct {
	MapName   string  `json:"mapName"`   // Map name
	X         int     `json:"x"`         // X coordinate on the map
	Y         int     `json:"y"`         // Y coordinate on the map
	Rot       int     `json:"rot"`       // Rotation angle (0-359 degrees)
	LocConf   float64 `json:"locConf"`   // Location confidence
	RotConf   float64 `json:"rotConf"`   // Rotation confidence
	LocTimeMs int64   `json:"locTimeMs"` // Location inference time in ms
	RotTimeMs int64   `json:"rotTimeMs"` // Rotation inference time in ms
}
```

### FAQ

- **How to match location only in specific maps?**  
   Please use the `map_name_regex` parameter to filter map names. Be careful that you must ensure the player is just in the map that can be matched, otherwise the recognition may fail.
