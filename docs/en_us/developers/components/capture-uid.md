# Developer Manual - CaptureUid Reference Documentation

`CaptureUid` is a generic UID acquisition and caching module. It reads the player UID via `gamesetting.GetCachedUID` from Unity PlayerPrefs `PLDK_cachedRoleId` (game role UID), caches the raw UID digits, and converts them at output time according to `output_type` — hashed (default), masked, or raw — for other subsystems to reference as a pseudonymous identifier.

> [!important]
> The original UID is kept only in the in-memory cache and is never persisted. Use the default `hashed` pseudonymous identifier when persisting or uploading data.

## Implementation Files

The current implementation is located in `agent/go-service/captureuid/`:

| File | Responsibility |
| ------------- | ---------------------------------------------------------------------------------- |
| `action.go` | CustomAction entry point, deserializes parameters, calls `Capture` or `ClearCache` |
| `capture.go` | Core logic: registry read, hashing, caching |
| `register.go` | Registers the `CaptureUid` custom action with MaaFramework |

Related tasks call `CaptureUid` when an account identity is needed and write the default
pseudonymous hash to the shared state node `CurrentAccountIdentity.attach.account_id`. This
Resource-scoped node shares the current account identity between Go Service and cpp-algo. Only the
hash crosses the process boundary; the raw UID remains in Go memory.

## Calling in Pipeline

### Get UID

Get UID with default parameters (cache priority, allow degradation to `"unknown"`):

```json
"SomeGetUidNode": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "CaptureUid",
            "custom_action_param": {}
        }
    }
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

| Field | Type | Default | Description |
| -------------- | -------- | --------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `use_cache` | `bool` | `true` | If the cache contains a UID, return it directly without reading the registry again. |
| `allow_unknown` | `bool` | `true` | If registry read or region resolution fails, return `"unknown"` instead of throwing an error. If `false`, failure causes the action to fail. |
| `clear_cache` | `bool` | `false` | Clear the UID cache and return immediately, without reading the registry. |
| `output_type` | `string` | `"hashed"` | Output format: `hashed` (salted SHA-256, first 16 hex characters), `masked` (first and last 3 characters kept, middle replaced with `*`), or `raw` (original UID digits). |

> [!note]
> When `clear_cache` is `true`, all other parameters are ignored—the action only clears the cache and returns success directly.

Region is resolved by `gamesetting.ResolveRegion` / `SetRegion`; CaptureUid itself does not accept a region parameter.

## Calling Directly from Go Code

In addition to Pipeline, other Go modules can also call the exported functions from the `captureuid` package directly:

### Get UID (with cache)

```go
uid, err := captureuid.Capture(true, true, captureuid.OutputTypeHashed)
// useCache=true, allowUnknown=true, outputType=hashed
```

### Read Cached UID (converted to the requested output format)

```go
uid := captureuid.GetCachedUID(captureuid.OutputTypeMasked)
// Returns an empty string "" if no cache exists.
```

### Clear Cache

```go
captureuid.ClearCache()
```

## How It Works

The action executes in the following order:

1. **Cache Check** — If `use_cache` is `true` and the cache already contains a UID, return it directly.
2. **Registry Read** — Call `gamesetting.GetCachedUID()` to read `PLDK_cachedRoleId` for the current region.
3. **Digit Validation** — Verify the number has 8–12 digits. On failure, based on `allow_unknown`, either return `"unknown"` or throw an error.
4. **Output Formatting** — Convert according to `output_type`: for `hashed`, read (or generate for the first time) the random salt `debug/record/random_salt.txt`, compute `SHA-256(numeric UID + salt)`, and take the first 16 hexadecimal characters; for `masked`, keep the first and last 3 characters and mask the middle with `*`; for `raw`, return the digits unchanged.
5. **Caching** — Store the raw UID digits in an in-memory cache so subsequent calls can convert them to any `output_type`.
6. **Publish Account Identity** — The Custom Action writes the hash to the shared state node `CurrentAccountIdentity` so cross-Agent consumers such as zipline planning can identify the current account. An `"unknown"` capture publishes an empty value and planning falls back to walking.

## Privacy Design

- The original numeric UID is kept only in the in-memory cache and is **never persisted**. Even when a caller requests `raw` output, logs contain only a masked value.
- A 16-byte salt is randomly generated per installation and saved to `debug/record/random_salt.txt`.
- The `hashed` output is `SHA-256(UID digits + salt)[:16]` — a 16-character hexadecimal string, sufficient to identify the same player across sessions but irreversible to the original UID; use this format when persisting or uploading.

## Existing Integration

| User | File | Method | Purpose |
| ---------------------- | ----------------------------------------------------------------------------------- | ------------------------ | ----------------------------------------------- |
| AutoStockpile | `agent/go-service/autostockpile/selector.go` | Go API (`Capture`) | Correlate price upload with pseudonymous identity |
| CreditShopping | `agent/go-service/creditshopping/action_record.go` | Go API (`Capture`) | Correlate UID when recording shelf snapshots |
| AccountSwitch | `assets/resource/pipeline/AccountSwitch.json` (`__AccountSwitchClearUidCache` node) | Pipeline (`clear_cache`) | Clear cache after switching accounts |
| SceneImageCheck | `assets/resource/pipeline/SceneManager/SceneImageCheck.json` (`__SceneImageCaptureUid`) | Pipeline (empty-param `CaptureUid`) | Publish `CurrentAccountIdentity` during scene screen check |
| MapNavigator | `assets/resource/pipeline/Common/AccountIdentity.json` | Resource shared state node | Automatically select zipline records for the current account |
| ZiplineImport (Linux) | `agent/go-service/ziplineimport/parse.go` | Go API (`AccountIDFromRawUID`) | Derive the same account identity from the web `roleId` and store zipline records per account |

After `AccountSwitch` succeeds, it also resets scene-image initialization so the next scene task in
the same queue re-runs the screen check, and clears the UID cache plus `CurrentAccountIdentity` so
the previous account identity is not reused.
