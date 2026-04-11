# Coding standards

## Pipeline low-code standards

### Naming: PascalCase

Node names use PascalCase and are prefixed by task or module name inside the same task, e.g. `ResellMain`, `DailyProtocolPassInMenu`, `RealTimeAutoFightEntry`.

### Avoid hard delays

Use `pre_delay`, `post_delay`, `timeout`, and `on_error` sparingly. Prefer extra recognition nodes instead of blind sleeps.

When waiting for the screen to settle, prefer `pre_wait_freezes` / `post_wait_freezes`.

> [!NOTE]
>
> For more on delays, see [ALAS basic operating mode](https://github.com/LmeSzinc/AzurLaneAutoScript/wiki/1.-Start#%E5%9F%BA%E6%9C%AC%E8%BF%90%E4%BD%9C%E6%A8%A1%E5%BC%8F); the recommended practice aligns with our `next` field.

### Hit the right node on the first screenshot pass

Expand `next` so every plausible game screen maps to an expected node—aim for one capture to land on the right state.

### Recognize → act → recognize again

Every action must be grounded in recognition.

**Good:** recognize A → tap A → recognize B → tap B

**Bad:** recognize once → tap A → tap B → tap C

_You cannot assume the screen stays the same after A—e.g. a gacha banner might appear and the next tap hits the wrong UI._

### Do not double-tap blindly

Use `pre_wait_freezes` / `post_wait_freezes` to wait for a stable frame, or insert intermediate nodes so a button is confirmed clickable. A second tap may already apply to the next screen. See [Issue #816](https://github.com/MaaEnd/MaaEnd/issues/816).

### Handle pop-ups and loading

A solid flow handles the happy path **and** pop-ups, loading, and “wrong scene” recovery.

Common `next` hooks:

- `[JumpBack]SceneDialogConfirm`
- `[JumpBack]SceneWaitLoadingExit`
- `[JumpBack]SceneAnyEnterWorld`

### OCR: full strings in `expected`

Write full text in `expected`, not fragments. Multilingual handling goes through the i18n toolchain. For fragments or hand-written regex, use `// @i18n-skip`. See [OCR & i18n](#ocr--i18n) below.

### Color matching: prefer HSV / grayscale

Different GPUs render slightly differently; raw RGB is unstable across devices. See [Color matching: HSV first](#color-matching-hsv-first) under Resource standards.

### Reuse before adding

Before writing a new node, check the [components guide](./components-guide.md) for existing capabilities.

## Go Service standards

Go Service is for recognition or interaction that Pipeline cannot express well.**Overall flow stays in Pipeline—do not implement large flowcharts in Go.**

Example: in a shopping task, Go may compare prices or iterate items; opening details, tapping buy, and returning to the list stay in Pipeline.

**Pipeline owns the flow; Go owns the hard parts.**

## Cpp Algo standards

Cpp Algo can use OpenCV and ONNX Runtime, but only for single recognition algorithms. Prefer Go Service for operations and business glue.

Other rules: [MaaFramework AGENTS.md](https://github.com/MaaXYZ/MaaFramework/blob/main/AGENTS.md).

## Pre-submit checks

```bash
pnpm format        # JSON/YAML formatting
pnpm format:go     # Go formatting
pnpm check         # Resource & schema checks
pnpm test          # Node tests
```

CI runs along the same lines: `pnpm check`, `python tools/validate_schema.py`, `pnpm test`, `pnpm format:all`.

## Files that often change together

A single feature rarely touches only one file.

### New or updated tasks

- `assets/tasks/*.json`
- `assets/resource/pipeline/**/*.json`
- `assets/locales/interface/en_us.json` (and other locales)
- `assets/interface.json`
- `tests/**/*.json`

### New Go Custom pieces

- Register in the subpackage `register.go`
- Wire in `registerAll()` in `agent/go-service/register.go`
- Run `python tools/build_and_install.py` again

> MXU is an end-user GUI—not recommended for day-to-day dev debugging. The MaaFramework dev tools above are far more productive.

## Debugging workflow

### Editing Pipeline

After changing `assets/resource/pipeline/**/*.json`, reload resources in the dev tool—no rebuild.

### Editing Go Service

After changing `agent/go-service/`, rebuild:

```bash
python tools/build_and_install.py
```

You can use the VS Code `build` task, or set breakpoints / attach to go-service.

### Editing `interface.json`

`assets/interface.json` is the source of truth. After edits:

```bash
python tools/build_and_install.py
```

If you edited `install/interface.json` via a tool, sync back to `assets/interface.json`.

### Editing Cpp Algo

Requires the VC toolchain and CMake—most contributors skip this:

```bash
python tools/build_and_install.py --cpp-algo
```

## Resource standards

### Resolution: 720p baseline

All images and coordinates (`roi`, `target`, `box`) use **1280×720** as the design resolution. MaaFramework scales at runtime. Use dev tools for screenshots and coordinate conversion.

<a id="color-matching-hsv-first"></a>

### Color matching: HSV first

Vendor GPUs (NVIDIA, AMD, Intel) differ; raw RGB is unstable across devices. Prefer fixing hue in HSV and tuning saturation/brightness.

### HDR / color management

**If HDR or “automatically manage color for apps” is on, do not capture screenshots or pick colors**—templates may not match what users see.

### Linked asset folder

The asset tree is linked: editing `assets` is equivalent to editing what ships under `install` without extra copy steps.**`interface.json` is copied**—sync manually or run `build_and_install.py`.

<a id="ocr--i18n"></a>

## OCR & i18n

Authors do not maintain multilingual OCR by hand: write `expected` in your working language and `tools/i18n` will expand it.

### Rules

- Use full strings in `expected`, not fragments. Example: write the whole sentence, not a substring.
- English `expected` values become case-insensitive regex with `\\s*` between words, e.g. `Send Local Clues` → `(?i)Send\\s*Local\\s*Clues`.
- Nodes that are not skipped get automatic `roi_offset` based on display width; `only_rec: true` nodes are excluded.

### Skipping automatic handling

For fragments or custom regex, add `// @i18n-skip` inside the `expected` array:

```jsonc
"expected": [
    // @i18n-skip
    "partial text"
]
```

Default (recommended, auto i18n):

```jsonc
"expected": [
    "This is a full example sentence"
]
```

## Testing

MaaEnd uses maa-tools for node tests—see [node testing](./node-testing.md). Add test cases when you add recognition nodes.

## Common pitfalls

| Problem                             | What to do                                                                              |
| ----------------------------------- | --------------------------------------------------------------------------------------- |
| `pnpm check` / `pnpm test` fails    | Run `pnpm install`                                                                      |
| Missing model or C++ deps           | `git submodule update --init --recursive` or `python tools/setup_workspace.py --update` |
| Go changes not applied              | Forgot `python tools/build_and_install.py`                                              |
| Referencing `__ScenePrivate*` nodes | Use the public scene interface nodes under `Interface/`                                 |
| Only happy-path, no pop-up/loading  | Treat pop-ups, loading, and in-between states as normal                                 |
| Task changed but strings missing    | Add copy under `assets/locales/`                                                        |
| Works locally but not for others    | Filters on / different FPS / GPU color drift—RGB too strict                             |
