# Coding Standards

## AI Programming Standards

### Prohibit Brainless Use of AI for Development

- Issuing vague commands to AI directly, such as "help me develop xxx feature and submit a PR" or "help me fix this bug and submit a PR."
- In critical modules, using AI to generate large amounts of hard-to-maintain, hard-to-understand "black box" code—for example, meaningless over-encapsulation or piling thousands of lines of Go/C++ for simple features.
- Submitting code that you cannot understand or control in critical modules.

> [!CAUTION]
> It is prohibited to have AI directly write Pipelines without providing context such as game interface screenshots and interface navigation logic.
> MaaFramework's Pipeline is heavily dependent on the game interface and business logic. AI lacking interface information can only rely on hallucinations and existing project code to piece things together, resulting in very low-quality code.
> Adequate information must include: each recognition node needs to provide `roi` and template images, and explain the navigation relationship between interfaces (from which interface, click what, to where).
>
> PRs that do not meet the above conditions will be closed directly.

_Custom code is usually only maintainable by the original author. If even the author cannot understand it, don't even think about extending new features—no one will dare to fix bugs either. Moreover, you cannot thoughtlessly let AI take full responsibility for fixes—neither reviewing nor understanding the changes yourself; furthermore, the success rate of fully handing things over to AI is still relatively low in this project._

### Recommended AI Development Approach

- First learn the coding standards of this project; design the architecture yourself, or use AI suggestions as a reference.
- Use AI for targeted incremental development, and review the generated code yourself to ensure it meets expectations.
- Submit a PR only after confirming everything is correct.

## Pipeline Low-Code Standards

### Naming: PascalCase

Node names use PascalCase, prefixed with the task name or module name within the same task. For example, `ResellMain`, `DailyProtocolPassInMenu`, `RealTimeAutoFightEntry`.

### Prohibit Hard Delays

Use `pre_delay`, `post_delay`, `timeout`, `on_error` as little as possible. Avoid blind sleep by adding intermediate recognition nodes.

Use `pre_wait_freezes` / `post_wait_freezes` only when it is absolutely necessary to wait for the screen to stabilize; otherwise, avoid delays as much as possible.  
**Do not use delays for stability; instead, add intermediate node judgments, because delays are actually masking problems and will still be unstable when the user's device has high latency.**

