# Map Tracker Pipeline Example

```json
{
    "MapTracking": {
        "recognition": {
            "type": "Custom",
            "param": {
                "name": "MapTrackerInfer",
                "precision": 0.4,
                "threshold": 0.5
            }
        }
    }
}
```

## Parameters

Typically, the default parameters work well for most cases. Only adjust them if you have specific needs or want to optimize for certain scenarios.

- `precision`: Range \(0.0, 1.0\]. Default 0.4. Controls the precision of location matching and rotation matching. Higher values yield more accurate results but may increase inference time.
- `threshold`: Range \[0.0, 1.0). Default 0.5. Controls the confidence threshold for location recognition. Higher values lead to higher chance of "no-hit" results.
