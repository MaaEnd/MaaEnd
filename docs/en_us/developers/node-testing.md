# Development Guide - Node Testing

This document describes how node tests are currently written in MaaEnd. The rules here follow `maatools.config.mts`, `tools/schema/test.schema.json`, and the existing files under `tests/`.

## What Node Tests Are For

Node tests verify which nodes should hit, and which should not hit, on a static screenshot.

- Good for validating recognition nodes, reusable nodes, and scene-detection nodes.
- Good for regression coverage when templates, thresholds, or ROI are changed.
- Not a replacement for full workflow debugging; sequence correctness still needs to be checked in development tools and on real runs.

## Directory Layout

The repository currently uses this structure:

```text
tests/
|- MaaEndTestset/
|  |- Win32/Official_CN/*.png
|  `- ADB/Official_CN/*.png
|- Common/Button/test_button.json
|- DeliveryJobs/test_region.json
`- ...
```

- Test definition files can be placed anywhere under `tests/`, but the filename must match `test_*.json`.
- Test screenshots live under `tests/MaaEndTestset/`.
- The `image` field contains the screenshot name, and it may also include the file extension.
- The real screenshot path is resolved by `maatools.config.mts`; in practice it points under `tests/MaaEndTestset/<controller>/<resource>/` and matches the concrete screenshot file from `image`.

With the current config:

- `controller = "Win32"` maps to `tests/MaaEndTestset/Win32/`.
- `controller = "ADB"` maps to `tests/MaaEndTestset/ADB/`.
- `controller = ["Win32", "ADB"]` runs the same cases once under each controller directory.
- `resource = "官服"` maps to `tests/MaaEndTestset/*/Official_CN/`.
- If either `controller` or `resource` is an array, tests are expanded as a Cartesian product.

If you add a new resource server or controller enum, update the mapping in `maatools.config.mts` as well.

## File Schema

The file structure is validated by `tools/schema/test.schema.json`. The top level must contain `configs` and `cases`.

```jsonc
{
    "configs": {
        "name": "(Win32/ADB-官服)Common Buttons",
        "resource": ["官服"],
        "controller": ["Win32", "ADB"],
    },
    "cases": [
        {
            "name": "Optional case name",
            "image": "帝江号_大世界",
            "hits": [
                "InWorld",
                {
                    "node": "RegionalDevelopmentButton",
                    "box": {
                        "Win32": [223, 32, 32, 19],
                        "ADB": [220, 30, 34, 20],
                    },
                },
            ],
        },
    ],
}
```

### `configs`

- `name`: optional test group name; recommended because it makes CLI output easier to read.
- `resource`: resource server name. It can be either a single string or an array of strings. The current repo uses `官服`.
- `controller`: controller type. It can be either a single string or an array of strings. The current repo uses `Win32` and `ADB`.

When `controller` or `resource` is an array, `maatools.config.mts` expands the file into multiple test groups using the Cartesian product of controller and resource values.

For example:

- `controller = ["ADB", "Win32"]`
- `resource = ["官服", "B服"]`

expands into four groups: `ADB-官服`, `ADB-B服`, `Win32-官服`, and `Win32-B服`.

Only use values that already have mappings in `maatools.config.mts` and screenshots in the test set.

### `cases`

- `cases` must be an array with at least one case.
- Each case must contain `image` and `hits`.
- `name` is optional, but useful when the screenshot name alone is not descriptive enough.
- `image` points to the screenshot file name; under the current schema, the extension may be omitted.

### `hits`

`hits` is the list of nodes expected to hit on that screenshot. It supports two forms.

1. Check only node hits:

```json
"hits": ["InWorld", "CloseButtonType1"]
```

1. Check both node hit and bounding box:

```json
"hits": [
    {
        "node": "RegionalDevelopmentButton",
        "box": [223, 32, 32, 19]
    }
]
```

`box` can also be a matrix object so different controllers or resources use different rectangles:

```json
"hits": [
    {
        "node": "SpecialButtonWithOffset",
        "box": {
            "ADB": [91, 587, 274, 48],
            "Win32": [100, 600, 250, 50],
            "default": [95, 590, 260, 50]
        }
    }
]
```

The current lookup priority in `maatools.config.mts` is:

- `controller:resource`
- `controller/resource`
- `resource:controller`
- `resource/controller`
- `controller`
- `resource`
- `default`
- `*`

The final value passed to `maa-tools` is still a fixed `[x, y, width, height]` rectangle, and all four values must be integers greater than or equal to 0.

If nothing should hit on a screenshot, use an empty array:

```json
"hits": []
```

Negative cases like this are important for catching false positives.

## Writing Guidelines

### 1. Keep one file focused on one capability

Follow the existing pattern and group tests by module or node family, for example:

- `tests/Common/Button/test_button.json`
- `tests/DeliveryJobs/test_region.json`
- `tests/EnvironmentMonitoring/test_job.json`

This makes failures easier to diagnose and regression samples easier to expand.

If the same screenshots and expectations apply to multiple controllers, prefer a controller array to avoid maintaining duplicate files.

If screenshots, expected hits, or `box` assertions already differ by controller, keep separate files instead.

If only the rectangle differs while screenshots and expected nodes stay the same, prefer keeping one file and using a matrix `box` object.

### 2. Use screenshot names that describe the scene directly

The current naming style works well: location + page hierarchy + key state, for example:

- `帝江号_大世界`
- `四号谷地_地区建设_仓储节点_货物装箱_填充至满`
- `武陵_拍照模式_拍摄目标未达成`

The more specific the screenshot name is, the easier the test set is to maintain later.

### 3. Include both positive and negative samples

Only testing “should hit” is not enough. For nodes that are easy to confuse or over-match, add at least one screenshot that should not hit.

Typical examples:

- A button template that looks similar to another button.
- A region-recognition node that might hit on a nearby region UI.
- A highlighted-state button that must not hit in its normal state.

### 4. Assert `box` only when position matters

If you only care whether the node hits, use the string form.

If you also care whether it hits the correct target location, especially for full-screen searches or screens with multiple similar targets, add a `box` assertion.

### 5. Cover real failure-prone scenes

Based on the current pipeline guidelines, prioritize screenshots like these:

- Frames around transitions where recognition is easy to miss.
- Screens with multiple clickable elements where misclicks are likely.
- Hover, selected, disabled, reward-popup, or other special states.
- Screens that differ slightly between ADB and Win32.

## Relation To Pipeline Guidelines

Node tests should support the current pipeline design rules:

- If a node is responsible for a key state decision, it should usually have tests so that the first `next` scan remains reliable.
- If you added intermediate recognition nodes to avoid `pre_delay` or `post_delay`, add tests for those intermediate nodes too.
- If a reusable node will be shared across multiple tasks, add tests before promoting wider reuse.

In short, better tests turn “seems to work” into something regression-safe and maintainable.

## Running Tests

After installing dependencies, run this from the project root:

```bash
pnpm test
```

In this repository, `package.json` maps that command to `maa-tools test`, and CI runs the same command.

Logs are written to:

```text
tests/maatools/
```

Detailed failures are written to:

```text
tests/maatools/error_details.json
```

If you only want to check configuration and resource conventions first, you can also run:

```bash
pnpm check
```

## Editor Support

The repository already maps `tests/**/*.json` to `tools/schema/test.schema.json` in `.vscode/settings.json`, and treats those files as `jsonc`.

That means:

- New test files get schema validation and completion in the editor.
- Comments are allowed, just like in existing test files.

Even so, keep committed files clean and readable; avoid leaving large blocks of temporary comments behind.

## Complete Example

```jsonc
{
    "configs": {
        "name": "(Multi-controller/multi-resource) Example Node Test",
        // Only use this form when mappings and screenshots exist for every combination
        "resource": ["官服", "B服"],
        "controller": ["Win32", "ADB"],
    },
    "cases": [
        {
            "name": "World home page",
            "image": "帝江号_大世界",
            "hits": ["InWorld"],
        },
        {
            "name": "Check controller-specific box positions",
            "image": "帝江号_某个复杂界面",
            "hits": [
                {
                    "node": "SpecialButtonWithOffset",
                    "box": {
                        "ADB": [91, 587, 274, 48],
                        "Win32": [100, 600, 250, 50],
                    },
                },
            ],
        },
        {
            "name": "Negative case: should not hit",
            "image": "武陵_拍照模式_拍摄目标未达成",
            "hits": [],
        },
    ],
}
```

## Before You Commit

After adding or editing node tests, check at least these items:

- The file name matches `test_*.json`.
- `configs.resource` and `configs.controller` are mapped in `maatools.config.mts`.
- If `configs.controller` or `configs.resource` is an array, confirm screenshots exist for every expanded combination.
- If `box` is a matrix object, confirm every required combination can be resolved, or provide a suitable `default`.
- `image` points to a real screenshot in the resolved directory; if the extension is omitted, make sure the base file name still matches.
- `hits` includes only the nodes that truly should hit on that image.
- A `box` assertion is added when location correctness matters.
- There are enough negative samples to catch common false positives.

That is what makes node tests useful as a safety net during refactors, threshold tuning, and template updates.
