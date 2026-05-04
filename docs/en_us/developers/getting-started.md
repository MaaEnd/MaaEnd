# Getting started

This guide walks through the full flow from idea to merge, using **auto-selling items** as an example.

## Prerequisites

- Git
- Python 3.10+
- Node.js 22
- pnpm 10+
- Go 1.25.6+

### Check your local environment

```bash
git --version
python3 --version   # or python --version (watch out for Python 2)
node --version
pnpm --version
go version
```

### Clone and set up

```bash
git clone --recursive https://github.com/MaaEnd/MaaEnd.git
cd MaaEnd
python tools/setup_workspace.py
pnpm install
```

> [!NOTE]
>
> If `setup_workspace.py` fails, see the [manual setup guide](#manual-setup-guide) below.

### Editor (recommended)

We recommend [Visual Studio Code](https://code.visualstudio.com/) (VS Code) for day-to-day development. After the clone and setup commands above, **open the repo root** in VS Code (it must contain `.vscode/extensions.json`) and install the **workspace recommended extensions** so your setup matches the team—Black, Prettier, **Maa Pipeline Support**, Markdownlint, Go, LLDB, and others listed in `.vscode/extensions.json`.

**How to install recommended extensions:**

1. **Open the workspace:** **File → Open Folder…** and select the cloned repository root.
2. **Notification:** If VS Code shows “This workspace has extension recommendations,” choose **Install** / **Install All**.
3. **Extensions view:** Press `Ctrl+Shift+X` (Windows/Linux) or `Cmd+Shift+X` (macOS), type `@recommended` in the search box, expand **Workspace Recommendations**, then **Install** what you need.
4. **Command Palette:** `Ctrl+Shift+P` / `Cmd+Shift+P` → run **`Extensions: Show Recommended Extensions`** and install from the list.

See also: [Workspace recommended extensions](https://code.visualstudio.com/docs/editor/extension-marketplace#_workspace-recommended-extensions) in the VS Code docs.

## 0. Git basics and conventions

This project relies on Git features—**submodules** in particular. Before you dive into code, be comfortable with basic branching and history.

**If Git is still new, work through this interactive tutorial first:**
👉 **[Learn Git Branching](https://learngitbranching.js.org/)**

Beyond `add` / `commit` / `push` / `pull`, two topics matter here:

### Conventional Commits

Commits follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). A clear history helps reviewers. Use these prefixes:

- `feat:` new feature (e.g. new Pipeline nodes)
- `fix:` bug fix (e.g. incorrect ROI)
- `docs:` documentation only
- `style:` formatting, whitespace, etc. (no behavior change)
- `chore:` tooling and housekeeping (not production logic)

> **Example:** `feat(SellProduct): add Regional Development auto-sell Pipeline`

### Submodule updates

Git submodules hold standalone dependencies and large assets (e.g. recognition **model** libraries).

**Common pitfall:** When you commit, `git status` may show `model` (or another submodule) as modified even though you never touched those files. You often just **pulled** or **switched branches**: the superproject now records a new submodule revision, but your **local submodule checkout is out of date**, so Git reports a diff.

The same mismatch can show up as strange changes or “model not found” errors after a pull or branch switch.

**What to do:** Whenever you see that ghost diff, or after each `git pull`, run this from the repo root:

```bash
git submodule update --init --recursive
```

## 1. Confirm the requirement

Open or file an [Issue](https://github.com/MaaEnd/MaaEnd/issues), e.g. “Automatically sell selected items from inventory.”

- Check whether the idea is in scope and whether someone is already working on it.
- If unsure, discuss in the Issue thread or ping a maintainer via Issue/PR.

## 2. Fork and open a Draft PR

```bash
# After forking, clone your fork and create a feature branch
git checkout -b feat/auto-sell-items
```

Open a **Draft PR** on GitHub early, with a clear title. Others can see work in progress and avoid duplicate effort.

## 3. Author Pipeline

Skim the [components guide](./components-guide.md) so you know where changes belong.

For “sell items,” organize Pipeline under the task name **SellProduct**: put the entry in `assets/resource/pipeline/SellProduct.json`, and split into the `SellProduct/` subfolder when the flow grows (same layout as the existing **Sell Product** task in this repo), then add nodes.

### Naming

Use PascalCase and keep the task prefix, e.g. `SellProductOpenBag`, `SellProductSelectItem`, `SellProductConfirmSell`.

### Think like a state machine / decision tree

Pipeline core logic is similar to a **finite state machine (FSM)** / **decision tree**: each node recognizes the screen, acts, then follows `next`:

```text
Open bag → recognize item → tap item → recognize sell → tap sell → recognize confirm → confirm → back to list
```

**Recognize before you act—never tap blind.** See [coding standards](./coding-standards.md) for more.

## 4. Screenshots and templates

Recognition needs template images. Capture them with the [dev tools](./tools-and-debug.md#development-tools):

- **Maa Pipeline Support** (VS Code extension)—screenshots, ROI, color pick.
- Or [MaaPipelineEditor](https://mpe.codax.site/docs) for visual Pipeline editing.
- All images and coordinates assume **1280×720**; with **Maa Pipeline Support** you do not need to change the game resolution—the framework scales captures.

When capturing, avoid HDR, night mode, and overlays such as NVIDIA filters or game++—they skew colors and break recognition.

![screenshot](https://github.com/user-attachments/assets/c9bb7157-97e4-4049-bb0a-e937456456f8)

Background clutter hurts match quality; an automatic green-screen tool helps. (Doing green-screen by hand is slow and inaccurate.)

![green background](https://github.com/user-attachments/assets/4da87f61-30fe-4a94-b6ed-68672877fff3)

Save templates under `assets/resource/image/SellProduct/`.

With images in place, add your first node. The example below uses **TemplateMatch** to find the **Regional Development** entry on the main screen, then **Click**; set `template` to the path under `assets/resource/image/`, tighten `roi` with the plugin (tune to your template and UI); if you used green-screen processing, set `green_mask`.

```json
{
    "SellProductMain": {
        "desc": "On main screen, find Regional Development entry and tap",
        "recognition": {
            "type": "TemplateMatch",
            "param": {
                "template": "SellProduct/RegionalDevelopmentEntry.png",
                "roi": [
                    400,
                    200,
                    480,
                    320
                ],
                "threshold": 0.7,
                "green_mask": true
            }
        },
        "action": {
            "type": "Click"
        },
        "pre_delay": 0,
        "post_delay": 0,
        "rate_limit": 0,
        "post_wait_freezes": 100,
        "next": [
            "SellProductLoop"
        ]
    }
}
```

On hit, `Click` runs (default: tap center of the match box).

Coding standards: avoid `pre_delay` / `post_delay` as fixed waits—device performance varies; 10 fps vs 60 fps changes how long animations take, and hard delays hide bugs that only show up for users.

Only use `pre_wait_freezes` / `post_wait_freezes` when the screen must settle; avoid delays otherwise. They track pixel change in the match ROI. Here `"post_wait_freezes": 100` means: after pixels in `[400, 200, 480, 320]` settle, wait another 100 ms.

The next node, `SellProductLoop`, should **recognize** that you are in Regional Development—do not assume the tap always succeeds. FSM rule: **recognize state, then act.**

```json
{
    "SellProductLoop": {
        "desc": "Main loop; expects to start from Regional Development",
        "recognition": "And",
        "all_of": [
            "InRegionalDevelopment"
        ],
        "pre_delay": 0,
        "post_delay": 0,
        "rate_limit": 0,
        "next": [
            "SellProductAuto",
            "SellProductValleyIV",
            "SellProductWuling",
            "SellProductTaskEnd"
        ]
    }
}
```

`InRegionalDevelopment` is a shared recognition node already defined in the project. **You can reuse any existing recognition logic by simply referencing its node name**—no need to duplicate the definition.

> **💡 Tip: Combination recognition (And / Or)**
>
> Beyond `TemplateMatch`, `Color`, and other basic methods, Pipeline supports logical **`And`** and **`Or`** conditions to combine multiple recognition nodes—useful for complex or variable UI states.
>
> See [MaaFramework Pipeline protocol – And/Or](https://maafw.com/docs/3.1-PipelineProtocol#and) for syntax and advanced usage.

The example below shows `InRegionalDevelopmentView2`, which recognizes the Regional Development secondary screen by OCR'ing the top function names.

```json
{
    "InRegionalDevelopmentView2": {
        "desc": "Regional Development secondary screen",
        "recognition": "OCR",
        "roi": [
            0,
            0,
            400,
            70
        ],
        "expected": [
            "据点",
            "據點",
            "Outpost",
            "拠点",
            "거점",
            "物资调度",
            "物資調度",
            "Stock Redistribution",
            "商品取引",
            "물자 관리",
            "仓储节点",
            "倉儲節點",
            "Depot Node",
            "保管ボックス",
            "저장고 노드",
            "环境监测",
            "環境監測",
            "Environment Monitoring",
            "環境観測",
            "환경 관측"
        ]
    }
}
```

Use **OCR** for text so i18n can apply; do not use TemplateMatch for text. The snippet above is illustrative—production uses more reusable patterns.

Prefer calling existing scene navigation nodes, then **JumpBack**, then the next state—don’t reinvent the wheel.

```json
{
    "SellProductMain": {
        "desc": "Task entry",
        "pre_delay": 0,
        "post_delay": 0,
        "rate_limit": 0,
        "next": [
            "SellProductLoop",
            "[JumpBack]SceneEnterMenuRegionalDevelopment"
        ]
    }
}
```

Handy entry points:

| Area           | Description                                   | Doc                                      |
| -------------- | --------------------------------------------- | ---------------------------------------- |
| Common buttons | White/yellow confirm, cancel, close, teleport | [common-buttons.md](./common-buttons.md) |
| SceneManager   | Navigate from any screen to a target scene    | [scene-manager.md](./scene-manager.md)   |

## 5. Debug and test

After a task is in place, test it. Tools and workflow: [Tools & debugging](./tools-and-debug.md).

Load resources in a dev tool, connect an emulator or PC client, and run your nodes.

- After each Pipeline edit, **reload resources** in the tool—no rebuild.
- Different frame rates (12 fps vs 60 fps) change animation timing and can shift when recognition fires.

> If you changed Go Service, run `python tools/build_and_install.py` first.

This walkthrough uses **Maa Pipeline Support**: open **Admin mode** in the control panel and attach the window.

![admin](https://github.com/user-attachments/assets/9d86ae89-0985-4606-bfa6-d4ec96dbee6f)

Click **Launch** on the Pipeline task to run and parse the flow. Logs show which nodes ran and which failed.

![launch](https://github.com/user-attachments/assets/6392310c-756c-4c33-b54a-9ab5ff9f4ad2)
![debug panel](https://github.com/user-attachments/assets/653c5314-f6ba-4ffc-91a5-739ab15382dc)

Iterate from there.

## 6. Ship supporting files

Once Pipeline works, wire up the rest:

### Task definition

Create or edit JSON under `assets/tasks/` to define the task entry and options for the UI. Example:

```json
{
    "task": [
        {
            "name": "SellProduct",
            "label": "$task.SellProduct.label",
            "entry": "SellProductMain",
            "description": "$task.SellProduct.description",
            "option": [
                "ValleyIVSell",
                "WulingSell"
            ],
            "group": [
                "daily"
            ]
        }
    ]
}
```

### i18n

Add task name/description keys under `assets/locales/interface/`. Example:

```json
{
    "task.SellProduct.label": "🛒 Sell Products",
    "task.SellProduct.description": "Use products at various outposts to exchange for corresponding procurement vouchers.\nTask options allow you to enable or disable sales features in specific regions."
}
```

Import the task file from `assets/interface.json`:

```json
{
    "import": [
        "tasks/DijiangRewards.json",
        "tasks/DailyRewards.json",
        "tasks/ClaimSimulationRewards.json",
        "tasks/SellProduct.json"
    ]
}
```

(Real `import` lists are longer—append in the same style as the rest of the project.)

## 7. Verify and submit

### Verify in MXU

Run `install/mxu.exe` and confirm the task appears and runs.

### Push and request review

```bash
git push origin feat/auto-sell-items
```

Mark the Draft PR **Ready for review** and wait for maintainer feedback.

Congratulations on shipping your first task!

## What to read next

- Reusable nodes → [components guide](./components-guide.md)
- Dev tools in depth → [tools & debugging](./tools-and-debug.md)
- Full coding standards → [coding standards](./coding-standards.md)
- Doc index → [README.md](./README.md)
- Pipeline protocol details → [Pipeline protocol](https://maafw.com/docs/3.1-PipelineProtocol/)

---

## Manual setup guide

<details>

1. Fully clone the repo and submodules.

2. Download [MaaFramework](https://github.com/MaaXYZ/MaaFramework/releases) and extract into `deps/`.

3. Download MaaDeps pre-built:

    ```bash
    python tools/maadeps-download.py
    ```

4. Build go-service and configure paths:

    ```bash
    python tools/build_and_install.py
    ```

    > To build cpp-algo as well, add `--cpp-algo`:
    >
    > ```bash
    > python tools/build_and_install.py --cpp-algo
    > ```

5. Copy contents of `deps/bin` from step 2 into `install/maafw/`.

6. Download [MXU](https://github.com/MistEO/MXU/releases) and extract into `install/`.

</details>