> [!NOTE]
>
> For more on delays, you can read the [basic operating mode of the neighboring ALAS](https://github.com/LmeSzinc/AzurLaneAutoScript/wiki/1.-Start#%E5%9F%BA%E6%9C%AC%E8%BF%90%E4%BD%9C%E6%A8%A1%E5%BC%8F), whose recommended practices are essentially equivalent to our `next` field.

### `next` Hit on the First Round

Expand the `next` list as much as possible to ensure that any game screen is expected, achieving the target node hit with a single screenshot.  
**The project generally rejects all forms of retry mechanisms. All tasks must be completed in a single process, unless encountering an unsolvable problem, which must be discussed in the development group.**

### Recognize → Operate → Recognize Again

Each step of operation is based on recognition.

**Recommended:** Recognize A → Click A → Recognize B → Click B

**Prohibited:** Recognize everything once → Click A → Click B → Click C

For example:

1. In interface navigation, you need to recognize the navigation button → click the navigation button → recognize that the interface has finished navigating.
   _You cannot guarantee that the screen will still be the same after clicking the close button. In extreme cases, a game might pop up a new pool announcement, and directly clicking the next node might lead you into the gacha system._
   _You cannot guarantee whether background loading is needed during interface navigation, causing the screen to freeze. Directly clicking the next node might have no effect._

2. When clicking a button that changes account data, you need to recognize the submit button → click the submit button → recognize that the button click was successful.\_
   _You cannot guarantee that every user's network is smooth. If the button click event does not successfully interact with the server, the entire interactive interface may freeze and become unresponsive, no matter how many times you click._

### Do Not Blindly Retry or Add Limits

**Recommended:** When encountering a bug, find the root cause, specifying which node failed, which recognition did not meet expectations, and what the game triggered that caused the unintended interaction or lack of response. Then fix the recognition and operation issues of the corresponding node.

**Prohibited:** Trying the same operation again, blindly adding max_hit.

For example:

1. When a click has no response, clicking again.
   _Wait for the screen to stabilize using `pre_wait_freezes`, `post_wait_freezes`, or add an intermediate node to confirm the button is clickable before executing. The second click might have already acted on an element on the next screen. See [Issue #816](https://github.com/MaaEnd/MaaEnd/issues/816) for details._

2. Rerunning a sub-task after it fails.
   _Retrying only slightly increases the success rate and does not fundamentally solve the problem. It only makes the code harder to maintain, eventually leading to trying A, then B, then C if it fails, retrying A 3 times and B 2 times, making the problem difficult to locate._

3. Adding max*hit after a node enters an infinite loop.
   \_An infinite loop is usually caused by recognition issues or logical flaws. Blindly adding `max_hit` will only interrupt the logic, similar to blindly throwing exceptions in code to break out of a task, leading to unpredictable consequences.*

### Handling Pop-ups and Loading

A good process is not just "the main flow works," but: the normal main flow works, pop-ups can be handled, loading can be waited through, and it can automatically jump past when not in the target scenario.

Common practice is to add to `next`:

- `[JumpBack]SceneDialogConfirm`
- `[JumpBack]SceneWaitLoadingExit`
- `[JumpBack]SceneAnyEnterWorld`

### OCR Write Complete Text

`expected` must contain the complete text, not partial. Multi-language processing is delegated to the i18n toolchain. **Only when the OCR engine is unstable in recognizing complete text** is it allowed to use fragments or handwritten regular expressions. In this case, you must add `// @i18n-skip` and a comment above the array to retain the complete original text. See below [OCR and i18n](#ocr-and-i18n) for details.

### Reuse First, Then Add New

Before writing new nodes, first check the [Component Guide](./components-guide.md) to confirm if there is existing capability.

## Go Service Standards

Go Service is only for handling complex image algorithms or special interaction logic that is difficult to implement with Pipeline. **The overall process is still connected by Pipeline. It is prohibited to write extensive process code in Go.**

For example, in a product purchase task, Go Service only does price comparison, product traversal, etc.; opening product details, clicking buy, returning to the list, etc., are handled by Pipeline.

In one sentence: **Pipeline manages the process, Go manages the difficulties.**  
_Unnecessary Go logic greatly increases code complexity, making it extremely difficult for the next developer to develop and debug, and very challenging for cross-platform adaptation._

## Cpp Algo Standards

Cpp Algo supports native OpenCV and ONNX Runtime, but it is only recommended for implementing individual recognition algorithms. Various business logic like operations is recommended to be written using Go Service.

Refer to the [MaaFramework Development Standards](https://github.com/MaaXYZ/MaaFramework/blob/main/AGENTS.md#%E5%BC%80%E5%8F%91%E8%A7%84%E8%8C%83) for other standards.

## Pre-submission Check

```bash
pnpm format        # JSON/YAML formatting
pnpm format:go     # Go formatting
pnpm check         # Resource and schema check
pnpm test          # Node testing
```

CI is also built around these validations: `pnpm check`, `python tools/validate_schema.py`, `pnpm test`, `pnpm format:all`.

## Supporting Files

A functional change in MaaEnd often involves more than one place.

### Adding or Modifying Tasks

- `assets/tasks/*.json`
- `assets/resource/pipeline/**/*.json`
- `assets/locales/interface/zh_cn.json`
- `assets/interface.json`
- `tests/**/*.json`

### Adding Go Custom Components

- Register in the corresponding sub-package `register.go`
- Integrate in `agent/go-service/register.go`'s `registerAll()`
- Re-run `python tools/build_and_install.py`

> MXU is a GUI for end-users and is not recommended for daily development and debugging. The above development tools can greatly improve development efficiency.

## Debug Workflow

### Editing Pipeline

After modifying `assets/resource/pipeline/**/*.json`, just reload the resource in the development tool; no recompilation is needed.

### Editing Go Service

After modifying `agent/go-service/`, you must recompile:

```bash
python tools/build_and_install.py
```

You can use the `build` task in VS Code's terminal run tasks for quick execution, or set breakpoints or attach debugging to go-service.

### Editing `interface.json`

`assets/interface.json` is the main source file. After modification, run:

```bash
python tools/build_and_install.py
```

If `install/interface.json` is modified through a tool, it needs to be manually synced back to `assets/interface.json`.

### Editing Cpp Algo

Requires a VC generator and cmake; generally, developers do not need to change it:

```bash
python tools/build_and_install.py --cpp-algo
```

## Resource Standards

### Resolution: 720p Base

All images and coordinates (`roi`, `target`, `box`) are based on **1280x720**. MaaFramework automatically converts based on the user's device at runtime. It is recommended to use the above development tools for screenshots and coordinate conversion.

### HDR / Color Management

**When prompted that features like "HDR" or "Automatically manage application colors" are enabled, do not perform screenshots, color picking, or other operations**, as this may cause the template effect to differ from the user's actual display.

### Resource Folder Link

The resource folder is in a linked state. Modifying `assets` is equivalent to modifying the content in `install`; no additional copying is needed. **However, `interface.json` is a copy**; modification requires manual sync or running `build_and_install.py`.

<a id="ocr-与-i18n"></a>

## OCR and i18n

Developers do not need to manually maintain multi-language OCR; just write the expected text in the current language, and `tools/i18n` will automatically process it.

### Writing Requirements

- `expected` should contain the complete text, not just a fragment. For example, write "This is an example content" instead of just "example content."
- Only when the OCR engine is unstable in recognizing complete text (e.g., containing percent signs, special symbols, easily confused characters) and it is truly necessary to use fragments or handwritten regular expressions for stable matching is skipping automatic processing allowed. **Truncation is not a default method but a fallback.**

Requirements:

1. Add `// @i18n-skip` inside the `expected` array to have the i18n tool skip that node;
2. Add a normal JSON comment above the array to retain the **complete original text** for review, multilingual reference, and future restoration of complete matching after OCR engine upgrades.

```jsonc
// OCR engine is unstable with percent signs in "stable production 100%", using truncated match
"expected": [
    // "稳定生产 100%"
    // @i18n-skip
    "稳定生产"
]
```

Default writing (recommended, automatic i18n processing):

```jsonc
"expected": [
    "This is an example content"
]
```

- English `expected` will automatically generate case-insensitive regular expressions after processing, using `\\s*` between words. For example, `Send Local Clues` → `(?i)Send\\s*Local\\s*Clues`.
- For OCR nodes not skipped in processing, the script will automatically supplement `roi_offset` based on display width differences; nodes with `only_rec: true` are excluded.

## Testing

MaaEnd uses maa-tools for node testing. See [Node Testing Documentation](./node-testing.md) for details. Please try to add test cases when writing recognition nodes.

## Common Pitfalls

| Pitfall                                                      | Handling                                                                                            |
| ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| `pnpm check` / `pnpm test` fails to run                      | `pnpm install`                                                                                      |
| Model or C++ dependency directory missing                    | `git submodule update --init --recursive` or `python tools/setup_workspace.py --update`             |
| Go changes not taking effect                                 | Forgot `python tools/build_and_install.py`                                                          |
| Directly referenced `__ScenePrivate*` nodes                  | Should reference scene interface nodes exposed in the `Interface` directory                         |
| Only focusing on the main flow, not handling pop-ups/loading | Treat pop-ups, loading, and intermediate states as normal scenarios                                 |
| Changed tasks but didn't add text                            | Text goes in `assets/locales/`                                                                      |
| Runs locally but not for others                              | Filters enabled/different frame rates/slightly different colors due to different GPUs/RGB too rigid |
