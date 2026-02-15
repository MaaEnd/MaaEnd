# Map Tracker Pipeline Example

```json
{
  "MyNode": {
    "recognition": {
      "type": "Custom",
      "param": {
        "custom_recognition": "MapTrackerInfer",
        "custom_recognition_param": {
          "precision": 0.4,
          "threshold": 0.5,
          "map_name_regex": "^map\\d+_lv\\d+$"
        }
      }
    }
  }
}
```

## Parameters

Typically, the default parameters work well for most cases. Only adjust them if you have specific needs or want to optimize for certain scenarios.

- `precision`: Range \(0.0, 1.0\]. Default 0.4. Controls the precision of location matching and rotation matching. Higher values yield more accurate results but may increase inference time.
- `threshold`: Range \[0.0, 1.0). Default 0.5. Controls the confidence threshold for location recognition. Higher values lead to higher chance of "no-hit" results.
- `map_name_regex`: String. A regular expression that filters map names. Only maps whose names match this regex will be used for matching.
  - `^map\\d+_lv\\d+$`: Matches all normal maps. (Default)
  - `^map\\d+_lv\\d+_(tier_\\d+)?$`: Matches all normal maps and tier maps.
  - `^map001_lv001$`: Matches only map001_lv001.
  - `^map001_lv\\d+$`: Matches all levels of map001.
