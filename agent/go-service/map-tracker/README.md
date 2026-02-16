# Map Tracker Reference

## Examples

## Pipeline Node

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

## Go Agent Result Handling

```go
type MyAction struct{}

var _ maa.CustomActionRunner = &MyAction{}

func (a *MyAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg.RecognitionDetail == nil {
		return false;
	}

	var result InferResult
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}

	if err := json.Unmarshal([]byte(arg.RecognitionDetail.DetailJson), &wrapped); err != nil {
    return false;
  }
  
  if err := json.Unmarshal(wrapped.Best.Detail, &result); err != nil {
    return false;
  }

	msg := fmt.Sprintf("- Map: %s\n- Coord: (%d, %d)\n- Rot: %d°\n- Time: %dms/%dms\n- Conf: %.2f/%.2f",
		result.MapName, result.X, result.Y, result.Rot,
		result.LocTimeMs, result.RotTimeMs,
		result.LocConf, result.RotConf)
  println(msg)

	return true;
}
```

## Parameters

Typically, the default parameters work well for most cases. Only adjust them if you have specific needs or want to optimize for certain scenarios.

- `precision`: Range \(0.0, 1.0\]. Default 0.4. Controls the precision of matching. Higher values yield more accurate results but increase inference time.
- `threshold`: Range \[0.0, 1.0). Default 0.5. Controls the confidence threshold for a success recognition.
- `map_name_regex`: String. A regular expression that filters map names. Only maps whose names match this regex will be used for matching.
    - `^map\\d+_lv\\d+$`: Matches all normal maps. (Default)
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: Matches all normal maps and tier maps.
    - `^map001_lv001$`: Matches only "map001_lv001".
    - `^map001_lv\\d+$`: Matches all levels of "map001".
