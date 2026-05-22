# Coding standards

## AI programming standards

### Do not use AI blindly for development

- Giving AI vague instructions such as “implement feature X and open a PR” or “fix this bug and open a PR” without understanding the work.
- Using AI to generate large amounts of unmaintainable, opaque “black box” code in critical modules—for example pointless over-abstraction, or thousands of lines of Go/C++ for a simple feature.
- Submitting code in critical modules that you do not understand and cannot control.

_Custom code is usually maintainable only by the author who wrote it. If the author cannot read it, nobody can extend it—let alone fix bugs. Do not mindlessly let AI take full responsibility for fixes without reviewing or understanding the changes; moreover, fully offloading work to AI still has a low success rate in this project._

### Recommended way to use AI

- Learn this project’s coding standards first; do your own architecture, or use AI suggestions as a reference.
- Use AI for targeted, incremental development, and review generated code yourself to ensure it matches intent.
- Submit a PR only after you are confident the changes are correct.

## Pipeline low-code standards

### Naming: PascalCase

Node names use PascalCase and are prefixed by task or module name inside the same task, e.g. `ResellMain`, `DailyProtocolPassInMenu`, `RealTimeAutoFightEntry`.

### Avoid hard delays

Use `pre_delay`, `post_delay`, `timeout`, and `on_error` sparingly. Prefer extra recognition nodes instead of blind sleeps.

Only use `pre_wait_freezes` / `post_wait_freezes` when the screen must settle; avoid delays otherwise.  
**Don't use delays to work around instability — add intermediate recognition nodes instead. A delay hides the real problem and will still fail on slower devices.**

> [!NOTE]
>
> For more on delays, see [ALAS basic operating mode](https://github.com/LmeSzinc/AzurLaneAutoScript/wiki/1.-Start#%E5%9F%BA%E6%9C%AC%E8%BF%90%E4%BD%9C%E6%A8%A1%E5%BC%8F); the recommended practice aligns with our `next` field.

### Hit the right node on the first screenshot pass

Expand `next` so every plausible game screen maps to an expected node—aim for one capture to land on the right state.  
**The project generally rejects any form of retry mechanism. Tasks must complete in a single pass. If a problem seems unsolvable without retries, it must be discussed in the dev group.**

### Recognize → act → recognize again

Every action must be grounded in recognition.

**Good:** recognize A → tap A → recognize B → tap B

**Bad:** recognize once → tap A → tap B → tap C

For example:

1. During UI navigation: recognize the navigation button → tap it → recognize that the screen has finished transitioning.  
   _You cannot assume the screen stays the same after tapping a close button. In extreme cases the game may show a new banner pool notice; tapping the next node directly might hit the gacha screen._  
   _You cannot assume no background loading is needed during a screen transition—the screen may freeze; tapping the next node directly may do nothing._

2. When tapping buttons that change account data: recognize the submit button → tap it → recognize that the tap succeeded.  
   _You cannot assume every user has smooth network connectivity. If the button tap never reaches the server, the whole UI may freeze and ignore further taps._

### Do not blindly retry or add limits

**Recommended:** When you hit a bug, find the root cause—down to which node failed, which recognition missed, what in-game trigger caused a mis-tap or no response—and fix recognition or action on that node.

**Forbidden:** Retry the same operation, or blindly add `max_hit`.

For example:

1. Tap again when the first tap had no effect.  
   _Use `pre_wait_freezes` / `post_wait_freezes` to wait for a stable frame, or insert intermediate nodes so a button is confirmed clickable. A second tap may already apply to the next screen. See [Issue #816](https://github.com/MaaEnd/MaaEnd/issues/816)._

2. Re-run a sub-task after it failed.  
   _Retries only slightly improve success rate; they do not fix the root issue and make the code hard to maintain—eventually you get “try B if A fails, try C if B fails, retry A 3 times, B 2 times,” and problems become hard to pinpoint._

3. Add `max_hit` when a node loops forever.  
   _Infinite loops are usually recognition or logic bugs; blindly adding `max_hit` just aborts the flow—like throwing an exception to exit the task—with unpredictable consequences._

### Handle pop-ups and loading

A good flow is not just “the main path runs”—it must handle the main path, pop-ups, loading waits, and automatically recover when not in the target scene.

Common `next` hooks:

- `[JumpBack]SceneDialogConfirm`
- `[JumpBack]SceneWaitLoadingExit`
- `[JumpBack]SceneAnyEnterWorld`

### OCR: full strings in `expected`

Write full text in `expected`, not fragments. Multilingual handling goes through the i18n toolchain. For fragments or hand-written regex, use `// @i18n-skip`. See [OCR & i18n](#ocr--i18n) below.

### Reuse before adding

Before writing a new node, check the [components guide](./components-guide.md) for existing capabilities.

## Go Service standards

Go Service is for recognition or interaction that Pipeline cannot express well.**Overall flow stays in Pipeline—do not implement large flowcharts in Go.**

Example: in a shopping task, Go may compare prices or iterate items; opening details, tapping buy, and returning to the list stay in Pipeline.

**Pipeline owns the flow; Go owns the hard parts.**  
_Unnecessary Go logic greatly increases complexity, makes debugging very hard for the next developer, and cross-platform adaptation much harder._

## Cpp Algo standards

Cpp Algo can use OpenCV and ONNX Runtime, but only for single recognition algorithms. Prefer Go Service for operations and business glue.

Other rules: [MaaFramework development standards](https://github.com/MaaXYZ/MaaFramework/blob/main/AGENTS.md#%E5%BC%80%E5%8F%91%E8%A7%84%E8%8C%83).

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
