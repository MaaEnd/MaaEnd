# Go Service Shared Packages (`pkg/`) Reference

`agent/go-service/pkg/` holds toolkits that are **reused across features**: they do not register Custom nodes and do not carry a complete business flow. They only provide general capabilities such as "unwrapping recognition results, expression evaluation, OCR numbers, environment variables, i18n, and resource reading".

Business packages (such as `autostockpile/` and `ims/`) and shared Custom components (such as `common/expressionrecognition/`) should reuse the implementations here first. **Do not copy them again on the business side.**

> [!TIP]
> For writing the Custom component itself, read [Custom Actions and Recognition](./custom.md) and the [Go Service coding standards](./coding-standards.md#go-service-standards) first. This document only covers what is in `pkg/`: "who uses it, how to use it, and what not to reinvent".

## Package overview

Roughly sorted by how often they are reused day to day:

| Package | One-liner | Typical callers |
| --- | --- | --- |
| [`i18n`](#i18n-localized-text) | `T` / `RenderHTML`, fetch text by the client language | Almost every Action that shows the user a message |
| [`maafocus`](#maafocus-push-focus-messages-to-the-client) | Push messages to the client's Focus UI | `FocusOCRAction`, combat / Outpost Trading, etc. |
| [`recogtarget`](#recogtarget-unwrapping-and-recognition-results) | Pick the sub-recognition result that is actually needed from a Pipeline node (including `And`) | `ExpressionRecognition`, `FocusOCRAction` |
| [`boolexpr`](#boolexpr-condition-evaluation) | `{placeholder}` substitution + integer / boolean expression evaluation | `ExpressionRecognition`, IMS R1 |
| [`ocrnum`](#ocrnum-ocr-number-parsing) | OCR text → `int` (supports `1.38万` / `13.8K`, etc.) | `ExpressionRecognition`, `iconqty` |
| [`jsonclean`](#jsonclean-jsonc-cleaning) | Strip comments / trailing commas / BOM to get strict JSON | `resource`, `i18n`, all kinds of config loading |
| [`pienv`](#pienv-project-interface-environment) | Read the `PI_*` environment variables (language, controller, resource) | `i18n`, `iconqty`, startup logic |
| [`resource`](#resource-locating-and-reading-resource-files) | Find files under `resource/` / `assets/` by relative path | JSON configs such as the goods catalog and outpost data |
| [`fsutil`](#fsutil-output-paths-and-atomic-file-writes) | Path base for persistent output; temporary file + rename to avoid half-written files | Logs / debug / persistent caches |
| [`levenshtein`](#levenshtein-edit-distance) | Rune-level edit distance | OCR / fuzzy name matching |
| [`iconrecognition`](#iconrecognition--iconqty) | Shared structures for IconRecognition parameters and detail | IMS (via `iconqty`), Outpost Trading goods recognition |
| [`iconqty`](#iconrecognition--iconqty) | Grid scanning + in-cell quantity OCR | IMS A2 / A3 |
| [`minicv`](#minicv-lightweight-cv) | Pure-Go CV such as template matching and circle masks | When you need local matching and do not want to go through a Pipeline template node |
| [`parentwatch`](#parentwatch-parent-process-monitoring) | Exit when the parent process exits, to avoid an orphaned Agent | Only at `main` startup |

---

## `i18n`: Localized text

Path: `agent/go-service/pkg/i18n/`

- Text directory: `assets/locales/go-service/<lang>.json` (the sibling `interface` locale is merged in as well)
- `i18n.T("key", args...)`: short text
- `i18n.RenderHTML("registered.key", data)`: HTML template (must be registered in the package's `htmlTemplates`)

Messages shown to the user should go through i18n first and then be handed to `maafocus`. Do not hard-code Chinese text in Go.

---

## `maafocus`: Push Focus messages to the client

Path: `agent/go-service/pkg/maafocus/`

| Function | When to use |
| --- | --- |
| `Print` | Regular Focus message |
| `PrintThrottle` | Recognition / Action that runs on every heartbeat; avoids flooding the screen with the same text |
| `PrintLargeContent` | Large content goes to stdout to keep the Maa log small |
| `PrintLargeContentTrimNewline` | Large HTML, flattened into a single line to avoid client parsing issues |

---

## `recogtarget`: Unwrapping And recognition results

Path: `agent/go-service/pkg/recogtarget/`

Pipelines commonly have two kinds of "number / text source":

1. A plain `OCR` node — the detail returned by `RunRecognition` is the OCR result itself.
2. An `And` node — the detail is a `CombinedResult`, and what you actually need is the entry that `box_index` points to (usually OCR).

If business code digs through `CombinedResult` on its own, it can easily get out of step with the flat syntax / v2 syntax and with nested And references by name. **Any logic that "runs a recognition node and then takes the OCR text or box" should go through this package.**

### What it solves

- Supports both flat and v2 node JSON:
    - Flat: `"recognition":"And","all_of":[...],"box_index":n`
    - v2: `"recognition":{"type":"And","param":{"all_of":[...],"box_index":n}}`
- Takes the sub-result from `CombinedResult` using the node's native `box_index`.
- If a sub-item is itself a reference to an And node by name, it keeps drilling down along the `box_index` chain (a cycle is reported as an error).

### Common APIs

| Function | Purpose |
| --- | --- |
| `ParseNodeJSON` | Parse node JSON → `Fields{Type, AllOf, BoxIndex}` |
| `ResolveAndBoxIndex` | Check whether the node is an And and return a valid `box_index` |
| `SelectDetail` | **Preferred**: given `ctx` + node name + detail, select the target sub-result (supports nesting) |
| `SelectDetailFromJSON` | Select one level when you already have the node JSON (use `SelectDetail` for nested And) |
| `SelectedDetail` | Take `CombinedResult[i]` directly when the index is known |
| `EffectiveType` | Resolve the "effective recognition type" along the And chain (e.g. it ends up as `OCR`) |

### Recommended usage

```go
import (
    "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
    "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
)

detail, err := ctx.RunRecognition(nodeName, arg.Img)
if err != nil {
    return 0, err
}

selected, err := recogtarget.SelectDetail(ctx, nodeName, detail)
if err != nil {
    return 0, err
}

// Numbers: hand off to ocrnum; text / Box: read selected directly
return ocrnum.Extract(selected)
```

Reference implementations: `common/expressionrecognition` (reads numbers) and `common/focusocr` (reads the OCR text, then calls `maafocus.Print`).

### What not to do

- Do not hand-write `detail.CombinedResult[0]` and assume it is always OCR.
- Do not build yet another parser for "OCR or And→OCR"; to extend it, change `recogtarget`.
- Do not override the node's native field with a business-defined "fake box_index"; keep the unwrapping semantics consistent with the Pipeline node definition.

---

## `boolexpr`: Condition evaluation

Path: `agent/go-service/pkg/boolexpr/`

Turns an "expression string with placeholders" into an integer / boolean expression that can be evaluated. Both [`ExpressionRecognition`](./custom.md#expressionrecognition) on the Pipeline side and IMS R1 `ItemQuantitySatisfied` are built on it: in the former the placeholders are recognition node names, in the latter they are item IDs.

### Two-step flow

1. **`ResolvePlaceholders(expr, resolve)`**  
   Matches `{name}` with a regex, calls `resolve(name) (int, error)` to get an integer, substitutes it into the expression, and returns a `map[string]int` for logging.
2. **`Evaluate(resolved)`**  
   Parses the AST with the Go standard library `go/parser` and evaluates it over integers / booleans. To decide whether the recognition hits, the caller must assert that the result is a `bool`.

### Supported operations

| Category | Operators |
| --- | --- |
| Arithmetic | `+` `-` `*` `/` `%` |
| Comparison | `<` `<=` `>` `>=` `==` `!=` |
| Logical | `&&` `\|\|` `!` |
| Grouping | `(...)` |

Literals outside the platform's `int` range are clamped to `IntMax` / `IntMin` and a warning is logged; evaluation continues instead of failing outright (the same overflow policy as `ocrnum`).

### Recommended usage

```go
import "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"

resolved, values, err := boolexpr.ResolvePlaceholders(
    `{CurrentCredit}-{RefreshCost}<400`,
    func(name string) (int, error) {
        // node name → OCR integer; or item ID → cached quantity
        return lookup(name)
    },
)
if err != nil {
    return false, err
}

result, err := boolexpr.Evaluate(resolved)
if err != nil {
    return false, err
}
matched, ok := result.(bool)
if !ok {
    return false, fmt.Errorf("expression must evaluate to bool")
}
_ = values // for logging
return matched, nil
```

### Division of work with Pipeline

| Need | What to use |
| --- | --- |
| Compare several OCR numbers / decide after adding or subtracting them | Pipeline node + `ExpressionRecognition` (already uses this package internally) |
| Whether IMS cached quantities are sufficient | IMS R1 `ItemQuantitySatisfied` (placeholders are item IDs, not node names) |
| Evaluate a similar expression yourself in Go | Call `boolexpr` directly; do not embed another expression parser |

---

## `ocrnum`: OCR number parsing

Path: `agent/go-service/pkg/ocrnum/`

Reads the quantity in OCR text as an `int`:

- English magnitudes: `k` / `m` / `b` (case-insensitive)
- Chinese / Korean: `万` `萬` `만`, `亿` `億` `억`
- Decimals and a single thousands-separator comma (e.g. `12,345`)

| Function | Purpose |
| --- | --- |
| `Parse` | Plain text → `int` |
| `Extract` | Take the OCR from a `RecognitionDetail`, then `Parse` |

Typical chain: `RunRecognition` → `recogtarget.SelectDetail` → `ocrnum.Extract`.

---

## Other highly reused packages (summary + usage notes)

### `jsonclean`: JSONC cleaning

Path: `pkg/jsonclean/`

Many configs in the repository are **JSONC** (`//`, `/* */`, trailing commas, optional BOM). When `encoding/json` cannot handle them:

```go
json.Unmarshal(jsonclean.Clean(raw), &out)
```

Line numbers are preserved as far as possible (comment lines are replaced with line breaks), so errors are easy to locate. Almost every "read a project JSON config" should run through it first.

### `pienv`: Project Interface environment

Path: `pkg/pienv/`

At startup the Client injects `PI_*` (see Project Interface v2.5). `Init()` / `Get()` parse them into a singleton:

- `ClientLanguage()` → passed to `i18n`
- `ControllerType()` / `ControllerName()` → tell Win32 / Adb, etc. apart
- `ControllerKind()` / `NormalizeControllerKind()` / `IsAdbLikeControllerType()` → the normalized controller kind (`adb` / `playcover` / `native_android` / `win32` / `macos` / `linux` / `gamepad`); letter case and aliases such as `play_cover` are all handled here
- `ResolveControllerKind(ctrl)` → the fallback version of the normalized kind: when PI_CONTROLLER is missing or of an unknown type, it decides with `controller.GetInfo()` instead. The PI protocol allows the Client to omit variables that are unavailable, so **you cannot assume PI_CONTROLLER always exists**
- `ResourceName()`, version numbers, etc.

Do not scatter your own `os.Getenv("PI_...")` parsing around.

To determine the controller type, prefer `ControllerType()`; for comparisons, go through `ControllerKind()` / `NormalizeControllerKind()` (do not hand-roll your own `ToLower` string comparisons). When you need to handle "the Client omits `PI_CONTROLLER`", use `ResolveControllerKind(ctrl)` — `ctrl` must be the one already obtained in this callback and passed down; do not call `GetController()` again just to check the type: every extra controller fetch in the same callback invalidates the one obtained before (see [Coding Standards · Using Handles Inside Callbacks](./coding-standards.md#using-handles-inside-callbacks)). When ADB, PlayCover (including the alias `play_cover`) and native Android share the "touch + mobile UI" branch, check with `IsAdbLikeControllerType()`.

### `resource`: Locating and reading resource files

Path: `pkg/resource/`

- `FindResource` / `ReadResource` / `ReadJsonResource` (already runs `jsonclean` internally)
- Lookup order: absolute / relative path → resource sink base path → `resource/` and `assets/` in the cwd and its parent directories

Use this package to read the goods table, outpost data, etc.; do not hard-code `install/...` absolute paths.

### `fsutil`: Output paths and atomic file writes

Path: `pkg/fsutil/`

**Output paths**: persistent output such as logs, debug screenshots, records under `debug/record/` and exported files is always rooted at **the working directory at process startup**; it depends neither on the working directory at call time nor on the location of the executable.

| Function | Purpose |
| --- | --- |
| `StartupDir()` | Returns the working directory captured at startup and the capture error; `main` checks it once at startup and exits on failure |
| `OutputPath(parts...)` | Joins a relative path under the startup working directory and returns the absolute path |

```go
path := fsutil.OutputPath("debug", "record", "IMS.json")
```

- Both writing files and reading back files you wrote go through `OutputPath`, so reads and writes hit the same location; do not write relative paths like `filepath.Join("debug", ...)` anymore.
- Only for **output**. To read the `resource/`, `data/` and `locales/` shipped with the package, use the `resource` package above; for temporary files, use the system temporary directory.
- The cpp-algo counterpart is `common::OutputPath` (`source/Common/output_paths.h`); files shared by both sides (such as `debug/record/random_salt.txt` and `Ziplines.json`) line up because they use the same base.

**Atomic writes**: `WriteFileAtomic(path, content, perm)`: writes a temporary file in the same directory, then renames it. Suitable for debug dumps and overwritable caches; **durability across power loss is not guaranteed** (no directory fsync).

### `levenshtein`: Edit distance

Path: `pkg/levenshtein/`

`Distance(a, b string) int` compares by **rune**, which suits approximate name matching under CJK OCR noise. The threshold is up to the business code.

### `iconrecognition` / `iconqty`

| Package | Responsibility |
| --- | --- |
| `iconrecognition` | Shared structures such as `Params` / `Detail` / `Match` / `GridType`; **business packages must not declare their own copies** |
| `iconqty` | Runs IconRecognition on the grid cells, then OCRs the quantity by `cell_box` (IMS default ROI / filters) |

For the detailed protocol and Pipeline usage, see [IconRecognition](./components/icon-recognition.md) and [IMS](./components/ims.md).

### `minicv`: Lightweight CV

Path: `pkg/minicv/`

Pure-Go template matching (with a SIMD-accelerated path), integral images, circle-mask templates, in-circle statistics, filled-circle drawing, etc. Suitable for local matching inside the Agent; anything that can be expressed with a Pipeline `TemplateMatch` should still be written in Pipeline first.

### `parentwatch`: Parent process monitoring

Path: `pkg/parentwatch/`

Call `Start()` only once, at `main` startup: when the parent process exits, it calls `os.Exit(0)` so that MaaAgentServer does not become an orphan. Business packages must not depend on it.

---

## Quick selection guide

| I want to… | Use |
| --- | --- |
| Show the user a message in the current language | `i18n` + `maafocus` |
| Run a recognition node and take the actual OCR / Box inside an And | `recogtarget` |
| Turn OCR text into an integer | `ocrnum` |
| Conditions like `{A}+{B}<100` | Pipeline → `ExpressionRecognition`; in Go → `boolexpr` |
| Read a JSON config with comments | `jsonclean` (or via `resource.ReadJsonResource`) |
| Tell Adb from Win32 | `pienv.ResolveControllerKind(ctrl)` + `IsAdbLikeControllerType()` |
| Scan backpack cells and read quantities | `iconrecognition` + `iconqty` |
| Decide where logs / records / exported files are written | `fsutil.OutputPath` |
| Write files without leaving half-written content | `fsutil.WriteFileAtomic` |

## Related documentation

- [Custom Actions and Recognition](./custom.md) (`ExpressionRecognition`, `FocusOCRAction`, etc.)
- [Coding Standards · Go Service](./coding-standards.md#go-service-standards)
- [Component Guide](./components-guide.md)
- [IconRecognition](./components/icon-recognition.md)
- [IMS](./components/ims.md)
