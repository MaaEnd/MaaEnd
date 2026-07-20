# Quick Start

Take "Auto-sell Items" as an example, go through the complete development process from requirement to merge.

## Environment Preparation

- Git
- Python 3.10+
- Node.js 22
- pnpm 10+
- Go 1.25.6+

### Check Local Environment

```bash
git --version
python3 --version   # or python --version (be mindful of Python 2)
node --version
pnpm --version
go version
```

### Project Pull and Deployment

```bash
git clone --recursive https://github.com/MaaEnd/MaaEnd.git
cd MaaEnd
python tools/setup_workspace.py
pnpm install
```

> [!NOTE]
>
> If `setup_workspace.py` fails, refer to the [Manual Configuration Guide](#manual-configuration-guide) below.

**Common parameters for `setup_workspace.py`:**

| Parameter             | Description                                                                                                      |
| --------------------- | ---------------------------------------------------------------------------------------------------------------- |
| (No parameters)       | First-time initialization, skips already installed components                                                    |
| `--update`            | Force update all installed dependencies to the latest version                                                    |
| `--clean-cache`       | Clean the download cache directory                                                                               |
| `--cpp-algo-pr <N>`   | Download cpp-algo from the latest successful CI run of a specified PR (for quick testing of unmerged PR changes) |
| `--cpp-algo-run <ID>` | Download cpp-algo from a specified workflow run ID                                                               |

> `--cpp-algo-pr` and `--cpp-algo-run` are mutually exclusive; choose one. If not specified, defaults to downloading from the latest push build of the `v2` branch.

### Editor (Recommended)

We recommend using [Visual Studio Code](https://code.visualstudio.com/) (VS Code) as the daily development IDE for this project. After completing the clone and initialization above, **open the repository root directory** (must contain `.vscode/extensions.json`) with VS Code, and install the **Workspace Recommendations**, to align with the team environment (e.g., Black, Prettier, **Maa Pipeline Support**, Markdownlint, Go, LLDB, etc., full list in `.vscode/extensions.json` in the repository).

**Install Recommended Extensions:**

1.  **Open Workspace**: Menu **File → Open Folder…**, select the cloned repository root directory.
2.  **Notification Bar Installation**: If prompted in the bottom right corner with "This workspace has extension recommendations" or similar, choose **Install** / **Install All**.
3.  **Extensions View**: Press `Ctrl+Shift+X` (macOS: `Cmd+Shift+X`) to open the Extensions sidebar, type `@recommended` in the search box, expand **Workspace Recommendations**, and click **Install** for the needed extensions.
4.  **Command Palette**: `Ctrl+Shift+P` (macOS: `Cmd+Shift+P`) → Run **`Extensions: Show Recommended Extensions`**, and install from the list.

For more complete instructions, see the VS Code documentation: [Workspace Recommended Extensions](https://code.visualstudio.com/docs/editor/extension-marketplace#_workspace-recommended-extensions).

## 0. Git Prerequisites and Conventions

This project relies on certain Git features (especially submodules). Before you start writing code, ensure you are familiar with basic Git branching operations.

**If you are not very familiar with Git, please be sure to practice interactively through the following link first, and continue only after you are proficient:**
👉 **[Learn Git Branching (Git Interactive Learning and Practice)](https://learngitbranching.js.org/)**

In addition to basic `add` / `commit` / `push` / `pull`, participating in this project requires you to understand the following two points:

### Commit Conventions (Conventional Commits)

Code commits in this project strictly follow the [Conventional Commits specification](https://www.conventionalcommits.org/zh-hans/v1.0.0/). A clear commit history helps Reviewers quickly understand your intent. Use the following prefixes for each commit:

- `feat:` A new feature (e.g., wrote a new Pipeline node)
- `fix:` A Bug fix (e.g., fixed an ROI coordinate error)
- `docs:` Documentation only changes
- `style:` Changes that do not affect the meaning of the code (whitespace, formatting, missing semicolons, etc.)
- `chore:` Changes to the build process or auxiliary tools (does not involve production code)

> **Example**: `feat(SellProduct): Add regional construction auto-sell Pipeline`

### About Submodule Updates

This project uses Git Submodules to manage some independent dependency libraries and large files (e.g., model libraries used for recognition).

**🚧 Common pitfalls for beginners:**
When preparing a `commit` to submit code, you might see a prompt in the Git status indicating that `model` (or another submodule) has been modified, even though you are sure you haven't changed any model files.
This usually happens because you just pulled the latest code or switched branches, the submodule version pointer recorded in the main repository has been updated, but **your local submodule files haven't been synced yet**, causing Git to think you "modified" it.

Similarly, after pulling updates from the main branch or switching branches, you might encounter inexplicable modifications, or the code might report that the model cannot be found—this is also often because **the pointer in the main repository has been updated, but your local submodule files haven't been synced**.

> The main repository doesn't store the file contents of submodules, only a "pointer"—pointing to a specific commit SHA of the submodule repository.
> **💡 Solution:**
> When encountering this "ghost modification," or after every `git pull` to get the latest code, run the following command in the repository root directory:

```bash
git submodule update --init --recursive
```

## 1. Confirm Requirements

Go to the [Issue](https://github.com/MaaEnd/MaaEnd/issues) to find or create the corresponding requirement. For example: "Want to auto-sell specified items in the backpack."

- First, confirm if the requirement is reasonable and if someone is already working on it.
- If unsure, discuss it in the Issue, or directly create an Issue / PR to communicate with the maintainer.

## 2. Fork and Create a Draft PR

```bash
# After forking, clone your repository and create a feature branch
git checkout -b feat/auto-sell-items
```

Create a **Draft PR** on GitHub as early as possible, with a clear title stating what you are doing. This lets others know someone is working on it, avoiding duplicate work.

## 3. Write the Pipeline

First, read the [Component Guide](./components-guide.md) to understand the project structure and confirm where you should make changes.

For "Sell Product", organize the Pipeline by task name **SellProduct**: the entry point is written in `assets/resource/pipeline/SellProduct.json`. If the process is complex, you can create a subdirectory `SellProduct/` in the same location and split it into multiple JSON files (consistent with the existing "Sell Product" task in the MaaEnd repository), then start writing nodes.

### Naming

Node names use PascalCase and are consistent with the task prefix, e.g., `SellProductOpenBag`, `SellProductSelectItem`, `SellProductConfirmSell`.

### Think like a State Machine/Decision Tree

The core logic of the Pipeline is similar to a **Finite State Machine (FSM) / Decision Tree**: each node first recognizes the current screen, performs an operation, then uses `next` to jump to the next state:

```text
Open backpack → Recognize item → Click item → Recognize sell button → Click sell → Recognize confirmation popup → Confirm → Return to list
```

**Always recognize first, then act. Never click blindly.** See the [Coding Standards](./coding-standards.md) for more rules.

## 4. Screenshots and Templates

Recognition nodes require template images. Use the [Development Tools](./tools-and-debug.md#development-tools) to take screenshots:

- Recommended: **Maa Pipeline Support** (VS Code plugin) — allows direct screenshot capture, ROI selection, and color picking.
- You can also use [MaaPipelineEditor](https://mpe.codax.site/docs) to visually build Pipelines.
- All images and coordinates are based on **1280×720**. In the image below, we use **Maa Pipeline Support**; you don't need to switch the game resolution yourself, the framework will automatically resize images.

> [!NOTE]
> When taking screenshots, ensure HDR, night mode, and filters from Nvidia or game++ etc. are disabled, as colors will interfere with recognition. Use the color picker tool to verify if the color code is standard.

![screenshot](https://github.com/user-attachments/assets/c9bb7157-97e4-4049-bb0a-e937456456f8)

As you can see, our image has background interference, which reduces matching efficiency. We can use the automatic green screen tool to solve this problem. (Manual green screening is not recommended as it is slow and inaccurate)

![green background](https://github.com/user-attachments/assets/4da87f61-30fe-4a94-b6ed-68672877fff3)

Place the captured templates under `assets/resource/image/SellProduct/`.

Once we have the images, we can start writing the first node. Below, we use **TemplateMatch** to find the "Regional Construction" entry on the main interface, and after a hit, **Click** to enter. `template` is the relative path to your image placed under `assets/resource/image/`, `roi` is selected using the plugin to narrow the search area (needs adjustment based on your template and interface); if you processed the template with a green screen, you can add `green_mask`.

```json
{
    "SellProductMain": {
        "desc": "On the main interface, recognize the regional construction entry and click to enter",
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

This node will recognize this image. When a recognition hit occurs, it will execute `Click` (defaulting to the center of the match box).

Coding Standard: Using hard delays like `pre_delay` or `post_delay` is not recommended because performance varies greatly across devices. Waiting times for animations differ completely between 10 fps and 60 fps. Hard delays can mask many issues; what works in a development environment may not work in a user environment.

Use `pre_wait_freezes` or `post_wait_freezes` only when necessary to wait for the screen to stabilize; otherwise, delays should be avoided as much as possible. For example, `"post_wait_freezes": 100` in the text above means waiting 100 ms after pixel changes in the `roi` area `[400, 200, 480, 320]` have stopped.

The next step in `SellProductLoop` should continue using a recognition node to confirm entry into the regional construction interface, rather than assuming the click was successful. The most important rule for an FSM is: recognize and confirm the current state first, then perform the operation.

```json
{
    "SellProductLoop": {
        "desc": "Main loop, only supports starting from the regional construction interface",
        "recognition": "And",
        "all_of": [
            "InRegionalDevelopment"
        ],
        "pre_delay": 0,
        "post_delay": 0,
        "rate_limit": 0,
        "next": [
            "SellProductValleyIV",
            "SellProductWuling",
            "SellProductTaskEnd"
        ]
    }
}
```

The `InRegionalDevelopment` called in `all_of` above is a recognition node already defined in the project, used to confirm you are currently on the main regional construction interface. **You can directly reuse existing recognition logic by filling in the node name**, avoiding rewriting the same code.

> **💡 Advanced Tip: Combinational Recognition (And / Or)**
>
> In addition to traditional basic methods like `TemplateMatch` (template matching) and `Color` (color matching), the Pipeline also supports using logical conditions **`And`** and **`Or`** to combine multiple recognition nodes. This is very useful when dealing with complex or variable UI states.
>
> For specific syntax and advanced usage of combinational recognition, please refer to the [MaaFramework Official Documentation - Pipeline Protocol](https://maafw.com/docs/3.1-PipelineProtocol#and).

The example below shows another node, `InRegionalDevelopmentView2`, used to recognize the secondary interface of regional construction. It uses OCR to recognize the top function name to accurately confirm the current interface state:

```json
{
    "InRegionalDevelopmentView2": {
        "desc": "On the regional construction secondary interface",
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

For text recognition, use OCR to support i18n; do not use TemplateMatch for text recognition. The above is for demonstration purposes only; the project already has a more mature reusable solution.

It is recommended to directly call existing scene transition nodes. After completion, return via JumpBack, and then enter the next state, avoiding reinventing the wheel.

```json
{
    "SellProductMain": {
        "desc": "Script entry point",
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

Common reusable entry points are listed below:

| Node           | Description                                                                   | Documentation                            |
| -------------- | ----------------------------------------------------------------------------- | ---------------------------------------- |
| Common Buttons | White/yellow confirm, cancel, close, teleport, etc.                           | [common-buttons.md](./common-buttons.md) |
| SceneManager   | Universal jump: automatically navigate to the target scene from any interface | [scene-manager.md](./scene-manager.md)   |

## 5. Debugging and Testing

After completing a set of tasks, testing is required. See [Tools and Debugging](./tools-and-debug.md) for optional tools and procedures.

Load resources with the development tool, connect to the emulator or PC client, and run your nodes.

- Every time you modify the Pipeline, simply **reload the resources** in the tool; no recompilation is needed.
- Be aware that animation transition speeds differ at different frame rates (e.g., 12 fps vs 60 fps), which may cause recognition timing discrepancies.

> If you modified the Go Service, you must first run `python tools/build_and_install.py` to recompile.

The current example uses **Maa Pipeline Support** (VS Code plugin): enable Admin Mode on the control panel and connect to the window.

![admin](https://github.com/user-attachments/assets/9d86ae89-0985-4606-bfa6-d4ec96dbee6f)

Then click Launch on the Pipeline task; it will automatically start executing and parsing the task. Which nodes were executed and which node reported an error can be viewed in the logs.

![launch](https://github.com/user-attachments/assets/6392310c-756c-4c33-b54a-9ab5ff9f4ad2)
![debug panel](https://github.com/user-attachments/assets/653c5314-f6ba-4ffc-91a5-739ab15382dc)

Next, debug based on the feedback.

## 6. Complete Supporting Files

After the Pipeline runs, complete the supporting files:

### Task Definition

Create or modify a JSON file under `assets/tasks/` to define the task entry node and options for importing into the frontend. For example:

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

### i18n Text

Add translation keys for the task name and description in `assets/locales/interface/`. For example:

```json
{
    "task.SellProduct.label": "🛒 Sell Products",
    "task.SellProduct.description": "Use products to redeem corresponding dispatch vouchers at various outposts.\nYou can enable or disable sales functions for specific regions in the task options."
}
```

Finally, import the task file via `import` in `assets/interface.json`, for example:

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

(The actual file will have more entries; simply append according to the existing order in the project.)

## 7. Verification and Submission

### Verification in MXU

Launch `install/mxu.exe` and confirm that the task displays and runs correctly in the UI.

### Push and Request Review

```bash
git push origin feat/auto-sell-items
```

On GitHub, change the Draft PR to **Ready for Review**, select `v2` as the Base branch, and wait for maintainer review.

> If the Bug you fixed also exists in the latest stable release, you need to submit to the `release/vX.Y` branch. See the [Release Process](./release-process.md) for details.

Congratulations, you've completed your first task!

## What's Next

- Learn about reusable nodes to avoid reinventing the wheel → [Component Guide](./components-guide.md)
- Master development tool details → [Tools and Debugging](./tools-and-debug.md)
- View the complete version of coding standards → [Coding Standards](./coding-standards.md)
- All documentation index → [README.md](./README.md)
- More specific Pipeline protocol explanation → [Pipeline Protocol](https://maafw.com/docs/3.1-PipelineProtocol/)

---

## Manual Configuration Guide

<details>

1.  Fully clone the project and sub-repositories.

2.  Download [MaaFramework](https://github.com/MaaXYZ/MaaFramework/releases) and extract its contents into the `deps` folder.

3.  Download MaaDeps pre-built.

    ```bash
    python tools/maadeps-download.py
    ```

4.  Compile go-service and configure paths.

    ```bash
    python tools/build_and_install.py
    ```

    > If you also need to compile cpp-algo, add the `--cpp-algo` parameter:
    >
    > ```bash
    > python tools/build_and_install.py --cpp-algo
    > ```

5.  Copy the contents of `deps/bin` extracted in Step 2 to `install/maafw/`.

6.  Download [MXU](https://github.com/MistEO/MXU/releases) and extract it to `install/`.

</details>
