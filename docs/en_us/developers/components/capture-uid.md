# Developer Manual - CaptureUid Reference Documentation

`CaptureUid` is a generic UID acquisition and caching module. It reads the player UID via screenshot OCR, hashes it with a random salt using SHA-256, and caches it for other subsystems to reference as a pseudonymous identifier.

> [!important]
> The original UID is not stored or recorded. Only the hashed pseudonymous identifier is retained for cross-session data correlation.

## Implementation Files

The current implementation is located in `agent/go-service/captureuid/`:

| File          | Responsibility                                                                     |
| ------------- | ---------------------------------------------------------------------------------- |
| `action.go`   | CustomAction entry point, deserializes parameters, calls `Capture` or `ClearCache` |
| `capture.go`  | Core logic: screenshot, OCR, hashing, caching                                      |
| `register.go` | Registers the `CaptureUid` custom action with MaaFramework                         |

## Calling in Pipeline

### Get UID

> [!note]
> It's not possible to process the captured UID within the pipeline. Its actual use is to take a screenshot at a stable interface during Scene navigation and cache the captured result at that moment, thereby improving recognition accuracy.

Get UID with default parameters (cache priority, current screen OCR, allow degradation to `"unknown"`):

```json
"AutoStockpileGetUid": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "CaptureUid",
            "custom_action_param": {}
        }
    },
    "next": [
        "AutoStockpileStart"
    ]
}
```

### Clear Cache

After switching accounts, the cache must be cleared to prevent reuse of old UIDs:

```json
"__AccountSwitchClearUidCache": {
    "desc": "Clear UID cache",
    "recognition": "DirectHit",
    "action": "Custom",
    "custom_action": "CaptureUid",
    "custom_action_param": {
        "clear_cache": true
    }
}
```

## Parameter Description

| Field                    | Type   | Default | Description                                                                                                                   |
| ------------------------ | ------ | ------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `use_cache`              | `bool` | `true`  | If the cache contains a UID, return it directly without taking a new screenshot for OCR.                                      |
| `stay_on_current_screen` | `bool` | `true`  | Whether to take a screenshot for OCR on the current screen. If `false`, navigates to `SceneEnterMenuOperationalManual` first. |
| `allow_unknown`          | `bool` | `true`  | If OCR fails, return `"unknown"` instead of throwing an error. If `false`, OCR failure causes the action to fail.             |
| `clear_cache`            | `bool` | `false` | Clear the UID cache and return immediately, without performing screenshot OCR.                                                |

> [!note]
> When `clear_cache` is `true`, all other parameters are ignored—the action only clears the cache and returns success directly.

## Calling Directly from Go Code

In addition to Pipeline, other Go modules can also call the exported functions from the `captureuid` package directly:

### Get UID (with cache)

```go
uid, err := captureuid.Capture(ctx, ctrl, true, true, true)
// useCache=true, stayOnCurrentScreen=true, allowUnknown=true
```

### Read Cached UID

```go
uid := captureuid.GetCachedUID()
// Returns an empty string "" if no cache exists.
```

### Clear Cache

```go
captureuid.ClearCache()
```

## How It Works

The action executes in the following order:

1. **Cache Check** — If `use_cache` is `true` and the cache already contains a UID, return it directly.
2. **Navigation (optional)** — If `stay_on_current_screen` is `false`, first execute `SceneEnterMenuOperationalManual` to navigate to an interface where the UID can be read.
3. **Screenshot** — Obtain the current screen via `ctrl.PostScreencap()`.
4. **OCR** — Recognize text within the ROI region `{60, 690, 120, 25}` and extract all numeric characters.
5. **Digit Validation** — Verify the extracted number has 8–12 digits. If not, based on `allow_unknown`, either return `"unknown"` or throw an error.
6. **Hashing** — Read (or generate for the first time) the random salt `debug/record/random_salt.txt`, compute `SHA-256(numeric UID + salt)`, and take the first 16 hexadecimal characters as the pseudonymous identifier.
7. **Caching** — Store the hash result in an in-memory cache for direct use in subsequent calls.

## Privacy Design

- The original numeric UID is **not stored or recorded**, existing only in memory for the current computation.
- A 16-byte salt is randomly generated per installation and saved to `debug/record/random_salt.txt`.
- The final identifier is `SHA-256(UID digits + salt)[:16]` — a 16-character hexadecimal string, sufficient to identify the same player across sessions but irreversible to the original UID.

## Existing Integration

| User                   | File                                                                                | Method                   | Purpose                                         |
| ---------------------- | ----------------------------------------------------------------------------------- | ------------------------ | ----------------------------------------------- |
| AutoStockpile          | `assets/resource/pipeline/AutoStockpile/Main.json` (`AutoStockpileGetUid` node)     | Pipeline                 | Get and cache UID                               |
| AutoStockpile selector | `agent/go-service/autostockpile/selector.go`                                        | Go API (`GetCachedUID`)  | Correlate price data with pseudonymous identity |
| CreditShopping         | `agent/go-service/creditshopping/action_record.go`                                  | Go API (`Capture`)       | Correlate UID when recording shelf snapshots    |
| AccountSwitch          | `assets/resource/pipeline/AccountSwitch.json` (`__AccountSwitchClearUidCache` node) | Pipeline (`clear_cache`) | Clear cache after switching accounts            |
